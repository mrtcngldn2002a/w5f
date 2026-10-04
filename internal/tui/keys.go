package tui

import (
	"sort"

	tea "charm.land/bubbletea/v2"
)

// The keys (chosen with the owner, 2026-10-04): the arrows only move, enter
// alone opens, backspace goes back, q closes what is open and esc closes the
// innermost layer, asking before it quits. ← and → used to open and go back,
// and in a room of two columns → opened the selected line when the eye was
// already on the column to its right.

// columnMove moves the selection to the next column (dir 1) or the one
// before it (dir -1), to the line nearest the selected one. It reports
// false when the selection is not in columns.
func (m *Model) columnMove(dir int) bool {
	f := m.focused()
	if f == nil || f.Group == 0 || !m.onScreen(m.cur.focus) {
		return false
	}
	best, bestDist := 0, 0
	for i, c := range m.cur.layout.Focus {
		if c.Group != f.Group || c.Col != f.Col+dir {
			continue
		}
		d := c.Line - f.Line
		if d < 0 {
			d = -d
		}
		if best == 0 || d < bestDist {
			best, bestDist = i+1, d
		}
	}
	if best == 0 {
		if dir > 0 {
			m.status = "nothing to select to the right"
		} else {
			m.status = "nothing to select to the left"
		}
		return true
	}
	m.cur.focus = best
	m.reveal(m.cur.layout.Focus[best-1].Line)
	return true
}

// headingMove scrolls to the next heading below the selection (dir 1) or
// the one above it (dir -1) and selects the first line after it.
func (m *Model) headingMove(dir int) {
	p := m.cur
	if len(p.layout.Headings) == 0 {
		m.status = "no headings on this page"
		return
	}
	lines := make([]int, 0, len(p.layout.Headings))
	for _, h := range p.layout.Headings {
		lines = append(lines, h.Line)
	}
	sort.Ints(lines)
	// Below the selection; above the screen's top, so a heading already
	// shown is not "the one above".
	ref := p.offset
	if p.focus > 0 && m.onScreen(p.focus) && (dir > 0 || p.layout.Focus[p.focus-1].Line < ref) {
		ref = p.layout.Focus[p.focus-1].Line
	}
	at := -1
	if dir > 0 {
		for _, l := range lines {
			if l > ref {
				at = l
				break
			}
		}
	} else {
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i] < ref {
				at = lines[i]
				break
			}
		}
	}
	if at < 0 {
		if dir > 0 {
			m.status = "no heading below"
		} else {
			m.status = "no heading above"
		}
		return
	}
	p.offset = at
	m.clamp()
	p.focus = 0
	for i, f := range p.layout.Focus {
		if f.Line >= at && m.onScreen(i+1) {
			p.focus = i + 1
			break
		}
	}
	if p.focus == 0 {
		p.focus = m.visibleFocus(1)
	}
}

// pageMove scrolls a screen down (dir 1) or up (dir -1) and brings the
// selection along: a selection left off screen goes to the first (or the
// last) line on the new one.
func (m *Model) pageMove(dir int) {
	p := m.cur
	before := p.offset
	p.offset += dir * (m.bodyHeight() - 2)
	m.clamp()
	if p.offset == before {
		// Already at the end: the last (or first) line is the one to select.
		p.focus = m.visibleFocus(-dir)
		if dir > 0 {
			m.status = "end of page"
		} else {
			m.status = "top of page"
		}
		return
	}
	if p.focus == 0 || !m.onScreen(p.focus) {
		p.focus = m.visibleFocus(dir)
	}
}

// jumpEnd goes to the top of the page and its first line (dir -1) or to the
// bottom and its last line (dir 1).
func (m *Model) jumpEnd(dir int) {
	p := m.cur
	if dir < 0 {
		p.offset = 0
	} else {
		p.offset = len(p.layout.Lines)
	}
	m.clamp()
	p.focus = m.visibleFocus(-dir)
}

// isRoomPage reports a room's own page (not a page inside it).
func isRoomPage(target string) bool {
	for _, r := range rooms {
		if target == r.target {
			return true
		}
	}
	return false
}

// closePage is q: it closes the page and what was opened from it, back to
// the room it was reached from. A room closes to the Reading Room.
func (m Model) closePage() (tea.Model, tea.Cmd) {
	p := m.cur
	if isRoomPage(p.target) {
		if p.target == "w5f:welcome" {
			m.status = "this is the Reading Room · esc quits"
			return m, nil
		}
		return m.openRoom("1")
	}
	for i := len(m.back) - 1; i >= 0; i-- {
		if !isRoomPage(m.back[i].target) {
			continue
		}
		m.leavePage()
		// What is closed can still be reopened with l, as after backspace.
		m.forward = append(m.forward, m.cur)
		for j := len(m.back) - 1; j > i; j-- {
			m.forward = append(m.forward, m.back[j])
		}
		m.cur = m.back[i]
		m.back = m.back[:i]
		m.relayout()
		return m, m.refreshLocal()
	}
	// Opened from the shell or from no room: its own room, or the Reading Room.
	if r := roomFor(p.target); r != nil {
		return m.openRoom(r.key)
	}
	return m.openRoom("1")
}

// quitKey answers "Quit W5F?": esc again quits, any other key stays.
func (m Model) quitKey(s string) (tea.Model, tea.Cmd) {
	m.mode = modeRead
	if s == "esc" {
		m.leavePage()
		cancelLoad()
		return m, tea.Quit
	}
	return m, nil
}
