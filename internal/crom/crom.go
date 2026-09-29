// Package crom talks to Crom (crom.avn.sh), the community index of the SCP
// Wiki, Wanderers' Library, the Backrooms and related Wikidot sites. The API
// is documented as alpha, so W5F treats it as an optional helper: callers must
// cope with errors and fall back to plain Wikidot pages.
package crom

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"w5f/internal/fetch"
)

// Endpoint is the GraphQL endpoint; a variable so tests can point elsewhere.
var Endpoint = "https://apiv1.crom.avn.sh/graphql"

// UpgradeHTTPS rewrites Crom's http:// page URLs to https (disabled in tests).
var UpgradeHTTPS = true

// Preset is a named random-page filter.
type Preset struct {
	Name     string
	Label    string
	Site     string // base URL as Crom knows it (http://…)
	AllTags  []string
	NoneTags []string
	// Reject filters out results Crom's tag filter cannot express, e.g.
	// translated SCPs hosted on the English wiki.
	Reject *regexp.Regexp
}

// Presets are the random sources offered by the reader.
var Presets = map[string]Preset{
	"scp": {Name: "scp", Label: "random SCP", Site: "http://scp-wiki.wikidot.com",
		AllTags: []string{"scp"}, NoneTags: []string{"joke", "explained"},
		Reject: regexp.MustCompile(`/scp-[a-z]{2,3}-`)},
	"tale": {Name: "tale", Label: "random SCP tale", Site: "http://scp-wiki.wikidot.com",
		AllTags: []string{"tale"}, NoneTags: []string{"hub"}},
	"wl": {Name: "wl", Label: "random Wanderers' Library page", Site: "http://wanderers-library.wikidot.com",
		NoneTags: []string{"hub", "admin", "author", "guide", "_redirect"}},
	"backrooms": {Name: "backrooms", Label: "random Backrooms page", Site: "http://backrooms-wiki.wikidot.com",
		NoneTags: []string{"hub", "admin", "author", "guide", "_redirect"}},
}

// PresetForHost picks the random preset matching the site being read.
func PresetForHost(host string) Preset {
	switch {
	case strings.Contains(host, "wanderers-library"):
		return Presets["wl"]
	case strings.Contains(host, "backrooms-wiki"):
		return Presets["backrooms"]
	}
	return Presets["scp"]
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type randomReply struct {
	Data struct {
		RandomPage *struct {
			Page struct {
				URL string `json:"url"`
			} `json:"page"`
		} `json:"randomPage"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const randomQuery = `query R($site: [String!], $all: [String!], $none: [String!]) {
  randomPage(filter: {anyBaseUrl: $site, allTags: $all, noneTags: $none}) { page { url } }
}`

// Page is a search hit.
type Page struct {
	URL    string
	Title  string
	Rating int
}

type searchReply struct {
	Data struct {
		SearchPages []struct {
			URL         string `json:"url"`
			WikidotInfo *struct {
				Title  string `json:"title"`
				Rating int    `json:"rating"`
			} `json:"wikidotInfo"`
		} `json:"searchPages"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const searchQuery = `query S($q: String!, $site: [String!]) {
  searchPages(query: $q, filter: {anyBaseUrl: $site}) { url wikidotInfo { title rating } }
}`

// Search finds pages by title/text on the given sites.
func Search(ctx context.Context, f *fetch.Fetcher, q string, sites []string) ([]Page, error) {
	var r searchReply
	err := f.PostJSON(ctx, Endpoint, gqlRequest{Query: searchQuery, Variables: map[string]any{"q": q, "site": sites}}, &r)
	if err != nil {
		return nil, err
	}
	if len(r.Errors) > 0 {
		return nil, errors.New("crom: " + r.Errors[0].Message)
	}
	var out []Page
	for _, p := range r.Data.SearchPages {
		pg := Page{URL: p.URL}
		if UpgradeHTTPS {
			pg.URL = strings.Replace(pg.URL, "http://", "https://", 1)
		}
		if p.WikidotInfo != nil {
			pg.Title, pg.Rating = p.WikidotInfo.Title, p.WikidotInfo.Rating
		}
		if pg.Title == "" {
			pg.Title = pg.URL
		}
		out = append(out, pg)
	}
	return out, nil
}

// Random returns the https URL of a random page for the preset.
func Random(ctx context.Context, f *fetch.Fetcher, p Preset) (string, error) {
	vars := map[string]any{"site": []string{p.Site}}
	if len(p.AllTags) > 0 {
		vars["all"] = p.AllTags
	}
	if len(p.NoneTags) > 0 {
		vars["none"] = p.NoneTags
	}
	for try := 0; try < 5; try++ {
		var r randomReply
		if err := f.PostJSON(ctx, Endpoint, gqlRequest{Query: randomQuery, Variables: vars}, &r); err != nil {
			return "", err
		}
		if len(r.Errors) > 0 {
			return "", errors.New("crom: " + r.Errors[0].Message)
		}
		if r.Data.RandomPage == nil || r.Data.RandomPage.Page.URL == "" {
			return "", errors.New("crom: no page returned")
		}
		u := r.Data.RandomPage.Page.URL
		if UpgradeHTTPS {
			u = strings.Replace(u, "http://", "https://", 1)
		}
		if p.Reject != nil && p.Reject.MatchString(u) {
			continue
		}
		return u, nil
	}
	return "", errors.New("crom: no matching page after several tries")
}
