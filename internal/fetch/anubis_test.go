package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func anubisPage(data string, difficulty int) string {
	return fmt.Sprintf(`<html><head><title>Making sure you're not a bot!</title>
<script id="anubis_version" type="application/json">"v1.27.0"</script>
<script id="anubis_challenge" type="application/json">{"rules":{"algorithm":"fast","difficulty":%d},"challenge":{"id":"c-1","randomData":%q}}</script>
<script id="anubis_base_prefix" type="application/json">""</script></head><body>Loading…</body></html>`, difficulty, data)
}

// A fake Anubis: the page asks for the work, pass-challenge checks it the
// way the server does and sets the pass cookie.
func anubisServer(t *testing.T, difficulty int) (*httptest.Server, *int) {
	passes := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.within.website/x/cmd/anubis/api/pass-challenge" {
			q := r.URL.Query()
			sum := sha256.Sum256([]byte("abc123" + q.Get("nonce")))
			if q.Get("id") != "c-1" || hex.EncodeToString(sum[:]) != q.Get("response") || !strings.HasPrefix(q.Get("response"), strings.Repeat("0", difficulty)) {
				w.WriteHeader(403)
				return
			}
			*passes++
			http.SetCookie(w, &http.Cookie{Name: "anubis-auth", Value: "ok", Path: "/"})
			http.Redirect(w, r, q.Get("redir"), http.StatusFound)
			return
		}
		if c, err := r.Cookie("anubis-auth"); err == nil && c.Value == "ok" {
			fmt.Fprint(w, "<title>Reading</title><p>The text itself.</p>")
			return
		}
		fmt.Fprint(w, anubisPage("abc123", difficulty))
	}))
	t.Cleanup(srv.Close)
	return srv, passes
}

func TestAnubisWorkMatchesTheWorker(t *testing.T) {
	for _, d := range []int{1, 3, 4} {
		hash, nonce, err := anubisWork(context.Background(), "abc123", d)
		sum := sha256.Sum256([]byte(fmt.Sprintf("abc123%d", nonce)))
		if err != nil || hash != hex.EncodeToString(sum[:]) || !strings.HasPrefix(hash, strings.Repeat("0", d)) {
			t.Fatalf("difficulty %d: %s %d %v", d, hash, nonce, err)
		}
	}
}

func TestAnubisPassThenPage(t *testing.T) {
	srv, passes := anubisServer(t, 4)
	f := New(t.TempDir(), "test")
	f.HostGap, f.SolverURL = 0, ""
	u, _ := url.Parse(srv.URL + "/texts/a?x=1")
	r, err := f.Get(context.Background(), u, Options{})
	if err != nil || !strings.Contains(string(r.Body), "The text itself") || *passes != 1 {
		t.Fatalf("%v %v passes=%d", r, err, *passes)
	}
	if r.URL.RequestURI() != "/texts/a?x=1" {
		t.Fatalf("landed on %s", r.URL)
	}
}

func TestAnubisLimits(t *testing.T) {
	srv, passes := anubisServer(t, anubisMaxDifficulty+1)
	f := New(t.TempDir(), "test")
	f.HostGap, f.SolverURL = 0, ""
	u, _ := url.Parse(srv.URL + "/")
	_, err := f.Get(context.Background(), u, Options{})
	var ce *ChallengeError
	if !errors.As(err, &ce) || *passes != 0 {
		t.Fatalf("too hard a wall was worked on: %v passes=%d", err, *passes)
	}
	if _, err := parseAnubis([]byte(strings.Replace(anubisPage("x", 2), `"fast"`, `"metarefresh"`, 1))); err == nil {
		t.Fatal("an unknown algorithm was accepted")
	}
}
