package htmlconv

import (
	"bytes"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"

	"w5f/internal/doc"
)

// minArticleText is the least amount of readable text Readability must find
// before its result is preferred over the generic converter.
const minArticleText = 200

// Article extracts the main content of an arbitrary web page with the
// Readability algorithm (the one behind Firefox Reader View) and converts it
// into a Document. Pages where extraction finds too little text fall back to
// the generic converter.
func Article(body []byte, pageURL string) (*doc.Document, error) {
	u, _ := url.Parse(pageURL)
	body = stripBritannicaAI(body, u)
	art, err := readability.FromReader(bytes.NewReader(body), u)
	if err != nil || art.Node == nil {
		return Generic(bytes.NewReader(body), pageURL)
	}
	d := &doc.Document{
		URL:    pageURL,
		Origin: "live",
		Title:  collapseSpace(art.Title()),
		Byline: collapseSpace(art.Byline()),
		Lang:   strings.ToLower(art.Language()),
	}
	if site := collapseSpace(art.SiteName()); site != "" {
		d.Meta = append(d.Meta, doc.KV{Key: "site", Value: site})
	}
	if d.Byline != "" {
		d.Meta = append(d.Meta, doc.KV{Key: "by", Value: d.Byline})
	}
	if t, err := art.PublishedTime(); err == nil && !t.IsZero() {
		d.Meta = append(d.Meta, doc.KV{Key: "published", Value: t.Format("2006-01-02")})
	}
	c := newConverter(d, u)
	c.skip = func(n *html.Node) bool { return isHidden(n) }
	d.Blocks = c.blocks(art.Node)
	dropRepeatedTitle(d)
	d.Renumber()
	if doc.TextLength(d.Blocks) < minArticleText {
		g, gerr := Generic(bytes.NewReader(body), pageURL)
		if gerr == nil && doc.TextLength(g.Blocks) > doc.TextLength(d.Blocks) {
			return g, nil
		}
	}
	if d.Lang == "" {
		if root, err := html.Parse(bytes.NewReader(body)); err == nil {
			d.Lang = docLang(root)
		}
	}
	return d, nil
}

// dropRepeatedTitle removes a leading heading that repeats the title.
func dropRepeatedTitle(d *doc.Document) {
	if len(d.Blocks) == 0 {
		return
	}
	if h, ok := d.Blocks[0].(doc.Heading); ok &&
		strings.EqualFold(collapseSpace(h.Text.PlainText()), d.Title) {
		d.Blocks = d.Blocks[1:]
	}
}
