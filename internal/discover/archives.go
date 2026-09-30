package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"

	"w5f/internal/fetch"
)

// Sources added in M9 (chosen with the owner, 2026-09-30): the Internet
// Encyclopedia of Philosophy, the Perseus Digital Library, the Biodiversity
// Heritage Library (through its Internet Archive copy: its own site asks
// every visitor for a browser check) and small web directories.

// --- Internet Encyclopedia of Philosophy: an article from a random letter.

var iepBase = "https://iep.utm.edu/"

func iepPick(ctx context.Context, f *fetch.Fetcher) (string, string, error) {
	var errs []string
	for try := 0; try < 3; try++ {
		letter := string(rune('a' + rand.IntN(26)))
		gq, base, err := get(ctx, f, iepBase+letter+"/")
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		type art struct{ href, title string }
		var arts []art
		gq.Find(`a[rel="bookmark"][href]`).Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			u, err := base.Parse(href)
			if err != nil || u.Host != base.Host {
				return
			}
			title := a.AttrOr("title", strings.TrimSpace(a.Text()))
			arts = append(arts, art{u.String(), strings.Join(strings.Fields(title), " ")})
		})
		if len(arts) > 0 {
			a := pick(arts)
			return a.href, a.title, nil
		}
		errs = append(errs, "IEP /"+letter+"/: no articles")
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

// --- Perseus: an English text of the Greek and Roman collection (its
// classic interface; the new Scaife viewer asks for a browser check).

var (
	perseusCollection = "https://www.perseus.tufts.edu/hopper/collection?collection=Perseus:collection:Greco-Roman"
	// Reference works and commentaries are for looking things up, not reading.
	reNotReading = regexp.MustCompile(`(?i)commentar|grammar|lexicon|dictionar|syntax|notes on|index|handbook|introduction|a companion|vocabular|gazetteer`)
	// Perseus numbers its secondary sources (scholarship, commentaries,
	// grammars) 1999.04; the other series are the ancient texts themselves.
	reSecondary = regexp.MustCompile(`(?i)text(%3a|:)1999\.04\.`)
)

func perseusPick(ctx context.Context, env Env) (string, string, error) {
	gq, base, err := get(ctx, env.Fetcher, perseusCollection)
	if err != nil {
		return "", "", err
	}
	type work struct{ href, title string }
	var works []work
	gq.Find("tr.trResults").Each(func(_ int, tr *goquery.Selection) {
		text := strings.Join(strings.Fields(tr.Find("td.tdAuthor").First().Text()), " ")
		i := strings.Index(text, "(English)")
		if i < 0 {
			return
		}
		title := strings.TrimSpace(text[:i])
		if reNotReading.MatchString(title) {
			return
		}
		href, ok := tr.Find(`a[href^="text?doc="]`).First().Attr("href")
		if !ok || reSecondary.MatchString(href) {
			return
		}
		u, err := base.Parse(href)
		if err != nil {
			return
		}
		works = append(works, work{u.String(), strings.TrimSuffix(title, ".")})
	})
	if len(works) == 0 {
		return "", "", errors.New("Perseus: no English texts listed")
	}
	w := pick(works)
	return w.href, w.title, nil
}

// --- Biodiversity Heritage Library, through the Internet Archive's copy of
// it (collection "biodiversity"): old natural history on the curious side.
// The draw opens the book's full text.

var (
	archiveAPI = "https://archive.org"
	bhlQuery   = `collection:biodiversity AND mediatype:texts AND (subject:(bestiaries OR monsters OR dragons OR "sea serpents" OR folklore OR superstition OR mythology OR legends OR magic OR alchemy OR witchcraft OR herbals OR "pre-linnean" OR "early works to 1800" OR curiosities) OR title:(bestiary OR monsters OR wonders OR curiosities OR "natural magic" OR superstitions OR folk-lore OR herbal))`
)

type archiveDoc struct {
	Identifier string          `json:"identifier"`
	Title      string          `json:"title"`
	Year       json.RawMessage `json:"year"`
}

func archiveSearch(ctx context.Context, f *fetch.Fetcher, q string, rows, page int) (int, []archiveDoc, error) {
	v := url.Values{"q": {q}, "fl[]": {"identifier", "title", "year"}, "rows": {strconv.Itoa(rows)},
		"page": {strconv.Itoa(page)}, "output": {"json"}}
	u, _ := url.Parse(archiveAPI + "/advancedsearch.php?" + v.Encode())
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return 0, nil, err
	}
	var r struct {
		Response struct {
			NumFound int          `json:"numFound"`
			Docs     []archiveDoc `json:"docs"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return 0, nil, errors.New("unexpected answer from the Internet Archive")
	}
	return r.Response.NumFound, r.Response.Docs, nil
}

func bhlPick(ctx context.Context, env Env) (Draw, error) {
	n, _, err := archiveSearch(ctx, env.Fetcher, bhlQuery, 1, 1)
	if err != nil {
		return Draw{}, err
	}
	if n == 0 {
		return Draw{}, errors.New("BHL: nothing found")
	}
	_, docs, err := archiveSearch(ctx, env.Fetcher, bhlQuery, 1, 1+rand.IntN(min(n, 10000)))
	if err != nil || len(docs) == 0 {
		return Draw{}, errors.New("BHL: no book on that page")
	}
	d := docs[0]
	id := url.PathEscape(d.Identifier)
	title := strings.Join(strings.Fields(d.Title), " ")
	if len([]rune(title)) > 110 {
		title = string([]rune(title)[:110]) + "…"
	}
	var year string
	if json.Unmarshal(d.Year, &year) != nil {
		var y int
		if json.Unmarshal(d.Year, &y) == nil && y > 0 {
			year = strconv.Itoa(y)
		}
	}
	if year != "" {
		title += " (" + year + ")"
	}
	return Draw{Target: archiveAPI + "/stream/" + id + "/" + id + "_djvu.txt",
		Why: "public domain/Biodiversity Heritage Library · " + title}, nil
}

// --- Small web directories.

var (
	oohRandom   = "https://ooh.directory/random/"
	kagiFeed    = "https://kagi.com/api/v1/smallweb/feed/"
	oocitiesTop = "https://www.oocities.org/"
	// GeoCities neighborhoods close to the owner's reading (their own index
	// pages list the members' folders).
	oocitiesHoods = []string{"area51", "athens", "soho", "capecanaveral", "researchtriangle", "rainforest", "tokyo", "hollywood", "timessquare", "enchantedforest"}
)

// oohDraw takes a blog from ooh.directory's random page: its latest post,
// or the blog itself.
func oohDraw(ctx context.Context, f *fetch.Fetcher) (Draw, error) {
	u, _ := url.Parse(oohRandom)
	resp, err := f.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return Draw{}, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return Draw{}, err
	}
	var draws []Draw
	gq.Find("li.websites__item").Each(func(_ int, li *goquery.Selection) {
		a := li.Find(".website__intro a[href]").First()
		home, _ := a.Attr("href")
		name := strings.Join(strings.Fields(a.Text()), " ")
		target := home
		if post, ok := li.Find("figcaption a[href]").First().Attr("href"); ok {
			target = post
		}
		t, err := u.Parse(target)
		if err != nil || (t.Scheme != "http" && t.Scheme != "https") || t.Host == u.Host {
			return
		}
		why := "smallweb/ooh.directory · " + name
		if q := strings.Join(strings.Fields(li.Find("q").First().Text()), " "); q != "" {
			why += " — " + q
		}
		draws = append(draws, Draw{Target: t.String(), Why: why})
	})
	if len(draws) == 0 {
		return Draw{}, errors.New("ooh.directory: no blogs on the random page")
	}
	return pick(draws), nil
}

// kagiDraw takes a recent post from Kagi's Small Web feed (personal sites
// written by people).
func kagiDraw(ctx context.Context, f *fetch.Fetcher) (Draw, error) {
	u, _ := url.Parse(kagiFeed)
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return Draw{}, err
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(resp.Body))
	if err != nil {
		return Draw{}, fmt.Errorf("Kagi Small Web: %v", err)
	}
	var draws []Draw
	for _, it := range feed.Items {
		t, err := url.Parse(strings.TrimSpace(it.Link))
		if err != nil || (t.Scheme != "http" && t.Scheme != "https") {
			continue
		}
		why := "smallweb/Kagi Small Web · " + t.Host
		if title := strings.Join(strings.Fields(it.Title), " "); title != "" {
			why += " — " + title
		}
		draws = append(draws, Draw{Target: t.String(), Why: why})
	}
	if len(draws) == 0 {
		return Draw{}, errors.New("Kagi Small Web: the feed is empty")
	}
	return pick(draws), nil
}

// oocitiesDraw walks from a neighborhood's folder list down to a member's
// page of the GeoCities archive; a walk that ends in an empty folder starts
// over in another neighborhood.
func oocitiesDraw(ctx context.Context, f *fetch.Fetcher) (Draw, error) {
	var errs []string
	for try := 0; try < 3; try++ {
		hood := pick(oocitiesHoods)
		target, title, err := oocitiesWalk(ctx, f, oocitiesTop+hood+"/")
		if err != nil {
			errs = append(errs, hood+": "+err.Error())
			continue
		}
		why := "smallweb/GeoCities (OoCities) · " + hood
		if title != "" {
			why += " — " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New("OoCities: " + strings.Join(errs, "; "))
}

// oocitiesWalk goes down folder listings ("Index of …", which the archive
// pads with a long notice, so they look like text) until a real page:
// a folder's own index page first, else another page of it, else a random
// subfolder.
func oocitiesWalk(ctx context.Context, f *fetch.Fetcher, start string) (string, string, error) {
	cur := start
	for level := 0; level < 6; level++ {
		gq, base, err := get(ctx, f, cur)
		if err != nil {
			return "", "", err
		}
		title := strings.Join(strings.Fields(gq.Find("title").First().Text()), " ")
		if !strings.HasPrefix(title, "Index of") {
			if level == 0 {
				return "", "", errors.New("not a folder listing")
			}
			return base.String(), title, nil
		}
		var index, pages, dirs []string
		gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			u, err := base.Parse(href)
			if err != nil || u.Host != base.Host || u.RawQuery != "" || !strings.HasPrefix(u.Path, base.Path) || u.Path == base.Path {
				return // other sites, sort links, the parent folder
			}
			name := strings.ToLower(strings.TrimPrefix(u.Path, base.Path))
			switch {
			case strings.HasSuffix(name, "/") && strings.Count(name, "/") == 1:
				dirs = append(dirs, u.String())
			case name == "index.html" || name == "index.htm":
				index = append(index, u.String())
			case strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".htm"):
				pages = append(pages, u.String())
			}
		})
		switch {
		case len(index) > 0:
			cur = index[0]
		case len(pages) > 0:
			cur = pick(pages)
		case len(dirs) > 0:
			cur = pick(dirs)
		default:
			return "", "", errors.New("an empty folder")
		}
	}
	return "", "", errors.New("too deep")
}
