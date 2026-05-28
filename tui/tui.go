// Package tui provides the interactive terminal UI for selecting which packages
// to install.  It wraps the Bubble Tea library and exposes a single function,
// PromptUser, so that the rest of the program has no direct dependency on the
// TUI framework.
//
// Isolating the TUI here means it can be swapped for a different UI (a simple
// stdin prompt, a web page, etc.) without touching cmd or any other package.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// model is the Bubble Tea model for the package-selection screen.
type model struct {
	choices  []string       // packages the user can select
	cursor   int            // index of the currently highlighted row
	selected map[int]struct{} // set of selected row indices
}

// Init satisfies tea.Model.  No initial command is needed.
func (m model) Init() tea.Cmd { return nil }

// Update handles keyboard input and returns the next model state.
//
// Controls:
//
//	↑ / ↓   — move cursor
//	Space / Tab — toggle the item under the cursor
//	A       — select all / deselect all
//	Enter   — confirm and exit
//	q / Ctrl+C — quit without selecting
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down":
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
		// Toggle all: if everything is selected, clear; otherwise select all.
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

// View renders the current state of the picker as a string.
func (m model) View() string {
	s := "\nSelect packages to install (Space/Tab to toggle, A = all, Enter to confirm):\n\n"
	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		check := " "
		if _, ok := m.selected[i]; ok {
			check = "x"
		}
		s += fmt.Sprintf("%s [%s] %s\n", cursor, check, choice)
	}
	s += fmt.Sprintf("\n%d/%d selected  (q to quit)\n", len(m.selected), len(m.choices))
	return s
}

// PromptUser launches the interactive picker and returns the packages the user
// confirmed, in the original slice order (deterministic — iterates the slice,
// not the selected map).
func PromptUser(choices []string) ([]string, error) {
	initial := model{
		choices:  choices,
		selected: make(map[int]struct{}),
	}

	final, err := tea.NewProgram(initial).Run()
	if err != nil {
		return nil, err
	}

	m := final.(model)

	// Collect selected packages by iterating choices (not m.selected) to
	// guarantee a stable, index-ordered result regardless of map iteration.
	var selected []string
	for i, pkg := range m.choices {
		if _, ok := m.selected[i]; ok {
			selected = append(selected, pkg)
		}
	}
	return selected, nil
}