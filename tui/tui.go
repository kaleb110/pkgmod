// Package tui provides the interactive terminal UI for selecting which packages
// to install.  It wraps Bubble Tea for the event loop and Lip Gloss for
// colours and layout, and exposes a single function — PromptUser — so that
// the rest of the program has no direct dependency on the TUI framework.
//
// Isolating the TUI here means it can be swapped for a different UI (a plain
// stdin prompt, a web page, etc.) without touching cmd or any other package.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	// Header bar shown at the top of the picker.
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")) // bright pink

	// The manager badge shown in the header (e.g. "pnpm").
	styleManagerBadge = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).   // black text
				Background(lipgloss.Color("205")). // pink background
				Padding(0, 1)

	// The ">" cursor next to the highlighted row.
	styleCursor = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	// Filled circle for a selected item  ●
	styleCircleFilled = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")). // pink
				Bold(true)

	// Empty circle for an unselected item  ○
	styleCircleEmpty = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")) // grey

	// Package name when the row is highlighted by the cursor.
	styleHighlighted = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")). // bright white
				Bold(true)

	// Package name in normal (non-highlighted) state.
	styleNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")) // light grey

	// The count line at the bottom (e.g. "3 / 7 selected").
	styleCount = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")). // mid-grey
			Italic(true)

	// Keybinding hints in the footer.
	styleKey = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	styleKeyHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))
)

// ── Model ─────────────────────────────────────────────────────────────────────

// model is the Bubble Tea model for the package-selection screen.
type model struct {
	manager string         // e.g. "pnpm" — shown in the header
	choices []string       // packages the user can select, in stable order
	cursor  int            // index of the currently highlighted row
	selected map[int]struct{} // set of selected row indices
	aborted bool           // true when the user pressed q / ctrl+c
}

// Init satisfies tea.Model.  No initial command is needed.
func (m model) Init() tea.Cmd { return nil }

// Update handles keyboard input and returns the next model state.
//
// Controls:
//
//	↑ / k       — move cursor up
//	↓ / j       — move cursor down
//	Space / Tab — toggle the item under the cursor
//	A           — select all / deselect all
//	Enter       — confirm and proceed with installation
//	q / Ctrl+C  — quit without installing anything
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "ctrl+c", "q":
		// Mark as aborted so PromptUser can return nil instead of an empty
		// slice — the caller uses this to print "Aborted." rather than
		// "Nothing selected." and skips installation entirely.
		m.aborted = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.choices)-1 {
			m.cursor++
		}

	case " ", "tab":
		if _, ok := m.selected[m.cursor]; ok {
			delete(m.selected, m.cursor)
		} else {
			m.selected[m.cursor] = struct{}{}
		}

	case "a":
		// Toggle all: if everything is already selected, deselect all.
		if len(m.selected) == len(m.choices) {
			m.selected = make(map[int]struct{})
		} else {
			for i := range m.choices {
				m.selected[i] = struct{}{}
			}
		}

	case "enter":
		return m, tea.Quit
	}

	return m, nil
}

// View renders the current state of the picker as a styled string.
func (m model) View() string {
	// ── Header ────────────────────────────────────────────────────────────────
	badge := styleManagerBadge.Render(m.manager)
	heading := styleHeader.Render(" packages to install")
	header := fmt.Sprintf("\n  Using %s%s\n\n", badge, heading)

	// ── Package list ──────────────────────────────────────────────────────────
	list := ""
	for i, choice := range m.choices {
		// Cursor column
		cursor := "  "
		if m.cursor == i {
			cursor = styleCursor.Render("❯ ")
		}

		// Toggle circle
		circle := styleCircleEmpty.Render("○")
		if _, ok := m.selected[i]; ok {
			circle = styleCircleFilled.Render("●")
		}

		// Package name
		name := styleNormal.Render(choice)
		if m.cursor == i {
			name = styleHighlighted.Render(choice)
		}

		list += fmt.Sprintf("  %s%s  %s\n", cursor, circle, name)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	count := styleCount.Render(fmt.Sprintf(
		"\n  %d / %d selected\n\n", len(m.selected), len(m.choices),
	))

	keys := fmt.Sprintf("  %s%s  %s%s  %s%s  %s%s\n",
		styleKey.Render("space"), styleKeyHint.Render(" toggle  "),
		styleKey.Render("a"), styleKeyHint.Render(" all  "),
		styleKey.Render("enter"), styleKeyHint.Render(" install  "),
		styleKey.Render("q"), styleKeyHint.Render(" quit"),
	)

	return header + list + count + keys
}

// ── Public API ────────────────────────────────────────────────────────────────

// PromptUser launches the interactive picker and returns the packages the user
// confirmed, in the original slice order (deterministic — iterates the slice,
// not the selected map).
//
// Returns (nil, nil) when the user presses q / ctrl+c so the caller can
// distinguish "quit without selecting" from "confirmed with nothing toggled".
func PromptUser(manager string, choices []string) ([]string, error) {
	initial := model{
		manager:  manager,
		choices:  choices,
		selected: make(map[int]struct{}),
	}

	final, err := tea.NewProgram(initial).Run()
	if err != nil {
		return nil, err
	}

	m := final.(model)

	// User explicitly quit — return nil so the caller can print "Aborted."
	// and skip installation without treating it as an error.
	if m.aborted {
		return nil, nil
	}

	// Collect selected packages by iterating choices (not m.selected) to
	// guarantee a stable, index-ordered result regardless of map iteration order.
	var selected []string
	for i, pkg := range m.choices {
		if _, ok := m.selected[i]; ok {
			selected = append(selected, pkg)
		}
	}
	return selected, nil
}