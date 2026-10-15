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
	"slices"
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
	// SitesPath is the owner's sites file (random.toml); "": none.
	SitesPath string
}

// families are the Deep Random sources; each family file adds its own.
var families []Family

const bagKey, lastKey = "discover:bag", "discover:last"

// nextFamily takes the next family from the shuffle bag: every family once
// per bag, never the same one twice in a row.
func nextFamily(db *store.DB, families []Family) (Family, error) {
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
	fresh := func() []string {
		// The bag is dealt in rounds: every family once, then those worth a
		// second place, and so on, each round shuffled. A family with
		// several sources takes several places, but not close together.
		var b []string
		for round := 1; ; round++ {
			var r []string
			for _, f := range families {
				if weightOf(f) >= round {
					r = append(r, f.Name())
				}
			}
			if len(r) == 0 {
				break
			}
			rand.Shuffle(len(r), func(i, j int) { r[i], r[j] = r[j], r[i] })
			b = append(b, r...)
		}
		spreadBag(b)
		return b
	}
	if len(bag) == 0 {
		bag = fresh()
	}
	// The family of the last page shown never comes right again (even when
	// the one in between failed), even when only its own places are left.
	last := db.Get(lastKey)
	if bag[0] == last && !slices.ContainsFunc(bag, func(n string) bool { return n != last }) {
		bag = append(bag, fresh()...)
	}
	notFirst(bag, last)
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
	fams := allFamilies(env)
	var errs []string
	for try := 0; try < 3; try++ {
		f, err := nextFamily(env.DB, fams)
		if err != nil {
			return Draw{}, nil, err
		}
		d, err := drawUnseen(ctx, env, f)
		if err == nil {
			var page *doc.Document
			if page, err = env.Load(ctx, d.Target); err == nil && thin(page) {
				err = errThin
			}
			if err == nil {
				d.Family = f.Name()
				_ = env.DB.Set(lastKey, d.Family)
				markSeen(env.DB, d.Target)
				head := []doc.Block{doc.Notice{Kind: "info", Text: "Deep random · " + d.Why + " · press x for another"}}
				// Ultan's note on the shelf it came from, and on a page
				// read before that it is kin to.
				note := ultan.Shelf(env.DB, d.Family, time.Now())
				if k, ok := ultan.Echo(env.DB, d.Target, page, time.Now()); ok {
					note = note.And(k)
				}
				head = append(head, note.Blocks(func(href, text string) int {
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

// thinWords is the least prose a drawn page shows: less is a page of
// pictures, a site's front door or a page that did not really open.
const thinWords = 80

var errThin = errors.New("the page has hardly any text")

// thin tells a page with less than thinWords words of prose (link text
// does not count: a page of links is not reading matter).
func thin(d *doc.Document) bool { return proseWords(d.Blocks, thinWords) < thinWords }

// proseWords counts the words of prose in blocks, stopping at enough.
func proseWords(bs []doc.Block, enough int) int {
	n := 0
	for _, b := range bs {
		if n >= enough {
			break
		}
		switch b := b.(type) {
		case doc.Paragraph:
			for _, s := range b.Text {
				if s.Link == 0 {
					n += len(strings.Fields(s.Text))
				}
			}
		case doc.Pre:
			n += len(strings.Fields(b.Text))
		case doc.Heading:
		case doc.List:
			for _, it := range b.Items {
				n += proseWords(it, enough-n)
			}
		case doc.Quote:
			n += proseWords(b.Blocks, enough-n)
		case doc.Collapsible:
			n += proseWords(b.Blocks, enough-n)
		case doc.Columns:
			for _, c := range b.Cols {
				n += proseWords(c, enough-n)
			}
		case doc.Table:
			for _, r := range b.Rows {
				for _, c := range r {
					for _, s := range c {
						if s.Link == 0 {
							n += len(strings.Fields(s.Text))
						}
					}
				}
			}
		}
	}
	return n
}

// drawUnseen draws from a family, drawing again (twice at most) when the page
// was shown lately; a shelf too small to offer anything new repeats itself.
func drawUnseen(ctx context.Context, env Env, f Family) (Draw, error) {
	var d Draw
	var err error
	for range 3 {
		if d, err = f.Draw(ctx, env); err != nil || ctx.Err() != nil || !wasSeen(env.DB, d.Target) {
			return d, err
		}
	}
	return d, nil
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
	case target == "w5f:discover/sites" || strings.HasPrefix(target, "w5f:discover/sites/") || strings.HasPrefix(target, "w5f:discover/sites?"):
		return sitesRoute(ctx, env, target)
	case strings.HasPrefix(target, "w5f:discover/window?"):
		return windowRoute(ctx, env, target)
	case strings.HasPrefix(target, "w5f:discover/bhl?"):
		return bhlRoute(ctx, env, target)
	case strings.HasPrefix(target, "w5f:discover/futures?"):
		return futuresRoute(ctx, env, target)
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
		return worldsDoc("")
	case strings.HasPrefix(target, "w5f:worlds/add?"), strings.HasPrefix(target, "w5f:worlds/remove?"):
		return worldsRoute(target)
	case target == "w5f:packet" || strings.HasPrefix(target, "w5f:packet/"):
		return packetRoute(ctx, target, env)
	}
	return nil, errors.New("unknown discovery address: " + target)
}
