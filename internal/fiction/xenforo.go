package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/htmlconv"
)

// xenForo reads threadmarked stories on XenForo forums.
type xenForo struct{}

var xenForoHosts = map[string]bool{
	"forums.spacebattles.com": true, "forums.sufficientvelocity.com": true,
	"questionablequesting.com": true, "forum.questionablequesting.com": true,
}

var (
	reXFThread = regexp.MustCompile(`^/threads/([^/]+\.\d+)(/|$)`)
	reXFPost   = regexp.MustCompile(`(?:^/posts/|#post-|/post-)(\d+)`)
)

func init() { adapters = append(adapters, xenForo{}) }

func (xenForo) Kind() string { return "xenforo" }

func (xenForo) Match(u *url.URL) (string, bool) {
	h := strings.ToLower(u.Hostname())
	if !xenForoHosts[h] {
		return "", false
	}
	m := reXFThread.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return "https://" + h + "/threads/" + m[1] + "/", true
}

// Key: a chapter is a post, however it was addressed.
func (xenForo) Key(u *url.URL) string {
	if m := reXFPost.FindStringSubmatch(u.Path + "#" + u.Fragment); m != nil {
		return "xf-post:" + m[1]
	}
	return ""
}

func (xenForo) Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error) {
	site := siteName("xenforo", serial)
	s := &Serial{Kind: "xenforo", URL: serial}
	seen := map[string]bool{}
	next := strings.TrimSuffix(serial, "/") + "/threadmarks?threadmark_category=1&per_page=200"
	for page := 0; next != "" && page < 100; page++ {
		gq, base, err := getPage(ctx, f, next, site, true)
		if err != nil {
			return nil, err
		}
		if page == 0 {
			title := gq.Find("h1.p-title-value").First().Clone()
			title.Find(".label, .labelLink").Remove()
			s.Title = text(title)
		}
		gq.Find(".structItem--threadmark").Each(func(_ int, it *goquery.Selection) {
			a := it.Find(".structItem-title a").First()
			href, _ := a.Attr("href")
			m := reXFPost.FindStringSubmatch(href)
			if m == nil || seen[m[1]] {
				return
			}
			seen[m[1]] = true
			if s.Author == "" {
				s.Author, _ = it.Attr("data-content-author")
			}
			c := Chapter{Title: text(a), URL: resolve(base, "/posts/"+m[1]+"/")}
			if t, ok := it.Find("time[data-time]").Attr("data-time"); ok {
				c.Published = unixTime(t)
			}
			s.Chapters = append(s.Chapters, c)
		})
		next = ""
		if href, ok := gq.Find("a.pageNav-jump--next").First().Attr("href"); ok {
			next = resolve(base, href)
		}
	}
	if len(s.Chapters) == 0 {
		return nil, fmt.Errorf("%s: %w (no threadmarks)", site, errNotSerial)
	}
	return s, nil
}

func (a xenForo) Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error) {
	site := siteName("xenforo", ch.URL)
	u, err := url.Parse(ch.URL)
	if err != nil {
		return nil, err
	}
	id := strings.TrimPrefix(a.Key(u), "xf-post:")
	if id == "" {
		return nil, errors.New(site + ": not a post address")
	}
	gq, base, err := getPage(ctx, f, ch.URL, site, false)
	if err != nil {
		return nil, err
	}
	post := gq.Find("#js-post-" + id).First()
	if post.Length() == 0 {
		post = gq.Find(`[data-content="post-` + id + `"]`).First()
	}
	body := post.Find(".bbWrapper").First()
	if body.Length() == 0 {
		return nil, errors.New(site + ": post not found on its page — it may have been deleted or moved")
	}
	d := &doc.Document{Lang: "en"}
	if ch.Title != "" {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: ch.Title}}})
	}
	d.Blocks = append(d.Blocks, spoilerBlocks(d, body, base)...)
	return d, nil
}

// spoilerBlocks converts a post body; top-level spoilers become folded
// sections, nested ones are opened in place with their label.
func spoilerBlocks(d *doc.Document, body *goquery.Selection, base *url.URL) []doc.Block {
	label := func(sp *goquery.Selection) string {
		l := text(sp.Find(".bbCodeSpoiler-button .button-text").First())
		if l == "" {
			l = "Spoiler"
		}
		return l
	}
	body.Find(".bbCodeSpoiler .bbCodeSpoiler").Each(func(_ int, sp *goquery.Selection) {
		content, _ := sp.Find(".bbCodeBlock-content").First().Html()
		sp.ReplaceWithHtml("<p><b>" + html.EscapeString(label(sp)) + "</b></p>" + content)
	})
	var out []doc.Block
	var chunk strings.Builder
	flush := func() {
		if strings.TrimSpace(chunk.String()) != "" {
			out = append(out, htmlconv.Fragment(d, chunk.String(), base.String())...)
		}
		chunk.Reset()
	}
	body.Contents().Each(func(_ int, n *goquery.Selection) {
		if n.HasClass("bbCodeSpoiler") {
			flush()
			content := n.Find(".bbCodeBlock-content").First()
			out = append(out, folded(label(n), blocksOf(d, content, base)))
			return
		}
		h, err := goquery.OuterHtml(n)
		if err == nil {
			chunk.WriteString(h)
		}
	})
	flush()
	return out
}
