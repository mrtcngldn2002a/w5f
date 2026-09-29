package view

import (
	"image"
	"testing"

	"github.com/jezek/xgb/xproto"
)

var screen = image.Pt(1280, 800)

func TestFitModes(t *testing.T) {
	page := image.Pt(1600, 2400)
	p := Layout([]int{3}, []image.Point{page}, screen, FitPage, false, 0)[0]
	if p.Size != image.Pt(533, 800) || p.Dst.Min.X != (1280-533)/2 || p.Dst.Dy() != 800 {
		t.Errorf("fit page: %+v", p)
	}
	w := Layout([]int{3}, []image.Point{page}, screen, FitWidth, false, 500)[0]
	if w.Size != image.Pt(1280, 1920) || w.Src.Min.Y != 500 || w.Dst.Dy() != 800 {
		t.Errorf("fit width: %+v", w)
	}
	end := Layout([]int{3}, []image.Point{page}, screen, FitWidth, false, 99999)[0]
	if end.Src.Min.Y != 1920-800 {
		t.Errorf("scroll stops at the bottom: %+v", end)
	}
	wide := Layout([]int{0}, []image.Point{image.Pt(3000, 1000)}, screen, FitHeight, false, 0)[0]
	if wide.Size.Y != 800 || wide.Dst.Dx() != 1280 || wide.Src.Min.X != (wide.Size.X-1280)/2 {
		t.Errorf("fit height of a wide page shows its middle: %+v", wide)
	}
	if bad := Layout([]int{0}, []image.Point{{}}, screen, FitPage, false, 0)[0]; bad.Size != (image.Point{}) {
		t.Errorf("a page without size: %+v", bad)
	}
}

func TestSpreadsAndRTL(t *testing.T) {
	sizes := map[int]image.Point{5: image.Pt(3200, 2400)} // page 5 is a two-page spread
	size := func(i int) image.Point {
		if s, ok := sizes[i]; ok {
			return s
		}
		return image.Pt(1600, 2400)
	}
	n := 9
	var views [][]int
	for i := 0; i < n; {
		v := Spread(i, n, true, size)
		views = append(views, v)
		i += len(v)
	}
	want := [][]int{{0}, {1, 2}, {3, 4}, {5}, {6, 7}, {8}}
	if len(views) != len(want) {
		t.Fatalf("views %v", views)
	}
	for i := range want {
		if len(views[i]) != len(want[i]) || views[i][0] != want[i][0] {
			t.Fatalf("views %v, want %v", views, want)
		}
	}
	for i, v := range want[1:] {
		if got := PrevStart(v[0], true, size); got != want[i][0] {
			t.Errorf("before %d comes %d, got %d", v[0], want[i][0], got)
		}
	}
	if PrevStart(4, false, size) != 3 || PrevStart(0, true, size) != 0 {
		t.Error("single-page and first-page steps")
	}
	pl := Layout([]int{1, 2}, []image.Point{size(1), size(2)}, screen, FitPage, true, 0)
	if pl[0].Page != 2 || pl[1].Page != 1 || pl[0].Dst.Min.X >= pl[1].Dst.Min.X {
		t.Errorf("right to left puts the first page on the right: %+v", pl)
	}
	if pl := Layout([]int{1, 2}, []image.Point{size(1), size(2)}, screen, FitPage, false, 0); pl[0].Page != 1 {
		t.Errorf("left to right: %+v", pl)
	}
}

func TestKeysymNames(t *testing.T) {
	for sym, want := range map[uint32]Key{0x20: "space", 0x71: "q", 0x51: "q", 0x5d: "]", 0x2bb: "[", 0xfc: "]", 0xff51: "left", 0xff1b: "esc", 0xffb3: "3", 0xff0d: "enter", 0x1234: ""} {
		if got := keysymName(xproto.Keysym(sym)); got != want {
			t.Errorf("keysym %#x: %q, want %q", sym, got, want)
		}
	}
}
