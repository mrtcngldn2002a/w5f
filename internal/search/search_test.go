package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func TestWebParsesResultsAndSkipsAds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "gobekli tepe" {
			t.Errorf("query = %q", r.URL.Query().Get("q"))
		}
		fmt.Fprint(w, `<div class="result result--ad"><a class="result__a" href="//duckduckgo.com/y.js?ad=1">Buy now</a></div>
<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FG%25C3%25B6bekli_Tepe&amp;rut=x">Göbekli Tepe - Wikipedia</a>
<a class="result__snippet">A <b>Neolithic</b> site in Turkey.</a></div>`)
	}))
	defer srv.Close()
	WebEndpoint = srv.URL + "/html/"
	defer func() { WebEndpoint = "https://html.duckduckgo.com/html/" }()
	f := fetch.New("", "test")
	f.HostGap = 0
	d, err := Web(context.Background(), f, "gobekli tepe")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || d.Links[0].Href != "https://en.wikipedia.org/wiki/G%C3%B6bekli_Tepe" {
		t.Fatalf("links = %+v", d.Links)
	}
	l := d.Blocks[0].(doc.List)
	first := l.Items[0][0].(doc.Paragraph).Text.PlainText()
	snip := l.Items[0][1].(doc.Paragraph).Text.PlainText()
	if first != "Göbekli Tepe - Wikipedia" || !strings.Contains(snip, "Neolithic site in Turkey") {
		t.Errorf("result = %q / %q", first, snip)
	}
}
