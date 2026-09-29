package sitecat

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"w5f/internal/fetch"
)

// Query is a parsed search: an optional field and the words.
type Query struct {
	Field string // "", "author" or "title"
	Words string
}

// ParseQuery reads "author:…" / "title:…" prefixes.
func ParseQuery(s string) Query {
	s = strings.TrimSpace(s)
	for _, f := range []string{"author", "title"} {
		if strings.HasPrefix(strings.ToLower(s), f+":") {
			return Query{Field: f, Words: strings.TrimSpace(s[len(f)+1:])}
		}
	}
	return Query{Words: s}
}

// BuildURL fills the right template. fieldIgnored reports a field prefix the
// site cannot honour (the words are then searched generally).
func BuildURL(p Profile, q Query) (string, bool) {
	t := p.Search.Template
	ignored := false
	if q.Field != "" {
		if ft := p.Search.Fields[q.Field]; ft != "" {
			t = ft
		} else {
			ignored = true
		}
	}
	// {q} before the query string is a path segment ("/search/{q}").
	esc := url.QueryEscape(q.Words)
	if i := strings.Index(t, "{q}"); i >= 0 && (!strings.Contains(t, "?") || i < strings.Index(t, "?")) {
		esc = url.PathEscape(q.Words)
	}
	return strings.ReplaceAll(t, "{q}", esc), ignored
}

// Progress reports search progress (page being read, results so far).
type Progress func(page, found int)

// CurrentProgress holds a short status line for the UI while a search runs.
var CurrentProgress atomic.Value

// Found is a complete search result.
type Found struct {
	Results      []Result
	Pages        int
	Capped       bool
	FieldIgnored bool
}

func normURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Fragment = ""
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// Search runs a full search: it follows the site's result pages until they
// end, repeat, stop adding results, or a cap is reached.
func Search(ctx context.Context, f *fetch.Fetcher, p *Profile, raw string, progress Progress) (*Found, error) {
	f, err := f.ForCatalog()
	if err != nil {
		return nil, err
	}
	p.ApplyDefaults()
	if p.Search.Kind == "anna" {
		return searchAnna(ctx, f, p, raw, progress, p.Search.MaxPages)
	}
	if p.Search.Kind == "libgen" {
		return searchLibgen(ctx, f, p, raw, progress)
	}
	b := backendFor(p.Search.Kind)
	if b == nil || p.Search.Kind == "browse" {
		return nil, errors.New("this catalog has no search; browse the site instead")
	}
	q := ParseQuery(raw)
	if q.Words == "" {
		return nil, errors.New("type some words to search")
	}
	if ls, ok := b.(localSearcher); ok {
		rs, ignored, err := ls.SearchLocal(p, q)
		if err != nil {
			return nil, err
		}
		out := &Found{Results: rs, Pages: 1, FieldIgnored: ignored}
		if len(rs) > p.Search.MaxResults {
			out.Results, out.Capped = rs[:p.Search.MaxResults], true
		}
		return out, nil
	}
	first, ignored := b.First(p, q)
	next := &first
	out := &Found{FieldIgnored: ignored}
	seenReq := map[string]bool{}
	seenItem := map[string]bool{}
	defer CurrentProgress.Store("")
	for next != nil && !seenReq[next.key()] {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if out.Pages >= p.Search.MaxPages {
			out.Capped = true
			break
		}
		seenReq[next.key()] = true
		resp, err := doRequest(ctx, f, *next)
		if err != nil {
			if out.Pages == 0 {
				return nil, err
			}
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			return out, err // pages already read are shown with a warning
		}
		out.Pages++
		po, err := b.Page(ctx, f, p, out.Pages, resp.Body, resp.URL.String())
		if err != nil {
			if out.Pages == 1 {
				return nil, err
			}
			out.Pages--
			return out, err
		}
		added := 0
		for _, r := range po.Results {
			key := normURL(r.URL)
			alt := strings.ToLower(r.Title + "|" + r.Extra)
			if key == "" || seenItem[key] || seenItem[alt] {
				continue
			}
			seenItem[key], seenItem[alt] = true, true
			out.Results = append(out.Results, r)
			added++
			if len(out.Results) >= p.Search.MaxResults {
				out.Capped = true
				break
			}
		}
		if added == 0 {
			if out.Pages > 1 {
				out.Pages-- // a repeated last page does not count
			}
			break
		}
		CurrentProgress.Store("searching page " + itoa(out.Pages) + " · " + itoa(len(out.Results)) + " found")
		if progress != nil {
			progress(out.Pages, len(out.Results))
		}
		if out.Capped {
			break
		}
		next = po.Next
	}
	return out, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
