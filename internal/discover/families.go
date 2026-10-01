package discover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

func init() {
	families = append(families, esoteric{}, textfiles{}, folklore{}, encyclopedic{})
}

// get fetches and parses a page (cached like any page).
func get(ctx context.Context, f *fetch.Fetcher, raw string) (*goquery.Document, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, nil, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, nil, err
	}
	// Old pages with a malformed comment (the Internet Classics Archive's
	// copyright notice) lose everything after it: parse again without comments.
	if gq.Find("a[href]").Length() == 0 && bytes.Contains(bytes.ToLower(resp.Body), []byte("href")) {
		if again, err := goquery.NewDocumentFromReader(bytes.NewReader(reComment.ReplaceAll(resp.Body, nil))); err == nil {
			gq = again
		}
		if gq.Find("a[href]").Length() == 0 { // still broken: keep just its links
			var b strings.Builder
			b.WriteString("<html><body>")
			for _, m := range reHref.FindAllSubmatch(resp.Body, -1) {
				fmt.Fprintf(&b, "<a href=%q>link</a>\n", string(m[1]))
			}
			b.WriteString("</body></html>")
			if again, err := goquery.NewDocumentFromReader(strings.NewReader(b.String())); err == nil {
				gq = again
			}
		}
	}
	if resp.URL != nil {
		u = resp.URL
	}
	return gq, u, nil
}

var (
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reHref    = regexp.MustCompile(`(?i)href\s*=\s*"([^"]+)"`)
)

func pick[T any](xs []T) T { return xs[rand.IntN(len(xs))] }

// reNotContent: shop, donation, search and site pages are not reading matter.
var reNotContent = regexp.MustCompile(`(?i)/(\w*shop|store|cart|buy|donat\w*|search|help|contact|about|faq|login|privacy|links?|cgi-bin|advert\w*|subscribe)(/|\.|$)`)

var skipExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".pdf": true, ".zip": true,
	".mp3": true, ".css": true, ".js": true, ".ico": true, ".svg": true, ".webp": true, ".tmp": true, ".mid": true, ".wav": true}

// descend walks from an index page down random internal links until a page
// with real text (at most depth levels), staying in the start page's folder;
// it returns that page and its title.
func descend(ctx context.Context, f *fetch.Fetcher, start string, depth int) (string, string, error) {
	return descendIn(ctx, f, start, dirOf(startPath(start)), depth)
}

// descendIn is descend limited to paths under scope.
func descendIn(ctx context.Context, f *fetch.Fetcher, start, scope string, depth int) (string, string, error) {
	cur := start
	visited := map[string]bool{} // never climb back to a page already seen
	for level := 0; level <= depth; level++ {
		gq, base, err := get(ctx, f, cur)
		if err != nil {
			return "", "", err
		}
		visited[base.String()] = true
		gq.Find("script, style, nav, header, footer").Remove()
		body := gq.Find("body")
		if body.Length() == 0 { // old frameset-style pages have no body
			body = gq.Selection
		}
		textLen := len(strings.Join(strings.Fields(body.Text()), " "))
		linkText := 0
		var links []string
		seen := map[string]bool{}
		body.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			linkText += len(strings.Join(strings.Fields(a.Text()), " "))
			href, _ := a.Attr("href")
			u, err := base.Parse(strings.TrimSpace(href))
			if err != nil || u.Host != base.Host || (u.Scheme != "http" && u.Scheme != "https") {
				return
			}
			u.Fragment = ""
			if skipExt[strings.ToLower(path.Ext(u.Path))] || visited[u.String()] || seen[u.String()] || !strings.HasPrefix(u.Path, scope) ||
				u.Path == "/" || strings.HasSuffix(u.Path, "/index.html") && path.Dir(u.Path) == "/" ||
				reNotContent.MatchString(u.Path) || u.RawQuery != "" {
				return
			}
			seen[u.String()] = true
			links = append(links, u.String())
		})
		prose := textLen - linkText
		// Sacred Texts book/section indexes contain long introductions; keep
		// walking instead of returning their table of contents as a text.
		name := strings.ToLower(path.Base(base.Path))
		indexPage := name == "index.htm" || name == "index.html"
		if level > 0 && !indexPage && (prose >= 1500 || len(links) == 0 && prose >= 200) {
			title := strings.TrimSpace(gq.Find("title").First().Text())
			if title == "" {
				title = strings.TrimSpace(gq.Find("h1").First().Text())
			}
			return base.String(), strings.Join(strings.Fields(title), " "), nil
		}
		if len(links) == 0 {
			break
		}
		cur = pick(links)
	}
	return "", "", errors.New("no text page found under " + start)
}

func startPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	return u.Path
}

// dirOf keeps descent inside the start page's folder ("/eso/" for
// "/eso/index.htm"; the whole site for "/").
func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i+1]
	}
	return "/"
}

type site struct{ name, start string }

// drawFromSites tries a family's sites in random order: one that is down or
// behind a browser check gives way to the next.
func drawFromSites(ctx context.Context, env Env, family string, sites []site) (Draw, error) {
	var errs []string
	for _, i := range rand.Perm(len(sites)) {
		s := sites[i]
		var target, title string
		var err error
		if s.name == "Sacred Texts" {
			target, title, err = sacredTextsPick(ctx, env.Fetcher, s.start)
		} else {
			target, title, err = descend(ctx, env.Fetcher, s.start, 4)
		}
		if err != nil {
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		why := family + "/" + s.name
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// esoteric: primary texts of religion, gnosticism and the occult.
type esoteric struct{}

var esotericSites = []site{
	// The modern site links to this official archive for its original texts.
	{"Sacred Texts", "https://archive.sacred-texts.com/world.htm"},
	{"Hermetic Library", "https://hermetic.com/"},
	{"gnosis.org", "http://gnosis.org/library.html"},
	{"esotericarchives", "https://www.esotericarchives.com/"},
	{"The Alchemy Web Site", "https://www.alchemywebsite.com/"},
	{"Early Christian Writings", "https://www.earlychristianwritings.com/"},
}

func (esoteric) Name() string { return "esoteric" }
func (esoteric) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawFromSites(ctx, env, "esoteric", esotericSites)
}

// folklore: folktales and mythology with their sources.
type folklore struct{}

var folkloreSites = []site{
	{"Ashliman folktexts", "https://sites.pitt.edu/~dash/folktexts.html"},
	{"Theoi", "https://www.theoi.com/"},
}

func (folklore) Name() string { return "folklore" }
func (folklore) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawFromSites(ctx, env, "folklore", folkloreSites)
}

// textfiles: BBS-era text files from textfiles.com, from every one of its
// directories (the owner asked for every kind of content, 2026-10-02: no
// directory is left out, anarchy, drugs, hacking, sex and virus included).
type textfiles struct{}

var textfilesBase = "http://www.textfiles.com"

// textfilesDirs is the site's directory list (textfiles.com/directory.html,
// 2026-10-02).
var textfilesDirs = []string{"100", "adventure", "anarchy", "apple", "art", "bbs", "computers", "conspiracy", "drugs", "etext",
	"food", "fun", "games", "groups", "hacking", "hamradio", "holiday", "humor", "internet", "law", "magazines", "media",
	"messages", "music", "news", "occult", "phreak", "piracy", "politics", "programming", "reports", "rpg", "science", "sex",
	"sf", "stories", "survival", "ufo", "uploads", "virus"}

func (textfiles) Name() string { return "textfiles" }
func (textfiles) Draw(ctx context.Context, env Env) (Draw, error) {
	// An empty or vanished listing is tried again elsewhere, twice.
	var err error
	for range 3 {
		var d Draw
		if d, err = textfilesDraw(ctx, env.Fetcher, textfilesBase, textfilesDirs); err == nil || ctx.Err() != nil {
			return d, err
		}
	}
	return Draw{}, err
}

// textfilesDraw picks a directory, then an entry of its listing: a file
// (a plain link), or a collection (a bold link: a magazine, a group, a
// BBS), whose own listing is then drawn from, up to three levels down.
func textfilesDraw(ctx context.Context, f *fetch.Fetcher, base string, dirs []string) (Draw, error) {
	dir := pick(dirs)
	where := base + "/" + dir + "/"
	trail := []string{dir}
	for depth := 0; depth < 3; depth++ {
		gq, u, err := get(ctx, f, where)
		if err != nil {
			return Draw{}, err
		}
		type entry struct {
			href, desc string
			sub        bool
		}
		var entries []entry
		gq.Find("tr").Each(func(_ int, tr *goquery.Selection) {
			a := tr.Find("a[href]").First()
			href, _ := a.Attr("href")
			if href == "" || strings.Contains(href, "?") || strings.HasPrefix(href, ".") || strings.HasPrefix(href, "/") || strings.Contains(href, "://") {
				return
			}
			desc := strings.Join(strings.Fields(tr.Find("td").Last().Text()), " ")
			sub := strings.HasSuffix(href, "/") || a.ParentsFiltered("b").Length() > 0
			entries = append(entries, entry{href, desc, sub})
		})
		if len(entries) == 0 {
			return Draw{}, fmt.Errorf("textfiles/%s: nothing listed", strings.Join(trail, "/"))
		}
		e := pick(entries)
		target, err := u.Parse(e.href)
		if err != nil {
			return Draw{}, err
		}
		if e.sub {
			trail = append(trail, strings.TrimSuffix(e.href, "/"))
			where = strings.TrimSuffix(target.String(), "/") + "/"
			continue
		}
		why := "textfiles/" + strings.Join(trail, "/") + " · " + path.Base(target.Path)
		if r := []rune(e.desc); len(r) > 160 {
			e.desc = strings.TrimSpace(string(r[:160])) + "…"
		}
		if e.desc != "" {
			why += " — " + e.desc
		}
		return Draw{Target: target.String(), Why: why}, nil
	}
	return Draw{}, fmt.Errorf("textfiles/%s: no file within three levels", strings.Join(trail, "/"))
}

// encyclopedic: the sites' own random-article addresses.
type encyclopedic struct{}

var encyclopedias = []site{
	{"Britannica", "https://www.britannica.com/browse/Philosophy-Religion"},
	{"Wikipedia (EN)", "https://en.wikipedia.org/wiki/Special:Random"},
	{"Stanford Encyclopedia of Philosophy", "https://plato.stanford.edu/cgi-bin/encyclopedia/random"},
	{"Internet Encyclopedia of Philosophy", "https://iep.utm.edu/"},
}

func (encyclopedic) Name() string { return "encyclopedic" }
func (encyclopedic) Draw(ctx context.Context, env Env) (Draw, error) {
	var errs []string
	for _, i := range rand.Perm(len(encyclopedias)) {
		s := encyclopedias[i]
		var target, title string
		var err error
		if s.name == "Britannica" {
			target, title, err = britannicaPick(ctx, env.Fetcher, s.start)
		} else if s.name == "Internet Encyclopedia of Philosophy" {
			target, title, err = iepPick(ctx, env.Fetcher)
		} else {
			target, err = resolveRandom(ctx, env.Fetcher, s.start)
		}
		if err != nil {
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		if title == "" {
			if u, err := url.Parse(target); err == nil {
				if name, err := url.PathUnescape(path.Base(u.Path)); err == nil && name != "/" {
					title = strings.ReplaceAll(name, "_", " ")
				}
			}
		}
		why := "encyclopedic/" + s.name
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// resolveRandom follows a random address without the cache, so every call
// lands on a new page; the final address is what the reader opens.
func resolveRandom(ctx context.Context, f *fetch.Fetcher, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	resp, err := f.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return "", err
	}
	if resp.URL == nil || resp.URL.String() == raw {
		return "", errors.New("the random address did not lead anywhere")
	}
	return resp.URL.String(), nil
}
