package htmlconv

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"w5f/internal/doc"
)

// Fragment converts an HTML fragment (e.g. feed entry content) into blocks.
// Links are appended to d.Links so the blocks can live inside d.
func Fragment(d *doc.Document, fragment, baseURL string) []doc.Block {
	ctxNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctxNode)
	if err != nil {
		return nil
	}
	container := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		container.AppendChild(n)
	}
	base, _ := url.Parse(baseURL)
	c := newConverter(d, base)
	c.skip = isHidden
	return c.blocks(container)
}

// FragmentText returns the visible text of an HTML fragment.
func FragmentText(fragment string) string {
	ctxNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctxNode)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(textContent(n))
		b.WriteByte(' ')
	}
	return collapseSpace(b.String())
}
