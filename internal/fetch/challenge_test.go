package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestChallengesNeverPoisonCache(t *testing.T) {
	for _, status := range []int{200, 403, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					w.WriteHeader(status)
					fmt.Fprint(w, `<title>Just a moment...</title><div id="cf-challenge">Wait</div>`)
				} else {
					fmt.Fprint(w, `<title>Books</title><p>Catalog ready</p>`)
				}
			}))
			defer srv.Close()
			f := New(t.TempDir(), "test")
			f.HostGap = 0
			u, _ := url.Parse(srv.URL)
			_, err := f.Get(context.Background(), u, Options{})
			var ce *ChallengeError
			if !errors.As(err, &ce) {
				t.Fatalf("%T %v", err, err)
			}
			r, err := f.Get(context.Background(), u, Options{})
			if err != nil || r.FromCache || calls != 2 {
				t.Fatalf("cache poisoned: %+v %v", r, err)
			}
		})
	}
}
func TestEmbeddedChallengeWidgetIsNotWall(t *testing.T) {
	body := []byte(`<title>Catalog</title><form><input type="search" name="q"></form><script src="/cdn-cgi/challenge-platform/widget.js"></script><div class="g-recaptcha"></div>`)
	if r := ChallengeReason(body); r != "" {
		t.Fatalf("false positive: %s", r)
	}
}

func TestCloudflareBackgroundDetectionIsNotVerification(t *testing.T) {
	for _, title := range []string{"A short story", "archiveofourown.org | 525: SSL handshake failed"} {
		body := []byte(`<title>` + title + `</title><body><p>Temporarily unavailable.</p><script>var a=document.createElement('script'); a.src='/cdn-cgi/challenge-platform/scripts/jsd/main.js';</script></body>`)
		if got := ChallengeReason(body); got != "" {
			t.Fatalf("%q misclassified as %s", title, got)
		}
	}
	body := []byte(`<title>Checking connection</title><body><script>window._cf_chl_opt={};</script></body>`)
	if got := ChallengeReason(body); got == "" {
		t.Fatal("actual challenge was accepted")
	}
}
func TestPostSearchPreservesSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
			fmt.Fprint(w, "ready")
			return
		}
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "ok" || r.Header.Get("Origin") == "" || r.Referer() == "" {
			http.Error(w, "refused", 403)
			return
		}
		fmt.Fprint(w, "results")
	}))
	defer srv.Close()
	f := New("", "test")
	f.HostGap = 0
	u, _ := url.Parse(srv.URL)
	if _, err := f.Get(context.Background(), u, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.PostForm(context.Background(), srv.URL, url.Values{"q": {"book"}}); err != nil {
		t.Fatal(err)
	}
}
func TestNoStoreBypassesExistingCacheAndOverridesHeader(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 && (r.UserAgent() != "custom" || len(r.Header.Values("User-Agent")) != 1) {
			t.Error("header was appended instead of replaced")
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	f := New(t.TempDir(), "test")
	f.HostGap = 0
	u, _ := url.Parse(srv.URL)
	f.Get(context.Background(), u, Options{})
	if _, err := f.Get(context.Background(), u, Options{NoStore: true, Header: http.Header{"User-Agent": {"custom"}}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("NoStore reused stale download key")
	}
}
