package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// A page that finishes loading while paragraphs are being selected must not
// crash the view.
func TestClipModeEndsWhenAPageLoads(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://example.org/six", textPage("Six", "1", "2", "3", "4", "5", "6"))
	m = press(m, "y", "down", "down", "down", "down", "down")
	m = open(m, "https://example.org/one", textPage("One", "only"))
	if m.mode != modeRead {
		t.Errorf("mode %v", m.mode)
	}
	_ = m.View()
}

// "Continue reading" reopens a page where it was left, and a quick look
// does not erase that position.
func TestContinueResumesPosition(t *testing.T) {
	db, _ := store.Default()
	target := "https://example.org/long-read"
	db.Visit(target, "Long", "web", "")
	db.SavePos(target, 0.5)
	m := sized(New("", "test"))
	m = open(m, target, textPage("Long", strings.Repeat("Many words in a long text. ", 600)))
	if m.cur.offset == 0 {
		t.Fatal("page opened at the top")
	}
	m.leavePage()
	un, _ := db.Unfinished(10)
	found := false
	for _, v := range un {
		if v.Target == target && v.Pos > 0.3 && v.Pos < 0.7 {
			found = true
		}
	}
	if !found {
		t.Errorf("position lost: %+v", un)
	}
}

// Notes and saved pages open from files; their links may navigate but must
// not run W5F actions.
func TestFilePagesCannotRunActions(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "C:/notes/Saved/x.md", &doc.Document{Title: "Saved"})
	if _, cmd := m.follow("w5f:queue/remove?u=https%3A%2F%2Fa"); cmd != nil {
		t.Error("a file page ran a queue action")
	}
	if _, cmd := m.follow("w5f:book/3"); cmd == nil {
		t.Error("navigation links from notes must work")
	}
}

func TestNotePasteWithCarriageReturns(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://example.org/p", textPage("P", "x"))
	m = press(m, "n")
	next, _ := m.Update(tea.PasteMsg{Content: "one\rtwo\r\nthree"})
	m = next.(Model)
	if got := m.note.text(); got != "one\ntwo\nthree" {
		t.Errorf("text %q", got)
	}
}
