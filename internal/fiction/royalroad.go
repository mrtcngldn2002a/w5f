package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// royalRoad reads royalroad.com serials.
type royalRoad struct{}

const rrBase = "https://www.royalroad.com"

var reRRFiction = regexp.MustCompile(`^/fiction/(\d+)(/|$)`)

func init() {
	adapters = append(adapters, royalRoad{})
	extraRoutes["fiction/rr/best"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return rrList(ctx, env.Fetcher, rrBase+"/fictions/best-rated", "Royal Road — best rated")
	}
	extraRoutes["fiction/rr/latest"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return rrList(ctx, env.Fetcher, rrBase+"/fictions/latest-updates", "Royal Road — latest updates")
	}
	extraRoutes["fiction/rr/search"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		if strings.TrimSpace(q.Get("q")) == "" {
			return nil, errors.New("search needs words: g → rr <words>")
		}
		return rrList(ctx, env.Fetcher, rrBase+"/fictions/search?"+url.Values{"title": {q.Get("q")}}.Encode(), "Royal Road: "+q.Get("q"))
	}
}

func (royalRoad) Kind() string { return "royalroad" }

func (royalRoad) Match(u *url.URL) (string, bool) {
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if h != "royalroad.com" {
		return "", false
	}
	m := reRRFiction.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return rrBase + "/fiction/" + m[1], true
}

// Key: chapters are identified by their numeric id.
func (royalRoad) Key(u *url.URL) string {
	if i := strings.Index(u.Path, "/chapter/"); i >= 0 {
		return "rr:" + strings.Split(u.Path[i+len("/chapter/"):], "/")[0]
	}
	return ""
}

func (royalRoad) Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error) {
	gq, base, err := getPage(ctx, f, serial, "Royal Road", true)
	if err != nil {
		return nil, err
	}
	s := &Serial{Kind: "royalroad", URL: serial, Title: text(gq.Find(".fic-header h1, h1").First()),
		Author:  text(gq.Find(`h4 a[href^="/profile/"]`).First()),
		Summary: text(gq.Find(".description .hidden-content").First())}
	if s.Summary == "" {
		s.Summary = text(gq.Find(".description").First())
	}
	gq.Find("tr.chapter-row").Each(func(_ int, tr *goquery.Selection) {
		href, _ := tr.Attr("data-url")
		if href == "" {
			href, _ = tr.Find("a").First().Attr("href")
		}
		if href == "" {
			return
		}
		c := Chapter{Title: text(tr.Find("td a").First()), URL: resolve(base, href)}
		if ut, ok := tr.Find("time[unixtime]").Attr("unixtime"); ok {
			c.Published = unixTime(ut)
		}
		s.Chapters = append(s.Chapters, c)
	})
	if len(s.Chapters) == 0 {
		return nil, errors.New("Royal Road: no chapter list found — the site layout may have changed")
	}
	return s, nil
}

var reHiddenClass = regexp.MustCompile(`\.([A-Za-z0-9_-]+)\s*\{[^}]*display:\s*none`)

func (royalRoad) Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error) {
	gq, base, err := getPage(ctx, f, ch.URL, "Royal Road", false)
	if err != nil {
		return nil, err
	}
	// Royal Road hides anti-copying sentences with a random class declared
	// display:none in the page's own style sheet.
	gq.Find("style").Each(func(_ int, st *goquery.Selection) {
		for _, m := range reHiddenClass.FindAllStringSubmatch(st.Text(), -1) {
			gq.Find("." + m[1]).Remove()
		}
	})
	content := gq.Find(".chapter-content").First()
	if content.Length() == 0 {
		return nil, errors.New("Royal Road: no chapter text found — the site layout may have changed")
	}
	d := &doc.Document{Lang: "en"}
	title := text(gq.Find("h1").First())
	if title == "" {
		title = ch.Title
	}
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: title}}})
	var after []doc.Block
	gq.Find(".author-note-portlet").Each(func(_ int, n *goquery.Selection) {
		note := folded("Author's note", blocksOf(d, n.Find(".author-note"), base))
		if isBefore(n, content) {
			d.Blocks = append(d.Blocks, note)
		} else {
			after = append(after, note)
		}
	})
	d.Blocks = append(d.Blocks, blocksOf(d, content, base)...)
	d.Blocks = append(d.Blocks, after...)
	return d, nil
}

// isBefore reports whether a precedes b in document order.
func isBefore(a, b *goquery.Selection) bool {
	found := false
	before := false
	a.Parents().Last().Find("*").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		switch s.Nodes[0] {
		case a.Nodes[0]:
			before, found = true, true
			return false
		case b.Nodes[0]:
			found = true
			return false
		}
		return true
	})
	return found && before
}

// rrList is a Royal Road list or search result page.
func rrList(ctx context.Context, f *fetch.Fetcher, page, title string) (*doc.Document, error) {
	gq, base, err := getPage(ctx, f, page, "Royal Road", false)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: title, URL: page, Origin: "live", Lang: "en"}
	var items [][]doc.Block
	gq.Find(".fiction-list-item style, .fiction-list-item script").Remove()
	gq.Find(".fiction-list-item").Each(func(_ int, it *goquery.Selection) {
		a := it.Find(".fiction-title a").First()
		href, _ := a.Attr("href")
		name := text(a)
		if href == "" || name == "" {
			return
		}
		in := doc.Inline{{Text: name, Style: doc.Bold, Link: link(d, openHref(resolve(base, href)), name)}}
		var sub []string
		it.Find(".stats .col-sm-6 span, .stats span").Each(func(i int, s *goquery.Selection) {
			if t := text(s); t != "" && i < 4 {
				sub = append(sub, t)
			}
		})
		if len(sub) > 0 {
			in = append(in, plain("  · "+joinDot(sub), doc.Italic))
		}
		blocks := []doc.Block{para(in...)}
		if desc := text(it.Find(".hidden-content, .fiction-description").First()); desc != "" {
			if r := []rune(desc); len(r) > 240 {
				desc = string(r[:240]) + "…"
			}
			blocks = append(blocks, para(plain(desc, 0)))
		}
		items = append(items, blocks)
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("Royal Road: no fictions found on %s", page)
	}
	d.Blocks = []doc.Block{doc.List{Ordered: true, Items: items}}
	return d, nil
}
