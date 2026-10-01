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
	AmberP3 = Palette{"amber", hex("#120C02"), hex("#FFB000"), hex("#B07A00"), hex("#FFD27A"), hex("#4A3300"), hex("#FF6A00")}
	// PaperInk is the long-reading palette under the amber chrome.
	PaperInk    = Palette{"paper", hex("#1B1A17"), hex("#D8D2C4"), hex("#8A857A"), hex("#C8A15A"), hex("#3A3833"), hex("#D08040")}
	ColdArchive = Palette{"cold", hex("#0B0E12"), hex("#C9D1D9"), hex("#8B949E"), hex("#F0716A"), hex("#30363D"), hex("#D29922")}
	// DayPaper is dark ink on cream: the most legible on a yellowed panel
	// and by day.
	DayPaper  = Palette{"day", hex("#F4ECD8"), hex("#2B2118"), hex("#6B5E4E"), hex("#8A3B12"), hex("#C9BBA0"), hex("#B03A00")}
	DayChrome = Palette{"day", hex("#E2D3B4"), hex("#3A2A18"), hex("#64543E"), hex("#8A3B12"), hex("#B8A888"), hex("#B03A00")}
	// NightRed is low light: reds only, for reading in the dark.
	NightRed    = Palette{"night", hex("#0A0302"), hex("#F2735C"), hex("#C05844"), hex("#FF8A6E"), hex("#3A1410"), hex("#FFB000")}
	NightChrome = Palette{"night", hex("#140504"), hex("#F2735C"), hex("#C85E49"), hex("#FF8A6E"), hex("#3A1410"), hex("#FFB000")}
)

func hex(s string) color.Color { return lipgloss.Color(s) }

// Theme is the full UI theme: chrome (bars, menus) and reading surface can use
// different palettes — amber chrome over a paper reading page, say.
type Theme struct {
	Name, Label string
	Chrome      Palette
	Page        Palette
	// Focus is the selected link or line: Page's ground on Chrome's
	// colour, never left to the terminal's reverse video (on a yellowed
	// panel reverse video washed out).
	FocusFG, FocusBG color.Color
}

// The themes (chosen with the owner, 2026-10-01): amber, and three more.
var (
	Amber = Theme{"amber", "Amber — amber chrome over a dark paper page", AmberP3, PaperInk, PaperInk.BG, AmberP3.FG}
	Day   = Theme{"day", "Day paper — dark ink on cream, for daylight and yellowed screens", DayChrome, DayPaper, DayPaper.BG, DayChrome.FG}
	Cold  = Theme{"cold", "Cold archive — grey-blue, quiet, a red accent", ColdArchive, ColdArchive, ColdArchive.BG, ColdArchive.FG}
	Night = Theme{"night", "Night red — reds only, for reading in the dark", NightChrome, NightRed, NightRed.BG, hex("#FF7A5C")}

	Themes = []Theme{Amber, Day, Cold, Night}
)

// Default is the decided default: Amber.
var Default = Amber

// ByName returns a theme by its short name.
func ByName(name string) (Theme, bool) {
	for _, t := range Themes {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

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
		st = st.Foreground(t.FocusFG).Background(t.FocusBG).Bold(true).Underline(false)
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
