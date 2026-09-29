package sitecat

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
	"w5f/internal/search"
)

// EngineEndpoint is DuckDuckGo's HTML endpoint. "" disables the
// search-engine fallback (unit tests).
var EngineEndpoint = search.WebEndpoint

// ErrEngineCheck means DuckDuckGo asked for a human check; W5F stops.
var ErrEngineCheck = errors.New("DuckDuckGo asked for a verification; try again later")

func init() { engineDetector = detectEngine }

// detectEngine offers searching the site through DuckDuckGo (site:host).
func detectEngine(ctx context.Context, s *site) []candidate {
	if EngineEndpoint == "" {
		return nil
	}
	host := strings.TrimPrefix(s.Home.Hostname(), "www.")
	return []candidate{{Spec: SearchSpec{Kind: "engine", Template: host},
		Desc: "search engine (DuckDuckGo, site:" + host + "; results depend on its index)"}}
}

// engineBackend searches one site through DuckDuckGo's HTML results.
type engineBackend struct{}

func (engineBackend) First(p *Profile, q Query) (Request, bool) {
	words := q.Words
	if q.Field != "" { // no field search: keep the words together
		words = `"` + q.Words + `"`
	}
	u, _ := url.Parse(EngineEndpoint)
	u.RawQuery = url.Values{"q": {"site:" + p.Search.Template + " " + words}}.Encode()
	return Request{URL: u.String()}, q.Field != ""
}

func (engineBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	l := strings.ToLower(string(body))
	if strings.Contains(l, "anomaly-modal") || strings.Contains(l, "bots use duckduckgo") {
		return pageOut{}, ErrEngineCheck
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return pageOut{}, err
	}
	host := &url.URL{Host: p.Search.Template}
	var out pageOut
	gq.Find(".result").Each(func(_ int, s *goquery.Selection) {
		if s.HasClass("result--ad") {
			return
		}
		a := s.Find("a.result__a").First()
		href := search.RealURL(a.AttrOr("href", ""))
		u, err := url.Parse(href)
		if href == "" || err != nil || !sameSite(u, host) {
			return
		}
		extra := collapse(s.Find(".result__snippet").First().Text())
		if r := []rune(extra); len(r) > 160 {
			extra = string(r[:157]) + "…"
		}
		out.Results = append(out.Results, Result{Title: collapse(a.Text()), URL: href, Extra: extra})
	})
	out.Next = nextForm(body, pageURL)
	return out, nil
}

func (engineBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}
