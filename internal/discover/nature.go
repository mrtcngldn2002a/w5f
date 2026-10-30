package discover

import (
	"context"
	_ "embed"
	"errors"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

func init() { families = append(families, nature{}) }

// nature: old natural history on the curious side (chosen with the owner,
// 2026-10-12): a whole article of a Biodiversity Heritage Library journal, or
// an article of the Popular Science Monthly (1872-1915).
type nature struct{}

type natureSource struct {
	name   string
	weight int
	pick   func(context.Context, Env) (target, title string, err error)
}

var natureSources = []natureSource{
	{"Biodiversity Heritage Library", 3, bhlArticlePick},
	{"Popular Science Monthly", 2, psmPick},
}

func (nature) Name() string { return "nature" }
func (nature) sources() int { return len(natureSources) }

func (nature) Draw(ctx context.Context, env Env) (Draw, error) {
	w := make([]int, len(natureSources))
	for i, s := range natureSources {
		w[i] = s.weight
	}
	var errs []string
	for _, i := range weightedOrder(w) {
		s := natureSources[i]
		target, title, err := s.pick(ctx, env)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		why := "nature/" + s.name
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// --- Biodiversity Heritage Library, from its open data (CC0, on a public S3
// bucket: no bot check). The list of books is embedded (scripts/bhl_index.py
// makes it); the OCR of every page of a book is one small text file there.

//go:embed bhl_curious.tsv
var bhlTSV string

var bhlBucket = "https://bhl-open-data.s3.us-east-2.amazonaws.com"

type bhlItem struct {
	tier, id, year int
	title          string
}

var (
	bhlOnce  sync.Once
	bhlItems [3][]bhlItem // by tier
)

func loadBHL() {
	for _, line := range strings.Split(bhlTSV, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "\t", 4)
		if len(f) != 4 {
			continue
		}
		tier, e1 := strconv.Atoi(f[0])
		id, e2 := strconv.Atoi(f[1])
		year, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil || tier < 1 || tier > 2 {
			continue
		}
		bhlItems[tier] = append(bhlItems[tier], bhlItem{tier, id, year, f[3]})
	}
}

// pickBHL takes a book of the curious tier half of the time, else of the
// broad one (natural history and travel).
func pickBHL() (bhlItem, bool) {
	bhlOnce.Do(loadBHL)
	tier := 2
	if rand.IntN(2) == 0 {
		tier = 1
	}
	if len(bhlItems[tier]) == 0 {
		tier = 3 - tier
	}
	if len(bhlItems[tier]) == 0 {
		return bhlItem{}, false
	}
	return bhlItems[tier][rand.IntN(len(bhlItems[tier]))], true
}

// --- The Popular Science Monthly on Wikisource: a random article of a random
// volume (87 volumes, each its own category).

var (
	psmRandom = "https://en.wikisource.org/wiki/Special:RandomInCategory/Popular_Science_Monthly_Volume_"
	// Pages of a volume that are not articles to read.
	psmSkip = map[string]bool{"Notes": true, "Contents": true, "Index": true, "Title_Page": true, "Preface": true}
)

func psmPick(ctx context.Context, env Env) (string, string, error) {
	var errs []string
	for range 4 {
		target, err := resolveRandom(ctx, env.Fetcher, psmRandom+strconv.Itoa(1+rand.IntN(87)))
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		u, err := url.Parse(target)
		if err != nil {
			continue
		}
		// .../Popular_Science_Monthly/Volume_30/April_1887/The_History_of_a_Delusion
		parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/wiki/"), "/")
		if len(parts) < 4 || psmSkip[parts[len(parts)-1]] {
			errs = append(errs, "not an article: "+u.Path)
			continue
		}
		name, _ := url.PathUnescape(parts[len(parts)-1])
		month, _ := url.PathUnescape(parts[2])
		return target, strings.ReplaceAll(name, "_", " ") + " (" + strings.ReplaceAll(month, "_", " ") + ")", nil
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}
