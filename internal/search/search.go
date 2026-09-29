// Package search turns queries into readable result documents: web search
// (DuckDuckGo's no-JavaScript HTML endpoint) and wiki search (Crom). Result
// pages are ordinary Documents, so they are read with the same reader.
package search

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/crom"
	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// WebEndpoint is DuckDuckGo's HTML endpoint (variable for tests).
var WebEndpoint = "https://html.duckduckgo.com/html/"

// Web searches the web and returns a result list document.
func Web(ctx context.Context, f *fetch.Fetcher, q string) (*doc.Document, error) {
	u, _ := url.Parse(WebEndpoint)
	v := u.Query()
	v.Set("q", q)
	u.RawQuery = v.Encode()
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}
	var results []result
	gq.Find(".result").Each(func(_ int, s *goquery.Selection) {
		if s.HasClass("result--ad") {
			return // skip adverts
		}
		a := s.Find("a.result__a").First()
		href := RealURL(a.AttrOr("href", ""))
		title := clean(a.Text())
		if href == "" || title == "" {
			return
		}
		results = append(results, result{Title: title, URL: href, Snippet: clean(s.Find(".result__snippet").First().Text())})
	})
	return resultDoc("Web search: "+q, u.String(), results, "No results. Try other words, or prefix w for Wikipedia and scp for the wikis."), nil
}

// Wikis searches the SCP Wiki, Wanderers' Library and the Backrooms via Crom.
func Wikis(ctx context.Context, f *fetch.Fetcher, q string) (*doc.Document, error) {
	// Crom accepts one base URL per search, so each wiki is asked in turn.
	var pages []crom.Page
	var firstErr error
	for _, site := range []string{"http://scp-wiki.wikidot.com", "http://wanderers-library.wikidot.com", "http://backrooms-wiki.wikidot.com"} {
		ps, err := crom.Search(ctx, f, q, []string{site})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(ps) > 10 {
			ps = ps[:10]
		}
		pages = append(pages, ps...)
	}
	if len(pages) == 0 && firstErr != nil {
		return nil, firstErr
	}
	var results []result
	for _, p := range pages {
		host := ""
		if u, err := url.Parse(p.URL); err == nil {
			host = u.Host
		}
		results = append(results, result{Title: p.Title, URL: p.URL,
			Snippet: fmt.Sprintf("%s · rating %+d", siteName(host), p.Rating)})
	}
	return resultDoc("Wiki search: "+q, "", results, "No pages found on the SCP Wiki, Wanderers' Library or the Backrooms."), nil
}

type result struct{ Title, URL, Snippet string }

func resultDoc(title, src string, rs []result, empty string) *doc.Document {
	d := &doc.Document{Title: title, URL: src, Origin: "live", Lang: "en"}
	if len(rs) == 0 {
		d.Blocks = []doc.Block{doc.Notice{Kind: "info", Text: empty}}
		return d
	}
	d.Meta = []doc.KV{{Key: "results", Value: fmt.Sprint(len(rs))}}
	var items [][]doc.Block
	for _, r := range rs {
		d.Links = append(d.Links, doc.Link{Href: r.URL, Text: r.Title})
		item := []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: r.Title, Style: doc.Bold, Link: len(d.Links)}}}}
		var sub doc.Inline
		if r.Snippet != "" {
			sub = append(sub, doc.Span{Text: r.Snippet})
			sub = append(sub, doc.Span{Break: true})
		}
		sub = append(sub, doc.Span{Text: shortURL(r.URL), Style: doc.Italic})
		item = append(item, doc.Paragraph{Text: sub})
		items = append(items, item)
	}
	d.Blocks = []doc.Block{doc.List{Ordered: true, Items: items}}
	return d
}

// RealURL unwraps DuckDuckGo's redirect links (//duckduckgo.com/l/?uddg=…).
func RealURL(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") && u.Path == "/l/" {
		if t := u.Query().Get("uddg"); t != "" {
			return t
		}
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func shortURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	p := strings.TrimSuffix(u.Path, "/")
	if len(p) > 48 {
		p = p[:45] + "…"
	}
	return strings.TrimPrefix(u.Host, "www.") + p
}

func siteName(host string) string {
	switch {
	case strings.Contains(host, "wanderers-library"):
		return "Wanderers' Library"
	case strings.Contains(host, "backrooms"):
		return "Backrooms"
	case strings.Contains(host, "scp-wiki"):
		return "SCP Wiki"
	}
	return host
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }
