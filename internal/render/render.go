// Package render lays a document out into terminal lines for a given width.
// It is pure (no terminal I/O): the TUI styles the resulting segments.
package render

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"

	"w5f/internal/doc"
)

// Role is the semantic role of a segment; the theme maps roles to colors.
type Role uint8

const (
	Body Role = iota
	Dim
	Title
	H1
	H2
	H3
	LinkRole
	Fold // collapsible marker and label
	RuleRole
	CodeRole
	QuoteBar
	NoticeRole
	Meta
	ImageRole
)

// Seg is a styled run of text on one line.
type Seg struct {
	Text  string
	Role  Role
	Style doc.Style
	Link  int // 1-based link index, 0 = none
	Focus int // 1-based focusable index, 0 = none
}

// Line is one rendered terminal line (without the left margin).
type Line struct{ Segs []Seg }

// Text returns the plain text of the line.
func (l Line) Text() string {
	var b strings.Builder
	for _, s := range l.Segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

// FocusKind distinguishes what a focusable element does when activated.
type FocusKind uint8

const (
	FocusLink FocusKind = iota
	FocusFold
)

// Focusable is an element the cursor can land on (Tab) and activate (Enter).
type Focusable struct {
	Kind FocusKind
	Link int // for FocusLink
	Fold int // collapsible ID for FocusFold
	Line int // first line where it appears
	// Group and Col place it in side-by-side columns: Group is the first
	// line of the columns block plus one (0 = not in columns), Col the
	// column, left first. ← and → move between columns by them.
	Group int
	Col   int
}

// HeadingRef is a table-of-contents entry.
type HeadingRef struct {
	Level int
	Text  string
	Line  int
}

// Para is a text block's place on screen, for clippings: lines
// [Start, End) and its plain text.
type Para struct {
	Start, End int
	Text       string
}

// Layout is the rendered form of a document at one width and fold state.
type Layout struct {
	Width    int
	Lines    []Line
	Focus    []Focusable
	Headings []HeadingRef
	Paras    []Para
}

// Options control layout.
type Options struct {
	Width int          // text column width in cells
	Open  map[int]bool // collapsible ID -> open; missing = document default
}

type renderer struct {
	d   *doc.Document
	o   Options
	out *Layout
	// linkFocus maps a link index to its focusable index for the current
	// occurrence run, so one wrapped link shares one focus index.
	lastLinkFocus map[int]int
}

// Render lays out d.
func Render(d *doc.Document, o Options) *Layout {
	if o.Width < 20 {
		o.Width = 20
	}
	r := &renderer{d: d, o: o, out: &Layout{Width: o.Width}, lastLinkFocus: map[int]int{}}
	r.header()
	r.blocks(d.Blocks, ctx{width: o.Width})
	// Trim trailing blank lines.
	for len(r.out.Lines) > 0 && len(r.out.Lines[len(r.out.Lines)-1].Segs) == 0 {
		r.out.Lines = r.out.Lines[:len(r.out.Lines)-1]
	}
	return r.out
}

// ctx is the indentation context for nested blocks.
type ctx struct {
	first []Seg // prefix for the first line of the next block
	rest  []Seg // prefix for following lines
	width int   // total width including prefixes
}

func (c ctx) nest(first, rest Seg) ctx {
	return ctx{
		first: append(append([]Seg{}, c.rest...), first),
		rest:  append(append([]Seg{}, c.rest...), rest),
		width: c.width,
	}
}

func (c ctx) nestFirst(first, rest Seg) ctx {
	return ctx{
		first: append(append([]Seg{}, c.first...), first),
		rest:  append(append([]Seg{}, c.rest...), rest),
		width: c.width,
	}
}

func segsWidth(s []Seg) int {
	w := 0
	for _, x := range s {
		w += runewidth.StringWidth(x.Text)
	}
	return w
}

func (r *renderer) emit(segs []Seg) {
	r.out.Lines = append(r.out.Lines, Line{Segs: merge(segs)})
}

func (r *renderer) blank(c ctx) {
	n := len(r.out.Lines)
	if n == 0 {
		return
	}
	// Avoid double blanks; inside nested contexts keep the bar prefix.
	if strings.TrimSpace(r.out.Lines[n-1].Text()) == strings.TrimSpace(Line{Segs: c.rest}.Text()) {
		return
	}
	r.emit(trimTrailingSpace(c.rest))
}

func (r *renderer) header() {
	d := r.d
	if d.Title != "" {
		r.wrap(doc.Inline{{Text: d.Title}}, ctx{width: r.o.Width}, Title)
	}
	var meta []string
	for _, kv := range d.Meta {
		if kv.Key == "" || kv.Key == "·" {
			meta = append(meta, kv.Value)
			continue
		}
		meta = append(meta, kv.Key+" "+kv.Value)
	}
	if len(meta) > 0 {
		r.wrap(doc.Inline{{Text: strings.Join(meta, " · ")}}, ctx{width: r.o.Width}, Meta)
	}
	if d.Title != "" || len(meta) > 0 {
		r.emit([]Seg{{Text: strings.Repeat("━", r.o.Width), Role: RuleRole}})
		r.emit(nil)
	}
}

func (r *renderer) blocks(bs []doc.Block, c ctx) {
	for i, b := range bs {
		if i > 0 {
			r.blank(c)
			// Only the first block gets the lead prefix (list marker etc.).
			c.first = c.rest
		}
		r.block(b, c)
	}
}

func (r *renderer) block(b doc.Block, c ctx) {
	start := len(r.out.Lines)
	defer func() {
		if text := paraText(b); text != "" && len(r.out.Lines) > start {
			r.out.Paras = append(r.out.Paras, Para{Start: start, End: len(r.out.Lines), Text: text})
		}
	}()
	switch b := b.(type) {
	case doc.Paragraph:
		r.wrap(b.Text, c, Body)
	case doc.Heading:
		role := H3
		switch b.Level {
		case 1:
			role = H1
		case 2:
			role = H2
		}
		line := len(r.out.Lines)
		text := strings.TrimSpace(b.Text.PlainText())
		r.out.Headings = append(r.out.Headings, HeadingRef{Level: b.Level, Text: text, Line: line})
		in := b.Text
		if b.Level <= 2 {
			in = upper(in, r.d.Lang)
		}
		r.wrap(in, c, role)
		if b.Level == 1 {
			w := runewidth.StringWidth(text)
			if max := c.width - segsWidth(c.rest); w > max {
				w = max
			}
			r.emit(append(append([]Seg{}, c.rest...), Seg{Text: strings.Repeat("─", w), Role: RuleRole}))
		}
	case doc.Quote:
		bar := Seg{Text: "│ ", Role: QuoteBar}
		r.blocks(b.Blocks, c.nestFirst(bar, bar))
	case doc.List:
		for i, item := range b.Items {
			marker := "• "
			if b.Ordered {
				marker = strconv.Itoa(i+1) + ". "
			}
			pad := strings.Repeat(" ", runewidth.StringWidth(marker))
			var ic ctx
			if i == 0 {
				ic = c.nestFirst(Seg{Text: marker, Role: Dim}, Seg{Text: pad})
			} else {
				ic = c.nest(Seg{Text: marker, Role: Dim}, Seg{Text: pad})
			}
			r.blocks(item, ic)
		}
	case doc.Collapsible:
		r.collapsible(b, c)
	case doc.Table:
		r.table(b, c)
	case doc.Rule:
		w := c.width - segsWidth(c.first)
		r.emit(append(append([]Seg{}, c.first...), Seg{Text: centered("·  ·  ·", w), Role: RuleRole}))
	case doc.Pre:
		for i, ln := range strings.Split(b.Text, "\n") {
			p := c.rest
			if i == 0 {
				p = c.first
			}
			avail := c.width - segsWidth(p)
			for _, part := range hardSplit(expandTabs(ln), avail) {
				r.emit(append(append([]Seg{}, p...), Seg{Text: part, Role: CodeRole}))
				p = c.rest
			}
		}
	case doc.Image:
		label := "image"
		if b.Caption != "" {
			label = b.Caption
		} else if b.Alt != "" {
			label = b.Alt
		}
		r.wrapPrefixed(doc.Inline{{Text: label}}, c, ImageRole, Seg{Text: "▒▒ ", Role: ImageRole})
	case doc.Columns:
		r.columns(b, c)
	case doc.Notice:
		r.wrapPrefixed(doc.Inline{{Text: b.Text}}, c, NoticeRole, Seg{Text: "※ ", Role: NoticeRole})
	case doc.Embed:
		r.wrapPrefixed(doc.Inline{{Text: "embedded block (not loaded)"}}, c, NoticeRole, Seg{Text: "※ ", Role: NoticeRole})
	case doc.Footnotes:
		r.wrap(doc.Inline{{Text: "FOOTNOTES"}}, c, Dim)
		for _, n := range b.Notes {
			r.wrapPrefixed(n.Text, c.nest(Seg{}, Seg{Text: "    "}), Dim, Seg{Text: padRight("["+n.Label+"]", 4), Role: Dim})
		}
	}
}

// Columns need this much room each, and this gap between them.
const minColumn, columnGap = 34, 4

// columns lays blocks out side by side: each column on its own, then the
// lines joined. Its links come in column order, left column first.
func (r *renderer) columns(b doc.Columns, c ctx) {
	avail := c.width - segsWidth(c.rest)
	n := len(b.Cols)
	if n == 0 {
		return
	}
	colW := (avail - columnGap*(n-1)) / n
	if n == 1 || colW < minColumn {
		var all []doc.Block
		for _, col := range b.Cols {
			all = append(all, col...)
		}
		r.blocks(all, c)
		return
	}
	base := len(r.out.Lines)
	var cols [][]Line
	rows := 0
	for k, col := range b.Cols {
		sub := &renderer{d: r.d, o: Options{Width: colW, Open: r.o.Open}, out: &Layout{Width: colW}, lastLinkFocus: map[int]int{}}
		sub.blocks(col, ctx{width: colW})
		for len(sub.out.Lines) > 0 && len(sub.out.Lines[len(sub.out.Lines)-1].Segs) == 0 {
			sub.out.Lines = sub.out.Lines[:len(sub.out.Lines)-1]
		}
		shift := len(r.out.Focus)
		for _, f := range sub.out.Focus {
			if f.Group > 0 {
				f.Group += base // columns inside a column keep their own place
			} else {
				f.Group, f.Col = base+1, k
			}
			f.Line += base
			r.out.Focus = append(r.out.Focus, f)
		}
		for i := range sub.out.Lines {
			for j := range sub.out.Lines[i].Segs {
				if sub.out.Lines[i].Segs[j].Focus > 0 {
					sub.out.Lines[i].Segs[j].Focus += shift
				}
			}
		}
		for _, h := range sub.out.Headings {
			h.Line += base
			r.out.Headings = append(r.out.Headings, h)
		}
		for _, p := range sub.out.Paras {
			p.Start, p.End = p.Start+base, p.End+base
			r.out.Paras = append(r.out.Paras, p)
		}
		cols = append(cols, sub.out.Lines)
		rows = max(rows, len(sub.out.Lines))
	}
	for i := 0; i < rows; i++ {
		line := append([]Seg{}, c.rest...)
		if i == 0 {
			line = append([]Seg{}, c.first...)
		}
		for k, col := range cols {
			var segs []Seg
			if i < len(col) {
				segs = col[i].Segs
			}
			line = append(line, segs...)
			if k < len(cols)-1 {
				if pad := colW - segsWidth(segs) + columnGap; pad > 0 {
					line = append(line, Seg{Text: strings.Repeat(" ", pad)})
				}
			}
		}
		r.emit(trimTrailingSpace(line))
	}
	r.lastLinkFocus = map[int]int{}
}

func (r *renderer) collapsible(b doc.Collapsible, c ctx) {
	open := b.Open
	if v, ok := r.o.Open[b.ID]; ok {
		open = v
	}
	r.out.Focus = append(r.out.Focus, Focusable{Kind: FocusFold, Fold: b.ID, Line: len(r.out.Lines)})
	fi := len(r.out.Focus)
	marker, label := "[+] ", b.Show
	if open {
		marker = "[-] "
		if b.Hide != "" {
			label = b.Hide
		}
	}
	r.wrapPrefixedFocus(doc.Inline{{Text: label}}, c, Fold, Seg{Text: marker, Role: Fold, Focus: fi}, fi)
	if !open {
		return
	}
	bar := Seg{Text: "│ ", Role: RuleRole}
	inner := ctx{first: append(append([]Seg{}, c.rest...), bar), rest: append(append([]Seg{}, c.rest...), bar), width: c.width}
	r.blocks(b.Blocks, inner)
	r.emit(append(append([]Seg{}, c.rest...), Seg{Text: "└─", Role: RuleRole}))
}

func (r *renderer) table(t doc.Table, c ctx) {
	cols := 0
	for _, row := range t.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	widths := make([]int, cols)
	for _, row := range t.Rows {
		for i, cell := range row {
			if w := runewidth.StringWidth(collapse(cell.PlainText())); w > widths[i] {
				widths[i] = w
			}
		}
	}
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	avail := c.width - segsWidth(c.rest)
	if total <= avail && cols > 0 {
		border := func(l, m, rgt string) {
			var b strings.Builder
			b.WriteString(l)
			for i, w := range widths {
				b.WriteString(strings.Repeat("─", w+2))
				if i < cols-1 {
					b.WriteString(m)
				}
			}
			b.WriteString(rgt)
			r.emit(append(append([]Seg{}, c.rest...), Seg{Text: b.String(), Role: RuleRole}))
		}
		border("┌", "┬", "┐")
		for ri, row := range t.Rows {
			segs := append([]Seg{}, c.rest...)
			segs = append(segs, Seg{Text: "│ ", Role: RuleRole})
			for i := 0; i < cols; i++ {
				text := ""
				if i < len(row) {
					text = collapse(row[i].PlainText())
				}
				role := Body
				if ri == 0 && t.Header {
					role = H3
				}
				segs = append(segs, Seg{Text: padRight(text, widths[i]), Role: role}, Seg{Text: " │ ", Role: RuleRole})
			}
			segs[len(segs)-1].Text = " │"
			r.emit(segs)
			if ri == 0 && t.Header {
				border("├", "┼", "┤")
			}
		}
		border("└", "┴", "┘")
		return
	}
	// Too wide: render each row as a small card.
	var header []string
	rows := t.Rows
	if t.Header && len(rows) > 0 {
		for _, h := range rows[0] {
			header = append(header, collapse(h.PlainText()))
		}
		rows = rows[1:]
	}
	for ri, row := range rows {
		if ri > 0 {
			r.blank(c)
		}
		for i, cell := range row {
			label := "•"
			if i < len(header) && header[i] != "" {
				label = header[i] + ":"
			}
			in := append(doc.Inline{{Text: label + " ", Style: doc.Bold}}, cell...)
			r.wrap(in, c, Body)
		}
	}
}

// wrap flows inline text into lines within ctx.
func (r *renderer) wrap(in doc.Inline, c ctx, role Role) {
	r.flow(in, c.first, c.rest, c.width, role, 0)
}

func (r *renderer) wrapPrefixed(in doc.Inline, c ctx, role Role, lead Seg) {
	pad := Seg{Text: strings.Repeat(" ", runewidth.StringWidth(lead.Text))}
	first := append(append([]Seg{}, c.first...), lead)
	rest := append(append([]Seg{}, c.rest...), pad)
	r.flow(in, first, rest, c.width, role, 0)
}

func (r *renderer) wrapPrefixedFocus(in doc.Inline, c ctx, role Role, lead Seg, focus int) {
	pad := Seg{Text: strings.Repeat(" ", runewidth.StringWidth(lead.Text))}
	first := append(append([]Seg{}, c.first...), lead)
	rest := append(append([]Seg{}, c.rest...), pad)
	r.flow(in, first, rest, c.width, role, focus)
}

// token is a word, a space, or a hard break, carrying span attributes.
type token struct {
	text  string
	space bool
	brk   bool
	span  doc.Span
}

func tokenize(in doc.Inline) []token {
	var toks []token
	for _, s := range in {
		if s.Break {
			toks = append(toks, token{brk: true})
			continue
		}
		var word strings.Builder
		flushWord := func() {
			if word.Len() > 0 {
				toks = append(toks, token{text: word.String(), span: s})
				word.Reset()
			}
		}
		for _, ch := range s.Text {
			if ch == ' ' {
				word.WriteRune(' ') // non-breaking space stays inside the word
				continue
			}
			if unicode.IsSpace(ch) {
				flushWord()
				if len(toks) == 0 || !toks[len(toks)-1].space {
					toks = append(toks, token{text: " ", space: true, span: s})
				}
				continue
			}
			word.WriteRune(ch)
		}
		flushWord()
	}
	return toks
}

func (r *renderer) flow(in doc.Inline, first, rest []Seg, width int, role Role, focus int) {
	toks := tokenize(in)
	prefix := first
	line := append([]Seg{}, prefix...)
	lineW := segsWidth(prefix)
	contentW := 0
	var pendingSpace *token
	startLine := len(r.out.Lines)

	newLine := func() {
		r.emit(line)
		prefix = rest
		line = append([]Seg{}, prefix...)
		lineW = segsWidth(prefix)
		contentW = 0
		pendingSpace = nil
	}
	seg := func(t token) Seg {
		sg := Seg{Text: t.text, Role: role, Style: t.span.Style, Link: t.span.Link, Focus: focus}
		if t.span.Link > 0 {
			sg.Role = LinkRole
			if focus == 0 {
				sg.Focus = r.linkFocus(t.span.Link, startLine+(len(r.out.Lines)-startLine))
			}
		}
		if t.span.Style&doc.Code != 0 && role == Body {
			sg.Role = CodeRole
		}
		return sg
	}
	for i := range toks {
		t := toks[i]
		switch {
		case t.brk:
			newLine()
		case t.space:
			if contentW > 0 {
				pendingSpace = &toks[i]
			}
		default:
			w := runewidth.StringWidth(t.text)
			need := w
			if pendingSpace != nil {
				need++
			}
			if contentW > 0 && lineW+need > width {
				newLine()
				need = w
			}
			if pendingSpace != nil {
				sp := seg(*pendingSpace)
				// A space only keeps link styling when both neighbours share the link.
				if pendingSpace.span.Link != t.span.Link {
					sp.Link, sp.Role, sp.Focus = 0, role, focus
					sp.Style &^= doc.Underline
				}
				line = append(line, sp)
				lineW++
				pendingSpace = nil
			}
			// Hard-split words longer than the available width.
			for lineW+w > width && w > width-segsWidth(prefix) {
				avail := width - lineW
				if avail <= 0 {
					newLine()
					continue
				}
				head, tail := splitWidth(t.text, avail)
				tt := t
				tt.text = head
				line = append(line, seg(tt))
				newLine()
				t.text = tail
				w = runewidth.StringWidth(tail)
			}
			line = append(line, seg(t))
			lineW += w
			contentW += w
		}
	}
	if contentW > 0 || len(r.out.Lines) == startLine {
		r.emit(line)
	}
	r.lastLinkFocus = map[int]int{}
}

// linkFocus returns the focusable index for a link occurrence, creating one
// when the link starts a new run.
func (r *renderer) linkFocus(link, line int) int {
	if fi, ok := r.lastLinkFocus[link]; ok {
		return fi
	}
	r.out.Focus = append(r.out.Focus, Focusable{Kind: FocusLink, Link: link, Line: line})
	fi := len(r.out.Focus)
	r.lastLinkFocus[link] = fi
	return fi
}

// merge joins adjacent segments with identical attributes.
func merge(segs []Seg) []Seg {
	var out []Seg
	for _, s := range segs {
		if s.Text == "" {
			continue
		}
		if n := len(out); n > 0 {
			p := &out[n-1]
			if p.Role == s.Role && p.Style == s.Style && p.Link == s.Link && p.Focus == s.Focus {
				p.Text += s.Text
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func trimTrailingSpace(segs []Seg) []Seg {
	out := append([]Seg{}, segs...)
	for len(out) > 0 {
		last := &out[len(out)-1]
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// upper upper-cases headings using the document language's casing rules:
// Turkish maps i→İ and ı→I, everything else uses the Unicode default.
func upper(in doc.Inline, lang string) doc.Inline {
	out := make(doc.Inline, len(in))
	for i, s := range in {
		if strings.HasPrefix(lang, "tr") || strings.HasPrefix(lang, "az") {
			s.Text = strings.ToUpperSpecial(unicode.TurkishCase, s.Text)
		} else {
			s.Text = strings.ToUpper(s.Text)
		}
		out[i] = s
	}
	return out
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func padRight(s string, w int) string {
	if d := w - runewidth.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func centered(s string, w int) string {
	d := w - runewidth.StringWidth(s)
	if d <= 0 {
		return s
	}
	return strings.Repeat(" ", d/2) + s
}

func splitWidth(s string, w int) (string, string) {
	cur := 0
	for i, ch := range s {
		cw := runewidth.RuneWidth(ch)
		if cur+cw > w {
			return s[:i], s[i:]
		}
		cur += cw
	}
	return s, ""
}

func hardSplit(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	for runewidth.StringWidth(s) > w {
		h, t := splitWidth(s, w)
		out = append(out, h)
		s = t
	}
	return append(out, s)
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// paraText is the clippable text of a block; containers (lists, quotes,
// sections) have none of their own — their paragraphs are recorded.
func paraText(b doc.Block) string {
	switch b := b.(type) {
	case doc.Paragraph:
		return strings.TrimSpace(b.Text.PlainText())
	case doc.Heading:
		return strings.TrimSpace(b.Text.PlainText())
	case doc.Pre:
		return strings.TrimRight(b.Text, "\n")
	case doc.Table:
		var rows []string
		for _, r := range b.Rows {
			var cells []string
			for _, c := range r {
				cells = append(cells, strings.TrimSpace(c.PlainText()))
			}
			rows = append(rows, strings.Join(cells, " · "))
		}
		return strings.Join(rows, "\n")
	}
	return ""
}
