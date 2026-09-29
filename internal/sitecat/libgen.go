package sitecat

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"w5f/internal/books"
	"w5f/internal/fetch"
	"w5f/internal/libgen"
	"w5f/internal/store"
)

func libgenClient(f *fetch.Fetcher, p *Profile) (*libgen.Client, error) {
	c, err := libgen.New(f)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(p.Home)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid LibGen catalog home")
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "", "", "", ""
	m := strings.TrimSuffix(u.String(), "/")
	c.SearchMirrors = prependMirror(m, c.SearchMirrors)
	c.DownloadMirrors = prependMirror(m, c.DownloadMirrors)
	return c, nil
}
func prependMirror(m string, ms []string) []string {
	out := []string{m}
	for _, s := range ms {
		if s != m {
			out = append(out, s)
		}
	}
	return out
}
func probeLibgen(ctx context.Context, f *fetch.Fetcher, raw, word string) (*Report, error) {
	p := Profile{Home: raw, Name: "Library Genesis", Added: time.Now().UTC(), Search: SearchSpec{Kind: "libgen"}}
	p.ApplyDefaults()
	rep := &Report{Profile: p}
	c, err := libgenClient(f, &p)
	if err != nil {
		return nil, err
	}
	o, err := libgen.ParseQuery(word)
	if err != nil {
		return nil, err
	}
	rs, err := c.Search(ctx, o)
	if err != nil {
		rep.add(false, err.Error())
		return rep, nil
	}
	rep.CanAdd = true
	rep.add(true, "Library Genesis protocol (halfurness/libgen-cli): search, metadata, mirror fallback and verified downloads")
	for _, b := range rs.Books[:min(5, len(rs.Books))] {
		rep.Sample = append(rep.Sample, Result{Title: b.Title, URL: "libgen:" + b.MD5, Extra: b.Author + " · " + b.Extension})
	}
	return rep, nil
}
func searchLibgen(ctx context.Context, f *fetch.Fetcher, p *Profile, raw string, progress Progress) (*Found, error) {
	c, err := libgenClient(f, p)
	if err != nil {
		return nil, err
	}
	o, err := libgen.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	out := &Found{}
	seen := map[string]bool{}
	defer CurrentProgress.Store("")
	for o.Page = 1; o.Page <= p.Search.MaxPages; o.Page++ {
		rs, err := c.Search(ctx, o)
		if err != nil {
			if out.Pages == 0 {
				return nil, err
			}
			return out, err
		}
		out.Pages++
		for _, b := range rs.Books {
			if seen[b.MD5] {
				continue
			}
			seen[b.MD5] = true
			out.Results = append(out.Results, Result{Title: b.Title, URL: "libgen:" + b.MD5, Extra: strings.Join([]string{b.Author, b.Year, b.Language, b.Extension, b.Size}, " · ")})
			if len(out.Results) >= p.Search.MaxResults {
				out.Capped = true
				return out, nil
			}
		}
		CurrentProgress.Store("searching LibGen page " + itoa(o.Page) + " · " + itoa(len(out.Results)) + " found")
		if progress != nil {
			progress(out.Pages, len(out.Results))
		}
		if !rs.More {
			return out, nil
		}
		if o.Page == p.Search.MaxPages {
			out.Capped = true
		}
	}
	return out, nil
}
func libgenHash(raw string) (string, error) {
	h := strings.TrimPrefix(raw, "libgen:")
	if !libgen.ValidHash(h) {
		return "", errors.New("invalid LibGen book address")
	}
	return strings.ToLower(h), nil
}
func getLibgen(ctx context.Context, f *fetch.Fetcher, db *store.DB, p *Profile, raw string) (store.Book, error) {
	h, err := libgenHash(raw)
	if err != nil {
		return store.Book{}, err
	}
	if b, ok := db.BookBySource("libgen:" + h); ok {
		if _, err := os.Stat(b.Path); err == nil {
			return b, nil
		}
	}
	c, err := libgenClient(f, p)
	if err != nil {
		return store.Book{}, err
	}
	b, err := c.Details(ctx, h)
	if err != nil {
		return store.Book{}, err
	}
	if extFormats["."+b.Extension] == "" {
		return store.Book{}, errors.New("unsupported LibGen book format: " + b.Extension)
	}
	tmp, err := c.Download(ctx, b, books.LibraryDir())
	if err != nil {
		return store.Book{}, err
	}
	defer os.Remove(tmp)
	return books.AddFile(db, tmp, b.Extension, b.Title, b.Author, "libgen:"+h)
}
func catalogSource(p Profile, item string) string {
	if p.Search.Kind == "libgen" {
		return strings.ToLower(item)
	}
	return "site:" + p.ID + ":" + item
}
