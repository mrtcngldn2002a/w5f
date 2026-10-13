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

func TestWindowOnPagesOfTheXML(t *testing.T) {
	var xml strings.Builder
	xml.WriteString("<?xml version=\"1.0\"?>\n<DjVuXML><BODY>\n")
	left := strings.Fields(strings.Repeat("The old naturalist walked down to the shore every morning and wrote what he saw in a small book. ", 400))
	right := strings.Fields(strings.Repeat("For he held that nothing seen once is ever seen twice in quite the same way, and so he looked again. ", 400))
	at := 0
	for p := 0; p < 12; p++ {
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
	body := xml.String()
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
	if len(d.Links) != 1 || d.Links[0].Href != strings.Replace(src, "_djvu.xml", "_djvu.txt", 1) {
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
			from, to := pagesWindowRange(size)
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
