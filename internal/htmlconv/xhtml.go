package htmlconv

import (
	"bytes"
	"net/url"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"w5f/internal/doc"
)

// XHTML converts a book chapter. Unlike Generic it keeps <header>, <footer>
// and <aside> (books put chapter titles and notes there) and applies no
// extraction heuristics: a chapter is all content.
func XHTML(body []byte, baseURL string) (*doc.Document, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(baseURL)
	d := &doc.Document{URL: baseURL, Origin: "file", Lang: docLang(root)}
	c := newConverter(d, base)
	c.skip = isHidden
	if t := find(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.DataAtom == atom.Title }); t != nil {
		d.Title = collapseSpace(textContent(t))
	}
	content := find(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.DataAtom == atom.Body })
	if content == nil {
		content = root
	}
	d.Blocks = c.blocks(content)
	d.Renumber()
	return d, nil
}
