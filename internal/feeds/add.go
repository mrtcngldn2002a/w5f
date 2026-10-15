package feeds

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/mmcdole/gofeed"

	"w5f/internal/doc"
)

// Following a site's feed from inside W5F (2026-10-15, the owner asked to
// put any site anywhere): g → add on a page offers the Periodical Gallery,
// which finds the page's feed, lets a shelf be chosen and writes the feed
// to feeds.toml. A feed is taken off the same way, with a disabled entry
// appended to the file.

// YoursShelf is the shelf of feeds the owner adds unless they choose another.
var YoursShelf = Shelf{ID: "yours", Label: "Your feeds"}

// FindFeed is the feed of an address: the address itself when it is a
// feed, else the feed its page advertises.
func FindFeed(ctx context.Context, env Env, raw string) (string, error) {
	if _, err := fetchFeed(ctx, env.Fetcher, raw); err == nil {
		return raw, nil
	}
	return discover(ctx, env.Fetcher, raw)
}

// Following is the catalog's feed of an address, or nil.
func (c *Catalog) Following(addrs ...string) *Feed {
	for i, fd := range c.Feeds {
		for _, u := range append(fd.URL, fd.Site) {
			for _, a := range addrs {
				if u != "" && a != "" && sameFeed(u, a) {
					return &c.Feeds[i]
				}
			}
		}
	}
	return nil
}

// UserFeeds are the feeds feeds.toml adds to the built-in catalog (those
// still followed).
func UserFeeds(c *Catalog) []Feed {
	b, err := os.ReadFile(UserCatalogPath())
	if err != nil {
		return nil
	}
	var u Catalog
	if toml.Unmarshal(b, &u) != nil {
		return nil
	}
	var builtinIDs Catalog
	_ = toml.Unmarshal(builtin, &builtinIDs)
	var out []Feed
	seen := map[string]bool{}
	for _, f := range u.Feeds {
		if seen[f.ID] || builtinIDs.Feed(f.ID) != nil {
			continue
		}
		seen[f.ID] = true
		if fd := c.Feed(f.ID); fd != nil {
			out = append(out, *fd)
		}
	}
	return out
}

func addQuery(page, feed, shelf, name string) url.Values {
	v := url.Values{"url": {page}}
	for k, val := range map[string]string{"feed": feed, "shelf": shelf, "name": name} {
		if val != "" {
			v.Set(k, val)
		}
	}
	return v
}

// addRoute is w5f:feeds/add: a check of the address's feed with a shelf to
// choose, and with sure=yes the feed written and synced.
func addRoute(ctx context.Context, env Env, q url.Values) (*doc.Document, error) {
	page := strings.TrimSpace(q.Get("url"))
	if page == "" {
		return nil, errors.New("which site? g → add <address>")
	}
	if !strings.Contains(page, "://") {
		page = "https://" + page
	}
	shelf := q.Get("shelf")
	if shelf == "" {
		shelf = YoursShelf.ID
	}
	feed := q.Get("feed")
	if q.Get("sure") == "yes" && feed != "" {
		return addFeed(ctx, env, page, feed, shelf, q.Get("name"))
	}
	d := &doc.Document{Title: "Periodical Gallery: follow a site", URL: "w5f:feeds/add?" + addQuery(page, feed, shelf, "").Encode(), Origin: "live", Lang: "en"}
	if have := env.Catalog.Following(page, feed); have != nil {
		s := env.Catalog.Shelf(have.Shelf)
		label := have.Shelf
		if s != nil {
			label = s.Label
		}
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "Already followed: " + have.Name + ", on the shelf " + label + "."},
			doc.Paragraph{Text: doc.Inline{{Text: have.Name, Link: link(d, "w5f:feeds/feed/"+have.ID, have.Name)}, {Text: " · "},
				{Text: "stop following it", Link: link(d, "w5f:feeds/remove?"+url.Values{"id": {have.ID}}.Encode(), "stop following")}}})
		return d, nil
	}
	if feed == "" {
		var err error
		if feed, err = FindFeed(ctx, env, page); err != nil {
			d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "No feed was found for " + page + " (" + err.Error() + "). " +
				"A feed address of its own can be given: g → add <feed address>."})
			return d, nil
		}
	}
	parsed, err := fetchFeed(ctx, env.Fetcher, feed)
	if err != nil {
		// Followed anyway, every sync tries it again (and the site's page).
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "The feed does not open now: " + err.Error() + ". It can be followed anyway; every sync tries it again."})
		parsed = &gofeed.Feed{}
	}
	name := strings.Join(strings.Fields(q.Get("name")), " ")
	if name == "" {
		name = strings.Join(strings.Fields(parsed.Title), " ")
	}
	if name == "" {
		name = hostOf(page)
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: name, Style: doc.Bold}, {Text: "  · " + feed, Style: doc.Italic}}})
	var latest [][]doc.Block
	for _, it := range parsed.Items {
		if len(latest) == 3 {
			break
		}
		if t := strings.Join(strings.Fields(it.Title), " "); t != "" {
			latest = append(latest, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: t}}}})
		}
	}
	if len(latest) > 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "lately:", Style: doc.Italic}}}, doc.List{Items: latest})
	}
	shelves := env.Catalog.Shelves
	if env.Catalog.Shelf(YoursShelf.ID) == nil {
		shelves = append([]Shelf{YoursShelf}, shelves...)
	}
	label := shelf
	in := doc.Inline{{Text: "on the shelf  ", Style: doc.Italic}}
	for i, s := range shelves {
		if i > 0 {
			in = append(in, doc.Span{Text: " · "})
		}
		if s.ID == shelf {
			label = s.Label
			in = append(in, doc.Span{Text: s.Label, Style: doc.Bold})
			continue
		}
		in = append(in, doc.Span{Text: s.Label, Link: link(d, "w5f:feeds/add?"+addQuery(page, feed, s.ID, q.Get("name")).Encode(), s.Label)})
	}
	sure := addQuery(page, feed, shelf, name)
	sure.Set("sure", "yes")
	d.Blocks = append(d.Blocks,
		doc.Paragraph{Text: doc.Inline{{Text: "→ Follow it on " + label, Style: doc.Bold, Link: link(d, "w5f:feeds/add?"+sure.Encode(), "follow")}}},
		doc.Paragraph{Text: in},
		doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "It is written to ", Style: doc.Italic}, {Text: UserCatalogPath(), Style: doc.Code},
			{Text: "; the gallery's shelves take it in at once.", Style: doc.Italic}}})
	return d, nil
}

// addFeed writes a feed to feeds.toml, fetches it once and opens its shelf.
func addFeed(ctx context.Context, env Env, page, feed, shelf, name string) (*doc.Document, error) {
	if have := env.Catalog.Following(page, feed); have != nil {
		return shelfDoc(env, have.Shelf, 1, have.Name+" is already followed here.")
	}
	if name == "" {
		name = hostOf(page)
	}
	ids := map[string]bool{}
	for _, f := range env.Catalog.Feeds {
		ids[f.ID] = true
	}
	if b, err := os.ReadFile(UserCatalogPath()); err == nil { // taken-off ids are kept out of the way too
		var u Catalog
		if toml.Unmarshal(b, &u) == nil {
			for _, f := range u.Feeds {
				ids[f.ID] = true
			}
		}
	}
	fd := Feed{ID: unique(slug(name), ids), Name: name, Shelf: shelf, URL: []string{feed}}
	if page != feed {
		fd.Site = page
	}
	add := Catalog{Feeds: []Feed{fd}}
	if env.Catalog.Shelf(shelf) == nil {
		if shelf != YoursShelf.ID {
			return nil, errors.New("unknown shelf " + shelf)
		}
		add.Shelves = []Shelf{YoursShelf}
	}
	if err := appendUser("adding it", "Followed from W5F on "+time.Now().Format("2006-01-02"), add); err != nil {
		return nil, err
	}
	c, err := LoadCatalog()
	if err != nil {
		return nil, err
	}
	env.Catalog = c
	rep := Sync(ctx, env.Fetcher, env.DB, c, []string{fd.ID}, nil)
	note := fmt.Sprintf("Now following %s: %d items came in.", name, rep.New())
	if len(rep.Failed()) > 0 {
		note = "Now following " + name + "; it did not answer this time, the next sync tries again."
	}
	return shelfDoc(env, shelf, 1, note)
}

// removeRoute is w5f:feeds/remove: asks, then appends a disabled entry,
// which takes a feed (a built-in one too) off the gallery. Its items stay
// in the database, out of sight.
func removeRoute(env Env, q url.Values) (*doc.Document, error) {
	id := q.Get("id")
	fd := env.Catalog.Feed(id)
	if fd == nil {
		return shelvesDoc(env, &doc.Notice{Kind: "info", Text: "That feed is not followed."}), nil
	}
	if q.Get("sure") != "yes" { // every removal asks first, as in the Weeding Room
		d := &doc.Document{Title: "Stop following this feed?", URL: "w5f:feeds/remove?" + url.Values{"id": {id}}.Encode(), Origin: "local", Lang: "en"}
		d.Blocks = []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: fd.Name + " · " + strings.Join(fd.URL, ", ")}}},
			doc.Paragraph{Text: doc.Inline{{Text: "It leaves the Periodical Gallery; what it brought stays searchable. g → add <address> follows it again.", Style: doc.Italic}}},
			doc.Paragraph{Text: doc.Inline{{Text: "No, keep it", Link: link(d, "w5f:feeds/feed/"+id, "keep")}, {Text: " · "},
				{Text: "Yes, stop following it", Style: doc.Bold, Link: link(d, "w5f:feeds/remove?"+url.Values{"id": {id}, "sure": {"yes"}}.Encode(), "remove")}}},
		}
		return d, nil
	}
	off := *fd
	off.Off = true
	if err := appendUser("taking it off", "Taken off in W5F on "+time.Now().Format("2006-01-02"), Catalog{Feeds: []Feed{off}}); err != nil {
		return nil, err
	}
	c, err := LoadCatalog()
	if err != nil {
		return nil, err
	}
	env.Catalog = c
	return shelvesDoc(env, &doc.Notice{Kind: "info", Text: "No longer following " + fd.Name + "."}), nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return raw
}
