// Package htmlconv converts HTML into the W5F document model.
//
// The converter never applies a site's CSS. It reads semantic structure only
// (headings, paragraphs, lists, tables, links) plus a small set of known
// widget patterns (Wikidot collapsibles, tabs, image blocks, footnotes) that
// it maps onto first-class blocks.
package htmlconv

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"w5f/internal/doc"
)

// converter holds state while walking one document.
type converter struct {
	d    *doc.Document
	base *url.URL
	// skip reports nodes to drop entirely (ads, rating widgets, nav chrome).
	skip func(n *html.Node) bool
	// special lets a site profile turn a node into blocks. It returns ok=false
	// to fall back to the generic rules.
	special func(c *converter, n *html.Node) ([]doc.Block, bool)
}

func newConverter(d *doc.Document, base *url.URL) *converter {
	return &converter{d: d, base: base, skip: func(*html.Node) bool { return false }}
}

// blocks converts the children of n into a block list, gathering loose inline
// content into paragraphs.
func (c *converter) blocks(n *html.Node) []doc.Block {
	var out []doc.Block
	var inl doc.Inline
	flush := func() {
		if t := trimInline(inl); len(t) > 0 {
			out = append(out, doc.Paragraph{Text: t})
		}
		inl = nil
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && c.skip(ch) {
			continue
		}
		if isBlock(ch) {
			flush()
			out = append(out, c.block(ch)...)
			continue
		}
		inl = append(inl, c.inline(ch, 0, 0)...)
	}
	flush()
	return out
}

// block converts a single block-level element.
func (c *converter) block(n *html.Node) []doc.Block {
	if c.special != nil {
		if b, ok := c.special(c, n); ok {
			return b
		}
	}
	switch n.DataAtom {
	case atom.P:
		if hasBlockChild(n) {
			return c.blocks(n)
		}
		if t := trimInline(c.inlineChildren(n, 0, 0)); len(t) > 0 {
			return []doc.Block{doc.Paragraph{Text: t}}
		}
		return nil
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		if t := trimInline(c.inlineChildren(n, 0, 0)); len(t) > 0 {
			return []doc.Block{doc.Heading{Level: level, Text: t}}
		}
		return nil
	case atom.Blockquote:
		if b := c.blocks(n); len(b) > 0 {
			return []doc.Block{doc.Quote{Blocks: b}}
		}
		return nil
	case atom.Ul, atom.Ol:
		l := doc.List{Ordered: n.DataAtom == atom.Ol}
		for li := n.FirstChild; li != nil; li = li.NextSibling {
			if li.Type == html.ElementNode && li.DataAtom == atom.Li {
				if b := c.blocks(li); len(b) > 0 {
					l.Items = append(l.Items, b)
				}
			}
		}
		if len(l.Items) == 0 {
			return nil
		}
		return []doc.Block{l}
	case atom.Dl:
		var out []doc.Block
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			t := trimInline(c.inlineChildren(ch, 0, 0))
			if len(t) == 0 {
				continue
			}
			if ch.DataAtom == atom.Dt {
				t = restyle(t, doc.Bold)
			}
			out = append(out, doc.Paragraph{Text: t})
		}
		return out
	case atom.Table:
		return c.table(n)
	case atom.Hr:
		return []doc.Block{doc.Rule{}}
	case atom.Pre:
		return []doc.Block{doc.Pre{Text: strings.TrimRight(textContent(n), "\n ")}}
	case atom.Img:
		return []doc.Block{c.image(n, "")}
	case atom.Figure:
		var img *html.Node
		var caption string
		walk(n, func(x *html.Node) bool {
			if x.Type == html.ElementNode && x.DataAtom == atom.Img && img == nil {
				img = x
			}
			if x.Type == html.ElementNode && x.DataAtom == atom.Figcaption {
				caption = collapseSpace(textContent(x))
				return false
			}
			return true
		})
		if img != nil {
			return []doc.Block{c.image(img, caption)}
		}
		return c.blocks(n)
	case atom.Script, atom.Style, atom.Noscript, atom.Iframe, atom.Form, atom.Button, atom.Svg, atom.Nav:
		return nil
	}
	return c.blocks(n)
}

func (c *converter) image(n *html.Node, caption string) doc.Image {
	return doc.Image{Src: c.resolve(attr(n, "src")), Alt: collapseSpace(attr(n, "alt")), Caption: caption}
}

func (c *converter) table(n *html.Node) []doc.Block {
	t := doc.Table{}
	first := true
	walk(n, func(x *html.Node) bool {
		if x.Type != html.ElementNode {
			return true
		}
		if x.DataAtom == atom.Table && x != n {
			return false // nested tables are flattened into cell text
		}
		if x.DataAtom != atom.Tr {
			return true
		}
		var row []doc.Inline
		allTH := true
		for td := x.FirstChild; td != nil; td = td.NextSibling {
			if td.Type != html.ElementNode || (td.DataAtom != atom.Td && td.DataAtom != atom.Th) {
				continue
			}
			if td.DataAtom != atom.Th {
				allTH = false
			}
			row = append(row, trimInline(c.inlineChildren(td, 0, 0)))
		}
		if len(row) > 0 {
			if first && allTH {
				t.Header = true
			}
			first = false
			t.Rows = append(t.Rows, row)
		}
		return false
	})
	if len(t.Rows) == 0 {
		return nil
	}
	return []doc.Block{t}
}

// inlineChildren converts all children of n as inline content.
func (c *converter) inlineChildren(n *html.Node, st doc.Style, link int) doc.Inline {
	var out doc.Inline
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		out = append(out, c.inline(ch, st, link)...)
	}
	return out
}

// inline converts one node (and its subtree) as inline content.
func (c *converter) inline(n *html.Node, st doc.Style, link int) doc.Inline {
	switch n.Type {
	case html.TextNode:
		if n.Data == "" {
			return nil
		}
		return doc.Inline{{Text: n.Data, Style: st, Link: link}}
	case html.ElementNode:
	default:
		return nil
	}
	if c.skip(n) {
		return nil
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Iframe, atom.Button, atom.Svg:
		return nil
	case atom.Br:
		return doc.Inline{{Break: true}}
	case atom.Strong, atom.B:
		st |= doc.Bold
	case atom.Em, atom.I, atom.Cite:
		st |= doc.Italic
	case atom.U, atom.Ins:
		st |= doc.Underline
	case atom.S, atom.Strike, atom.Del:
		st |= doc.Strike
	case atom.Code, atom.Tt, atom.Kbd, atom.Samp:
		st |= doc.Code
	case atom.Sup:
		st |= doc.Sup
	case atom.Img:
		alt := collapseSpace(attr(n, "alt"))
		if alt == "" {
			alt = "image"
		}
		return doc.Inline{{Text: "[" + alt + "]", Style: st | doc.Italic, Link: link}}
	case atom.A:
		href := attr(n, "href")
		// w5f: links in web content could trigger W5F actions; drop them.
		if href != "" && !strings.HasPrefix(href, "javascript:") && !strings.HasPrefix(href, "#") && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "w5f:") {
			text := collapseSpace(textContent(n))
			c.d.Links = append(c.d.Links, doc.Link{Href: c.resolve(href), Text: text})
			link = len(c.d.Links)
		}
	case atom.Span:
		style := strings.ToLower(attr(n, "style"))
		if strings.Contains(style, "line-through") {
			st |= doc.Strike
		}
		if strings.Contains(style, "underline") {
			st |= doc.Underline
		}
		if isRedactionStyle(style) {
			st |= doc.Redacted
		}
	}
	return c.inlineChildren(n, st, link)
}

func (c *converter) resolve(href string) string {
	if href == "" || c.base == nil {
		return href
	}
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	return c.base.ResolveReference(u).String()
}

// isRedactionStyle detects the common "black text on black background" trick.
func isRedactionStyle(style string) bool {
	return (strings.Contains(style, "background-color: black") || strings.Contains(style, "background:black") ||
		strings.Contains(style, "background-color:black") || strings.Contains(style, "background: black")) &&
		(strings.Contains(style, "color: black") || strings.Contains(style, "color:black"))
}

var blockAtoms = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Blockquote: true, atom.Ul: true, atom.Ol: true, atom.Dl: true,
	atom.Table: true, atom.Hr: true, atom.Pre: true, atom.Figure: true, atom.Section: true,
	atom.Article: true, atom.Header: true, atom.Footer: true, atom.Main: true, atom.Aside: true,
	atom.Nav: true, atom.Center: true, atom.Details: true, atom.Form: true, atom.Script: true,
	atom.Style: true, atom.Noscript: true, atom.Iframe: true,
}

func isBlock(n *html.Node) bool {
	return n.Type == html.ElementNode && blockAtoms[n.DataAtom]
}

func hasBlockChild(n *html.Node) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if isBlock(ch) {
			return true
		}
	}
	return false
}

// --- small DOM helpers ---

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

// walk visits n and its descendants depth-first; fn returns false to skip a
// node's children.
func walk(n *html.Node, fn func(*html.Node) bool) {
	if !fn(n) {
		return
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		walk(ch, fn)
	}
}

func find(n *html.Node, pred func(*html.Node) bool) *html.Node {
	var found *html.Node
	walk(n, func(x *html.Node) bool {
		if found != nil {
			return false
		}
		if pred(x) {
			found = x
			return false
		}
		return true
	})
	return found
}

func byClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool { return hasClass(n, class) }
}

func byID(id string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "id") == id }
}

func textContent(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) bool {
		if x.Type == html.ElementNode && (x.DataAtom == atom.Script || x.DataAtom == atom.Style) {
			return false
		}
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		return true
	})
	return b.String()
}

// docLang returns the <html lang> attribute, lower-cased.
func docLang(root *html.Node) string {
	if h := find(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.DataAtom == atom.Html }); h != nil {
		return strings.ToLower(attr(h, "lang"))
	}
	return ""
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

// trimInline drops leading/trailing whitespace and breaks; returns nil if the
// inline content is visually empty.
func trimInline(in doc.Inline) doc.Inline {
	empty := true
	for _, s := range in {
		if !s.Break && strings.TrimSpace(strings.ReplaceAll(s.Text, " ", " ")) != "" {
			empty = false
			break
		}
	}
	if empty {
		return nil
	}
	for len(in) > 0 && (in[0].Break || strings.TrimSpace(in[0].Text) == "") {
		in = in[1:]
	}
	for len(in) > 0 && (in[len(in)-1].Break || strings.TrimSpace(in[len(in)-1].Text) == "") {
		in = in[:len(in)-1]
	}
	return in
}

func restyle(in doc.Inline, st doc.Style) doc.Inline {
	out := make(doc.Inline, len(in))
	for i, s := range in {
		s.Style |= st
		out[i] = s
	}
	return out
}
