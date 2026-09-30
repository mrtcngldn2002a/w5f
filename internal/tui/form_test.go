package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
)

func TestRegisteredFormAsksHidesAndSaves(t *testing.T) {
	old := forms
	defer func() { forms = old }()
	var got []string
	RegisterForm("w5f:test-form/", func(href string) (*Form, error) {
		return &Form{Title: "Site session", Intro: []string{"Paste them."},
			Fields: []FormField{{Label: "User-Agent"}, {Label: "Cookie", Hidden: true}},
			Save:   func(v []string) (string, string, error) { got = v; return "", "saved it", nil }}, nil
	})
	d := &doc.Document{URL: "w5f:test", Title: "t", Links: []doc.Link{{Href: "w5f:test-form/x", Text: "paste"}},
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "paste", Link: 1}}}}}
	m := sized(New("", "test"))
	next, _ := m.Update(loadedMsg{target: "w5f:test", doc: d, gen: loadGen.Load()})
	m = next.(Model)
	next, _ = m.follow("w5f:test-form/x")
	m = next.(Model)
	if m.mode != modeSecret || !strings.Contains(m.View().Content, "SITE SESSION") {
		t.Fatalf("form not open: mode %v\n%s", m.mode, m.View().Content)
	}
	next, _ = m.Update(tea.PasteMsg{Content: "Mozilla/5.0 Test"})
	m = next.(Model)
	if !strings.Contains(m.View().Content, "Mozilla/5.0 Test") {
		t.Error("a visible field shows what is typed")
	}
	m = press(m, "enter")
	next, _ = m.Update(tea.PasteMsg{Content: "cf_clearance=secret"})
	m = next.(Model)
	if strings.Contains(m.View().Content, "secret") {
		t.Fatal("a hidden field must never be shown")
	}
	m = press(m, "enter")
	if len(got) != 2 || got[0] != "Mozilla/5.0 Test" || got[1] != "cf_clearance=secret" || m.status != "saved it" || m.mode != modeRead {
		t.Fatalf("saved %q, status %q, mode %v", got, m.status, m.mode)
	}
	// esc cancels without saving
	got = nil
	next, _ = m.follow("w5f:test-form/x")
	m = press(next.(Model), "a", "esc")
	if got != nil || m.mode != modeRead {
		t.Error("esc saved")
	}
}
