package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/personal"
)

// clipState is the paragraph selection of the clipping mode (key y).
type clipState struct{ anchor, cur int }

func (cs clipState) span() (int, int) {
	lo, hi := cs.anchor, cs.cur
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

func (m *Model) startClip() {
	paras := m.cur.layout.Paras
	if len(paras) == 0 {
		m.status = "nothing to clip on this page"
		return
	}
	i := 0
	for j, pr := range paras {
		if pr.End > m.cur.offset {
			i = j
			break
		}
	}
	m.clip = clipState{anchor: i, cur: i}
	m.mode = modeClip
}

func (m Model) clipKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	paras := m.cur.layout.Paras
	cs := &m.clip
	switch k.String() {
	case "esc", "q":
		m.mode, m.status = modeRead, "clipping cancelled"
		return m, nil
	case "up", "k":
		if cs.cur > 0 {
			cs.cur--
		}
		cs.anchor = cs.cur
	case "down", "j":
		if cs.cur < len(paras)-1 {
			cs.cur++
		}
		cs.anchor = cs.cur
	case "shift+up", "K":
		if cs.cur > 0 {
			cs.cur--
		}
	case "shift+down", "J":
		if cs.cur < len(paras)-1 {
			cs.cur++
		}
	case "enter":
		lo, hi := cs.span()
		var texts []string
		for _, pr := range paras[lo : hi+1] {
			texts = append(texts, pr.Text)
		}
		m.mode = modeRead
		path, err := personal.AppendClipping(pageSource(m.cur), texts, time.Now())
		if err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		plural := "s"
		if len(texts) == 1 {
			plural = ""
		}
		m.status = fmt.Sprintf("clipping saved (%d paragraph%s) to %s", len(texts), plural, personal.Rel(path))
		return m, indexFile(path)
	}
	pr := paras[cs.cur]
	m.reveal(pr.End - 1)
	m.reveal(pr.Start)
	return m, nil
}
