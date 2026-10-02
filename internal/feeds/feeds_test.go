package feeds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

func rss(items ...string) string {
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>Test Feed</title>` + strings.Join(items, "") + `</channel></rss>`
}

func item(guid, title, date, desc string) string {
	return fmt.Sprintf(`<item><guid>%s</guid><title>%s</title><link>%s</link><pubDate>%s</pubDate><description><![CDATA[%s]]></description></item>`,
		guid, title, "http://example.invalid/"+guid, date, desc)
}

func env(t *testing.T, srv *httptest.Server, feeds ...Feed) Env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := fetch.New("", "test")
	f.HostGap = 0
	return Env{Fetcher: f, DB: db, Catalog: &Catalog{Shelves: []Shelf{{ID: "s", Label: "Shelf"}}, Feeds: feeds}}
}

func TestSyncFallbackURLAndFirstSyncRules(t *testing.T) {
	recent := time.Now().Add(-2 * time.Hour).Format(time.RFC1123Z)
	old := time.Now().AddDate(0, -3, 0).Format(time.RFC1123Z)
	var body strings.Builder
	body.WriteString(item("new1", "Fresh story", recent, "<p>short excerpt</p>"))
	body.WriteString(item("old1", "Ancient story", old, "<p>old</p>"))
	for i := 0; i < 150; i++ {
		body.WriteString(item(fmt.Sprintf("bulk%d", i), "bulk", old, "x"))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, rss(body.String()))
	}))
	defer srv.Close()
	e := env(t, srv, Feed{ID: "f", Name: "Test", Shelf: "s", URL: []string{srv.URL + "/broken", srv.URL + "/feed"}})
	rep := Sync(context.Background(), e.Fetcher, e.DB, e.Catalog, nil, nil)
	if len(rep.Failed()) != 0 || rep.Results[0].Found != srv.URL+"/feed" {
		t.Fatalf("fallback url not used: %+v", rep.Results)
	}
	if rep.New() != maxPerSync {
		t.Errorf("new = %d, want cap %d", rep.New(), maxPerSync)
	}
	unread, _ := e.DB.Items(store.Query{Unread: true})
	if len(unread) != 1 || unread[0].Title != "Fresh story" {
		t.Errorf("first sync should leave only recent items unread, got %d", len(unread))
	}
	// Second sync: nothing new, stored working URL tried first.
	rep = Sync(context.Background(), e.Fetcher, e.DB, e.Catalog, nil, nil)
	if rep.New() != 0 {
		t.Errorf("resync new = %d", rep.New())
	}
}

// A feed taken off the catalog keeps its items in the database, out of the
// unread lists.
func TestRemovedFeedIsOutOfSight(t *testing.T) {
	e := env(t, nil, Feed{ID: "kept", Name: "Kept", Shelf: "s"})
	now := time.Now()
	for _, id := range []string{"kept", "gone"} {
		if _, err := e.DB.UpsertItem(store.Item{FeedID: id, GUID: id, URL: "http://example.invalid/" + id, Title: id, Published: now}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Route(context.Background(), "w5f:feeds/unread", e)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, l := range d.Links {
		titles = append(titles, l.Text)
	}
	if got := strings.Join(titles, " "); !strings.Contains(got, "kept") || strings.Contains(got, "gone") {
		t.Errorf("unread list: %s", got)
	}
	if all, _ := e.DB.Items(store.Query{}); len(all) != 2 {
		t.Errorf("the removed feed's item was deleted: %d left", len(all))
	}
}

// A DergiPark item opens the article's full-text PDF, not its abstract page.
func TestDergiParkItemOpensThePDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><p>Öz: kısa.</p><a href="/tr/download/article-file/42">Tam Metin</a></body></html>`)
	}))
	defer srv.Close()
	old := dergiparkHost
	dergiparkHost = strings.TrimPrefix(srv.URL, "http://")
	defer func() { dergiparkHost = old }()
	e := env(t, srv, Feed{ID: "mf", Name: "Millî Folklor", Shelf: "s", Lang: "tr"})
	var opened []string
	e.Article = func(ctx context.Context, u string) (*doc.Document, error) {
		opened = append(opened, u)
		return &doc.Document{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: strings.Repeat("Makalenin tam metni. ", 100)}}}}}, nil
	}
	// The feed carries the abstract, long enough to pass for the article.
	e.DB.UpsertItem(store.Item{FeedID: "mf", GUID: "a", URL: srv.URL + "/tr/pub/millifolklor/article/1788179", Title: "Bir makale", Published: time.Now(),
		Summary: strings.Repeat("Uzun bir öz cümlesi. ", 80)})
	items, _ := e.DB.Items(store.Query{})
	d, err := Route(context.Background(), fmt.Sprintf("w5f:item/%d", items[0].ID), e)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != srv.URL+"/tr/download/article-file/42" {
		t.Errorf("opened %v", opened)
	}
	if d.Links[len(d.Links)-1].Href != srv.URL+"/tr/pub/millifolklor/article/1788179" {
		t.Errorf("the original page link is the article's page: %+v", d.Links)
	}
	if pdfOf(context.Background(), e.Fetcher, "https://example.org/article/1") != "" {
		t.Error("only DergiPark's article pages")
	}
}

func TestShelvesAndItemDocs(t *testing.T) {
	recent := time.Now().Add(-time.Hour).Format(time.RFC1123Z)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, rss(item("a", "Göbeklitepe", recent, "<p>kısa özet</p>")))
	}))
	defer srv.Close()
	e := env(t, srv, Feed{ID: "f", Name: "Arkeofili", Shelf: "s", Lang: "tr", URL: []string{srv.URL}})
	full := &doc.Document{Title: "x", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: strings.Repeat("Tam metin paragrafı. ", 80)}}}}}
	e.Article = func(ctx context.Context, u string) (*doc.Document, error) { return full, nil }

	d, err := Route(context.Background(), "w5f:feeds", e) // first visit syncs
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "The Periodical Gallery" || len(d.Links) < 5 || d.Links[5].Href != "w5f:item/1" { // the newest unread at hand
		t.Fatalf("shelves doc: %+v", d)
	}
	items, _ := e.DB.Items(store.Query{})
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	it, err := Route(context.Background(), fmt.Sprintf("w5f:item/%d", items[0].ID), e)
	if err != nil {
		t.Fatal(err)
	}
	if it.Ref != fmt.Sprintf("item:%d", items[0].ID) || it.Lang != "tr" {
		t.Errorf("ref=%q lang=%q", it.Ref, it.Lang)
	}
	if doc.TextLength(it.Blocks) < 500 {
		t.Error("short feed item should be expanded with the full article")
	}
	got, _ := e.DB.Item(items[0].ID)
	if !got.Read {
		t.Error("opening an item should mark it read")
	}
	shelf, err := Route(context.Background(), "w5f:feeds/shelf/s", e)
	if err != nil || shelf.Title != "Shelf" {
		t.Fatalf("shelf doc: %v %+v", err, shelf)
	}
}
