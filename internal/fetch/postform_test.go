package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPostFormSendsBodyAndIdentity(t *testing.T) {
	var method, ua, ct string
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method, ua, ct, got = r.Method, r.UserAgent(), r.Header.Get("Content-Type"), r.PostForm
		fmt.Fprint(w, "<html>results</html>")
	}))
	defer srv.Close()
	f := New("", "test")
	f.HostGap = 0
	resp, err := f.PostForm(context.Background(), srv.URL+"/find", url.Values{"q": {"dracula"}, "lang": {"en"}})
	if err != nil {
		t.Fatal(err)
	}
	if method != "POST" || ct != "application/x-www-form-urlencoded" || got.Get("q") != "dracula" || got.Get("lang") != "en" {
		t.Errorf("method=%s ct=%s form=%v", method, ct, got)
	}
	if ua != f.UserAgent || string(resp.Body) != "<html>results</html>" || resp.URL.Path != "/find" {
		t.Errorf("ua=%q body=%q url=%v", ua, resp.Body, resp.URL)
	}
	f.Offline = true
	if _, err := f.PostForm(context.Background(), srv.URL+"/find", nil); err == nil {
		t.Error("offline must not post")
	}
}

// DuckDuckGo shows a verification page to readers that ask too quickly.
func TestSearchEngineHasPoliteGap(t *testing.T) {
	f := New("", "test")
	if f.HostGaps["duckduckgo.com"] < 3e9 {
		t.Errorf("duckduckgo gap = %v", f.HostGaps["duckduckgo.com"])
	}
}
