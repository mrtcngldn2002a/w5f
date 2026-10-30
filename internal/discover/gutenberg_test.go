package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// issueHTML is an old magazine's issue as Gutenberg sets it: contents that
// link to the stories' headings and to page numbers, a story of chapters, a
// serial's first part, a story too short to show, the readers' letters, and
// a footnote.
func issueHTML() string {
	para := func(n int, what string) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "<p>%s %d. %s</p>\n", what, i, goodText)
		}
		return b.String()
	}
	return `<html><head><title>The Project Gutenberg eBook of Astounding Stories, May 1930</title></head><body>
<h1>Astounding Stories</h1>
<h2>CONTENTS</h2>
<table>
<tr><td><a href="#Into_the_Depths">INTO THE OCEAN'S DEPTHS</a></td><td><a href="#Page_151">151</a></td></tr>
<tr><td><a href="#Murder_Madness">MURDER MADNESS</a></td><td><a href="#Page_166">166</a></td></tr>
<tr><td><a href="#Jest">The Jovian Jest</a></td><td><a href="#Page_228">228</a></td></tr>
<tr><td><a href="#Readers">THE READERS' CORNER</a></td></tr>
</table>
<h2><a id="Into_the_Depths"></a>Into the Ocean's Depths</h2>
<h3><i>By Sewell Peaslee Wright</i></h3>
<h4>CHAPTER I</h4>
<div class="figleft"><img alt="I" src="images/image_i.jpg"></div>
<p> read the telegram for the second time.<a id="Page_151"></a> Then I folded it.<a href="#Footnote_1">[1]</a></p>
` + para(20, "Depths") + `<h4>CHAPTER II</h4>
` + para(20, "Depths again") + `
<h2><a id="Murder_Madness"></a>Murder Madness</h2>
<h4>BEGINNING A FOUR-PART NOVEL</h4>
<h3><i>By Murray Leinster</i></h3>
<a id="Page_166"></a>` + para(30, "Madness") + `
<h2><a id="Jest"></a>The Jovian Jest</h2>
<p><a id="Page_228"></a>A short one.</p>
` + para(2, "Jest") + `
<h2 id="Readers">The Readers' Corner</h2>
` + para(30, "Letter") + `
<p><a id="Footnote_1"></a>A note.</p>
*** END OF THE PROJECT GUTENBERG EBOOK ***
<p>The licence.</p>
</body></html>`
}

func TestGutenbergPiecesAreTheWholeStoriesOfAnIssue(t *testing.T) {
	body := []byte(issueHTML())
	pieces := pgPieces(body)
	if len(pieces) != 1 {
		t.Fatalf("one whole story of a length to read, not a serial's part, the letters or a short one: %+v", pieces)
	}
	p := pieces[0]
	if p.id != "Into_the_Depths" || p.title != "Into the Ocean's Depths" {
		t.Errorf("the story and its title in title case: %+v", p)
	}
	text := string(body[p.from:p.to])
	if !strings.Contains(text, "CHAPTER II") || strings.Contains(text, "Murder Madness") {
		t.Errorf("the piece runs over its chapters to the next story: %.80q … %.80q", text, text[len(text)-80:])
	}
}

func TestGutenbergRouteOpensThePiece(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cache/epub/29809/pg29809-images.html" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, issueHTML())
	}))
	defer srv.Close()
	swap(t, &gutenbergSite, srv.URL)
	target := "w5f:discover/gutenberg?" + url.Values{"n": {"29809"}, "at": {"Into_the_Depths"}, "t": {"Into the Ocean's Depths"}}.Encode()
	if !IsTarget(target) {
		t.Fatal("a Gutenberg piece is a discovery address")
	}
	d, err := Route(context.Background(), target, Env{Fetcher: testFetcher()})
	if err != nil {
		t.Fatal(err)
	}
	all := plain(d)
	if d.Title != "Into the Ocean's Depths" || !strings.Contains(all, "I read the telegram") {
		t.Errorf("the piece, its first letter given back from the picture: %q\n%.300s", d.Title, all)
	}
	if strings.Contains(all, "Murder Madness") || strings.Contains(all, "The licence") {
		t.Errorf("only the piece: %.300s", all)
	}
	if !strings.Contains(all, "The whole book on Project Gutenberg") {
		t.Error("the piece links to its book")
	}
	if _, err := Route(context.Background(), strings.Replace(target, "Into_the_Depths", "Murder_Madness", 1), Env{Fetcher: testFetcher()}); err == nil {
		t.Error("a serial's part is not opened as a piece")
	}
}

func TestDropCapFromAPictureTitle(t *testing.T) {
	in := `<div class="figleft"><img alt="" src="o.jpg" title="O"></div>
<p><span class="smcap">wing</span> to the fact`
	if got := reDropCap.ReplaceAllString(in, "<p$2>$1"); got != `<div class="figleft"><p>O<span class="smcap">wing</span> to the fact` {
		t.Errorf("%q", got)
	}
}

func TestGutenbergListLoads(t *testing.T) {
	pgOnce.Do(loadPG)
	for _, k := range []byte("seia") {
		if len(pgBooks[k]) < 300 {
			t.Errorf("kind %c: %d books", k, len(pgBooks[k]))
		}
	}
	for _, b := range pgBooks['s'] {
		if b.words > 20000 {
			t.Fatalf("a single work is short: %+v", b)
		}
	}
}

func TestGutenbergLabels(t *testing.T) {
	b := pgBook{title: "The Time Machine", author: "Wells, H. G. (Herbert George)"}
	if got := b.label(); got != "The Time Machine — H. G. Wells" {
		t.Errorf("label: %q", got)
	}
	if got := (pgBook{title: "Astounding Stories, May 1930", author: "Various"}).label(); got != "Astounding Stories, May 1930" {
		t.Errorf("a magazine has no writer: %q", got)
	}
	if pgTitle("THE READERS' CORNER") != "The Readers' Corner" || pgTitle("Murder Madness") != "Murder Madness" {
		t.Error("contents in capitals are shown in title case")
	}
}

func TestWikisourceTitle(t *testing.T) {
	if got := wikisourceTitle("https://en.wikisource.org/wiki/Mosses_from_an_Old_Manse/The_Artist_of_the_Beautiful"); got != "The Artist of the Beautiful (Mosses from an Old Manse)" {
		t.Errorf("%q", got)
	}
	if got := wikisourceTitle("https://en.wikisource.org/wiki/The_Yellow_Wallpaper"); got != "The Yellow Wallpaper" {
		t.Errorf("%q", got)
	}
}

func TestStoryInPartsIsDrawnAgain(t *testing.T) {
	titles := []string{"Klokwerk's Heart (part 2 of 2)", "Joined Together"}
	i := 0
	pick := wholeTitled(func(context.Context, Env) (string, string, error) {
		i++
		return "https://x.example/" + fmt.Sprint(i), titles[i-1], nil
	})
	target, title, err := pick(context.Background(), Env{})
	if err != nil || title != "Joined Together" || target != "https://x.example/2" {
		t.Errorf("%q %q %v", target, title, err)
	}
}
