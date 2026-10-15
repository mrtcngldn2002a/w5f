package discover

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/BurntSushi/toml"

	"w5f/internal/doc"
)

//go:embed worlds.toml
var worldsTOML string

// World is a small or old fiction wiki.
type World struct {
	Name, URL, Family, Status, Note string
	Tier                            int
}

// Worlds is the embedded catalog.
func Worlds() ([]World, error) {
	var c struct {
		World []World `toml:"world"`
	}
	if _, err := toml.Decode(worldsTOML, &c); err != nil {
		return nil, err
	}
	return c.World, nil
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

func worldsDoc() (*doc.Document, error) {
	ws, err := Worlds()
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "Obscure & Archived Worlds", URL: "w5f:worlds", Origin: "local", Lang: "en"}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Small and old fiction wikis. Dead ones open from the Wayback Machine. Deep random (x) visits them now and then.", Style: doc.Italic}}})
	for _, fam := range []struct{ key, title string }{{"confic", "Containment fiction"}, {"backrooms", "Backrooms offshoots"}, {"fiction", "Other fiction"}} {
		var items [][]doc.Block
		for _, w := range ws {
			if w.Family != fam.key {
				continue
			}
			d.Links = append(d.Links, doc.Link{Href: w.URL, Text: w.Name})
			in := doc.Inline{{Text: w.Name, Style: doc.Bold, Link: len(d.Links)}, {Text: fmt.Sprintf("  · %s · tier %d", w.Status, w.Tier), Style: doc.Italic}}
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
	ws, err := Worlds()
	if err != nil || len(ws) == 0 {
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
