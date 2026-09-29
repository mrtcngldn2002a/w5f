package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
	"w5f/internal/fiction"
)

func TestAO3TypedFilterFromGoto(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://archiveofourown.org/tags/X/works", &doc.Document{Title: "X",
		URL: "w5f:fiction/ao3/page?u=https%3A%2F%2Farchiveofourown.org%2Ftags%2FX%2Fworks"})
	m = press(m, "g")
	for _, r := range "f words 1000-" {
		m = press(m, string(r))
	}
	m = press(m, "enter")
	if !strings.HasPrefix(m.loading, "w5f:fiction/ao3/filter/set?") {
		t.Errorf("loading %q", m.loading)
	}
}

func TestAO3LoginFromGoto(t *testing.T) {
	var saved string
	origReddit := saveSession
	saveAO3Session = func(v string) error { saved = v; return nil }
	redditSaved := false
	saveSession = func(string) error { redditSaved = true; return nil }
	defer func() { saveAO3Session, saveSession = fiction.SaveAO3Session, origReddit }()

	m := sized(New("", "test"))
	m = press(m, "g")
	for _, r := range "ao3-login" {
		m = press(m, string(r))
	}
	m = press(m, "enter")
	if m.mode != modeSecret || !strings.Contains(m.View().Content, "CONNECT AO3") {
		t.Fatalf("mode = %v, want the AO3 prompt", m.mode)
	}
	next, _ := m.Update(tea.PasteMsg{Content: "ao3secretvalue"})
	m = next.(Model)
	if strings.Contains(m.View().Content, "ao3secretvalue") {
		t.Fatal("the cookie must never be shown on screen")
	}
	m = press(m, "enter")
	if saved != "ao3secretvalue" || redditSaved || !strings.Contains(m.status, "AO3 connected") {
		t.Errorf("saved %q, reddit %v, status %q", saved, redditSaved, m.status)
	}
}
