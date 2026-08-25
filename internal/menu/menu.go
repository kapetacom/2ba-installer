// Package menu is the interactive service picker. It replaces the old
// stty+dd arrow-key TUI with a bubbletea program. Because the program is
// driven through tea.WithInput/tea.WithOutput, it works when the binary's
// stdin is a pipe (e.g. `curl … | sh` execs us) as long as the caller hands it
// the /dev/tty pair from internal/termx.
package menu

import (
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kapetacom/2ba-installer/internal/detect"
)

var (
	bold      = lipgloss.NewStyle().Bold(true)
	dim       = lipgloss.NewStyle().Faint(true)
	green     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	greenBold = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
)

// names/descs are the menu rows, in a fixed order that the whole binary relies
// on (selection index i == this row). Keep in sync with detect + main.
var (
	names = []string{"shell env", "opencode", "windsurf", "kimi", "continue", "cursor", "zcode"}
	descs = []string{
		"OPENAI_API_KEY/BASE for aider & similar tools",
		"OpenCode CLI (~/.config/opencode)",
		"Windsurf IDE model settings",
		"Kimi CLI / Kimi Code CLI",
		"Continue (prints manual steps)",
		"Cursor (prints manual steps)",
		"ZCode CLI / desktop (~/.zcode)",
	}
)

// Selections is the user's pick, in menu order.
type Selections struct {
	Shell, Opencode, Windsurf, Kimi, Continue, Cursor, Zcode bool
}

// Any reports whether at least one service is selected.
func (s Selections) Any() bool {
	return s.Shell || s.Opencode || s.Windsurf || s.Kimi || s.Continue || s.Cursor || s.Zcode
}

// Result is what Run returns after the menu closes.
type Result struct {
	Selections
	Confirmed bool // true when the user pressed enter, false when they quit
}

type model struct {
	sel      [7]bool
	cursor   int
	quitting bool
}

// New builds a menu with the given detection pre-ticks.
func New(initial detect.Services) model {
	return model{
		sel: [7]bool{
			initial.Shell, initial.Opencode, initial.Windsurf,
			initial.Kimi, initial.Continue, initial.Cursor,
			initial.Zcode,
		},
	}
}

func (m model) Init() tea.Cmd { return nil }

// Update handles keys: up/down move, space or 1-7 toggle, a selects all,
// enter confirms, q quits. It mirrors the historical install.sh key map.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+j":
			if m.cursor < len(names)-1 {
				m.cursor++
			}
		case " ":
			m.sel[m.cursor] = !m.sel[m.cursor]
		case "a":
			for i := range m.sel {
				m.sel[i] = true
			}
		case "enter":
			return m, tea.Quit
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "1", "2", "3", "4", "5", "6", "7":
			if i := int(msg.String()[0] - '1'); i >= 0 && i < len(names) {
				m.sel[i] = !m.sel[i]
			}
		}
	}
	return m, nil
}

// View renders the current menu state.
func (m model) View() string {
	var b strings.Builder
	b.WriteString(greenBold.Render("What should 2ba configure?") + "\n\n")
	for i, name := range names {
		cursor := " "
		label := name
		if i == m.cursor {
			cursor = greenBold.Render(">")
			label = bold.Render(name)
		}
		mark := dim.Render("[ ]")
		if m.sel[i] {
			mark = green.Render("[x]")
		}
		b.WriteString("    " + cursor + " " + mark + " " + label + padRight(name, 12) + " " + descs[i] + "\n")
	}
	b.WriteString("\n")
	b.WriteString("  " + dim.Render("up/down move · space toggle · 1-7 toggle · a all · enter continue · q quit") + "\n")
	return b.String()
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return ""
	}
	return strings.Repeat(" ", w-len(s))
}

// Run builds a menu from the detected services, runs it over the given
// reader/writer, and returns the outcome.
func Run(stdin io.Reader, stdout io.Writer) (Result, error) {
	final, err := runModel(stdin, stdout, New(detect.Detect()))
	if err != nil {
		return Result{}, err
	}
	return Result{
		Selections: Selections{
			Shell: final.sel[0], Opencode: final.sel[1], Windsurf: final.sel[2],
			Kimi: final.sel[3], Continue: final.sel[4], Cursor: final.sel[5],
			Zcode: final.sel[6],
		},
		Confirmed: !final.quitting,
	}, nil
}

// runModel runs a bubbletea program over the given reader/writer and returns
// the final model. The input must be a real terminal (see internal/termx):
// bubbletea does not read keys from a plain pipe.
func runModel(stdin io.Reader, stdout io.Writer, m model) (model, error) {
	p := tea.NewProgram(m, tea.WithInput(stdin), tea.WithOutput(stdout))
	final, err := p.Run()
	if err != nil {
		return model{}, err
	}
	if fm, ok := final.(model); ok {
		return fm, nil
	}
	return model{}, nil
}
