package sitecat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"w5f/internal/fetch"
)

// detectDSpace recognises DSpace 7+ repositories (an Angular "ds-app"
// front end) and checks their REST API.
func detectDSpace(ctx context.Context, s *site) []candidate {
	if !strings.Contains(strings.ToLower(string(s.Body)), "<ds-app") && !strings.Contains(s.Generator, "dspace") {
		return nil
	}
	api := s.Home.Scheme + "://" + s.Home.Host + "/server/api"
	u, _ := url.Parse(api)
	resp, err := s.F.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil
	}
	var root struct {
		Version string `json:"dspaceVersion"`
	}
	if json.Unmarshal(resp.Body, &root) != nil || root.Version == "" {
		return nil
	}
	return []candidate{{Spec: SearchSpec{Kind: "dspace", API: api}, Desc: root.Version + " REST API"}}
}

// dspaceBackend searches a DSpace 7+ repository through its REST API.
type dspaceBackend struct{}

func (dspaceBackend) First(p *Profile, q Query) (Request, bool) {
	w := strings.Join(strings.Fields(reLuceneSpecial.ReplaceAllString(q.Words, " ")), " ")
	query := q.Words
	switch q.Field {
	case "author":
		query = "dc.contributor.author:(" + w + ")"
	case "title":
		query = "dc.title:(" + w + ")"
	}
	v := url.Values{"query": {query}, "dsoType": {"ITEM"}, "page": {"0"}, "size": {"20"}}
	return Request{URL: p.Search.API + "/discover/search/objects?" + v.Encode()}, false
}

type dspaceValues map[string][]struct {
	Value string `json:"value"`
}

func (m dspaceValues) all(key string) []string {
	var out []string
	for _, v := range m[key] {
		out = append(out, v.Value)
	}
	return out
}

func (dspaceBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	var r struct {
		Embedded struct {
			SearchResult struct {
				Page struct {
					Number     int `json:"number"`
					TotalPages int `json:"totalPages"`
				} `json:"page"`
				Embedded struct {
					Objects []struct {
						Embedded struct {
							Object struct {
								UUID     string       `json:"uuid"`
								Name     string       `json:"name"`
								Metadata dspaceValues `json:"metadata"`
							} `json:"indexableObject"`
						} `json:"_embedded"`
					} `json:"objects"`
				} `json:"_embedded"`
			} `json:"searchResult"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return pageOut{}, fmt.Errorf("unexpected answer from the DSpace API")
	}
	api, _ := url.Parse(p.Search.API)
	origin := api.Scheme + "://" + api.Host
	sr := r.Embedded.SearchResult
	var out pageOut
	for _, o := range sr.Embedded.Objects {
		obj := o.Embedded.Object
		title := obj.Name
		if t := obj.Metadata.all("dc.title"); len(t) > 0 {
			title = t[0]
		}
		extra := strings.Join(obj.Metadata.all("dc.contributor.author"), "; ")
		if d := obj.Metadata.all("dc.date.issued"); len(d) > 0 {
			if extra != "" {
				extra += " · "
			}
			extra += d[0]
		}
		out.Results = append(out.Results, Result{Title: title, URL: origin + "/items/" + obj.UUID, Extra: extra})
	}
	if sr.Page.Number+1 < sr.Page.TotalPages {
		if u, err := url.Parse(pageURL); err == nil {
			v := u.Query()
			v.Set("page", strconv.Itoa(sr.Page.Number+1))
			u.RawQuery = v.Encode()
			out.Next = &Request{URL: u.String()}
		}
	}
	return out, nil
}

// Item lists the book files of an item's ORIGINAL bundle.
func (dspaceBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	iu, err := url.Parse(itemURL)
	if err != nil {
		return nil, err
	}
	uuid := path.Base(strings.TrimSuffix(iu.Path, "/"))
	getJSON := func(u string, out any) error {
		pu, err := url.Parse(u)
		if err != nil {
			return err
		}
		resp, err := f.Get(ctx, pu, fetch.Options{})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return fmt.Errorf("unexpected answer from the DSpace API")
		}
		return nil
	}
	var bundles struct {
		Embedded struct {
			Bundles []struct {
				Name  string `json:"name"`
				Links struct {
					Bitstreams struct {
						Href string `json:"href"`
					} `json:"bitstreams"`
				} `json:"_links"`
			} `json:"bundles"`
		} `json:"_embedded"`
	}
	if err := getJSON(p.Search.API+"/core/items/"+url.PathEscape(uuid)+"/bundles", &bundles); err != nil {
		return nil, err
	}
	var ds []Download
	for _, b := range bundles.Embedded.Bundles {
		if b.Name != "ORIGINAL" {
			continue
		}
		var bs struct {
			Embedded struct {
				Bitstreams []struct {
					Name  string `json:"name"`
					Size  int64  `json:"sizeBytes"`
					Links struct {
						Content struct {
							Href string `json:"href"`
						} `json:"content"`
					} `json:"_links"`
				} `json:"bitstreams"`
			} `json:"_embedded"`
		}
		if err := getJSON(b.Links.Bitstreams.Href, &bs); err != nil {
			return nil, err
		}
		for _, x := range bs.Embedded.Bitstreams {
			format := extFormats[strings.ToLower(path.Ext(x.Name))]
			if format == "" {
				continue
			}
			label := x.Name
			if x.Size > 0 {
				label += fmt.Sprintf(" (%.1f MB)", float64(x.Size)/(1<<20))
			}
			ds = append(ds, Download{URL: x.Links.Content.Href, Format: format, Label: label})
		}
	}
	return ds, nil
}
