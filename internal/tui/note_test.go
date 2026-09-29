package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/personal"
)

func key(m Model, k tea.KeyPressMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

func TestNoteBoxSaveAndDiscard(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-096", textPage("SCP-096", "The shy guy."))
	m = press(m, "n", "h", "i")
	if m.mode != modeNote {
		t.Fatalf("mode %v", m.mode)
	}
	m = press(m, "enter", "s", "e", "c", "o", "n", "d", "left", "left")
	m = key(m, tea.KeyPressMsg{Code: tea.KeyBackspace}) // "seco|nd" → "sec|nd"
	m = key(m, tea.KeyPressMsg{Code: tea.KeyUp})
	m = key(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	m = press(m, "!")
	if got := m.note.text(); got != "hi!\nsecnd" {
		t.Fatalf("text %q", got)
	}
	if !strings.Contains(strings.Join(m.bodyLines(), "\n"), "NOTE") {
		t.Error("note box not drawn")
	}
	m = key(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.mode != modeRead || !strings.HasPrefix(m.status, "note saved to Notes/SCP-096.md") {
		t.Fatalf("after save: mode %v status %q", m.mode, m.status)
	}
	b, _ := os.ReadFile(filepath.Join(personal.Dir(), "Notes", "SCP-096.md"))
	if !strings.Contains(string(b), "hi!\nsecnd") || !strings.Contains(string(b), "catalog: \"FIC·SCP·096\"") {
		t.Errorf("note file:\n%s", b)
	}
	m = press(m, "n", "x", "esc")
	if m.mode != modeNote || !strings.Contains(m.note.msg, "esc again") {
		t.Fatalf("first esc must ask: %v %q", m.mode, m.note.msg)
	}
	m = press(m, "esc")
	if m.mode != modeRead {
		t.Error("second esc discards")
	}
	m = press(m, "n", "esc")
	if m.mode != modeRead {
		t.Error("an empty note box closes on the first esc")
	}
}
