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

// english leaves out what is catalogued in another language (a third of the
// zines are: they would only fail the draw); the many items with no language
// named stay.
const english = `(language:eng OR language:English OR (NOT language:[* TO *]))`

var printSources = []printSource{
	{"pulp magazines", `collection:pulpmagazinearchive AND mediatype:texts AND ` + english, 3},
	{"zines", `collection:zines AND mediatype:texts AND ` + english, 2},
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
// it when it is small, else span bytes from a random place, not in the first
// or last twentieth (covers, advertisements, indexes).
func pagesWindowRange(size, span int) (from, to int) {
	if room := size - size/10 - span; room > 0 {
		from = size/20 + rand.IntN(room)
		return from, from + span - 1
	}
	return 0, size - 1
}

// printDraw takes a random item of the source's Internet Archive query and
// returns a window onto its text. Not every item has a text of its own (a
// scan of pictures, a file that was never read) nor a passage worth showing,
// so the window is built here, and another item tried when it fails. The
// window is cut from a few pages of the item's DjVu XML, the columns put in
// order from where its words stood (layout.go): the Archive's plain text
// reads a page across its columns, so an item with no XML is passed over.
func printDraw(ctx context.Context, env Env, s printSource) (Draw, error) {
	var errs []string
	tried := map[string]bool{} // an item found wanting is not tried again in the same draw
	for picks, items := 0, 0; items < 4 && picks < 12; picks++ {
		d, err := archivePick(ctx, env.Fetcher, s.query)
		if err != nil {
			return Draw{}, err
		}
		if tried[d.Identifier] {
			continue
		}
		tried[d.Identifier] = true
		items++
		_, xml, xmlSize, err := archiveFiles(ctx, env.Fetcher, d.Identifier)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, d.Identifier+": "+err.Error())
			continue
		}
		label := d.label()
		base := archiveAPI + "/download/" + url.PathEscape(d.Identifier) + "/"
		if xml == "" { // the Archive's plain text reads a page across its columns
			errs = append(errs, d.Identifier+": no layout file")
			continue
		}
		span := pagesRange
		for range 2 {
			from, to := pagesWindowRange(xmlSize, span)
			target := windowPagesTarget(base+url.PathEscape(xml), label, from, to, xmlSize)
			_, err := buildWindow(ctx, env.Fetcher, target)
			if err == nil {
				return Draw{Target: target, Why: "print/" + s.name + " · " + label}, nil
			}
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, d.Identifier+": "+err.Error())
			if errors.Is(err, errDirtyScan) || errors.Is(err, errFetch) {
				break // another place in it will not do better: another item
			}
			if errors.Is(err, errNoPage) {
				span *= 2 // the pages are bigger than the range
			}
		}
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}
