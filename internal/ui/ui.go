// Package ui renders the framed panels the installer prints around its plain
// event log. It uses lipgloss, which the menu already depends on; the color
// profile is auto-detected, so a non-color terminal just gets the boxes.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	greenC  = lipgloss.Color("2")
	yellowC = lipgloss.Color("3")
	cyanC   = lipgloss.Color("14")
	grayC   = lipgloss.Color("240")

	dim     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	greenB  = lipgloss.NewStyle().Foreground(greenC).Bold(true)
	yellowB = lipgloss.NewStyle().Foreground(yellowC).Bold(true)
	link    = lipgloss.NewStyle().Foreground(cyanC)
)

// hpad is the total horizontal padding inside the panel border (1 cell on
// each side); Padding(1) applies 1 cell to all four sides.
const hpad = 2

// panel renders lines inside a rounded box with a coloured border. Width is
// measured with lipgloss.Width so styling never skews the alignment; the box
// width must cover the widest line plus the horizontal padding, or lipgloss
// wraps it.
func panel(border lipgloss.Color, lines ...string) string {
	width := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(1).
		Width(width + hpad)
	return box.Render(strings.Join(lines, "\n"))
}

// labeled aligns the values of the pairs under a dim label column.
func labeled(pairs ...[2]string) []string {
	w := 0
	for _, p := range pairs {
		if len(p[0]) > w {
			w = len(p[0])
		}
	}
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		label := p[0] + strings.Repeat(" ", w-len(p[0])+1)
		out = append(out, dim.Render(label)+p[1])
	}
	return out
}

// Summary is the panel printed right after the service picker closes: what
// will be configured, plus the model and API base that get written into it.
func Summary(services, model, base string) string {
	var title string
	if services != "" {
		title = dim.Render("Configuring ") + greenB.Render(services)
	} else {
		title = dim.Render("No services selected") + yellowB.Render(" — key will be stored only")
	}
	lines := append([]string{title}, labeled(
		[2]string{"model", model},
		[2]string{"base", base},
	)...)
	return panel(grayC, lines...)
}

// Done is the success panel printed once configuration has been written.
func Done(key, model, apiKeysURL string) string {
	lines := append([]string{greenB.Render("✓ done")}, labeled(
		[2]string{"key", key},
		[2]string{"model", model},
		[2]string{"api keys", link.Render(apiKeysURL)},
	)...)
	return panel(greenC, lines...)
}

// DryRun is the panel printed instead of Done on a dry run.
func DryRun() string {
	return panel(yellowC,
		yellowB.Render("dry run complete"),
		dim.Render("re-run without --dry-run to apply"),
	)
}

// Log renders an event-log line for an action taken: a green check plus the
// message.
func Log(msg string) string { return "  " + greenB.Render("✓") + " " + msg }

// Warn renders an event-log line: a yellow bang plus the message.
func Warn(msg string) string { return "  " + yellowB.Render("!") + " " + msg }

// Note renders an event-log line for a no-op outcome (nothing was changed):
// dim, no symbol, so it reads quieter than an action.
func Note(msg string) string { return "  " + dim.Render(msg) }

// Hint renders a dim continuation line under a Log/Warn line.
func Hint(msg string) string { return "    " + dim.Render(msg) }
