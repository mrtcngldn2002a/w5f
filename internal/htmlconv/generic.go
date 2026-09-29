package htmlconv

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"w5f/internal/doc"
)

// Generic converts an arbitrary HTML page. It prefers <main>/<article> when
// present and drops obvious page chrome. Full Readability extraction arrives
// with the article adapter (M1); this is the fallback path.
func Generic(r io.Reader, pageURL string) (*doc.Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(pageURL)
	d := &doc.Document{URL: pageURL, Origin: "live", Lang: docLang(root)}
	c := newConverter(d, base)
	c.skip = func(n *html.Node) bool {
		switch n.DataAtom {
		case atom.Nav, atom.Header, atom.Footer, atom.Aside:
			return true
		}
		return false
	}
	if t := find(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.DataAtom == atom.Title }); t != nil {
		d.Title = collapseSpace(textContent(t))
	}
	content := find(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (n.DataAtom == atom.Main || n.DataAtom == atom.Article)
	})
	if content == nil {
		content = find(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.DataAtom == atom.Body })
	}
	if content == nil {
		content = root
	}
	d.Blocks = c.blocks(content)
	// Drop a leading H1 that repeats the title.
	if len(d.Blocks) > 0 {
		if h, ok := d.Blocks[0].(doc.Heading); ok && h.Level == 1 &&
			strings.EqualFold(strings.TrimSpace(h.Text.PlainText()), d.Title) {
			d.Blocks = d.Blocks[1:]
		}
	}
	d.Renumber()
	return d, nil
}
