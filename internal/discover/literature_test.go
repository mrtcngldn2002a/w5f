package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"w5f/internal/fetch"
)

func TestWPPickTakesTheNthPostOfTheRESTList(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/posts") || r.URL.Query().Get("categories") != "63" {
			http.NotFound(w, r) // the filter goes with every request
			return
		}
		w.Header().Set("X-WP-Total", "3191")
		page := r.URL.Query().Get("page")
		if page == "" {
			fmt.Fprint(w, `[{"id":1}]`)
			return
		}
		pages = append(pages, page)
		fmt.Fprintf(w, `[{"link":"%s/post-%s/","title":{"rendered":"An Interview with Daniel Dennett &#8211; Part <em>%s</em>"}}]`, "http://example.org", page, page)
	}))
	defer srv.Close()
	pick := wpPick(srv.URL, "categories=63")
	env := Env{Fetcher: testFetcher()}
	for range 10 {
		target, title, err := pick(context.Background(), env)
		if err != nil || !strings.HasPrefix(target, "http://example.org/post-") || !strings.HasPrefix(title, "An Interview with Daniel Dennett – Part ") {
			t.Fatalf("pick: %q %q %v", target, title, err)
		}
	}
	for _, p := range pages {
		if p == "0" || len(p) > 4 {
			t.Errorf("page %s is outside 1..3191", p)
		}
	}
	if wpTotals[srv.URL+"/wp-json/wp/v2/posts?per_page=1&categories=63"] != 3191 {
		t.Error("the total is kept")
	}
}

func TestWPPickWithoutATotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[{"id":1}]`) }))
	defer srv.Close()
	if _, _, err := wpPick(srv.URL, "")(context.Background(), Env{Fetcher: testFetcher()}); err == nil {
		t.Error("a REST API that does not say how many posts has no random post")
	}
}

func TestSitemapPickFindsAnAddressOfTheKindWanted(t *testing.T) {
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			fmt.Fprintf(w, `<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap><loc>%[1]s/post-sitemap.xml</loc></sitemap><sitemap><loc>%[1]s/page-sitemap.xml</loc></sitemap><sitemap><loc>%[1]s/post-sitemap2.xml</loc></sitemap></sitemapindex>`, base)
		case "/post-sitemap.xml": // holds nothing wanted
			fmt.Fprint(w, `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>http://x.org/about/</loc></url></urlset>`)
		case "/post-sitemap2.xml":
			fmt.Fprint(w, `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url><loc>http://x.org/read/article/2020-01/the-week-in-translation-5/</loc></url>
<url><loc>http://x.org/read/article/2020-01/the-chaldean-ruins/</loc></url></urlset>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	base = srv.URL
	pick := sitemapPick(srv.URL+"/sitemap.xml", `/post-sitemap\d*\.xml$`, `/read/article/`, `week-in-translation`)
	for range 10 {
		target, title, err := pick(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil || target != "http://x.org/read/article/2020-01/the-chaldean-ruins/" || title != "the chaldean ruins" {
			t.Fatalf("pick: %q %q %v", target, title, err)
		}
	}
	none := sitemapPick(srv.URL+"/sitemap.xml", `/post-sitemap\d*\.xml$`, `/poem/`, ``)
	if _, _, err := none(context.Background(), Env{Fetcher: testFetcher()}); err == nil {
		t.Error("nothing of the kind: an error is expected")
	}
}

// poetryServer plays the Poetry Foundation page (with the search settings in
// its scripts) and the search itself.
func poetryServer(t *testing.T, key string, hits []map[string]any) (*httptest.Server, *int) {
	t.Helper()
	queries := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/poems/browse":
			fmt.Fprintf(w, `<html><script>window.__NUXT__.config={public:{algolia:{apiKey:"%s",applicationId:"APP123",lite:true},globalIndex:"prod_X"}}</script></html>`, key)
		case strings.HasSuffix(r.URL.Path, "/query"):
			*queries++
			if r.Header.Get("X-Algolia-Api-Key") != "searchkey" || r.Header.Get("X-Algolia-Application-Id") != "APP123" {
				http.Error(w, "bad key", http.StatusForbidden)
				return
			}
			var body struct{ Params string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body.Params, "filters=type%3Apoems") {
				http.Error(w, "poems only", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, queries
}

func TestPoetryPickReadsTheSearchFromThePageAndSkipsScans(t *testing.T) {
	long := strings.Repeat("Two roads diverged in a yellow wood ", 5)
	srv, queries := poetryServer(t, "searchkey", []map[string]any{
		{"title": "Refusing the Sea", "uri": "poetrymagazine/poems/30216/refusing-the-sea", "authors": []string{"Philip Booth"}, "content": ""},
		{"title": "Dust of Snow", "uri": "poems/44262/dust-of-snow", "authors": []string{"Robert Frost"}, "content": long},
	})
	swap(t, &poetryConfURL, srv.URL+"/poems/browse")
	swap(t, &poetrySite, "https://www.poetryfoundation.org/")
	swap(t, &algoliaHost, func(string) string { return srv.URL })
	env := testEnv(t)
	env.Fetcher = testFetcher()
	for range 5 {
		target, title, err := poetryPick(context.Background(), env)
		if err != nil || target != "https://www.poetryfoundation.org/poems/44262/dust-of-snow" || title != "Dust of Snow by Robert Frost" {
			t.Fatalf("pick: %q %q %v", target, title, err)
		}
	}
	if *queries != 5 {
		t.Errorf("five draws, %d searches", *queries)
	}
	if !strings.Contains(env.DB.Get(poetryConfKey), "APP123") {
		t.Error("the settings read from the page are kept")
	}
}

func TestPoetryPickReadsThePageAgainWhenTheKeyChanged(t *testing.T) {
	long := strings.Repeat("Two roads diverged in a yellow wood ", 5)
	srv, _ := poetryServer(t, "searchkey", []map[string]any{{"title": "Dust of Snow", "uri": "poems/44262/dust-of-snow", "content": long}})
	swap(t, &poetryConfURL, srv.URL+"/poems/browse")
	swap(t, &algoliaHost, func(string) string { return srv.URL })
	env := testEnv(t)
	env.Fetcher = testFetcher()
	_ = env.DB.Set(poetryConfKey, `{"app":"APP123","key":"staleKey","index":"prod_X","at":"`+time.Now().Format(time.RFC3339)+`"}`)
	if _, _, err := poetryPick(context.Background(), env); err != nil {
		t.Fatalf("a refused key is read again from the page: %v", err)
	}
	if !strings.Contains(env.DB.Get(poetryConfKey), `"key":"searchkey"`) {
		t.Error("the new key is kept")
	}
}

func TestPoetryPickWhenThePageChanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>no scripts</html>") }))
	defer srv.Close()
	swap(t, &poetryConfURL, srv.URL)
	env := testEnv(t)
	env.Fetcher = testFetcher()
	if _, _, err := poetryPick(context.Background(), env); err == nil || !strings.Contains(err.Error(), "no longer says") {
		t.Errorf("got %v", err)
	}
}

func TestLiteratureFamily(t *testing.T) {
	old := litSources
	t.Cleanup(func() { litSources = old })
	litSources = []litSource{
		{"down", 1, func(context.Context, Env) (string, string, error) { return "", "", fmt.Errorf("closed") }},
		{"up", 1, func(context.Context, Env) (string, string, error) {
			return "https://example.org/essay", "An essay", nil
		}},
	}
	for range 10 {
		d, err := literature{}.Draw(context.Background(), Env{})
		if err != nil || d.Target != "https://example.org/essay" || d.Why != "literature/up · An essay" {
			t.Fatalf("draw: %+v %v", d, err)
		}
	}
	if len(old) != 5 {
		t.Errorf("five sources chosen, %d listed", len(old))
	}
}

func TestOncePerVisitStopsWhenTheMeterIsSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "bpwallcount", Value: "1", Path: "/"})
		fmt.Fprint(w, "<p>The whole post.</p>")
	}))
	defer srv.Close()
	draws := 0
	pick := oncePerVisit(srv.URL+"/", "bpwallcount", func(context.Context, Env) (string, string, error) {
		draws++
		return srv.URL + "/post/", "A post", nil
	})
	env := Env{Fetcher: testFetcher()}
	target, _, err := pick(context.Background(), env)
	if err != nil || draws != 1 {
		t.Fatalf("the first draw of a session: %q %v", target, err)
	}
	u, _ := url.Parse(target)
	if _, err := env.Fetcher.Get(context.Background(), u, fetch.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pick(context.Background(), env); err == nil || draws != 1 {
		t.Error("once the free post is read, the site is not drawn again")
	}
}
