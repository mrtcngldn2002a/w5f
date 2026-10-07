package discover

import (
	"context"
	"errors"
	"math/rand/v2"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

func init() { families = append(families, underground{}) }

// underground: the hacker and counterculture press, chosen by the owner
// (2026-10-04, "phrack, cdc, totse, erowid, the anarchist library"). Like
// textfiles, nothing in it is filtered by content.
type underground struct{}

type undergroundSource struct {
	name string
	pick func(context.Context, Env) (string, string, error)
}

func siteSource(s Site) undergroundSource {
	return undergroundSource{s.Name, func(ctx context.Context, env Env) (string, string, error) {
		return drawSite(ctx, env, s)
	}}
}

var undergroundSources = []undergroundSource{
	// Every issue since 1985 as plain text: /issues/<n>/<article>.txt.
	{"Phrack", phrackPick},
	// The t-files, numbered, at /cDc_files/cDc-NNNN.html.
	siteSource(Site{Name: "Cult of the Dead Cow", URL: "https://cultdeadcow.com/cDc_files/", How: "walk", Depth: 2}),
	// Its own random-text address.
	siteSource(Site{Name: "The Anarchist Library", URL: "https://theanarchistlibrary.org/random", How: "random"}),
	// Behind Cloudflare's check: the bot-check helper opens it when one is running.
	{"Erowid", erowidPick},
	// Closed since 2007 or so; read from the Internet Archive's copies.
	{"Totse", totsePick},
}

func (underground) Name() string { return "underground" }
func (underground) sources() int { return len(undergroundSources) }

func (underground) Draw(ctx context.Context, env Env) (Draw, error) {
	var errs []string
	for n, i := range rand.Perm(len(undergroundSources)) {
		if n == 3 {
			break
		}
		s := undergroundSources[i]
		target, title, err := s.pick(ctx, env)
		if err != nil {
			if ctx.Err() != nil {
				return Draw{}, ctx.Err()
			}
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		why := "underground/" + s.name
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

var rePhrack = regexp.MustCompile(`/issues/(\d+)/(\d+)\.txt$`)

func phrackPick(ctx context.Context, env Env) (string, string, error) {
	target, _, err := drawSite(ctx, env, Site{URL: "https://archives.phrack.org/issues/", How: "walk", Depth: 3})
	if err != nil {
		return "", "", err
	}
	title := ""
	if m := rePhrack.FindStringSubmatch(target); m != nil {
		title = "issue " + m[1] + ", file " + m[2]
	}
	return target, title, nil
}

// erowidVaults are the substance vaults the Erowid library lists on its front
// page (erowid.org/library/, 2026-10-07); the walk stays inside one, so it
// never lands in the bookstore.
var erowidVaults = []string{"plants/amanitas", "plants/cacti", "plants/cannabis", "plants/coca", "plants/mushrooms", "plants/salvia",
	"plants/tobacco", "chemicals/amphetamines", "chemicals/dxm", "chemicals/ghb", "chemicals/ketamine", "chemicals/lsd",
	"chemicals/mdma", "chemicals/nitrous", "pharms/alprazolam", "pharms/bupropion", "pharms/diazepam", "pharms/fluoxetine",
	"pharms/hydrocodone", "pharms/methylphenidate", "pharms/paroxetine", "herbs/calamus", "herbs/damiana", "herbs/foxglove",
	"herbs/ginseng", "herbs/milk_thistle", "herbs/pennyroyal", "herbs/valerian", "smarts/adrafinil", "smarts/dmae",
	"smarts/ginkgo", "smarts/hydergine", "smarts/melatonin", "smarts/piracetam", "smarts/tryptophan", "animals/toads",
	"animals/phyllomedusa"}

func erowidPick(ctx context.Context, env Env) (string, string, error) {
	v := pick(erowidVaults)
	scope := "/" + v + "/"
	target, title, err := descendIn(ctx, env.Fetcher, "https://www.erowid.org"+scope, scope, 3)
	if err != nil {
		return "", "", err
	}
	return target, title, nil
}

// totseSections are the folders of totse.com/en/ that held texts.
var totseSections = []string{"bad_ideas", "conspiracy", "drugs", "ego", "erotica_home", "fringe", "hack", "law", "media",
	"politics", "privacy", "religion", "society", "technology"}

var totseWayback = "https://web.archive.org/web/2007/http://www.totse.com"

// reTotse takes a Wayback link apart: /web/<time>/http://www.totse.com<path>.
var reTotse = regexp.MustCompile(`^/web/\d+[a-z_]*/https?://(?:www\.)?totse\.com(/en/[^?#]+)$`)

type totseLink struct{ href, path, text string }

// totseLinks lists the links of a Wayback copy of a Totse page that satisfy
// keep, which is given each link's path on totse.com.
func totseLinks(ctx context.Context, f *fetch.Fetcher, raw string, keep func(p string) bool) ([]totseLink, error) {
	gq, base, err := get(ctx, f, raw)
	if err != nil {
		return nil, err
	}
	var out []totseLink
	seen := map[string]bool{}
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u, err := base.Parse(strings.TrimSpace(href))
		if err != nil || u.Host != "web.archive.org" {
			return
		}
		m := reTotse.FindStringSubmatch(u.Path)
		if m == nil || !keep(m[1]) || seen[m[1]] {
			return
		}
		seen[m[1]] = true
		out = append(out, totseLink{u.String(), m[1], strings.Join(strings.Fields(a.Text()), " ")})
	})
	return out, nil
}

// totsePick draws a section, then one of its folders, then a text of that
// folder: three steps, two of them page loads.
func totsePick(ctx context.Context, env Env) (string, string, error) {
	section := pick(totseSections)
	dir := "/en/" + section + "/"
	subs, err := totseLinks(ctx, env.Fetcher, totseWayback+dir+"index.html", func(p string) bool {
		return strings.HasPrefix(p, dir) && strings.HasSuffix(p, "/index.html") && strings.Count(p, "/") == 4
	})
	if err != nil {
		return "", "", err
	}
	from, where := totseWayback+dir+"index.html", dir
	if len(subs) > 0 {
		s := pick(subs)
		from, where = s.href, path.Dir(s.path)+"/"
	}
	arts, err := totseLinks(ctx, env.Fetcher, from, func(p string) bool {
		return strings.HasPrefix(p, where) && strings.HasSuffix(p, ".html") && !strings.HasSuffix(p, "/index.html") &&
			strings.Count(p, "/") == strings.Count(where, "/")
	})
	if err != nil {
		return "", "", err
	}
	if len(arts) == 0 {
		return "", "", errors.New("nothing listed under " + where)
	}
	a := pick(arts)
	title := strings.Trim(where, "/")
	title = strings.TrimPrefix(title, "en/")
	if a.text != "" {
		title += " · " + a.text
	}
	return a.href, title, nil
}
