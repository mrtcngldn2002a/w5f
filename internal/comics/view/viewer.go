package view

import (
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
	"strings"
	"sync"
	"time"

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

// size is a page's pixel size (decoded on demand).
func (v *Viewer) size(i int) image.Point {
	if s, ok := v.sizes[i]; ok {
		return s
	}
	img, err := v.cache.get(i)
	if err != nil {
		return image.Pt(1, 1)
	}
	v.sizes[i] = img.Bounds().Size()
	return v.sizes[i]
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
	go v.cache.prefetch(vw[len(vw)-1] + 1)
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
		if err != nil {
			v.text(fmt.Sprintf("page %d cannot be shown: %v", p.Page+1, err), image.Pt(20, sc.Y/2))
			continue
		}
		draw.Draw(v.frame, p.Dst, img, p.Src.Min, draw.Src)
	}
	if v.overlay != "" {
		v.text(v.overlay, image.Pt(12, sc.Y-12))
	}
	return v.d.Show(v.frame)
}

func (v *Viewer) text(s string, at image.Point) {
	face := basicfont.Face7x13
	w := font.MeasureString(face, s).Ceil()
	bg := image.Rect(at.X-6, at.Y-15, at.X+w+6, at.Y+6)
	draw.Draw(v.frame, bg, image.NewUniform(color.RGBA{0x12, 0x0c, 0x02, 0xff}), image.Point{}, draw.Src)
	(&font.Drawer{Dst: v.frame, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(at.X, at.Y)}).DrawString(s)
}

// pageCache keeps decoded pages and their scaled copies (a few of each).
type pageCache struct {
	mu      sync.Mutex
	pages   comics.Pages
	decoded map[int]image.Image
	order   []int
	scaledM map[string]*image.RGBA
}

func newPageCache(p comics.Pages) *pageCache {
	return &pageCache{pages: p, decoded: map[int]image.Image{}, scaledM: map[string]*image.RGBA{}}
}

const keepDecoded = 2 // the page shown and the one prefetched (a 1600×2400 page is 6–15 MB decoded)

func (c *pageCache) get(i int) (image.Image, error) {
	c.mu.Lock()
	if img, ok := c.decoded[i]; ok {
		c.mu.Unlock()
		return img, nil
	}
	c.mu.Unlock()
	rc, err := c.pages.Open(i)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(io.LimitReader(rc, 64<<20))
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %v", c.pages.Name(i), err)
	}
	c.mu.Lock()
	c.decoded[i] = img
	c.order = append(c.order, i)
	for len(c.order) > keepDecoded {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.decoded, old)
		for k := range c.scaledM {
			if pageOf(k) == old {
				delete(c.scaledM, k)
			}
		}
	}
	c.mu.Unlock()
	return img, nil
}

func key(i int, sz image.Point) string { return fmt.Sprintf("%d:%dx%d", i, sz.X, sz.Y) }

func pageOf(k string) int {
	page, _, _ := strings.Cut(k, ":")
	n, _ := strconv.Atoi(page)
	return n
}

func (c *pageCache) scaled(i int, sz image.Point) (*image.RGBA, error) {
	c.mu.Lock()
	if img, ok := c.scaledM[key(i, sz)]; ok {
		c.mu.Unlock()
		return img, nil
	}
	c.mu.Unlock()
	src, err := c.get(i)
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rectangle{Max: sz})
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	c.mu.Lock()
	c.scaledM[key(i, sz)] = dst
	c.mu.Unlock()
	return dst, nil
}

// prefetch decodes the page after the view in the background.
func (c *pageCache) prefetch(i int) {
	if i >= 0 && i < c.pages.Len() {
		_, _ = c.get(i)
	}
}
