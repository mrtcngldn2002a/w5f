// Package discover is W5F's deliberate randomness: Deep Random draws a page
// from several families of sources, the Daily Packet gathers a small issue
// each day, and the archived worlds catalog keeps small or dead fiction wikis
// within reach.
package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/smallweb"
	"w5f/internal/store"
	"w5f/internal/ultan"
)

// Draw is one random pick: where to go, from which family, and why.
type Draw struct {
	Target string
	Family string
	Why    string // "textfiles/occult · chaos.txt"
}

// Family is a kind of source Deep Random draws from.
type Family interface {
	Name() string
	Draw(ctx context.Context, env Env) (Draw, error)
}

// Env carries what discovery needs.
type Env struct {
	Fetcher *fetch.Fetcher
	DB      *store.DB
	// Load opens a target like the reader does (web page, gemini, w5f:).
	Load func(ctx context.Context, target string) (*doc.Document, error)
	// Crom returns a random page address of a preset ("scp", "tale", "wl", "backrooms").
	Crom func(ctx context.Context, preset string) (string, error)
	// RedditTop lists top-of-all-time post addresses of a subreddit (owner's session); nil without one.
	RedditTop func(ctx context.Context, sub string) ([]string, error)
	// Smallweb reads Gemini and Gopher.
	Smallweb smallweb.Env
	// Shelves maps the Periodicals catalog's feeds to their shelves; the
	// Daily Packet takes only these feeds' items (nil: any unread item).
	Shelves map[string]string
}

// families are the Deep Random sources; each family file adds its own.
var families []Family

const bagKey, lastKey = "discover:bag", "discover:last"

// nextFamily takes the next family from the shuffle bag: every family once
// per bag, never the same one twice in a row.
func nextFamily(db *store.DB) (Family, error) {
	if len(families) == 0 {
		return nil, errors.New("no discovery sources")
	}
	byName := map[string]Family{}
	for _, f := range families {
		byName[f.Name()] = f
	}
	var bag []string
	_ = json.Unmarshal([]byte(db.Get(bagKey)), &bag)
	for len(bag) > 0 && byName[bag[0]] == nil {
		bag = bag[1:]
	}
	if len(bag) == 0 {
		for _, f := range families {
			bag = append(bag, f.Name())
		}
		rand.Shuffle(len(bag), func(i, j int) { bag[i], bag[j] = bag[j], bag[i] })
	}
	// The family of the last page shown never comes right again (even when
	// the one in between failed).
	if last := db.Get(lastKey); len(bag) > 1 && bag[0] == last {
		bag[0], bag[1] = bag[1], bag[0]
	}
	f := byName[bag[0]]
	b, _ := json.Marshal(bag[1:])
	_ = db.Set(bagKey, string(b))
	return f, nil
}

// Next draws a page: up to three families are tried, a failing one is
// skipped. The page opens with its "why you are here" line.
func Next(ctx context.Context, env Env) (Draw, *doc.Document, error) {
	// One bot-check helper request per draw (up to a minute); the other
	// shelves are tried without it.
	ctx = fetch.SolverOnce(ctx)
	var errs []string
	for try := 0; try < 3; try++ {
		f, err := nextFamily(env.DB)
		if err != nil {
			return Draw{}, nil, err
		}
		d, err := f.Draw(ctx, env)
		if err == nil {
			var page *doc.Document
			if page, err = env.Load(ctx, d.Target); err == nil {
				d.Family = f.Name()
				_ = env.DB.Set(lastKey, d.Family)
				head := []doc.Block{doc.Notice{Kind: "info", Text: "Deep random · " + d.Why + " · press x for another"}}
				// Ultan's note on the shelf it came from.
				head = append(head, ultan.Shelf(env.DB, d.Family, time.Now()).Blocks(func(href, text string) int {
					page.Links = append(page.Links, doc.Link{Href: href, Text: text})
					return len(page.Links)
				})...)
				page.Blocks = append(head, page.Blocks...)
				return d, page, nil
			}
		}
		errs = append(errs, fmt.Sprintf("%s: %v", f.Name(), err))
	}
	return Draw{}, nil, errors.New("Deep random found nothing this time (" + strings.Join(errs, "; ") + ")")
}

// IsTarget reports discovery addresses.
func IsTarget(t string) bool {
	return strings.HasPrefix(t, "w5f:discover/") || t == "w5f:packet" || strings.HasPrefix(t, "w5f:packet/") ||
		t == "w5f:worlds" || strings.HasPrefix(t, "w5f:worlds/") || t == "w5f:almanac" || strings.HasPrefix(t, "w5f:almanac/")
}

// Route builds the page of a discovery address.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	switch {
	case target == "w5f:discover/tarot", strings.HasPrefix(target, "w5f:discover/tarot/"):
		return tarotRoute(ctx, env, target)
	case target == "w5f:discover/iching", strings.HasPrefix(target, "w5f:discover/iching/"):
		return ichingRoute(ctx, env, target)
	case target == "w5f:almanac":
		return almanacDoc(ctx, env, time.Now().Format("01-02"))
	case strings.HasPrefix(target, "w5f:almanac/"):
		return almanacDoc(ctx, env, strings.TrimPrefix(target, "w5f:almanac/"))
	case target == "w5f:discover/random":
		_, page, err := Next(ctx, env)
		return page, err
	case directoryDraws[strings.TrimPrefix(target, "w5f:discover/")].draw != nil:
		dd := directoryDraws[strings.TrimPrefix(target, "w5f:discover/")]
		d, err := dd.draw(ctx, env.Fetcher)
		if err != nil {
			return nil, err
		}
		page, err := env.Load(ctx, d.Target)
		if err != nil {
			return nil, err
		}
		_, detail, _ := strings.Cut(d.Why, " · ")
		page.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: dd.label + " · " + detail + " · open it again from Small Web for another"}}, page.Blocks...)
		return page, nil
	case target == "w5f:worlds":
		return worldsDoc()
	case target == "w5f:packet" || strings.HasPrefix(target, "w5f:packet/"):
		return packetRoute(ctx, target, env)
	}
	return nil, errors.New("unknown discovery address: " + target)
}
