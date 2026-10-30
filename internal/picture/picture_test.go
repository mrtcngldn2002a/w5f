package picture

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// gradient is a w x h PNG, dark at the left and light at the right.
func gradient(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8(x * 255 / (w - 1))
			img.Set(x, y, color.RGBA{v, v / 2, 0, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDecodeKeepsAspectWithinMaxSide(t *testing.T) {
	p, err := Decode(gradient(t, 2048, 1024))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := p.Size(); w != MaxSide || h != MaxSide/2 {
		t.Fatalf("size %dx%d, want %dx%d", w, h, MaxSide, MaxSide/2)
	}
}

func TestFit(t *testing.T) {
	p, _ := Decode(gradient(t, 400, 400))
	// A square picture is as many rows as half its columns.
	if c, r := p.Fit(60, 100); c != 60 || r != 30 {
		t.Fatalf("Fit(60,100) = %d,%d", c, r)
	}
	// Too tall: the rows bound it, the columns shrink with them.
	if c, r := p.Fit(60, 10); c != 20 || r != 10 {
		t.Fatalf("Fit(60,10) = %d,%d", c, r)
	}
}

func TestDitherRunsDarkToLight(t *testing.T) {
	if level(0, 3, 5, SixelLevels) != 0 || level(255, 3, 5, SixelLevels) != SixelLevels-1 {
		t.Fatal("black and white are not the ends of the ramp")
	}
	// A mid tone takes both of its neighbouring levels, about half and half.
	counts := map[int]int{}
	for y := range 8 {
		for x := range 8 {
			counts[level(128, x, y, SixelLevels)]++
		}
	}
	if len(counts) != 2 {
		t.Fatalf("mid grey dithers to %v", counts)
	}
}

func TestHalfBlockRows(t *testing.T) {
	p, _ := Decode(gradient(t, 100, 50))
	rows := p.HalfBlock(20, 5, color.RGBA{0x12, 0x0c, 0x02, 255}, color.RGBA{0xff, 0xb0, 0, 255})
	if len(rows) != 5 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if n := strings.Count(r, "▀"); n != 20 {
			t.Fatalf("a row of %d cells", n)
		}
	}
	// The left is the ramp's dark end, the right its light end.
	left, right := rows[0][:len(rows[0])/3], rows[0][len(rows[0])*2/3:]
	if !strings.Contains(left, "18;12;2m") || strings.Contains(left, "255;176;0") || !strings.Contains(right, "255;176;0") {
		t.Fatalf("row %q", rows[0])
	}
}

func TestSixel(t *testing.T) {
	p, _ := Decode(gradient(t, 100, 50))
	s := p.Sixel(64, 13, color.Black, color.White)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;64;13#0;2;0;0;0") || !strings.HasSuffix(s, "-\x1b\\") {
		t.Fatalf("sixel %q", s)
	}
	// Thirteen pixel rows are three bands of six.
	if n := strings.Count(s, "-"); n != 3 {
		t.Fatalf("%d bands", n)
	}
}
