package htmlconv

import (
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"w5f/internal/doc"
)

// IsWikidot reports whether a URL belongs to a Wikidot-hosted site.
func IsWikidot(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return strings.HasSuffix(h, ".wikidot.com") || h == "scpwiki.com" || h == "www.scpwiki.com"
}

// wikidotSkipClasses are page-content widgets that carry no reading value.
var wikidotSkipClasses = []string{
	"page-rate-widget-box", "creditRate", "rate-box-with-credit-button", "credit-back",
	"scpnet-interwiki-wrapper", "wl-translations", "modalbox", "page-tags", "error-block",
	"backlinks",
}

// isHidden reports inline-styled display:none elements. Collapsible bodies are
// also display:none, but they are handled by wikidotSpecial before this runs.
func isHidden(n *html.Node) bool {
	st := strings.ReplaceAll(strings.ToLower(attr(n, "style")), " ", "")
	return strings.Contains(st, "display:none")
}

// Wikidot converts a full Wikidot page (as served to browsers) into a Document.
func Wikidot(r io.Reader, pageURL string) (*doc.Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(pageURL)
	d := &doc.Document{URL: pageURL, Origin: "live", Lang: docLang(root)}
	c := newConverter(d, base)
	c.skip = func(n *html.Node) bool {
		for _, cl := range wikidotSkipClasses {
			if hasClass(n, cl) {
				return true
			}
		}
		return isHidden(n) && !hasClass(n, "collapsible-block-unfolded")
	}
	c.special = wikidotSpecial

	if t := find(root, byID("page-title")); t != nil {
		d.Title = collapseSpace(textContent(t))
	}
	if d.Title == "" {
		if t := find(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.DataAtom == atom.Title }); t != nil {
			d.Title = collapseSpace(strings.SplitN(textContent(t), " - ", 2)[0])
		}
	}
	if rp := find(root, byClass("rate-points")); rp != nil {
		if num := find(rp, byClass("number")); num != nil {
			d.Meta = append(d.Meta, doc.KV{Key: "rating", Value: collapseSpace(textContent(num))})
		}
	}
	if tags := find(root, byClass("page-tags")); tags != nil {
		var ts []string
		walk(tags, func(n *html.Node) bool {
			if n.Type == html.ElementNode && n.DataAtom == atom.A {
				if t := collapseSpace(textContent(n)); t != "" && !strings.HasPrefix(t, "_") {
					ts = append(ts, t)
				}
				return false
			}
			return true
		})
		if len(ts) > 0 {
			d.Meta = append(d.Meta, doc.KV{Key: "tags", Value: strings.Join(ts, " ")})
		}
	}

	// The site's top menu first, so its links come before the page's.
	menu := siteMenu(c, root)
	content := find(root, byID("page-content"))
	if content == nil {
		content = root
	}
	d.Blocks = c.blocks(content)
	if menu != nil {
		d.Blocks = append([]doc.Block{*menu}, d.Blocks...)
	}
	d.Renumber()
	return d, nil
}

// siteMenu keeps the wiki's top menu as one closed section. Hub pages rely on
// it: Wanderers' Library's "Browse the Library" opens Wing One, and Wing Two,
// Wing Three and the halls are only reachable from this menu.
func siteMenu(c *converter, root *html.Node) *doc.Collapsible {
	bar := find(root, byClass("top-bar"))
	if bar == nil {
		return nil
	}
	links := 0
	walk(bar, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.A && !strings.HasPrefix(attr(n, "href"), "javascript:") {
			links++
		}
		return true
	})
	if links < 3 {
		return nil
	}
	blocks := c.blocks(bar)
	if len(blocks) == 0 {
		return nil
	}
	return &doc.Collapsible{Show: "Site menu", Hide: "Site menu", Blocks: blocks}
}

// wikidotSpecial maps known Wikidot widgets onto first-class blocks.
func wikidotSpecial(c *converter, n *html.Node) ([]doc.Block, bool) {
	switch {
	case hasClass(n, "collapsible-block"):
		return []doc.Block{c.collapsible(n)}, true
	case hasClass(n, "yui-navset"):
		return c.tabs(n), true
	case hasClass(n, "scp-image-block"):
		img := find(n, func(x *html.Node) bool { return x.Type == html.ElementNode && x.DataAtom == atom.Img })
		if img == nil {
			return nil, true
		}
		caption := ""
		if cap := find(n, byClass("scp-image-caption")); cap != nil {
			caption = collapseSpace(textContent(cap))
		}
		return []doc.Block{c.image(img, caption)}, true
	case hasClass(n, "footnotes-footer"):
		return c.footnotes(n), true
	case n.DataAtom == atom.Iframe && hasClass(n, "html-block-iframe"):
		// Resolved by the source layer: inlined if it has text, else dropped.
		return []doc.Block{doc.Embed{Src: c.resolve(attr(n, "src"))}}, true
	}
	return nil, false
}

var labelJunk = regexp.MustCompile(`^[\s\x{00a0}+\-−–—▸►▼▲•·‡†»›\x{fffd}]+`)

func cleanLabel(s string) string {
	return strings.TrimSpace(labelJunk.ReplaceAllString(collapseSpace(s), ""))
}

func (c *converter) collapsible(n *html.Node) doc.Collapsible {
	col := doc.Collapsible{}
	if f := find(n, byClass("collapsible-block-folded")); f != nil {
		col.Show = cleanLabel(textContent(f))
	}
	if u := find(n, byClass("collapsible-block-unfolded-link")); u != nil {
		col.Hide = cleanLabel(textContent(u))
	}
	if col.Show == "" {
		col.Show = "show"
	}
	if body := find(n, byClass("collapsible-block-content")); body != nil {
		col.Blocks = c.blocks(body)
	}
	return col
}

// tabs converts a YUI tab view into a sequence of collapsibles; the first tab
// starts open, mirroring the browser default.
func (c *converter) tabs(n *html.Node) []doc.Block {
	var titles []string
	if nav := find(n, byClass("yui-nav")); nav != nil {
		for li := nav.FirstChild; li != nil; li = li.NextSibling {
			if li.Type == html.ElementNode && li.DataAtom == atom.Li {
				titles = append(titles, collapseSpace(textContent(li)))
			}
		}
	}
	var out []doc.Block
	if content := find(n, byClass("yui-content")); content != nil {
		i := 0
		for pane := content.FirstChild; pane != nil; pane = pane.NextSibling {
			if pane.Type != html.ElementNode {
				continue
			}
			title := "tab"
			if i < len(titles) && titles[i] != "" {
				title = titles[i]
			}
			out = append(out, doc.Collapsible{Show: title, Blocks: c.blocks(pane), Open: i == 0})
			i++
		}
	}
	return out
}

func (c *converter) footnotes(n *html.Node) []doc.Block {
	fn := doc.Footnotes{}
	walk(n, func(x *html.Node) bool {
		if !hasClass(x, "footnote-footer") {
			return true
		}
		inl := trimInline(c.inlineChildren(x, 0, 0))
		label := ""
		if len(inl) > 0 {
			// Wikidot renders "<a>1</a>. text"; lift the number into the label.
			label = strings.TrimSpace(inl[0].Text)
			inl = inl[1:]
			if len(inl) > 0 {
				inl[0].Text = strings.TrimLeft(inl[0].Text, ". ")
			}
		}
		fn.Notes = append(fn.Notes, doc.Footnote{Label: label, Text: trimInline(inl)})
		return false
	})
	if len(fn.Notes) == 0 {
		return nil
	}
	return []doc.Block{fn}
}
