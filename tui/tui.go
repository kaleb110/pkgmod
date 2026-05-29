// Package tui provides the interactive terminal UI for selecting which packages
// to install.  It wraps Bubble Tea for the event loop and Lip Gloss for
// colours and layout, and exposes a single function — PromptUser — so that
// the rest of the program has no direct dependency on the TUI framework.
//
// The list is fully virtualised: only the rows that fit in the terminal window
// are rendered, so the UI works correctly whether there are 5 packages or 500.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Layout constants ──────────────────────────────────────────────────────────

const (
	// headerLines is the number of fixed lines the header occupies.
	headerLines = 4
	// footerLines is the number of fixed lines the footer occupies.
	footerLines = 3
	// minVisibleRows is the minimum list rows to show regardless of terminal size.
	minVisibleRows = 3
	// fallbackHeight is used before the first tea.WindowSizeMsg arrives.
	fallbackHeight = 24
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	styleManagerBadge = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("205")).
				Padding(0, 1)

	styleCursor = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	styleCircleFilled = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)

	styleCircleEmpty = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

	styleRowHighlighted = lipgloss.NewStyle().
				Background(lipgloss.Color("236")) // dark highlight band

	styleNameHighlighted = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Bold(true)

	styleNameNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	styleCount = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	styleScrollbar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("239"))

	styleScrollThumb = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205"))

	styleScrollHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)

	styleKey = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	styleKeyHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	styleDivider = lipgloss.NewStyle().
			Foreground(lipgloss.Color("237"))
)

// ── Model ─────────────────────────────────────────────────────────────────────

// model is the Bubble Tea model for the package-selection screen.
type model struct {
	manager  string           // e.g. "pnpm" — shown in the header badge
	choices  []string         // full package list, stable order
	cursor   int              // index of the currently highlighted row
	viewport int              // index of the first visible row (scroll offset)
	height   int              // terminal height in rows, from WindowSizeMsg
	selected map[int]struct{} // set of selected row indices
	aborted  bool             // true when user pressed q / ctrl+c
}

// visibleRows returns how many list rows fit in the current terminal height.
func (m model) visibleRows() int {
	h := m.height
	if h <= 0 {
		h = fallbackHeight
	}
	rows := h - headerLines - footerLines
	if rows < minVisibleRows {
		rows = minVisibleRows
	}
	if rows > len(m.choices) {
		rows = len(m.choices)
	}
	return rows
}

// clampViewport ensures the viewport window stays valid after cursor moves:
//   - cursor above viewport  → scroll up
//   - cursor below viewport  → scroll down
//   - viewport past end      → clamp back
func (m *model) clampViewport() {
	vis := m.visibleRows()
	// Scroll up if cursor moved above the window.
	if m.cursor < m.viewport {
		m.viewport = m.cursor
	}
	// Scroll down if cursor moved below the window.
	if m.cursor >= m.viewport+vis {
		m.viewport = m.cursor - vis + 1
	}
	// Clamp viewport so it never shows empty space at the bottom.
	maxViewport := len(m.choices) - vis
	if maxViewport < 0 {
		maxViewport = 0
	}
	if m.viewport > maxViewport {
		m.viewport = maxViewport
	}
	if m.viewport < 0 {
		m.viewport = 0
	}
}

// Init satisfies tea.Model.  No initial command is needed.
func (m model) Init() tea.Cmd { return nil }

// Update handles keyboard input and window resize events.
//
// Controls:
//
//	↑ / k         — move cursor up one row
//	↓ / j         — move cursor down one row
//	PgUp / ctrl+u — scroll up half a page
//	PgDn / ctrl+d — scroll down half a page
//	g             — jump to top
//	G             — jump to bottom
//	Space / Tab   — toggle the item under the cursor
//	A             — select all / deselect all
//	Enter         — confirm and proceed with installation
//	q / Ctrl+C    — abort without installing anything
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// Terminal was resized (or first render): recalculate the viewport.
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.clampViewport()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.clampViewport()
			}

		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
				m.clampViewport()
			}

		case "pgup", "ctrl+u":
			half := m.visibleRows() / 2
			m.cursor -= half
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.clampViewport()

		case "pgdown", "ctrl+d":
			half := m.visibleRows() / 2
			m.cursor += half
			if m.cursor >= len(m.choices) {
				m.cursor = len(m.choices) - 1
			}
			m.clampViewport()

		case "g":
			m.cursor = 0
			m.clampViewport()

		case "G":
			m.cursor = len(m.choices) - 1
			m.clampViewport()

		case " ", "tab":
			if _, ok := m.selected[m.cursor]; ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}

		case "a":
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
	}

	return m, nil
}

// View renders the visible slice of the list plus fixed header and footer.
func (m model) View() string {
	var b strings.Builder
	vis := m.visibleRows()

	// ── Header ────────────────────────────────────────────────────────────────
	badge   := styleManagerBadge.Render(m.manager)
	heading := styleHeader.Render(" packages to install")
	fmt.Fprintf(&b, "\n  Using %s%s\n", badge, heading)

	// Divider line
	b.WriteString(styleDivider.Render("  " + strings.Repeat("─", 46)) + "\n\n")

	// ── Virtualised list ──────────────────────────────────────────────────────
	//
	// Build the scrollbar track alongside the list rows so they share the same
	// height.  The thumb position is proportional to viewport / total.

	end := m.viewport + vis
	if end > len(m.choices) {
		end = len(m.choices)
	}

	// Scrollbar: only shown when the list is taller than the viewport.
	showScrollbar := len(m.choices) > vis
	var thumbTop, thumbBot int
	if showScrollbar && vis > 0 {
		// Map viewport position to a thumb position within [0, vis).
		thumbTop = m.viewport * vis / len(m.choices)
		thumbBot = (m.viewport+vis)*vis/len(m.choices)
		if thumbBot <= thumbTop {
			thumbBot = thumbTop + 1
		}
		if thumbBot > vis {
			thumbBot = vis
		}
	}

	for row, i := 0, m.viewport; i < end; i, row = i+1, row+1 {
		isHighlighted := m.cursor == i
		_, isSelected := m.selected[i]

		// Cursor glyph
		cursor := "  "
		if isHighlighted {
			cursor = styleCursor.Render("❯ ")
		}

		// Toggle circle
		circle := styleCircleEmpty.Render("○")
		if isSelected {
			circle = styleCircleFilled.Render("●")
		}

		// Package name
		name := styleNameNormal.Render(m.choices[i])
		if isHighlighted {
			name = styleNameHighlighted.Render(m.choices[i])
		}

		// Compose the row content
		rowContent := fmt.Sprintf("  %s%s  %s", cursor, circle, name)

		// Apply background highlight band across the full row
		if isHighlighted {
			// Pad to consistent width so the highlight band spans the line
			rowContent = styleRowHighlighted.Width(50).Render(rowContent)
		}

		// Scrollbar column
		scrollCol := "  "
		if showScrollbar {
			if row >= thumbTop && row < thumbBot {
				scrollCol = styleScrollThumb.Render(" ▐")
			} else {
				scrollCol = styleScrollbar.Render(" │")
			}
		}

		b.WriteString(rowContent + scrollCol + "\n")
	}

	// ── Scroll hints ──────────────────────────────────────────────────────────
	//
	// Show "▲ N more above" / "▼ N more below" so the user always knows
	// their position in the list without having to count scrollbar pixels.

	aboveCount := m.viewport
	belowCount := len(m.choices) - end

	if aboveCount > 0 || belowCount > 0 {
		b.WriteString("\n")
	}
	if aboveCount > 0 {
		hint := fmt.Sprintf("  ▲ %d more above", aboveCount)
		b.WriteString(styleScrollHint.Render(hint) + "\n")
	}
	if belowCount > 0 {
		hint := fmt.Sprintf("  ▼ %d more below", belowCount)
		b.WriteString(styleScrollHint.Render(hint) + "\n")
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString("\n")

	// Selected count
	countStr := fmt.Sprintf("  %d / %d selected", len(m.selected), len(m.choices))
	b.WriteString(styleCount.Render(countStr) + "\n")

	// Key hints
	keys := fmt.Sprintf("  %s%s  %s%s  %s%s  %s%s  %s%s",
		styleKey.Render("space"), styleKeyHint.Render(" toggle  "),
		styleKey.Render("a"), styleKeyHint.Render(" all  "),
		styleKey.Render("enter"), styleKeyHint.Render(" install  "),
		styleKey.Render("g/G"), styleKeyHint.Render(" top/bottom  "),
		styleKey.Render("q"), styleKeyHint.Render(" quit"),
	)
	b.WriteString(keys + "\n")

	return b.String()
}

// ── Public API ────────────────────────────────────────────────────────────────

// PromptUser launches the interactive picker and returns the packages the user
// confirmed, in the original slice order (deterministic — iterates the slice,
// not the selected map).
//
// Returns (nil, nil) when the user presses q / ctrl+c so the caller can
// distinguish "aborted" from "confirmed with nothing toggled".
func PromptUser(manager string, choices []string) ([]string, error) {
	initial := model{
		manager:  manager,
		choices:  choices,
		height:   fallbackHeight,
		selected: make(map[int]struct{}),
	}

	// tea.WithAltScreen keeps the picker in a private buffer so it doesn't
	// scroll the user's terminal history.
	final, err := tea.NewProgram(initial, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}

	m := final.(model)

	// User explicitly quit — return nil so the caller prints "Aborted."
	if m.aborted {
		return nil, nil
	}

	// Collect in slice order for deterministic results.
	var selected []string
	for i, pkg := range m.choices {
		if _, ok := m.selected[i]; ok {
			selected = append(selected, pkg)
		}
	}
	return selected, nil
}