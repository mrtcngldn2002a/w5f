package discover

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand/v2"

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
	return Draw{Target: w.URL, Why: fmt.Sprintf("weird/archived worlds · %s (tier %d)", w.Name, w.Tier)}, nil
}

// fiction: a top-of-all-time story from a few literary subreddits (with
// the owner's Reddit session), else a random SCP tale.
type fiction struct{}

var fictionSubs = []string{"WeirdLit", "LibraryOfShadows", "shortstories"}

func (fiction) Name() string { return "fiction" }
func (fiction) Draw(ctx context.Context, env Env) (Draw, error) {
	if env.RedditTop != nil {
		sub := pick(fictionSubs)
		if posts, err := env.RedditTop(ctx, sub); err == nil && len(posts) > 0 {
			return Draw{Target: pick(posts), Why: "fiction/r/" + sub + " · top of all time"}, nil
		}
	}
	if env.Crom == nil {
		return Draw{}, errors.New("no fiction source")
	}
	u, err := env.Crom(ctx, "tale")
	if err != nil {
		return Draw{}, err
	}
	return Draw{Target: u, Why: "fiction/SCP tales"}, nil
}

func init() { families = append(families, weird{}, fiction{}) }
