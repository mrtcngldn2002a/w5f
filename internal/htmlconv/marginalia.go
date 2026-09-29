package htmlconv

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
)

// IsMarginalia reports Marginalia Search's text-friendly interface.
func IsMarginalia(u *url.URL) bool {
	return strings.EqualFold(u.Hostname(), "old-search.marginalia.nu")
}

// Marginalia converts a Marginalia results page: the article extractor would
// keep the search help and drop the results. When the site is busy it shows a
// short wait page whose own link (meant for browsers without JavaScript)
// leads on; that link is kept for the reader to press, never followed here.
func Marginalia(body []byte, pageURL string) (*doc.Document, error) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(pageURL)
	resolve := func(href string) string {
		r, err := url.Parse(strings.TrimSpace(href))
		if err != nil || base == nil {
			return href
		}
		return base.ResolveReference(r).String()
	}
	d := &doc.Document{URL: pageURL, Origin: "live", Lang: "en"}
	query := strings.TrimSpace(base.Query().Get("query"))
	switch {
	case query == "browse:random":
		d.Title = "Marginalia: random sites"
	case strings.HasPrefix(query, "browse:"):
		d.Title = "Marginalia: sites like " + strings.TrimPrefix(query, "browse:")
	default:
		d.Title = "Marginalia: " + query
	}

	if cards := gq.Find("section.browse-result"); cards.Length() > 0 {
		marginaliaSites(d, cards, resolve)
		return d, nil
	}
	if box := gq.Find(".infobox"); box.Length() > 0 && gq.Find("section.search-result").Length() == 0 {
		href, ok := box.Find(`a[href*="sst="]`).Attr("href")
		if !ok {
			return Article(body, pageURL)
		}
		d.Links = []doc.Link{{Href: resolve(href), Text: "continue"}}
		d.Blocks = []doc.Block{
			doc.Notice{Kind: "info", Text: "Marginalia is busy with bots and asks for a short wait before showing results."},
			doc.Paragraph{Text: doc.Inline{{Text: "Wait a few seconds, then press enter on "}, {Text: "→ continue to the results", Style: doc.Bold, Link: 1}, {Text: ". Too early brings this page back; just wait a little longer.", Style: doc.Italic}}},
		}
		return d, nil
	}

	var items [][]doc.Block
	gq.Find("section.search-result").Each(func(_ int, s *goquery.Selection) {
		a := s.Find("a.title").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		href = resolve(href)
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			return
		}
		title := strings.Join(strings.Fields(a.Text()), " ")
		if title == "" {
			title = href
		}
		d.Links = append(d.Links, doc.Link{Href: href, Text: title})
		host := href
		if hu, err := url.Parse(href); err == nil {
			host = hu.Host
		}
		line := doc.Inline{{Text: host, Style: doc.Italic}}
		if desc := strings.Join(strings.Fields(s.Find(".description").First().Text()), " "); desc != "" {
			line = append(line, doc.Span{Text: " — " + desc})
		}
		items = append(items, []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: title, Link: len(d.Links)}}},
			doc.Paragraph{Text: line},
		})
	})
	if len(items) == 0 && gq.Find("#results").Length() == 0 {
		return Article(body, pageURL) // not a results page
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "Nothing found."})
	} else {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	marginaliaNext(gq, d, resolve)
	return d, nil
}

// marginaliaSites lists the site cards of "explore" pages: random domains
// (browse:random) or domains like one (browse:<domain>), each with its
// "similar" link to steer the exploration.
func marginaliaSites(d *doc.Document, cards *goquery.Selection, resolve func(string) string) {
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Small independent sites from Marginalia's index.", Style: doc.Italic}}})
	var items [][]doc.Block
	cards.Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Find("a[href]").Not(".utils a").First().Attr("href")
		if !ok {
			return
		}
		href = resolve(href)
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			return
		}
		name := strings.TrimSpace(s.Find("h2").First().Text())
		if name == "" {
			name = href
		}
		d.Links = append(d.Links, doc.Link{Href: href, Text: name})
		line := doc.Inline{{Text: name, Link: len(d.Links)}}
		if sim, ok := s.Find(`.utils a[href^="/explore/"]`).Attr("href"); ok {
			d.Links = append(d.Links, doc.Link{Href: resolve(sim), Text: "similar to " + name})
			line = append(line, doc.Span{Text: "   "}, doc.Span{Text: "similar", Style: doc.Italic, Link: len(d.Links)})
		}
		items = append(items, []doc.Block{doc.Paragraph{Text: line}})
	})
	d.Blocks = append(d.Blocks, doc.List{Items: items})
}

func marginaliaNext(gq *goquery.Document, d *doc.Document, resolve func(string) string) {
	if next, ok := gq.Find("a.page-link.active").Next().Filter("a.page-link").Attr("href"); ok {
		d.Next = resolve(next)
		d.Links = append(d.Links, doc.Link{Href: d.Next, Text: "next page"})
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "next page →", Link: len(d.Links)}, {Text: "  (or ])", Style: doc.Italic}}})
	}
}
