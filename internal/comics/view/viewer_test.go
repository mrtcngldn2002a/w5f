package view

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strconv"
	"testing"
)

// memPages are pages in memory; bad marks pages that do not decode.
type memPages struct {
	sizes []image.Point
	bad   map[int]bool
}

func (m *memPages) Len() int          { return len(m.sizes) }
func (m *memPages) Name(i int) string { return strconv.Itoa(i) + ".png" }
func (m *memPages) Close() error      { return nil }
func (m *memPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(m.sizes) {
		return nil, errors.New("no page")
	}
	if m.bad[i] {
		return io.NopCloser(bytes.NewReader([]byte("not an image"))), nil
	}
	img := image.NewRGBA(image.Rectangle{Max: m.sizes[i]})
	for p := 0; p < len(img.Pix); p += 4 {
		img.Pix[p], img.Pix[p+3] = uint8(i*20), 0xff
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return io.NopCloser(&b), nil
}

type fakeDisplay struct {
	size   image.Point
	keys   chan Key
	frames []*image.RGBA
}

func (f *fakeDisplay) Size() image.Point { return f.size }
func (f *fakeDisplay) Keys() <-chan Key  { return f.keys }
func (f *fakeDisplay) Close() error      { return nil }
func (f *fakeDisplay) Show(fr *image.RGBA) error {
	c := image.NewRGBA(fr.Bounds())
	copy(c.Pix, fr.Pix)
	f.frames = append(f.frames, c)
	return nil
}

func run(t *testing.T, b Book, neighbor func(int) (Book, error), keys ...Key) (*fakeDisplay, int, []int) {
	t.Helper()
	d := &fakeDisplay{size: image.Pt(128, 80), keys: make(chan Key, len(keys)+1)}
	var progress []int
	b.OnPage = func(p, n int) { progress = append(progress, p) }
	for _, k := range keys {
		d.keys <- k
	}
	d.keys <- "q"
	last, err := New(d, b, neighbor).Run()
	if err != nil {
		t.Fatal(err)
	}
	return d, last, progress
}

func tall(n int) *memPages {
	m := &memPages{bad: map[int]bool{}}
	for i := 0; i < n; i++ {
		m.sizes = append(m.sizes, image.Pt(40, 60))
	}
	return m
}

func TestTurningPagesSavesProgress(t *testing.T) {
	_, last, progress := run(t, Book{Pages: tall(5)}, nil, "space", "space", "b", "right", "g", "5", "enter", "space")
	if last != 4 {
		t.Errorf("left on page %d", last)
	}
	want := []int{1, 2, 1, 2, 4, 4} // …, the last one is the report on quit
	if len(progress) != len(want) {
		t.Fatalf("progress %v", progress)
	}
	for i := range want {
		if progress[i] != want[i] {
			t.Fatalf("progress %v, want %v", progress, want)
		}
	}
}

func TestRightToLeftArrows(t *testing.T) {
	_, last, _ := run(t, Book{Pages: tall(4), RTL: true}, nil, "left", "left", "right")
	if last != 1 {
		t.Errorf("in RTL ← goes forward: page %d", last)
	}
}

func TestFitWidthScrollsBeforeTurning(t *testing.T) {
	// fit width on a 128×80 screen makes a 40×60 page 128×192: two scrolls.
	_, last, progress := run(t, Book{Pages: tall(3)}, nil, "f", "space", "space", "space")
	if last != 1 || len(progress) != 2 {
		t.Errorf("space scrolls first: page %d, turns %v", last, progress)
	}
}

func TestNextIssueAndBrokenPage(t *testing.T) {
	second := tall(2)
	second.bad[0] = true
	calls := 0
	d, last, _ := run(t, Book{Title: "one", Pages: tall(1)}, func(dir int) (Book, error) {
		calls++
		if dir < 0 {
			return Book{}, errors.New("no earlier issue")
		}
		return Book{Title: "two", Pages: second}, nil
	}, "[", "]")
	if calls != 2 || last != 0 {
		t.Fatalf("neighbor calls %d, page %d", calls, last)
	}
	// The broken page is drawn as a message on black, not a crash.
	fr := d.frames[len(d.frames)-1]
	lit := 0
	for p := 0; p < len(fr.Pix); p += 4 {
		if fr.Pix[p] == 0xff && fr.Pix[p+1] == 0xb0 {
			lit++
		}
	}
	if lit == 0 {
		t.Error("an error message is shown for a page that does not decode")
	}
	_ = color.Black
}

func TestTwoPageView(t *testing.T) {
	d, _, progress := run(t, Book{Pages: tall(5)}, nil, "d", "space", "space")
	if len(progress) != 3 || progress[0] != 2 || progress[1] != 4 {
		t.Errorf("spreads: cover, 1–2, 3–4: %v", progress)
	}
	if len(d.frames) == 0 {
		t.Error("nothing drawn")
	}
}
