package index

import (
	"net/url"
	"strconv"
	"strings"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/store"
)

const perPage = 30

var filters = []struct{ kind, label string }{
	{"", "all"}, {"feed", "RSS"}, {"scp", "SCP & wikis"}, {"book", "books"}, {"web", "web"}, {"notes", "notes"},
}

// IsTarget reports whether target is a search of the owner's archive.
func IsTarget(target string) bool { return strings.HasPrefix(target, "w5f:find") }

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func findHref(q, kind string, page int) string {
	v := url.Values{"q": {q}}
	if kind != "" {
		v.Set("kind", kind)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	return "w5f:find?" + v.Encode()
}

// Route shows search results (w5f:find?q=…&kind=…&page=…).
func Route(target string, db *store.DB) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	v := u.Query()
	q, kind := strings.TrimSpace(v.Get("q")), v.Get("kind")
	page, _ := strconv.Atoi(v.Get("page"))
	if page < 1 {
		page = 1
	}
	d := &doc.Document{Title: "Search: " + q, URL: target, Origin: "local", Lang: "en"}
	if q == "" {
		d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Press / and type some words to search everything you have read.", Style: doc.Italic}}}}
		return d, nil
	}
	hits, err := Search(db, q, kind, perPage+1, (page-1)*perPage)
	if err != nil {
		return nil, err
	}
	var bar doc.Inline
	for i, f := range filters {
		if i > 0 {
			bar = append(bar, doc.Span{Text: " · "})
		}
		if f.kind == kind {
			bar = append(bar, doc.Span{Text: f.label, Style: doc.Bold})
			continue
		}
		bar = append(bar, doc.Span{Text: f.label, Link: link(d, findHref(q, f.kind, 1), f.label)})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: bar})
	more := len(hits) > perPage
	if more {
		hits = hits[:perPage]
	}
	if len(hits) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing found in what you have read. All words must appear; try fewer or different words.", Style: doc.Italic}}})
		return d, nil
	}
	var items [][]doc.Block
	for _, h := range hits {
		head := doc.Inline{{Text: catalog.Label(h.Kind) + " ", Style: doc.Bold}, {Text: h.Title, Link: link(d, h.Target, h.Title)}}
		if h.Catalog != "" {
			head = append(head, doc.Span{Text: "  " + h.Catalog, Style: doc.Italic})
		}
		item := []doc.Block{doc.Paragraph{Text: head}}
		if len(h.Snippet) > 0 {
			item = append(item, doc.Paragraph{Text: h.Snippet})
		}
		items = append(items, item)
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	if more {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ more results", Link: link(d, findHref(q, kind, page+1), "more")}}})
	}
	return d, nil
}
