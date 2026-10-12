package discover

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
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

// natureFuturesPick takes one of the short science fiction stories Nature
// prints on its last page (some 1,200 of them, twenty to a page of the list).
func natureFuturesPick(ctx context.Context, env Env) (string, string, error) {
	var errs []string
	for range 3 {
		gq, base, err := get(ctx, env.Fetcher, fmt.Sprintf("%s%d", natureFuturesList, 1+rand.IntN(60)))
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		type story struct{ href, title string }
		var stories []story
		seen := map[string]bool{}
		gq.Find(`a[href^="/articles/"]`).Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			title := strings.Join(strings.Fields(a.Text()), " ")
			if title == "" || seen[href] || strings.Contains(href, "#") {
				return
			}
			seen[href] = true
			if u, err := base.Parse(href); err == nil {
				stories = append(stories, story{u.String(), title})
			}
		})
		if len(stories) > 0 {
			s := stories[rand.IntN(len(stories))]
			return s.href, s.title, nil
		}
		errs = append(errs, "no stories listed")
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}
