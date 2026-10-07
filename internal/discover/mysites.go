package discover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/smallweb"
	"w5f/internal/store"
)

// The owner's own sites in Deep random (asked for 2026-10-04): any site can
// be added, with g → random-add <address> or by hand in random.toml. A site
// joins a shelf of its own ("yours" unless named) or one of the built-in
// families, where it is drawn as often as each of that family's sources.

// Site is a source the owner added to Deep random.
type Site struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
	// How is the way a page is drawn from it: "walk" (down random links
	// from the address to a page of real text), "feed" (an item of its RSS
	// or Atom feed), "random" (its own random-page address, followed each
	// time) or "links" (one of the links on the page, as it is).
	How   string `toml:"how,omitempty"`
	Scope string `toml:"scope,omitempty"` // walk: stay under this path (unset: the address's folder)
	Depth int    `toml:"depth,omitempty"` // walk: levels down at most (unset: 4)
	// Family is the shelf it is drawn on: "yours" when unset, a built-in
	// family's name to join it, any other name for a shelf of that name.
	Family string `toml:"family,omitempty"`
	Added  string `toml:"added,omitempty"`
}

// YoursFamily is the shelf the owner's sites are drawn on unless they name
// another.
const YoursFamily = "yours"

var hows = []struct{ key, what string }{
	{"walk", "down random links from the address to a page of real text"},
	{"feed", "a random item of its feed"},
	{"random", "its own random-page address, followed each time"},
	{"links", "one of the links on the page, as it is"},
}

func (s Site) how() string {
	if s.How == "" {
		return "walk"
	}
	return s.How
}

func (s Site) family() string {
	if f := strings.TrimSpace(s.Family); f != "" {
		return strings.ToLower(f)
	}
	return YoursFamily
}

// SitesPath is random.toml in the data folder.
func SitesPath() string { return filepath.Join(store.DataDir(), "random.toml") }

// LoadSites reads the owner's sites; no file is no sites.
func LoadSites(p string) ([]Site, error) {
	var f struct {
		Site []Site `toml:"site"`
	}
	if _, err := toml.DecodeFile(p, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	var out []Site
	for _, s := range f.Site {
		if strings.TrimSpace(s.URL) != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

const sitesHeader = `# Your sites in Deep random (x). W5F writes this file when you add a site
# (g → random-add <address> [shelf]); it can be edited by hand too.
#
#   name    what the "why you are here" line calls it
#   url     where drawing starts
#   how     walk:   down random links from the address to a page of real text
#           feed:   a random item of its RSS or Atom feed
#           random: its own random-page address, followed each time
#           links:  one of the links on the page, as it is
#   scope   walk: stay under this path (unset: the address's folder)
#   depth   walk: levels down at most (unset: 4)
#   family  the shelf: unset is "yours"; a built-in family's name (esoteric,
#           folklore, textfiles, encyclopedic, knowledge, smallweb, weird,
#           fiction) joins it; any other name makes a shelf of its own

`

// SaveSites writes the owner's sites.
func SaveSites(p string, sites []Site) error {
	var b bytes.Buffer
	b.WriteString(sitesHeader)
	if err := toml.NewEncoder(&b).Encode(struct {
		Site []Site `toml:"site"`
	}{sites}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// sized reports how many sources a built-in family draws from, so a site
// joining it comes up as often as each of them.
type sized interface{ sources() int }

func (esoteric) sources() int       { return len(esotericSites) }
func (folklore) sources() int       { return len(folkloreSites) }
func (encyclopedic) sources() int   { return len(encyclopedias) }
func (knowledge) sources() int      { return len(knowledgeSources) }
func (smallwebFamily) sources() int { return 6 }

func sourcesOf(f Family) int {
	if s, ok := f.(sized); ok {
		return max(s.sources(), 1)
	}
	return 3
}

// joined is a built-in family with the owner's sites added to it.
type joined struct {
	Family
	sites []Site
}

func (j joined) Draw(ctx context.Context, env Env) (Draw, error) {
	if rand.IntN(len(j.sites)+sourcesOf(j.Family)) < len(j.sites) {
		if d, err := drawSites(ctx, env, j.Name(), j.sites); err == nil {
			return d, nil
		}
	}
	return j.Family.Draw(ctx, env)
}

// shelf is a family made of the owner's sites alone.
type shelf struct {
	name  string
	sites []Site
}

func (s shelf) Name() string { return s.name }
func (s shelf) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawSites(ctx, env, s.name, s.sites)
}

// allFamilies are the built-in families, the owner's sites among them, and
// the owner's own shelves.
func allFamilies(env Env) []Family {
	var sites []Site
	if env.SitesPath != "" {
		sites, _ = LoadSites(env.SitesPath) // a broken file says so on its page, not here
	}
	if len(sites) == 0 {
		return families
	}
	byFamily := map[string][]Site{}
	for _, s := range sites {
		byFamily[s.family()] = append(byFamily[s.family()], s)
	}
	var out []Family
	for _, f := range families {
		if own := byFamily[f.Name()]; len(own) > 0 {
			out = append(out, joined{f, own})
			delete(byFamily, f.Name())
			continue
		}
		out = append(out, f)
	}
	var names []string
	for n := range byFamily {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, shelf{n, byFamily[n]})
	}
	return out
}

// drawSites tries up to three of the sites in random order.
func drawSites(ctx context.Context, env Env, family string, sites []Site) (Draw, error) {
	var errs []string
	for n, i := range rand.Perm(len(sites)) {
		if n == 3 {
			break
		}
		s := sites[i]
		target, title, err := drawSite(ctx, env, s)
		if err != nil {
			errs = append(errs, s.label()+": "+err.Error())
			continue
		}
		why := family + "/" + s.label()
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

func (s Site) label() string {
	if n := strings.TrimSpace(s.Name); n != "" {
		return n
	}
	return siteName(s.URL)
}

// siteName is an address's host without "www.".
func siteName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// drawSite draws one page from a site the way it says.
func drawSite(ctx context.Context, env Env, s Site) (string, string, error) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return "", "", err
	}
	if u.Scheme == "gemini" || u.Scheme == "gopher" {
		return smallLinkPick(ctx, env.Smallweb, s.URL)
	}
	switch s.how() {
	case "feed":
		return feedPick(ctx, env.Fetcher, s.URL)
	case "random":
		return fromRandom(s.URL)(ctx, env)
	case "links":
		return linkPick(ctx, env.Fetcher, s.URL, s.scope())
	case "walk":
		depth := s.Depth
		if depth <= 0 {
			depth = 4
		}
		return descendIn(ctx, env.Fetcher, s.URL, s.scope(), min(depth, 8))
	}
	return "", "", fmt.Errorf("unknown way %q (walk, feed, random or links)", s.How)
}

func (s Site) scope() string {
	if sc := strings.TrimSpace(s.Scope); sc != "" {
		if !strings.HasPrefix(sc, "/") {
			sc = "/" + sc
		}
		return sc
	}
	return dirOf(startPath(s.URL))
}

// feedPick returns a random item of an RSS, Atom or JSON feed.
func feedPick(ctx context.Context, f *fetch.Fetcher, raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return "", "", err
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(resp.Body))
	if err != nil {
		return "", "", errors.New("not a feed")
	}
	type item struct{ link, title string }
	var items []item
	for _, it := range feed.Items {
		t, err := u.Parse(strings.TrimSpace(it.Link))
		if err != nil || it.Link == "" || (t.Scheme != "http" && t.Scheme != "https") {
			continue
		}
		items = append(items, item{t.String(), strings.Join(strings.Fields(it.Title), " ")})
	}
	if len(items) == 0 {
		return "", "", errors.New("the feed has no items")
	}
	it := pick(items)
	return it.link, it.title, nil
}

// linkPick returns one of the page's links to its own site under scope.
func linkPick(ctx context.Context, f *fetch.Fetcher, raw, scope string) (string, string, error) {
	gq, base, err := get(ctx, f, raw)
	if err != nil {
		return "", "", err
	}
	gq.Find("script, style, nav, header, footer").Remove()
	type link struct{ href, text string }
	var links []link
	seen := map[string]bool{base.String(): true}
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		u, err := base.Parse(strings.TrimSpace(a.AttrOr("href", "")))
		if err != nil || u.Host != base.Host || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		u.Fragment = ""
		if seen[u.String()] || !strings.HasPrefix(u.Path, scope) || u.Path == "/" || skipExt[strings.ToLower(path.Ext(u.Path))] || reNotContent.MatchString(u.Path) {
			return
		}
		seen[u.String()] = true
		links = append(links, link{u.String(), strings.Join(strings.Fields(a.Text()), " ")})
	})
	if len(links) == 0 {
		return "", "", errors.New("no links to its own pages")
	}
	l := pick(links)
	return l.href, l.text, nil
}

// smallLinkPick returns one of a Gemini or Gopher page's links.
func smallLinkPick(ctx context.Context, env smallweb.Env, raw string) (string, string, error) {
	d, err := smallweb.Load(ctx, env, raw)
	if err != nil {
		return "", "", err
	}
	var links []doc.Link
	for _, l := range d.Links {
		u, err := url.Parse(l.Href)
		if err != nil || (u.Scheme != "gemini" && u.Scheme != "gopher") || l.Href == raw {
			continue
		}
		links = append(links, l)
	}
	if len(links) == 0 {
		return "", "", errors.New("no links")
	}
	l := pick(links)
	return l.Href, strings.Join(strings.Fields(l.Text), " "), nil
}

// tryWays finds how to draw from an address: a feed, a random-page address,
// a walk down its links, or one of its links, the first that lands.
func tryWays(ctx context.Context, env Env, s Site) (Site, string, string, []string) {
	var ways []string
	switch u, _ := url.Parse(s.URL); {
	case s.How != "":
		ways = []string{s.How}
	case u != nil && (u.Scheme == "gemini" || u.Scheme == "gopher"):
		ways = []string{"links"}
	case strings.Contains(strings.ToLower(s.URL), "random"):
		ways = []string{"random", "feed", "walk", "links"}
	default:
		ways = []string{"feed", "walk", "links"}
	}
	var errs []string
	for _, w := range ways {
		try := s
		try.How = w
		target, title, err := drawSite(ctx, env, try)
		if err == nil {
			return try, target, title, nil
		}
		if ctx.Err() != nil {
			return s, "", "", []string{ctx.Err().Error()}
		}
		errs = append(errs, w+": "+err.Error())
	}
	return s, "", "", errs
}

// withScheme adds https:// to an address typed without one.
func withScheme(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		return "https://" + raw
	}
	return raw
}

func siteQuery(s Site) url.Values {
	v := url.Values{"url": {s.URL}}
	for k, val := range map[string]string{"how": s.How, "family": s.Family, "name": s.Name, "scope": s.Scope} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if s.Depth > 0 {
		v.Set("depth", strconv.Itoa(s.Depth))
	}
	return v
}

func siteOf(q url.Values) Site {
	s := Site{URL: withScheme(q.Get("url")), How: q.Get("how"), Family: strings.ToLower(strings.TrimSpace(q.Get("family"))),
		Name: strings.TrimSpace(q.Get("name")), Scope: q.Get("scope")}
	s.Depth, _ = strconv.Atoi(q.Get("depth"))
	if s.Family == YoursFamily {
		s.Family = ""
	}
	return s
}

// sitesRoute builds the pages of the owner's sites: the list, a check of an
// address, adding, trying and removing one.
func sitesRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if env.SitesPath == "" {
		env.SitesPath = SitesPath()
	}
	sites, loadErr := LoadSites(env.SitesPath)
	find := func(raw string) int {
		for i, s := range sites {
			if s.URL == raw {
				return i
			}
		}
		return -1
	}
	switch strings.TrimPrefix(u.Opaque, "discover/sites") {
	case "":
		return sitesDoc(sites, loadErr, env.SitesPath, ""), nil
	case "/check":
		s := siteOf(q)
		if s.URL == "" {
			return nil, errors.New("which site? g → random-add <address>")
		}
		return checkSiteDoc(ctx, env, s, find(s.URL) >= 0), nil
	case "/add":
		if loadErr != nil {
			return nil, loadErr
		}
		s := siteOf(q)
		if s.URL == "" {
			return nil, errors.New("which site? g → random-add <address>")
		}
		if s.Name == "" {
			s.Name = siteName(s.URL)
		}
		s.Added = time.Now().Format("2006-01-02")
		note := "Added “" + s.Name + "” to the shelf " + s.family() + ". Deep random (x) now draws from it."
		if i := find(s.URL); i >= 0 {
			s.Added = sites[i].Added
			sites[i] = s
			note = "Changed “" + s.Name + "”."
		} else {
			sites = append(sites, s)
		}
		if err := SaveSites(env.SitesPath, sites); err != nil {
			return nil, err
		}
		return sitesDoc(sites, nil, env.SitesPath, note), nil
	case "/remove":
		if loadErr != nil {
			return nil, loadErr
		}
		i := find(q.Get("url"))
		if i < 0 {
			return sitesDoc(sites, nil, env.SitesPath, "That site is not in Deep random."), nil
		}
		name := sites[i].label()
		sites = append(sites[:i], sites[i+1:]...)
		if err := SaveSites(env.SitesPath, sites); err != nil {
			return nil, err
		}
		return sitesDoc(sites, nil, env.SitesPath, "Removed “"+name+"” from Deep random."), nil
	case "/try":
		i := find(q.Get("url"))
		if i < 0 {
			return nil, errors.New("that site is not in Deep random")
		}
		s := sites[i]
		ctx = fetch.SolverOnce(ctx)
		target, title, err := drawSite(ctx, env, s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.label(), err)
		}
		page, err := env.Load(ctx, target)
		if err != nil {
			return nil, err
		}
		why := s.family() + "/" + s.label()
		if title != "" {
			why += " · " + title
		}
		page.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: "Your site · " + why + " · open it again from your sites for another"}}, page.Blocks...)
		return page, nil
	}
	return nil, errors.New("unknown address: " + target)
}

// sitesDoc lists the owner's sites by shelf.
func sitesDoc(sites []Site, loadErr error, p, note string) *doc.Document {
	d := &doc.Document{Title: "Your sites in Deep random", URL: "w5f:discover/sites", Origin: "local", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	if loadErr != nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "The file cannot be read, so Deep random leaves your sites out: " + loadErr.Error()})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Any site can be added. W5F tries it once and finds how to draw a page from it: " +
		"its feed, its own random page, a walk down its links, or one of its links. A site is drawn on your own shelf, or joins one of Deep random's families.", Style: doc.Italic}}},
		doc.Paragraph{Text: doc.Inline{{Text: "add a site:  ", Style: doc.Italic}, {Text: "g → random-add <address> [shelf]", Style: doc.Code},
			{Text: "   (random-add alone adds the page you are on)", Style: doc.Italic}}})
	byFamily := map[string][]Site{}
	var names []string
	for _, s := range sites {
		if byFamily[s.family()] == nil {
			names = append(names, s.family())
		}
		byFamily[s.family()] = append(byFamily[s.family()], s)
	}
	sort.Strings(names)
	if len(sites) == 0 && loadErr == nil {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "No sites yet.", Style: doc.Italic}}})
	}
	builtin := map[string]bool{}
	for _, f := range families {
		builtin[f.Name()] = true
	}
	for _, n := range names {
		title := "Shelf: " + n
		if builtin[n] {
			title = "Joining " + n
		}
		var items [][]doc.Block
		for _, s := range byFamily[n] {
			v := url.Values{"url": {s.URL}}
			in := doc.Inline{{Text: s.label(), Style: doc.Bold, Link: link(s.URL, s.label())},
				{Text: "  · " + s.how() + " · ", Style: doc.Italic},
				{Text: "try one", Link: link("w5f:discover/sites/try?"+v.Encode(), "try "+s.label())}, {Text: " · "},
				{Text: "check again", Link: link("w5f:discover/sites/check?"+siteQuery(s).Encode(), "check "+s.label())}, {Text: " · "},
				{Text: "remove", Link: link("w5f:discover/sites/remove?"+v.Encode(), "remove "+s.label())}}
			items = append(items, []doc.Block{doc.Paragraph{Text: in}})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: title}}}, doc.List{Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "The list is kept in ", Style: doc.Italic}, {Text: p, Style: doc.Code},
		{Text: " — it can be edited by hand (how, scope, depth, family are explained at its top).", Style: doc.Italic}}})
	return d
}

// checkSiteDoc tries an address and offers to add it the way that worked.
func checkSiteDoc(ctx context.Context, env Env, s Site, have bool) *doc.Document {
	d := &doc.Document{Title: "Deep random: " + s.label(), URL: "w5f:discover/sites/check?" + siteQuery(s).Encode(), Origin: "live", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	found, target, title, errs := tryWays(fetch.SolverOnce(ctx), env, s)
	if target != "" {
		what := ""
		for _, h := range hows {
			if h.key == found.how() {
				what = h.what
			}
		}
		if title == "" {
			title = target
		}
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "It works: ", Style: doc.Bold}, {Text: "drawn " + found.how() + " — " + what + "."}}},
			doc.Paragraph{Text: doc.Inline{{Text: "tried once: ", Style: doc.Italic}, {Text: title, Link: link(target, title)}}})
	} else {
		found = s
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "No page could be drawn from it now (" + strings.Join(errs, "; ") + "). It can be added anyway, or tried another way."})
	}
	verb := "Add it to Deep random"
	if have {
		verb = "Keep it this way"
	}
	add := found
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ " + verb + " (shelf " + add.family() + ")", Style: doc.Bold,
		Link: link("w5f:discover/sites/add?"+siteQuery(add).Encode(), verb)}}})

	row := func(name string, opts []string, on string, set func(*Site, string)) {
		in := doc.Inline{{Text: name + "  ", Style: doc.Italic}}
		for i, o := range opts {
			if i > 0 {
				in = append(in, doc.Span{Text: " · "})
			}
			if o == on {
				in = append(in, doc.Span{Text: o, Style: doc.Bold})
				continue
			}
			x := found
			set(&x, o)
			in = append(in, doc.Span{Text: o, Link: link("w5f:discover/sites/check?"+siteQuery(x).Encode(), name+" "+o)})
		}
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: in})
	}
	var ways []string
	for _, h := range hows {
		ways = append(ways, h.key)
	}
	row("draw it another way", ways, found.how(), func(x *Site, o string) { x.How = o })
	shelves := []string{YoursFamily}
	for _, f := range families {
		shelves = append(shelves, f.Name())
	}
	row("put it on a shelf", shelves, found.family(), func(x *Site, o string) {
		x.Family = o
		if o == YoursFamily {
			x.Family = ""
		}
	})
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "A shelf of another name: g → random-add " + s.URL + " <name>", Style: doc.Italic}}},
		doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "your sites", Link: link("w5f:discover/sites", "your sites")}}})
	return d
}
