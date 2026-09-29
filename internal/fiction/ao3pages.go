package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
)

// AO3's own navigation pages (home, fandom categories, fandom lists, tag and
// search listings) are menus and lists that the article extractor would
// throw away; they get their own conversion here. Works open as serials.

// errNotAO3Page: an AO3 page with no special conversion (shown as a web page).
var errNotAO3Page = errors.New("not an AO3 navigation page")

// IsNotAO3Page reports errNotAO3Page.
func IsNotAO3Page(err error) bool { return errors.Is(err, errNotAO3Page) }

// IsAO3 reports an AO3 address.
func IsAO3(u *url.URL) bool {
	return ao3Hosts[strings.ToLower(u.Host)] || strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") == "archiveofourown.org"
}

func init() {
	extraRoutes["fiction/ao3/fandoms"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return ao3FandomsDoc(ctx, env, q.Get("u"), q.Get("l"))
	}
	extraRoutes["fiction/ao3/page"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		u, err := url.Parse(q.Get("u"))
		if err != nil || !IsAO3(u) {
			return nil, errors.New("not an AO3 address")
		}
		if d, err := AO3Page(ctx, env, u); !IsNotAO3Page(err) {
			return d, err
		}
		return OpenURL(ctx, env, u)
	}
}

// ao3Nav is the line of AO3 shortcuts at the top of its pages.
func ao3Nav(d *doc.Document) doc.Block {
	var in doc.Inline
	for i, it := range []struct{ text, href string }{
		{"AO3 home", ao3Root + "/"},
		{"Fandoms", ao3Root + "/media"},
		{"Works search", ao3Root + "/works/search"},
		{"My AO3", "w5f:fiction/ao3/me"},
		{"Internet Fiction", "w5f:fiction"},
	} {
		if i > 0 {
			in = append(in, plain(" · ", doc.Italic))
		}
		in = append(in, doc.Span{Text: it.text, Link: link(d, it.href, it.text)})
	}
	return para(in...)
}

func fandomsHref(abs string) string {
	return "w5f:fiction/ao3/fandoms?" + url.Values{"u": {abs}}.Encode()
}

// ao3Link maps an AO3 link: fandom categories to the letter index, the
// rest to its absolute address (works then open as serials).
func ao3Link(base *url.URL, href string) string {
	abs := resolve(base, href)
	if u, err := url.Parse(abs); err == nil && strings.HasPrefix(u.Path, "/media/") && strings.HasSuffix(u.Path, "/fandoms") {
		return fandomsHref(abs)
	}
	return abs
}

// AO3Page converts an AO3 navigation page; other AO3 pages return
// errNotAO3Page.
func AO3Page(ctx context.Context, env Env, u *url.URL) (*doc.Document, error) {
	p := strings.TrimSuffix(u.Path, "/")
	switch {
	case strings.HasPrefix(p, "/media/") && strings.HasSuffix(p, "/fandoms"):
		return ao3FandomsDoc(ctx, env, u.String(), "")
	case p == "" || p == "/media" || p == "/menu/fandoms" || ao3Listing(p):
	default:
		return nil, errNotAO3Page
	}
	gq, base, err := ao3Page(ctx, env.Fetcher, u.String(), false)
	if err != nil {
		return nil, err
	}
	// A w5f: address marks the page as W5F's own, so its W5F links work.
	d := &doc.Document{URL: "w5f:fiction/ao3/page?" + url.Values{"u": {u.String()}}.Encode(), Origin: "live", Lang: "en"}
	d.Blocks = append(d.Blocks, ao3Nav(d))
	switch {
	case p == "":
		d.Title = "Archive of Our Own"
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Find your favorites"}}})
		var items [][]doc.Block
		seen := map[string]bool{}
		gq.Find(".browse.module ul li a, ul.primary.navigation li.dropdown:first-child ul.menu li a").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			if seen[href] {
				return
			}
			seen[href] = true
			items = append(items, []doc.Block{para(doc.Span{Text: text(a), Link: link(d, ao3Link(base, href), text(a))})})
		})
		if len(items) == 0 {
			return nil, errors.New("AO3: the home page has no fandom menu — the layout may have changed")
		}
		d.Blocks = append(d.Blocks, doc.List{Items: items})
		if LoadAO3Session() == "" {
			d.Blocks = append(d.Blocks, para(plain("Connect your AO3 account for bookmarks and subscriptions: g → ao3-login", doc.Italic)))
		}
	case p == "/media" || p == "/menu/fandoms":
		d.Title = "AO3 — Fandoms"
		gq.Find("li.medium").Each(func(_ int, m *goquery.Selection) {
			h := m.Find("h3.heading a").First()
			href, _ := h.Attr("href")
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: text(h), Link: link(d, ao3Link(base, href), text(h))}}})
			var items [][]doc.Block
			m.Find("ol li").Each(func(_ int, li *goquery.Selection) {
				a := li.Find("a.tag").First()
				th, _ := a.Attr("href")
				if th == "" {
					return
				}
				count := strings.TrimSpace(strings.TrimPrefix(text(li), text(a)))
				items = append(items, []doc.Block{para(doc.Span{Text: text(a), Link: link(d, resolve(base, th), text(a))}, plain("  "+count, doc.Italic))})
			})
			items = append(items, []doc.Block{para(doc.Span{Text: "all " + text(h) + " fandoms ›", Link: link(d, ao3Link(base, href), "all")})})
			d.Blocks = append(d.Blocks, doc.List{Items: items})
		})
	default:
		heading := gq.Find("#main h2.heading").First()
		d.Title = text(heading)
		if d.Title == "" {
			d.Title = strings.TrimSuffix(text(gq.Find("title")), " | Archive of Our Own")
		}
		if f := parseAO3Form(gq, base); f != nil {
			in := doc.Inline{{Text: "Sort and filter ›", Style: doc.Bold, Link: link(d, filterHref(u.String()), "Sort and filter")}}
			if s := f.summary(); s != "" {
				in = append(in, plain("   "+s, doc.Italic))
			}
			d.Blocks = append(d.Blocks, para(in...))
		}
		items := ao3Blurbs(d, gq, base)
		if len(items) == 0 {
			d.Blocks = append(d.Blocks, para(plain("No works here.", doc.Italic)))
		} else {
			d.Blocks = append(d.Blocks, doc.List{Items: items})
		}
		var nav doc.Inline
		if prev, ok := gq.Find("ol.pagination li.previous a").First().Attr("href"); ok {
			d.Prev = resolve(base, prev)
			nav = append(nav, doc.Span{Text: "‹ previous page", Link: link(d, d.Prev, "previous")}, plain("   ", 0))
		}
		if next, ok := gq.Find("ol.pagination li.next a").First().Attr("href"); ok {
			d.Next = resolve(base, next)
			nav = append(nav, doc.Span{Text: "next page ›", Link: link(d, d.Next, "next")})
		}
		if len(nav) > 0 {
			d.Blocks = append(d.Blocks, doc.Rule{}, para(nav...))
		}
	}
	return d, nil
}

// ao3Listing reports AO3 pages that list works.
func ao3Listing(p string) bool {
	switch {
	case p == "/works", p == "/works/search", p == "/bookmarks":
		return true
	case strings.HasPrefix(p, "/tags/") && (strings.HasSuffix(p, "/works") || strings.HasSuffix(p, "/bookmarks")):
		return true
	case strings.HasPrefix(p, "/users/") && (strings.HasSuffix(p, "/works") || strings.HasSuffix(p, "/bookmarks") || strings.HasSuffix(p, "/series")):
		return true
	case strings.HasPrefix(p, "/series/"), strings.HasPrefix(p, "/collections/") && strings.HasSuffix(p, "/works"):
		return true
	}
	return false
}

// ao3FandomsDoc: a category's fandoms. AO3 lists thousands on one page,
// so W5F shows the letters first, then one letter's fandoms.
func ao3FandomsDoc(ctx context.Context, env Env, page, letter string) (*doc.Document, error) {
	u, err := url.Parse(page)
	if err != nil || !IsAO3(u) {
		return nil, errors.New("not an AO3 fandom list")
	}
	gq, base, err := ao3Page(ctx, env.Fetcher, page, false)
	if err != nil {
		return nil, err
	}
	cat := strings.TrimSpace(strings.TrimPrefix(text(gq.Find("#main h2.heading").First()), "Fandoms >"))
	if cat == "" {
		cat = "Fandoms"
	}
	d := &doc.Document{Title: "AO3 — " + cat, URL: fandomsHref(page), Origin: "live", Lang: "en"}
	d.Blocks = append(d.Blocks, ao3Nav(d))
	letters := gq.Find("li.letter")
	if letters.Length() == 0 {
		return nil, errors.New("AO3: no fandom list found — the layout may have changed")
	}
	if letter == "" {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: cat}}})
		var in doc.Inline
		letters.Each(func(i int, li *goquery.Selection) {
			l := strings.TrimSpace(strings.TrimSuffix(text(li.Find("h3.heading").First()), "↑"))
			n := li.Find("a.tag").Length()
			if i > 0 {
				in = append(in, plain("   ", 0))
			}
			label := fmt.Sprintf("%s (%d)", l, n)
			in = append(in, doc.Span{Text: label, Link: link(d, "w5f:fiction/ao3/fandoms?"+url.Values{"u": {page}, "l": {l}}.Encode(), label)})
		})
		d.Blocks = append(d.Blocks, para(in...))
		return d, nil
	}
	d.Title += " — " + letter
	d.Blocks = append(d.Blocks, para(doc.Span{Text: "‹ all letters", Link: link(d, fandomsHref(page), "letters")}))
	var items [][]doc.Block
	letters.Each(func(_ int, li *goquery.Selection) {
		if strings.TrimSpace(strings.TrimSuffix(text(li.Find("h3.heading").First()), "↑")) != letter {
			return
		}
		li.Find("ul.tags li").Each(func(_ int, t *goquery.Selection) {
			a := t.Find("a.tag").First()
			href, _ := a.Attr("href")
			if href == "" {
				return
			}
			count := strings.TrimSpace(strings.TrimPrefix(text(t), text(a)))
			items = append(items, []doc.Block{para(doc.Span{Text: text(a), Link: link(d, resolve(base, href), text(a))}, plain("  "+count, doc.Italic))})
		})
	})
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	return d, nil
}
