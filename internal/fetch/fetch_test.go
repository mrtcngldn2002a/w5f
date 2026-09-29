package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func newTest(t *testing.T) *Fetcher {
	f := New(t.TempDir(), "test")
	f.HostGap = 0
	return f
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestCacheFreshRevalidateAndOffline(t *testing.T) {
	var hits, notMod atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			notMod.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<p>hello</p>"))
	}))
	defer srv.Close()
	f := newTest(t)
	u := mustURL(srv.URL + "/a")
	ctx := context.Background()

	r, err := f.Get(ctx, u, Options{})
	if err != nil || string(r.Body) != "<p>hello</p>" || r.FromCache {
		t.Fatalf("first get: %v %+v", err, r)
	}
	// Within the freshness window: no network.
	r, _ = f.Get(ctx, u, Options{})
	if !r.FromCache || hits.Load() != 1 {
		t.Fatalf("expected fresh cache hit, hits=%d", hits.Load())
	}
	// Reload: conditional request answered with 304.
	r, _ = f.Get(ctx, u, Options{Revalidate: true})
	if notMod.Load() != 1 || string(r.Body) != "<p>hello</p>" {
		t.Fatalf("revalidate: notMod=%d body=%q", notMod.Load(), r.Body)
	}
	// Offline mode: served from disk.
	f.Offline = true
	r, err = f.Get(ctx, u, Options{})
	if err != nil || !r.FromCache {
		t.Fatalf("offline: %v", err)
	}
	if _, err := f.Get(ctx, mustURL(srv.URL+"/never"), Options{}); !errors.Is(err, ErrOffline) {
		t.Errorf("offline miss err = %v", err)
	}
}

func TestNetworkFailureServesStaleCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("cached body"))
	}))
	f := newTest(t)
	u := mustURL(srv.URL + "/x")
	if _, err := f.Get(context.Background(), u, Options{}); err != nil {
		t.Fatal(err)
	}
	srv.Close() // the site goes away
	r, err := f.Get(context.Background(), u, Options{Revalidate: true})
	if err != nil || !r.Stale || string(r.Body) != "cached body" {
		t.Fatalf("stale fallback: %v %+v", err, r)
	}
}

func TestGoneErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := newTest(t).Get(context.Background(), mustURL(srv.URL), Options{})
	if !Gone(err) {
		t.Errorf("404 should be Gone: %v", err)
	}
}

func TestHostGap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := newTest(t)
	f.HostGap = 150 * time.Millisecond
	start := time.Now()
	for i := 0; i < 3; i++ {
		f.Get(context.Background(), mustURL(srv.URL+"/"+string(rune('a'+i))), Options{NoStore: true})
	}
	if el := time.Since(start); el < 280*time.Millisecond {
		t.Errorf("3 requests took %v; politeness gap not applied", el)
	}
}
