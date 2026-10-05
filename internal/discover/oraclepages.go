package discover

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"w5f/internal/doc"
)

// The card's and the hexagram's own pages: the picture, then the text,
// clean, from what is kept (asked for by the owner, 2026-10-01).

type pageBuilder struct{ d *doc.Document }

func (p pageBuilder) link(href, text string) int {
	p.d.Links = append(p.d.Links, doc.Link{Href: href, Text: text})
	return len(p.d.Links)
}

func (p pageBuilder) add(bs ...doc.Block) { p.d.Blocks = append(p.d.Blocks, bs...) }

func (p pageBuilder) heading(t string) { p.add(doc.Heading{Level: 2, Text: doc.Inline{{Text: t}}}) }

func (p pageBuilder) para(text string, style doc.Style) {
	for _, part := range strings.Split(text, "\n\n") {
		if part = strings.TrimSpace(part); part != "" {
			p.add(doc.Paragraph{Text: doc.Inline{{Text: part, Style: style}}})
		}
	}
}

// tarotRoute serves w5f:discover/tarot (a new draw) and
// w5f:discover/tarot/<card>[?r=1] (a card drawn).
func tarotRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	if target == "w5f:discover/tarot" {
		c, rev := DrawCard()
		return tarotDoc(ctx, env, c, rev)
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	c, ok := CardByKey(strings.TrimPrefix(u.Opaque, "discover/tarot/"))
	if !ok {
		return nil, errors.New("no such card: " + target)
	}
	return tarotDoc(ctx, env, c, u.Query().Get("r") == "1")
}

func tarotDoc(ctx context.Context, env Env, c Card, rev bool) (*doc.Document, error) {
	title := c.Name
	if rev {
		title += ", reversed"
	}
	p := pageBuilder{&doc.Document{Title: title, URL: TarotHref(c.Key, rev), Origin: "local", Lang: "en"}}
	p.d.Meta = []doc.KV{{Key: "tarot", Value: "A. E. Waite, The Pictorial Key to the Tarot (1910)"}}
	p.add(doc.Pre{Text: CardFrame(c, rev)})
	t, err := CardTextOf(ctx, env.Fetcher, env.DB, c)
	if err != nil {
		p.add(doc.Notice{Kind: "warn", Text: "Waite's text could not be read now: " + err.Error()})
	} else {
		first, second, fl, sl := t.Upright, t.Reversed, "Upright", "Reversed"
		if rev {
			first, second, fl, sl = t.Reversed, t.Upright, "Reversed", "Upright"
		}
		if first == "" && t.Upright+t.Reversed != "" {
			// The Two of Cups has no reversed meaning in Waite.
			first = "Waite gives this card no " + strings.ToLower(fl) + " meaning; read the other."
		}
		if first != "" {
			p.heading(fl)
			p.para(first, 0)
		}
		if second != "" {
			p.heading(sl)
			p.para(second, doc.Italic)
		}
		if t.Description != "" {
			p.heading("The card")
			p.para(t.Description, 0)
		}
		p.add(doc.Paragraph{Text: doc.Inline{{Text: "Waite's text: ", Style: doc.Italic}, {Text: t.Source, Link: p.link(t.Source, "Waite's page")}}})
	}
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "draw another card", Link: p.link("w5f:discover/tarot", "draw another")}, {Text: "   (c)", Style: doc.Italic}}})
	return p.d, nil
}

// ichingRoute serves w5f:discover/iching (a new cast) and
// w5f:discover/iching/<six lines> (a cast).
func ichingRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	if target == "w5f:discover/iching" {
		return ichingDoc(ctx, env, castHexagram())
	}
	lines, ok := parseCast(strings.TrimPrefix(target, "w5f:discover/iching/"))
	if !ok {
		return nil, errors.New("not a cast hexagram: " + target)
	}
	return ichingDoc(ctx, env, lines)
}

// steady are a hexagram's lines with none moving (to open the hexagram a
// cast turns into).
func steady(n int) []int {
	for i := 0; i < 64; i++ {
		var y [6]bool
		lines := make([]int, 6)
		for b := 0; b < 6; b++ {
			y[b] = i&(1<<b) != 0
			lines[b] = 8
			if y[b] {
				lines[b] = 7
			}
		}
		if kingWen(y) == n {
			return lines
		}
	}
	return nil
}

var lineNames = []string{"", "first (lowest)", "second", "third", "fourth", "fifth", "sixth (topmost)"}

func ichingDoc(ctx context.Context, env Env, lines []int) (*doc.Document, error) {
	n, then, moving := castNumbers(lines)
	h, err := HexagramOf(ctx, env.Fetcher, env.DB, n)
	name := fmt.Sprintf("Hexagram %d", n)
	if err == nil && h.Name != "" {
		name += " · " + h.Name
	}
	p := pageBuilder{&doc.Document{Title: name, URL: IChingHref(lines), Origin: "local", Lang: "en"}}
	p.d.Meta = []doc.KV{{Key: "I Ching", Value: "James Legge's translation (1882)"}}
	p.add(doc.Pre{Text: hexagramFigure(lines)})
	if then > 0 {
		in := doc.Inline{{Text: fmt.Sprintf("Moving lines %s turn it into ", joinInts(moving))}}
		thenName := fmt.Sprintf("hexagram %d", then)
		if t, err := HexagramOf(ctx, env.Fetcher, env.DB, then); err == nil && t.Name != "" {
			thenName += " · " + t.Name
		}
		in = append(in, doc.Span{Text: thenName, Style: doc.Bold, Link: p.link(IChingHref(steady(then)), thenName)}, doc.Span{Text: " (right)."})
		p.add(doc.Paragraph{Text: in})
	} else {
		p.add(doc.Paragraph{Text: doc.Inline{{Text: "No moving lines: the hexagram stands as it is.", Style: doc.Italic}}})
	}
	if err != nil {
		p.add(doc.Notice{Kind: "warn", Text: "Legge's text could not be read now: " + err.Error()})
	} else {
		if h.Note != "" {
			p.para(h.Note, doc.Italic)
		}
		p.heading("The judgment")
		p.para(h.Judgment, 0)
		if len(moving) > 0 {
			p.heading("The moving lines")
			for _, m := range moving {
				if t := h.Lines[m-1]; t != "" {
					p.add(doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("%d. ", m), Style: doc.Bold}, {Text: t}}})
				}
			}
			// Hexagrams 1 and 2 have a seventh text, for all six lines moving.
			if len(moving) == 6 && h.Lines[6] != "" {
				p.add(doc.Paragraph{Text: doc.Inline{{Text: "7. ", Style: doc.Bold}, {Text: h.Lines[6]}}})
			}
		}
		var all []doc.Block
		for i := 0; i < 7; i++ {
			if h.Lines[i] == "" {
				continue
			}
			label := fmt.Sprintf("%d. ", i+1)
			style := doc.Style(0)
			for _, m := range moving {
				if m == i+1 {
					style = doc.Bold
				}
			}
			if i < 6 {
				label = fmt.Sprintf("%d (%s). ", i+1, lineNames[i+1])
			}
			all = append(all, doc.Paragraph{Text: doc.Inline{{Text: label, Style: doc.Bold}, {Text: h.Lines[i], Style: style}}})
		}
		if len(all) > 0 {
			p.add(doc.Collapsible{Show: "all six lines", Hide: "hide the lines", Blocks: all})
		}
		p.add(doc.Paragraph{Text: doc.Inline{{Text: "Legge's text: ", Style: doc.Italic}, {Text: h.Source, Link: p.link(h.Source, "Legge's page")}}})
	}
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "cast another", Link: p.link("w5f:discover/iching", "cast another")}, {Text: "   (i)", Style: doc.Italic}}})
	p.d.Renumber()
	return p.d, nil
}

func joinInts(xs []int) string {
	var s []string
	for _, x := range xs {
		s = append(s, fmt.Sprint(x))
	}
	return strings.Join(s, ", ")
}
