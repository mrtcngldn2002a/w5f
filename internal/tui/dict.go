package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"w5f/internal/dict"
	"w5f/internal/doc"
	"w5f/internal/htmlconv"
	"w5f/internal/render"
	"w5f/internal/source"
	"w5f/internal/store"
)

// dictState is the pop-up dictionary (key d). It floats over whatever is
// being read; esc returns to the page exactly where it was.
type dictState struct {
	query  string
	sugg   []string
	sel    int
	view   *render.Layout // the definition, when one is shown
	offset int
	msg    string
}

type dictInstalledMsg struct {
	title string
	err   error
}

// dictDir is where the dictionary lives (a variable so tests can redirect it).
var dictDir = func() string { return dict.Dir(store.DataDir()) }

func installDict(src string) tea.Cmd {
	return func() tea.Msg {
		title, err := dict.Install(context.Background(), src, dictDir(), source.Fetcher.UserAgent)
		return dictInstalledMsg{title: title, err: err}
	}
}

func (m *Model) openDict() {
	m.mode = modeDict
	m.dict = dictState{}
	if _, err := dict.Shared(dictDir()); err != nil {
		m.dict.msg = "No dictionary installed yet. Press esc, then g → dict-install (downloads the English–Turkish dictionary, 16 MB)."
	}
}

func (m Model) dictKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ds := &m.dict
	switch k.String() {
	case "esc":
		m.mode = modeRead
		return m, nil
	case "enter":
		word := strings.TrimSpace(ds.query)
		if ds.view == nil && len(ds.sugg) > 0 && ds.sel < len(ds.sugg) {
			word = ds.sugg[ds.sel]
		}
		m.showDefinition(word)
		return m, nil
	case "up":
		if ds.view != nil {
			ds.offset--
		} else if ds.sel > 0 {
			ds.sel--
		}
	case "down":
		if ds.view != nil {
			ds.offset++
		} else if ds.sel < len(ds.sugg)-1 {
			ds.sel++
		}
	case "pgdown", "space":
		if ds.view != nil {
			ds.offset += m.dictBodyHeight() - 1
		} else if k.String() == "space" {
			ds.query += " "
			m.refreshSuggestions()
		}
	case "pgup":
		ds.offset -= m.dictBodyHeight() - 1
	case "backspace":
		if r := []rune(ds.query); len(r) > 0 {
			ds.query = string(r[:len(r)-1])
		}
		m.refreshSuggestions()
	case "ctrl+u":
		ds.query = ""
		m.refreshSuggestions()
	default:
		if k.Text != "" {
			ds.query += k.Text
			m.refreshSuggestions()
		}
	}
	m.clampDict()
	return m, nil
}

func (m *Model) refreshSuggestions() {
	ds := &m.dict
	ds.view, ds.offset, ds.sel = nil, 0, 0
	d, err := dict.Shared(dictDir())
	if err != nil {
		return
	}
	ds.sugg = d.Suggest(ds.query, m.dictBodyHeight())
	ds.msg = ""
	if len(ds.sugg) == 0 && strings.TrimSpace(ds.query) != "" {
		ds.msg = "no matching words"
	}
}

func (m *Model) showDefinition(word string) {
	ds := &m.dict
	d, err := dict.Shared(dictDir())
	if err != nil || word == "" {
		return
	}
	entries := d.Lookup(word)
	if len(entries) == 0 {
		ds.msg = "“" + word + "” is not in the dictionary"
		return
	}
	ds.query = word
	page := &doc.Document{Lang: "tr"}
	for i, e := range entries {
		if i > 0 {
			page.Blocks = append(page.Blocks, doc.Rule{})
		}
		page.Blocks = append(page.Blocks, doc.Heading{Level: 3, Text: doc.Inline{{Text: e.Word}}})
		page.Blocks = append(page.Blocks, htmlconv.Fragment(page, e.HTML, "")...)
	}
	ds.view = render.Render(page, render.Options{Width: m.dictWidth() - 4})
	ds.offset, ds.msg = 0, ""
}

func (m *Model) clampDict() {
	ds := &m.dict
	if ds.view == nil {
		return
	}
	max := len(ds.view.Lines) - m.dictBodyHeight()
	if ds.offset > max {
		ds.offset = max
	}
	if ds.offset < 0 {
		ds.offset = 0
	}
}

func (m Model) dictWidth() int {
	w := m.width - 6
	if w > 78 {
		w = 78
	}
	if w < 30 {
		w = 30
	}
	return w
}

// dictBodyHeight is the number of content rows inside the box.
func (m Model) dictBodyHeight() int {
	h := m.bodyHeight() - 6
	if h > 20 {
		h = 20
	}
	if h < 3 {
		h = 3
	}
	return h
}

// overlayDict draws the dictionary box over the page lines.
func (m Model) overlayDict(page []string) []string {
	w := m.dictWidth()
	inner := w - 4
	chrome := m.theme.Chrome
	box := lipgloss.NewStyle().Background(chrome.BG).Foreground(chrome.FG)
	dim := lipgloss.NewStyle().Background(chrome.BG).Foreground(chrome.Dim)
	sel := lipgloss.NewStyle().Background(chrome.FG).Foreground(chrome.BG).Bold(true)

	pad := func(s string, width int) string {
		s = ansi.Truncate(s, width, "…")
		if d := width - ansi.StringWidth(s); d > 0 {
			s += strings.Repeat(" ", d)
		}
		return s
	}
	row := func(content string) string { // content already styled, inner width
		return box.Render("│ ") + content + box.Render(" │")
	}
	title := " DICTIONARY · EN → TR "
	top := box.Render("┌─" + title + strings.Repeat("─", max(0, w-3-ansi.StringWidth(title))) + "┐")
	var lines []string
	lines = append(lines, top)
	lines = append(lines, row(box.Bold(true).Render(pad("› "+m.dict.query+"_", inner))))
	lines = append(lines, box.Render("├"+strings.Repeat("─", w-2)+"┤"))

	body := m.dictBodyHeight()
	var content []string
	switch {
	case m.dict.view != nil:
		end := min(m.dict.offset+body, len(m.dict.view.Lines))
		for _, ln := range m.dict.view.Lines[m.dict.offset:end] {
			var sb strings.Builder
			for _, sg := range ln.Segs {
				st := m.theme.Seg(sg, false).Background(chrome.BG)
				if sg.Role == render.Body {
					st = st.Foreground(chrome.FG)
				}
				sb.WriteString(st.Render(sg.Text))
			}
			content = append(content, pad(sb.String(), inner)+"")
		}
	case m.dict.msg != "":
		for _, l := range wrapPlain(m.dict.msg, inner) {
			content = append(content, dim.Render(pad(l, inner)))
		}
	case len(m.dict.sugg) > 0:
		for i, s := range m.dict.sugg {
			if i == m.dict.sel {
				content = append(content, sel.Render(pad(" "+s, inner)))
			} else {
				content = append(content, box.Render(pad(" "+s, inner)))
			}
		}
	default:
		content = append(content, dim.Render(pad("Type an English word. ↑↓ choose · enter shows the meaning.", inner)))
	}
	for i := 0; i < body; i++ {
		if i < len(content) {
			// Re-apply the box background to any unstyled padding.
			lines = append(lines, row(box.Render("")+content[i]))
		} else {
			lines = append(lines, row(box.Render(pad("", inner))))
		}
	}
	hint := " enter meaning · ↑↓ " + map[bool]string{true: "scroll", false: "choose"}[m.dict.view != nil] + " · esc close "
	if m.dict.view != nil && len(m.dict.view.Lines) > body {
		hint = fmt.Sprintf(" %d/%d%s", min(m.dict.offset+body, len(m.dict.view.Lines)), len(m.dict.view.Lines), hint)
	}
	lines = append(lines, box.Render("└"+strings.Repeat("─", max(0, w-2-ansi.StringWidth(hint)))+hint+"┘"))

	out := append([]string{}, page...)
	for len(out) < m.bodyHeight() {
		out = append(out, "")
	}
	x := strings.Repeat(" ", max(0, (m.width-w)/2))
	for i, l := range lines {
		r := 1 + i
		if r >= len(out) {
			break
		}
		out[r] = x + l
	}
	return out
}

func wrapPlain(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) > w {
			out = append(out, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
