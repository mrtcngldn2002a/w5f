// Package picture draws pictures in the terminal the way the owner chose
// (2026-10-30): one tone of the theme, dithered — in half blocks anywhere,
// in sixel where the terminal has it (the laptop's xterm, with
// decGraphicsID). The original colours are the full-screen viewer's.
package picture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // the formats pages carry
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// MaxSide is the longest side a picture is kept at, in grey: about what the
// laptop's screen shows (1280x800), at a byte a pixel.
const MaxSide = 1024

// The tones a picture is dithered to, dark to light: a half block is a big
// dot, and takes more tones to read; sixel's small dots read in four.
const (
	HalfBlockLevels = 8
	SixelLevels     = 4
)

// Picture is a decoded picture, in grey and at most MaxSide on a side.
type Picture struct {
	Gray *image.Gray
}

// Decode reads a picture (JPEG, PNG, GIF, WebP) into grey, its tones
// stretched so a yellowed plate's paper is white and its ink black.
func Decode(b []byte) (*Picture, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 40_000_000 {
		return nil, errors.New("picture too large")
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	w, h := cfg.Width, cfg.Height
	if w > MaxSide || h > MaxSide {
		if w >= h {
			w, h = MaxSide, max(1, h*MaxSide/w)
		} else {
			w, h = max(1, w*MaxSide/h), MaxSide
		}
	}
	g := image.NewGray(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(g, g.Bounds(), src, src.Bounds(), draw.Src, nil)
	stretch(g)
	return &Picture{Gray: g}, nil
}

// Spread is the standard deviation of a picture's tones (0..255) as it is,
// before any stretching: a blank page has little, a plate or a page of
// print a lot.
func Spread(b []byte) (float64, error) {
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	g := image.NewGray(image.Rect(0, 0, 160, 160*src.Bounds().Dy()/max(1, src.Bounds().Dx())+1))
	draw.ApproxBiLinear.Scale(g, g.Bounds(), src, src.Bounds(), draw.Src, nil)
	var sum, sq float64
	for _, v := range g.Pix {
		sum += float64(v)
		sq += float64(v) * float64(v)
	}
	n := float64(len(g.Pix))
	mean := sum / n
	return math.Sqrt(max(sq/n-mean*mean, 0)), nil
}

// stretch maps the 2nd and 98th percentiles of the tones to black and white.
func stretch(g *image.Gray) {
	var hist [256]int
	for _, v := range g.Pix {
		hist[v]++
	}
	n := len(g.Pix)
	lo, hi, acc := 0, 255, 0
	for i, c := range hist {
		if acc += c; acc*50 >= n {
			lo = i
			break
		}
	}
	acc = 0
	for i := 255; i >= 0; i-- {
		if acc += hist[i]; acc*50 >= n {
			hi = i
			break
		}
	}
	if hi-lo < 16 { // a flat picture: leave it be
		return
	}
	var lut [256]uint8
	for i := range lut {
		v := (i - lo) * 255 / (hi - lo)
		lut[i] = uint8(min(max(v, 0), 255))
	}
	for i, v := range g.Pix {
		g.Pix[i] = lut[v]
	}
}

// Size is the picture's size in pixels.
func (p *Picture) Size() (w, h int) {
	b := p.Gray.Bounds()
	return b.Dx(), b.Dy()
}

// Fit is the largest size in cells the picture takes within cols x rows, a
// cell holding two square pixels one above the other (a half block; the
// laptop's cells are 8x16).
func (p *Picture) Fit(cols, rows int) (int, int) {
	w, h := p.Size()
	c := cols
	r := (c*h + w) / (2 * w) // rounded
	if r > rows {
		r = rows
		c = max(1, (2*r*w+h/2)/h)
	}
	return max(c, 1), max(r, 1)
}

// scaled is the picture at w x h pixels.
func (p *Picture) scaled(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(g, g.Bounds(), p.Gray, p.Gray.Bounds(), draw.Src, nil)
	return g
}

// bayer is the 8x8 ordered-dither matrix, 0..63.
var bayer = [8][8]int{
	{0, 32, 8, 40, 2, 34, 10, 42},
	{48, 16, 56, 24, 50, 18, 58, 26},
	{12, 44, 4, 36, 14, 46, 6, 38},
	{60, 28, 52, 20, 62, 30, 54, 22},
	{3, 35, 11, 43, 1, 33, 9, 41},
	{51, 19, 59, 27, 49, 17, 57, 25},
	{15, 47, 7, 39, 13, 45, 5, 37},
	{63, 31, 55, 23, 61, 29, 53, 21},
}

// level dithers the tone v at x, y to one of n levels.
func level(v uint8, x, y, n int) int {
	t := (bayer[y&7][x&7]*2 + 1) * 255 / 128 // the threshold within a step, 0..255
	f := int(v) * (n - 1)
	l := f / 255
	if l < n-1 && f%255 > t {
		l++
	}
	return l
}

// Ramp is n tones from dark to light, between two colours of the theme.
func Ramp(dark, light color.Color, n int) []color.RGBA {
	dr, dg, db, _ := dark.RGBA()
	lr, lg, lb, _ := light.RGBA()
	out := make([]color.RGBA, n)
	for i := range out {
		mix := func(a, b uint32) uint8 { return uint8((a*uint32(n-1-i) + b*uint32(i)) / uint32(n-1) >> 8) }
		out[i] = color.RGBA{mix(dr, lr), mix(dg, lg), mix(db, lb), 255}
	}
	return out
}

// HalfBlock draws the picture in cols x rows cells: each a ▀ whose
// foreground is the upper pixel and background the lower, in tones from
// dark to light (24-bit colour).
func (p *Picture) HalfBlock(cols, rows int, dark, light color.Color) []string {
	ramp := Ramp(dark, light, HalfBlockLevels)
	g := p.scaled(cols, rows*2)
	out := make([]string, rows)
	var b strings.Builder
	for r := range rows {
		b.Reset()
		lastF, lastB := -1, -1
		for c := range cols {
			f := level(g.Pix[2*r*g.Stride+c], c, 2*r, HalfBlockLevels)
			k := level(g.Pix[(2*r+1)*g.Stride+c], c, 2*r+1, HalfBlockLevels)
			if f != lastF {
				fc := ramp[f]
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", fc.R, fc.G, fc.B)
				lastF = f
			}
			if k != lastB {
				bc := ramp[k]
				fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm", bc.R, bc.G, bc.B)
				lastB = k
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	return out
}

// Sixel draws the picture at w x h pixels as a sixel image in tones from
// dark to light (a DCS string; the terminal draws it from the cursor down).
func (p *Picture) Sixel(w, h int, dark, light color.Color) string {
	ramp := Ramp(dark, light, SixelLevels)
	g := p.scaled(w, h)
	lv := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			lv[y*w+x] = uint8(level(g.Pix[y*g.Stride+x], x, y, SixelLevels))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\x1bP0;1;0q\"1;1;%d;%d", w, h)
	for i, c := range ramp {
		fmt.Fprintf(&b, "#%d;2;%d;%d;%d", i, int(c.R)*100/255, int(c.G)*100/255, int(c.B)*100/255)
	}
	row := make([]byte, w)
	for top := 0; top < h; top += 6 {
		first := true
		for col := range SixelLevels {
			any := false
			for x := range w {
				var bits byte
				for k := 0; k < 6 && top+k < h; k++ {
					if int(lv[(top+k)*w+x]) == col {
						bits |= 1 << k
					}
				}
				row[x] = bits
				if bits != 0 {
					any = true
				}
			}
			if !any {
				continue
			}
			if !first {
				b.WriteByte('$')
			}
			first = false
			fmt.Fprintf(&b, "#%d", col)
			sixelRuns(&b, row)
		}
		b.WriteByte('-')
	}
	b.WriteString("\x1b\\")
	return b.String()
}

// sixelRuns writes a band's row of sixels, runs of four or more as !n.
func sixelRuns(b *strings.Builder, row []byte) {
	for i := 0; i < len(row); {
		j := i
		for j < len(row) && row[j] == row[i] {
			j++
		}
		ch := byte(63 + row[i])
		if n := j - i; n >= 4 {
			fmt.Fprintf(b, "!%d%c", n, ch)
		} else {
			for range n {
				b.WriteByte(ch)
			}
		}
		i = j
	}
}
