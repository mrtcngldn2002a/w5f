package feeds

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

func pageText(d *doc.Document) string {
	var b strings.Builder
	var walk func([]doc.Block)
	walk = func(bs []doc.Block) {
		for _, bl := range bs {
			switch x := bl.(type) {
			case doc.Paragraph:
				for _, s := range x.Text {
					b.WriteString(s.Text)
				}
				b.WriteString("\n")
			case doc.Notice:
				b.WriteString(x.Text + "\n")
			case doc.List:
				for _, it := range x.Items {
					walk(it)
				}
			case doc.Columns:
				for _, c := range x.Cols {
					walk(c)
				}
			}
		}
	}
	walk(d.Blocks)
	return b.String()
}

// A site's feed is followed from inside W5F: found from its page, put on a
// shelf of the owner's choosing, synced at once, and taken off again.
func TestFollowASiteAndStop(t *testing.T) {
	dataDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.xml" {
			fmt.Fprint(w, rss(item("a1", "First letter", time.Now().Format(time.RFC1123Z), "<p>hello</p>")))
			return
		}
		fmt.Fprint(w, `<html><head><title>Letters</title><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head><body><p>Letters.</p></body></html>`)
	}))
	defer srv.Close()
	e := env(t, srv)
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	e.Catalog = c
	ctx := context.Background()
	route := func(target string) *doc.Document {
		t.Helper()
		d, err := Route(ctx, target, e)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if c, err := LoadCatalog(); err == nil {
			e.Catalog = c
		}
		return d
	}
	check := route("w5f:feeds/add?" + url.Values{"url": {srv.URL + "/"}}.Encode())
	if txt := pageText(check); !strings.Contains(txt, "Test Feed") || !strings.Contains(txt, "First letter") || !strings.Contains(txt, "Follow it on Your feeds") {
		t.Fatalf("check page:\n%s", txt)
	}
	var follow string
	for _, l := range check.Links {
		if l.Text == "follow" {
			follow = l.Href
		}
	}
	shelf := route(follow)
	if txt := pageText(shelf); !strings.Contains(txt, "Now following Test Feed: 1 items") {
		t.Fatalf("after following:\n%s", txt)
	}
	fd := e.Catalog.Feed("test-feed")
	if fd == nil || fd.Shelf != "yours" || fd.URL[0] != srv.URL+"/feed.xml" || fd.Site != srv.URL+"/" || e.Catalog.Shelf("yours") == nil {
		t.Fatalf("feed: %+v", fd)
	}
	if got := UserFeeds(e.Catalog); len(got) != 1 || got[0].ID != "test-feed" {
		t.Errorf("your feeds: %+v", got)
	}
	if txt := pageText(route("w5f:feeds/add?" + url.Values{"url": {srv.URL + "/"}}.Encode())); !strings.Contains(txt, "Already followed: Test Feed, on the shelf Your feeds") {
		t.Errorf("again:\n%s", txt)
	}
	if txt := pageText(route("w5f:feeds/remove?id=test-feed")); !strings.Contains(txt, "Yes, stop following it") {
		t.Errorf("removing asks first:\n%s", txt)
	}
	route("w5f:feeds/remove?id=test-feed&sure=yes")
	if e.Catalog.Feed("test-feed") != nil || len(UserFeeds(e.Catalog)) != 0 {
		t.Fatal("still followed")
	}
	route(follow)
	if fd := e.Catalog.Feed("test-feed-2"); fd == nil {
		t.Errorf("followed again under a new id: %+v", e.Catalog.Feeds[len(e.Catalog.Feeds)-1])
	}
}
