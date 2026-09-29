package source

import (
	"net/url"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func TestRefreshTarget(t *testing.T) {
	u, _ := url.Parse("https://wiby.me/surprise/")
	page := func(body string) *fetch.Response { return &fetch.Response{URL: u, Body: []byte(body)} }
	short := &doc.Document{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "You asked for it!"}}}}}
	next := refreshTarget(page(`<html><head><meta http-equiv="refresh" content="0; URL='http://old.example/home.html'"></head><body>You asked for it!</body></html>`), short)
	if next == nil || next.String() != "http://old.example/home.html" {
		t.Fatalf("refresh: %v", next)
	}
	if refreshTarget(page(`<meta http-equiv="refresh" content="600; url=/later">`), short) != nil {
		t.Error("a slow refresh (a news page reloading itself) is not followed")
	}
	if refreshTarget(page(`<meta http-equiv="refresh" content="0; url=javascript:alert(1)">`), short) != nil {
		t.Error("only web addresses are followed")
	}
	long := &doc.Document{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: string(make([]byte, 400))}}}}}
	if refreshTarget(page(`<meta http-equiv="refresh" content="0; url=/x">`), long) != nil {
		t.Error("a page with its own text stays")
	}
}
