package view

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"sync"
	"time"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"

	"w5f/internal/comics"
)

// Book is what the viewer shows: pages, where to start, and what to do on a
// page turn.
type Book struct {
	Title  string
	Pages  comics.Pages
	RTL    bool
	Start  int
	OnPage func(page, pages int) // progress; called on every turn and at the end
}

// Key is a key press, already named: "space", "q", "left", "5", …
type Key string

// Display is where frames go (an X11 window; a fake in tests).
type Display interface {
	Size() image.Point
	Show(frame *image.RGBA) error
	Keys() <-chan Key
	Close() error
}

// scaler is a display whose pixels are smaller than a screen's usual ones
// (a Retina screen in a browser tab): its text is drawn Scale() times larger.
type scaler interface{ Scale() int }

// scale is how many frame pixels make one pixel of the viewer's text.
func (v *Viewer) scale() int {
	if s, ok := v.d.(scaler); ok {
		return max(1, min(s.Scale(), 4))
	}
	return 1
}

// Viewer shows a book on a display.
type Viewer struct {
	d        Display
	book     Book
	neighbor func(dir int) (Book, error) // the next/previous issue ([ and ])
	fit      Fit
	double   bool
	pos      int // first page of the view
	scroll   int
	sizes    map[int]image.Point
	cache    *pageCache
	frame    *image.RGBA
	overlay  string
	overlayT time.Time
	goto_    string
}

// New prepares a viewer. neighbor may be nil.
func New(d Display, b Book, neighbor func(dir int) (Book, error)) *Viewer {
	v := &Viewer{d: d, neighbor: neighbor}
	v.load(b)
	return v
}

func (v *Viewer) load(b Book) {
	if v.book.Pages != nil && v.book.Pages != b.Pages {
		v.book.Pages.Close()
	}
	v.book = b
	v.pos = max(0, min(b.Start, b.Pages.Len()-1))
	v.scroll = 0
	v.sizes = map[int]image.Point{}
	v.cache = newPageCache(b.Pages)
	v.flash(b.Title)
}

func (v *Viewer) flash(s string) { v.overlay, v.overlayT = s, time.Now() }

// size is a page's pixel size, read from the image header.
func (v *Viewer) size(i int) image.Point {
	if s, ok := v.sizes[i]; ok {
		return s
	}
	sz, err := v.cache.size(i)
	if err != nil {
		sz = image.Pt(1, 1)
	}
	v.sizes[i] = sz
	return sz
}

func (v *Viewer) view() []int {
	return Spread(v.pos, v.book.Pages.Len(), v.double, v.size)
}

// Run shows the book until q or esc; it returns the page left on.
func (v *Viewer) Run() (int, error) {
	defer func() { v.report() }()
	if err := v.draw(); err != nil {
		return v.pos, err
	}
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case k, ok := <-v.d.Keys():
			if !ok {
				return v.pos, nil
			}
			quit, err := v.key(k)
			if err != nil {
				v.flash(err.Error())
			}
			if quit {
				return v.pos, nil
			}
			if err := v.draw(); err != nil {
				return v.pos, err
			}
		case <-tick.C:
			if v.overlay != "" && time.Since(v.overlayT) > 1500*time.Millisecond {
				v.overlay = ""
				if err := v.draw(); err != nil {
					return v.pos, err
				}
			}
		}
	}
}

func (v *Viewer) report() {
	if v.book.OnPage != nil {
		last := v.pos
		if vw := v.view(); len(vw) > 0 {
			last = vw[len(vw)-1]
		}
		v.book.OnPage(last, v.book.Pages.Len())
	}
}

// key handles one key; it reports whether to quit.
func (v *Viewer) key(k Key) (bool, error) {
	n := v.book.Pages.Len()
	if v.goto_ != "" { // g, then a page number, then enter
		switch {
		case len(k) == 1 && k >= "0" && k <= "9":
			v.goto_ += string(k)
			v.flash("go to page " + v.goto_[1:])
			return false, nil
		case k == "enter":
			p, err := strconv.Atoi(v.goto_[1:])
			v.goto_ = ""
			if err == nil && p >= 1 && p <= n {
				v.pos, v.scroll = p-1, 0
				v.turned()
			}
			return false, nil
		default:
			v.goto_ = ""
		}
	}
	forward, back := "right", "left"
	if v.book.RTL {
		forward, back = "left", "right"
	}
	switch k {
	case "q", "esc":
		return true, nil
	case "space", "pgdown", "down", "j", Key(forward):
		v.step(1, k != Key(forward))
	case "b", "pgup", "up", "k", Key(back):
		v.step(-1, k != Key(back))
	case "home":
		v.pos, v.scroll = 0, 0
		v.turned()
	case "end":
		v.pos, v.scroll = PrevStart(n, v.double, v.size), 0
		v.turned()
	case "f":
		v.fit = v.fit.Next()
		v.scroll = 0
		v.flash(v.fit.String())
	case "d":
		v.double = !v.double
		if v.double {
			v.flash("two pages")
		} else {
			v.flash("one page")
		}
	case "r":
		v.book.RTL = !v.book.RTL
		v.flash(map[bool]string{true: "right to left", false: "left to right"}[v.book.RTL])
	case "g":
		v.goto_ = "g"
		v.flash("go to page …")
	case "]", "[", "n", "p": // n / p too: [ and ] need AltGr on some keyboards
		if v.neighbor == nil {
			return false, errors.New("no other issue")
		}
		dir := 1
		if k == "[" || k == "p" {
			dir = -1
		}
		v.report()
		b, err := v.neighbor(dir)
		if err != nil {
			return false, err
		}
		v.load(b)
	}
	return false, nil
}

// step moves a screen (scrolling a tall page first when scroll is allowed).
func (v *Viewer) step(dir int, scroll bool) {
	vw := v.view()
	if scroll && len(vw) == 1 && v.fit != FitPage {
		sc := v.d.Size()
		pl := Layout(vw, []image.Point{v.size(vw[0])}, sc, v.fit, v.book.RTL, v.scroll)[0]
		if dir > 0 && pl.Src.Max.Y < pl.Size.Y {
			v.scroll = pl.Src.Min.Y + sc.Y*9/10
			return
		}
		if dir < 0 && pl.Src.Min.Y > 0 {
			v.scroll = max(0, pl.Src.Min.Y-sc.Y*9/10)
			return
		}
	}
	n := v.book.Pages.Len()
	if dir > 0 {
		next := v.pos + len(vw)
		if next >= n {
			v.flash("last page — ] for the next issue, q to close")
			return
		}
		v.pos, v.scroll = next, 0
	} else {
		if v.pos == 0 {
			v.flash("first page")
			return
		}
		v.pos = PrevStart(v.pos, v.double, v.size)
		v.scroll = 0
		if v.fit != FitPage { // coming back up shows the bottom of the page
			v.scroll = 1 << 30
		}
	}
	v.turned()
}

func (v *Viewer) turned() {
	vw := v.view()
	label := strconv.Itoa(vw[0] + 1)
	if len(vw) == 2 {
		label += "–" + strconv.Itoa(vw[1]+1)
	}
	v.flash(fmt.Sprintf("%s / %d", label, v.book.Pages.Len()))
	if v.book.OnPage != nil {
		v.book.OnPage(vw[len(vw)-1], v.book.Pages.Len())
	}
	v.prefetch()
}

var ink = color.RGBA{0xff, 0xb0, 0x00, 0xff} // Amber P3

// draw composes the current view into a frame and shows it.
func (v *Viewer) draw() error {
	sc := v.d.Size()
	if v.frame == nil || v.frame.Bounds().Size() != sc {
		v.frame = image.NewRGBA(image.Rectangle{Max: sc})
	}
	draw.Draw(v.frame, v.frame.Bounds(), image.Black, image.Point{}, draw.Src)
	vw := v.view()
	var sizes []image.Point
	for _, i := range vw {
		sizes = append(sizes, v.size(i))
	}
	placed := Layout(vw, sizes, sc, v.fit, v.book.RTL, v.scroll)
	if len(placed) == 1 {
		v.scroll = placed[0].Src.Min.Y
	}
	for _, p := range placed {
		img, err := v.cache.scaled(p.Page, p.Size)
		if err == nil {
			defer v.prefetch() // the next view gets ready while this one is read
		}
		if err != nil {
			v.text(fmt.Sprintf("page %d cannot be shown: %v", p.Page+1, err), image.Pt(20*v.scale(), sc.Y/2))
			continue
		}
		draw.Draw(v.frame, p.Dst, img, p.Src.Min, draw.Src)
	}
	if v.overlay != "" {
		k := v.scale()
		v.text(v.overlay, image.Pt(12*k, sc.Y-12*k))
	}
	return v.d.Show(v.frame)
}

// text writes s on a dark box, its baseline starting at at; on a scaled
// display the box and its letters are drawn larger, pixel for pixel.
func (v *Viewer) text(s string, at image.Point) {
	face := basicfont.Face7x13
	w := font.MeasureString(face, s).Ceil()
	box := image.NewRGBA(image.Rect(0, 0, w+12, 21))
	draw.Draw(box, box.Bounds(), image.NewUniform(color.RGBA{0x12, 0x0c, 0x02, 0xff}), image.Point{}, draw.Src)
	(&font.Drawer{Dst: box, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(6, 15)}).DrawString(s)
	k := v.scale()
	dst := image.Rect(at.X-6*k, at.Y-15*k, at.X+(w+6)*k, at.Y+6*k)
	xdraw.NearestNeighbor.Scale(v.frame, dst, box, box.Bounds(), draw.Src, nil)
}

// prefetch gets the next view ready (decoded and scaled) in the background:
// a big page takes about half a second to decode on the W5F laptop, which
// the reader spends reading the current one.
func (v *Viewer) prefetch() {
	vw := v.view()
	if len(vw) == 0 {
		return
	}
	// Everything, even the next pages' sizes, is worked out in the
	// background: in a solid archive (CB7, CBR) reading a page's header
	// means decompressing it, which must not hold up the page on screen.
	c, n, start := v.cache, v.book.Pages.Len(), vw[len(vw)-1]+1
	double, screen, fit, rtl := v.double, v.d.Size(), v.fit, v.book.RTL
	go func() {
		size := func(i int) image.Point {
			sz, err := c.size(i)
			if err != nil {
				return image.Pt(1, 1)
			}
			return sz
		}
		next := Spread(start, n, double, size)
		if len(next) == 0 {
			return
		}
		var sizes []image.Point
		for _, i := range next {
			sizes = append(sizes, size(i))
		}
		for _, p := range Layout(next, sizes, screen, fit, rtl, 0) {
			c.scaled(p.Page, p.Size)
		}
	}()
}

// pageCache keeps a few pages' bytes and their screen-sized copies; full
// decoded pages (up to 15 MB each) are dropped as soon as they are scaled.
type pageCache struct {
	mu       sync.Mutex
	pages    comics.Pages
	raw      map[int][]byte
	rawOrder []int
	sizes    map[int]image.Point
	scaledM  map[string]*image.RGBA
	order    []string
	inflight map[string]chan struct{}
}

func newPageCache(p comics.Pages) *pageCache {
	return &pageCache{pages: p, raw: map[int][]byte{}, sizes: map[int]image.Point{},
		scaledM: map[string]*image.RGBA{}, inflight: map[string]chan struct{}{}}
}

const (
	keepRaw    = 3 // compressed pages (about 1 MB each)
	keepScaled = 3 // screen-sized pages (about 2 MB each): shown, next, previous
)

// bytes reads a page once (from the archive or the server).
func (c *pageCache) bytes(i int) ([]byte, error) {
	c.mu.Lock()
	if b, ok := c.raw[i]; ok {
		c.mu.Unlock()
		return b, nil
	}
	c.mu.Unlock()
	rc, err := c.pages.Open(i)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	rc.Close()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.raw[i] = b
	c.rawOrder = append(c.rawOrder, i)
	for len(c.rawOrder) > keepRaw {
		delete(c.raw, c.rawOrder[0])
		c.rawOrder = c.rawOrder[1:]
	}
	c.mu.Unlock()
	return b, nil
}

// size reads a page's size from its header, without decoding it.
func (c *pageCache) size(i int) (image.Point, error) {
	c.mu.Lock()
	if s, ok := c.sizes[i]; ok {
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()
	b, err := c.bytes(i)
	if err != nil {
		return image.Point{}, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return image.Point{}, fmt.Errorf("%s: %v", c.pages.Name(i), err)
	}
	sz := image.Pt(cfg.Width, cfg.Height)
	c.mu.Lock()
	c.sizes[i] = sz
	c.mu.Unlock()
	return sz, nil
}

func key(i int, sz image.Point) string { return fmt.Sprintf("%d:%dx%d", i, sz.X, sz.Y) }

// scaled returns page i at a screen size, decoding it once; a second caller
// for the same page waits for the first instead of decoding it again.
func (c *pageCache) scaled(i int, sz image.Point) (*image.RGBA, error) {
	k := key(i, sz)
	for {
		c.mu.Lock()
		if img, ok := c.scaledM[k]; ok {
			c.mu.Unlock()
			return img, nil
		}
		wait, busy := c.inflight[k]
		if !busy {
			c.inflight[k] = make(chan struct{})
			c.mu.Unlock()
			break
		}
		c.mu.Unlock()
		<-wait
	}
	img, err := c.decodeScaled(i, sz)
	c.mu.Lock()
	close(c.inflight[k])
	delete(c.inflight, k)
	if err == nil {
		c.scaledM[k] = img
		c.order = append(c.order, k)
		for len(c.order) > keepScaled {
			delete(c.scaledM, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.mu.Unlock()
	return img, err
}

func (c *pageCache) decodeScaled(i int, sz image.Point) (*image.RGBA, error) {
	b, err := c.bytes(i)
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", c.pages.Name(i), err)
	}
	dst := image.NewRGBA(image.Rectangle{Max: sz})
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst, nil
}
