package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ddg serves DuckDuckGo-shaped pages: page 1 by GET (with an advert, an
// other-site hit, a book page and a direct PDF), page 2 by the POST form.
func ddg(t *testing.T, host string, anomaly bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if anomaly {
			fmt.Fprint(w, `<html><body><div class="anomaly-modal__title">Unfortunately, bots use DuckDuckGo too.</div></body></html>`)
			return
		}
		r.ParseForm()
		if !strings.Contains(r.Form.Get("q"), "site:"+host) {
			t.Errorf("query = %q", r.Form.Get("q"))
		}
		res := func(href, title string) string {
			return `<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=` + href + `">` + title + `</a><a class="result__snippet">A classic.</a></div>`
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `<html><body>`+
				`<div class="result result--ad"><a class="result__a" href="https://ads.example/x">Ad</a></div>`+
				res("https%3A%2F%2Fother.example%2Fdracula", "Elsewhere")+
				res("https%3A%2F%2F"+host+"%2Fshowbook.php%3Fpid%3D1", "Dracula - Faded")+
				res("https%3A%2F%2Fwww."+host+"%2Flink.php%3Ffile%3D1-a5.pdf", "Dracula PDF")+
				`<form action="/html/" method="post"><input type="submit" value="Next"><input type="hidden" name="q" value="`+r.Form.Get("q")+`"><input type="hidden" name="s" value="10"></form></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><body>`+res("https%3A%2F%2F"+host+"%2Fshowbook.php%3Fpid%3D2", "Dracula's Guest")+`</body></html>`)
	}))
}

func TestEngineSearch(t *testing.T) {
	srv := ddg(t, "books.example", false)
	defer srv.Close()
	EngineEndpoint = srv.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	p := Profile{Search: SearchSpec{Kind: "engine", Template: "books.example"}}
	got, err := Search(context.Background(), testFetcher(), &p, "dracula", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 3 || got.Pages != 2 {
		t.Fatalf("results=%+v pages=%d", got.Results, got.Pages)
	}
	for _, r := range got.Results {
		if !strings.Contains(r.URL, "books.example") {
			t.Errorf("foreign result %+v", r)
		}
	}
	if d, ok := directDownload(got.Results[1].URL); !ok || d.Format != "pdf" {
		t.Errorf("file hit not downloadable: %+v", got.Results[1])
	}
	if got, _ := Search(context.Background(), testFetcher(), &p, "author:Bram Stoker", nil); !got.FieldIgnored {
		t.Error("the engine cannot search by field; that must be reported")
	}
}

func TestEngineAnomalyStops(t *testing.T) {
	srv := ddg(t, "books.example", true)
	defer srv.Close()
	EngineEndpoint = srv.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	p := Profile{Search: SearchSpec{Kind: "engine", Template: "books.example"}}
	if _, err := Search(context.Background(), testFetcher(), &p, "dracula", nil); !errors.Is(err, ErrEngineCheck) {
		t.Errorf("err = %v", err)
	}
}

func TestProbeFallsBackToEngine(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>JS Books</title></head><body><div id="app"></div><noscript>JS</noscript></body></html>`)
	}))
	defer site.Close()
	host := strings.TrimPrefix(site.URL, "http://")
	engine := ddg(t, strings.Split(host, ":")[0], false)
	defer engine.Close()
	EngineEndpoint = engine.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	rep, err := Probe(context.Background(), testFetcher(), site.URL, "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "engine" || rep.Profile.Search.MaxPages != 3 {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	last := rep.Findings[len(rep.Findings)-1].Text
	all := ""
	for _, f := range rep.Findings {
		all += f.Text + "\n"
	}
	if !strings.Contains(all, "DuckDuckGo") {
		t.Errorf("findings:\n%s (last %q)", all, last)
	}
}

// A searchable fallback beats a browse-only front page (spec: Faded Page
// is added through DuckDuckGo).
func TestEngineBeforeBrowseOnly(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><ul class="new">
<li><a href="/b/1">The Maltese Falcon</a> Hammett</li><li><a href="/b/2">Wise Blood</a> O'Connor</li>
<li><a href="/b/3">The Night Land</a> Hodgson</li><li><a href="/b/4">The Willows</a> Blackwood</li>
<li><a href="/b/5">Carmilla</a> Le Fanu</li></ul></body></html>`)
	}))
	defer site.Close()
	engine := ddg(t, "127.0.0.1", false)
	defer engine.Close()
	EngineEndpoint = engine.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	rep, err := Probe(context.Background(), testFetcher(), site.URL, "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "engine" {
		t.Fatalf("probe: %v kind=%q findings=%+v", err, rep.Profile.Search.Kind, rep.Findings)
	}
	// Without the engine the front-page list is still offered.
	EngineEndpoint = ""
	rep, _ = Probe(context.Background(), testFetcher(), site.URL, "dracula")
	if !rep.CanAdd || rep.Profile.Search.Kind != "browse" {
		t.Errorf("browse fallback: kind=%q", rep.Profile.Search.Kind)
	}
}
