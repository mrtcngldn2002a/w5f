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

// Britannica's On This Day as it serves it (2026-10-02): a year box before
// each headline, text or birthday name, and the featured event in a card.
const britannicaPage = `<html><body><main>
<div class="tw:flex"><div class="tw:basis-1/2">
  <div class="tw:mb-2 font-oswald tw:text-lg">1791</div>
  <div class="font-lora tw:text-3xl">The Magic Flute premieres</div>
  <div class="font-newsreader">On this day in 1791, <a href="https://www.britannica.com/topic/The-Magic-Flute">The Magic Flute</a> was first performed in Vienna.</div>
  <a href="/today-in-history/September-30-1791-Magic-Flute">READ&nbsp;MORE</a>
</div></div>
<div class="tw:basis-1/3"><div class="font-oswald tw:uppercase!">Featured Event</div>
<div class="day-in-history-card tw:flex"><div class="tw:w-89"><img src="x.jpg"></div>
  <div class="tw:text-xs tw:italic">© A photographer</div>
  <div class="font-lora">The Munich Agreement</div>
  <div class="font-newsreader">Britain and France agreed to the Nazi annexation this day in 1938.</div></div></div>
<div><div class="font-oswald tw:uppercase">Famous Birthdays</div>
  <div class="tw:flex"><div class="tw:mb-2 font-oswald tw:text-sm">1207</div><a href="https://www.britannica.com/biography/Rumi">Rumi</a><div class="font-lora">Persian poet</div></div></div>
<h2>More on September&nbsp;30</h2>
<div><div class="tw:text-xs tw:italic">Credit</div>
  <div class="tw:flex"><div class="font-oswald tw:text-sm">1955</div><div class="font-newsreader">American actor <a href="https://www.britannica.com/biography/James-Dean">James Dean</a> died in a car crash. <em>[<a href="/quiz/x">Take our quiz</a>.]</em></div></div></div>
</main></body></html>`

// Today in Science History as it serves it: section anchors, a name
// heading per person or event, and the text in an indented box.
const sciencePage = `<html><body>
<div class="daysubheading"><a NAME="birth"></a>SEPTEMBER 30 – BIRTHS</div>
<div class="daynameheading"><a NAME="GeigerHans"></a>&nbsp; Hans Geiger</div><div><div class="dayleftcell100"><img src="g.jpg"></div>
<div style="margin-left:100px;padding:3px;"><span class="sprite icon-baby"></span> &nbsp;Born 30 Sep 1882; died 24 Sep 1945 at age 62. <span class="footnote">quotes</span><br>German physicist who introduced the Geiger counter.<div class="bookline"></div></div></div>
<div class="daysubheading"><a NAME="death"></a>SEPTEMBER 30 – DEATHS</div>
<div class="daynameheading"><a NAME="X"></a>&nbsp; A Chemist</div><div><div style="margin-left:100px;padding:3px;">Died 30 Sep 1950 at age 70 (born 1 Jan 1880).<br>Chemist of note.</div></div>
<div class="daysubheading"><a NAME="event"></a>SEPTEMBER 30 – EVENTS</div>
<div class="daynameheading"><a NAME="{09-30-1954}"></a>&nbsp; Nautilus</div><div><div style="margin-left:100px;padding:3px;">&nbsp;
In 1954, the first nuclear submarine was commissioned.<div class="bookline"></div></div></div>
</body></html>`

func TestAlmanacColumnAndPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/months/sept/30.htm":
			fmt.Fprint(w, bookOfDaysPage)
		case "/months/sept/30a.htm":
			fmt.Fprint(w, `<html><body><p><b>THE SECOND PART</b></p><p>More of the day.</p></body></html>`)
		case "/otd/September-30":
			fmt.Fprint(w, britannicaPage)
		case "/tis/9/9_30.htm":
			fmt.Fprint(w, sciencePage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	swap(t, &bookOfDaysBase, srv.URL+"/months/")
	swap(t, &britannicaOTD, srv.URL+"/otd/%s-%d")
	swap(t, &todayInSci, srv.URL+"/tis/%d/%d_%02d.htm")
	a, err := buildAlmanac(context.Background(), testFetcher(), time.September, 30)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.Headlines, "|") != "William Hutton|A Contest for Precedence" || !strings.HasPrefix(a.Born, "Euripides") || a.Day != "09-30" {
		t.Errorf("almanac: %+v", a)
	}
	var events []string
	for _, e := range a.Events {
		events = append(events, fmt.Sprintf("%d %s|%s", e.Year, e.Title, e.Text))
	}
	if want := []string{"1791 The Magic Flute premieres|On this day in 1791, The Magic Flute was first performed in Vienna.",
		"1938 The Munich Agreement|Britain and France agreed to the Nazi annexation this day in 1938.",
		"1955 |American actor James Dean died in a car crash."}; strings.Join(events, "\n") != strings.Join(want, "\n") {
		t.Errorf("Britannica's events in year order, the quiz aside left out:\n%s", strings.Join(events, "\n"))
	}
	if !strings.HasSuffix(a.Events[0].Target, "/today-in-history/September-30-1791-Magic-Flute") || len(a.Birthdays) != 1 || a.Birthdays[0].Title != "Rumi" || a.Birthdays[0].Year != 1207 {
		t.Errorf("story link and birthdays: %+v %+v", a.Events[0], a.Birthdays)
	}
	var science []string
	for _, s := range a.Science {
		science = append(science, fmt.Sprintf("%s %d %s: %s", s.Kind, s.Year, s.Title, s.Text))
	}
	if want := []string{"born 1882 Hans Geiger: German physicist who introduced the Geiger counter.", "died 1950 A Chemist: Chemist of note.",
		"event 1954 Nautilus: In 1954, the first nuclear submarine was commissioned."}; strings.Join(science, "\n") != strings.Join(want, "\n") {
		t.Errorf("science:\n%s", strings.Join(science, "\n"))
	}
	if !strings.HasSuffix(a.Science[0].Target, "/tis/9/9_30.htm#GeigerHans") {
		t.Errorf("science link: %s", a.Science[0].Target)
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
	for _, want := range []string{"On this day · 30 September", "The Book of Days (1864): William Hutton · A Contest for Precedence",
		"Britannica, On This Day", "1791 — The Magic Flute premieres", "1955 — American actor James Dean died", "Birthdays: Rumi, Persian poet (1207)",
		"Today in Science History", "born 1882 — Hans Geiger: German physicist", "1954 — Nautilus: In 1954",
		"The oracle", "I Ching · James Legge", "Hexagram 1, Khien — no moving lines"} {
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
