package discover

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
	"w5f/internal/smallweb"
)

// smallwebFamily: Wiby's "surprise me", a capsule Antenna saw lately, or a
// hole from Floodgap's list of Gopher servers.
type smallwebFamily struct{}

func init() { families = append(families, smallwebFamily{}) }

var (
	wibySurprise = "https://wiby.me/surprise/"
	cosmosFeed   = "gemini://skyjake.fi/~Cosmos/" // Antenna (warmedal.se) is gone
	gopherLists  = []string{"gopher://gopher.floodgap.com/1/new", "gopher://gopher.floodgap.com/1/world"}
	reRefresh    = regexp.MustCompile(`(?i)<meta[^>]+http-equiv=["']?refresh["']?[^>]*content=["'][^"']*url=['"]?([^'">]+)`)
)

func (smallwebFamily) Name() string { return "smallweb" }

func (smallwebFamily) Draw(ctx context.Context, env Env) (Draw, error) {
	var errs []string
	tries := []func() (Draw, error){
		func() (Draw, error) { return fuller(ctx, env.Fetcher)(wibyDraw(ctx, env.Fetcher)) },
		func() (Draw, error) {
			return linkFromPage(ctx, env.Smallweb, cosmosFeed, "smallweb/Gemini via Cosmos")
		},
		func() (Draw, error) {
			return linkFromPage(ctx, env.Smallweb, pick(gopherLists), "smallweb/Gopher via Floodgap")
		},
		func() (Draw, error) { return oohDraw(ctx, env.Fetcher) },
		func() (Draw, error) { return kagiDraw(ctx, env.Fetcher) },
		func() (Draw, error) { return fuller(ctx, env.Fetcher)(oocitiesDraw(ctx, env.Fetcher)) },
	}
	for _, i := range rand.Perm(len(tries)) {
		d, err := tries[i]()
		if err == nil {
			return d, nil
		}
		errs = append(errs, err.Error())
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// directoryDraws are the small web pickers the Small Web page opens directly
// (w5f:discover/<name>), each landing on a new page every time.
var directoryDraws = map[string]struct {
	label string
	draw  func(context.Context, *fetch.Fetcher) (Draw, error)
}{
	"wiby":      {"Wiby surprise", wibyDraw},
	"ooh":       {"ooh.directory random blog", oohDraw},
	"kagi":      {"Kagi Small Web", kagiDraw},
	"geocities": {"GeoCities archive", oocitiesDraw},
}

// fullerLook is how many of a small site's other pages a draw looks at when
// the page drawn has too little to read.
const fullerLook = 3

// fuller: a small site's front page is often a welcome and a list of its
// pages (the P0 audit's Wiby and GeoCities draws: most under 100 words). A
// draw whose page has less than the shelf's bar of its own words (link text
// left out) moves to one of up to three pages of the same folder that has
// it; none has, and the draw fails, so the shelf tries another source. The
// Small Web page's own surprise buttons go to the site as it is.
func fuller(ctx context.Context, f *fetch.Fetcher) func(Draw, error) (Draw, error) {
	return func(d Draw, err error) (Draw, error) {
		if err != nil {
			return d, err
		}
		bar := ShelfBar("smallweb")
		gq, base, err := get(ctx, f, d.Target)
		if err != nil {
			return Draw{}, err
		}
		if ownWords(gq) >= bar {
			return d, nil
		}
		dir := dirOf(base.Path)
		var pages []string
		seen := map[string]bool{base.String(): true}
		gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			u, err := base.Parse(strings.TrimSpace(a.AttrOr("href", "")))
			if err != nil || u.Host != base.Host || u.RawQuery != "" || !strings.HasPrefix(u.Path, dir) ||
				skipExt[strings.ToLower(path.Ext(u.Path))] || reNotContent.MatchString(u.Path) {
				return
			}
			u.Fragment = ""
			if !seen[u.String()] {
				seen[u.String()] = true
				pages = append(pages, u.String())
			}
		})
		for n, i := range rand.Perm(len(pages)) {
			if n == fullerLook {
				break
			}
			page, at, err := get(ctx, f, pages[i])
			if err != nil || ownWords(page) < bar {
				continue
			}
			d.Target = at.String()
			if title := strings.Join(strings.Fields(page.Find("title").First().Text()), " "); title != "" {
				d.Why += " — " + title
			}
			return d, nil
		}
		return Draw{}, errors.New(SourceOf(d) + ": " + d.Target + " has little to read")
	}
}

// ownWords counts a page's words that are not link text.
func ownWords(gq *goquery.Document) int {
	gq.Find("script, style, noscript").Remove()
	body := gq.Find("body")
	if body.Length() == 0 {
		body = gq.Selection
	}
	links := 0
	body.Find("a").Each(func(_ int, a *goquery.Selection) { links += len(strings.Fields(a.Text())) })
	return len(strings.Fields(body.Text())) - links
}

// wibyDraw follows Wiby's surprise page (a meta refresh to a random site).
func wibyDraw(ctx context.Context, f *fetch.Fetcher) (Draw, error) {
	u, _ := url.Parse(wibySurprise)
	resp, err := f.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return Draw{}, err
	}
	m := reRefresh.FindSubmatch(resp.Body)
	if m == nil {
		return Draw{}, errors.New("Wiby: no surprise found")
	}
	target, err := u.Parse(strings.TrimSpace(string(m[1])))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return Draw{}, errors.New("Wiby: bad surprise address")
	}
	return Draw{Target: target.String(), Why: "smallweb/Wiby · " + target.Host}, nil
}

// linkFromPage opens a small web page and picks one of its links that goes
// elsewhere (another capsule or hole).
func linkFromPage(ctx context.Context, env smallweb.Env, page, why string) (Draw, error) {
	d, err := smallweb.Load(ctx, env, page)
	if err != nil {
		return Draw{}, err
	}
	base, _ := url.Parse(page)
	var links []string
	for _, l := range d.Links {
		u, err := url.Parse(l.Href)
		if err != nil || (u.Scheme != "gemini" && u.Scheme != "gopher") || u.Hostname() == base.Hostname() {
			continue
		}
		links = append(links, l.Href)
	}
	if len(links) == 0 {
		return Draw{}, errors.New(why + ": no links")
	}
	target := pick(links)
	tu, _ := url.Parse(target)
	return Draw{Target: target, Why: why + " · " + tu.Host}, nil
}
