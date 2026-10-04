package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
	"w5f/internal/reddit"
	"w5f/internal/render"
	"w5f/internal/smallweb"
)

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "pgdown":
			msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "pgup":
			msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
		case "home":
			msg = tea.KeyPressMsg{Code: tea.KeyHome}
		case "end":
			msg = tea.KeyPressMsg{Code: tea.KeyEnd}
		case "shift+down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
		case "shift+up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
		case "alt+left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
		case "ctrl+q":
			msg = tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func sized(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	return next.(Model)
}

// Bubble Tea calls View before any WindowSizeMsg; this used to panic.
func TestViewBeforeWindowSize(t *testing.T) {
	m := New("", "test")
	if !strings.Contains(m.View().Content, "W5F // ARCHIVE NODE") {
		t.Error("first frame did not render")
	}
	m = New("some-file.html", "test") // still loading: no page yet
	if !strings.Contains(m.View().Content, "loading") {
		t.Error("loading frame did not render")
	}
}

func TestWelcomeFoldToggle(t *testing.T) {
	m := sized(New("", "test"))
	if strings.Contains(m.View().Content, "of which you hold the only card") {
		t.Fatal("collapsible content visible before opening")
	}
	// The first link is selected on load; ↓ walks down to the collapsible.
	if m.cur.focus != 1 {
		t.Fatalf("first link not preselected: focus=%d", m.cur.focus)
	}
	for i := 0; i < 80; i++ {
		if f := m.focused(); f != nil && f.Kind == render.FocusFold {
			break
		}
		m = press(m, "down")
	}
	if f := m.focused(); f == nil || f.Fold != 1 {
		t.Fatalf("focus = %+v", f)
	}
	m = press(m, "right")
	if strings.Contains(m.View().Content, "of which you hold the only card") {
		t.Error("→ opened the section: only enter opens")
	}
	m = press(m, "enter")
	if !strings.Contains(m.View().Content, "of which you hold the only card") {
		t.Error("collapsible did not open")
	}
	m = press(m, "enter")
	if strings.Contains(m.View().Content, "of which you hold the only card") {
		t.Error("collapsible did not close again")
	}
}

func TestHistoryBackAndForwardRepeatedly(t *testing.T) {
	m := sized(New("", "test"))
	a := &doc.Document{Title: "Page A", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "alpha"}}}}}
	b := &doc.Document{Title: "Page B", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "beta"}}}}}
	next, _ := m.Update(loadedMsg{target: "a", doc: a})
	next, _ = next.(Model).Update(loadedMsg{target: "b", doc: b})
	m = next.(Model)
	// v1's ELinks setup could only go forward once; W5F must survive loops.
	for i := 0; i < 3; i++ {
		m = press(m, "backspace")
		if m.cur.doc != a {
			t.Fatalf("round %d: back did not reach A", i)
		}
		m = press(m, "l")
		if m.cur.doc != b {
			t.Fatalf("round %d: forward did not reach B", i)
		}
	}
	m = press(m, "h", "h")
	if m.cur.target != "w5f:welcome" {
		t.Errorf("expected welcome page, got %q", m.cur.target)
	}
}

func TestHintsSelectLink(t *testing.T) {
	m := sized(New("", "test"))
	// The welcome page is longer than one screen; bring its section into view.
	for _, f := range m.cur.layout.Focus {
		if f.Kind == render.FocusFold {
			m.cur.offset = f.Line - 2
			m.clamp()
		}
	}
	m = press(m, "f")
	if m.mode != modeHints || len(m.hints) < 2 {
		t.Fatalf("mode=%v hints=%d", m.mode, len(m.hints))
	}
	label := 0
	for l, fi := range m.hints {
		if m.cur.layout.Focus[fi-1].Kind == render.FocusFold {
			label = l
		}
	}
	for _, r := range fmt.Sprint(label) {
		m = press(m, string(r))
	}
	if m.mode == modeHints {
		m = press(m, "enter")
	}
	if m.mode != modeRead {
		t.Error("hint did not activate")
	}
	if !m.isOpen(1) {
		t.Error("the collapsible hint should have toggled it open")
	}
}

// Lynx-style arrows: when the next link is far below, ↓ scrolls a page so the
// text in between is shown instead of jumping over it.
func TestLynxArrowsScrollBeforeSkippingText(t *testing.T) {
	var blocks []doc.Block
	blocks = append(blocks, doc.Paragraph{Text: doc.Inline{{Text: "top link", Link: 1}}})
	for i := 0; i < 80; i++ {
		blocks = append(blocks, doc.Paragraph{Text: doc.Inline{{Text: "filler text line"}}})
	}
	blocks = append(blocks, doc.Paragraph{Text: doc.Inline{{Text: "bottom link", Link: 2}}})
	d := &doc.Document{Title: "Long", Blocks: blocks, Links: []doc.Link{{Href: "https://a/1"}, {Href: "https://a/2"}}}
	m := sized(New("", "test"))
	next, _ := m.Update(loadedMsg{target: "long", doc: d})
	m = next.(Model)
	if m.cur.focus != 1 {
		t.Fatalf("top link not preselected")
	}
	m = press(m, "down")
	if m.cur.offset == 0 {
		t.Fatal("down should scroll when the next link is off screen")
	}
	if m.cur.focus == 2 {
		t.Fatal("down jumped over the text straight to the bottom link")
	}
	for i := 0; i < 10 && m.cur.focus != 2; i++ {
		m = press(m, "down")
	}
	if m.cur.focus != 2 {
		t.Fatalf("never reached the bottom link, focus=%d offset=%d", m.cur.focus, m.cur.offset)
	}
	m = press(m, "up")
	if m.cur.focus == 1 && m.cur.offset == 0 {
		t.Fatal("up jumped straight to the top link")
	}
	m = press(m, "left")
	if m.cur.target != "long" {
		t.Fatalf("← left the page, at %q: only backspace goes back", m.cur.target)
	}
	m = press(m, "backspace") // back to the welcome page
	if m.cur.target != "w5f:welcome" {
		t.Errorf("backspace did not go back, at %q", m.cur.target)
	}
}

func TestGotoPrompt(t *testing.T) {
	m := sized(New("", "test"))
	m = press(m, "g")
	if m.mode != modeGoto {
		t.Fatal("g should open the go-to prompt")
	}
	m = press(m, "s", "c", "p", "-", "1", "7", "3")
	if m.gotoBuf != "scp-173" {
		t.Fatalf("buffer = %q", m.gotoBuf)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.mode != modeRead || cmd == nil || m.loading != "https://scp-wiki.wikidot.com/scp-173" {
		t.Errorf("enter should start loading the resolved URL, loading=%q", m.loading)
	}
	m = press(m, "g", "x", "esc")
	if m.mode != modeRead {
		t.Error("esc should cancel the prompt")
	}
}

func TestRedditLoginFromGoto(t *testing.T) {
	var saved string
	saveSession = func(v string) error { saved = v; return nil }
	deleted := false
	deleteSession = func() error { deleted = true; return nil }
	defer func() { saveSession, deleteSession = reddit.SaveSession, reddit.DeleteSession }()

	m := sized(New("", "test"))
	m = press(m, "g")
	for _, r := range "reddit-login" {
		m = press(m, string(r))
	}
	m = press(m, "enter")
	if m.mode != modeSecret {
		t.Fatalf("mode = %v, want secret prompt", m.mode)
	}
	next, _ := m.Update(tea.PasteMsg{Content: "  abc123secret \n"})
	m = next.(Model)
	view := m.View().Content
	if strings.Contains(view, "abc123secret") {
		t.Fatal("the cookie must never be shown on screen")
	}
	if !strings.Contains(view, "•••••••••••• ") && !strings.Contains(view, "••••••••••••") {
		t.Error("masked input not shown")
	}
	m = press(m, "enter")
	if saved != "abc123secret" || m.mode != modeRead || !strings.Contains(m.status, "Reddit connected") {
		t.Errorf("saved=%q mode=%v status=%q", saved, m.mode, m.status)
	}
	// Logout from the same bar.
	m = press(m, "g")
	for _, r := range "reddit-logout" {
		m = press(m, string(r))
	}
	m = press(m, "enter")
	if !deleted {
		t.Error("reddit-logout did not remove the session")
	}
	// Esc cancels without saving.
	saved = ""
	m = press(m, "g")
	for _, r := range "reddit-login" {
		m = press(m, string(r))
	}
	m = press(m, "enter", "x", "esc")
	if saved != "" || m.mode != modeRead {
		t.Error("esc should cancel without saving")
	}
}

func TestDictionaryPopupOpensAndCloses(t *testing.T) {
	old := dictDir
	dictDir = func() string { return t.TempDir() } // no dictionary installed
	defer func() { dictDir = old }()
	m := sized(New("", "test"))
	before := m.cur.offset
	m = press(m, "d")
	if m.mode != modeDict {
		t.Fatal("d should open the dictionary")
	}
	view := m.View().Content
	if !strings.Contains(view, "DICTIONARY") || !strings.Contains(view, "dict-install") {
		t.Errorf("popup should explain how to install:\n%s", view)
	}
	m = press(m, "esc")
	if m.mode != modeRead || m.cur.offset != before {
		t.Error("esc should return to the page where it was")
	}
}

// Discovery keys work without shift; an input page takes the answer on enter.
func TestDiscoveryKeysAndInputPage(t *testing.T) {
	for _, k := range []string{"x", "X", "p", "P"} {
		m := sized(New("", "test"))
		m = open(m, "https://example.org/a", textPage("A", "Text."))
		next, cmd := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		if m = next.(Model); cmd == nil || m.loading == "" {
			t.Errorf("%s: no load (%q)", k, m.loading)
		}
	}
	m := sized(New("", "test"))
	addr := smallweb.WebSearchPage("https://search.example/search", "query", "Search.")
	m = open(m, addr, &doc.Document{Title: "Input requested", URL: addr, Blocks: []doc.Block{doc.Notice{Text: "Search."}}})
	m = press(m, "enter")
	if m.mode != modeGoto || m.gotoBuf != "? " {
		t.Errorf("enter on an input page: mode %v buf %q", m.mode, m.gotoBuf)
	}
}
