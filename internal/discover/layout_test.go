package discover

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
)

// columnLine is a line of a column: words of uneven width with 24 pixels between.
func columnLine(x, y int, words []string) []lword {
	var ws []lword
	for i, w := range words {
		width := 50 + (x/7+y/3+i*31)%60 // uneven, as words are: no river runs down the column
		ws = append(ws, lword{l: x, r: x + width, t: y, b: y + 36, s: w})
		x += width + 24
	}
	return ws
}

func twoColumns(lines int) []lword {
	var page []lword
	for i := 0; i < lines; i++ {
		y := 200 + i*48
		page = append(page, columnLine(100, y, strings.Fields(fmt.Sprintf("left%d a b c d e f g", i)))...)
		page = append(page, columnLine(1300, y, strings.Fields(fmt.Sprintf("right%d a b c d e f g", i)))...)
	}
	return page
}

func TestReflowPageReadsColumnsOneAfterTheOther(t *testing.T) {
	out := reflowPage(twoColumns(30))
	lastLeft, firstRight := strings.Index(out, "left29"), strings.Index(out, "right0 ")
	if lastLeft < 0 || firstRight < 0 || lastLeft > firstRight {
		t.Fatalf("the left column is read first, to its end:\n%s", out)
	}
	if strings.Contains(out, "g right") || strings.Contains(out, "g left") {
		t.Errorf("a line is not made of both columns:\n%s", out)
	}
}

func TestReflowPageKeepsAHeadlineAboveItsColumns(t *testing.T) {
	page := columnLine(100, 100, []string{"THE", "GREAT", "HEADLINE", "OVER", "BOTH", "COLUMNS", "OF", "THE", "PAGE", "ENDS", "HERE", "AND", "NOW"})
	for i := range page { // spans the gutter
		page[i].l, page[i].r = 100+i*200, 100+i*200+180
	}
	page = append(page, twoColumns(30)...)
	out := reflowPage(page)
	if !strings.HasPrefix(out, "THE GREAT HEADLINE") || !strings.Contains(out, "NOW") {
		t.Errorf("the headline is read whole, first:\n%s", out[:200])
	}
	if strings.Index(out, "left29") > strings.Index(out, "right0 ") {
		t.Error("columns under the headline are still read one after the other")
	}
}

func TestReflowPageIgnoresARuleBetweenColumns(t *testing.T) {
	page := twoColumns(30)
	for i := 0; i < 30; i++ { // the column rule, read as a letter on every line
		page = append(page, lword{l: 1085, r: 1095, t: 200 + i*48, b: 236 + i*48, s: "|"})
	}
	out := reflowPage(page)
	if strings.Index(out, "left29") > strings.Index(out, "right0 ") {
		t.Errorf("a rule in the gutter does not join the columns:\n%s", out[:300])
	}
}

func xmlPage(ws []lword) string {
	var b strings.Builder
	b.WriteString("<OBJECT height=\"4843\" width=\"3189\">\n<HIDDENTEXT><PAGECOLUMN><REGION><PARAGRAPH><LINE>\n")
	for _, w := range ws {
		fmt.Fprintf(&b, "<WORD coords=\"%d,%d,%d,%d,%d\">%s</WORD>\n", w.l, w.b, w.r, w.t, w.b-4, w.s)
	}
	b.WriteString("</LINE></PARAGRAPH></REGION></PAGECOLUMN></HIDDENTEXT>\n</OBJECT>\n")
	return b.String()
}

func TestDjvuPagesSkipsPagesTheRangeCutsOff(t *testing.T) {
	full := xmlPage(twoColumns(3))
	chunk := "      <WORD coords=\"1,2,3,4\">cut</WORD>\n</OBJECT>\n" + full + strings.Replace(full, "</OBJECT>", "", 1)
	pages := djvuPages(chunk)
	if len(pages) != 1 || len(pages[0]) != 48 {
		t.Fatalf("one whole page: %d pages", len(pages))
	}
	esc := djvuPages("<OBJECT >\n<WORD coords=\"1,10,50,2\" x-confidence=\"98\">Smith &amp; Sons</WORD></OBJECT>")
	if len(esc) != 1 || esc[0][0].s != "Smith & Sons" {
		t.Errorf("entities and extra attributes: %+v", esc)
	}
}

// columnsXML is the DjVu XML of an issue of pages: two columns on each, the
// left telling one thing and the right another, so that a page read across
// shows.
func columnsXML(pages int) string {
	var xml strings.Builder
	xml.WriteString("<?xml version=\"1.0\"?>\n<DjVuXML><BODY>\n")
	left := strings.Fields(strings.Repeat("The old naturalist walked down to the shore every morning and wrote what he saw in a small book. ", 400))
	right := strings.Fields(strings.Repeat("For he held that nothing seen once is ever seen twice in quite the same way, and so he looked again. ", 400))
	at := 0
	for p := 0; p < pages; p++ {
		var page []lword
		for i := 0; i < 40; i++ {
			y := 200 + i*48
			page = append(page, columnLine(100, y, left[at:at+8])...)
			page = append(page, columnLine(1300, y, right[at:at+8])...)
			at += 8
		}
		xml.WriteString(xmlPage(page))
	}
	xml.WriteString("</BODY></DjVuXML>\n")
	return xml.String()
}

func TestWindowOnPagesOfTheXML(t *testing.T) {
	body := columnsXML(12)
	var gotRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		http.ServeContent(w, r, "x_djvu.xml", time.Time{}, strings.NewReader(body))
	}))
	defer srv.Close()
	src := srv.URL + "/download/x/x_djvu.xml"
	from := len(body) / 5
	target := windowPagesTarget(src, "Weird Tales v22 (1933)", from, from+len(body)/3, len(body))
	q, _ := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/window?"))
	if q.Get("r") != fmt.Sprintf("%d-%d/%d", from, from+len(body)/3, len(body)) {
		t.Fatalf("query %v", q)
	}
	d, err := Route(t.Context(), target, Env{Fetcher: testFetcher()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotRange, "bytes=") {
		t.Errorf("asked for a range, got %q", gotRange)
	}
	if len(d.Links) == 0 || d.Links[0].Href != strings.Replace(src, "_djvu.xml", "_djvu.txt", 1) {
		t.Errorf("the whole text is the Archive's text file: %+v", d.Links)
	}
	var all strings.Builder
	for _, b := range d.Blocks {
		if p, ok := b.(doc.Paragraph); ok {
			all.WriteString(p.Text[0].Text + "\n")
		}
	}
	if !strings.Contains(all.String(), "naturalist") || strings.Contains(all.String(), "book. For he") || strings.Contains(all.String(), "every For") {
		t.Errorf("the left column goes on in the left column:\n%.400s", all.String())
	}
	if _, err := Route(t.Context(), "w5f:discover/window?u="+url.QueryEscape(src)+"&r=9-3/100", Env{Fetcher: testFetcher()}); err == nil {
		t.Error("a bad range is refused")
	}
}

func TestPagesWindowRangeStaysInsideTheFile(t *testing.T) {
	for _, size := range []int{1, 9000, pagesRange - 1, pagesRange, pagesRange + 1, pagesRange + pagesRange/9, pagesRange*10/9 + 5, 3 * pagesRange, 200 << 20} {
		for range 200 {
			from, to := pagesWindowRange(size, pagesRange)
			if from < 0 || to < from || to > size-1 {
				t.Fatalf("size %d: bytes %d-%d", size, from, to)
			}
			if size > pagesRange*2 && to-from+1 != pagesRange {
				t.Fatalf("size %d: %d bytes", size, to-from+1)
			}
		}
	}
}

func TestBlockTextChainsLinesOfColumnsTooCloseToCut(t *testing.T) {
	var ws []lword
	id := 0
	for col, x0 := range []int{100, 706} { // 600 pixels wide each, 6 between
		name := []string{"left", "right"}[col]
		for i := 0; i < 20; i++ {
			id++
			y := 200 + i*40
			x := x0
			for k, w := range strings.Fields(fmt.Sprintf("%s%d aa bbb cc dddd ee ff", name, i)) {
				width := 60 + (k*37+i*11)%50
				ws = append(ws, lword{l: x, r: x + width, t: y, b: y + 34, s: w, line: id})
				x += width + 20
			}
		}
	}
	var out []string
	blockText(ws, 34, &out)
	text := strings.Join(out, "\n\n")
	if strings.Index(text, "left19") > strings.Index(text, "right0 ") {
		t.Errorf("the left column is read before the right one:\n%.300s", text)
	}
	if strings.Contains(text, "ff right") || strings.Contains(text, "ff left") {
		t.Errorf("no line holds pieces of two columns:\n%.300s", text)
	}
}

// A headline set close over its columns (less than a line and a half of
// space) is not cut in two at the gutter under it.
func TestReflowPageKeepsATightHeadlineWhole(t *testing.T) {
	head := columnLine(100, 100, strings.Fields("THE SENATE VOTES TO END THE LONG STRIKE AFTER TEN DAYS OF TALKS"))
	for i := range head { // spans the gutter, 44 pixels over the first line of the columns
		head[i].l, head[i].r = 100+i*190, 100+i*190+170
		head[i].t, head[i].b = 120, 156
	}
	page := append(head, twoColumns(30)...)
	out := reflowPage(page)
	i, j := strings.Index(out, "THE SENATE"), strings.Index(out, "DAYS OF TALKS")
	if i != 0 || j < 0 || strings.Contains(out[i:j], "left0") || strings.Contains(out[i:j], "right0") {
		t.Errorf("the headline is read whole, before the columns:\n%.300s", out)
	}
	if strings.Index(out, "left29") > strings.Index(out, "right0 ") {
		t.Error("the columns under it are read one after the other")
	}
}

// Print set tighter than the height of a word's box is read in lines.
func TestTextRowsOfTightPrint(t *testing.T) {
	var ws []lword
	for row := 0; row < 8; row++ {
		x := 100
		for k := 0; k < 6; k++ {
			ws = append(ws, lword{l: x, r: x + 60, t: 100 + row*27 + k%2*3, b: 136 + row*27 + k%2*3, s: fmt.Sprintf("w%d_%d", row, k)})
			x += 80
		}
	}
	rows := textRows(ws, 36)
	if len(rows) != 8 {
		t.Fatalf("%d rows, want 8", len(rows))
	}
	for i, r := range rows {
		if len(r) != 6 || !strings.HasPrefix(r[0].s, fmt.Sprintf("w%d_", i)) {
			t.Errorf("row %d: %v", i, r)
		}
	}
}

// The Archive cuts justified print into lines at its widest spaces: those
// pieces do not stand side by side, and the rows are read as they are.
func TestJustifiedPrintCutIntoPiecesIsNotColumns(t *testing.T) {
	var ws []lword
	id := 0
	for row := 0; row < 20; row++ {
		x := 100
		for k, w := range strings.Fields(fmt.Sprintf("row%d alpha beta gamma delta epsilon zeta", row)) {
			if (row*7+k*3)%5 == 0 { // a piece of its own, where it happens to fall
				id++
			}
			width := 50 + (k*37+row*11)%40
			ws = append(ws, lword{l: x, r: x + width, t: 200 + row*40, b: 236 + row*40, s: w, line: id})
			x += width + 20 + (row*13+k*17)%25
		}
		id++
	}
	var out []string
	blockText(ws, 36, &out)
	text := strings.Join(out, "\n")
	for row := 0; row < 19; row++ {
		if strings.Index(text, fmt.Sprintf("row%d ", row)) > strings.Index(text, fmt.Sprintf("row%d ", row+1)) {
			t.Fatalf("rows out of order at %d:\n%.300s", row, text)
		}
	}
}

// The paragraphs of a column set tight begin where a line is set in, though
// every line starts a few pixels off the margin.
func TestEmitParagraphsFindsTheIndentsOfAJitteryColumn(t *testing.T) {
	var lines [][]lword
	for i := 0; i < 12; i++ {
		x := 100 + (i*5)%9 // the margin wanders
		if i == 0 || i == 6 {
			x = 140
		}
		var l []lword
		for k := 0; k < 5; k++ {
			l = append(l, lword{l: x, r: x + 60, t: 200 + i*27, b: 236 + i*27, s: fmt.Sprintf("w%d_%d", i, k)})
			x += 80
		}
		lines = append(lines, l)
	}
	var out []string
	emitParagraphs(lines, 40, &out)
	if len(out) != 2 || !strings.HasPrefix(out[1], "w6_0") || strings.Count(out[0], "\n") != 5 {
		t.Errorf("two paragraphs, the second at the second indent: %q", out)
	}
}

// A line set in another size than the text over it (a headline over a deck, a
// deck over its story) is a paragraph of its own.
func TestEmitParagraphsSetsApartAHeadline(t *testing.T) {
	var lines [][]lword
	for i := 0; i < 6; i++ {
		size := 36
		if i < 2 {
			size = 60
		}
		var l []lword
		x := 100
		for k := 0; k < 5; k++ {
			l = append(l, lword{l: x, r: x + 60, t: 200 + i*40, b: 200 + i*40 + size, s: fmt.Sprintf("w%d_%d", i, k)})
			x += 80
		}
		lines = append(lines, l)
	}
	var out []string
	emitParagraphs(lines, 40, &out)
	if len(out) != 2 || strings.Count(out[0], "\n") != 1 {
		t.Errorf("the headline apart from the text: %q", out)
	}
}

// justifiedLine is a line of a column set to its full width: words of uneven
// width, the last one ending at the column's right edge, all of one line of
// the Archive's layout.
func justifiedLine(x0, width, y, box, id int, words []string) []lword {
	widths, total := make([]int, len(words)), 0
	for k := range words {
		widths[k] = 50 + (k*37+id*11)%50
		total += widths[k]
	}
	var ws []lword
	x := x0
	for k, w := range words {
		if k == len(words)-1 {
			x = x0 + width - widths[k]
		}
		ws = append(ws, lword{l: x, r: x + widths[k], t: y, b: y + box, s: w, line: id})
		x += widths[k] + (width-total)/(len(words)-1)
	}
	return ws
}

// narrowColumns is a page of two columns 600 pixels wide with 20 between,
// lines of them 40 apart.
func narrowColumns(lines int) []lword {
	var page []lword
	for i := 0; i < lines; i++ {
		y := 200 + i*40
		page = append(page, justifiedLine(100, 600, y, 34, 2*i+1, strings.Fields(fmt.Sprintf("left%d aa bbb cc dddd ee ff", i)))...)
		page = append(page, justifiedLine(720, 600, y, 34, 2*i+2, strings.Fields(fmt.Sprintf("right%d aa bbb cc dddd ee ff", i)))...)
	}
	return page
}

func columnsReadInTurn(t *testing.T, out string, lines int) {
	t.Helper()
	last, first := strings.Index(out, fmt.Sprintf("left%d ", lines-1)), strings.Index(out, "right0 ")
	if last < 0 || first < 0 || last > first {
		t.Errorf("the left column is read first, to its end:\n%.400s", out)
	}
	if strings.Contains(out, "ff right") || strings.Contains(out, "ff left") {
		t.Errorf("no line holds pieces of two columns:\n%.400s", out)
	}
	for i := 0; i+1 < lines; i++ {
		if strings.Index(out, fmt.Sprintf("left%d ", i)) > strings.Index(out, fmt.Sprintf("left%d ", i+1)) {
			t.Fatalf("lines out of order at %d:\n%.400s", i, out)
		}
	}
}

// A page whose lines run uphill by nearly the space between two of them (the
// page curled, the film lay askew) is read line by line all the same.
func TestReflowPageLevelsSlantedLines(t *testing.T) {
	var page []lword
	for i := 0; i < 24; i++ {
		line := justifiedLine(100, 1300, 200+i*40, 34, i+1, strings.Fields(fmt.Sprintf("line%d aa bbb cc dddd ee ff gg hhh ii jj kkk ll end%d", i, i)))
		for k := range line {
			up := (line[k].l - 100) * 35 / 1000 // 42 pixels at the right edge, the lines 40 apart
			line[k].t, line[k].b = line[k].t-up, line[k].b-up
		}
		page = append(page, line...)
	}
	lines := strings.Split(strings.ReplaceAll(reflowPage(page), "\n\n", "\n"), "\n")
	if len(lines) != 24 {
		t.Fatalf("%d lines, want 24:\n%.400s", len(lines), strings.Join(lines, "\n"))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, fmt.Sprintf("line%d ", i)) || !strings.HasSuffix(l, fmt.Sprintf(" end%d", i)) {
			t.Errorf("line %d: %q", i, l)
		}
	}
}

// A curled page slants more on one side than on the other: each side is
// levelled by its own slant.
func TestLevelFollowsASlantThatChangesAcrossThePage(t *testing.T) {
	page := narrowColumns(30)
	for k := range page {
		if page[k].l < 710 { // the left column runs uphill, the right one is level
			up := (page[k].l - 100) * 30 / 1000
			page[k].t, page[k].b = page[k].t-up, page[k].b-up
		}
	}
	top, bottom := map[int]float64{}, map[int]float64{}
	for _, w := range level(page, 34) {
		y := midY(w)
		if _, ok := top[w.line]; !ok {
			top[w.line], bottom[w.line] = y, y
		}
		top[w.line], bottom[w.line] = min(top[w.line], y), max(bottom[w.line], y)
	}
	for id := range top {
		if bottom[id]-top[id] > 6 {
			t.Errorf("line %d still runs %v pixels up or down", id, bottom[id]-top[id])
		}
	}
}

// Columns that lean by more than their gutter is wide (no straight cut goes
// down between them) are stood upright and read one after the other.
func TestReflowPageStandsLeaningColumnsUpright(t *testing.T) {
	page := narrowColumns(60)
	for k := range page {
		over := (page[k].t - 200) * 30 / 1000 // 72 pixels at the foot of the page
		page[k].l, page[k].r = page[k].l+over, page[k].r+over
	}
	columnsReadInTurn(t, reflowPage(page), 60)
}

// The rule between two columns, read as a mark in the gutter, and a last word
// that reaches a little into it are not a headline across the columns: the
// page is not cut across at their lines.
func TestReflowPageKeepsColumnsWholeWhereMarksCrossTheGutter(t *testing.T) {
	page := narrowColumns(60)
	for k := range page {
		if page[k].s == "ff" && page[k].line == 2*20+1 {
			page[k].r += 13 // the last word of a left line, three pixels past the middle of the gutter
		}
	}
	for _, i := range []int{7, 33, 48} {
		page = append(page, lword{l: 704, r: 716, t: 200 + i*40, b: 234 + i*40, s: ",", line: 2*i + 1})
	}
	columnsReadInTurn(t, reflowPage(page), 60)
}

// Small print is set tighter than its words' boxes are tall (a legal notice,
// a market report): the lines of two columns too close to cut between are
// still chained each to the one under it, not to the next but one.
func TestBlockTextChainsSmallPrintSetTight(t *testing.T) {
	var ws []lword
	for i := 0; i < 30; i++ {
		y := 200 + i*16
		ws = append(ws, justifiedLine(100, 600, y, 28, 2*i+1, strings.Fields(fmt.Sprintf("left%d aa bbb cc dddd ee ff", i)))...)
		ws = append(ws, justifiedLine(706, 600, y, 28, 2*i+2, strings.Fields(fmt.Sprintf("right%d aa bbb cc dddd ee ff", i)))...)
	}
	var out []string
	blockText(ws, 29, &out)
	columnsReadInTurn(t, strings.Join(out, "\n"), 30)
}

// One mark read too tall in a line (a blot, a piece of a rule) does not make
// the line reach into the next and break the chain of its column.
func TestBlockTextChainsPastAMarkReadTooTall(t *testing.T) {
	var ws []lword
	for i := 0; i < 20; i++ {
		y := 200 + i*40
		ws = append(ws, justifiedLine(100, 600, y, 34, 2*i+1, strings.Fields(fmt.Sprintf("left%d aa bbb cc dddd ee ff", i)))...)
		ws = append(ws, justifiedLine(706, 600, y, 34, 2*i+2, strings.Fields(fmt.Sprintf("right%d aa bbb cc dddd ee ff", i)))...)
		if i%2 == 1 {
			ws = append(ws, lword{l: 300, r: 308, t: y - 30, b: y + 60, s: "l", line: 2*i + 1})
		}
	}
	var out []string
	blockText(ws, 34, &out)
	columnsReadInTurn(t, strings.Join(out, "\n"), 20)
}
