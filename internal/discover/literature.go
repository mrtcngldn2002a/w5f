package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"w5f/internal/fetch"
)

func init() { families = append(families, literature{}) }

// literature: what literary magazines and poetry sites keep online, not
// books (chosen with the owner, 2026-10-12): essays, interviews, stories in
// translation and poems.
type literature struct{}

type litSource struct {
	name   string
	weight int
	pick   func(context.Context, Env) (target, title string, err error)
}

var litSources = []litSource{
	{"The Believer", 3, wpPick("https://www.thebeliever.net")},
	{"Electric Literature", 2, wpPick("https://electricliterature.com")},
	{"Words Without Borders", 2, sitemapPick("https://wordswithoutborders.org/sitemap_index.xml",
		`/post-sitemap\d*\.xml$`, `/read/article/`, `week-in-translation|translator-relay|book-club|translation-roundup`)},
	{"Poets.org", 2, sitemapPick("https://poets.org/sitemap.xml", `sitemap\.xml\?page=\d+$`, `/poem/`, ``)},
	{"Poetry Foundation", 3, poetryPick},
}

func (literature) Name() string { return "literature" }
func (literature) sources() int { return len(litSources) }

func (literature) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawWeighted(ctx, env, "literature", litSources)
}

// drawWeighted tries up to three of a family's sources, the heavier first
// more often.
func drawWeighted(ctx context.Context, env Env, family string, sources []litSource) (Draw, error) {
	w := make([]int, len(sources))
	for i, s := range sources {
		w[i] = s.weight
	}
	var errs []string
	for n, i := range weightedOrder(w) {
		if n == 3 {
			break
		}
		s := sources[i]
		target, title, err := s.pick(ctx, env)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		why := family + "/" + s.name
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// rawDo sends a request on the fetcher's client (no cache, headers and POST
// bodies allowed) and returns the body and the response headers.
func rawDo(ctx context.Context, f *fetch.Fetcher, method, raw string, header http.Header, body []byte) ([]byte, http.Header, error) {
	if f == nil || f.Offline {
		return nil, nil, errors.New("offline")
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b, resp.Header, nil
}

// --- WordPress sites: the REST API lists every post, and says how many.

var (
	wpTotals   = map[string]int{}
	wpTotalsMu sync.Mutex
)

// wpPick takes a random post of a WordPress site (the REST API's posts, one
// per page: page N is the N-th post).
func wpPick(base string) func(context.Context, Env) (string, string, error) {
	return func(ctx context.Context, env Env) (string, string, error) {
		wpTotalsMu.Lock()
		total := wpTotals[base]
		wpTotalsMu.Unlock()
		if total == 0 {
			_, h, err := rawDo(ctx, env.Fetcher, "GET", base+"/wp-json/wp/v2/posts?per_page=1&_fields=id", nil, nil)
			if err != nil {
				return "", "", err
			}
			total, _ = strconv.Atoi(h.Get("X-WP-Total"))
			if total == 0 {
				return "", "", errors.New("the REST API does not say how many posts")
			}
			wpTotalsMu.Lock()
			wpTotals[base] = total
			wpTotalsMu.Unlock()
		}
		b, _, err := rawDo(ctx, env.Fetcher, "GET",
			fmt.Sprintf("%s/wp-json/wp/v2/posts?per_page=1&page=%d&_fields=link,title", base, 1+rand.IntN(total)), nil, nil)
		if err != nil {
			return "", "", err
		}
		var posts []struct {
			Link  string `json:"link"`
			Title struct {
				Rendered string `json:"rendered"`
			} `json:"title"`
		}
		if err := json.Unmarshal(b, &posts); err != nil || len(posts) == 0 || posts[0].Link == "" {
			return "", "", errors.New("no post on that page")
		}
		return posts[0].Link, cleanTitle(posts[0].Title.Rendered), nil
	}
}

var reTag = regexp.MustCompile(`<[^>]+>`)

func cleanTitle(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(reTag.ReplaceAllString(s, ""))), " ")
}

// --- Sites with an XML sitemap: a random address of the kind wanted.

type sitemapXML struct {
	Sitemaps []string `xml:"sitemap>loc"`
	URLs     []string `xml:"url>loc"`
}

func sitemapPick(index, sitemapRe, wantRe, rejectRe string) func(context.Context, Env) (string, string, error) {
	sm, want := regexp.MustCompile(sitemapRe), regexp.MustCompile(wantRe)
	var reject *regexp.Regexp
	if rejectRe != "" {
		reject = regexp.MustCompile(rejectRe)
	}
	read := func(ctx context.Context, f *fetch.Fetcher, raw string) (sitemapXML, error) {
		var x sitemapXML
		u, err := url.Parse(raw)
		if err != nil {
			return x, err
		}
		resp, err := f.Get(ctx, u, fetch.Options{})
		if err != nil {
			return x, err
		}
		return x, xml.Unmarshal(resp.Body, &x)
	}
	return func(ctx context.Context, env Env) (string, string, error) {
		top, err := read(ctx, env.Fetcher, index)
		if err != nil {
			return "", "", err
		}
		var maps []string
		for _, s := range top.Sitemaps {
			if sm.MatchString(s) {
				maps = append(maps, s)
			}
		}
		if len(maps) == 0 {
			return "", "", errors.New("no sitemap of the kind wanted")
		}
		// A sitemap may hold none of what is wanted (poets.org keeps events
		// and anthologies in the first ones): try a few.
		for _, i := range rand.Perm(len(maps))[:min(len(maps), 8)] {
			page, err := read(ctx, env.Fetcher, maps[i])
			if err != nil {
				continue
			}
			var ok []string
			for _, u := range page.URLs {
				if want.MatchString(u) && (reject == nil || !reject.MatchString(u)) {
					ok = append(ok, u)
				}
			}
			if len(ok) > 0 {
				u := ok[rand.IntN(len(ok))]
				return u, slugTitle(u), nil
			}
		}
		return "", "", errors.New("no address of the kind wanted")
	}
}

// slugTitle makes a title of the last part of an address.
func slugTitle(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	s := strings.Trim(u.Path, "/")
	s = s[strings.LastIndex(s, "/")+1:]
	return strings.ReplaceAll(s, "-", " ")
}

// --- Poetry Foundation: its poem list is a public Algolia index (the site's
// own search). The page says where it is and gives the search-only key the
// site's scripts use, so the draw reads them from the page, and keeps them a
// week. A random word, a random page of the hits: Algolia gives no more than
// a thousand hits to a query.

type algoliaConf struct {
	App   string    `json:"app"`
	Key   string    `json:"key"`
	Index string    `json:"index"`
	At    time.Time `json:"at"`
}

var (
	poetryConfURL = "https://www.poetryfoundation.org/poems/browse"
	poetrySite    = "https://www.poetryfoundation.org/"
	// algoliaHost makes the search address of an application; a test replaces it.
	algoliaHost = func(app string) string { return "https://" + app + "-dsn.algolia.net" }
	reAlgolia   = regexp.MustCompile(`algolia:\{apiKey:"([^"]+)",applicationId:"([^"]+)"`)
	reAlgIndex  = regexp.MustCompile(`globalIndex:"([^"]+)"`)
)

const poetryConfKey = "discover:poetry-algolia"

func poetryConf(ctx context.Context, env Env, refresh bool) (algoliaConf, error) {
	var c algoliaConf
	if !refresh && env.DB != nil && json.Unmarshal([]byte(env.DB.Get(poetryConfKey)), &c) == nil &&
		c.Key != "" && time.Since(c.At) < 7*24*time.Hour {
		return c, nil
	}
	u, _ := url.Parse(poetryConfURL)
	resp, err := env.Fetcher.Get(ctx, u, fetch.Options{})
	if err != nil {
		return c, err
	}
	k, i := reAlgolia.FindSubmatch(resp.Body), reAlgIndex.FindSubmatch(resp.Body)
	if k == nil || i == nil {
		return c, errors.New("the page no longer says where its poems are searched")
	}
	c = algoliaConf{App: string(k[2]), Key: string(k[1]), Index: string(i[1]), At: time.Now()}
	if b, err := json.Marshal(c); err == nil && env.DB != nil {
		_ = env.DB.Set(poetryConfKey, string(b))
	}
	return c, nil
}

var poetryWords = strings.Fields(`sea mother war night garden snow god city love dust river bone winter ghost mirror fire
	father moon house bird salt window water grief forest bread hands blood sleep rain stone road winter light door
	horse star ocean wolf song dream silence sun field tree body names letter room island rose smoke bell`)

func poetryPick(ctx context.Context, env Env) (string, string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		c, err := poetryConf(ctx, env, attempt > 0)
		if err != nil {
			return "", "", err
		}
		params := url.Values{
			"query": {poetryWords[rand.IntN(len(poetryWords))]}, "hitsPerPage": {"20"}, "filters": {"type:poems"},
			"page": {strconv.Itoa(rand.IntN(25))}, "attributesToRetrieve": {"title,uri,authors,content"}, "attributesToHighlight": {""},
		}
		body, _ := json.Marshal(map[string]string{"params": params.Encode()})
		h := http.Header{"X-Algolia-Application-Id": {c.App}, "X-Algolia-Api-Key": {c.Key}, "Content-Type": {"application/json"}}
		b, _, err := rawDo(ctx, env.Fetcher, "POST", algoliaHost(c.App)+"/1/indexes/"+url.PathEscape(c.Index)+"/query", h, body)
		if err != nil {
			lastErr = err // a changed key shows as a refusal: read the page again
			continue
		}
		var r struct {
			Hits []struct {
				Title   string   `json:"title"`
				URI     string   `json:"uri"`
				Authors []string `json:"authors"`
				Content string   `json:"content"`
			} `json:"hits"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return "", "", errors.New("unexpected answer from the poem search")
		}
		// Poems of the old magazine issues (before 2013) are scans with no
		// text of their own: their entries are empty. Skip those.
		var ok []int
		for i, hit := range r.Hits {
			if hit.URI != "" && len([]rune(hit.Content)) >= 120 {
				ok = append(ok, i)
			}
		}
		if len(ok) == 0 {
			lastErr = errors.New("no poem with a text among the hits")
			continue
		}
		hit := r.Hits[ok[rand.IntN(len(ok))]]
		title := cleanTitle(hit.Title)
		if len(hit.Authors) > 0 {
			title += " by " + cleanTitle(hit.Authors[0])
		}
		return poetrySite + strings.TrimPrefix(hit.URI, "/"), title, nil
	}
	return "", "", lastErr
}
