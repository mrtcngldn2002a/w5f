package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/render"
)

// Form is a small input screen a page opens with a link: a site's session
// pasted from the reader's browser, for example. Hidden fields show dots.
type Form struct {
	Title  string
	Intro  []string // lines above the fields
	Fields []FormField
	Note   string // the last line (where the answer is kept)
	// Save receives the answers in order; it returns the page to open next
	// ("" stays on the current one) and a status line.
	Save func(values []string) (next, status string, err error)
}

// FormField is one answer.
type FormField struct {
	Label  string
	Hint   string
	Hidden bool
}

var forms []formRoute

type formRoute struct {
	prefix string
	open   func(href string) (*Form, error)
}

// RegisterForm makes links starting with prefix open a form instead of a
// page. Call it before the reader starts.
func RegisterForm(prefix string, open func(href string) (*Form, error)) {
	forms = append(forms, formRoute{prefix, open})
}

func formFor(href string) (func(string) (*Form, error), bool) {
	for _, f := range forms {
		if strings.HasPrefix(href, f.prefix) {
			return f.open, true
		}
	}
	return nil, false
}

// formState is the open form and the answers so far.
type formState struct {
	f      *Form
	step   int
	values []string
}

func (m Model) openForm(open func(string) (*Form, error), href string) (tea.Model, tea.Cmd) {
	f, err := open(href)
	if err != nil {
		m.status = "error: " + err.Error()
		return m, nil
	}
	m.mode, m.secretBuf, m.form = modeSecret, "", &formState{f: f}
	return m, nil
}

func (m Model) formKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	fs := m.form
	switch k.String() {
	case "esc":
		m.mode, m.secretBuf, m.form = modeRead, "", nil
		m.status = fs.f.Title + ": cancelled."
	case "enter":
		fs.values = append(fs.values, strings.TrimSpace(m.secretBuf))
		m.secretBuf = ""
		if fs.step++; fs.step < len(fs.f.Fields) {
			return m, nil
		}
		m.mode, m.form = modeRead, nil
		next, status, err := fs.f.Save(fs.values)
		if err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		m.status = status
		if next != "" {
			m.loading = next
			return m, load(next, false)
		}
	case "backspace":
		if r := []rune(m.secretBuf); len(r) > 0 {
			m.secretBuf = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.secretBuf = ""
	default:
		if k.Text != "" {
			m.secretBuf += k.Text
		}
	}
	return m, nil
}

func (m Model) formLines() []string {
	fs := m.form
	margin := m.margin()
	st := func(r render.Role) func(string) string {
		return func(s string) string { return m.theme.Seg(render.Seg{Role: r}, false).Render(s) }
	}
	title, body, dim := st(render.Title), st(render.Body), st(render.Dim)
	out := []string{margin + title(strings.ToUpper(fs.f.Title)), ""}
	for _, l := range fs.f.Intro {
		out = append(out, margin+body(l))
	}
	out = append(out, "")
	w := m.textWidth() - 4
	for i, fld := range fs.f.Fields {
		label := fmt.Sprintf("%d/%d  %s", i+1, len(fs.f.Fields), fld.Label)
		switch {
		case i < fs.step:
			v := fs.values[i]
			if fld.Hidden {
				v = fmt.Sprintf("(%d characters)", len([]rune(v)))
			}
			out = append(out, margin+dim(label+": "+clipLeft(v, w-len(label))))
		case i == fs.step:
			v := m.secretBuf
			if fld.Hidden {
				v = strings.Repeat("•", len([]rune(v)))
			}
			out = append(out, margin+body(label), margin+m.theme.Seg(render.Seg{Role: render.Fold}, false).Render("› ")+body(clipLeft(v, w))+body("_"))
			if fld.Hint != "" {
				out = append(out, margin+dim(fld.Hint))
			}
		}
	}
	out = append(out, "", margin+dim(fmt.Sprintf("%d characters · enter next · esc cancels · ctrl+u clears", len([]rune(m.secretBuf)))))
	if fs.f.Note != "" {
		out = append(out, margin+dim(fs.f.Note))
	}
	return out
}

// clipLeft keeps the end of a long line (what is being typed).
func clipLeft(s string, w int) string {
	r := []rune(s)
	if w < 2 || len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-(w-1):])
}
