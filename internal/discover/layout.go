package discover

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The Archive's plain text for a scan reads a page line by line across the
// whole width, so a two or four column page comes out with its columns
// woven together. Its DjVu XML keeps where every word stood on the page, and
// from that the columns can be told apart again: a page is cut where blank
// space runs through it (down between columns, across between blocks), the
// pieces are read in order, and the lines of a piece are put back together.
// Draws read a range of that file, a few pages, not all of it.

// lword is a word of a scanned page: left, bottom, right and top edges in
// pixels (y grows downward).
type lword struct {
	l, b, r, t int
	s          string
	line       int // the number of the line the Archive's own layout put it in
}

var reLayoutWord = regexp.MustCompile(`<WORD coords="(\d+),(\d+),(\d+),(\d+)(?:,\d+)?"[^>]*>([^<]*)</WORD>`)

// djvuPages reads the pages of a piece of DjVu XML. A page the piece cuts
// off at either end is left out.
func djvuPages(chunk string) [][]lword {
	var pages [][]lword
	parts := strings.Split(chunk, "<OBJECT ")
	for _, part := range parts[1:] {
		if !strings.Contains(part, "</OBJECT>") {
			continue
		}
		var ws []lword
		lines := strings.Split(part, "<LINE>")
		if len(lines) > 1 {
			lines = lines[1:] // what is before the first line is the page's own tags
		}
		for n, ln := range lines {
			for _, m := range reLayoutWord.FindAllStringSubmatch(ln, -1) {
				l, _ := strconv.Atoi(m[1])
				b, _ := strconv.Atoi(m[2])
				r, _ := strconv.Atoi(m[3])
				t, _ := strconv.Atoi(m[4])
				s := m[5]
				if strings.Contains(s, "&") {
					s = html.UnescapeString(s)
				}
				if s != "" && r >= l && b >= t {
					ws = append(ws, lword{l: l, b: b, r: r, t: t, s: s, line: n + 1})
				}
			}
		}
		if len(ws) > 0 {
			pages = append(pages, ws)
		}
	}
	return pages
}

// reflowPages makes plain text of the pages, in reading order, with a blank
// line between paragraphs and a line break between lines (as the Archive's
// text has them, so that the cleaning in paragraphs() takes it as it is).
func reflowPages(pages [][]lword) string {
	var out []string
	for _, ws := range pages {
		out = append(out, reflowPage(ws))
	}
	return strings.Join(out, "\n\n")
}

func reflowPage(ws []lword) string {
	var hs []int
	for _, w := range ws {
		if len([]rune(w.s)) >= 2 {
			hs = append(hs, w.b-w.t)
		}
	}
	if len(hs) == 0 {
		return ""
	}
	sort.Ints(hs)
	h := float64(max(hs[len(hs)/2], 1))
	// The columns of the page as a whole, found with some words allowed
	// across them (headlines, advertisements): a block of a few lines has too
	// little of its own to find its gutters by.
	_, _, top, bottom := extent(ws)
	grid := gutterCuts(ws, h, int(0.15*float64(bottom-top)/h), 0.45*h)
	var paras []string
	cutRegion(ws, h, grid, &paras)
	return strings.Join(paras, "\n\n")
}

func extent(ws []lword) (lo, hi, top, bottom int) {
	lo, hi, top, bottom = ws[0].l, ws[0].r, ws[0].t, ws[0].b
	for _, w := range ws {
		lo, hi, top, bottom = min(lo, w.l), max(hi, w.r), min(top, w.t), max(bottom, w.b)
	}
	return
}

// cutRegion reads a region of a page: it is cut where space runs through it
// and the pieces are read in turn; a piece that cannot be cut is a block of
// lines. Cuts go from the surest to the least sure: a gutter with no word
// across it; a wide space between blocks (so that a headline over the
// columns is told from them); a gutter a few words cross (a headline or a
// rule that reaches over it); a gutter of the page's columns that no word of
// this piece crosses; then narrower spaces between lines.
func cutRegion(ws []lword, h float64, grid []int, out *[]string) {
	if len(ws) == 0 {
		return
	}
	if len(ws) >= 2 {
		for _, cut := range []func() [][]lword{
			func() [][]lword { return cutColumns(ws, h, false) },
			func() [][]lword { return cutRows(ws, 1.5*h) },
			func() [][]lword { return cutColumns(ws, h, true) },
			func() [][]lword { return cutGrid(ws, grid) },
			func() [][]lword { return cutRows(ws, 1.0*h) },
			func() [][]lword { return cutRows(ws, 0.5*h) },
		} {
			if groups := cut(); len(groups) > 1 {
				for _, g := range groups {
					cutRegion(g, h, grid, out)
				}
				return
			}
		}
	}
	blockText(ws, h, out)
}

// cutColumns cuts a region down its gutters: runs of x with no (or, when
// tolerant, hardly any) word across them.
func cutColumns(ws []lword, h float64, tolerant bool) [][]lword {
	_, _, top, bottom := extent(ws)
	lines := float64(bottom-top) / h
	need := 1.5 * h
	switch {
	case lines >= 20:
		need = 0.35 * h
	case lines >= 8:
		need = 0.6 * h
	}
	tol := 0
	if tolerant && lines >= 8 {
		tol = int(0.06 * lines)
	}
	return splitAt(ws, gutterCuts(ws, h, tol, need))
}

// gutterCuts are the x positions of the gutters of a region: runs of at
// least need pixels that no more than tol words cross. Words are counted
// only if they are wider than a stray mark, because the rule between
// newspaper columns is often read as a column of small letters.
func gutterCuts(ws []lword, h float64, tol int, need float64) []int {
	lo, hi, _, _ := extent(ws)
	diff := make([]int, hi-lo+2)
	for _, w := range ws {
		if float64(w.r-w.l) < 0.45*h {
			continue
		}
		diff[w.l-lo]++
		diff[w.r-lo+1]--
	}
	var cuts []int
	start, c := -1, 0
	for x := range diff {
		c += diff[x]
		if c <= tol {
			if start < 0 {
				start = x
			}
			continue
		}
		if start > 0 && float64(x-start) >= need {
			cuts = append(cuts, (start+x)/2+lo)
		}
		start = -1
	}
	return cuts
}

// cutGrid cuts a region at the page's gutters that none of its words
// crosses.
func cutGrid(ws []lword, grid []int) [][]lword {
	var cuts []int
	for _, c := range grid {
		crossed := false
		for _, w := range ws {
			if w.l < c && w.r > c {
				crossed = true
				break
			}
		}
		if !crossed {
			cuts = append(cuts, c)
		}
	}
	return splitAt(ws, cuts)
}

// splitAt divides words at the given x positions, left to right, and drops
// the pieces with nothing in them.
func splitAt(ws []lword, cuts []int) [][]lword {
	if len(cuts) == 0 {
		return nil
	}
	groups := make([][]lword, len(cuts)+1)
	for _, w := range ws {
		cx := (w.l + w.r) / 2
		i := sort.SearchInts(cuts, cx)
		groups[i] = append(groups[i], w)
	}
	var out [][]lword
	for _, g := range groups {
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// cutRows cuts a region across, where a band of at least gap pixels has no
// word in it, top to bottom.
func cutRows(ws []lword, gap float64) [][]lword {
	s := append([]lword(nil), ws...)
	sort.Slice(s, func(i, j int) bool { return s[i].t < s[j].t })
	var out [][]lword
	cur, maxB := []lword{s[0]}, s[0].b
	for _, w := range s[1:] {
		if float64(w.t-maxB) >= gap {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, w)
		maxB = max(maxB, w.b)
	}
	out = append(out, cur)
	if len(out) == 1 {
		return nil
	}
	return out
}

// blockText puts the lines of a block (a piece nothing cuts) back together.
// Where its rows hold pieces of several lines of the Archive's own layout,
// side by side, the columns are too close to cut between and the lines are
// chained instead (see lineChains).
func blockText(ws []lword, h float64, out *[]string) {
	rows := textRows(ws, h)
	if chains := lineChains(rows, h); chains != nil {
		for _, c := range chains {
			emitParagraphs(c, h, out)
		}
		return
	}
	emitParagraphs(rows, h, out)
}

// textRows groups words into rows by where they stand on the page, each
// row left to right, top to bottom.
func textRows(ws []lword, h float64) [][]lword {
	s := append([]lword(nil), ws...)
	mid := func(w lword) float64 { return float64(w.b+w.t) / 2 }
	sort.Slice(s, func(i, j int) bool { return mid(s[i]) < mid(s[j]) })
	var rows [][]lword
	cur, sum := []lword{s[0]}, mid(s[0])
	for _, w := range s[1:] {
		if abs := mid(w) - sum/float64(len(cur)); abs > 0.6*h || abs < -0.6*h {
			rows = append(rows, cur)
			cur, sum = nil, 0
		}
		cur = append(cur, w)
		sum += mid(w)
	}
	rows = append(rows, cur)
	for _, r := range rows {
		sort.Slice(r, func(i, j int) bool { return r[i].l < r[j].l })
	}
	return rows
}

// lineChains reads a block whose columns are too close to cut between (the
// small print of a market report, the columns of a classified page, where
// the gutter is hardly wider than a space). The Archive's layout gives the
// lines of a column as lines of their own, so each line is linked to the
// one under it that stands in the same place, and the chains so made are
// the columns, read one after the other. It returns nil when no row of the
// block holds two lines (the rows are then right as they are).
func lineChains(rows [][]lword, h float64) [][][]lword {
	multi := false
	for _, r := range rows {
		for _, w := range r[1:] {
			if w.line != r[0].line {
				multi = true
			}
		}
	}
	if !multi {
		return nil
	}
	byLine := map[int][]lword{}
	for _, r := range rows {
		for _, w := range r {
			byLine[w.line] = append(byLine[w.line], w)
		}
	}
	type seg struct {
		ws         []lword
		l, r, t, b int
	}
	var segs []*seg
	for _, ws := range byLine {
		sort.Slice(ws, func(i, j int) bool { return ws[i].l < ws[j].l })
		l, r, t, b := extent(ws)
		segs = append(segs, &seg{ws, l, r, t, b})
	}
	sort.Slice(segs, func(i, j int) bool {
		if segs[i].t != segs[j].t {
			return segs[i].t < segs[j].t
		}
		return segs[i].l < segs[j].l
	})
	next := make([]int, len(segs))
	hasPrev := make([]bool, len(segs))
	for i := range next {
		next[i] = -1
	}
	for i, si := range segs {
		best, bestGap := -1, 1e18
		for j := i - 1; j >= 0; j-- {
			sj := segs[j]
			gap := float64(si.t - sj.b)
			if gap > 2.2*h && float64(si.t-sj.t) > 6*h {
				break // everything before is further up still
			}
			if next[j] >= 0 || gap < -0.4*h || gap > 2.2*h {
				continue
			}
			over := float64(min(si.r, sj.r) - max(si.l, sj.l))
			if over < 0.5*float64(min(si.r-si.l, sj.r-sj.l)) {
				continue
			}
			if gap < bestGap {
				best, bestGap = j, gap
			}
		}
		if best >= 0 {
			next[best] = i
			hasPrev[i] = true
		}
	}
	type chain struct {
		lines      [][]lword
		l, r, t, b int
	}
	var chains []*chain
	for i := range segs {
		if hasPrev[i] {
			continue
		}
		c := &chain{l: segs[i].l, r: segs[i].r, t: segs[i].t, b: segs[i].b}
		for k := i; k >= 0; k = next[k] {
			c.lines = append(c.lines, segs[k].ws)
			c.l, c.r, c.b = min(c.l, segs[k].l), max(c.r, segs[k].r), max(c.b, segs[k].b)
		}
		chains = append(chains, c)
	}
	// Read the chains column by column: those that start at the same place
	// from the left are a column, top to bottom.
	var widths []int
	for _, c := range chains {
		widths = append(widths, c.r-c.l)
	}
	sort.Ints(widths)
	tol := 0.4 * float64(widths[len(widths)/2])
	sort.Slice(chains, func(i, j int) bool { return chains[i].l < chains[j].l })
	var cols [][]*chain
	for _, c := range chains {
		if n := len(cols); n > 0 && float64(c.l-cols[n-1][0].l) <= tol {
			cols[n-1] = append(cols[n-1], c)
			continue
		}
		cols = append(cols, []*chain{c})
	}
	var out [][][]lword
	for _, col := range cols {
		sort.Slice(col, func(i, j int) bool { return col[i].t < col[j].t })
		for _, c := range col {
			out = append(out, c.lines)
		}
	}
	return out
}

// emitParagraphs makes paragraphs of lines (each left to right): a new one
// begins where the print starts it: a wider space between lines, a first
// line set in, or a line that stopped short.
func emitParagraphs(lines [][]lword, h float64, out *[]string) {
	minL, maxR := lines[0][0].l, 0
	for _, l := range lines {
		minL, maxR = min(minL, l[0].l), max(maxR, l[len(l)-1].r)
	}
	var para []string
	var prev []lword
	flush := func() {
		if len(para) > 0 {
			*out = append(*out, strings.Join(para, "\n"))
			para = nil
		}
	}
	for _, l := range lines {
		if prev != nil {
			top, bottom := l[0].t, prev[0].b
			for _, w := range l {
				top = min(top, w.t)
			}
			for _, w := range prev {
				bottom = max(bottom, w.b)
			}
			indent := float64(l[0].l - minL)
			short := float64(prev[len(prev)-1].r) < float64(maxR)-4*h
			if float64(top-bottom) > 0.6*h || indent >= 1.2*h && indent <= 5*h || short {
				flush()
			}
		}
		para = append(para, lineText(l))
		prev = l
	}
	flush()
}

// edgeMarks are what the rule between two columns turns into where the line
// starts or ends: a quote, a comma, a hyphen, a bullet.
const edgeMarks = "'`,.-•_:;|~"

// lineText joins the words of a line, without the marks the column rules
// left at its two ends.
func lineText(l []lword) string {
	words := make([]string, 0, len(l))
	for _, w := range l {
		words = append(words, w.s)
	}
	for len(words) > 1 && strings.Trim(words[0], edgeMarks) == "" {
		words = words[1:]
	}
	for len(words) > 1 && strings.Trim(words[len(words)-1], edgeMarks+"") == "" && words[len(words)-1] != "." {
		words = words[:len(words)-1]
	}
	if t := strings.TrimLeft(words[0], edgeMarks); t != "" {
		words[0] = t
	}
	return strings.Join(words, " ")
}
