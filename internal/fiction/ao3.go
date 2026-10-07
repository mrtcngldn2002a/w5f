package fiction

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// ao3 reads works on the Archive of Our Own.
type ao3 struct{}

const ao3Base = "https://archiveofourown.org"

var reAO3Work = regexp.MustCompile(`^/works/(\d+)(/|$)`)

// errAdult is AO3's adult-content warning; W5F shows it and lets the owner
// decide.
type errAdult struct{ work, text string }

func (e errAdult) Error() string { return "AO3: " + e.text }

func init() {
	adapters = append(adapters, ao3{})
	extraRoutes["fiction/ao3/search"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		if strings.TrimSpace(q.Get("q")) == "" {
			return nil, errors.New("search needs words: g → ao3 <words>")
		}
		return ao3Search(ctx, env.Fetcher, q.Get("q"))
	}
}

func (ao3) Kind() string { return "ao3" }

func (ao3) Match(u *url.URL) (string, bool) {
	if strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") != "archiveofourown.org" {
		return "", false
	}
	m := reAO3Work.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	s := ao3Base + "/works/" + m[1]
	if u.Query().Get("view_adult") == "true" {
		s += "?view_adult=true"
	}
	return s, true
}

// withAdult keeps the owner's "view adult content" choice on every request
// of a work where it was given.
func withAdult(serial, raw string) string {
	if !strings.Contains(serial, "view_adult=true") || strings.Contains(raw, "view_adult=") {
		return raw
	}
	if strings.Contains(raw, "?") {
		return raw + "&view_adult=true"
	}
	return raw + "?view_adult=true"
}

func adultWarning(gq *goquery.Document, work string) error {
	if c := gq.Find("p.caution").First(); c.Length() > 0 && strings.Contains(strings.ToLower(c.Text()), "adult content") {
		return errAdult{work: work, text: text(c)}
	}
	return nil
}

func (ao3) Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error) {
	work := strings.SplitN(serial, "?", 2)[0]
	gq, base, err := ao3Page(ctx, f, serial, true)
	if err != nil {
		return nil, err
	}
	if err := adultWarning(gq, work); err != nil {
		return nil, err
	}
	s := &Serial{Kind: "ao3", URL: serial, Title: text(gq.Find("h2.title").First()),
		Author:  text(gq.Find(`h3.byline a[rel="author"]`).First()),
		Summary: text(gq.Find(".summary .userstuff").First())}
	nav, nbase, err := ao3Page(ctx, f, withAdult(serial, work+"/navigate"), true)
	if err != nil {
		return nil, err
	}
	nav.Find("ol.chapter.index li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a").First()
		href, _ := a.Attr("href")
		if href == "" {
			return
		}
		c := Chapter{Title: text(a), URL: withAdult(serial, resolve(nbase, href))}
		dt := strings.Trim(text(li.Find(".datetime")), "()")
		if t, err := time.Parse("2006-01-02", dt); err == nil {
			c.Published = t
		}
		s.Chapters = append(s.Chapters, c)
	})
	if len(s.Chapters) == 0 && gq.Find("#chapters .userstuff").Length() > 0 {
		s.Chapters = []Chapter{{Title: s.Title, URL: withAdult(serial, resolve(base, work))}}
	}
	if s.Title == "" || len(s.Chapters) == 0 {
		return nil, errors.New("AO3: no chapter list found — the site layout may have changed")
	}
	return s, nil
}

func (ao3) Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error) {
	gq, base, err := ao3Page(ctx, f, ch.URL, false)
	if err != nil {
		return nil, err
	}
	if err := adultWarning(gq, ch.URL); err != nil {
		return nil, err
	}
	body := gq.Find("#chapters .userstuff.module, #chapters > .userstuff").First()
	if body.Length() == 0 {
		return nil, errors.New("AO3: no chapter text found — the site layout may have changed")
	}
	body.Find("h3.landmark").Remove()
	d := &doc.Document{Lang: "en"}
	if t := text(gq.Find("#chapters .chapter.preface h3.title").First()); t != "" {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: t}}})
	}
	gq.Find("#chapters .chapter.preface .notes .userstuff").Each(func(_ int, n *goquery.Selection) {
		d.Blocks = append(d.Blocks, folded("Notes", blocksOf(d, n, base)))
	})
	d.Blocks = append(d.Blocks, blocksOf(d, body, base)...)
	gq.Find("#chapters .chapter.preface ~ .end.notes .userstuff, #chapters .end.notes .userstuff").Each(func(_ int, n *goquery.Selection) {
		d.Blocks = append(d.Blocks, folded("End notes", blocksOf(d, n, base)))
	})
	return d, nil
}

// adultDoc shows AO3's own warning; only the owner's choice continues.
func adultDoc(e errAdult) *doc.Document {
	d := &doc.Document{Title: "AO3 — adult content warning", URL: "w5f:fiction/ao3/page?" + url.Values{"u": {e.work}}.Encode(), Origin: "live", Lang: "en"}
	d.Blocks = []doc.Block{
		doc.Notice{Kind: "warn", Text: e.text},
		para(doc.Span{Text: "Proceed (show adult content)", Style: doc.Bold, Link: link(d, openHref(withAdult("view_adult=true", strings.SplitN(e.work, "?", 2)[0])), "proceed")}),
		para(plain("Press backspace to go back.", doc.Italic)),
	}
	return d
}

// ao3Search lists works matching words (AO3's own work search).
func ao3Search(ctx context.Context, f *fetch.Fetcher, q string) (*doc.Document, error) {
	page := ao3Base + "/works/search?" + url.Values{"work_search[query]": {q}}.Encode()
	gq, base, err := ao3Page(ctx, f, page, false)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "AO3: " + q, URL: page, Origin: "live", Lang: "en"}
	var items [][]doc.Block
	gq.Find("li.work.blurb").Each(func(_ int, it *goquery.Selection) {
		a := it.Find("h4.heading a").First()
		href, _ := a.Attr("href")
		if href == "" {
			return
		}
		in := doc.Inline{{Text: text(a), Style: doc.Bold, Link: link(d, openHref(resolve(base, href)), text(a))}}
		if by := text(it.Find(`h4.heading a[rel="author"]`)); by != "" {
			in = append(in, plain("  by "+by, doc.Italic))
		}
		blocks := []doc.Block{para(in...)}
		if sum := text(it.Find("blockquote.summary").First()); sum != "" {
			blocks = append(blocks, para(plain(sum, 0)))
		}
		if stats := text(it.Find("dl.stats").First()); stats != "" {
			blocks = append(blocks, para(plain(stats, doc.Italic)))
		}
		items = append(items, blocks)
	})
	if len(items) == 0 {
		d.Blocks = []doc.Block{para(plain("No works found.", doc.Italic))}
		return d, nil
	}
	d.Blocks = []doc.Block{doc.List{Ordered: true, Items: items}}
	return d, nil
}
