package discover

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"w5f/internal/fetch"
)

// The archive's world-religion list has a site-wide sidebar before its main
// links. Start with a religion's directory and stay inside it, so chronology,
// shop and other navigation pages cannot become the selected text.
func sacredTextsPick(ctx context.Context, f *fetch.Fetcher, start string) (string, string, error) {
	d, base, err := get(ctx, f, start)
	if err != nil {
		return "", "", err
	}
	var sections []string
	seen := map[string]bool{}
	d.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if strings.HasPrefix(href, "./") {
			return
		} // archive sidebar
		u, err := base.Parse(href)
		if err != nil || u.Host != base.Host || u.Scheme != base.Scheme || u.RawQuery != "" {
			return
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 2 || parts[1] != "index.htm" || reNotContent.MatchString(u.Path) || strings.Contains(parts[0], "shop") {
			return
		}
		u.Fragment = ""
		if !seen[u.String()] {
			sections = append(sections, u.String())
			seen[u.String()] = true
		}
	})
	if len(sections) == 0 {
		return "", "", errors.New("Sacred Texts religion index has no sections")
	}
	var errs []string
	for n, i := range rand.Perm(len(sections)) {
		if n == 3 {
			break
		}
		u, title, err := descend(ctx, f, sections[i], 5)
		if err == nil {
			return u, title, nil
		}
		errs = append(errs, err.Error())
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}
