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

// The list page of Nature Futures: old stories, open ones, and links that are no stories.
func futuresListServer(t *testing.T, ids ...string) (*httptest.Server, *[]string) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		fmt.Fprint(w, `<html><body><nav><a href="/nature/">Nature</a></nav><ul>`)
		for _, id := range ids {
			fmt.Fprintf(w, `<li><a href="/articles/%s">Story %s</a></li><li><a href="/articles/%s">Story %s</a></li>`, id, id, id, id)
		}
		fmt.Fprint(w, `<li><a href="/articles/d41586-025-03605-4#comments">   </a></li>
<li><a href="/articles/d41586-025-03666-5"><img src="x"></a></li>
<li><a href="/subjects/futures">Futures</a></li></ul></body></html>`)
	}))
	t.Cleanup(srv.Close)
	swap(t, &natureFuturesList, srv.URL+"/nature/articles?type=futures&page=")
	return srv, &asked
}

// Every story is paywalled now (2026-10-30): while the Archive is down the
// shelf draws nothing, and never the journal's own preview.
func TestNatureFuturesDrawsNothingWhileTheArchiveIsDown(t *testing.T) {
	(&wayback{down: true}).serve(t)
	_, asked := futuresListServer(t, "516444a", "35012720", "d41586-025-03600-9")
	for range 8 {
		if target, _, err := natureFuturesPick(context.Background(), Env{Fetcher: testFetcher()}); err == nil {
			t.Fatalf("drew %q with the Archive down", target)
		}
	}
	recent := 0
	for _, q := range *asked {
		var page int
		if _, err := fmt.Sscanf(q, "type=futures&page=%d", &page); err != nil || page < 1 || page > natureFuturesPages {
			t.Errorf("page %q is outside the list", q)
		}
		if page <= natureFuturesRecentPages {
			recent++
		}
	}
	if recent*2 < len(*asked) {
		t.Errorf("%d of %d list pages were of the stories since 2018", recent, len(*asked))
	}
}

// A story since 2018 is read from the Archive too, as a page of its own.
func TestNatureFuturesPicksANewStoryFromTheArchive(t *testing.T) {
	w := &wayback{current: []string{"20230101000000"}, pages: map[string]string{"20230101000000": pageCurrent(storyParas(8))}}
	w.serve(t)
	futuresListServer(t, "d41586-025-03600-9")
	target, _, err := natureFuturesPick(context.Background(), Env{Fetcher: testFetcher()})
	if want := "w5f:discover/futures?id=d41586-025-03600-9&t=Story+d41586-025-03600-9"; err != nil || target != want {
		t.Fatalf("pick: %q %v, want %q", target, err, want)
	}
}

// An old story the Archive holds is drawn as a page of its own.
func TestNatureFuturesPicksAnOldStoryFromTheArchive(t *testing.T) {
	w := &wayback{legacy: []string{"20170101000000"}, pages: map[string]string{"20170101000000": pageArticle(storyParas(8))}}
	w.serve(t)
	futuresListServer(t, "516444a")
	target, title, err := natureFuturesPick(context.Background(), Env{Fetcher: testFetcher()})
	want := "w5f:discover/futures?id=516444a&t=Story+516444a"
	if err != nil || target != want || title != "Story 516444a" {
		t.Fatalf("pick: %q %q %v", target, title, err)
	}
	if !IsTarget(target) {
		t.Error("the draw is no discovery address")
	}
}

// A story the Archive does not hold gives way to another one: the mock
// holds no old captures, only one of the present address.
func TestNatureFuturesPassesOverAStoryWithoutACopy(t *testing.T) {
	w := &wayback{current: []string{"20230101000000"}, pages: map[string]string{"20230101000000": pageCurrent(storyParas(8))}}
	w.serve(t)
	futuresListServer(t, "516444a", "d41586-025-03600-9")
	for range 6 {
		target, _, err := natureFuturesPick(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil || !strings.HasPrefix(target, "w5f:discover/futures?") {
			t.Fatalf("pick: %q %v", target, err)
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
