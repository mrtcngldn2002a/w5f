// Package view is W5F's comics viewer: one full-screen X11 window, pages
// scaled to fit, right-to-left and two-page spreads, keys in W5F's
// lowercase language, progress saved on every page turn.
package view

import "image"

// Fit is how a page is scaled to the screen.
type Fit int

const (
	FitPage   Fit = iota // the whole page on screen
	FitWidth             // page as wide as the screen, scrolled down
	FitHeight            // page as tall as the screen
)

func (f Fit) String() string { return [...]string{"fit page", "fit width", "fit height"}[f] }

// Next cycles the fit modes.
func (f Fit) Next() Fit { return (f + 1) % 3 }

// scaled is the size of a w×h page drawn into a box with a fit mode.
func scaled(w, h, boxW, boxH int, fit Fit) (int, int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	sx := float64(boxW) / float64(w)
	sy := float64(boxH) / float64(h)
	s := sx
	switch fit {
	case FitPage:
		s = min(sx, sy)
	case FitHeight:
		s = sy
	}
	return max(1, int(float64(w)*s+0.5)), max(1, int(float64(h)*s+0.5))
}

// Placed is one page's spot on the screen (Dst), and the part of the scaled
// page shown (Src, for pages taller than the screen).
type Placed struct {
	Page     int
	Size     image.Point // scaled size
	Dst, Src image.Rectangle
}

// Layout places the pages of one view (one page, or a spread) on a screen.
// scroll is how far a tall page is scrolled down (fit width).
func Layout(pages []int, sizes []image.Point, screen image.Point, fit Fit, rtl bool, scroll int) []Placed {
	if len(pages) == 2 {
		box := image.Pt(screen.X/2, screen.Y)
		var out []Placed
		total := 0
		var ws [2]image.Point
		for i := range pages {
			w, h := scaled(sizes[i].X, sizes[i].Y, box.X, box.Y, FitPage)
			ws[i] = image.Pt(w, h)
			total += w
		}
		x := (screen.X - total) / 2
		order := []int{0, 1}
		if rtl {
			order = []int{1, 0}
		}
		for _, i := range order {
			sz := ws[i]
			y := (screen.Y - sz.Y) / 2
			out = append(out, Placed{Page: pages[i], Size: sz, Dst: image.Rect(x, y, x+sz.X, y+sz.Y), Src: image.Rect(0, 0, sz.X, sz.Y)})
			x += sz.X
		}
		return out
	}
	w, h := scaled(sizes[0].X, sizes[0].Y, screen.X, screen.Y, fit)
	scroll = clampScroll(scroll, h, screen.Y)
	vis := image.Pt(min(w, screen.X), min(h, screen.Y))
	x := (screen.X - vis.X) / 2
	y := (screen.Y - vis.Y) / 2
	sx := (w - vis.X) / 2 // a page wider than the screen (fit height) shows its middle
	return []Placed{{Page: pages[0], Size: image.Pt(w, h), Dst: image.Rect(x, y, x+vis.X, y+vis.Y),
		Src: image.Rect(sx, scroll, sx+vis.X, scroll+vis.Y)}}
}

func clampScroll(scroll, pageH, screenH int) int {
	return max(0, min(scroll, pageH-screenH))
}

// Wide reports a landscape page (a spread already), shown alone.
func Wide(sz image.Point) bool { return sz.X > sz.Y }

// Spread picks the pages of the view starting at i: in double mode the
// cover (page 0) and wide pages stand alone, others pair up.
func Spread(i, n int, double bool, size func(int) image.Point) []int {
	if i < 0 || i >= n {
		return nil
	}
	if !double || i == 0 || i == n-1 || Wide(size(i)) || Wide(size(i+1)) {
		return []int{i}
	}
	return []int{i, i + 1}
}

// PrevStart is where the view before the one at i starts.
func PrevStart(i int, double bool, size func(int) image.Point) int {
	if i <= 0 {
		return 0
	}
	if !double {
		return i - 1
	}
	// Walk from the start: spreads depend on what came before.
	start := 0
	for n := i + 1; ; {
		next := start + len(Spread(start, n, true, size))
		if next >= i {
			return start
		}
		start = next
	}
}
