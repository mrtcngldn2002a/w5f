package source

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/store"
)

func addPageText(d *doc.Document) string {
	var b strings.Builder
	var walk func([]doc.Block)
	walk = func(bs []doc.Block) {
		for _, bl := range bs {
			switch x := bl.(type) {
			case doc.Paragraph:
				for _, s := range x.Text {
					b.WriteString(s.Text)
				}
				b.WriteString("\n")
			case doc.Notice:
				b.WriteString(x.Text + "\n")
			case doc.List:
				for _, it := range x.Items {
					walk(it)
				}
			}
		}
	}
	walk(d.Blocks)
	return b.String()
}

func TestResolveAdd(t *testing.T) {
	cases := map[string]string{
		"add tdv.org.tr":                    "w5f:add?url=tdv.org.tr",
		"ADD https://a.org/x The A Pages":   "w5f:add?name=The+A+Pages&url=https%3A%2F%2Fa.org%2Fx",
		"add milk to the list":              "w5f:search/web?q=add+milk+to+the+list",
		"additions":                         "w5f:add",
		"my additions":                      "w5f:add",
		"random-add https://a.org/x occult": "w5f:discover/sites/check?family=occult&url=https%3A%2F%2Fa.org%2Fx",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

// The add page offers every place, each through its own check, and says
// where the site already is.
func TestAddPageOffersEveryPlace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.SetDefault(db)
	t.Cleanup(func() { store.SetDefault(nil); db.Close() })
	addr := "https://www.tdv.example/madde/"
	if err := discover.SaveSites(discover.SitesPath(), []discover.Site{{Name: "TDV", URL: "https://tdv.example/madde", Family: "sufism", Packet: true}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := Load(ctx, "w5f:places/add?"+url.Values{"url": {addr}, "name": {"TDV"}}.Encode(), Options{}); err != nil {
		t.Fatal(err)
	}
	d, err := Load(ctx, AddTarget(addr, "TDV İslâm Ansiklopedisi"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Add “TDV İslâm Ansiklopedisi”" {
		t.Errorf("title %q", d.Title)
	}
	txt := addPageText(d)
	for _, want := range []string{"Deep random (x)  · in it, on the shelf sufism", "The Daily Packet (p)  · in it",
		"Periodical Gallery (2)\n", "The Reading Room (1)  · pinned", "Obscure & Archived Worlds\n"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	hrefs := map[string]string{}
	for _, l := range d.Links {
		hrefs[l.Text] = l.Href
	}
	for text, prefix := range map[string]string{
		"Deep random":       "w5f:discover/sites/check?",
		"Daily Packet":      "w5f:discover/sites/check?",
		"Periodicals":       "w5f:feeds/add?url=",
		"worlds: backrooms": "w5f:worlds/add?",
		"Reading Room":      "w5f:places/add?",
		"Stacks":            "w5f:catalog/check?url=",
		"Serial Hall":       "w5f:serial/open?u=",
		"your additions":    "w5f:add",
	} {
		if !strings.HasPrefix(hrefs[text], prefix) {
			t.Errorf("%s: %q, want %s…", text, hrefs[text], prefix)
		}
	}
	// The check keeps what the site has: its shelf and its place in the Packet.
	if q, _ := url.ParseQuery(strings.TrimPrefix(hrefs["Deep random"], "w5f:discover/sites/check?")); q.Get("family") != "sufism" || q.Get("packet") != "1" {
		t.Errorf("Deep random check: %s", hrefs["Deep random"])
	}

	// A world added through the page shows on the additions page.
	if _, err := Load(ctx, hrefs["worlds: backrooms"], Options{}); err != nil {
		t.Fatal(err)
	}
	all, err := Load(ctx, "w5f:add", Options{})
	if err != nil {
		t.Fatal(err)
	}
	txt = addPageText(all)
	for _, want := range []string{"Deep random  · 1 site\nTDV", "The Daily Packet  · 1 site under “Your sites”", "Obscure & Archived Worlds  · 1 world\nTDV İslâm Ansiklopedisi",
		"The Reading Room  · 1 place pinned\nTDV", "Periodical Gallery  · 0 feeds of yours"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(discover.SitesPath()), "worlds.toml")); err != nil {
		t.Error(err)
	}
}
