package comics

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"w5f/internal/comics/suwayomi"
	"w5f/internal/doc"
	"w5f/internal/smallweb"
	"w5f/internal/store"
)

// Env is what the Comics pages need.
type Env struct {
	DB     *store.DB
	Root   string
	Server suwayomi.Server
	// View opens the viewer: a local comic file, or a Suwayomi chapter.
	View func(v ViewRequest) error
}

// ViewRequest is one thing to read in the viewer.
type ViewRequest struct {
	Path      string // a local file or folder (with ComicID for progress)
	ComicID   int64
	ChapterID int    // a Suwayomi chapter, read through the server
	Title     string // for messages
}

func (e Env) client() *suwayomi.Client { return suwayomi.New(e.Server.Addr()) }

// AutoStart starts Suwayomi in the background when the Comics page opens;
// the interactive reader turns it on (one-shot commands would exit and leave
// a half-started server behind).
var AutoStart = false

// The server W5F started (stopped again by Shutdown).
var (
	serverMu      sync.Mutex
	serverStarted bool
	serverStartOp bool
	serverErr     error
)

// startInBackground starts Suwayomi once without waiting for it.
func (e Env) startInBackground() {
	serverMu.Lock()
	defer serverMu.Unlock()
	if serverStartOp {
		return
	}
	serverStartOp, serverErr = true, nil
	go func() {
		started, err := e.Server.Start(context.Background(), 90*time.Second)
		serverMu.Lock()
		serverStarted = serverStarted || started
		serverStartOp, serverErr = false, err
		serverMu.Unlock()
	}()
}

// ensureServer waits for the server, starting it when needed.
func (e Env) ensureServer(ctx context.Context) error {
	if e.Server.Running(ctx) {
		return nil
	}
	if e.Server.Jar() == "" {
		return errors.New("Suwayomi is not installed: run w5f comics server install")
	}
	started, err := e.Server.Start(ctx, 90*time.Second)
	serverMu.Lock()
	serverStarted = serverStarted || started
	serverMu.Unlock()
	return err
}

// Shutdown stops the Suwayomi server if W5F started it, also one still
// starting (its pid is written as soon as it is launched).
func Shutdown(s suwayomi.Server) {
	serverMu.Lock()
	started, starting := serverStarted, serverStartOp
	serverStarted = false
	serverMu.Unlock()
	if starting {
		// Give a start in progress the moment it needs to launch the
		// server and write its pid.
		time.Sleep(2 * time.Second)
	}
	if started || starting {
		_ = s.Stop()
	}
}

// IsTarget reports the Comics pages.
func IsTarget(t string) bool { return t == "w5f:comics" || strings.HasPrefix(t, "w5f:comics/") }

// page builds a document with links added in reading order.
type page struct{ d *doc.Document }

func newPage(title, target string) *page {
	return &page{&doc.Document{Title: title, URL: target, Origin: "local", Lang: "en"}}
}

func (p *page) link(href, text string) int {
	p.d.Links = append(p.d.Links, doc.Link{Href: href, Text: text})
	return len(p.d.Links)
}
func (p *page) heading(t string) {
	p.d.Blocks = append(p.d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: t}}})
}
func (p *page) para(in doc.Inline) { p.d.Blocks = append(p.d.Blocks, doc.Paragraph{Text: in}) }
func (p *page) note(kind, t string) {
	p.d.Blocks = append(p.d.Blocks, doc.Notice{Kind: kind, Text: t})
}
func (p *page) list(items []doc.Inline) {
	var l [][]doc.Block
	for _, it := range items {
		l = append(l, []doc.Block{doc.Paragraph{Text: it}})
	}
	if len(l) > 0 {
		p.d.Blocks = append(p.d.Blocks, doc.List{Items: l})
	}
}
func (p *page) a(href, text string) doc.Span { return doc.Span{Text: text, Link: p.link(href, text)} }

func dim(t string) doc.Span { return doc.Span{Text: t, Style: doc.Italic} }

// Route builds a Comics page (and performs its action, for action links).
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	path, query := target, url.Values{}
	if i := strings.Index(target, "?"); i >= 0 {
		path, query = target[:i], parseQuery(target[i+1:])
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, "w5f:comics"), "/"), "/")
	switch parts[0] {
	case "":
		return home(ctx, env)
	case "library":
		return libraryDoc(env)
	case "series":
		return seriesDoc(env, query.Get("name"))
	case "open", "mark":
		id, err := strconv.ParseInt(at(parts, 1), 10, 64)
		if err != nil {
			return nil, errors.New("bad comic address")
		}
		c, err := env.DB.Comic(id)
		if err != nil {
			return nil, err
		}
		if parts[0] == "mark" {
			_ = env.DB.SetComicFinished(id, !c.Finished)
			return seriesDoc(env, c.Series)
		}
		return viewed(env, ViewRequest{Path: c.Path, ComicID: c.ID, Title: c.Title}, "w5f:comics/series?"+url.Values{"name": {c.Series}}.Encode())
	case "server":
		return serverDoc(ctx, env, at(parts, 1))
	}
	// Everything below talks to Suwayomi.
	if err := env.ensureServer(ctx); err != nil {
		return nil, err
	}
	c := env.client()
	switch parts[0] {
	case "following":
		return followingDoc(ctx, c)
	case "update":
		if err := c.UpdateLibrary(ctx); err != nil {
			return nil, err
		}
		d, err := followingDoc(ctx, c)
		if err == nil {
			d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: "Checking every followed series for new chapters. Reload (ctrl+r) in a minute to see them."}}, d.Blocks...)
		}
		return d, err
	case "manga":
		id, err := strconv.Atoi(at(parts, 1))
		if err != nil {
			return nil, errors.New("bad series address")
		}
		return mangaRoute(ctx, c, id, at(parts, 2))
	case "chapter":
		id, err := strconv.Atoi(at(parts, 1))
		if err != nil {
			return nil, errors.New("bad chapter address")
		}
		return chapterRoute(ctx, env, c, id, at(parts, 2), query.Get("manga"))
	case "sources":
		return sourcesDoc(ctx, c)
	case "source":
		pg, _ := strconv.Atoi(query.Get("page"))
		return browseDoc(ctx, c, at(parts, 1), strings.ToUpper(query.Get("type")), "", max(pg, 1))
	case "search":
		pg, _ := strconv.Atoi(query.Get("page"))
		return browseDoc(ctx, c, at(parts, 1), "SEARCH", query.Get("q"), max(pg, 1))
	case "downloads":
		return downloadsDoc(ctx, c)
	case "extensions":
		return extensionsDoc(ctx, c, at(parts, 1) == "refresh", "")
	case "extension":
		action := at(parts, 2)
		msg := fmt.Sprintf("%s: %s done.", at(parts, 1), action)
		if err := c.SetExtension(ctx, at(parts, 1), action); err != nil {
			msg = err.Error()
		}
		return extensionsDoc(ctx, c, false, msg)
	case "repo":
		u := query.Get("url")
		var err error
		switch at(parts, 1) {
		case "add":
			err = c.AddStore(ctx, u)
		case "remove":
			err = c.RemoveStore(ctx, u)
		}
		msg := "Repository list changed."
		if err != nil {
			msg = err.Error()
		}
		return extensionsDoc(ctx, c, err == nil, msg)
	}
	return nil, errors.New("unknown comics address: " + target)
}

func at(p []string, i int) string {
	if i < len(p) {
		return p[i]
	}
	return ""
}

func parseQuery(s string) url.Values {
	q, _ := url.ParseQuery(s)
	return q
}

func viewed(env Env, v ViewRequest, back string) (*doc.Document, error) {
	p := newPage(v.Title, "")
	if env.View == nil {
		return nil, errors.New("no viewer")
	}
	if err := env.View(v); err != nil {
		p.note("warn", err.Error())
	} else {
		p.note("info", "Opened in the viewer. Close it (q) to come back here; your page is kept.")
	}
	p.para(doc.Inline{p.a(back, "← back")})
	return p.d, nil
}

// home: continue reading, the local library, and Suwayomi's side.
func home(ctx context.Context, env Env) (*doc.Document, error) {
	p := newPage("Comics", "w5f:comics")
	n, scanErr := Scan(env.DB, env.Root)
	p.d.Meta = []doc.KV{{Key: "local", Value: strconv.Itoa(n)}}
	if scanErr != nil {
		p.note("warn", "Comics folder: "+scanErr.Error())
	}
	if recent, _ := env.DB.RecentComics(5); len(recent) > 0 {
		p.heading("Continue")
		var items []doc.Inline
		for _, c := range recent {
			items = append(items, doc.Inline{p.a(fmt.Sprintf("w5f:comics/open/%d", c.ID), c.Series+" · "+c.Title), dim("  " + progress(c))})
		}
		p.list(items)
	}

	p.heading("Following (Suwayomi)")
	switch {
	case env.Server.Running(ctx):
		p.list([]doc.Inline{
			{p.a("w5f:comics/following", "Followed series"), dim("  — new chapters, downloads")},
			{p.a("w5f:comics/sources", "Sources"), dim("  — browse and search what your extensions offer")},
			{p.a("w5f:comics/downloads", "Downloads")},
			{p.a("w5f:comics/extensions", "Extensions and repositories")},
			{p.a("w5f:comics/server/stop", "Stop Suwayomi"), dim("  (it stops by itself when W5F closes)")},
		})
	case env.Server.Jar() == "":
		p.para(doc.Inline{dim("Suwayomi is not installed. Install it with "), {Text: "w5f comics server install", Style: doc.Code}, dim(" (Java 21 needed).")})
	case !AutoStart:
		p.para(doc.Inline{dim("Suwayomi is not running. "), p.a("w5f:comics/server/start", "▶ start it"), dim(" (about 15 seconds)")})
	default:
		env.startInBackground()
		serverMu.Lock()
		lastErr := serverErr
		serverMu.Unlock()
		if lastErr != nil {
			p.note("warn", "Suwayomi did not start: "+lastErr.Error())
		}
		p.para(doc.Inline{dim("Suwayomi is starting (about 15 seconds). "), p.a("w5f:comics/following", "Open the followed series"), dim(" — it waits until the server answers.")})
	}

	p.heading("Library")
	series, _ := env.DB.ComicSeriesList()
	if len(series) == 0 {
		p.para(doc.Inline{dim("Put CBZ files or image folders in " + env.Root + " (one folder per series).")})
	} else {
		var items []doc.Inline
		for i, s := range series {
			if i == 12 {
				items = append(items, doc.Inline{p.a("w5f:comics/library", fmt.Sprintf("… all %d series", len(series)))})
				break
			}
			items = append(items, seriesItem(p, s))
		}
		p.list(items)
	}
	return p.d, nil
}

func seriesItem(p *page, s store.ComicSeries) doc.Inline {
	state := fmt.Sprintf("  %d issues", s.Issues)
	if s.Unread > 0 && s.Unread < s.Issues {
		state += fmt.Sprintf(" · %d unread", s.Unread)
	} else if s.Unread == 0 {
		state += " · read"
	}
	return doc.Inline{p.a("w5f:comics/series?"+url.Values{"name": {s.Name}}.Encode(), s.Name), dim(state)}
}

func progress(c store.Comic) string {
	switch {
	case c.Finished:
		return "read"
	case c.Opened.IsZero():
		return "new"
	case c.Pages > 0:
		return fmt.Sprintf("page %d/%d", c.Page+1, c.Pages)
	}
	return "opened"
}

func libraryDoc(env Env) (*doc.Document, error) {
	p := newPage("Comics library", "w5f:comics/library")
	series, err := env.DB.ComicSeriesList()
	if err != nil {
		return nil, err
	}
	var items []doc.Inline
	for _, s := range series {
		items = append(items, seriesItem(p, s))
	}
	p.list(items)
	return p.d, nil
}

func seriesDoc(env Env, name string) (*doc.Document, error) {
	p := newPage(name, "w5f:comics/series?"+url.Values{"name": {name}}.Encode())
	issues, err := env.DB.ComicsInSeries(name)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		p.note("info", "Nothing in this series (the files may have moved).")
	}
	var items []doc.Inline
	for _, c := range issues {
		label := c.Title
		if c.Number > 0 && !strings.Contains(c.Title, strconv.FormatFloat(c.Number, 'f', -1, 64)) {
			label = "#" + strconv.FormatFloat(c.Number, 'f', -1, 64) + " " + label
		}
		mark := "mark read"
		if c.Finished {
			mark = "mark unread"
		}
		items = append(items, doc.Inline{p.a(fmt.Sprintf("w5f:comics/open/%d", c.ID), label), dim("  " + progress(c) + "  "),
			p.a(fmt.Sprintf("w5f:comics/mark/%d", c.ID), mark)})
	}
	p.list(items)
	p.para(doc.Inline{p.a("w5f:comics", "← Comics")})
	return p.d, nil
}

func serverDoc(ctx context.Context, env Env, action string) (*doc.Document, error) {
	switch action {
	case "start":
		if err := env.ensureServer(ctx); err != nil {
			return nil, err
		}
	case "stop":
		Shutdown(env.Server)
		_ = env.Server.Stop()
	}
	return home(ctx, env)
}

func followingDoc(ctx context.Context, c *suwayomi.Client) (*doc.Document, error) {
	p := newPage("Following", "w5f:comics/following")
	lib, err := c.Library(ctx)
	if err != nil {
		return nil, err
	}
	p.para(doc.Inline{p.a("w5f:comics/update", "↻ check all for new chapters"), dim("   "), p.a("w5f:comics/sources", "find more in Sources")})
	if len(lib) == 0 {
		p.note("info", "You follow nothing yet: open a series from Sources and choose follow.")
	}
	var items []doc.Inline
	for _, m := range lib {
		state := "  " + m.SourceName()
		if m.UnreadCount > 0 {
			state += fmt.Sprintf(" · %d unread", m.UnreadCount)
		}
		if m.DownloadCount > 0 {
			state += fmt.Sprintf(" · %d downloaded", m.DownloadCount)
		}
		items = append(items, doc.Inline{p.a(fmt.Sprintf("w5f:comics/manga/%d", m.ID), m.Title), dim(state)})
	}
	p.list(items)
	return p.d, nil
}

func mangaRoute(ctx context.Context, c *suwayomi.Client, id int, action string) (*doc.Document, error) {
	var notice string
	refresh := false
	switch action {
	case "follow", "unfollow":
		if err := c.Follow(ctx, []int{id}, action == "follow"); err != nil {
			return nil, err
		}
		refresh = action == "follow"
	case "refresh":
		refresh = true
	case "download-unread":
		_, chs, err := c.Manga(ctx, id, false)
		if err != nil {
			return nil, err
		}
		var ids []int
		for _, ch := range chs {
			if !ch.IsRead && !ch.IsDownloaded {
				ids = append(ids, ch.ID)
			}
		}
		if len(ids) == 0 {
			notice = "Every unread chapter is downloaded already."
		} else if err := c.Download(ctx, ids); err != nil {
			return nil, err
		} else {
			notice = fmt.Sprintf("%d chapters queued for download (as CBZ, into your Comics folder).", len(ids))
		}
	}
	m, chs, err := c.Manga(ctx, id, refresh)
	if err != nil {
		return nil, err
	}
	if len(chs) == 0 && !refresh {
		m, chs, err = c.Manga(ctx, id, true) // never fetched yet
		if err != nil {
			return nil, err
		}
	}
	p := newPage(m.Title, fmt.Sprintf("w5f:comics/manga/%d", id))
	p.d.Meta = []doc.KV{{Key: "source", Value: m.SourceName()}, {Key: "status", Value: strings.ToLower(strings.ReplaceAll(m.Status, "_", " "))}}
	if m.Author != "" {
		p.d.Meta = append(p.d.Meta, doc.KV{Key: "by", Value: m.Author})
	}
	if notice != "" {
		p.note("info", notice)
	}
	follow := doc.Span{}
	if m.InLibrary {
		follow = p.a(fmt.Sprintf("w5f:comics/manga/%d/unfollow", id), "unfollow")
	} else {
		follow = p.a(fmt.Sprintf("w5f:comics/manga/%d/follow", id), "★ follow")
	}
	actions := doc.Inline{follow, dim("   "), p.a(fmt.Sprintf("w5f:comics/manga/%d/refresh", id), "↻ refresh")}
	if m.SourceName() != "Local source" { // its chapters are files already
		actions = append(actions, dim("   "), p.a(fmt.Sprintf("w5f:comics/manga/%d/download-unread", id), "download unread"))
	}
	p.para(actions)
	if d := strings.TrimSpace(m.Description); d != "" {
		p.para(doc.Inline{{Text: d}})
	}
	p.heading(fmt.Sprintf("Chapters (%d)", len(chs)))
	var items []doc.Inline
	for _, ch := range chs {
		state := ""
		switch {
		case ch.IsRead:
			state = "read"
		case ch.LastPageRead > 0:
			state = fmt.Sprintf("page %d", ch.LastPageRead+1)
		}
		if ch.IsDownloaded {
			state = strings.TrimPrefix(state+" · downloaded", " · ")
		}
		if t := ch.Uploaded(); !t.IsZero() {
			state = strings.TrimPrefix(state+" · "+t.Format("2006-01-02"), " · ")
		}
		in := doc.Inline{p.a(fmt.Sprintf("w5f:comics/chapter/%d/read?manga=%d", ch.ID, id), ch.Name)}
		if state != "" {
			in = append(in, dim("  "+state))
		}
		if !ch.IsDownloaded && m.SourceName() != "Local source" {
			in = append(in, dim("  "), p.a(fmt.Sprintf("w5f:comics/chapter/%d/download?manga=%d", ch.ID, id), "download"))
		}
		items = append(items, in)
	}
	p.list(items)
	return p.d, nil
}

func chapterRoute(ctx context.Context, env Env, c *suwayomi.Client, id int, action, manga string) (*doc.Document, error) {
	back := "w5f:comics/following"
	if manga != "" {
		back = "w5f:comics/manga/" + manga
	}
	mid, _ := strconv.Atoi(manga)
	switch action {
	case "download":
		if err := c.Download(ctx, []int{id}); err != nil {
			return nil, err
		}
		if mid > 0 {
			d, err := mangaRoute(ctx, c, mid, "")
			if err == nil {
				d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: "Chapter queued for download."}}, d.Blocks...)
			}
			return d, err
		}
		return downloadsDoc(ctx, c)
	case "read":
		return viewed(env, ViewRequest{ChapterID: id, Title: "Chapter"}, back)
	}
	return nil, errors.New("unknown chapter action")
}

func sourcesDoc(ctx context.Context, c *suwayomi.Client) (*doc.Document, error) {
	p := newPage("Sources", "w5f:comics/sources")
	srcs, err := c.Sources(ctx)
	if err != nil {
		return nil, err
	}
	p.para(doc.Inline{dim("Sources come from the extensions you install ("), p.a("w5f:comics/extensions", "Extensions"),
		dim("). The Local source reads your own files in " + "Comics/Local" + ".")})
	var items []doc.Inline
	for _, s := range srcs {
		in := doc.Inline{{Text: s.DisplayName, Style: doc.Bold}, dim("  "),
			p.a(fmt.Sprintf("w5f:comics/source/%s?type=POPULAR", s.ID), "popular")}
		if s.SupportsLatest {
			in = append(in, dim(" · "), p.a(fmt.Sprintf("w5f:comics/source/%s?type=LATEST", s.ID), "latest"))
		}
		in = append(in, dim(" · "), p.a(SearchPage(s.ID, s.DisplayName), "search"))
		if s.ContentWarning == "NSFW" {
			in = append(in, dim("  (adult)"))
		}
		items = append(items, in)
	}
	p.list(items)
	return p.d, nil
}

// SearchPage is the input page that searches a source: enter, type, and the
// answer goes to w5f:comics/search/<source>?q=<text>.
func SearchPage(sourceID, name string) string {
	return smallweb.WebSearchPage("w5f:comics/search/"+sourceID, "q", "Search "+name+":")
}

func browseDoc(ctx context.Context, c *suwayomi.Client, source, kind, query string, pg int) (*doc.Document, error) {
	if kind == "" {
		kind = "POPULAR"
	}
	if kind == "SEARCH" && strings.TrimSpace(query) == "" {
		return nil, errors.New("nothing to search for")
	}
	mangas, next, err := c.Browse(ctx, source, kind, query, pg)
	if err != nil {
		return nil, err
	}
	title := map[string]string{"POPULAR": "Popular", "LATEST": "Latest"}[kind]
	if kind == "SEARCH" {
		title = "Search: " + query
	}
	self := func(n int) string {
		if kind == "SEARCH" {
			return fmt.Sprintf("w5f:comics/search/%s?%s", source, url.Values{"q": {query}, "page": {strconv.Itoa(n)}}.Encode())
		}
		return fmt.Sprintf("w5f:comics/source/%s?type=%s&page=%d", source, kind, n)
	}
	p := newPage(title, self(pg))
	if len(mangas) == 0 {
		p.note("info", "Nothing found.")
	}
	var items []doc.Inline
	for _, m := range mangas {
		in := doc.Inline{p.a(fmt.Sprintf("w5f:comics/manga/%d", m.ID), m.Title)}
		if m.InLibrary {
			in = append(in, dim("  ★ following"))
		}
		items = append(items, in)
	}
	p.list(items)
	if next {
		p.d.Next = self(pg + 1)
		p.para(doc.Inline{p.a(self(pg+1), "next page →")})
	}
	return p.d, nil
}

func downloadsDoc(ctx context.Context, c *suwayomi.Client) (*doc.Document, error) {
	p := newPage("Downloads", "w5f:comics/downloads")
	running, queue, err := c.Downloads(ctx)
	if err != nil {
		return nil, err
	}
	state := "stopped"
	if running {
		state = "running"
	}
	p.d.Meta = []doc.KV{{Key: "downloader", Value: state}}
	if len(queue) == 0 {
		p.note("info", "Nothing is waiting. Downloaded chapters are in your Comics folder and in the Library.")
	}
	var items []doc.Inline
	for _, d := range queue {
		items = append(items, doc.Inline{p.a(fmt.Sprintf("w5f:comics/manga/%d", d.Manga.ID), d.Manga.Title), {Text: " · " + d.Chapter.Name},
			dim(fmt.Sprintf("  %s %d%%", strings.ToLower(d.State), int(d.Progress*100)))})
	}
	p.list(items)
	p.para(doc.Inline{dim("Reload (ctrl+r) to follow the progress.")})
	return p.d, nil
}

// RepoPage is the input page that adds an extension repository by its index
// address.
func RepoPage() string {
	return smallweb.WebSearchPage("w5f:comics/repo/add", "url",
		"Address of the repository's index (it ends in index.min.json). Only add repositories you trust.")
}

func extensionsDoc(ctx context.Context, c *suwayomi.Client, refresh bool, msg string) (*doc.Document, error) {
	p := newPage("Extensions", "w5f:comics/extensions")
	exts, stores, err := c.Extensions(ctx, refresh || msg != "")
	if err != nil {
		return nil, err
	}
	if msg != "" {
		p.note("info", msg)
	}
	p.para(doc.Inline{dim("W5F comes with no repositories. The ones you add, and what you install from them, are your choice — "),
		dim("mind the rights of what a source offers.")})
	p.heading("Repositories")
	var repos []doc.Inline
	for _, s := range stores {
		repos = append(repos, doc.Inline{{Text: s.Name + "  "}, dim(s.IndexURL + "  "),
			p.a("w5f:comics/repo/remove?"+url.Values{"url": {s.IndexURL}}.Encode(), "remove")})
	}
	p.list(repos)
	p.para(doc.Inline{p.a(RepoPage(), "+ add a repository"), dim("   "), p.a("w5f:comics/extensions/refresh", "↻ refresh lists")})
	p.heading("Extensions")
	if len(exts) == 0 {
		p.note("info", "No extensions listed: add a repository first.")
	}
	var items []doc.Inline
	for _, e := range exts {
		in := doc.Inline{{Text: e.Name, Style: doc.Bold}, dim(fmt.Sprintf("  %s %s  ", e.Lang, e.VersionName))}
		switch {
		case e.IsInstalled && e.HasUpdate:
			in = append(in, p.a("w5f:comics/extension/"+e.PkgName+"/update", "update"), dim(" · "))
			fallthrough
		case e.IsInstalled:
			in = append(in, p.a("w5f:comics/extension/"+e.PkgName+"/uninstall", "uninstall"))
		default:
			in = append(in, p.a("w5f:comics/extension/"+e.PkgName+"/install", "install"))
		}
		if e.ContentWarning == "NSFW" {
			in = append(in, dim("  (adult)"))
		}
		if e.IsObsolete {
			in = append(in, dim("  (obsolete)"))
		}
		items = append(items, in)
	}
	p.list(items)
	return p.d, nil
}
