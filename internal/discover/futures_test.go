package discover

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
)

// story is a few hundred words of "text", so that a page passes for a whole
// story; sentence i of paragraph p is the same everywhere.
func storyParas(n int) []string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("Paragraph %d of the story. %s", i+1, strings.Repeat("The lamps went out over the quiet town. ", 12)))
	}
	return out
}

func p(tag, class string, text string) string {
	if class != "" {
		return fmt.Sprintf(`<%s class=%q>%s</%s>`, tag, class, text, tag)
	}
	return fmt.Sprintf("<%s>%s</%s>", tag, text, tag)
}

const metaHead = `<head><title>Chains : Nature</title>
<meta name="citation_title" content="The chains of plenty" />
<meta name="citation_author" content="S. R. Algernon" />
<meta name="citation_online_date" content="2014-12-17" />
<meta name="description" content="All you want for Christmas." /></head>`

// The layout of 2018 on.
func pageCurrent(paras []string) string {
	var b strings.Builder
	b.WriteString(`<html>` + metaHead + `<body><div class="c-article-body">
<div class="c-article-section__content--standfirst"><p>All you want for Christmas.</p></div>
<div class="main-content"><div class="c-article-section__content">`)
	b.WriteString(`<div class="c-article-section__illustration"><div class="c-article-section__figure-1-border-caption"><span><p>Credit: JACEY</p></span></div></div>`)
	for _, x := range paras {
		b.WriteString(p("p", "", x))
	}
	b.WriteString(`</div></div>
<div class="c-article-section"><p>S. R. Algernon studied fiction writing.</p><p>Follow Futures on Twitter.</p></div></div></body></html>`)
	return b.String()
}

// The layout of 2015–2017.
func pageArticle(paras []string) string {
	var b strings.Builder
	b.WriteString(`<html>` + metaHead + `<body><article><header><p class="article-type"><span>Nature</span> | Futures</p>
<p class="article-type"><span>Nature</span> | Corrigendum</p></header>
<section><div class="section first no-nav no-title"><div class="content"><p>Not the story.</p></div></div>
<div class="section second no-nav no-title"><div class="content">
<div class="illustration"><div class="illustration-sleeve"><p class="credit">Illustration by Jacey</p></div></div>`)
	for _, x := range paras {
		b.WriteString(p("p", "", x))
	}
	b.WriteString(`</div></div><div id="author-information" class="section"><div class="content"><p>Not the story either.</p></div></div></section></article></body></html>`)
	return b.String()
}

// The layout before that.
func pageOld(paras []string) string {
	var b strings.Builder
	b.WriteString(`<html>` + metaHead + `<body><div class="articleCol"><h2 id="atl">The chains of plenty</h2><p id="aug">rpg</p>
<div id="abs"><p class="lead">All you want for Christmas.</p></div><div id="articlebody">
<div id="illus1" class="image-full"><p>JACEY</p></div>`)
	for _, x := range paras {
		b.WriteString(p("p", "norm", x))
	}
	b.WriteString(`</div><div class="archive"><p>Not the story.</p></div></div></body></html>`)
	return b.String()
}

func parse(t *testing.T, page string) *futuresStory {
	t.Helper()
	gq, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return parseFuturesPage(gq)
}

func TestFuturesPageLayouts(t *testing.T) {
	paras := storyParas(8)
	for name, page := range map[string]string{
		"current": pageCurrent(paras),
		"article": pageArticle(paras),
		"old":     pageOld(paras),
	} {
		s := parse(t, page)
		if len(s.paras) != len(paras) || s.paras[0] != strings.Join(strings.Fields(paras[0]), " ") {
			t.Errorf("%s: got %d paragraphs %q, want %d", name, len(s.paras), s.paras, len(paras))
		}
		for _, x := range s.paras {
			for _, bad := range []string{"Credit", "Illustration", "JACEY", "Not the story", "Follow Futures", "studied fiction", "Corrigendum"} {
				if strings.Contains(x, bad) {
					t.Errorf("%s: %q is not part of the story: %q", name, bad, x)
				}
			}
		}
		if s.title != "The chains of plenty" || s.author != "S. R. Algernon" || s.standfirst != "All you want for Christmas." || s.date != "2014-12-17" {
			t.Errorf("%s: %q by %q (%q) %q", name, s.title, s.author, s.standfirst, s.date)
		}
	}
}

// The standfirst is in the page when the metadata has none (the layout before 2015).
func TestFuturesStandfirstFromThePage(t *testing.T) {
	page := strings.Replace(pageOld(storyParas(8)), `<meta name="description" content="All you want for Christmas." />`, "", 1)
	if s := parse(t, page); s.standfirst != "All you want for Christmas." {
		t.Errorf("got %q", s.standfirst)
	}
}

// wayback stands in for the Internet Archive: a timemap and the captures.
type wayback struct {
	legacy, current []string          // captures' timestamps, oldest first
	pages           map[string]string // timestamp -> page
	down            bool
	asked           []string
}

func (w *wayback) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.asked = append(w.asked, r.URL.Path)
		if w.down {
			fmt.Fprint(rw, `<html><head><title>Internet Archive: Temporarily Offline</title></head></html>`)
			return
		}
		switch {
		case r.URL.Path == "/web/timemap/json":
			u := r.URL.Query().Get("url")
			var stamps []string
			orig := "http://www.nature.com/articles/516444a"
			if strings.Contains(u, "journal") {
				stamps = w.legacy
				orig = "http://www.nature.com:80/nature/journal/v516/n7531/full/516444a.html"
			} else {
				stamps = w.current
			}
			if len(stamps) == 0 {
				return // no captures: an empty answer
			}
			rows := []string{`["timestamp","original"]`}
			for _, s := range stamps {
				rows = append(rows, fmt.Sprintf("[%q,%q]", s, orig))
			}
			fmt.Fprint(rw, "["+strings.Join(rows, ",\n")+"]")
		case strings.HasPrefix(r.URL.Path, "/web/"):
			rest := strings.TrimPrefix(r.URL.Path, "/web/")
			stamp, _, _ := strings.Cut(rest, "id_/")
			if page, ok := w.pages[stamp]; ok {
				rw.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(rw, page)
				return
			}
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(srv.Close)
	swap(t, &waybackBase, srv.URL)
	return srv
}

func TestFuturesTakesTheOldAddressFirst(t *testing.T) {
	w := &wayback{legacy: []string{"20160101000000", "20170101000000"}, current: []string{"20240101000000"},
		pages: map[string]string{"20170101000000": pageArticle(storyParas(8)), "20240101000000": pageCurrent(storyParas(8))}}
	w.serve(t)
	s, err := futuresStoryFor(context.Background(), testFetcher(), "516444a")
	if err != nil || s.from.stamp != "20170101000000" || len(s.paras) != 8 {
		t.Fatalf("got %+v %v", s, err)
	}
}

// The newest capture of the present address is often a paywalled preview or
// an empty page: the ones from before 2025 come first, and a preview is no story.
func TestFuturesSkipsPreviewsAndLateCaptures(t *testing.T) {
	w := &wayback{current: []string{"20230101000000", "20240101000000", "20250417000000"},
		pages: map[string]string{
			"20250417000000": pageCurrent(storyParas(1)), // a preview
			"20240101000000": pageCurrent(storyParas(8)),
		}}
	w.serve(t)
	s, err := futuresStoryFor(context.Background(), testFetcher(), "35012720")
	if err != nil || s.from.stamp != "20240101000000" {
		t.Fatalf("got %+v %v", s, err)
	}
	for _, a := range w.asked {
		if strings.Contains(a, "20250417") {
			t.Errorf("the preview was fetched before a good capture: %v", w.asked)
		}
	}
}

func TestFuturesAfterCapturesThatAreAllPreviews(t *testing.T) {
	w := &wayback{current: []string{"20250417000000"}, pages: map[string]string{"20250417000000": pageCurrent(storyParas(1))}}
	w.serve(t)
	_, err := futuresStoryFor(context.Background(), testFetcher(), "516444a")
	if !errors.Is(err, errNoCopy) || errors.Is(err, errArchiveDown) {
		t.Errorf("got %v", err)
	}
}

func TestFuturesWithoutCapturesIsNotTheArchiveDown(t *testing.T) {
	(&wayback{}).serve(t)
	_, err := futuresStoryFor(context.Background(), testFetcher(), "516444a")
	if !errors.Is(err, errNoCopy) || errors.Is(err, errArchiveDown) {
		t.Errorf("got %v", err)
	}
}

func TestFuturesWhenTheArchiveIsDown(t *testing.T) {
	(&wayback{down: true}).serve(t)
	_, err := futuresStoryFor(context.Background(), testFetcher(), "516444a")
	if !errors.Is(err, errArchiveDown) {
		t.Errorf("an offline page was taken for \"no capture\": %v", err)
	}
}

func TestFuturesRejectsStrangeIDs(t *testing.T) {
	for _, id := range []string{"", "../x", "d41586-025-03600-9?x", "5164 44a"} {
		if _, err := futuresStoryFor(context.Background(), testFetcher(), id); err == nil || strings.Contains(err.Error(), "Archive") {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestFuturesRouteBuildsThePage(t *testing.T) {
	w := &wayback{legacy: []string{"20170101000000"}, pages: map[string]string{"20170101000000": pageArticle(storyParas(8))}}
	w.serve(t)
	target := "w5f:discover/futures?" + url.Values{"id": {"516444a"}, "t": {"Chains"}}.Encode()
	d, err := Route(context.Background(), target, Env{Fetcher: testFetcher()})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "The chains of plenty" || d.Origin != "archive" || d.Byline != "S. R. Algernon · Nature Futures · 2014-12-17" {
		t.Errorf("page: %q %q %q", d.Title, d.Origin, d.Byline)
	}
	if len(d.Links) != 2 || !strings.Contains(d.Links[0].Href, "/web/20170101000000/") || d.Links[1].Href != "https://www.nature.com/articles/516444a" {
		t.Errorf("links: %+v", d.Links)
	}
	// standfirst, eight paragraphs, the links line
	if len(d.Blocks) != 10 {
		t.Fatalf("%d blocks", len(d.Blocks))
	}
	if first := d.Blocks[0].(doc.Paragraph).Text; first[0].Style != doc.Italic || first.PlainText() != "All you want for Christmas." {
		t.Errorf("standfirst: %+v", first)
	}
}
