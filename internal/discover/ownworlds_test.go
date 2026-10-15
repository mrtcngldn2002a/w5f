package discover

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"w5f/internal/doc"
)

// ownDataDir points W5F's data folder at a temporary one.
func ownDataDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("APPDATA", d)
	t.Setenv("XDG_DATA_HOME", d)
	t.Setenv("XDG_CONFIG_HOME", d)
}

func plain(d *doc.Document) string {
	var b strings.Builder
	var walk func([]doc.Block)
	walk = func(bs []doc.Block) {
		for _, bl := range bs {
			switch x := bl.(type) {
			case doc.Paragraph:
				b.WriteString(x.Text.PlainText() + "\n")
			case doc.Notice:
				b.WriteString(x.Text + "\n")
			case doc.Heading:
				b.WriteString("## " + x.Text.PlainText() + "\n")
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

// A wiki of the owner's joins the Archived Worlds, in its section and in
// Deep random's draw, and leaves again after asking.
func TestOwnWorlds(t *testing.T) {
	ownDataDir(t)
	builtin, _ := Worlds()
	ctx := context.Background()
	open := func(target string) *doc.Document {
		t.Helper()
		d, err := Route(ctx, target, Env{})
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		return d
	}
	add := "w5f:worlds/add?" + url.Values{"url": {"https://wiki.example/"}, "name": {"The Example Wiki"}, "family": {"backrooms"}}.Encode()
	d := open(add)
	if txt := plain(d); !strings.Contains(txt, "Added “The Example Wiki” to the Archived Worlds") ||
		!strings.Contains(txt, "The Example Wiki  · live · tier 1 · yours · remove") {
		t.Fatalf("added:\n%s", txt)
	}
	ws, err := Worlds()
	if err != nil || len(ws) != len(builtin)+1 || ws[len(ws)-1].Family != "backrooms" || ws[len(ws)-1].Added == "" {
		t.Fatalf("worlds: %v %+v", err, ws[len(ws)-1])
	}
	if txt := plain(open(add)); !strings.Contains(txt, "Changed “The Example Wiki”") {
		t.Errorf("again:\n%s", txt)
	}
	if txt := plain(open("w5f:worlds/add?" + url.Values{"url": {"https://rpcauthority.wikidot.com/x"}}.Encode())); !strings.Contains(txt, "already among the worlds") {
		t.Errorf("a built-in world:\n%s", txt)
	}
	remove := "w5f:worlds/remove?" + url.Values{"url": {"https://wiki.example/"}}.Encode()
	if txt := plain(open(remove)); !strings.Contains(txt, "No, keep it · Yes, remove it") {
		t.Errorf("remove asks first:\n%s", txt)
	}
	if own, _ := UserWorlds(); len(own) != 1 {
		t.Fatal("removed before asking")
	}
	if txt := plain(open(remove + "&sure=yes")); !strings.Contains(txt, "Removed “The Example Wiki”") {
		t.Errorf("removed:\n%s", txt)
	}
	if own, _ := UserWorlds(); len(own) != 0 {
		t.Errorf("still there: %+v", own)
	}
	// A broken file of the owner's leaves the built-in worlds in place.
	os.WriteFile(UserWorldsPath(), []byte("[[world]\nname = "), 0o644)
	if ws, err := Worlds(); err == nil || len(ws) != len(builtin) {
		t.Errorf("broken file: %v %d", err, len(ws))
	}
	if txt := plain(open("w5f:worlds")); !strings.Contains(txt, "Your worlds cannot be read") {
		t.Errorf("broken file's page:\n%s", txt)
	}
}

// The check of a site carries its place in the Daily Packet through to
// the file.
func TestOwnSiteForThePacket(t *testing.T) {
	srv := ownSite(t)
	env := siteEnv(t, fakeFamily{name: "esoteric"})
	ctx := context.Background()
	check, err := Route(ctx, "w5f:discover/sites/check?url="+srv.URL+"/feed.xml", env)
	if err != nil {
		t.Fatal(err)
	}
	var yes string
	for _, l := range check.Links {
		if l.Text == "in the Daily Packet too yes" {
			yes = l.Href
		}
	}
	check, err = Route(ctx, yes, env)
	if err != nil {
		t.Fatal(err)
	}
	var add string
	for _, l := range check.Links {
		if l.Text == "Add it to Deep random and the Daily Packet" {
			add = l.Href
		}
	}
	added, err := Route(ctx, add, env)
	if err != nil {
		t.Fatal(err)
	}
	if txt := plain(added); !strings.Contains(txt, "and to the Daily Packet") || !strings.Contains(txt, "· feed · in the Daily Packet ·") {
		t.Errorf("added:\n%s", txt)
	}
	if sites, _ := LoadSites(env.SitesPath); len(sites) != 1 || !sites[0].Packet {
		t.Errorf("saved: %+v", sites)
	}
	b, _ := os.ReadFile(env.SitesPath)
	if !strings.Contains(string(b), "packet = true") {
		t.Errorf("file:\n%s", b)
	}
}
