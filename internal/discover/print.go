package discover

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strings"
)

func init() { families = append(families, printFamily{}) }

// printFamily: old print, whole pieces of it. It was the Internet Archive's
// scanned magazines, zines and newspapers, opened at a window of a page or
// two; the owner took the Archive out (2026-10-30: half-pieces and bad OCR)
// for sources whose pieces are whole: Project Gutenberg's short works and
// the stories and articles of its anthologies and old magazine issues
// (gutenberg.go), Wikisource's short stories and essays, and the Public
// Domain Review's essays.
type printFamily struct{}

var printSources = []litSource{
	{"Project Gutenberg", 3, gutenbergShortPick},
	{"Gutenberg anthologies and magazines", 3, gutenbergPiecePick},
	{"Wikisource", 2, wikisourcePick},
	{"Public Domain Review", 1, sitemapPick("https://publicdomainreview.org/sitemap-index.xml", `sitemap-\d+\.xml$`, `/essay/`, ``)},
}

func (printFamily) Name() string { return "print" }
func (printFamily) sources() int { return len(printSources) }

func (printFamily) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawWeighted(ctx, env, "print", printSources)
}

// wikisourceShelves are the Wikisource categories drawn from, with their
// weights: a random page of the category itself (not of its subcategories).
// Plain random lands on court reports and census pages.
var wikisourceShelves = []struct {
	cat    string
	weight int
}{
	{"Short_stories", 4}, // 2,397 pages (2026-10-30)
	{"Essays", 2},        // 482
	{"Horror_short_stories", 1},
	{"Ghost_stories", 1},
	{"Science_fiction_short_stories", 1},
	{"Fantasy_short_stories", 1},
}

var wikisourceRandom = "https://en.wikisource.org/wiki/Special:RandomInCategory/"

func wikisourcePick(ctx context.Context, env Env) (string, string, error) {
	w := make([]int, len(wikisourceShelves))
	for i, s := range wikisourceShelves {
		w[i] = s.weight
	}
	var errs []string
	for n, i := range weightedOrder(w) {
		if n == 3 {
			break
		}
		target, err := resolveRandom(ctx, env.Fetcher, wikisourceRandom+wikisourceShelves[i].cat)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			errs = append(errs, err.Error())
			continue
		}
		return target, wikisourceTitle(target), nil
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

// wikisourceTitle: "Mosses_from_an_Old_Manse/The_Artist_of_the_Beautiful" is
// "The Artist of the Beautiful (Mosses from an Old Manse)".
func wikisourceTitle(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	name, _ := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/wiki/"))
	name = strings.ReplaceAll(name, "_", " ")
	if dir, base := path.Split(name); dir != "" {
		return base + " (" + strings.TrimSuffix(dir, "/") + ")"
	}
	return name
}
