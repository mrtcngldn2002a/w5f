package discover

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// fictionWeb are the web places the fiction family draws from besides Reddit
// and the SCP tales (chosen with the owner, 2026-10-12; the professional
// magazines added 2026-10-30, their stories whole on their sites).
type fictionPlace struct {
	name   string
	weight int
	pick   func(context.Context, Env) (target, title string, err error)
}

var fictionWeb = []fictionPlace{
	{"Nature Futures", 2, natureFuturesPick},
	{"Creepypasta.com", 2, wpPick("https://www.creepypasta.com", "")},
	// Category 58 "text": the stories (1,279), not their podcasts.
	{"Clarkesworld", 2, wpPick("https://clarkesworldmagazine.com", "categories=58")},
	// Category 5 "Fiction" (1,521): the posts with a podcast hold the text too.
	{"Lightspeed", 2, wpPick("https://www.lightspeedmagazine.com", "categories=5")},
	// Category 1 "Fiction" (1,312), but a story in parts is cut in its posts.
	{"Strange Horizons", 2, wholeTitled(wpPick("https://strangehorizons.com", "categories=1"))},
}

// reInParts: a post that holds one part of a story ("Klokwerk's Heart (part 2
// of 2)").
var reInParts = regexp.MustCompile(`(?i)\bpart \d+ of \d+\b|\(part \d+\)`)

// wholeTitled draws again (up to three times) when the post drawn is a part of
// a story.
func wholeTitled(pick func(context.Context, Env) (string, string, error)) func(context.Context, Env) (string, string, error) {
	return func(ctx context.Context, env Env) (string, string, error) {
		var target, title string
		var err error
		for range 3 {
			if target, title, err = pick(ctx, env); err != nil || !reInParts.MatchString(title) {
				return target, title, err
			}
		}
		return "", "", errors.New("only parts of stories drawn: " + title)
	}
}

var natureFuturesList = "https://www.nature.com/nature/articles?type=futures&page="

// Nature Futures keeps some 1,240 stories on 62 pages of its list (twenty to a
// page, the newest first). All of them are behind the journal's paywall now,
// which shows the first paragraph and "Rent or buy this article" (those since
// 2018, "d41586-...", were open until about 2025; the source audit found
// 0 of 9 whole on 2026-10-30). They are read from the Internet Archive's
// copies of the years they were free (futures.go); while the Archive is down
// the shelf draws nothing.
const (
	natureFuturesPages       = 62
	natureFuturesRecentPages = 24 // the list pages of the stories since 2018
	natureFuturesRecent      = "d41586-"
	natureFuturesTries       = 3 // stories tried in one draw
)

// natureFuturesPick takes one of the short science fiction stories Nature
// prints on its last page, one the Archive holds whole.
func natureFuturesPick(ctx context.Context, env Env) (string, string, error) {
	var errs []string
	oldSlow := false
	for range 3 {
		// Most draws are of the stories since 2018: the Archive finds their
		// captures by their address. The old ones need a search over the
		// journal's folders, which the Archive often does not finish in
		// time; after one such failure a draw stays with the new ones.
		page := 1 + rand.IntN(natureFuturesRecentPages)
		if !oldSlow && rand.IntN(4) == 0 {
			page = natureFuturesRecentPages + 1 + rand.IntN(natureFuturesPages-natureFuturesRecentPages)
		}
		gq, base, err := get(ctx, env.Fetcher, fmt.Sprintf("%s%d", natureFuturesList, page))
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		type story struct{ href, title, id string }
		var stories []story
		seen := map[string]bool{}
		gq.Find(`a[href^="/articles/"]`).Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			title := strings.Join(strings.Fields(a.Text()), " ")
			id := strings.TrimPrefix(href, "/articles/")
			if title == "" || seen[href] || id == "" || strings.ContainsAny(id, "#?/") {
				return
			}
			seen[href] = true
			if u, err := base.Parse(href); err == nil {
				stories = append(stories, story{u.String(), title, id})
			}
		})
		if len(stories) == 0 {
			errs = append(errs, "no stories listed")
			continue
		}
		tried := 0
		for _, i := range rand.Perm(len(stories)) {
			s := stories[i]
			if tried == natureFuturesTries {
				break
			}
			tried++
			if _, err := futuresStoryFor(ctx, env.Fetcher, s.id); err != nil {
				if ctx.Err() != nil {
					return "", "", ctx.Err()
				}
				if errors.Is(err, errArchiveDown) {
					if strings.HasPrefix(s.id, natureFuturesRecent) {
						return "", "", err
					}
					oldSlow = true
					errs = append(errs, s.id+": "+err.Error())
					break
				}
				errs = append(errs, s.id+": "+err.Error())
				continue
			}
			return "w5f:discover/futures?" + url.Values{"id": {s.id}, "t": {s.title}}.Encode(), s.title, nil
		}
		errs = append(errs, "no story to read")
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}
