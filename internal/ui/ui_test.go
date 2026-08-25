package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// everyLineSameWidth reports whether all lines of a rendered panel have the
// same display width (the box is not ragged).
func everyLineSameWidth(t *testing.T, rendered string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least a border, content and border, got %d lines", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != lipgloss.Width(lines[0]) {
			t.Errorf("line %d has width %d, want %d:\n%s", i, w, lipgloss.Width(lines[0]), rendered)
		}
	}
}

func TestSummary(t *testing.T) {
	got := Summary("zcode, opencode", "amber", "https://api.2ba.ai/v1")
	for _, want := range []string{"Configuring", "zcode, opencode", "model", "amber", "base", "https://api.2ba.ai/v1"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	everyLineSameWidth(t, got)
}

func TestSummaryNoServices(t *testing.T) {
	got := Summary("", "amber", "https://api.2ba.ai/v1")
	if !strings.Contains(got, "No services selected") {
		t.Errorf("summary missing no-services title:\n%s", got)
	}
	everyLineSameWidth(t, got)
}

func TestDone(t *testing.T) {
	got := Done("sk-abc…wxyz", "amber", "https://2ba.ai/api-keys")
	for _, want := range []string{"done", "sk-abc…wxyz", "model", "amber", "https://2ba.ai/api-keys"} {
		if !strings.Contains(got, want) {
			t.Errorf("done panel missing %q:\n%s", want, got)
		}
	}
	everyLineSameWidth(t, got)
}

func TestDryRun(t *testing.T) {
	got := DryRun()
	for _, want := range []string{"dry run complete", "re-run without --dry-run"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run panel missing %q:\n%s", want, got)
		}
	}
	everyLineSameWidth(t, got)
}

func TestLogLines(t *testing.T) {
	log := Log("did a thing")
	if !strings.HasPrefix(log, "  ✓ ") || !strings.Contains(log, "did a thing") {
		t.Errorf("log line wrong: %q", log)
	}
	warn := Warn("needs attention")
	if !strings.HasPrefix(warn, "  ! ") || !strings.Contains(warn, "needs attention") {
		t.Errorf("warn line wrong: %q", warn)
	}
	hint := Hint("a continuation")
	if !strings.HasPrefix(hint, "    ") || !strings.Contains(hint, "a continuation") {
		t.Errorf("hint line wrong: %q", hint)
	}
}
