package fiction

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// fixtureServer serves testdata/fiction files by path.
func fixtureServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		name, ok := routes[key]
		if !ok {
			name, ok = routes[r.URL.Path]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fiction", name))
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRoyalRoad(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/fiction/21220": "rr-fiction.html",
		"/fiction/21220/mother-of-learning/chapter/301778/1-good-morning-brother": "rr-chapter.html",
		"/fictions/best-rated": "rr-best.html",
	})
	a := royalRoad{}
	if s, ok := a.Match(mustURL("https://www.royalroad.com/fiction/21220/mother-of-learning/chapter/301778/1-good-morning-brother")); !ok || s != "https://www.royalroad.com/fiction/21220" {
		t.Errorf("match: %q %v", s, ok)
	}
	if _, ok := a.Match(mustURL("https://www.royalroad.com/fictions/best-rated")); ok {
		t.Error("lists are not serials")
	}
	f := fetch.New("", "test")
	ctx := context.Background()
	s, err := a.Serial(ctx, f, srv.URL+"/fiction/21220")
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Mother of Learning" || s.Author != "nobody103" || !strings.Contains(s.Summary, "Zorian") || len(s.Chapters) < 100 {
		t.Fatalf("serial: %q %q %.40q %d", s.Title, s.Author, s.Summary, len(s.Chapters))
	}
	c0 := s.Chapters[0]
	if c0.Title != "1. Good Morning Brother" || !strings.HasSuffix(c0.URL, "/chapter/301778/1-good-morning-brother") || c0.Published.Year() != 2018 {
		t.Errorf("chapter 0: %+v", c0)
	}
	d, err := a.Chapter(ctx, f, c0)
	if err != nil {
		t.Fatal(err)
	}
	txt := flat(d)
	if !strings.Contains(txt, "Zorian") || strings.Contains(txt, "Stolen from its rightful place") {
		t.Errorf("chapter text (hidden sentence must go):\n%.600s", txt)
	}
	notes := 0
	for _, b := range d.Blocks {
		if c, ok := b.(doc.Collapsible); ok && strings.Contains(c.Show, "note") {
			notes++
		}
	}
	if notes == 0 {
		t.Error("author's note not folded")
	}
	list, err := rrList(ctx, f, srv.URL+"/fictions/best-rated", "Royal Road — best rated")
	if err != nil || len(list.Links) < 10 || !strings.Contains(list.Links[0].Href, "w5f:serial/open?u=") {
		t.Errorf("list: %v %d", err, len(list.Links))
	}
}
