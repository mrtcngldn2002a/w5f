package source

import (
	"fmt"
	"net/url"
	"strings"

	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/feeds"
	"w5f/internal/places"
)

// g → add (2026-10-15, the owner asked that any site or source could be put
// anywhere): one page asks where a site goes, and sends it on to the place's
// own check. g → add alone takes the page being read; w5f:add with no
// address shows what has been added, place by place.

// AddTarget is the "where should it go?" page of an address; name is what
// the page calls itself, if known.
func AddTarget(addr, name string) string {
	v := url.Values{"url": {addr}}
	if name = strings.Join(strings.Fields(name), " "); name != "" {
		v.Set("name", name)
	}
	return "w5f:add?" + v.Encode()
}

// isAddTarget reports the add page's addresses.
func isAddTarget(t string) bool { return t == "w5f:add" || strings.HasPrefix(t, "w5f:add?") }

func addRoute(target string) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	addr := strings.TrimSpace(u.Query().Get("url"))
	if addr == "" {
		return additionsDoc(), nil
	}
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	return whereDoc(addr, u.Query().Get("name")), nil
}

// whereDoc lists the places a site can go, saying where it already is.
func whereDoc(addr, name string) *doc.Document {
	if name == "" {
		name = hostName(addr)
	}
	d := &doc.Document{Title: "Add “" + name + "”", URL: AddTarget(addr, name), Origin: "local", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: addr, Style: doc.Italic}}},
		doc.Paragraph{Text: doc.Inline{{Text: "Where should it go? Each place tries it first and asks before it keeps anything.", Style: doc.Italic}}})

	sites, _ := discover.LoadSites(discover.SitesPath())
	var site *discover.Site
	for i := range sites {
		if sameSite(sites[i].URL, addr) {
			site = &sites[i]
		}
	}
	var feed *feeds.Feed
	if c, err := feeds.LoadCatalog(); err == nil {
		feed = c.Following(addr)
	}
	worlds, _ := discover.UserWorlds()
	var world *discover.World
	for i := range worlds {
		if sameSite(worlds[i].URL, addr) {
			world = &worlds[i]
		}
	}
	pinned, _ := places.Load()
	var place *places.Place
	for i := range pinned {
		if pinned[i].URL == addr {
			place = &pinned[i]
		}
	}

	var items [][]doc.Block
	item := func(title, what, have string, choices ...doc.Span) {
		in := doc.Inline{{Text: title, Style: doc.Bold}}
		if have != "" {
			in = append(in, doc.Span{Text: "  · " + have, Style: doc.Italic})
		}
		bs := []doc.Block{doc.Paragraph{Text: in}, doc.Paragraph{Text: doc.Inline{{Text: what, Style: doc.Italic}}}}
		if len(choices) > 0 {
			bs = append(bs, doc.Paragraph{Text: append(doc.Inline{{Text: "→ "}}, choices...)})
		}
		items = append(items, bs)
	}
	sep := doc.Span{Text: " · "}
	check := func(packet bool) string {
		v := url.Values{"url": {addr}}
		if name != hostName(addr) {
			v.Set("name", name)
		}
		if site != nil {
			v.Set("family", site.Family)
			v.Set("how", site.How)
		}
		if packet || (site != nil && site.Packet) {
			v.Set("packet", "1")
		}
		return "w5f:discover/sites/check?" + v.Encode()
	}

	have := ""
	if site != nil {
		shelf := site.Family
		if shelf == "" {
			shelf = discover.YoursFamily
		}
		have = "in it, on the shelf " + shelf
	}
	item("Deep random (x)", "A page of it now and then, on a shelf of yours or joining one of the families (esoteric, sufism, weird…).", have,
		doc.Span{Text: "try it and choose a shelf", Link: link(check(false), "Deep random")})

	have = ""
	if site != nil && site.Packet {
		have = "in it"
	}
	item("The Daily Packet (p)", "A page of it in every issue, under “Your sites” (it joins Deep random too).", have,
		doc.Span{Text: "try it for the Packet", Link: link(check(true), "Daily Packet")})

	have = ""
	if feed != nil {
		have = "followed, as " + feed.Name
	}
	item("Periodical Gallery (2)", "Its new posts, through its feed (RSS or Atom), on a shelf of the gallery.", have,
		doc.Span{Text: "find its feed", Link: link("w5f:feeds/add?"+url.Values{"url": {addr}}.Encode(), "Periodicals")})

	have = ""
	if world != nil {
		have = "one of them"
	}
	wv := func(family string) string {
		return "w5f:worlds/add?" + url.Values{"url": {addr}, "name": {name}, "family": {family}}.Encode()
	}
	item("Obscure & Archived Worlds", "A fiction wiki among the worlds; Deep random walks into it now and then.", have,
		doc.Span{Text: "containment fiction", Link: link(wv("confic"), "worlds: confic")}, sep,
		doc.Span{Text: "Backrooms", Link: link(wv("backrooms"), "worlds: backrooms")}, sep,
		doc.Span{Text: "other fiction", Link: link(wv("fiction"), "worlds: fiction")})

	have = ""
	if place != nil {
		have = "pinned"
	}
	item("The Reading Room (1)", "Pinned under “Your places”, a key away.", have,
		doc.Span{Text: "pin it", Link: link("w5f:places/add?"+url.Values{"url": {addr}, "name": {name}}.Encode(), "Reading Room")})

	item("The Stacks (3)", "A site with books: its search becomes one of your catalogs (cat …).", "",
		doc.Span{Text: "inspect it as a catalog", Link: link("w5f:catalog/check?"+url.Values{"url": {addr}}.Encode(), "Stacks")})

	item("The Serial Hall (4)", "A web serial or a forum thread of chapters: opened as a serial, u follows it.", "",
		doc.Span{Text: "open it as a serial", Link: link("w5f:serial/open?"+url.Values{"u": {addr}}.Encode(), "Serial Hall")})

	d.Blocks = append(d.Blocks, doc.List{Items: items}, doc.Rule{},
		doc.Paragraph{Text: doc.Inline{{Text: "everything you have added", Link: link("w5f:add", "your additions")}}})
	return d
}

// additionsDoc is what the owner has added, place by place, each with its
// own page to change it.
func additionsDoc() *doc.Document {
	d := &doc.Document{Title: "What you have added", URL: "w5f:add", Origin: "local", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Add a site to any of these with ", Style: doc.Italic},
		{Text: "g → add", Style: doc.Code}, {Text: " on its page, or ", Style: doc.Italic}, {Text: "g → add <address>", Style: doc.Code}, {Text: ".", Style: doc.Italic}}})
	count := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	var items [][]doc.Block
	row := func(title, href, sub string, extra ...[]doc.Block) {
		bs := []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: title, Style: doc.Bold, Link: link(href, title)}, {Text: "  · " + sub, Style: doc.Italic}}}}
		for _, e := range extra {
			bs = append(bs, e...)
		}
		items = append(items, bs)
	}
	names := func(ns []string) [][]doc.Block {
		if len(ns) == 0 {
			return nil
		}
		return [][]doc.Block{{doc.Paragraph{Text: doc.Inline{{Text: strings.Join(ns, " · ")}}}}}
	}

	sites, err := discover.LoadSites(discover.SitesPath())
	var siteNames, packetNames []string
	for _, s := range sites {
		n := s.Name
		if n == "" {
			n = hostName(s.URL)
		}
		siteNames = append(siteNames, n)
		if s.Packet {
			packetNames = append(packetNames, n)
		}
	}
	sub := count(len(sites), "site", "sites")
	if err != nil {
		sub = "random.toml cannot be read: " + err.Error()
	}
	row("Deep random", "w5f:discover/sites", sub, names(siteNames)...)
	row("The Daily Packet", "w5f:discover/sites", count(len(packetNames), "site", "sites")+" under “Your sites”", names(packetNames)...)

	var feedNames []string
	if c, err := feeds.LoadCatalog(); err == nil {
		for _, f := range feeds.UserFeeds(c) {
			feedNames = append(feedNames, f.Name)
		}
	}
	row("Periodical Gallery", "w5f:feeds", count(len(feedNames), "feed of yours", "feeds of yours")+" (feeds.toml)", names(feedNames)...)

	worlds, werr := discover.UserWorlds()
	var worldNames []string
	for _, w := range worlds {
		worldNames = append(worldNames, w.Name)
	}
	sub = count(len(worlds), "world", "worlds")
	if werr != nil {
		sub = "worlds.toml cannot be read: " + werr.Error()
	}
	row("Obscure & Archived Worlds", "w5f:worlds", sub, names(worldNames)...)

	pinned, perr := places.Load()
	var placeNames []string
	for _, p := range pinned {
		placeNames = append(placeNames, p.Name)
	}
	sub = count(len(pinned), "place", "places")
	if perr != nil {
		sub = "places.toml cannot be read: " + perr.Error()
	}
	row("The Reading Room", "w5f:places", sub+" pinned", names(placeNames)...)
	row("The Stacks", "w5f:catalogs", "your site catalogs")
	row("The Serial Hall", "w5f:following", "the serials you follow")

	d.Blocks = append(d.Blocks, doc.List{Items: items})
	return d
}

// sameSite reports two addresses of the same page, www. and a final slash
// aside.
func sameSite(a, b string) bool {
	norm := func(s string) string {
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || u.Host == "" {
			return strings.ToLower(strings.TrimSpace(s))
		}
		return strings.TrimPrefix(strings.ToLower(u.Host), "www.") + strings.TrimSuffix(u.EscapedPath(), "/") + "?" + u.RawQuery
	}
	return norm(a) == norm(b)
}

func hostName(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return raw
}
