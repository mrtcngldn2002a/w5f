package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
