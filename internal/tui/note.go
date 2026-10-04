package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"w5f/internal/personal"
)

// noteState is the pop-up note box (key n).
type noteState struct {
	src     personal.Source
	lines   [][]rune
	row     int
	col     int
	confirm bool // esc pressed once with text in the box
	msg     string
}

func (m *Model) openNote() {
	m.mode = modeNote
	m.note = noteState{src: pageSource(m.cur), lines: [][]rune{{}}}
}

func (ns *noteState) text() string {
	parts := make([]string, len(ns.lines))
	for i, l := range ns.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (ns *noteState) empty() bool { return strings.TrimSpace(ns.text()) == "" }

func (ns *noteState) insert(s string) {
	// Linux terminals paste lines separated by a lone carriage return.
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	for i, part := range strings.Split(s, "\n") {
		if i > 0 {
			ns.newline()
		}
		rs := []rune(part)
		ln := ns.lines[ns.row]
		nl := make([]rune, 0, len(ln)+len(rs))
		nl = append(nl, ln[:ns.col]...)
		nl = append(nl, rs...)
		nl = append(nl, ln[ns.col:]...)
		ns.lines[ns.row] = nl
		ns.col += len(rs)
	}
}

func (ns *noteState) newline() {
	ln := ns.lines[ns.row]
	head := append([]rune{}, ln[:ns.col]...)
	tail := append([]rune{}, ln[ns.col:]...)
	ns.lines[ns.row] = head
	ns.lines = append(ns.lines[:ns.row+1], append([][]rune{tail}, ns.lines[ns.row+1:]...)...)
	ns.row, ns.col = ns.row+1, 0
}

func (ns *noteState) backspace() {
	if ns.col > 0 {
		ln := ns.lines[ns.row]
		ns.lines[ns.row] = append(append([]rune{}, ln[:ns.col-1]...), ln[ns.col:]...)
		ns.col--
		return
	}
	if ns.row == 0 {
		return
	}
	prev := ns.lines[ns.row-1]
	ns.col = len(prev)
	ns.lines[ns.row-1] = append(append([]rune{}, prev...), ns.lines[ns.row]...)
	ns.lines = append(ns.lines[:ns.row], ns.lines[ns.row+1:]...)
	ns.row--
}

func (ns *noteState) del() {
	ln := ns.lines[ns.row]
	if ns.col < len(ln) {
		ns.lines[ns.row] = append(append([]rune{}, ln[:ns.col]...), ln[ns.col+1:]...)
		return
	}
	if ns.row+1 < len(ns.lines) {
		ns.lines[ns.row] = append(append([]rune{}, ln...), ns.lines[ns.row+1]...)
		ns.lines = append(ns.lines[:ns.row+1], ns.lines[ns.row+2:]...)
	}
}

func (m Model) noteKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ns := &m.note
	s := k.String()
	if s != "esc" {
		ns.confirm, ns.msg = false, ""
	}
	switch s {
	case "esc":
		if ns.empty() || ns.confirm {
			m.mode = modeRead
			return m, nil
		}
		ns.confirm, ns.msg = true, "press esc again to discard this note"
	case "ctrl+s":
		if ns.empty() {
			ns.msg = "the note is empty"
			return m, nil
		}
		path, err := personal.AppendNote(ns.src, ns.text(), time.Now())
		if err != nil {
			ns.msg = "error: " + err.Error() // the text stays in the box
			return m, nil
		}
		m.mode = modeRead
		m.status = "note saved to " + personal.Rel(path)
		return m, indexFile(path)
	case "enter":
		ns.newline()
	case "backspace":
		ns.backspace()
	case "delete":
		ns.del()
	case "left":
		if ns.col > 0 {
			ns.col--
		} else if ns.row > 0 {
			ns.row--
			ns.col = len(ns.lines[ns.row])
		}
	case "right":
		if ns.col < len(ns.lines[ns.row]) {
			ns.col++
		} else if ns.row < len(ns.lines)-1 {
			ns.row, ns.col = ns.row+1, 0
		}
	case "up":
		if ns.row > 0 {
			ns.row--
			ns.col = min(ns.col, len(ns.lines[ns.row]))
		}
	case "down":
		if ns.row < len(ns.lines)-1 {
			ns.row++
			ns.col = min(ns.col, len(ns.lines[ns.row]))
		}
	case "home":
		ns.col = 0
	case "end":
		ns.col = len(ns.lines[ns.row])
	default:
		if k.Text != "" {
			ns.insert(k.Text)
		}
	}
	return m, nil
}

// noteView lays the text out in rows of at most width runes with a block
// cursor, and returns the row that holds the cursor.
func noteView(ns noteState, width int) ([]string, int) {
	var rows []string
	cur := 0
	for i, ln := range ns.lines {
		r := append([]rune{}, ln...)
		if i == ns.row {
			r = append(r[:ns.col], append([]rune{'█'}, r[ns.col:]...)...)
			cur = len(rows) + ns.col/width
		}
		for {
			if len(r) <= width {
				rows = append(rows, string(r))
				break
			}
			rows = append(rows, string(r[:width]))
			r = r[width:]
		}
	}
	return rows, cur
}

// overlayNote draws the note box over the page lines.
func (m Model) overlayNote(page []string) []string {
	w := m.dictWidth()
	inner := w - 4
	chrome := m.theme.Chrome
	box := lipgloss.NewStyle().Background(chrome.BG).Foreground(chrome.FG)
	dim := lipgloss.NewStyle().Background(chrome.BG).Foreground(chrome.Dim)
	pad := func(s string) string {
		s = ansi.Truncate(s, inner, "…")
		if d := inner - ansi.StringWidth(s); d > 0 {
			s += strings.Repeat(" ", d)
		}
		return s
	}
	row := func(content string) string { return box.Render("│ ") + content + box.Render(" │") }
	title := " NOTE "
	if m.note.src.Catalog != "" {
		title += "· " + m.note.src.Catalog + " "
	}
	lines := []string{box.Render("┌─" + title + strings.Repeat("─", max(0, w-3-ansi.StringWidth(title))) + "┐")}
	lines = append(lines, row(dim.Render(pad(m.note.src.Title))))
	lines = append(lines, box.Render("├"+strings.Repeat("─", w-2)+"┤"))
	text, cur := noteView(m.note, inner)
	body := m.dictBodyHeight()
	start := 0
	if cur >= body {
		start = cur - body + 1
	}
	for i := 0; i < body; i++ {
		if j := start + i; j < len(text) {
			lines = append(lines, row(box.Render(pad(text[j]))))
		} else {
			lines = append(lines, row(box.Render(pad(""))))
		}
	}
	hint := " ctrl+s save · esc discard "
	if m.note.msg != "" {
		hint = " " + m.note.msg + " "
	}
	lines = append(lines, box.Render("└"+strings.Repeat("─", max(0, w-2-ansi.StringWidth(hint)))+hint+"┘"))
	out := append([]string{}, page...)
	for len(out) < m.bodyHeight() {
		out = append(out, "")
	}
	x := max(0, (m.pageWidth()-w)/2)
	for i, l := range lines {
		r := 1 + i
		if r >= len(out) {
			break
		}
		out[r] = overlayLine(out[r], x, l)
	}
	return out
}
