package sitecat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"w5f/internal/fetch"
)

// ArchiveBase is the Internet Archive's address (tests use a fixture).
var ArchiveBase = "https://archive.org"

var (
	reArchiveCollection = regexp.MustCompile(`^/details/([^/?#]+)`)
	reLuceneSpecial     = regexp.MustCompile(`[():"\\\[\]{}^~*?!+\-&|/]+`)
)

// detectArchive recognises the Internet Archive; a /details/<collection>
// address restricts searches to that collection.
func detectArchive(ctx context.Context, s *site) []candidate {
	base, err := url.Parse(ArchiveBase)
	if err != nil || strings.TrimPrefix(s.Home.Hostname(), "www.") != strings.TrimPrefix(base.Hostname(), "www.") {
		return nil
	}
	spec := SearchSpec{Kind: "archive"}
	desc := "Internet Archive API (public texts only)"
	if m := reArchiveCollection.FindStringSubmatch(s.Home.Path); m != nil {
		spec.Collection = m[1]
		desc += ", collection " + m[1]
	}
	return []candidate{{Spec: spec, Desc: desc}}
}

// archiveQuery builds the advanced-search query: public texts only.
func archiveQuery(p *Profile, q Query) string {
	w := strings.Join(strings.Fields(reLuceneSpecial.ReplaceAllString(q.Words, " ")), " ")
	var s string
	switch q.Field {
	case "author":
		s = "creator:(" + w + ")"
	case "title":
		s = "title:(" + w + ")"
	default:
		s = "(title:(" + w + ") OR creator:(" + w + ") OR subject:(" + w + "))"
	}
	s += " AND mediatype:texts AND -collection:inlibrary AND -collection:printdisabled AND -collection:lendinglibrary"
	if p.Search.Collection != "" {
		s += " AND collection:(" + p.Search.Collection + ")"
	}
	return s
}

func archiveSearchURL(query string, page int) string {
	v := url.Values{"q": {query}, "fl[]": {"identifier", "title", "creator", "year"}, "rows": {"50"},
		"page": {strconv.Itoa(page)}, "output": {"json"}, "sort[]": {"downloads desc"}}
	return ArchiveBase + "/advancedsearch.php?" + v.Encode()
}

// archiveBackend searches the Internet Archive through its public API.
type archiveBackend struct{}

func (archiveBackend) First(p *Profile, q Query) (Request, bool) {
	return Request{URL: archiveSearchURL(archiveQuery(p, q), 1)}, false
}

// flexString reads a JSON string, number or list of strings.
func flexString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, "; ")
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func (archiveBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	var r struct {
		Response struct {
			NumFound int `json:"numFound"`
			Start    int `json:"start"`
			Docs     []struct {
				Identifier string          `json:"identifier"`
				Title      string          `json:"title"`
				Creator    json.RawMessage `json:"creator"`
				Year       json.RawMessage `json:"year"`
			} `json:"docs"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return pageOut{}, fmt.Errorf("unexpected answer from the Internet Archive API")
	}
	var out pageOut
	for _, d := range r.Response.Docs {
		extra := flexString(d.Creator)
		if y := flexString(d.Year); y != "" {
			if extra != "" {
				extra += " · "
			}
			extra += y
		}
		title := d.Title
		if title == "" {
			title = d.Identifier
		}
		out.Results = append(out.Results, Result{Title: title, URL: ArchiveBase + "/details/" + d.Identifier, Extra: extra})
	}
	if r.Response.Start+len(r.Response.Docs) < r.Response.NumFound && len(r.Response.Docs) > 0 {
		u, err := url.Parse(pageURL)
		if err == nil {
			v := u.Query()
			v.Set("page", strconv.Itoa(pageNo+1))
			u.RawQuery = v.Encode()
			out.Next = &Request{URL: u.String()}
		}
	}
	return out, nil
}

// Item lists an item's public book files (at most 30).
func (archiveBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	iu, err := url.Parse(itemURL)
	if err != nil {
		return nil, err
	}
	id := path.Base(strings.TrimSuffix(iu.Path, "/"))
	mu, _ := url.Parse(ArchiveBase + "/metadata/" + url.PathEscape(id))
	resp, err := f.Get(ctx, mu, fetch.Options{})
	if err != nil {
		return nil, err
	}
	var m struct {
		Metadata map[string]json.RawMessage `json:"metadata"`
		Files    []struct {
			Name    string          `json:"name"`
			Size    json.RawMessage `json:"size"`
			Private json.RawMessage `json:"private"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, fmt.Errorf("unexpected answer from the Internet Archive API")
	}
	if flexString(m.Metadata["access-restricted-item"]) == "true" {
		return nil, &notDownloadable{Reason: "This is a lending-library item on the Internet Archive — it can be borrowed there, not downloaded."}
	}
	var ds []Download
	for _, fl := range m.Files {
		if flexString(fl.Private) == "true" {
			continue
		}
		format := extFormats[strings.ToLower(path.Ext(fl.Name))]
		if format == "" {
			continue
		}
		segs := strings.Split(fl.Name, "/")
		for i := range segs {
			segs[i] = url.PathEscape(segs[i])
		}
		label := fl.Name
		if n, err := strconv.ParseInt(flexString(fl.Size), 10, 64); err == nil && n > 0 {
			label += fmt.Sprintf(" (%.1f MB)", float64(n)/(1<<20))
		}
		ds = append(ds, Download{URL: ArchiveBase + "/download/" + url.PathEscape(id) + "/" + strings.Join(segs, "/"), Format: format, Label: label})
	}
	rank := func(f string) int {
		for i, x := range p.Download.Prefer {
			if x == f {
				return i
			}
		}
		return len(p.Download.Prefer)
	}
	// Sort first, then cap: large items must still offer the preferred format.
	sortStable(ds, func(a, b Download) bool { return rank(a.Format) < rank(b.Format) })
	if len(ds) > 30 {
		ds = ds[:30]
	}
	return ds, nil
}
