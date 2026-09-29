package htmlconv

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"w5f/internal/doc"
)

// IsEksi reports whether u is on Ekşi Sözlük.
func IsEksi(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return h == "eksisozluk.com" || h == "www.eksisozluk.com"
}

// Eksi converts Ekşi Sözlük pages. The generic article extractor treats the
// left-hand topic list ("gündem") as page chrome and drops it; this adapter
// reads the site's structure instead:
//
//   - home and /basliklar/… pages: the topic list, then featured entries
//   - topic pages: the entries with author, date and page navigation
func Eksi(body []byte, pageURL string) (*doc.Document, error) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(pageURL)
	d := &doc.Document{URL: pageURL, Origin: "live", Lang: "tr"}
	c := newConverter(d, u)
	c.skip = isHidden

	lists := gq.Find("ul#entry-item-list")
	isTopic := lists.Length() == 1 && !lists.HasClass("home-page-entry-list")
	if isTopic {
		eksiTopic(gq, d, c, u)
	} else {
		eksiIndex(gq, d, c)
	}
	if len(d.Blocks) == 0 {
		// Unknown page type: fall back to the article extractor.
		return Article(body, pageURL)
	}
	d.Renumber()
	return d, nil
}

func eksiIndex(gq *goquery.Document, d *doc.Document, c *converter) {
	d.Title = "ekşi sözlük"
	heading := strings.TrimSpace(gq.Find("#index-section h2, .index-section h2, h2").First().Text())
	if heading == "" || len(heading) > 40 {
		heading = "gündem"
	}
	var items [][]doc.Block
	gq.Find("ul.topic-list li").Each(func(_ int, li *goquery.Selection) {
		if li.AttrOr("style", "") != "" || li.HasClass("sponsored-index-item") {
			return
		}
		a := li.Find("a").First()
		href := a.AttrOr("href", "")
		if href == "" {
			return
		}
		count := strings.TrimSpace(a.Find("small").Text())
		a.Find("small").Remove()
		title := collapseSpace(a.Text())
		if title == "" {
			return
		}
		d.Links = append(d.Links, doc.Link{Href: c.resolve(href), Text: title})
		in := doc.Inline{{Text: title, Link: len(d.Links)}}
		if count != "" {
			in = append(in, doc.Span{Text: "  " + count, Style: doc.Italic})
		}
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	})
	if len(items) > 0 {
		d.Meta = append(d.Meta, doc.KV{Key: "başlık", Value: strconv.Itoa(len(items))})
		d.Blocks = append(d.Blocks,
			doc.Heading{Level: 2, Text: doc.Inline{{Text: heading}}},
			doc.List{Items: items})
	}
	// Featured entries on the home page: one per topic.
	var featured []doc.Block
	gq.Find("h1#title").Each(func(_ int, h *goquery.Selection) {
		a := h.Find("a").First()
		title := collapseSpace(a.Text())
		if title == "" {
			return
		}
		d.Links = append(d.Links, doc.Link{Href: c.resolve(a.AttrOr("href", "")), Text: title})
		featured = append(featured, doc.Heading{Level: 3, Text: doc.Inline{{Text: title, Link: len(d.Links)}}})
		h.NextAllFiltered("ul#entry-item-list").First().Find("li[data-id]").Each(func(_ int, li *goquery.Selection) {
			featured = append(featured, eksiEntry(li, d, c)...)
		})
	})
	if len(featured) > 0 {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Heading{Level: 2, Text: doc.Inline{{Text: "seçilmiş entry'ler"}}})
		d.Blocks = append(d.Blocks, featured...)
	}
}

func eksiTopic(gq *goquery.Document, d *doc.Document, c *converter, u *url.URL) {
	h := gq.Find("h1#title").First()
	d.Title = collapseSpace(h.AttrOr("data-title", ""))
	if d.Title == "" {
		d.Title = collapseSpace(h.Text())
	}
	pager := gq.Find("div.pager").First()
	cur, _ := strconv.Atoi(pager.AttrOr("data-currentpage", "1"))
	total, _ := strconv.Atoi(pager.AttrOr("data-pagecount", "1"))
	if cur < 1 {
		cur = 1
	}
	if total > 1 {
		d.Meta = append(d.Meta, doc.KV{Key: "sayfa", Value: strconv.Itoa(cur) + "/" + strconv.Itoa(total)})
	}
	nav := eksiPager(d, u, cur, total)
	if nav != nil {
		d.Blocks = append(d.Blocks, nav)
	}
	first := true
	gq.Find("ul#entry-item-list li[data-id]").Each(func(_ int, li *goquery.Selection) {
		if !first {
			d.Blocks = append(d.Blocks, doc.Rule{})
		}
		first = false
		d.Blocks = append(d.Blocks, eksiEntry(li, d, c)...)
	})
	if nav != nil && !first {
		d.Blocks = append(d.Blocks, doc.Rule{}, eksiPager(d, u, cur, total))
	}
}

// eksiEntry converts one entry: its text, then a signature line.
func eksiEntry(li *goquery.Selection, d *doc.Document, c *converter) []doc.Block {
	var out []doc.Block
	if content := li.Find("div.content").First(); content.Length() > 0 {
		for _, n := range content.Nodes {
			out = append(out, c.blocks(n)...)
		}
	}
	author := collapseSpace(li.Find("a.entry-author").First().Text())
	date := collapseSpace(li.Find("a.entry-date").First().Text())
	sig := "— " + author
	if date != "" {
		sig += " · " + date
	}
	if fav := li.AttrOr("data-favorite-count", "0"); fav != "0" && fav != "" {
		sig += " · ✦" + fav
	}
	if author != "" {
		out = append(out, doc.Paragraph{Text: doc.Inline{{Text: sig, Style: doc.Italic}}})
	}
	return out
}

// eksiPager builds a "‹ önceki · sayfa x/y · sonraki ›" line.
func eksiPager(d *doc.Document, u *url.URL, cur, total int) doc.Block {
	if total <= 1 || u == nil {
		return nil
	}
	pageURL := func(p int) string {
		n := *u
		q := n.Query()
		q.Set("p", strconv.Itoa(p))
		n.RawQuery = q.Encode()
		return n.String()
	}
	var in doc.Inline
	if cur > 1 {
		d.Links = append(d.Links, doc.Link{Href: pageURL(cur - 1), Text: "önceki"})
		in = append(in, doc.Span{Text: "‹ önceki", Link: len(d.Links)}, doc.Span{Text: "   "})
	}
	in = append(in, doc.Span{Text: "sayfa " + strconv.Itoa(cur) + "/" + strconv.Itoa(total), Style: doc.Italic})
	if cur < total {
		d.Links = append(d.Links, doc.Link{Href: pageURL(cur + 1), Text: "sonraki"})
		in = append(in, doc.Span{Text: "   "}, doc.Span{Text: "sonraki ›", Link: len(d.Links)})
		d.Links = append(d.Links, doc.Link{Href: pageURL(total), Text: "son"})
		in = append(in, doc.Span{Text: "   "}, doc.Span{Text: "son »", Link: len(d.Links)})
	}
	return doc.Paragraph{Text: in}
}
