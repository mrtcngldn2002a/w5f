package discover

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"w5f/internal/fetch"
)

// Britannica has category indexes, not a random-article endpoint. Select
// actual encyclopedia articles, excluding quizzes, galleries and account pages.
func britannicaPick(ctx context.Context, f *fetch.Fetcher, start string) (string, string, error) {
	d, base, err := get(ctx, f, start)
	if err != nil {
		return "", "", err
	}
	d.Find("nav, header, footer, script, style").Remove()
	var candidates []string
	seen := map[string]bool{}
	d.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		u, err := base.Parse(a.AttrOr("href", ""))
		if err != nil || u.Host != base.Host || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" {
			return
		}
		if !britannicaArticlePath(u.Path) {
			return
		}
		u.Fragment = ""
		if !seen[u.String()] {
			candidates = append(candidates, u.String())
			seen[u.String()] = true
		}
	})
	if len(candidates) == 0 {
		return "", "", errors.New("Britannica index has no article links")
	}
	var errs []string
	for n, i := range rand.Perm(len(candidates)) {
		if n == 3 {
			break
		}
		article, final, err := get(ctx, f, candidates[i])
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		// Only accept an article page; a subscription/login redirect is not a pick.
		if final.Host != base.Host || !britannicaArticlePath(final.Path) {
			errs = append(errs, "Britannica redirected away from an article")
			continue
		}
		body := article.Find("article, #article-content, .article-body").First()
		if len(strings.TrimSpace(body.Text())) < 200 {
			errs = append(errs, "Britannica returned no readable article text")
			continue
		}
		title := strings.TrimSpace(article.Find("h1").First().Text())
		if title == "" {
			title = strings.TrimSpace(article.Find("title").Text())
		}
		return final.String(), title, nil
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

func britannicaArticlePath(p string) bool {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 2 || parts[1] == "" {
		return false
	}
	switch parts[0] {
	case "topic", "biography", "science", "art", "place", "event", "animal", "plant", "technology", "sports":
		return true
	}
	return false
}
