package fiction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// wordPress reads independent serials: from a table of contents page, or
// by following "Next Chapter" links. It never claims an address by itself;
// it is used for `serial <address>`, the presets and serials stored as such.
type wordPress struct{}

// continuer is an adapter that can continue from the chapters already known
// (serials without a table of contents).
type continuer interface {
	SerialFrom(ctx context.Context, f *fetch.Fetcher, serial string, known []Chapter) (*Serial, error)
}

// wpHostAlias maps link hosts to the host actually serving them (tests).
var wpHostAlias map[string]string

// wpWalk is how many pages one refresh follows.
const wpWalk = 50

var (
	reNavText  = regexp.MustCompile(`(?i)^\s*(last|previous|prev|next|table of contents|contents|index|home)(\s+(chapter|part|page|arc))?\s*[›»>→<«‹←]*\s*$`)
	reNextText = regexp.MustCompile(`(?i)^\s*next(\s+(chapter|part|page))?\s*[›»>→]*\s*$`)
	// Category links stay: some serials (Worm) link chapters through them.
	reSkipPath = regexp.MustCompile(`(?i)/(tag|author|feed|comments?|wp-login|wp-admin|page/\d+)(/|$)|\?share=|/#|replytocom`)
)

func init() { adapters = append(adapters, wordPress{}) }

func (wordPress) Kind() string                  { return "wordpress" }
func (wordPress) Match(*url.URL) (string, bool) { return "", false }

func (a wordPress) Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error) {
	return a.SerialFrom(ctx, f, serial, nil)
}

func contentOf(gq *goquery.Document) *goquery.Selection {
	// .pjgm-postcontent: Unsong's theme; .page__content: qntm.org (Ra).
	for _, sel := range []string{".entry-content", ".pjgm-postcontent", ".page__content", "article", "main", "#content"} {
		if c := gq.Find(sel).First(); c.Length() > 0 {
			return c
		}
	}
	return gq.Find("body")
}

func siteTitle(gq *goquery.Document, host string) string {
	if s, ok := gq.Find(`meta[property="og:site_name"]`).Attr("content"); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return host
}

func sameHost(a, b *url.URL) bool {
	h := func(u *url.URL) string { return strings.TrimPrefix(strings.ToLower(u.Host), "www.") }
	return h(a) == h(b)
}

// linkURL resolves a link, applying test host aliases.
func linkURL(base *url.URL, href string) *url.URL {
	u, err := base.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil
	}
	if to, ok := wpHostAlias[strings.ToLower(u.Host)]; ok {
		u.Host, u.Scheme = to, base.Scheme
	}
	u.Fragment = ""
	return u
}

func (a wordPress) SerialFrom(ctx context.Context, f *fetch.Fetcher, serial string, known []Chapter) (*Serial, error) {
	gq, base, err := getPage(ctx, f, serial, siteName("wordpress", serial), true)
	if err != nil {
		return nil, err
	}
	s := &Serial{Kind: "wordpress", URL: serial, Title: siteTitle(gq, base.Host)}
	// A site's front page is a blog index, not a table of contents: the
	// site's public post list (oldest first) gives every chapter at once.
	if p := strings.Trim(base.Path, "/"); p == "" {
		if chs, err := wpPostList(ctx, f, base); err == nil && len(chs) >= 2 {
			s.Chapters = chs
			return s, nil
		}
	}
	if chs := tocLinks(gq, base); len(chs) >= 5 {
		s.Chapters = chs
		if t := text(gq.Find("h1.entry-title, h1").First()); t != "" && !strings.Contains(strings.ToLower(t), "contents") {
			s.Title = t
		}
		return s, nil
	}
	if len(known) > 0 { // continue from the last known chapter
		if gq, base, err = getPage(ctx, f, known[len(known)-1].URL, siteName("wordpress", serial), true); err != nil {
			return nil, err
		}
	}
	// Walk "Next" links.
	s.Chapters = append(s.Chapters, known...)
	seen := map[string]bool{}
	for _, c := range known {
		seen[c.URL] = true
	}
	for i := 0; i < wpWalk; i++ {
		if len(known) == 0 || i > 0 {
			c := Chapter{Title: text(gq.Find("h1.entry-title, h1").First()), URL: base.String()}
			if !seen[c.URL] {
				seen[c.URL] = true
				s.Chapters = append(s.Chapters, c)
			}
		}
		next := nextLink(gq, base)
		if next == nil || seen[next.String()] {
			break
		}
		if gq, base, err = getPage(ctx, f, next.String(), siteName("wordpress", serial), false); err != nil {
			if len(s.Chapters) > 0 {
				break // keep what was found
			}
			return nil, err
		}
	}
	if len(s.Chapters) == 0 {
		return nil, errors.New(siteName("wordpress", serial) + ": no table of contents or next-chapter links found")
	}
	return s, nil
}

// wpPostList reads a WordPress site's public post list, oldest first:
// the site's own REST API (/wp-json), else WordPress.com's public API for
// sites hosted there. At most 30 pages of 100 posts.
func wpPostList(ctx context.Context, f *fetch.Fetcher, site *url.URL) ([]Chapter, error) {
	root := site.Scheme + "://" + site.Host
	var out []Chapter
	for page := 1; page <= 30; page++ {
		u, _ := url.Parse(fmt.Sprintf("%s/wp-json/wp/v2/posts?per_page=100&order=asc&orderby=date&_fields=link,title,date&page=%d", root, page))
		resp, err := f.Get(ctx, u, fetch.Options{Revalidate: true})
		if err != nil {
			break // past the last page (HTTP 400) or no REST API
		}
		var posts []struct {
			Link, Date string
			Title      struct{ Rendered string }
		}
		if json.Unmarshal(resp.Body, &posts) != nil || len(posts) == 0 {
			break
		}
		for _, p := range posts {
			c := Chapter{Title: html.UnescapeString(strings.TrimSpace(p.Title.Rendered)), URL: p.Link}
			c.Published, _ = time.Parse("2006-01-02T15:04:05", p.Date)
			out = append(out, c)
		}
		if len(posts) < 100 {
			return out, nil
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	for page := 1; page <= 30; page++ {
		u, _ := url.Parse(fmt.Sprintf("https://public-api.wordpress.com/rest/v1.1/sites/%s/posts/?number=100&order=ASC&fields=URL,title,date&page=%d", site.Host, page))
		resp, err := f.Get(ctx, u, fetch.Options{Revalidate: true})
		if err != nil {
			break
		}
		var res struct {
			Found int
			Posts []struct{ URL, Title, Date string }
		}
		if json.Unmarshal(resp.Body, &res) != nil || len(res.Posts) == 0 {
			break
		}
		for _, p := range res.Posts {
			c := Chapter{Title: html.UnescapeString(strings.TrimSpace(p.Title)), URL: p.URL}
			c.Published, _ = time.Parse(time.RFC3339, p.Date)
			out = append(out, c)
		}
		if len(out) >= res.Found {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no public post list")
	}
	return out, nil
}

// tocLinks are the chapter links of a table of contents page.
func tocLinks(gq *goquery.Document, base *url.URL) []Chapter {
	var out []Chapter
	seen := map[string]bool{base.String(): true}
	links := contentOf(gq).Find("a[href]")
	// Table-of-contents layouts with their own chapter rows (The Wandering
	// Inn: one row per chapter, the web link in .body-web).
	for _, sel := range []string{".chapter-entry .body-web a[href]", "#table-of-contents a[href]", ".toc a[href]"} {
		if l := gq.Find(sel); l.Length() >= 5 {
			links = l
			break
		}
	}
	links.Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u := linkURL(base, href)
		t := text(a)
		if u == nil || !sameHost(u, base) || u.Path == "" || u.Path == "/" || reSkipPath.MatchString(u.String()) || t == "" || reNavText.MatchString(t) {
			return
		}
		k := u.String()
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, Chapter{Title: t, URL: k})
	})
	return out
}

func nextLink(gq *goquery.Document, base *url.URL) *url.URL {
	if href, ok := gq.Find(`a[rel="next"]`).First().Attr("href"); ok {
		if u := linkURL(base, href); u != nil && sameHost(u, base) {
			return u
		}
	}
	var out *url.URL
	contentOf(gq).Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if reNextText.MatchString(text(a)) {
			href, _ := a.Attr("href")
			if u := linkURL(base, href); u != nil && sameHost(u, base) {
				out = u
				return false
			}
		}
		return true
	})
	return out
}

func (wordPress) Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error) {
	site := siteName("wordpress", ch.URL)
	gq, base, err := getPage(ctx, f, ch.URL, site, false)
	if err != nil {
		return nil, err
	}
	content := contentOf(gq)
	content.Find(".sharedaddy, .jp-relatedposts, .wpcnt, .wp-block-buttons, script, style, form").Remove()
	// Drop rows made only of navigation links (Last Chapter / Next Chapter).
	content.Find("p, div, center").Each(func(_ int, p *goquery.Selection) {
		links := p.Find("a")
		if links.Length() == 0 || p.Find("p, div").Length() > 0 {
			return
		}
		rest := text(p)
		onlyNav := true
		links.Each(func(_ int, a *goquery.Selection) {
			if !reNavText.MatchString(text(a)) {
				onlyNav = false
			}
			rest = strings.Replace(rest, text(a), "", 1)
		})
		if onlyNav && strings.Trim(rest, " |•·-–—.") == "" {
			p.Remove()
		}
	})
	d := &doc.Document{Lang: "en"}
	title := text(gq.Find("h1.entry-title").First())
	if title == "" {
		title = ch.Title
	}
	if title != "" {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: title}}})
	}
	d.Blocks = append(d.Blocks, blocksOf(d, content, base)...)
	if len(d.Blocks) <= 1 {
		return nil, errors.New(site + ": no chapter text found on the page")
	}
	return d, nil
}
