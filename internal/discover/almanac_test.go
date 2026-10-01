package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
)

func TestKingWenCoversEveryHexagramOnce(t *testing.T) {
	seen := map[int]bool{}
	for n := 0; n < 64; n++ {
		var y [6]bool
		for i := range y {
			y[i] = n&(1<<i) != 0
		}
		seen[kingWen(y)] = true
	}
	if len(seen) != 64 || seen[0] {
		t.Fatalf("%d hexagrams", len(seen))
	}
	for want, lines := range map[int][6]bool{
		1:  {true, true, true, true, true, true},
		2:  {},
		3:  {true, false, false, false, true, false}, // Zhen below, Kan above
		11: {true, true, true, false, false, false},  // Qian below, Kun above
		63: {true, false, true, false, true, false},  // Li below, Kan above
		30: {true, false, true, true, false, true},
	} {
		if got := kingWen(lines); got != want {
			t.Errorf("%v → %d, want %d", lines, got, want)
		}
	}
}

func TestOracleAlternatesByDay(t *testing.T) {
	a, b := oracleKind("2026-09-30"), oracleKind("2026-10-01")
	if a == b || oracleKind("2026-10-02") != a {
		t.Errorf("%s %s", a, b)
	}
}

// A day of the Book of Days as the site serves it: FrontPage markup, the
// menu in a paragraph and U+FFFD where dashes and pound signs were.
const bookOfDaysPage = `<html><body><table><tr><td>
<p><b>&nbsp; Home &nbsp;About: The Book of Days Its Author This Site</b></p>
<font SIZE="2"><p><font face="Verdana"><b>Born:</b> Euripides, tragic dramatist, 480
</font></font>
<font SIZE="1">B. C.</font>, Salamis.</p>
<p>Site Map</p><p>The Book of Days is proudly brought to you by the members of Emmitsburg.net</p><p>September 30th</p>
<p><font face="Verdana"><b>Feast Day :</b> St. Jerome.</font></p>
<p><b><font face="Verdana"><a name="WILLIAM HUTTON">WILLIAM HUTTON</a></font></b></p>
<p><font face="Verdana">Five shillings covered his expenses` + "\uFFFD" + `food and lodging, and he saved ` + "\uFFFD" + `20.</font></p>
<p><b>A CONTEST FOR PRECEDENCE</b></p>
<p>Sir John Finett, master of ceremonies.</p>
<p align="center">&nbsp;<a href="30a.htm"><u>Part 2 of Sept 30th</u></a></p>
<p>October 1st</p><p><b>BACK TO TOP &gt;</b></p>
</td></tr></table></body></html>`

func TestBookOfDaysParts(t *testing.T) {
	parts, err := bookOfDaysParts([]byte(bookOfDaysPage))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range parts {
		switch {
		case p.heading:
			got = append(got, "#"+p.text)
		case p.label != "":
			got = append(got, p.label+"|"+p.text)
		default:
			got = append(got, p.text)
		}
	}
	want := []string{"Born|Euripides, tragic dramatist, 480 B. C., Salamis.", "Feast Day|St. Jerome.", "#WILLIAM HUTTON",
		"Five shillings covered his expenses—food and lodging, and he saved £20.", "#A CONTEST FOR PRECEDENCE", "Sir John Finett, master of ceremonies.", "Part 2 of Sept 30th"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("parts:\n%s", strings.Join(got, "\n"))
	}
	if titleCase("A CONTEST FOR PRECEDENCE") != "A Contest for Precedence" || titleCase("REV. GEORGE WHITEFIELD") != "Rev. George Whitefield" {
		t.Error(titleCase("A CONTEST FOR PRECEDENCE"))
	}
}

func TestAlmanacColumnAndPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/months/sept/30.htm":
			fmt.Fprint(w, bookOfDaysPage)
		case "/months/sept/30a.htm":
			fmt.Fprint(w, `<html><body><p><b>THE SECOND PART</b></p><p>More of the day.</p></body></html>`)
		case "/en/selected/09/30":
			fmt.Fprint(w, `{"selected":[{"text":"The Magic Flute (poster pictured) premieres.","year":1791,"pages":[{"content_urls":{"desktop":{"page":"https://en.wikipedia.org/wiki/The_Magic_Flute"}}}]},
				{"text":"A second event.","year":1938,"pages":[{"content_urls":{"desktop":{"page":"https://en.wikipedia.org/wiki/B"}}}]},
				{"text":"No page.","year":2000,"pages":[]}]}`)
		case "/tr/events/09/30":
			fmt.Fprint(w, `{"events":[{"text":"Bir olay.","year":1453,"pages":[{"content_urls":{"desktop":{"page":"https://tr.wikipedia.org/wiki/X"}}}]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	swap(t, &bookOfDaysBase, srv.URL+"/months/")
	swap(t, &wikiOnThisDay, srv.URL+"/%s/%s/%02d/%02d")
	a, err := buildAlmanac(context.Background(), testFetcher(), time.September, 30)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.Headlines, "|") != "William Hutton|A Contest for Precedence" || !strings.HasPrefix(a.Born, "Euripides") || a.Day != "09-30" {
		t.Errorf("almanac: %+v", a)
	}
	var years []int
	for _, e := range a.Events {
		years = append(years, e.Year)
	}
	if fmt.Sprint(years) != "[1453 1791 1938]" {
		t.Errorf("events in year order, the one without a page left out: %v", years)
	}

	env := Env{Fetcher: testFetcher()}
	page, err := Route(context.Background(), "w5f:almanac/09-30", env)
	if err != nil || page.Title != "The Book of Days — 30 September" {
		t.Fatalf("page: %v %+v", err, page)
	}
	if h, ok := page.Blocks[2].(doc.Heading); !ok || h.Text[0].Text != "William Hutton" {
		t.Errorf("third block: %+v", page.Blocks[2])
	}
	if page.Next != "w5f:almanac/09-30/30a" {
		t.Errorf("a long day goes on to its next part: %q", page.Next)
	}
	if part, err := Route(context.Background(), "w5f:almanac/09-30/30a", env); err != nil || part.URL != "w5f:almanac/09-30/30a" {
		t.Errorf("part two: %v", err)
	}
	if _, err := Route(context.Background(), "w5f:almanac/09-30/../../x", env); err == nil {
		t.Error("only the site's own part pages are opened")
	}

	p := Packet{Date: "2026-09-30", Number: 3, Almanac: a, Oracle: &Oracle{Kind: "iching", Title: "Hexagram 1, Khien", Target: "https://x/ic01.htm", Detail: "no moving lines", Lines: []int{7, 7, 7, 7, 7, 7}}}
	text := flatText(coverDoc(p))
	for _, want := range []string{"On this day · 30 September", "The Book of Days (1864): William Hutton · A Contest for Precedence", "1791 — The Magic Flute premieres.", "(tr)", "The oracle", "I Ching · James Legge", "Hexagram 1, Khien — no moving lines"} {
		if !strings.Contains(text, want) {
			t.Errorf("cover lacks %q:\n%s", want, text)
		}
	}
	drawn, columns := false, false
	doc.ReplaceBlocks(coverDoc(p).Blocks, func(bl doc.Block) ([]doc.Block, bool) {
		if pre, ok := bl.(doc.Pre); ok && strings.Count(pre.Text, "━━━━━━━━━") == 6 {
			drawn = true
		}
		if _, ok := bl.(doc.Columns); ok {
			columns = true // the almanac and the oracle side by side
		}
		return nil, false
	})
	if !columns {
		t.Error("the cover's two columns are not side by side")
	}
	if !drawn {
		t.Error("the hexagram is drawn on the cover")
	}
}
