package theme

import (
	"image/color"
	"math"
	"testing"
)

// luminance is WCAG's relative luminance.
func luminance(c color.Color) float64 {
	r, g, b := RGB(c)
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func contrast(a, b color.Color) float64 {
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

// TestThemesAreLegible holds every theme to WCAG contrast: body text and
// the focused line at least 7:1 (the owner's yellowed panel washed out a
// weaker focus), dim text and links at least 4.5:1.
func TestThemesAreLegible(t *testing.T) {
	for _, th := range Themes {
		for _, c := range []struct {
			what   string
			fg, bg color.Color
			min    float64
		}{
			{"body", th.Page.FG, th.Page.BG, 7},
			{"focus", th.FocusFG, th.FocusBG, 7},
			{"link", th.Page.Accent, th.Page.BG, 4.5},
			{"dim", th.Page.Dim, th.Page.BG, 4.5},
			{"bar", th.Chrome.FG, th.Chrome.BG, 7},
			{"bar dim", th.Chrome.Dim, th.Chrome.BG, 4.5},
		} {
			if got := contrast(c.fg, c.bg); got < c.min {
				t.Errorf("%s: %s contrast %.1f, want at least %.1f", th.Name, c.what, got, c.min)
			}
		}
	}
}
