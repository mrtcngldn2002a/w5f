package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// ownSite is a small site of every kind Deep random can draw from: a feed,
// a random-page address, an index of long texts, and a list of short links.
func ownSite(t *testing.T) *httptest.Server {
	t.Helper()
	long := strings.Repeat("A long and patient paragraph about the old roads. ", 60)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Notes</title>
<item><title>On  milestones</title><link>%s/notes/milestones</link></item></channel></rss>`, srv.URL)
		case "/random":
			http.Redirect(w, r, "/wiki/Old_Roads", http.StatusFound)
		case "/docs/", "/docs/index.html":
			fmt.Fprint(w, `<html><body><a href="/docs/roads.html">Roads</a> <a href="/about">About</a> <a href="/elsewhere/x.html">Out</a></body></html>`)
		case "/docs/roads.html", "/wiki/Old_Roads", "/notes/milestones":
			fmt.Fprintf(w, `<html><head><title>Old Roads</title></head><body><p>%s</p></body></html>`, long)
		case "/list/":
			fmt.Fprint(w, `<html><body><a href="/list/one.html">One</a> <a href="/list/one.html">One again</a><p>short</p><a href="/list/two.html">Two</a></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func siteEnv(t *testing.T, fams ...Family) Env {
	t.Helper()
	env := testEnv(t, fams...)
	env.Fetcher = fetch.New(t.TempDir(), "test")
	env.Fetcher.HostGap = 0
	env.SitesPath = filepath.Join(t.TempDir(), "random.toml")
	return env
}

func TestOwnSitesFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "w5f", "random.toml")
	if s, err := LoadSites(p); err != nil || s != nil {
		t.Fatalf("no file: %v %v", s, err)
	}
	in := []Site{{Name: "Notes", URL: "https://example.org/feed.xml", How: "feed"},
		{URL: "https://example.org/docs/", Scope: "/docs/", Depth: 2, Family: "occult"}}
	if err := SaveSites(p, in); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "# Your sites in Deep random") || !strings.Contains(string(b), "[[site]]") {
		t.Errorf("file:\n%s", b)
	}
	out, err := LoadSites(p)
	if err != nil || len(out) != 2 || out[1] != in[1] || out[0].family() != "yours" || out[1].label() != "example.org" {
		t.Errorf("read back: %+v %v", out, err)
	}
	os.WriteFile(p, []byte("[[site]\nurl = "), 0o644)
	if _, err := LoadSites(p); err == nil {
		t.Error("a broken file read without an error")
	}
}

// W5F finds how to draw from each kind of address by trying it.
func TestOwnSiteWays(t *testing.T) {
	srv := ownSite(t)
	env := siteEnv(t)
	ctx := context.Background()
	for _, c := range []struct{ path, how, target, title string }{
		{"/feed.xml", "feed", "/notes/milestones", "On milestones"},
		{"/random", "random", "/wiki/Old_Roads", "Old Roads"},
		{"/docs/", "walk", "/docs/roads.html", "Old Roads"},
		{"/list/", "links", "/list/", ""}, // one of its two links
	} {
		s, target, title, errs := tryWays(ctx, env, Site{URL: srv.URL + c.path})
		if s.How != c.how || !strings.HasPrefix(target, srv.URL+c.target) || c.title != "" && title != c.title {
			t.Errorf("%s: how %q target %q title %q errs %v", c.path, s.How, target, title, errs)
		}
	}
	// The walk keeps to the address's folder; a scope widens it.
	if _, _, err := drawSite(ctx, env, Site{URL: srv.URL + "/docs/", How: "links"}); err != nil {
		t.Errorf("links under /docs/: %v", err)
	}
	if target, _, _ := drawSite(ctx, env, Site{URL: srv.URL + "/docs/", How: "links", Scope: "elsewhere"}); target != srv.URL+"/elsewhere/x.html" {
		t.Errorf("scope: %q", target)
	}
	if _, _, _, errs := tryWays(ctx, env, Site{URL: srv.URL + "/nothing"}); len(errs) != 3 {
		t.Errorf("a dead address: %v", errs)
	}
}

// The owner's sites come up in Deep random: on their own shelf, on a shelf
// they name, or in the family they join.
func TestOwnSitesInDeepRandom(t *testing.T) {
	srv := ownSite(t)
	esoteric := fakeFamily{name: "esoteric", fail: true}
	env := siteEnv(t, esoteric, fakeFamily{name: "folklore", fail: true})
	if fams := allFamilies(env); len(fams) != 2 {
		t.Fatalf("no sites, the families as they are: %v", fams)
	}
	SaveSites(env.SitesPath, []Site{
		{Name: "Notes", URL: srv.URL + "/feed.xml", How: "feed"},
		{URL: srv.URL + "/random", How: "random", Family: "occult"},
		{URL: srv.URL + "/docs/", Family: "Esoteric"},
	})
	var names []string
	for _, f := range allFamilies(env) {
		names = append(names, f.Name())
		if _, ok := f.(joined); ok != (f.Name() == "esoteric") {
			t.Errorf("%s joined: %v", f.Name(), ok)
		}
	}
	if strings.Join(names, " ") != "esoteric folklore occult yours" {
		t.Errorf("families: %v", names)
	}
	ctx := context.Background()
	seen := map[string]string{}
	for range 40 { // the built-in families here always fail; three in a row may
		d, page, err := Next(ctx, env)
		if err != nil {
			continue
		}
		seen[d.Family] = d.Why
		if !strings.HasPrefix(page.Blocks[0].(doc.Notice).Text, "Deep random · "+d.Why) {
			t.Errorf("why: %+v", page.Blocks[0])
		}
	}
	if seen["yours"] != "yours/Notes · On milestones" || seen["occult"] != "occult/127.0.0.1 · Old Roads" {
		t.Errorf("drawn: %v", seen)
	}
	// A failing family with a site of the owner's draws only from it.
	if w, ok := seen["esoteric"]; ok && w != "esoteric/127.0.0.1 · Old Roads" {
		t.Errorf("esoteric: %q", w)
	}
	if _, ok := seen["folklore"]; ok {
		t.Error("a failing family was drawn")
	}
}

func TestOwnSitesPages(t *testing.T) {
	srv := ownSite(t)
	env := siteEnv(t, fakeFamily{name: "esoteric"})
	ctx := context.Background()
	open := func(target string) *doc.Document {
		t.Helper()
		d, err := Route(ctx, target, env)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		return d
	}
	text := func(d *doc.Document) string {
		var b strings.Builder
		for _, bl := range d.Blocks {
			switch x := bl.(type) {
			case doc.Paragraph:
				b.WriteString(x.Text.PlainText() + "\n")
			case doc.Notice:
				b.WriteString(x.Text + "\n")
			case doc.Heading:
				b.WriteString("## " + x.Text.PlainText() + "\n")
			case doc.List:
				for _, it := range x.Items {
					b.WriteString("- " + it[0].(doc.Paragraph).Text.PlainText() + "\n")
				}
			}
		}
		return b.String()
	}
	href := func(d *doc.Document, text string) string {
		for _, l := range d.Links {
			if l.Text == text {
				return l.Href
			}
		}
		t.Fatalf("no link %q in %+v", text, d.Links)
		return ""
	}

	if d := open("w5f:discover/sites"); !strings.Contains(text(d), "No sites yet.") {
		t.Errorf("empty:\n%s", text(d))
	}
	// Checked: drawn from its feed; then put on the esoteric shelf instead.
	check := open("w5f:discover/sites/check?url=" + srv.URL + "/feed.xml")
	if !strings.Contains(text(check), "It works: drawn feed") || !strings.Contains(text(check), "tried once: On milestones") {
		t.Errorf("check:\n%s", text(check))
	}
	check = open(href(check, "put it on a shelf esoteric"))
	added := open(href(check, "Add it to Deep random"))
	if !strings.Contains(text(added), "Added “127.0.0.1” to the shelf esoteric") || !strings.Contains(text(added), "## Joining esoteric") {
		t.Errorf("added:\n%s", text(added))
	}
	sites, _ := LoadSites(env.SitesPath)
	if len(sites) != 1 || sites[0].How != "feed" || sites[0].Family != "esoteric" || sites[0].Added == "" {
		t.Errorf("saved: %+v", sites)
	}
	// Tried from the list: the page with the line saying where it came from.
	page := open(href(added, "try 127.0.0.1"))
	if n := page.Blocks[0].(doc.Notice).Text; !strings.HasPrefix(n, "Your site · esoteric/127.0.0.1 · On milestones") {
		t.Errorf("try: %q", n)
	}
	// Checked again, it is kept, not added twice; then removed.
	again := open(href(added, "check 127.0.0.1"))
	kept := open(href(again, "Keep it this way"))
	if sites, _ := LoadSites(env.SitesPath); len(sites) != 1 || !strings.Contains(text(kept), "Changed") {
		t.Errorf("kept: %+v\n%s", sites, text(kept))
	}
	gone := open(href(kept, "remove 127.0.0.1"))
	if sites, _ := LoadSites(env.SitesPath); len(sites) != 0 || !strings.Contains(text(gone), "Removed “127.0.0.1”") {
		t.Errorf("removed: %+v\n%s", sites, text(gone))
	}
	// A dead address can still be added, with a warning.
	dead := open("w5f:discover/sites/check?url=" + srv.URL + "/nothing&family=occult")
	if !strings.Contains(text(dead), "No page could be drawn") || !strings.Contains(text(dead), "→ Add it to Deep random (shelf occult)") {
		t.Errorf("dead:\n%s", text(dead))
	}
}
