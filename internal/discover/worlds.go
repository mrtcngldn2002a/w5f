package discover

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"w5f/internal/doc"
	"w5f/internal/store"
)

//go:embed worlds.toml
var worldsTOML string

// World is a small or old fiction wiki.
type World struct {
	Name, URL, Family, Status, Note string
	Tier                            int
	Added                           string `toml:",omitempty"`
	yours                           bool   // from the owner's worlds.toml
}

// Worlds is the embedded catalog with the owner's own worlds after it.
func Worlds() ([]World, error) {
	var c struct {
		World []World `toml:"world"`
	}
	if _, err := toml.Decode(worldsTOML, &c); err != nil {
		return nil, err
	}
	own, err := UserWorlds()
	return append(c.World, own...), err
}

type worldFamily struct{ key, title string }

// worldFamilies are the worlds' kinds, as the list shows them.
var worldFamilies = []worldFamily{{"confic", "Containment fiction"}, {"backrooms", "Backrooms offshoots"}, {"fiction", "Other fiction"}}

func knownWorldFamily(key string) bool {
	return slices.ContainsFunc(worldFamilies, func(f worldFamily) bool { return f.key == key })
}

// UserWorldsPath is the owner's worlds (2026-10-15: g → add puts a fiction
// wiki among the Archived Worlds).
func UserWorldsPath() string { return filepath.Join(store.DataDir(), "worlds.toml") }

// UserWorlds reads the owner's worlds; no file is none.
func UserWorlds() ([]World, error) {
	var c struct {
		World []World `toml:"world"`
	}
	if _, err := toml.DecodeFile(UserWorldsPath(), &c); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", UserWorldsPath(), err)
	}
	var out []World
	for _, w := range c.World {
		if strings.TrimSpace(w.URL) == "" {
			continue
		}
		w.yours = true
		if w.Tier < 1 || w.Tier > 3 {
			w.Tier = 1 // chosen by the owner: drawn as often as the main ones
		}
		if w.Status == "" {
			w.Status = "live"
		}
		if !knownWorldFamily(w.Family) {
			w.Family = "fiction"
		}
		out = append(out, w)
	}
	return out, nil
}

const worldsHeader = `# Your Archived Worlds (g → worlds). W5F writes this file when you add a
# wiki (g → add on its page); it can be edited by hand too.
#
#   name    the wiki's name      url     its front page
#   family  confic, backrooms or fiction (the list's sections)
#   tier    1 main, 2 good, 3 obscure: Deep random draws 3 : 2 : 1 (unset: 1)
#   status  live, or archived for one read from the Wayback Machine

`

func saveUserWorlds(ws []World) error {
	var b bytes.Buffer
	b.WriteString(worldsHeader)
	if err := toml.NewEncoder(&b).Encode(struct {
		World []World `toml:"world"`
	}{ws}); err != nil {
		return err
	}
	p := UserWorldsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// worldsRoute adds a world (w5f:worlds/add?url=…&name=…&family=…) or, asking
// first, removes one of the owner's (w5f:worlds/remove?url=…).
func worldsRoute(target string) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	addr := withScheme(q.Get("url"))
	if addr == "" {
		return nil, errors.New("which wiki? g → add <address>")
	}
	own, err := UserWorlds()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(own, func(w World) bool { return w.URL == addr })
	switch u.Opaque {
	case "worlds/add":
		w := World{Name: strings.TrimSpace(q.Get("name")), URL: addr, Family: q.Get("family"), Status: "live", Tier: 1, Added: time.Now().Format("2006-01-02")}
		if w.Name == "" {
			w.Name = siteName(addr)
		}
		if !knownWorldFamily(w.Family) {
			w.Family = "fiction"
		}
		note := "Added “" + w.Name + "” to the Archived Worlds. Deep random (x) visits it now and then."
		if i >= 0 {
			w.Added, own[i] = own[i].Added, w
			note = "Changed “" + w.Name + "”."
		} else {
			all, _ := Worlds()
			if slices.ContainsFunc(all, func(x World) bool { return siteName(x.URL) == siteName(addr) }) {
				return worldsDoc("“" + w.Name + "” is already among the worlds.")
			}
			own = append(own, w)
		}
		if err := saveUserWorlds(own); err != nil {
			return nil, err
		}
		return worldsDoc(note)
	case "worlds/remove":
		if i < 0 {
			return worldsDoc("That wiki is not one of yours.")
		}
		if q.Get("sure") != "yes" { // every removal asks first, as in the Weeding Room
			v := url.Values{"url": {addr}}
			d := &doc.Document{Title: "Remove this world?", URL: "w5f:worlds/remove?" + v.Encode(), Origin: "local", Lang: "en"}
			link := func(href, text string) int {
				d.Links = append(d.Links, doc.Link{Href: href, Text: text})
				return len(d.Links)
			}
			v.Set("sure", "yes")
			d.Blocks = []doc.Block{
				doc.Paragraph{Text: doc.Inline{{Text: own[i].Name + " · " + addr}}},
				doc.Paragraph{Text: doc.Inline{{Text: "It leaves the Archived Worlds and Deep random; g → add on its page brings it back.", Style: doc.Italic}}},
				doc.Paragraph{Text: doc.Inline{{Text: "No, keep it", Link: link("w5f:worlds", "keep")}, {Text: " · "},
					{Text: "Yes, remove it", Style: doc.Bold, Link: link("w5f:worlds/remove?"+v.Encode(), "remove")}}},
			}
			return d, nil
		}
		name := own[i].Name
		if err := saveUserWorlds(slices.Delete(own, i, i+1)); err != nil {
			return nil, err
		}
		return worldsDoc("Removed “" + name + "” from the Archived Worlds.")
	}
	return nil, errors.New("unknown address: " + target)
}

// pickWorld weights worlds by tier (1: 3, 2: 2, 3: 1).
func pickWorld(ws []World) World {
	total := 0
	for _, w := range ws {
		total += 4 - w.Tier
	}
	n := rand.IntN(total)
	for _, w := range ws {
		if n -= 4 - w.Tier; n < 0 {
			return w
		}
	}
	return ws[len(ws)-1]
}

func worldsDoc(note string) (*doc.Document, error) {
	ws, err := Worlds()
	d := &doc.Document{Title: "Obscure & Archived Worlds", URL: "w5f:worlds", Origin: "local", Lang: "en"}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	if err != nil {
		if len(ws) == 0 {
			return nil, err
		}
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "Your worlds cannot be read, so they are left out: " + err.Error()})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Small and old fiction wikis. Dead ones open from the Wayback Machine. Deep random (x) visits them now and then. " +
		"A wiki of your own joins them with g → add on its page.", Style: doc.Italic}}})
	for _, fam := range worldFamilies {
		var items [][]doc.Block
		for _, w := range ws {
			if w.Family != fam.key {
				continue
			}
			d.Links = append(d.Links, doc.Link{Href: w.URL, Text: w.Name})
			in := doc.Inline{{Text: w.Name, Style: doc.Bold, Link: len(d.Links)}, {Text: fmt.Sprintf("  · %s · tier %d", w.Status, w.Tier), Style: doc.Italic}}
			if w.yours {
				d.Links = append(d.Links, doc.Link{Href: "w5f:worlds/remove?" + url.Values{"url": {w.URL}}.Encode(), Text: "remove " + w.Name})
				in = append(in, doc.Span{Text: " · yours · ", Style: doc.Italic}, doc.Span{Text: "remove", Link: len(d.Links)})
			}
			bs := []doc.Block{doc.Paragraph{Text: in}}
			if w.Note != "" {
				bs = append(bs, doc.Paragraph{Text: doc.Inline{{Text: w.Note}}})
			}
			items = append(items, bs)
		}
		if len(items) > 0 {
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: fam.title}}}, doc.List{Items: items})
		}
	}
	return d, nil
}

// weird: SCP, Wanderers' Library, Backrooms (Crom) or an archived world.
type weird struct{}

func (weird) Name() string { return "weird" }
func (weird) Draw(ctx context.Context, env Env) (Draw, error) {
	if env.Crom != nil && rand.IntN(3) > 0 {
		preset := pick([]string{"scp", "tale", "wl", "backrooms"})
		u, err := env.Crom(ctx, preset)
		if err == nil {
			return Draw{Target: u, Why: "weird/" + map[string]string{"scp": "SCP Foundation", "tale": "SCP tales",
				"wl": "Wanderers' Library", "backrooms": "The Backrooms"}[preset]}, nil
		}
	}
	ws, _ := Worlds() // a broken worlds.toml of the owner's leaves the built-in ones
	if len(ws) == 0 {
		return Draw{}, errors.New("no worlds")
	}
	w := pickWorld(ws)
	why := fmt.Sprintf("weird/archived worlds · %s (tier %d)", w.Name, w.Tier)
	// A world's front door is a page of links: a page of its own is shown,
	// found by walking in (the door itself when the walk finds none).
	if env.Fetcher != nil {
		if target, title, err := descend(ctx, env.Fetcher, w.URL, 3); err == nil {
			if title != "" {
				why += " · " + title
			}
			return Draw{Target: target, Why: why}, nil
		}
	}
	return Draw{Target: w.URL, Why: why}, nil
}

// fiction: a story from several places: a top-of-all-time post of a few
// horror and literary subreddits (with the owner's Reddit session), Nature's
// one-page science fiction, Creepypasta.com, or a random SCP tale. The
// places that need the web are in fictionWeb (fictionweb.go).
type fiction struct{}

var fictionSubs = []string{"WeirdLit", "LibraryOfShadows", "shortstories", "nosleep", "TheCrypticCompendium",
	"Odd_directions", "Cryosleep", "shortscarystories"}

func (fiction) Name() string { return "fiction" }
func (fiction) sources() int { return 3 + len(fictionWeb) }

func (fiction) Draw(ctx context.Context, env Env) (Draw, error) {
	type source struct {
		weight int
		draw   func() (Draw, error)
	}
	var srcs []source
	if env.RedditTop != nil {
		srcs = append(srcs, source{4, func() (Draw, error) {
			sub := pick(fictionSubs)
			posts, err := env.RedditTop(ctx, sub)
			if err != nil || len(posts) == 0 {
				return Draw{}, errors.New("no posts on r/" + sub)
			}
			return Draw{Target: pick(posts), Why: "fiction/r/" + sub + " · top of all time"}, nil
		}})
	}
	if env.Fetcher != nil {
		for _, s := range fictionWeb {
			srcs = append(srcs, source{s.weight, func() (Draw, error) {
				target, title, err := s.pick(ctx, env)
				if err != nil {
					return Draw{}, errors.New(s.name + ": " + err.Error())
				}
				return Draw{Target: target, Why: "fiction/" + s.name + " · " + title}, nil
			}})
		}
	}
	if env.Crom != nil {
		srcs = append(srcs, source{2, func() (Draw, error) {
			u, err := env.Crom(ctx, "tale")
			if err != nil {
				return Draw{}, err
			}
			return Draw{Target: u, Why: "fiction/SCP tales"}, nil
		}})
	}
	if len(srcs) == 0 {
		return Draw{}, errors.New("no fiction source")
	}
	w := make([]int, len(srcs))
	for i, s := range srcs {
		w[i] = s.weight
	}
	var errs []string
	for n, i := range weightedOrder(w) {
		if n == 3 {
			break
		}
		d, err := srcs[i].draw()
		if err == nil {
			return d, nil
		}
		if ctx.Err() != nil {
			return Draw{}, ctx.Err()
		}
		errs = append(errs, err.Error())
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

func init() { families = append(families, weird{}, fiction{}) }
