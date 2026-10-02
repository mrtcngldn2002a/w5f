package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const wall = `<title>Just a moment...</title><p>Verify you are human</p>`

// solverTestFetcher answers every ordinary request with status and body.
func solverTestFetcher(t *testing.T, status int, header http.Header, body string) (*Fetcher, *int) {
	t.Helper()
	resetSolver(t)
	f := New(t.TempDir(), "test")
	f.HostGap = 0
	calls := new(int)
	fake := pageTestTransport(func(r *http.Request) (*http.Response, error) {
		*calls++
		h := http.Header{"Content-Type": {"text/html"}}
		for k, v := range header {
			h[k] = v
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	f.Client.Transport = fake
	f.catalog = New("", "test")
	f.catalog.Client.Transport = fake
	f.catalogOnce.Do(func() {})
	return f, calls
}

func resetSolver(t *testing.T) {
	clear := func() {
		solverState.mu.Lock()
		solverState.misses = map[string]time.Time{}
		solverState.mu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func solverReply(w http.ResponseWriter, target, body string) {
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "solution": map[string]any{"url": target, "status": 200, "response": body}})
}

// helperFor is a fake helper that returns page for every request.
func helperFor(t *testing.T, page string) (*httptest.Server, *int) {
	calls := new(int)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		solverReply(w, p["url"].(string), page)
	}))
	t.Cleanup(h.Close)
	return h, calls
}

func get(f *Fetcher, ctx context.Context, raw string, opts Options) (*Response, error) {
	u, _ := url.Parse(raw)
	return f.Get(ctx, u, opts)
}

func TestSolverPageCacheAndOffline(t *testing.T) {
	const target = "https://www.biodiversitylibrary.org/bibliography/1"
	helperCalls := 0
	helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		helperCalls++
		if r.Method != "POST" || r.URL.Path != "/v1" {
			t.Errorf("bad helper request: %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["url"] != target || payload["cmd"] != "request.get" || payload["session"] != "w5f" {
			t.Errorf("bad payload: %v", payload)
		}
		if _, ok := payload["cookies"]; ok {
			t.Error("cookies forwarded to helper")
		}
		solverReply(w, target, "<title>Reading</title><p>The library text.</p>")
	}))
	defer helper.Close()
	f, calls := solverTestFetcher(t, 200, nil, wall)
	f.SolverURL = helper.URL
	r, err := get(f, context.Background(), target, Options{})
	if err != nil || !strings.Contains(string(r.Body), "library text") || r.FromCache {
		t.Fatalf("first read: %+v %v", r, err)
	}
	f.Offline = true
	r, err = get(f, context.Background(), target, Options{Revalidate: true})
	if err != nil || !r.FromCache || helperCalls != 1 || *calls != 1 {
		t.Fatalf("offline read: %+v %v, helper=%d native=%d", r, err, helperCalls, *calls)
	}
}

// The helper is asked only for a wall, on any site, and never for
// HathiTrust or a file.
func TestSolverIsAskedOnlyForWalls(t *testing.T) {
	for _, tc := range []struct {
		name, target, body string
		status             int
		header             http.Header
		enabled, asked     bool
	}{
		{"disabled", "https://hermetic.com/", wall, 200, nil, false, false},
		{"any-site", "https://www.britannica.com/", wall, 200, nil, true, true},
		{"ordinary-page", "https://hermetic.com/", "<title>Reading</title><p>Text</p>", 200, nil, true, false},
		{"hathitrust", "https://babel.hathitrust.org/cgi/pt?id=x", wall, 200, nil, true, false},
		{"file", "https://example.org/book.pdf", wall, 200, nil, true, false},
		{"cloudflare-403", "https://www.loc.gov/", "<p>Forbidden</p>", 403, http.Header{"Cf-Mitigated": {"challenge"}}, true, true},
		{"plain-403", "https://www.loc.gov/", "<p>Forbidden</p>", 403, nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helper, calls := helperFor(t, "<title>Page</title><p>Real text.</p>")
			f, _ := solverTestFetcher(t, tc.status, tc.header, tc.body)
			if tc.enabled {
				f.SolverURL = helper.URL
			}
			r, err := get(f, context.Background(), tc.target, Options{})
			if tc.asked {
				if err != nil || !strings.Contains(string(r.Body), "Real text") || *calls != 1 {
					t.Fatalf("helper not used: %v calls=%d", err, *calls)
				}
				return
			}
			if *calls != 0 {
				t.Fatalf("unexpected helper calls: %d", *calls)
			}
			var ce *ChallengeError
			var he *HTTPError
			switch {
			case tc.status == 403 && !errors.As(err, &he):
				t.Fatalf("expected HTTP error, got %v", err)
			case tc.status == 200 && tc.body == wall && !errors.As(err, &ce):
				t.Fatalf("expected verification error, got %v", err)
			case tc.status == 200 && tc.body != wall && err != nil:
				t.Fatal(err)
			}
		})
	}
}

func TestSolverRejectsUnusablePagesAndRemembers(t *testing.T) {
	for _, tc := range []struct{ name, target, body string }{
		{"wall", "https://hermetic.com/", wall},
		{"foreign", "https://example.org/", "<p>Other site</p>"},
		{"lookalike", "https://hermetic.com.example.org/", "<p>Other site</p>"},
		{"empty", "https://hermetic.com/", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; solverReply(w, tc.target, tc.body) }))
			defer helper.Close()
			f, _ := solverTestFetcher(t, 200, nil, wall)
			f.SolverURL = helper.URL
			for range 2 {
				_, err := get(f, context.Background(), "https://hermetic.com/", Options{})
				var ce *ChallengeError
				if !errors.As(err, &ce) || ce.Helper == "" {
					t.Fatalf("invalid page accepted or reason lost: %v", err)
				}
			}
			if calls != 1 {
				t.Fatalf("a site the helper failed on was asked again today: calls=%d", calls)
			}
		})
	}
}

func TestSolverSameSiteSubdomain(t *testing.T) {
	helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		solverReply(w, "https://archive.example.org/a", "<p>Moved within the site.</p>")
	}))
	defer helper.Close()
	f, _ := solverTestFetcher(t, 200, nil, wall)
	f.SolverURL = helper.URL
	if r, err := get(f, context.Background(), "https://www.example.org/a", Options{}); err != nil || r.URL.Host != "archive.example.org" {
		t.Fatalf("%v %v", r, err)
	}
}

func TestSolverOncePerDraw(t *testing.T) {
	helper, calls := helperFor(t, "<p>Text.</p>")
	f, _ := solverTestFetcher(t, 200, nil, wall)
	f.SolverURL = helper.URL
	ctx := SolverOnce(context.Background())
	if _, err := get(f, ctx, "https://a.example.org/", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := get(f, ctx, "https://b.example.net/", Options{}); err == nil || *calls != 1 {
		t.Fatalf("a second helper request in one draw: %v calls=%d", err, *calls)
	}
	if _, err := get(f, context.Background(), "https://b.example.net/", Options{}); err != nil || *calls != 2 {
		t.Fatalf("an ordinary read lost its helper: %v calls=%d", err, *calls)
	}
}

// Nothing listening: a fast, plain answer, and the site is not marked.
func TestSolverNotRunning(t *testing.T) {
	f, _ := solverTestFetcher(t, 200, nil, wall)
	f.SolverURL = "http://127.0.0.1:1"
	start := time.Now()
	_, err := get(f, context.Background(), "https://hermetic.com/", Options{})
	if err == nil || !strings.Contains(err.Error(), "no bot-check helper") || time.Since(start) > 5*time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
	if len(solverState.misses) != 0 {
		t.Fatal("a missing helper marked the site")
	}
}

func TestSolverNoStore(t *testing.T) {
	helper, calls := helperFor(t, "<p>Library</p>")
	f, _ := solverTestFetcher(t, 200, nil, wall)
	f.SolverURL = helper.URL
	if _, err := get(f, context.Background(), "https://hermetic.com/", Options{NoStore: true}); err != nil {
		t.Fatal(err)
	}
	f.Offline = true
	if _, err := get(f, context.Background(), "https://hermetic.com/", Options{}); !errors.Is(err, ErrOffline) {
		t.Fatalf("NoStore page retained: %v", err)
	}
	if *calls != 1 {
		t.Fatal(*calls)
	}
}

func TestSolverRejectsRemoteEndpointAndRedirect(t *testing.T) {
	f, _ := solverTestFetcher(t, 200, nil, wall)
	for _, endpoint := range []string{"https://example.org", "file:///tmp/solver", "http://user:pass@127.0.0.1:8191"} {
		f.SolverURL = endpoint
		if _, err := get(f, context.Background(), "https://hermetic.com/", Options{}); err == nil {
			t.Fatal("accepted endpoint", endpoint)
		}
	}
	redirected := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++ }))
	defer other.Close()
	helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
	defer helper.Close()
	f.SolverURL = helper.URL
	if _, err := get(f, context.Background(), "https://hermetic.com/", Options{}); err == nil || redirected != 0 {
		t.Fatalf("helper redirect followed: %v, calls=%d", err, redirected)
	}
}
