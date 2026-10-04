package view

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Web shows the viewer in a browser tab, for systems without X (a Mac,
// Windows, a Wayland-only desktop; chosen with the owner, 2026-10-04). The
// same viewer lays out and draws every frame; the page only shows the
// latest one and sends the keys back. It listens on 127.0.0.1 alone, under
// a random path, and answers only to its own address.
type Web struct {
	URL string // the page to open in the browser

	srv   *http.Server
	host  string // 127.0.0.1:port, the only Host it answers
	base  string // /<token>/
	keys  chan Key
	ready chan struct{} // closed when the page has said its size
	once  sync.Once

	mu      sync.Mutex
	title   string
	size    image.Point
	scale   int // device pixels to a CSS pixel, as the page says (1 or 2)
	frame   []byte
	seq     int
	changed chan struct{} // closed and replaced on every new frame
	closed  bool
	clients int
	idle    *time.Timer
}

// webIdle is how long the viewer waits for its tab to come back (a reload)
// before it takes the tab as closed and quits.
var webIdle = 10 * time.Second

// OpenWeb starts the viewer's page; open URL in a browser, then WaitReady.
func OpenWeb(title string) (*Web, error) {
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("the viewer cannot listen on this computer: %w", err)
	}
	w := &Web{
		host: ln.Addr().String(), base: "/" + hex.EncodeToString(tok) + "/",
		keys: make(chan Key, 32), ready: make(chan struct{}), changed: make(chan struct{}),
		title: title, size: image.Pt(1280, 800), scale: 1,
	}
	w.URL = "http://" + w.host + w.base
	w.srv = &http.Server{Handler: w, ReadHeaderTimeout: 10 * time.Second}
	go w.srv.Serve(ln)
	return w, nil
}

// WaitReady waits for the page to load and say how big it is.
func (w *Web) WaitReady(timeout time.Duration) error {
	select {
	case <-w.ready:
		return nil
	case <-time.After(timeout):
		return errors.New("the browser did not open the viewer's page (" + w.URL + ")")
	}
}

func (w *Web) Size() image.Point {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.size
}

func (w *Web) Keys() <-chan Key { return w.keys }

// Scale is how many frame pixels the screen has to the page's pixel: the
// viewer writes its notes that much larger, so they read the same.
func (w *Web) Scale() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scale
}

// Show keeps the frame as a JPEG for the page and tells it there is one.
func (w *Web) Show(frame *image.RGBA) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, frame, &jpeg.Options{Quality: 90}); err != nil {
		return err
	}
	w.mu.Lock()
	w.frame = buf.Bytes()
	w.seq++
	close(w.changed)
	w.changed = make(chan struct{})
	w.mu.Unlock()
	return nil
}

// Close tells the page the viewer has closed, gives it a moment to hear it,
// and stops listening.
func (w *Web) Close() error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.changed)
		w.changed = make(chan struct{})
	}
	w.mu.Unlock()
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		w.mu.Lock()
		n := w.clients
		w.mu.Unlock()
		if n == 0 {
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return w.srv.Shutdown(ctx)
}

func (w *Web) key(k Key) {
	select {
	case w.keys <- k:
	default: // the viewer is behind: a key too many is dropped, not queued forever
	}
}

// webKey reports a key name the page may send.
func webKey(k string) bool {
	switch k {
	case "space", "left", "right", "up", "down", "pgup", "pgdown", "home", "end", "esc", "enter":
		return true
	}
	return len(k) == 1 && k[0] > ' ' && k[0] < 0x7f
}

func (w *Web) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// Its own address only (no other name pointing here), and its own page.
	if r.Host != w.host || !strings.HasPrefix(r.URL.Path, w.base) {
		http.NotFound(rw, r)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+w.host {
		http.Error(rw, "forbidden", http.StatusForbidden)
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	switch path := strings.TrimPrefix(r.URL.Path, w.base); {
	case path == "" && r.Method == http.MethodGet:
		w.mu.Lock()
		title := w.title
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
		io.WriteString(rw, strings.Replace(webPage, "{{title}}", html.EscapeString(title), 1))
	case path == "frame" && r.Method == http.MethodGet:
		w.mu.Lock()
		f := w.frame
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "image/jpeg")
		rw.Write(f)
	case path == "events" && r.Method == http.MethodGet:
		w.events(rw, r)
	case path == "key" && r.Method == http.MethodPost:
		b, _ := io.ReadAll(io.LimitReader(r.Body, 16))
		if k := string(b); webKey(k) {
			w.key(Key(k))
		}
		rw.WriteHeader(http.StatusNoContent)
	case path == "size" && r.Method == http.MethodPost:
		b, _ := io.ReadAll(io.LimitReader(r.Body, 32))
		f := strings.Fields(string(b))
		if len(f) == 2 {
			f = append(f, "1") // a page from before the scale was sent
		}
		if len(f) != 3 {
			http.Error(rw, "size: width height scale", http.StatusBadRequest)
			return
		}
		x, e1 := strconv.Atoi(f[0])
		y, e2 := strconv.Atoi(f[1])
		k, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil {
			http.Error(rw, "size: width height scale", http.StatusBadRequest)
			return
		}
		w.mu.Lock()
		w.size = image.Pt(min(max(x, 200), 5120), min(max(y, 200), 3200))
		w.scale = min(max(k, 1), 2)
		w.mu.Unlock()
		w.once.Do(func() { close(w.ready) })
		w.key("expose") // draw again at the new size
		rw.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(rw, r)
	}
}

// events streams the number of each new frame to the page (server-sent
// events), and "closed" when the viewer closes. When no page has listened
// for webIdle, the tab was closed: the viewer quits, its place kept.
func (w *Web) events(rw http.ResponseWriter, r *http.Request) {
	fl, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "no streaming", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(rw, ": open\n\n") // the headers now, not with the first frame
	fl.Flush()
	w.mu.Lock()
	w.clients++
	if w.idle != nil {
		w.idle.Stop()
	}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.clients--
		if w.clients == 0 && !w.closed {
			w.idle = time.AfterFunc(webIdle, func() {
				w.mu.Lock()
				gone := w.clients == 0
				w.mu.Unlock()
				if gone {
					w.key("q")
				}
			})
		}
		w.mu.Unlock()
	}()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	sent := -1
	for {
		w.mu.Lock()
		seq, changed, closed := w.seq, w.changed, w.closed
		w.mu.Unlock()
		switch {
		case closed:
			fmt.Fprint(rw, "event: closed\ndata: 1\n\n")
			fl.Flush()
			return
		case seq != sent && seq > 0:
			fmt.Fprintf(rw, "data: %d\n\n", seq)
			fl.Flush()
			sent = seq
		}
		select {
		case <-changed:
		case <-ping.C:
			fmt.Fprint(rw, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// webPage is the viewer's tab: the latest frame, fitted to the window at
// the screen's own pixels, and the keys (and a click: left or right half)
// sent back as the viewer names them.
const webPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{title}} — W5F</title>
<style>
html,body{margin:0;height:100%;background:#000;overflow:hidden}
img{display:block;width:100vw;height:100vh;object-fit:contain;cursor:pointer}
#msg{position:fixed;inset:0;display:none;align-items:center;justify-content:center;padding:16px;
  background:#000;color:#ffb000;font:16px ui-monospace,Menlo,Consolas,monospace;text-align:center}
</style></head>
<body><img id="page" alt=""><div id="msg"></div>
<script>
const base = location.pathname, page = document.getElementById('page');
const post = (path, body) => fetch(base + path, {method: 'POST', body}).catch(() => {});
function size() {
  const r = Math.min(window.devicePixelRatio || 1, 2);
  post('size', Math.round(innerWidth * r) + ' ' + Math.round(innerHeight * r) + ' ' + Math.round(r));
}
let resizing;
addEventListener('resize', () => { clearTimeout(resizing); resizing = setTimeout(size, 150); });
size();
// One frame at a time: a newer one waits for the one loading.
let busy = false, want = 0;
function show(n) {
  want = Math.max(want, n);
  if (busy) return;
  busy = true;
  const next = new Image();
  next.onload = () => { page.src = next.src; busy = false; if (want > n) show(want); };
  next.onerror = () => { busy = false; };
  next.src = base + 'frame?n=' + n;
}
function say(text) { const m = document.getElementById('msg'); m.textContent = text; m.style.display = 'flex'; }
const events = new EventSource(base + 'events');
events.onmessage = e => show(+e.data);
events.addEventListener('closed', () => { events.close(); say('The viewer has closed; your page is kept. This tab can go.'); });
const names = {' ': 'space', ArrowLeft: 'left', ArrowRight: 'right', ArrowUp: 'up', ArrowDown: 'down',
  PageUp: 'pgup', PageDown: 'pgdown', Home: 'home', End: 'end', Escape: 'esc', Enter: 'enter',
  'ğ': '[', 'Ğ': '[', 'ü': ']', 'Ü': ']'};  // where [ and ] sit on a Turkish keyboard
addEventListener('keydown', e => {
  if (e.metaKey || e.ctrlKey || e.altKey) return;  // the browser's own shortcuts stay
  const k = names[e.key] || (e.key.length === 1 ? e.key.toLowerCase() : '');
  if (!k) return;
  e.preventDefault();
  post('key', k);
});
page.addEventListener('click', e => post('key', e.clientX < innerWidth / 2 ? 'left' : 'right'));
</script></body></html>
`
