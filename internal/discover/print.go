package discover

import (
	"context"
	"errors"
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
// returns a window onto its text. Not every item has a text of its own (a
// scan of pictures, a file that was never read) nor a passage worth showing,
// so the text is fetched here, and another item tried when it fails.
func printDraw(ctx context.Context, env Env, s printSource) (Draw, error) {
	var errs []string
	for range 4 {
		d, err := archivePick(ctx, env.Fetcher, s.query)
		if err != nil {
			return Draw{}, err
		}
		name, err := archiveTextFile(ctx, env.Fetcher, d.Identifier)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, d.Identifier+": "+err.Error())
			continue
		}
		label := d.label()
		target := windowTarget(archiveAPI+"/download/"+url.PathEscape(d.Identifier)+"/"+url.PathEscape(name), label)
		if _, err := buildWindow(ctx, env.Fetcher, target); err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, d.Identifier+": "+err.Error())
			continue
		}
		return Draw{Target: target, Why: "print/" + s.name + " · " + label}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}
