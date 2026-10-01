package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"w5f/internal/doc"
	"w5f/internal/store"
	"w5f/internal/theme"
)

func wide(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return next.(Model)
}

func plainView(m Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

// On a wide screen the rooms stay on the left and the page starts beside
// them, wider than the old 72-column measure; \ hides them.
func TestSideMenu(t *testing.T) {
	m := wide(New("", "test"))
	rows := plainView(m)
	if !strings.Contains(strings.Join(rows, "\n"), "THE LIBRARY") || !strings.Contains(rows[3], "» 1  The Reading Room") {
		t.Fatalf("no side menu:\n%s", strings.Join(rows[:8], "\n"))
	}
	for _, r := range rows[1 : len(rows)-1] {
		if w := ansi.StringWidth(r); w > 160 {
			t.Fatalf("row %d wide: %q", w, r)
		}
		if !strings.Contains(r, "│") {
			t.Fatalf("a row without the menu's rule: %q", r)
		}
	}
	if w := m.textWidth(); w != wideMeasure {
		t.Errorf("text width %d beside the menu, want %d", w, wideMeasure)
	}
	// The page is not centred: it starts two columns after the rule.
	for _, r := range rows {
		if i := strings.Index(r, "│"); i >= 0 && strings.Contains(r, "The Reading Room") && !strings.Contains(r, "1  The Reading Room") {
			if rest := []rune(r[i+len("│"):]); len(rest) < 2 || string(rest[:2]) != "  " || rest[2] == ' ' {
				t.Errorf("page title not beside the menu: %q", r)
			}
		}
	}

	m = press(m, `\`)
	if strings.Contains(m.View().Content, "THE LIBRARY") || m.textWidth() != maxMeasure {
		t.Error(`\ did not hide the menu`)
	}
	db, _ := store.Default()
	if db.Get("ui:sidebar") != "off" {
		t.Error("the choice was not kept")
	}
	m = press(m, `\`)
	if !strings.Contains(m.View().Content, "THE LIBRARY") {
		t.Error(`\ did not bring it back`)
	}

	// Narrow windows have no room for it.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	if strings.Contains(next.(Model).View().Content, "THE LIBRARY") {
		t.Error("menu on a narrow window")
	}

	// A number opens its room.
	if mm := press(m, "2"); mm.loading != "w5f:feeds" {
		t.Errorf("2 opened %q", mm.loading)
	}
	if r := roomFor("w5f:serial/4/continue"); r == nil || r.label != "The Serial Hall" {
		t.Errorf("a serial's room: %+v", r)
	}
}

// The selected link has a mark beside it and the theme's own colours,
// not reverse video.
func TestFocusMarkAndColours(t *testing.T) {
	m := wide(New("", "test"))
	if m.cur.focus == 0 {
		m = press(m, "down")
	}
	marked := 0
	for _, r := range plainView(m) {
		if strings.Contains(r, "│» ") {
			marked++
		}
	}
	if marked == 0 {
		t.Errorf("no mark beside the selected link:\n%s", strings.Join(plainView(m)[:12], "\n"))
	}
	st := m.theme.Seg(m.cur.layout.Lines[0].Segs[0], true)
	if st.GetReverse() || st.GetBackground() != m.theme.FocusBG {
		t.Error("focus is left to reverse video")
	}
}

// g → theme lists the themes; g → theme day puts one on and keeps it.
func TestThemes(t *testing.T) {
	m := wide(New("", "test"))
	m = press(m, "g")
	for _, r := range "theme day" {
		m = press(m, string(r))
	}
	m = press(m, "enter")
	if m.theme.Name != "day" {
		t.Fatalf("theme %q", m.theme.Name)
	}
	db, _ := store.Default()
	if db.Get("ui:theme") != "day" {
		t.Error("theme not kept")
	}
	if New("", "test").theme.Name != "day" {
		t.Error("a new window did not take the kept theme")
	}
	if d := themesDoc(m.theme); len(d.Links) != len(theme.Themes) || !strings.Contains(fmt.Sprint(d.Blocks), "in use") {
		t.Errorf("themes page: %+v", d.Links)
	}
	next, _ := m.setTheme("amber")
	m = next.(Model)
	if _, cmd := m.setTheme("plaid"); cmd != nil {
		t.Error("an unknown theme")
	}
}

// The bottom bar shows the keys of the room the reader is in; on a narrow
// window it drops room hints before help.
func TestBottomBarFollowsTheRoom(t *testing.T) {
	m := wide(New("", "test"))
	bar := func(m Model) string { return ansi.Strip(m.bottomBar()) }
	if b := bar(m); !strings.Contains(b, "1…0 rooms") || !strings.Contains(b, "? help") || ansi.StringWidth(b) != 160 {
		t.Errorf("reading room: %q", b)
	}
	for target, want := range map[string]string{
		"w5f:feeds":                "g → sync fetch new",
		"w5f:solo":                 "g → roll 2d6+1",
		"w5f:discover/tarot/ar16":  "T another card",
		"w5f:serial/3/ch/2":        "F follow",
		"https://example.org/page": "/ search",
	} {
		mm := m
		mm.cur = newPage(target, textPage("x", "y"))
		mm.relayout()
		if b := bar(mm); !strings.Contains(b, want) {
			t.Errorf("%s: %q lacks %q", target, b, want)
		}
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if b := bar(next.(Model)); !strings.HasSuffix(strings.TrimSpace(b), "? help · q quit") || ansi.StringWidth(b) > 60 {
		t.Errorf("narrow: %q", b)
	}
}

// The desk can be cleared, one page or all at once (asking first); what is
// set aside comes back when it is opened again. Clearing the history is
// never a link a note or a saved page can follow.
func TestDeskCanBeCleared(t *testing.T) {
	db, _ := store.Default()
	a, b := "https://example.org/desk-a", "https://example.org/desk-b"
	for _, u := range []string{a, b} {
		db.Visit(u, "Desk "+u[len(u)-1:], "web", "")
		db.SavePos(u, 0.5)
	}
	text := func(target string) string { return fmt.Sprint(deskDoc(target).Blocks) }
	if s := text("w5f:desk"); !strings.Contains(s, "Desk a") || !strings.Contains(s, "Desk b") || !strings.Contains(s, "set aside") {
		t.Fatalf("desk: %s", s)
	}
	if s := fmt.Sprint(welcomeDoc("test").Blocks); !strings.Contains(s, "clear the desk…") {
		t.Error("the Reading Room has no way to the desk")
	}
	s := text("w5f:desk/aside?u=" + a)
	if !strings.Contains(s, "Set aside") || strings.Contains(s, "Desk a") || !strings.Contains(s, "Desk b") {
		t.Errorf("one set aside: %s", s)
	}
	d := deskDoc("w5f:desk/clear")
	if d.Title != "Clear the desk?" || !strings.Contains(fmt.Sprint(d.Blocks), "Yes, clear the desk") || !strings.Contains(text("w5f:desk"), "Desk b") {
		t.Errorf("asking cleared the desk: %s", fmt.Sprint(d.Blocks))
	}
	if s := text("w5f:desk/clear?sure=yes"); !strings.Contains(s, "The desk is clear") || strings.Contains(s, "Desk b") {
		t.Errorf("cleared: %s", s)
	}
	db.Visit(a, "Desk a", "web", "")
	if s := text("w5f:desk"); !strings.Contains(s, "Desk a") {
		t.Errorf("opened again, not back on the desk: %s", s)
	}
	if navigationW5F("w5f:history/clear?sure=yes") || navigationW5F("w5f:history/clear") || !navigationW5F("w5f:history") {
		t.Error("clearing the history must not be followed from a file page")
	}
}

func TestClearDeskStartsOnNo(t *testing.T) {
	db, _ := store.Default()
	db.Visit("https://example.org/desk-c", "Desk c", "web", "")
	db.SavePos("https://example.org/desk-c", 0.5)
	if d := deskDoc("w5f:desk/clear"); len(d.Links) != 2 || d.Links[0].Href != "w5f:desk" {
		t.Errorf("first link: %+v", d.Links)
	}
}

// A change on the Gaming Table redraws it in place: no new page to go back
// through, the selection where it was (+ pressed again and again).
func TestTableRedrawKeepsSelection(t *testing.T) {
	table := func(n int) *doc.Document {
		d := &doc.Document{Title: "The Gaming Table", URL: "w5f:solo"}
		var in doc.Inline
		for i := 1; i <= 6; i++ {
			d.Links = append(d.Links, doc.Link{Href: fmt.Sprintf("w5f:solo/counter/up?i=%d", i), Text: "+"})
			in = append(in, doc.Span{Text: fmt.Sprintf("+%d", n), Link: i}, doc.Span{Text: " "})
		}
		d.Blocks = []doc.Block{doc.Paragraph{Text: in}}
		return d
	}
	m := open(sized(New("", "test")), "w5f:solo", table(0))
	m.cur.focus = 4
	back := len(m.back)
	if !soloEdit("w5f:solo/counter/up?i=4&n=Doom") || soloEdit("w5f:solo/roll?d=d6") {
		t.Error("soloEdit")
	}
	next, _ := m.Update(loadedMsg{target: "w5f:solo/counter/up?i=4&n=Doom", doc: table(1), replace: true})
	m = next.(Model)
	if m.cur.focus != 4 || len(m.back) != back || m.cur.target != "w5f:solo" {
		t.Errorf("focus %d, back %d (was %d), target %q", m.cur.focus, len(m.back), back, m.cur.target)
	}
}

func TestBrowserCommand(t *testing.T) {
	for in, want := range map[string]string{"browser": "browser", "chromium": "chromium", "browser old.reddit.com/login": "browser",
		"chromium https://example.org": "chromium", "Browser example.org": "browser", "browser wars": "", "chromium os history": "", "browsers": ""} {
		if got, _ := browserCommand(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// The Weeding Room is in the side menu under the archive, opened with W.
func TestWeedingRoomInTheMenu(t *testing.T) {
	m := wide(New("", "test"))
	if s := strings.Join(plainView(m), "\n"); !strings.Contains(s, "W  The Weeding Room") {
		t.Errorf("no Weeding Room in the menu:\n%s", s)
	}
	if r := roomFor("w5f:weeding/notes"); r == nil || r.key != "W" {
		t.Errorf("roomFor: %+v", r)
	}
}
