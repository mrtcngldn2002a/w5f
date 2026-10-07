package view

import (
	"bufio"
	"image"
	"image/jpeg"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The browser tab, played by an HTTP client: it says its size, hears of
// frames, fetches them and sends keys; the viewer turns pages and quits.
func TestWebViewerInTheBrowser(t *testing.T) {
	w, err := OpenWeb(`Saga <1> & "two"`)
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *http.Response {
		t.Helper()
		resp, err := http.Get(w.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	post := func(path, body string) int {
		t.Helper()
		resp, err := http.Post(w.URL+path, "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	page := get("")
	var html strings.Builder
	bufio.NewReader(page.Body).WriteTo(&html)
	page.Body.Close()
	if !strings.Contains(html.String(), "<title>Saga &lt;1&gt; &amp; &#34;two&#34; — W5F</title>") || !strings.Contains(html.String(), "EventSource") {
		t.Fatalf("the page: %.300s", html.String())
	}

	if err := w.WaitReady(50 * time.Millisecond); err == nil {
		t.Fatal("ready before the page said its size")
	}
	if c := post("size", "640 400"); c != http.StatusNoContent {
		t.Fatalf("size: %d", c)
	}
	if err := w.WaitReady(time.Second); err != nil || w.Size() != image.Pt(640, 400) || w.Scale() != 1 {
		t.Fatalf("ready: %v, size %v, scale %d", err, w.Size(), w.Scale())
	}
	if k := <-w.Keys(); k != "expose" {
		t.Fatalf("a new size redraws: %q", k)
	}
	// A Retina screen: twice the pixels, and the notes twice as large.
	post("size", "1280 800 2")
	if w.Size() != image.Pt(1280, 800) || w.Scale() != 2 {
		t.Errorf("retina: size %v scale %d", w.Size(), w.Scale())
	}
	<-w.Keys()
	post("size", "640 400 1")
	<-w.Keys()

	events := get("events")
	lines := bufio.NewReader(events.Body)
	next := func() string {
		t.Helper()
		for {
			l, err := lines.ReadString('\n')
			if err != nil {
				t.Fatalf("events: %v", err)
			}
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, ":") {
				return l
			}
		}
	}

	b := Book{Title: "Saga", Pages: &memPages{sizes: []image.Point{{60, 90}, {60, 90}, {60, 90}}}}
	var progress []int
	b.OnPage = func(p, n int) { progress = append(progress, p) }
	done := make(chan int)
	go func() {
		left, err := New(w, b, nil).Run()
		if err != nil {
			t.Error(err)
		}
		done <- left
	}()
	if l := next(); l != "data: 1" {
		t.Fatalf("first frame: %q", l)
	}
	fr := get("frame?n=1")
	img, err := jpeg.Decode(fr.Body)
	fr.Body.Close()
	if err != nil || img.Bounds().Size() != image.Pt(640, 400) || fr.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("the frame: %v %v %q", err, img, fr.Header.Get("Content-Type"))
	}

	post("key", "not a key") // ignored
	post("key", "space")
	if l := next(); l != "data: 2" {
		t.Fatalf("after space: %q", l)
	}
	post("key", "q")
	if left := <-done; left != 1 {
		t.Errorf("left on page %d, want 1", left)
	}
	if len(progress) == 0 || progress[len(progress)-1] != 1 {
		t.Errorf("progress: %v", progress)
	}
	go w.Close()
	if l := next(); l != "event: closed" {
		t.Errorf("the page hears the viewer close: %q", l)
	}
	events.Body.Close()
}

// Only its own address, its own path and its own page reach the viewer.
func TestWebViewerAnswersOnlyItsPage(t *testing.T) {
	w, err := OpenWeb("x")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if !strings.HasPrefix(w.URL, "http://127.0.0.1:") {
		t.Fatalf("not on this computer alone: %s", w.URL)
	}
	do := func(method, url, host, origin string) int {
		req, _ := http.NewRequest(method, url, strings.NewReader("q"))
		if host != "" {
			req.Host = host
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	root := strings.TrimSuffix(w.URL, w.base)
	for name, c := range map[string]int{
		"another path":        do("GET", root+"/", "", ""),
		"a guessed token":     do("POST", root+"/0123456789abcdef0123456789abcdef/key", "", ""),
		"another name for it": do("POST", w.URL+"key", "evil.example:80", ""),
		"another site's page": do("POST", w.URL+"key", "", "https://evil.example"),
	} {
		if c == http.StatusNoContent || c == http.StatusOK {
			t.Errorf("%s: answered %d", name, c)
		}
	}
	select {
	case k := <-w.Keys():
		t.Errorf("a key got through: %q", k)
	default:
	}
}

// A closed tab (no page listening for a while) closes the viewer, its
// place kept; a reload in time does not.
func TestWebViewerQuitsWhenTheTabCloses(t *testing.T) {
	old := webIdle
	webIdle = 100 * time.Millisecond
	defer func() { webIdle = old }()
	w, err := OpenWeb("x")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	listen := func() *http.Response {
		resp, err := http.Get(w.URL + "events")
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	listen().Body.Close()
	time.Sleep(30 * time.Millisecond)
	reload := listen() // back within webIdle
	select {
	case k := <-w.Keys():
		t.Fatalf("a reload closed the viewer: %q", k)
	case <-time.After(250 * time.Millisecond):
	}
	reload.Body.Close()
	select {
	case k := <-w.Keys():
		if k != "q" {
			t.Errorf("key %q", k)
		}
	case <-time.After(2 * time.Second):
		t.Error("a closed tab left the viewer open")
	}
}
