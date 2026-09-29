package theme

import (
	"fmt"
	"image/color"
)

// Console is a palette as the 16 ANSI colours of a console (kmscon, xterm):
// black, red, green, yellow, blue, magenta, cyan, white, then the bright
// eight. A monochrome palette stays monochrome: every colour is one of its
// shades, warnings keep their own hue.
func (p Palette) Console() [16]color.Color {
	return [16]color.Color{
		p.BG, p.Warn, p.Dim, p.FG, p.Rule, p.Accent, p.Dim, p.FG,
		p.Rule, p.Warn, p.Accent, p.Accent, p.Dim, p.Accent, p.Accent, p.Accent,
	}
}

// RGB is a colour as its 8-bit parts.
func RGB(c color.Color) (r, g, b uint8) {
	r32, g32, b32, _ := c.RGBA()
	return uint8(r32 >> 8), uint8(g32 >> 8), uint8(b32 >> 8)
}

// Hex is a colour as #rrggbb.
func Hex(c color.Color) string {
	r, g, b := RGB(c)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
