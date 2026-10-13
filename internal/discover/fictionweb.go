package discover

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// fictionWeb are the web places the fiction family draws from besides Reddit
// and the SCP tales (chosen with the owner, 2026-10-12).
type fictionPlace struct {
	name   string
	weight int
	pick   func(context.Context, Env) (target, title string, err error)
}

var fictionWeb = []fictionPlace{
	{"Nature Futures", 2, natureFuturesPick},
	{"Creepypasta.com", 2, wpPick("https://www.creepypasta.com")},
}

var natureFuturesList = "https://www.nature.com/nature/articles?type=futures&page="

// Nature Futures keeps some 1,240 stories on 62 pages of its list (twenty to a
// page, the newest first). Those since 2018 ("d41586-...", the first
// twenty-odd pages) are open. The older ones (ids like "516444a" or
// "35012720") are behind the journal's paywall, which shows a preview and
// "Buy this article"; they are read from the Internet Archive's copies
// (futures.go), and while the Archive is down only the open ones are drawn.
const (
	natureFuturesPages     = 62
	natureFuturesOpenPages = 24
	natureFuturesOpen      = "d41586-"
	natureFuturesTries     = 3 // old stories tried in one draw
)

// natureFuturesPick takes one of the short science fiction stories Nature
// prints on its last page: an open one, or an old one the Archive holds whole.
func natureFuturesPick(ctx context.Context, env Env) (string, string, error) {
	var errs []string
	archiveDown := false
	for range 3 {
		pages := natureFuturesPages
		if archiveDown {
			pages = natureFuturesOpenPages
		}
		gq, base, err := get(ctx, env.Fetcher, fmt.Sprintf("%s%d", natureFuturesList, 1+rand.IntN(pages)))
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
			if strings.HasPrefix(s.id, natureFuturesOpen) {
				return s.href, s.title, nil
			}
			if archiveDown || tried == natureFuturesTries {
				continue
			}
			tried++
			if _, err := futuresStoryFor(ctx, env.Fetcher, s.id); err != nil {
				if ctx.Err() != nil {
					return "", "", ctx.Err()
				}
				archiveDown = errors.Is(err, errArchiveDown)
				errs = append(errs, s.id+": "+err.Error())
				continue
			}
			return "w5f:discover/futures?" + url.Values{"id": {s.id}, "t": {s.title}}.Encode(), s.title, nil
		}
		errs = append(errs, "no story to read")
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}
