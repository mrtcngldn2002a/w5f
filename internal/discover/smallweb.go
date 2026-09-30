package discover

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strings"

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
		func() (Draw, error) { return wibyDraw(ctx, env.Fetcher) },
		func() (Draw, error) {
			return linkFromPage(ctx, env.Smallweb, cosmosFeed, "smallweb/Gemini via Cosmos")
		},
		func() (Draw, error) {
			return linkFromPage(ctx, env.Smallweb, pick(gopherLists), "smallweb/Gopher via Floodgap")
		},
		func() (Draw, error) { return oohDraw(ctx, env.Fetcher) },
		func() (Draw, error) { return kagiDraw(ctx, env.Fetcher) },
		func() (Draw, error) { return oocitiesDraw(ctx, env.Fetcher) },
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
		if err != nil || (u.Scheme != "gemini" && u.Scheme != "gopher") || u.Host == base.Host {
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
