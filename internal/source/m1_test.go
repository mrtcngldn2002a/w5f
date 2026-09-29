package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/crom"
	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func withFetcher(t *testing.T) {
	t.Helper()
	old := Fetcher
	Fetcher = fetch.New(t.TempDir(), "test")
	Fetcher.HostGap = 0
	t.Cleanup(func() { Fetcher = old })
}

func TestResolveShorthands(t *testing.T) {
	cases := map[string]string{
		"scp-173":             "https://scp-wiki.wikidot.com/scp-173",
		"SCP-3125":            "https://scp-wiki.wikidot.com/scp-3125",
		"173":                 "https://scp-wiki.wikidot.com/scp-173",
		"scp-001-j":           "https://scp-wiki.wikidot.com/scp-001-j",
		"arkeofili.com":       "https://arkeofili.com",
		"https://example.org": "https://example.org",
		"w5f:random/scp":      "w5f:random/scp",
		"":                    "",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

// A page that returns 404 is replaced by its closest Wayback snapshot.
func TestDeadPageFallsBackToWayback(t *testing.T) {
	withFetcher(t)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/gone", http.NotFound)
	mux.HandleFunc("/wayback/available", func(w http.ResponseWriter, r *http.Request) {
		snap := srv.URL + "/web/20210304120000/" + r.URL.Query().Get("url")
		json.NewEncoder(w).Encode(map[string]any{"archived_snapshots": map[string]any{
			"closest": map[string]any{"available": true, "url": snap, "timestamp": "20210304120000", "status": "200"}}})
	})
	mux.HandleFunc("/web/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "20210304120000id_/") {
			t.Errorf("snapshot must use the raw id_ variant, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><article><h1>Lost Page</h1><p>This text only survives in the archive, and it is long enough to be the main content of the page for the extractor to keep it around.</p></article></body></html>`)
	})
	WaybackAPI = srv.URL + "/wayback/available"
	defer func() { WaybackAPI = "https://archive.org/wayback/available" }()
	// The test server speaks plain HTTP; loadArchived upgrades http→https for
	// real snapshots, so point the test at an https-less path by stubbing.
	d, err := loadArchivedHTTP(t, srv.URL+"/gone")
	if err != nil {
		t.Fatal(err)
	}
	if d.Origin != "archive" || !strings.Contains(text(d), "Archived copy from 2021-03-04") ||
		!strings.Contains(text(d), "only survives in the archive") {
		t.Errorf("origin=%s\n%s", d.Origin, text(d))
	}
}

// loadArchivedHTTP runs Load with the snapshot https-upgrade disabled, since
// httptest servers are plain HTTP.
func loadArchivedHTTP(t *testing.T, target string) (*doc.Document, error) {
	t.Helper()
	upgradeSnapshots = false
	defer func() { upgradeSnapshots = true }()
	return Load(context.Background(), target, Options{})
}

func TestRandomViaCrom(t *testing.T) {
	withFetcher(t)
	calls := 0
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><div id="page-content"><p>Item #: SCP-999</p></div><script>WIKIREQUEST</script></body></html>`)
	}))
	defer page.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		u := page.URL + "/scp-999"
		if calls == 1 {
			u = page.URL + "/scp-pl-180" // translated page: must be rejected
		}
		fmt.Fprintf(w, `{"data":{"randomPage":{"page":{"url":%q}}}}`, u)
	}))
	defer api.Close()
	crom.Endpoint, crom.UpgradeHTTPS = api.URL, false
	defer func() { crom.Endpoint, crom.UpgradeHTTPS = "https://apiv1.crom.avn.sh/graphql", true }()

	d, err := Load(context.Background(), "w5f:random/scp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(d.URL, "/scp-999") || calls != 2 {
		t.Errorf("url=%s calls=%d", d.URL, calls)
	}
}
