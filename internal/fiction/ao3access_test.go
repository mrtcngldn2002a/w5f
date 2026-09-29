package fiction

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/fetch"
)

func TestAO3CookieCleaning(t *testing.T) {
	for in, want := range map[string]string{
		"abc123": "_otwarchive_session=abc123",
		"_otwarchive_session=abc; remember_user_token=xyz; _ga=1; view_adult=true": "_otwarchive_session=abc; remember_user_token=xyz",
		"Cookie: _otwarchive_session=abc":                                          "_otwarchive_session=abc",
		"two words":                                                                "",
		"":                                                                         "",
	} {
		if got := cleanAO3Cookie(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestAO3SessionIsSentOnlyToAO3AndErrorsAreHonest(t *testing.T) {
	t.Setenv("W5F_AO3_SESSION", "_otwarchive_session=secret")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.Contains(r.Header.Get("Cookie"), "secret") && !strings.HasPrefix(r.URL.Path, "/ao3/") {
			t.Error("the AO3 session went to another site")
		}
		switch r.URL.Path {
		case "/ao3/ok":
			if r.UserAgent() != fetch.CatalogUserAgent || r.Header.Get("Sec-Ch-Ua") == "" {
				t.Error("AO3 did not use the browser-compatible transport")
			}
			if !strings.Contains(r.Header.Get("Cookie"), "_otwarchive_session=secret") {
				t.Error("AO3 requests carry the owner's session")
			}
			http.SetCookie(w, &http.Cookie{Name: "navigation", Value: "same-session", Path: "/ao3/"})
			w.Write([]byte("<html><body><div id='greeting'><a href='/users/me'>Hi, me!</a></div></body></html>"))
		case "/ao3/next":
			cookie, err := r.Cookie("navigation")
			if err != nil || cookie.Value != "same-session" {
				t.Error("AO3 navigation lost its cookie session")
			}
			w.Write([]byte("<html><body>Next page</body></html>"))
		case "/ao3/down":
			w.WriteHeader(525)
			w.Write([]byte(`<title>archiveofourown.org | 525: SSL handshake failed</title><body>error code: 525<script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script></body>`))
		default:
			w.Write([]byte("<html><body>elsewhere</body></html>"))
		}
	}))
	defer srv.Close()
	old := ao3Hosts
	ao3Hosts = map[string]bool{strings.TrimPrefix(srv.URL, "http://"): true}
	defer func() { ao3Hosts = old }()
	oldWait := ao3RetryWait
	ao3RetryWait = 0
	defer func() { ao3RetryWait = oldWait }()

	f := fetch.New("", "test")
	f.HostGap = 0
	ctx := context.Background()
	if _, _, err := ao3Page(ctx, f, srv.URL+"/ao3/ok", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ao3Page(ctx, f, srv.URL+"/ao3/next", false); err != nil {
		t.Fatal(err)
	}
	// Other sites never get the cookie (getPage is the non-AO3 path).
	old2 := ao3Hosts
	ao3Hosts = map[string]bool{"archiveofourown.org": true}
	getPage(ctx, f, srv.URL+"/elsewhere", "x", false)
	ao3Hosts = old2

	calls = 0
	_, _, err := ao3Page(ctx, f, srv.URL+"/ao3/down", false)
	var un errUnavailable
	if !errors.As(err, &un) || calls != 2 {
		t.Fatalf("525: %v (calls %d, want one retry)", err, calls)
	}
	if !strings.Contains(err.Error(), "Downloads") || strings.Contains(err.Error(), "verification") {
		t.Errorf("message: %v", err)
	}
}
