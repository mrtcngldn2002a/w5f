package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
		"“HEY went down to the river.":               "“THEY went down to the river.",
		"“TI crept up to their guard, killed him.":   "“I crept up to their guard, killed him.",
		"“WHAT do you want?” he asked.":              "“WHAT do you want?” he asked.",
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
