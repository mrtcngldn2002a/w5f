// Package theme maps semantic render roles onto palette colors.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"w5f/internal/doc"
	"w5f/internal/render"
)

// Palette is a set of semantic colors (see 04 Estetik Plan).
type Palette struct {
	Name   string
	BG     color.Color
	FG     color.Color
	Dim    color.Color
	Accent color.Color
	Rule   color.Color
	Warn   color.Color
}

var (
	AmberP3 = Palette{"amber", hex("#120C02"), hex("#FFB000"), hex("#9C6B00"), hex("#FFD27A"), hex("#4A3300"), hex("#FF6A00")}
	GreenP1 = Palette{"green", hex("#020A04"), hex("#33FF66"), hex("#1F9C3E"), hex("#B6FFC9"), hex("#0F4A1E"), hex("#E6FF33")}
	// PaperInk is the long-reading palette.
	PaperInk    = Palette{"paper", hex("#1B1A17"), hex("#D8D2C4"), hex("#8A857A"), hex("#C8A15A"), hex("#3A3833"), hex("#D08040")}
	ColdArchive = Palette{"cold", hex("#0B0E12"), hex("#C9D1D9"), hex("#6E7681"), hex("#E5534B"), hex("#30363D"), hex("#D29922")}
)

// ByName returns a palette by its short name.
func ByName(name string) (Palette, bool) {
	for _, p := range []Palette{AmberP3, GreenP1, PaperInk, ColdArchive} {
		if p.Name == name {
			return p, true
		}
	}
	return Palette{}, false
}

func hex(s string) color.Color { return lipgloss.Color(s) }

// Theme is the full UI theme: chrome (bars, menus) and reading surface can use
// different palettes — the default is amber chrome over a paper reading page.
type Theme struct {
	Chrome Palette
	Page   Palette
}

// Default is the decided default: Amber P3 chrome, Paper/Ink while reading.
var Default = Theme{Chrome: AmberP3, Page: PaperInk}

// Seg returns the style for a rendered segment on the reading page.
func (t Theme) Seg(s render.Seg, focused bool) lipgloss.Style {
	p := t.Page
	st := lipgloss.NewStyle().Foreground(p.FG)
	switch s.Role {
	case render.Dim, render.Meta:
		st = st.Foreground(p.Dim)
	case render.Title:
		st = st.Foreground(t.Chrome.FG).Bold(true)
	case render.H1, render.H2:
		st = st.Foreground(t.Chrome.FG).Bold(true)
	case render.H3:
		st = st.Bold(true)
	case render.LinkRole:
		st = st.Foreground(p.Accent).Underline(true)
	case render.Fold:
		st = st.Foreground(t.Chrome.FG).Bold(true)
	case render.RuleRole, render.QuoteBar:
		st = st.Foreground(p.Rule)
	case render.CodeRole:
		st = st.Foreground(p.Dim)
	case render.NoticeRole:
		st = st.Foreground(p.Warn)
	case render.ImageRole:
		st = st.Foreground(p.Dim).Italic(true)
	}
	if s.Style&doc.Bold != 0 {
		st = st.Bold(true)
	}
	if s.Style&doc.Italic != 0 {
		st = st.Italic(true)
	}
	if s.Style&doc.Underline != 0 {
		st = st.Underline(true)
	}
	if s.Style&doc.Strike != 0 {
		st = st.Strikethrough(true)
	}
	if s.Style&doc.Redacted != 0 {
		st = st.Foreground(p.FG).Background(p.FG)
	}
	if focused {
		st = st.Reverse(true)
	}
	return st
}

// Bar is the style for the top and bottom chrome bars.
func (t Theme) Bar() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Chrome.FG).Background(t.Chrome.BG)
}

// BarDim is the secondary text style on the bars.
func (t Theme) BarDim() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Chrome.Dim).Background(t.Chrome.BG)
}

// Hint is the style for link-hint labels.
func (t Theme) Hint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Chrome.BG).Background(t.Chrome.FG).Bold(true)
}
