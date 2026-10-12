package discover

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/url"
	"strings"
)

func init() { families = append(families, printFamily{}) }

// printFamily: old magazines, zines and newspapers, scanned and kept by the
// Internet Archive (chosen with the owner, 2026-10-12). A scan is hundreds of
// pages, so the draw opens a window onto it (see window.go), not the whole.
type printFamily struct{}

type printSource struct {
	name   string
	query  string
	weight int
}

var printSources = []printSource{
	{"pulp magazines", `collection:pulpmagazinearchive AND mediatype:texts`, 3},
	{"zines", `collection:zines AND mediatype:texts`, 2},
	{"the magazine rack", `collection:magazine_rack AND mediatype:texts AND language:eng AND date:[1850-01-01 TO 1960-12-31]`, 2},
	// Microfilm scans: dirty text, so the window's readability check weeds out most of it.
	{"old newspapers", `collection:newspapers AND mediatype:texts AND language:eng AND date:[1800-01-01 TO 1945-12-31]`, 1},
}

func (printFamily) Name() string { return "print" }
func (printFamily) sources() int { return len(printSources) }

func (printFamily) Draw(ctx context.Context, env Env) (Draw, error) {
	w := make([]int, len(printSources))
	for i, s := range printSources {
		w[i] = s.weight
	}
	var errs []string
	for n, i := range weightedOrder(w) {
		if n == 3 {
			break
		}
		s := printSources[i]
		d, err := printDraw(ctx, env, s)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		return d, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// printDraw takes a random item of the source's Internet Archive query and
// returns a window onto its text.
func printDraw(ctx context.Context, env Env, s printSource) (Draw, error) {
	n, _, err := archiveSearch(ctx, env.Fetcher, s.query, 1, 1)
	if err != nil {
		return Draw{}, err
	}
	if n == 0 {
		return Draw{}, errors.New("nothing found")
	}
	_, docs, err := archiveSearch(ctx, env.Fetcher, s.query, 1, 1+rand.IntN(min(n, 10000)))
	if err != nil || len(docs) == 0 {
		return Draw{}, errors.New("no item on that page")
	}
	d := docs[0]
	id := url.PathEscape(d.Identifier)
	label := d.label()
	return Draw{Target: windowTarget(archiveAPI+"/download/"+id+"/"+id+"_djvu.txt", label),
		Why: "print/" + s.name + " · " + label}, nil
}
