package sitecat

import (
	"bytes"
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// htmlBackend searches sites through their own HTML result pages:
// OpenSearch HTML templates, GET and POST forms, WordPress and browse-only.
type htmlBackend struct{}

func (htmlBackend) First(p *Profile, q Query) (Request, bool) {
	if !strings.EqualFold(p.Search.Method, "POST") {
		u, ignored := BuildURL(*p, q)
		return Request{URL: u}, ignored
	}
	body, ignored := p.Search.Body, false
	if q.Field != "" {
		if fb := p.Search.Fields[q.Field]; fb != "" {
			body = fb
		} else {
			ignored = true
		}
	}
	form, _ := url.ParseQuery(strings.ReplaceAll(body, "{q}", url.QueryEscape(q.Words)))
	return Request{Method: "POST", URL: p.Search.Template, Form: form}, ignored
}

func (htmlBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	rs := Results(body, pageURL, p.Layout)
	if len(rs) == 0 && pageNo == 1 {
		// While checking (no layout yet) any list is learned; later only a
		// related one — never a sidebar on a "no results" page.
		if nl, r2 := LearnLayout(body, pageURL); nl.Item != "" &&
			(p.Layout.Item == "" || nl.Parent == p.Layout.Parent || nl.Item == p.Layout.Item) {
			rs, p.Layout = r2, nl
		}
	}
	out := pageOut{Results: rs}
	if n := NextPage(body, pageURL); n != "" {
		out.Next = &Request{URL: n}
	} else {
		out.Next = nextForm(body, pageURL)
	}
	return out, nil
}

func (htmlBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}

// detectHTML proposes every search method found on the home page and on a
// linked search page.
func detectHTML(ctx context.Context, s *site) []candidate {
	var out []candidate
	seen := map[string]bool{}
	for i, pg := range s.Pages {
		for _, c := range discoverAll(ctx, s.F, pg.Body, pg.URL.String()) {
			k := c.Spec.Kind + " " + c.Spec.Template + " " + c.Spec.Body
			if seen[k] {
				continue
			}
			seen[k] = true
			if i > 0 {
				c.Desc += " on " + pg.URL.String()
			}
			out = append(out, c)
		}
	}
	return out
}

// detectBrowse offers the front page's own book list (browse-only).
func detectBrowse(ctx context.Context, s *site) []candidate {
	return []candidate{{Spec: SearchSpec{Kind: "browse"}, Desc: "the front page's book list"}}
}

var reNextLabel = regexp.MustCompile(`(?i)^(next|next page|more results|›|»|>|→|sonraki|next ›|next »|next >|next →)$`)

// nextForm finds a "next page" button in a form and returns the request it
// would send (POST searches and DuckDuckGo page this way).
func nextForm(body []byte, pageURL string) *Request {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var out *Request
	gq.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		// A paging form carries hidden state only: a visible text box is the
		// site's search, an e-mail box a newsletter.
		if notSearchForm(form) || form.Find(`input:not([type]), input[type="text"], input[type="search"]`).Length() > 0 {
			return true
		}
		var submit *goquery.Selection
		form.Find(`input[type="submit"], button`).EachWithBreak(func(_ int, b *goquery.Selection) bool {
			if reNextLabel.MatchString(collapse(b.AttrOr("value", ""))) || reNextLabel.MatchString(collapse(b.Text())) {
				submit = b
				return false
			}
			return true
		})
		if submit == nil {
			return true
		}
		action, err := base.Parse(form.AttrOr("action", ""))
		if err != nil {
			return true
		}
		v := url.Values{}
		form.Find("input[name]").Each(func(_ int, in *goquery.Selection) {
			switch strings.ToLower(in.AttrOr("type", "text")) {
			case "submit", "button", "image", "reset":
				return
			case "checkbox", "radio":
				if _, on := in.Attr("checked"); !on {
					return
				}
			}
			v.Add(in.AttrOr("name", ""), in.AttrOr("value", ""))
		})
		if n := submit.AttrOr("name", ""); n != "" {
			v.Add(n, submit.AttrOr("value", ""))
		}
		if strings.EqualFold(strings.TrimSpace(form.AttrOr("method", "")), "post") {
			out = &Request{Method: "POST", URL: action.String(), Form: v}
		} else {
			action.RawQuery = v.Encode()
			out = &Request{Method: "GET", URL: action.String()}
		}
		return false
	})
	return out
}
