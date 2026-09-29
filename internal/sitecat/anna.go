package sitecat

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"w5f/internal/fetch"
	"w5f/internal/libgen"
)

func isAnna(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil {
		return false
	}
	switch strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") {
	case "annas-archive.gl", "annas-archive.pk", "annas-archive.gd":
		return true
	}
	return false
}

func probeAnna(ctx context.Context, f *fetch.Fetcher, raw, word string) (*Report, error) {
	p := Profile{Home: raw, Name: "Anna's Archive", Added: time.Now().UTC(), Search: SearchSpec{Kind: "anna"}}
	p.ApplyDefaults()
	rep := &Report{Profile: p}
	rs, err := searchAnna(ctx, f, &p, word, nil, 1)
	if err != nil {
		rep.add(false, err.Error())
		var challenge *fetch.ChallengeError
		if errors.As(err, &challenge) {
			rep.CanAdd = true
			rep.add(true, "Anna's Archive search endpoint is known; you can save this catalog now. Searches remain unavailable until the site's verification requirement clears.")
		}
		return rep, nil
	}
	rep.CanAdd = true
	rep.Sample = rs.Results[:min(5, len(rs.Results))]
	rep.add(true, "Anna's Archive search — results and book pages come directly from this catalog")
	return rep, nil
}

func searchAnna(ctx context.Context, f *fetch.Fetcher, p *Profile, raw string, progress Progress, pages int) (*Found, error) {
	defer CurrentProgress.Store("")
	q := ParseQuery(raw)
	if q.Words == "" {
		return nil, errors.New("type some words to search")
	}
	u, err := url.Parse(p.Home)
	if err != nil {
		return nil, err
	}
	// A navigation normally starts at the home page, where the protection
	// layer sets its session cookies. Do this once per cookie session.
	if !f.Offline && f.Client.Jar != nil && len(f.Client.Jar.Cookies(u)) == 0 {
		home := *u
		home.Path = "/"
		home.RawPath = ""
		home.RawQuery = ""
		home.Fragment = ""
		_, _ = f.Get(ctx, &home, fetch.Options{NoStore: true})
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	u.Path = "/search"
	u.RawPath = ""
	u.Fragment = ""
	out := &Found{FieldIgnored: q.Field != ""}
	seen := map[string]bool{}
	for n := 1; n <= pages; n++ {
		u.RawQuery = url.Values{"q": {q.Words}, "page": {itoa(n)}}.Encode()
		CurrentProgress.Store("searching Anna's Archive · page " + itoa(n))
		r, err := f.Get(ctx, u, fetch.Options{})
		if err != nil {
			if out.Pages > 0 {
				return out, err
			}
			return nil, err
		}
		d, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
		if err != nil {
			return nil, err
		}
		added := 0
		d.Find(`a[href*="/md5/"]`).Each(func(_ int, a *goquery.Selection) {
			href, e := r.URL.Parse(a.AttrOr("href", ""))
			if e != nil || href.Host != r.URL.Host {
				return
			}
			hash := strings.TrimPrefix(href.Path, "/md5/")
			if !libgen.ValidHash(hash) || seen[hash] {
				return
			}
			title := collapse(a.Text())
			if title == "" {
				return
			}
			seen[hash] = true
			added++
			out.Results = append(out.Results, Result{Title: title, URL: href.String()})
		})
		out.Pages++
		if progress != nil {
			progress(out.Pages, len(out.Results))
		}
		if len(out.Results) >= p.Search.MaxResults {
			out.Results = out.Results[:p.Search.MaxResults]
			out.Capped = true
			return out, nil
		}
		if added == 0 {
			if out.Pages == 1 && !containsNoResults(collapse(d.Text())) {
				return nil, errors.New("Anna's Archive returned an unrecognized search page")
			}
			return out, nil
		}
		more := false
		d.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			v, e := r.URL.Parse(a.AttrOr("href", ""))
			if e == nil && v.Host == u.Host && v.Path == "/search" && v.Query().Get("q") == q.Words && v.Query().Get("page") == itoa(n+1) {
				more = true
			}
		})
		if !more {
			return out, nil
		}
		if n == pages {
			out.Capped = true
		}
	}
	return out, nil
}
func containsNoResults(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "no results") || strings.Contains(s, "no files found") || strings.Contains(s, "no matches")
}
