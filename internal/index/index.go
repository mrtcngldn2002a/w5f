package index

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/htmlconv"
	"w5f/internal/store"
)

// MaxText caps the text indexed for one document.
const MaxText = 200 << 10

// Text returns a document's readable text, one block per line: paragraphs,
// headings, list items, quotes, folded sections, tables, code, footnotes.
func Text(d *doc.Document) string {
	var b strings.Builder
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" && b.Len() < MaxText {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			add(x.Text.PlainText())
		case doc.Heading:
			add(x.Text.PlainText())
		case doc.Pre:
			add(x.Text)
		case doc.Table:
			for _, r := range x.Rows {
				var cells []string
				for _, c := range r {
					cells = append(cells, strings.TrimSpace(c.PlainText()))
				}
				add(strings.Join(cells, " · "))
			}
		case doc.Footnotes:
			for _, n := range x.Notes {
				add(n.Text.PlainText())
			}
		}
		return nil, false
	})
	return capText(b.String())
}

// capText cuts text to MaxText bytes on a rune boundary.
func capText(s string) string {
	if len(s) <= MaxText {
		return s
	}
	s = s[:MaxText]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Kind classifies a loaded page and gives the address the index and the
// history use for it. An empty kind means "not recorded" (menus, lists,
// local files).
func Kind(target string, d *doc.Document) (kind, key string) {
	if d != nil {
		switch {
		case strings.HasPrefix(d.Ref, "item:"):
			return "feed", "w5f:item/" + strings.TrimPrefix(d.Ref, "item:")
		case strings.HasPrefix(d.Ref, "book:"):
			parts := strings.SplitN(strings.TrimPrefix(d.Ref, "book:"), ":", 2)
			if len(parts) == 2 {
				return "book", "w5f:book/" + parts[0] + "/ch/" + parts[1]
			}
			return "", ""
		case strings.HasPrefix(d.Ref, "serial:"):
			parts := strings.SplitN(strings.TrimPrefix(d.Ref, "serial:"), ":", 2)
			if len(parts) == 2 {
				return "fiction", "w5f:serial/" + parts[0] + "/ch/" + parts[1]
			}
			return "", ""
		}
	}
	u, err := url.Parse(target)
	if err == nil && (u.Scheme == "gemini" || u.Scheme == "gopher") {
		return "smallweb", target
	}
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case strings.HasPrefix(host, "scp-") && strings.HasSuffix(host, ".wikidot.com"):
		return "scp", target
	case strings.HasSuffix(host, ".wikidot.com"):
		return "wiki", target
	case host == "reddit.com" || strings.HasSuffix(host, ".reddit.com"):
		return "reddit", target
	}
	return "web", target
}

// Page indexes a document the reader has opened (content pages only).
func Page(db *store.DB, target string, d *doc.Document) error {
	kind, key := Kind(target, d)
	if kind == "" {
		return nil
	}
	return put(db, key, kind, d.Title, catalog.Number(target, d), Text(d))
}

func put(db *store.DB, key, kind, title, cat, text string) error {
	return db.PutDoc(store.IndexDoc{Target: key, Kind: kind, Title: title, Catalog: cat, Text: text,
		Updated: time.Now(), FoldTitle: Fold(title), FoldText: Fold(text)})
}

// Feeds indexes the feed items that are not in the index yet.
func Feeds(db *store.DB) (int, error) {
	n := 0
	for {
		its, err := db.ItemsToIndex(200)
		if err != nil || len(its) == 0 {
			return n, err
		}
		for _, it := range its {
			body := it.Content
			if strings.TrimSpace(body) == "" {
				body = it.Summary
			}
			text := capText(it.Author + "\n" + htmlconv.FragmentText(body))
			if err := put(db, fmt.Sprintf("w5f:item/%d", it.ID), "feed", it.Title, catalog.Feed(it.FeedID, it.Published), text); err != nil {
				return n, err
			}
			n++
		}
	}
}
