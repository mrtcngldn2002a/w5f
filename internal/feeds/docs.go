package feeds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/htmlconv"
	"w5f/internal/store"
)

// Env carries what the Periodicals pages need.
type Env struct {
	Fetcher *fetch.Fetcher
	DB      *store.DB
	Catalog *Catalog
	// Article loads a web page as a document (full-text for short feeds).
	Article func(ctx context.Context, url string) (*doc.Document, error)
}

// fullTextBelow: feed content shorter than this (non-space characters) is
// treated as an excerpt and the full article is fetched from the site.
const fullTextBelow = 900

const pageSize = 60

// IsTarget reports whether target belongs to this section.
func IsTarget(target string) bool {
	return strings.HasPrefix(target, "w5f:feeds") || strings.HasPrefix(target, "w5f:item/")
}

// Route builds the document for a Periodicals address.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSuffix(u.Opaque, "/")
	page, _ := strconv.Atoi(u.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	switch {
	case strings.HasPrefix(path, "item/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(path, "item/"), 10, 64)
		if err != nil {
			return nil, errors.New("bad item address")
		}
		return itemDoc(ctx, id, env)
	case path == "feeds":
		var notice *doc.Notice
		if env.DB.Get("last_sync") == "" {
			// First visit: fill the shelves once.
			rep := Sync(ctx, env.Fetcher, env.DB, env.Catalog, nil, nil)
			notice = syncNotice(rep)
		}
		return shelvesDoc(env, notice), nil
	case path == "feeds/sync":
		rep := Sync(ctx, env.Fetcher, env.DB, env.Catalog, nil, nil)
		return shelvesDoc(env, syncNotice(rep)), nil
	case path == "feeds/status":
		return statusDoc(env), nil
	case path == "feeds/import":
		return importDoc(env, u.Query().Get("f"))
	case path == "feeds/export":
		return exportDoc(env, u.Query().Get("f"))
	case path == "feeds/unread":
		return listDoc(env, "All unread", "w5f:feeds/unread", store.Query{Unread: true}, page, ""), nil
	case path == "feeds/starred":
		return listDoc(env, "Starred", "w5f:feeds/starred", store.Query{Starred: true}, page, ""), nil
	case strings.HasPrefix(path, "feeds/read/"):
		shelf := strings.TrimPrefix(path, "feeds/read/")
		if err := env.DB.MarkAllRead(env.Catalog.FeedsOn(shelf)); err != nil {
			return nil, err
		}
		return shelfDoc(env, shelf, 1, "Marked every item on this shelf as read.")
	case strings.HasPrefix(path, "feeds/shelf/"):
		return shelfDoc(env, strings.TrimPrefix(path, "feeds/shelf/"), page, "")
	case strings.HasPrefix(path, "feeds/feed/"):
		id := strings.TrimPrefix(path, "feeds/feed/")
		fd := env.Catalog.Feed(id)
		if fd == nil {
			return nil, errors.New("unknown feed " + id)
		}
		return listDoc(env, fd.Name, "w5f:feeds/feed/"+id, store.Query{Feeds: []string{id}}, page, ""), nil
	}
	return nil, errors.New("unknown periodicals address: " + target)
}

func syncNotice(rep Report) *doc.Notice {
	failed := len(rep.Failed())
	text := fmt.Sprintf("Synced %d feeds in %s: %d new items.", len(rep.Results), rep.Took.Round(time.Second), rep.New())
	if failed > 0 {
		text += fmt.Sprintf(" %d feeds could not be read (see feed status).", failed)
	}
	return &doc.Notice{Kind: "info", Text: text}
}

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func shelvesDoc(env Env, notice *doc.Notice) *doc.Document {
	d := &doc.Document{Title: "Periodicals", URL: "w5f:feeds", Origin: "local", Lang: "en"}
	counts, _ := env.DB.Counts()
	totalUnread := 0
	for _, c := range counts {
		totalUnread += c[0]
	}
	last := "never"
	if t, err := time.Parse(time.RFC3339, env.DB.Get("last_sync")); err == nil {
		last = ago(t)
	}
	d.Meta = []doc.KV{{Key: "unread", Value: strconv.Itoa(totalUnread)}, {Key: "synced", Value: last}}
	if notice != nil {
		d.Blocks = append(d.Blocks, *notice)
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{
		{Text: "↻ sync now", Link: link(d, "w5f:feeds/sync", "sync")}, {Text: "   "},
		{Text: fmt.Sprintf("all unread (%d)", totalUnread), Link: link(d, "w5f:feeds/unread", "unread")}, {Text: "   "},
		{Text: fmt.Sprintf("★ starred (%d)", env.DB.StarredCount()), Link: link(d, "w5f:feeds/starred", "starred")}, {Text: "   "},
		{Text: "feed status", Link: link(d, "w5f:feeds/status", "status")},
	}})
	var items [][]doc.Block
	for _, s := range env.Catalog.Shelves {
		ids := env.Catalog.FeedsOn(s.ID)
		if len(ids) == 0 {
			continue
		}
		unread, total := 0, 0
		for _, id := range ids {
			unread += counts[id][0]
			total += counts[id][1]
		}
		in := doc.Inline{{Text: s.Label, Link: link(d, "w5f:feeds/shelf/"+s.ID, s.Label)}}
		if unread > 0 {
			in = append(in, doc.Span{Text: fmt.Sprintf("  %d new", unread), Style: doc.Bold})
		}
		noun := "feeds"
		if len(ids) == 1 {
			noun = "feed"
		}
		in = append(in, doc.Span{Text: fmt.Sprintf("  · %d %s · %d items", len(ids), noun, total), Style: doc.Italic})
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Shelves"}}}, doc.List{Items: items})
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{
		{Text: "OPML: bring feeds from another reader with g → opml-import <file>   ", Style: doc.Italic},
		{Text: "export these shelves", Link: link(d, "w5f:feeds/export", "export")},
	}})
	return d
}

// DefaultExport is where the shelves are exported without a file name.
func DefaultExport() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "w5f-periodicals.opml")
	}
	return "w5f-periodicals.opml"
}

func importDoc(env Env, file string) (*doc.Document, error) {
	if file == "" {
		return nil, errors.New("which file? g → opml-import <file.opml>")
	}
	rep, err := Import(env.Catalog, file)
	if err != nil {
		return nil, err
	}
	var text string
	switch {
	case len(rep.Added) == 0:
		text = fmt.Sprintf("Nothing new: all %d feeds of %s are already on your shelves.", len(rep.Duplicate), filepath.Base(file))
	default:
		text = fmt.Sprintf("Added %d feeds to %s", len(rep.Added), rep.File)
		if len(rep.Shelves) > 0 {
			var labels []string
			for _, s := range rep.Shelves {
				labels = append(labels, s.Label)
			}
			text += " on new shelves: " + strings.Join(labels, ", ")
		}
		text += "."
		if len(rep.Duplicate) > 0 {
			text += fmt.Sprintf(" %d were already here.", len(rep.Duplicate))
		}
		text += " Sync to fetch them."
	}
	if c, err := LoadCatalog(); err == nil {
		env.Catalog = c
	}
	return shelvesDoc(env, &doc.Notice{Kind: "info", Text: text}), nil
}

func exportDoc(env Env, file string) (*doc.Document, error) {
	if file == "" {
		file = DefaultExport()
	}
	var b bytes.Buffer
	n, err := Export(env.Catalog, env.DB, &b)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(file, b.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return shelvesDoc(env, &doc.Notice{Kind: "info", Text: fmt.Sprintf("Exported %d feeds to %s.", n, file)}), nil
}

func shelfDoc(env Env, shelf string, page int, note string) (*doc.Document, error) {
	s := env.Catalog.Shelf(shelf)
	if s == nil {
		return nil, errors.New("unknown shelf " + shelf)
	}
	d := listDoc(env, s.Label, "w5f:feeds/shelf/"+shelf, store.Query{Feeds: env.Catalog.FeedsOn(shelf)}, page, note)
	// Shelf tools, then the feeds on this shelf.
	tools := doc.Paragraph{Text: doc.Inline{
		{Text: "mark all read", Link: link(d, "w5f:feeds/read/"+shelf, "mark all read")}, {Text: "   "},
		{Text: "↻ sync", Link: link(d, "w5f:feeds/sync", "sync")},
	}}
	d.Blocks = append([]doc.Block{tools}, d.Blocks...)
	var feeds doc.Inline
	for _, id := range env.Catalog.FeedsOn(shelf) {
		if len(feeds) > 0 {
			feeds = append(feeds, doc.Span{Text: " · "})
		}
		fd := env.Catalog.Feed(id)
		feeds = append(feeds, doc.Span{Text: fd.Name, Link: link(d, "w5f:feeds/feed/"+id, fd.Name)})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: append(doc.Inline{{Text: "Feeds: ", Style: doc.Italic}}, feeds...)})
	return d, nil
}

// listDoc renders a page of items.
func listDoc(env Env, title, base string, q store.Query, page int, note string) *doc.Document {
	d := &doc.Document{Title: title, URL: base, Origin: "local", Lang: "en"}
	q.Limit, q.Offset = pageSize+1, (page-1)*pageSize
	items, err := env.DB.Items(q)
	if err != nil {
		d.Blocks = []doc.Block{doc.Notice{Kind: "warn", Text: err.Error()}}
		return d
	}
	more := len(items) > pageSize
	if more {
		items = items[:pageSize]
	}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	if len(items) == 0 {
		msg := "Nothing here yet. Sync to fetch new items."
		if q.Starred {
			msg = "No starred items. Press * while reading an item to star it."
		} else if q.Unread {
			msg = "All caught up — no unread items."
		}
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: msg, Style: doc.Italic}}})
		return d
	}
	if page > 1 {
		d.Meta = append(d.Meta, doc.KV{Key: "page", Value: strconv.Itoa(page)})
	}
	var list [][]doc.Block
	for _, it := range items {
		mark := "  "
		if !it.Read {
			mark = "● "
		}
		if it.Starred {
			mark = "★ "
		}
		title := it.Title
		if title == "" {
			title = "(untitled)"
		}
		st := doc.Style(0)
		if !it.Read {
			st = doc.Bold
		}
		head := doc.Inline{{Text: mark, Style: doc.Bold}, {Text: title, Style: st, Link: link(d, fmt.Sprintf("w5f:item/%d", it.ID), title)}}
		sub := []string{feedName(env, it.FeedID)}
		if !it.Published.IsZero() {
			sub = append(sub, it.Published.Format("2 Jan 2006"))
		}
		if it.Author != "" && it.Author != feedName(env, it.FeedID) {
			sub = append(sub, it.Author)
		}
		list = append(list, []doc.Block{
			doc.Paragraph{Text: head},
			doc.Paragraph{Text: doc.Inline{{Text: "  " + strings.Join(sub, " · "), Style: doc.Italic}}},
		})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: list})
	var nav doc.Inline
	if page > 1 {
		nav = append(nav, doc.Span{Text: "‹ newer", Link: link(d, fmt.Sprintf("%s?page=%d", base, page-1), "newer")}, doc.Span{Text: "   "})
	}
	if more {
		nav = append(nav, doc.Span{Text: "older ›", Link: link(d, fmt.Sprintf("%s?page=%d", base, page+1), "older")})
	}
	if len(nav) > 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: nav})
	}
	return d
}

// itemDoc shows one item, fetching the full article for excerpt-only feeds.
func itemDoc(ctx context.Context, id int64, env Env) (*doc.Document, error) {
	it, err := env.DB.Item(id)
	if err != nil {
		return nil, err
	}
	_ = env.DB.SetRead(id, true)
	d := &doc.Document{Title: it.Title, URL: it.URL, Origin: "local", Ref: fmt.Sprintf("item:%d", id)}
	d.Catalog = catalog.Feed(it.FeedID, it.Published)
	fd := env.Catalog.Feed(it.FeedID)
	if fd != nil {
		d.Lang = fd.Lang
	}
	if d.Lang == "" {
		d.Lang = "en"
	}
	meta := []string{feedName(env, it.FeedID)}
	if !it.Published.IsZero() {
		meta = append(meta, it.Published.Format("2 Jan 2006 15:04"))
	}
	if it.Author != "" && it.Author != feedName(env, it.FeedID) {
		meta = append(meta, it.Author)
	}
	if it.Starred {
		meta = append(meta, "★ starred")
	}
	d.Meta = append(d.Meta, doc.KV{Key: "·", Value: strings.Join(meta, " · ")})

	body := it.Content
	if strings.TrimSpace(htmlconv.FragmentText(body)) == "" {
		body = it.Summary
	}
	feedText := htmlconv.FragmentText(body)
	source := "from the feed"
	if len(strings.Join(strings.Fields(feedText), "")) < fullTextBelow && it.URL != "" && env.Article != nil {
		if art, err := env.Article(ctx, it.URL); err == nil && doc.TextLength(art.Blocks) > len(strings.Join(strings.Fields(feedText), "")) {
			offset := len(d.Links)
			d.Links = append(d.Links, art.Links...)
			d.Blocks = append(d.Blocks, doc.ShiftLinks(art.Blocks, offset)...)
			source = "full text from the site"
			if art.Lang != "" {
				d.Lang = art.Lang
			}
		}
	}
	if source == "from the feed" {
		d.Blocks = append(d.Blocks, htmlconv.Fragment(d, body, it.URL)...)
		if len(d.Blocks) == 0 {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "This item has no text in the feed.", Style: doc.Italic}}})
		}
	}
	d.Meta = append(d.Meta, doc.KV{Key: "", Value: source})
	foot := doc.Inline{}
	if it.URL != "" {
		foot = append(foot, doc.Span{Text: "→ original page", Link: link(d, it.URL, "original")}, doc.Span{Text: "   "})
	}
	foot = append(foot, doc.Span{Text: "* star · m mark unread · ← back to the list", Style: doc.Italic})
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: foot})
	d.Renumber()
	return d, nil
}

func statusDoc(env Env) *doc.Document {
	d := &doc.Document{Title: "Feed status", URL: "w5f:feeds/status", Origin: "local", Lang: "en"}
	type row struct {
		f  Feed
		st store.FeedState
	}
	var bad, good []row
	for _, f := range env.Catalog.Feeds {
		st := env.DB.Feed(f.ID)
		if st.Error != "" || st.LastOK.IsZero() {
			bad = append(bad, row{f, st})
		} else {
			good = append(good, row{f, st})
		}
	}
	sort.Slice(bad, func(i, j int) bool { return bad[i].f.Name < bad[j].f.Name })
	d.Meta = []doc.KV{{Key: "ok", Value: strconv.Itoa(len(good))}, {Key: "problems", Value: strconv.Itoa(len(bad))}}
	if len(bad) > 0 {
		var items [][]doc.Block
		for _, r := range bad {
			msg := r.st.Error
			if msg == "" {
				msg = "not synced yet"
			}
			if len(msg) > 160 {
				msg = msg[:157] + "…"
			}
			items = append(items, []doc.Block{
				doc.Paragraph{Text: doc.Inline{{Text: r.f.Name, Style: doc.Bold, Link: link(d, "w5f:feeds/feed/"+r.f.ID, r.f.Name)}}},
				doc.Paragraph{Text: doc.Inline{{Text: msg, Style: doc.Italic}}},
			})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Needs attention"}}}, doc.List{Items: items})
	}
	var items [][]doc.Block
	for _, r := range good {
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{
			{Text: r.f.Name, Link: link(d, "w5f:feeds/feed/"+r.f.ID, r.f.Name)},
			{Text: "  · ok " + ago(r.st.LastOK), Style: doc.Italic}}}})
	}
	if len(items) > 0 {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Working"}}}, doc.List{Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{
		{Text: "Add or disable feeds in ", Style: doc.Italic}, {Text: UserCatalogPath(), Style: doc.Code}}})
	return d
}

func feedName(env Env, id string) string {
	if f := env.Catalog.Feed(id); f != nil {
		return f.Name
	}
	return id
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}
