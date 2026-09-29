package sitecat

import (
	"bytes"
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// atomEntries reads OPDS/Atom search results.
func atomEntries(body []byte, pageURL string) []Result {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	var out []Result
	gq.Find("entry").Each(func(_ int, e *goquery.Selection) {
		title := collapse(e.Find("title").First().Text())
		href := ""
		e.Find("link").EachWithBreak(func(_ int, l *goquery.Selection) bool {
			rel := l.AttrOr("rel", "")
			if strings.Contains(rel, "acquisition") || rel == "alternate" || rel == "" {
				href = l.AttrOr("href", "")
				return !strings.Contains(rel, "acquisition")
			}
			return true
		})
		if u, err := base.Parse(href); err == nil && title != "" && href != "" {
			out = append(out, Result{Title: title, URL: u.String(), Extra: collapse(e.Find("author name").First().Text())})
		}
	})
	return out
}

func atomNext(body []byte, pageURL string) string {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	base, _ := url.Parse(pageURL)
	if h, ok := gq.Find(`link[rel="next"]`).First().Attr("href"); ok {
		if u, err := base.Parse(h); err == nil {
			return u.String()
		}
	}
	return ""
}

// opdsBackend searches OPDS catalogs (Atom feeds).
type opdsBackend struct{}

func (opdsBackend) First(p *Profile, q Query) (Request, bool) {
	u, ignored := BuildURL(*p, q)
	return Request{URL: u}, ignored
}

func (opdsBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	out := pageOut{Results: atomEntries(body, pageURL)}
	if n := atomNext(body, pageURL); n != "" {
		out.Next = &Request{URL: n}
	}
	return out, nil
}

func (opdsBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}
