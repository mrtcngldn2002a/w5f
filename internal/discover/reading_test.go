package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
)

func TestOpeningTellsAStoryStart(t *testing.T) {
	for _, p := range []string{
		"THE sentry guarding my quarters called my attention to the great clouds of smoke on the horizon.",
		"A STRANGE thing happened to the old man when he came down the hill that night in the rain.",
		"HUNGER, weariness and confusion had brought Sin close to tears, and John Henry took her chin.",
	} {
		if !opening(p) {
			t.Errorf("an opening: %q", p)
		}
	}
	for _, p := range []string{
		"The sentry guarding my quarters called my attention to the great clouds of smoke on the horizon.",
		"“NO!” he cried, and the sentry guarding my quarters called my attention to the great clouds.",
		"THE WEIRD MONTHLY FOR APRIL",
		"HOW LIKE A GOD, by REX STOUT, is one of next issue's headliners and one of the finest novels.",
		"FREE BOOK TELLS HOW TO EARN BIG MONEY AT HOME IN YOUR SPARE TIME WITH NO RISK AT ALL",
	} {
		if opening(p) {
			t.Errorf("not an opening: %q", p)
		}
	}
}

// storyText is a magazine's text: scenes of plain paragraphs, each begun the
// way a magazine sets a story or a chapter going.
func storyText(scenes, each int) string {
	var b strings.Builder
	for s := 0; s < scenes; s++ {
		for i := 0; i < each; i++ {
			if i == 0 {
				fmt.Fprintf(&b, "THE old naturalist of scene %d walked down to the shore every morning and wrote what he saw in a small book, for he held that nothing seen once is ever seen twice.\n\n", s)
				continue
			}
			fmt.Fprintf(&b, "%s Scene %d, entry %d records the tide, the weather and the birds that came with them.\n\n", goodText, s, i)
		}
	}
	return b.String()
}

func TestWindowStartsAtAnOpeningAndReadsToTheNext(t *testing.T) {
	paras := paragraphs(storyText(12, 30))
	for seed := int64(1); seed < 20; seed++ {
		win, from := pickWindow(paras, seed)
		if len(win) == 0 {
			t.Fatal("no window")
		}
		if !opening(win[0]) {
			t.Fatalf("seed %d: the window starts mid-scene: %.60q", seed, win[0])
		}
		end := from + len(win)
		if end < len(paras) && !opening(paras[end]) {
			t.Errorf("seed %d: the window stops mid-scene, before %.60q", seed, paras[end])
		}
		for _, p := range win[1:] {
			if opening(p) {
				t.Errorf("seed %d: the window runs into the next scene", seed)
			}
		}
	}
}

func TestWindowReadsOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, storyText(12, 30))
	}))
	defer srv.Close()
	target := windowTarget(srv.URL+"/x/x_djvu.txt", "Weird Tales")
	env := Env{Fetcher: testFetcher()}
	d, err := Route(context.Background(), target, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.Byline, "from the start of a story or chapter") || d.Next == "" {
		t.Fatalf("byline %q, next %q", d.Byline, d.Next)
	}
	last := d.Blocks[len(d.Blocks)-2].(doc.Paragraph).Text[0].Text
	next, err := Route(context.Background(), d.Next, env)
	if err != nil {
		t.Fatal(err)
	}
	first := next.Blocks[0].(doc.Paragraph).Text[0].Text
	if !strings.HasPrefix(next.Byline, "read on") || !opening(first) || first == last {
		t.Errorf("read on: %q, starts %.60q after %.60q", next.Byline, first, last)
	}
	for _, page := range []*doc.Document{d, next} {
		foot := page.Blocks[len(page.Blocks)-1].(doc.Paragraph).Text
		if foot[0].Text != "The whole text" || (page.Next != "") != (foot[len(foot)-1].Text == "Read on ›") {
			t.Errorf("foot %+v, next %q", foot, page.Next)
		}
	}
}

func TestBHLPassageDropsGarbledParagraphs(t *testing.T) {
	garbled := "(liftbroiit (liHtrictH, but will just ^ivo ytni ud illimtmtioii of what Iioh been (loiip hy tlio ftottlora Hiiioo, mid wliat in hoin^ doiio iit tlio proHont."
	page := goodText + " " + goodText + "\n\n" + garbled + "\n\n" + strings.Repeat(goodText+" ", 12) + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }))
	defer srv.Close()
	swap(t, &bhlBucket, srv.URL)
	paras, _, err := bhlPassage(context.Background(), testFetcher(), []bhlLeaf{{seq: 1, key: "a.txt"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paras {
		if strings.Contains(p, "liftbroiit") {
			t.Errorf("the garbled paragraph is kept: %q", paras)
		}
	}
}

func TestWikipediaArticleSkipsListsAndStubs(t *testing.T) {
	article := "<html><body><div id=mw-content-text><p>" + strings.Repeat(goodText+" ", 20) + "</p></div></body></html>"
	stub := "<html><body><div id=mw-content-text><p>A village in the north.</p></div></body></html>"
	pages := []string{"/wiki/List_of_villages", "/wiki/Small_village", "/wiki/Old_naturalist"}
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wiki/Special:Random":
			http.Redirect(w, r, pages[min(n, len(pages)-1)], http.StatusFound)
			n++
		case "/wiki/Old_naturalist":
			fmt.Fprint(w, article)
		default:
			fmt.Fprint(w, stub)
		}
	}))
	defer srv.Close()
	got, err := wikipediaArticle(context.Background(), testFetcher(), srv.URL+"/wiki/Special:Random")
	if err != nil || !strings.HasSuffix(got, "/wiki/Old_naturalist") {
		t.Errorf("drawn %q %v", got, err)
	}
	u, _ := url.Parse(srv.URL + "/wiki/List_of_birds")
	if wikiWorthReading(u, []byte(article)) {
		t.Error("a list is not an article to read")
	}
}

func TestDeepRandomPassesOverThinPages(t *testing.T) {
	env := testEnv(t, fakeFamily{name: "smallweb"}, fakeFamily{name: "folklore"}, fakeFamily{name: "esoteric"})
	load := env.Load
	env.Load = func(ctx context.Context, target string) (*doc.Document, error) {
		if strings.Contains(target, "smallweb") {
			return &doc.Document{Title: "Spore shots", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "[image] Oooh, chills!"}}}}}, nil
		}
		return load(ctx, target)
	}
	for range 4 {
		d, _, err := Next(context.Background(), env)
		if err != nil || d.Family == "smallweb" {
			t.Fatalf("drawn %+v %v", d, err)
		}
	}
}

func TestTidyPagesDropsATitleCaseRunningHead(t *testing.T) {
	pages := []string{
		goodText + "\nThe forest went on for days, and the path\n",
		"In the Heart of Africa\nwhich recalls the colonnades of a beech forest or of a forest dome. " + goodText + "\n",
		"the river which flows from the\nmountains to the sea is wide. " + goodText + "\n",
	}
	got := strings.Join(tidyPages(pages), " | ")
	if strings.Contains(got, "Heart of Africa") || !strings.Contains(got, "and the path which recalls the colonnades") {
		t.Errorf("the running head stays or breaks the sentence: %q", got)
	}
	if !strings.Contains(got, "the river which flows from the mountains") {
		t.Errorf("a line of the text is not a running head: %q", got)
	}
}

func TestDropCapMended(t *testing.T) {
	for in, want := range map[string]string{
		"HEY went down to the river at dawn.":        "THEY went down to the river at dawn.",
		"ITH a cry the gulls rose.":                  "ITH a cry the gulls rose.", // "ith" is in the list: left
		"HE ship came down in the dark.":             "THE ship came down in the dark.",
		"HE international exchange has brought him.": "THE international exchange has brought him.",
		"HE was tired of the long road.":             "HE was tired of the long road.",
		"HE suddenly turned to the door.":            "HE suddenly turned to the door.",
		"HE knew the road well enough.":              "HE knew the road well enough.",
		"HE swore under his breath.":                 "HE swore under his breath.",
		"HIS is a believe-it-or-not story.":          "THIS is a believe-it-or-not story.",
		"HIS hand was cold on the latch.":            "HIS hand was cold on the latch.",
		"HERE at the edge of the map he lay.":        "HERE at the edge of the map he lay.",
		"NO light was left.":                         "NO light was left.",
		"OR ME, discovering it was a joy.":           "FOR ME, discovering it was a joy.",
		"HEY! You there.":                            "HEY! You there.",
	} {
		if got := mendDropCap(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestBylineMarksAStoryStart(t *testing.T) {
	text := "THE TRIAL\nby Brian Stableford\n\n" + goodText + " " + goodText + "\n\n" + goodText + " " + goodText + "\n\n" +
		"Asimov's SF, July 2007\nby Dell Magazine Authors\n\n" + goodText + " " + goodText + "\n"
	paras := paragraphs(text)
	if len(paras) != 3 || !opening(paras[0]) || opening(paras[1]) || opening(paras[2]) {
		t.Fatalf("the paragraph after a title and byline opens a story, no other: %q", paras)
	}
	if shown(paras[0]) != goodText+" "+goodText {
		t.Errorf("the mark is not shown: %q", shown(paras[0]))
	}
}

// logXML is the DjVu XML of a log of numbered entries, one column a page,
// every entry of eleven words: a page ends in the middle of one.
func logXML(pages int) (string, []string) {
	names := strings.Fields("tide bird boat stone wind rain shell fish cloud sand reed crab gull wave moon star")
	var entries, words []string
	for i := 0; len(words) < pages*320; i++ {
		e := fmt.Sprintf("The log of the %s and the %s tells the weather.", names[i%16], names[i/16%16])
		entries = append(entries, e)
		words = append(words, strings.Fields(e)...)
	}
	var xml strings.Builder
	xml.WriteString("<?xml version=\"1.0\"?>\n<DjVuXML><BODY>\n")
	for p := 0; p < pages; p++ {
		var page []lword
		for i := 0; i < 40; i++ {
			page = append(page, columnLine(100, 200+i*48, words[p*320+i*8:p*320+i*8+8])...)
		}
		xml.WriteString(xmlPage(page))
	}
	xml.WriteString("</BODY></DjVuXML>\n")
	return xml.String(), entries
}

func TestReadOnAcrossPagesLosesNoSentence(t *testing.T) {
	body, entries := logXML(12)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x_djvu.xml", time.Time{}, strings.NewReader(body))
	}))
	defer srv.Close()
	env := Env{Fetcher: testFetcher()}
	target := windowPagesTarget(srv.URL+"/download/x/x_djvu.xml", "The log", 0, len(body)/4, len(body))
	var read strings.Builder
	ranges := map[string]bool{}
	for hop := 0; hop < 30 && target != "" && len(ranges) < 3; hop++ {
		d, err := Route(context.Background(), target, env)
		if err != nil {
			t.Fatalf("hop %d: %v", hop, err)
		}
		q, _ := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/window?"))
		ranges[q.Get("r")] = true
		for _, b := range d.Blocks[:len(d.Blocks)-1] {
			read.WriteString(b.(doc.Paragraph).Text[0].Text + " ")
		}
		target = d.Next
	}
	if len(ranges) < 3 {
		t.Fatalf("read on into the pages after: %v", ranges)
	}
	text := read.String()
	first := -1
	for i, e := range entries {
		if strings.Contains(text, e) {
			first = i
			break
		}
	}
	last, at := first, 0
	for i := first; i < len(entries); i++ {
		j := strings.Index(text[at:], entries[i])
		if j < 0 {
			break
		}
		at += j + len(entries[i])
		last = i
	}
	if rest := strings.TrimSpace(text[at:]); first < 0 || rest != "" {
		t.Errorf("entries %d to %d read in order, then %.120q", first, last, rest)
	}
}
