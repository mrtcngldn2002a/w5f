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

// pagesRange is how much of an item's DjVu XML a draw reads: a few pages.
const pagesRange = 3 << 20 / 2

// pagesWindowRange is the bytes of an XML of size bytes a draw reads: all of
// it when it is small, else pagesRange bytes from a random place, not in the
// first or last twentieth (covers, advertisements, indexes).
func pagesWindowRange(size int) (from, to int) {
	if span := size - size/10 - pagesRange; span > 0 {
		from = size/20 + rand.IntN(span)
		return from, from + pagesRange - 1
	}
	return 0, size - 1
}

// printDraw takes a random item of the source's Internet Archive query and
// returns a window onto its text. Not every item has a text of its own (a
// scan of pictures, a file that was never read) nor a passage worth showing,
// so the text is fetched here, and another item tried when it fails. Where
// the item has its DjVu XML the window is cut from a few pages of it, the
// columns put in order from where its words stood (layout.go); otherwise
// from the Archive's plain text, which reads a page across its columns.
func printDraw(ctx context.Context, env Env, s printSource) (Draw, error) {
	var errs []string
	for range 4 {
		d, err := archivePick(ctx, env.Fetcher, s.query)
		if err != nil {
			return Draw{}, err
		}
		text, xml, xmlSize, err := archiveFiles(ctx, env.Fetcher, d.Identifier)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, d.Identifier+": "+err.Error())
			continue
		}
		label := d.label()
		base := archiveAPI + "/download/" + url.PathEscape(d.Identifier) + "/"
		var targets []string
		if xml != "" {
			for range 2 {
				from, to := pagesWindowRange(xmlSize)
				targets = append(targets, windowPagesTarget(base+url.PathEscape(xml), label, from, to, xmlSize))
			}
		} else if text != "" {
			targets = append(targets, windowTarget(base+url.PathEscape(text), label))
		}
		for _, target := range targets {
			if _, err := buildWindow(ctx, env.Fetcher, target); err != nil {
				if ctx.Err() != nil {
					return Draw{}, ctx.Err()
				}
				errs = append(errs, d.Identifier+": "+err.Error())
				continue
			}
			return Draw{Target: target, Why: "print/" + s.name + " · " + label}, nil
		}
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}
