package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
)

func TestClipModeSelectsAndSaves(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-682", textPage("SCP-682", "First paragraph.", "Second paragraph.", "Third paragraph."))
	if m = press(m, "y"); m.mode != modeClip {
		t.Fatalf("mode %v", m.mode)
	}
	m = press(m, "down")
	m = key(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	lo, hi := m.clip.span()
	if lo != 1 || hi != 2 {
		t.Fatalf("selection %d..%d", lo, hi)
	}
	m = press(m, "enter")
	if m.mode != modeRead || !strings.Contains(m.status, "clipping saved (2 paragraphs)") {
		t.Fatalf("status %q", m.status)
	}
	rel := m.status[strings.Index(m.status, " to ")+4:]
	b, err := os.ReadFile(personalPath(rel))
	if err != nil || !strings.Contains(string(b), "> Second paragraph.\n>\n> Third paragraph.") || strings.Contains(string(b), "First") {
		t.Errorf("clipping %v:\n%s", err, b)
	}
	m = open(m, "w5f:feeds", &doc.Document{Title: "Empty"})
	if m = press(m, "y"); m.mode != modeRead || !strings.Contains(m.status, "nothing to clip") {
		t.Errorf("empty page: %v %q", m.mode, m.status)
	}
}
