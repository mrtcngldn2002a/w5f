package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
)

// pressCmd presses one key and returns the command it gave.
func pressCmd(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// linksPage is a page of n links, one per line, under the given headings
// (a heading every n/len(heads) links).
func linksPage(title string, n int, heads ...string) *doc.Document {
	d := &doc.Document{Title: title}
	per := n
	if len(heads) > 0 {
		per = n / len(heads)
	}
	for i := 0; i < n; i++ {
		if len(heads) > 0 && i%per == 0 && i/per < len(heads) {
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: heads[i/per]}}})
		}
		d.Links = append(d.Links, doc.Link{Href: fmt.Sprintf("https://example.org/%d", i)})
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("link %d", i), Link: i + 1}}})
	}
	return d
}

// In a room of two columns ← and → move between them and never open
// anything (→ used to open the selected line).
func TestArrowsMoveBetweenColumns(t *testing.T) {
	col := func(from int) []doc.Block {
		var bs []doc.Block
		for i := from; i < from+4; i++ {
			bs = append(bs, doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("item %d", i), Link: i}}})
		}
		return bs
	}
	d := &doc.Document{Title: "Two columns", Blocks: []doc.Block{doc.Columns{Cols: [][]doc.Block{col(1), col(5)}}}}
	for i := 1; i <= 8; i++ {
		d.Links = append(d.Links, doc.Link{Href: fmt.Sprintf("https://example.org/%d", i)})
	}
	m := open(wide(New("", "test")), "w5f:cols", d)
	m = press(m, "down", "down") // the third line of the left column
	left := *m.focused()
	if left.Group == 0 || left.Col != 0 {
		t.Fatalf("not in the left column: %+v", left)
	}
	m = press(m, "right")
	right := m.focused()
	if m.loading != "" || m.cur.target != "w5f:cols" {
		t.Fatalf("→ opened something: loading %q at %q", m.loading, m.cur.target)
	}
	if right == nil || right.Col != 1 || right.Group != left.Group || right.Line != left.Line {
		t.Fatalf("→ did not go to the same line of the right column: %+v (from %+v)", right, left)
	}
	m = press(m, "right")
	if !strings.Contains(m.status, "nothing to select to the right") || m.focused().Col != 1 {
		t.Errorf("→ past the last column: %q %+v", m.status, m.focused())
	}
	m = press(m, "left")
	if f := m.focused(); f.Col != 0 || f.Line != left.Line {
		t.Errorf("← did not come back: %+v", f)
	}
}

// On a page of one column ← and → (and shift+↑↓) go from heading to heading.
func TestArrowsJumpHeadingsOnOneColumn(t *testing.T) {
	m := open(sized(New("", "test")), "w5f:list", linksPage("List", 90, "First", "Second", "Third"))
	second := m.cur.layout.Headings[1].Line
	m = press(m, "right")
	if m.cur.offset != second {
		t.Fatalf("→ did not scroll to the second heading: offset %d, heading at %d", m.cur.offset, second)
	}
	if f := m.focused(); f == nil || f.Line <= second || !m.onScreen(m.cur.focus) {
		t.Errorf("the selection did not follow: %+v", f)
	}
	m = press(m, "shift+down")
	if third := m.cur.layout.Headings[2].Line; m.cur.offset != min(third, len(m.cur.layout.Lines)-m.bodyHeight()) {
		t.Errorf("shift+↓ did not go to the third heading: offset %d", m.cur.offset)
	}
	m = press(m, "shift+up")
	if m.cur.offset != m.cur.layout.Headings[1].Line {
		t.Errorf("back up to the second heading: offset %d", m.cur.offset)
	}
	m = press(m, "left")
	if m.cur.offset != m.cur.layout.Headings[0].Line {
		t.Errorf("back up to the first heading: offset %d", m.cur.offset)
	}
	if m.cur.target != "w5f:list" {
		t.Errorf("← left the page: at %q", m.cur.target)
	}
}

// space, pgdn and end bring the selection along; home and end select the
// first and the last line.
func TestPageKeysCarryTheSelection(t *testing.T) {
	m := open(sized(New("", "test")), "w5f:list", linksPage("List", 120))
	n := len(m.cur.layout.Focus)
	for _, k := range []string{"pgdown", "space"} {
		before := m.cur.focus
		m = press(m, k)
		if m.cur.focus <= before || !m.onScreen(m.cur.focus) {
			t.Errorf("%s: selection %d → %d, on screen %v", k, before, m.cur.focus, m.onScreen(m.cur.focus))
		}
	}
	m = press(m, "pgup")
	if !m.onScreen(m.cur.focus) {
		t.Error("pgup left the selection off screen")
	}
	m = press(m, "end")
	if m.cur.focus != n {
		t.Errorf("end: selection %d of %d", m.cur.focus, n)
	}
	m = press(m, "pgdown")
	if m.cur.focus != n || m.status != "end of page" {
		t.Errorf("pgdn at the end: %d %q", m.cur.focus, m.status)
	}
	m = press(m, "home")
	if m.cur.focus != 1 || m.cur.offset != 0 {
		t.Errorf("home: selection %d offset %d", m.cur.focus, m.cur.offset)
	}
}

// q closes the page and what was opened from it, back to its room; a room
// closes to the Reading Room, which stays.
func TestQClosesBackToTheRoom(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "w5f:feeds", textPage("Periodical Gallery", "shelves"))
	m = open(m, "https://example.org/a", textPage("A", "alpha"))
	m = open(m, "https://example.org/b", textPage("B", "beta"))
	m = press(m, "q")
	if m.cur.target != "w5f:feeds" {
		t.Fatalf("q closed to %q", m.cur.target)
	}
	m = press(m, "l")
	if m.cur.target != "https://example.org/a" {
		t.Errorf("l after q reopens what was closed, in order: at %q", m.cur.target)
	}
	m = press(m, "q")
	next, cmd := m.closePage() // the gallery itself: to the Reading Room
	m = next.(Model)
	if m.cur.target != "w5f:feeds" || cmd == nil {
		t.Fatalf("q on a room: at %q", m.cur.target)
	}
	if msg := cmd(); msg != nil {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if m.cur.target != "w5f:welcome" {
		t.Fatalf("a room closes to the Reading Room: at %q", m.cur.target)
	}
	m = press(m, "q")
	if m.cur.target != "w5f:welcome" || !strings.Contains(m.status, "esc quits") {
		t.Errorf("q in the Reading Room: %q %q", m.cur.target, m.status)
	}
}

// esc closes the innermost layer; with nothing open it asks, and only a
// second esc quits. ctrl+q quits at once.
func TestEscAsksBeforeQuitting(t *testing.T) {
	m := sized(New("", "test"))
	m = press(m, "g")
	m, cmd := pressCmd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeRead || quits(cmd) {
		t.Fatalf("esc in the go-to prompt closes the prompt only: mode %v", m.mode)
	}
	m = press(m, "esc")
	if m.mode != modeQuit || !strings.Contains(m.bottomBar(), "Quit W5F?") {
		t.Fatalf("esc with nothing open asks first: mode %v, bar %q", m.mode, m.bottomBar())
	}
	m, cmd = pressCmd(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.mode != modeRead || quits(cmd) {
		t.Fatalf("any other key stays: mode %v", m.mode)
	}
	m = press(m, "esc")
	if _, cmd = pressCmd(m, tea.KeyPressMsg{Code: tea.KeyEscape}); !quits(cmd) {
		t.Error("a second esc quits")
	}
	if _, cmd = pressCmd(sized(New("", "test")), tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}); !quits(cmd) {
		t.Error("ctrl+q quits at once")
	}
	if _, cmd = pressCmd(sized(New("", "test")), tea.KeyPressMsg{Code: 'q', Text: "q"}); quits(cmd) {
		t.Error("q no longer quits")
	}
}

// backspace and alt+← go back; → and ← never do.
func TestBackspaceGoesBack(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://example.org/a", textPage("A", "alpha"))
	m = press(m, "left", "right")
	if m.cur.target != "https://example.org/a" {
		t.Fatalf("an arrow left the page: at %q", m.cur.target)
	}
	m = press(m, "alt+left")
	if m.cur.target != "w5f:welcome" {
		t.Errorf("alt+← did not go back: at %q", m.cur.target)
	}
}
