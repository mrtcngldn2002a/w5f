package discover

import (
	"context"
	"math/rand/v2"
	"net/url"
	"path"
	"regexp"
	"strings"

	"w5f/internal/fetch"
)

// Theoi's own pages are of two kinds worth reading: an entry of its
// encyclopedia (a god, a nymph, a monster, with what the ancient authors say
// of them) and a book of its classical texts library. A walk from the front
// page ended on its galleries, its letter indexes and its family pages
// ("Heroes, Kings & Villians") as often (P0 audit, 2026-10-30), so the draw
// takes one of the two kinds from its lists: an entry three times in five.
var (
	reTheoiLetter = regexp.MustCompile(`^/greek-mythology/encyclopedia-[a-z]+\.html$`)
	// An entry lives in its kind's folder ("/Nymphe/NympheKalypso.html").
	reTheoiEntry = regexp.MustCompile(`^/[A-Z][A-Za-z]+/[A-Za-z0-9]+\.html$`)
	reTheoiText  = regexp.MustCompile(`^/Text/[A-Za-z0-9]+\.html$`)
	// reBookNumber: a text's book, after its name ("HomerIliad5", "DiodorusSiculus4A").
	reBookNumber = regexp.MustCompile(`\d+[A-Z]?$`)
)

func theoiPick(ctx context.Context, f *fetch.Fetcher, start string) (string, string, error) {
	if rand.IntN(5) < 3 {
		letter, _, err := listPick(ctx, f, start+"greek-mythology/encyclopedia-a.html", "a[href]", func(u *url.URL) bool {
			return reTheoiLetter.MatchString(u.Path)
		})
		if err != nil {
			return "", "", err
		}
		return listPick(ctx, f, letter, "a[href]", func(u *url.URL) bool {
			return reTheoiEntry.MatchString(u.Path) && !strings.HasPrefix(u.Path, "/Text/") && !strings.Contains(u.Path, "Galler")
		})
	}
	work, title, err := listPick(ctx, f, start+"Library.html", "a[href]", func(u *url.URL) bool {
		return reTheoiText.MatchString(u.Path)
	})
	if err != nil {
		return "", "", err
	}
	// A text in many books lists them all on each of its pages.
	stem := reBookNumber.ReplaceAllString(strings.TrimSuffix(path.Base(work), ".html"), "")
	reBook := regexp.MustCompile(`^/Text/` + regexp.QuoteMeta(stem) + `(\d+[A-Z]?)?\.html$`)
	book, _, err := listPick(ctx, f, work, "a[href]", func(u *url.URL) bool { return reBook.MatchString(u.Path) })
	if err != nil {
		return work, title, nil
	}
	return book, title, nil
}
