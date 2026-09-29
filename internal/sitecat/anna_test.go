package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/fetch"
)

func TestAnnaUsesOwnResultsAndPaging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
			fmt.Fprint(w, "Anna's Archive")
			return
		}
		if r.URL.Path != "/search" {
			t.Fatalf("wrong source %s", r.URL)
		}
		if r.URL.Query().Get("q") != "history" {
			t.Errorf("wrong query %s", r.URL)
		}
		if _, err := r.Cookie("session"); err != nil {
			t.Error("search lost homepage session")
		}
		h := strings.Repeat("a", 32)
		if r.URL.Query().Get("page") == "2" {
			h = strings.Repeat("b", 32)
		}
		fmt.Fprintf(w, `<a href="/md5/%s">Anna exclusive book</a>`, h)
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `<a href="/search?q=history&amp;page=2">Next</a>`)
		}
	}))
	defer srv.Close()
	p := Profile{Home: srv.URL, Search: SearchSpec{Kind: "anna"}}
	rs, err := Search(context.Background(), testFetcher(), &p, "history", nil)
	if err != nil || rs.Pages != 2 || len(rs.Results) != 2 {
		t.Fatalf("%+v %v", rs, err)
	}
	for _, r := range rs.Results {
		if !strings.HasPrefix(r.URL, srv.URL+"/md5/") {
			t.Fatal("substituted a different catalog")
		}
	}
}

func TestAnnaVerificationNeverSwitchesSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `<title>DDoS-Guard</title><body>Checking your browser</body>`)
	}))
	defer srv.Close()
	p := Profile{Home: srv.URL, Search: SearchSpec{Kind: "anna"}}
	_, err := Search(context.Background(), testFetcher(), &p, "history", nil)
	if err == nil || !strings.Contains(err.Error(), "DDoS-Guard") {
		t.Fatalf("challenge not reported: %v", err)
	}
	if got := fetch.ChallengeReason([]byte(`<title>DDoS-Guard</title>`)); got == "" {
		t.Fatal("challenge missing")
	}
	rep, err := probeAnna(context.Background(), testFetcher(), srv.URL, "history")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "anna" || len(rep.Sample) != 0 {
		t.Fatalf("known catalog should be saveable with an honest warning: %+v %v", rep, err)
	}
}
