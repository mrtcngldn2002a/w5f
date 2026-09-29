// Package feeds is the Periodicals section: the built-in shelf catalog, feed
// syncing (RSS/Atom/JSON via gofeed) and the documents that present shelves,
// item lists and items in the reader.
package feeds

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"

	"w5f/internal/fetch"
	"w5f/internal/store"
)

//go:embed catalog.toml
var builtin []byte

// Shelf groups feeds.
type Shelf struct {
	ID    string `toml:"id"`
	Label string `toml:"label"`
}

// Feed is one catalog entry.
type Feed struct {
	ID    string   `toml:"id"`
	Name  string   `toml:"name"`
	Shelf string   `toml:"shelf"`
	Lang  string   `toml:"lang"`
	URL   []string `toml:"url"`
	Site  string   `toml:"site"`
	Off   bool     `toml:"disabled"`
}

// Catalog is the merged built-in + user catalog.
type Catalog struct {
	Shelves []Shelf `toml:"shelf"`
	Feeds   []Feed  `toml:"feed"`
}

// UserCatalogPath is an optional file (same format) whose entries are added
// to — or, with the same id, replace — the built-in ones. `disabled = true`
// hides a built-in feed.
func UserCatalogPath() string { return filepath.Join(store.DataDir(), "feeds.toml") }

// LoadCatalog returns the built-in catalog merged with the user's file.
func LoadCatalog() (*Catalog, error) {
	var c Catalog
	if err := toml.Unmarshal(builtin, &c); err != nil {
		return nil, fmt.Errorf("built-in catalog: %w", err)
	}
	if b, err := os.ReadFile(UserCatalogPath()); err == nil {
		var u Catalog
		if err := toml.Unmarshal(b, &u); err != nil {
			return nil, fmt.Errorf("%s: %w", UserCatalogPath(), err)
		}
		c.merge(u)
	}
	var feeds []Feed
	for _, f := range c.Feeds {
		if !f.Off {
			feeds = append(feeds, f)
		}
	}
	c.Feeds = feeds
	return &c, nil
}

func (c *Catalog) merge(u Catalog) {
	for _, s := range u.Shelves {
		if c.Shelf(s.ID) == nil {
			c.Shelves = append(c.Shelves, s)
		}
	}
	for _, f := range u.Feeds {
		replaced := false
		for i := range c.Feeds {
			if c.Feeds[i].ID == f.ID {
				c.Feeds[i], replaced = f, true
			}
		}
		if !replaced {
			c.Feeds = append(c.Feeds, f)
		}
	}
}

// Shelf returns a shelf by id.
func (c *Catalog) Shelf(id string) *Shelf {
	for i := range c.Shelves {
		if c.Shelves[i].ID == id {
			return &c.Shelves[i]
		}
	}
	return nil
}

// Feed returns a feed by id.
func (c *Catalog) Feed(id string) *Feed {
	for i := range c.Feeds {
		if c.Feeds[i].ID == id {
			return &c.Feeds[i]
		}
	}
	return nil
}

// FeedsOn lists the feed ids on a shelf.
func (c *Catalog) FeedsOn(shelf string) []string {
	var ids []string
	for _, f := range c.Feeds {
		if f.Shelf == shelf {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

// Result is the outcome of syncing one feed.
type Result struct {
	Feed  string
	New   int
	Err   error
	Found string // the address that worked
}

// Report summarizes a sync run.
type Report struct {
	Results []Result
	Started time.Time
	Took    time.Duration
}

// New returns the number of new items.
func (r Report) New() int {
	n := 0
	for _, x := range r.Results {
		n += x.New
	}
	return n
}

// Failed returns results with errors.
func (r Report) Failed() []Result {
	var out []Result
	for _, x := range r.Results {
		if x.Err != nil {
			out = append(out, x)
		}
	}
	return out
}

// Sync fetches every feed (a few at a time) and stores new items. progress,
// if set, is called after each feed.
func Sync(ctx context.Context, f *fetch.Fetcher, db *store.DB, c *Catalog, only []string, progress func(done, total int)) Report {
	rep := Report{Started: time.Now()}
	feeds := c.Feeds
	if len(only) > 0 {
		feeds = nil
		for _, id := range only {
			if fd := c.Feed(id); fd != nil {
				feeds = append(feeds, *fd)
			}
		}
	}
	jobs := make(chan Feed)
	results := make(chan Result)
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fd := range jobs {
				results <- syncOne(ctx, f, db, fd)
			}
		}()
	}
	go func() {
		for _, fd := range feeds {
			jobs <- fd
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for r := range results {
		rep.Results = append(rep.Results, r)
		if progress != nil {
			progress(len(rep.Results), len(feeds))
		}
	}
	sort.Slice(rep.Results, func(i, j int) bool { return rep.Results[i].Feed < rep.Results[j].Feed })
	rep.Took = time.Since(rep.Started)
	_ = db.Set("last_sync", rep.Started.Format(time.RFC3339))
	return rep
}

func syncOne(ctx context.Context, f *fetch.Fetcher, db *store.DB, fd Feed) Result {
	res := Result{Feed: fd.ID}
	state := db.Feed(fd.ID)
	candidates := fd.URL
	if state.URL != "" {
		candidates = append([]string{state.URL}, candidates...)
	}
	var parsed *gofeed.Feed
	var lastErr error
	seen := map[string]bool{}
	try := func(u string) bool {
		if u == "" || seen[u] {
			return false
		}
		seen[u] = true
		p, err := fetchFeed(ctx, f, u)
		if err != nil {
			lastErr = err
			return false
		}
		parsed, res.Found = p, u
		return true
	}
	ok := false
	for _, u := range candidates {
		if ok = try(u); ok {
			break
		}
	}
	if !ok && fd.Site != "" {
		if found, err := discover(ctx, f, fd.Site); err == nil {
			ok = try(found)
		} else if lastErr == nil {
			lastErr = err
		}
	}
	now := time.Now()
	if !ok {
		if lastErr == nil {
			lastErr = errors.New("no feed address")
		}
		res.Err = lastErr
		_ = db.SetFeed(store.FeedState{ID: fd.ID, URL: state.URL, LastSync: now, Error: lastErr.Error()})
		return res
	}
	// Newest first, and at most maxPerSync per run: some feeds carry their
	// entire archive. On a feed's first sync, items older than a month arrive
	// as already read so the shelves do not open with thousands of "new" items.
	items := parsed.Items
	sort.SliceStable(items, func(i, j int) bool { return published(items[i]).After(published(items[j])) })
	if len(items) > maxPerSync {
		items = items[:maxPerSync]
	}
	firstSync := state.LastOK.IsZero()
	for _, it := range items {
		item := store.Item{
			FeedID:  fd.ID,
			GUID:    firstNonEmpty(it.GUID, it.Link, it.Title),
			URL:     it.Link,
			Title:   strings.TrimSpace(it.Title),
			Summary: it.Description,
			Content: it.Content,
		}
		if len(it.Authors) > 0 && it.Authors[0] != nil {
			item.Author = it.Authors[0].Name
		}
		switch {
		case it.PublishedParsed != nil:
			item.Published = *it.PublishedParsed
		case it.UpdatedParsed != nil:
			item.Published = *it.UpdatedParsed
		default:
			item.Published = now
		}
		if item.Published.After(now.Add(24 * time.Hour)) {
			item.Published = now // feeds with bogus future dates
		}
		if firstSync && item.Published.Before(now.AddDate(0, -1, 0)) {
			item.Read = true
		}
		if isNew, err := db.UpsertItem(item); err == nil && isNew {
			res.New++
		}
	}
	_ = db.Prune(fd.ID, 200)
	_ = db.SetFeed(store.FeedState{ID: fd.ID, URL: res.Found, Title: strings.TrimSpace(parsed.Title), LastSync: now, LastOK: now})
	return res
}

// maxPerSync caps how many entries one feed contributes per sync.
const maxPerSync = 100

func published(it *gofeed.Item) time.Time {
	switch {
	case it.PublishedParsed != nil:
		return *it.PublishedParsed
	case it.UpdatedParsed != nil:
		return *it.UpdatedParsed
	}
	return time.Time{}
}

func fetchFeed(ctx context.Context, f *fetch.Fetcher, raw string) (*gofeed.Feed, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{Revalidate: true})
	if err != nil {
		return nil, err
	}
	p := gofeed.NewParser()
	feed, err := p.Parse(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("%s: not a feed (%v)", raw, err)
	}
	return feed, nil
}

// discover finds a feed address advertised by a page (<link rel=alternate>).
func discover(ctx context.Context, f *fetch.Fetcher, site string) (string, error) {
	u, err := url.Parse(site)
	if err != nil {
		return "", err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return "", err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return "", err
	}
	var found string
	gq.Find(`link[rel~="alternate"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		t := strings.ToLower(s.AttrOr("type", ""))
		if strings.Contains(t, "rss") || strings.Contains(t, "atom") || strings.Contains(t, "feed+json") {
			if h, err := resp.URL.Parse(s.AttrOr("href", "")); err == nil {
				found = h.String()
				return false
			}
		}
		return true
	})
	if found == "" {
		return "", fmt.Errorf("%s: no feed advertised", site)
	}
	return found, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
