package menu

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kapetacom/2ba-installer/internal/detect"
)

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(k tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: k}
}

// update applies one message and returns the concrete model.
func update(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestToggleByNumber(t *testing.T) {
	m := New(detect.Services{})
	m = update(m, keyRunes("1")) // shell on
	if !m.sel[0] {
		t.Error("1 should toggle shell on")
	}
	m = update(m, keyRunes("3")) // windsurf on
	if !m.sel[2] {
		t.Error("3 should toggle windsurf on")
	}
	m = update(m, keyRunes("1")) // shell off
	if m.sel[0] {
		t.Error("1 should toggle shell off")
	}
}

func TestArrowsAndSpace(t *testing.T) {
	m := New(detect.Services{})
	m = update(m, keyType(tea.KeyDown))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
	m = update(m, keyType(tea.KeySpace)) // toggle current (opencode)
	if !m.sel[1] {
		t.Error("space should toggle the current row (opencode)")
	}
	// cannot move up past the first row
	for i := 0; i < 3; i++ {
		m = update(m, keyType(tea.KeyUp))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped)", m.cursor)
	}
}

func TestSelectAll(t *testing.T) {
	m := New(detect.Services{})
	m = update(m, keyRunes("a"))
	for i, s := range m.sel {
		if !s {
			t.Errorf("row %d not selected after 'a'", i)
		}
	}
}

func TestPreTicks(t *testing.T) {
	m := New(detect.Services{Shell: true, Windsurf: true})
	if !m.sel[0] || !m.sel[2] {
		t.Errorf("expected shell+windsurf pre-ticked: %v", m.sel)
	}
	if m.sel[1] || m.sel[3] {
		t.Errorf("unexpected pre-ticks: %v", m.sel)
	}
}

func TestQuitFlag(t *testing.T) {
	m := New(detect.Services{})
	m = update(m, keyRunes("q"))
	if !m.quitting {
		t.Error("q should set the quitting flag")
	}
}
