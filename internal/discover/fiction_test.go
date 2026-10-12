package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFictionSubsAreThickened(t *testing.T) {
	for _, s := range []string{"nosleep", "TheCrypticCompendium", "Odd_directions", "Cryosleep", "shortscarystories", "WeirdLit"} {
		found := false
		for _, f := range fictionSubs {
			found = found || f == s
		}
		if !found {
			t.Errorf("r/%s is not among the fiction subreddits", s)
		}
	}
}

func TestNatureFuturesPicksAStoryFromAListPage(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		fmt.Fprint(w, `<html><body><nav><a href="/nature/">Nature</a></nav><ul>
<li><a href="/articles/d41586-025-03600-9">The last lighthouse keeper of Mars</a></li>
<li><a href="/articles/d41586-025-03600-9">The last lighthouse keeper of Mars</a></li>
<li><a href="/articles/d41586-025-03605-4#comments">   </a></li>
<li><a href="/articles/d41586-025-03666-5"><img src="x"></a></li>
<li><a href="/subjects/futures">Futures</a></li></ul></body></html>`)
	}))
	defer srv.Close()
	swap(t, &natureFuturesList, srv.URL+"/nature/articles?type=futures&page=")
	for range 5 {
		target, title, err := natureFuturesPick(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil || target != srv.URL+"/articles/d41586-025-03600-9" || title != "The last lighthouse keeper of Mars" {
			t.Fatalf("pick: %q %q %v", target, title, err)
		}
	}
	for _, q := range asked {
		var page int
		if _, err := fmt.Sscanf(q, "type=futures&page=%d", &page); err != nil || page < 1 || page > 60 {
			t.Errorf("page %q is outside 1..60", q)
		}
	}
}

func TestNatureFuturesWithoutStories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html><body>nothing</body></html>") }))
	defer srv.Close()
	swap(t, &natureFuturesList, srv.URL+"/?page=")
	if _, _, err := natureFuturesPick(context.Background(), Env{Fetcher: testFetcher()}); err == nil || !strings.Contains(err.Error(), "no stories") {
		t.Errorf("got %v", err)
	}
}

// With a fetcher, the fiction family draws from the web places too, and a
// place that is down gives way to another.
func TestFictionDrawsFromTheWebPlaces(t *testing.T) {
	old := fictionWeb
	t.Cleanup(func() { fictionWeb = old })
	fictionWeb = []fictionPlace{
		{"Up", 2, func(context.Context, Env) (string, string, error) { return "https://example.org/story", "A story", nil }},
		{"Down", 2, func(context.Context, Env) (string, string, error) { return "", "", fmt.Errorf("closed") }},
	}
	env := Env{Fetcher: testFetcher()}
	for range 40 {
		d, err := fiction{}.Draw(context.Background(), env)
		if err != nil || d.Target != "https://example.org/story" || d.Why != "fiction/Up · A story" {
			t.Fatalf("draw: %+v %v", d, err)
		}
	}
}
