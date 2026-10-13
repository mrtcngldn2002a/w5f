package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// Nature Futures stories from before 2018 are behind the journal's paywall
// (its page shows a preview and "Buy this article"), but the Internet
// Archive's copies of them are whole, from the years the stories were free.
// A draw of one is "w5f:discover/futures?id=<id>&t=<title>"; the page is
// built from the best copy the Archive has (futuresStory).

// waybackBase is the Internet Archive's Wayback Machine.
var waybackBase = "https://web.archive.org"

var (
	errNoCopy      = errors.New("no readable archived copy")
	errArchiveDown = errors.New("the Internet Archive is not answering")
)

var (
	// An id of the journal's own numbering: volume (3 digits), page, "a".
	reFuturesLegacy = regexp.MustCompile(`^(\d{3})\d+a$`)
	reFuturesID     = regexp.MustCompile(`^\d[0-9a-z]{3,12}$`)
)

const (
	futuresMinWords = 250 // fewer is a preview, not a story
	futuresMaxWords = 3500
)

// archiveCopy is one capture of a page: when, and of which address.
type archiveCopy struct{ stamp, orig string }

// replay is the capture as it was served, without the Archive's own frame.
func (c archiveCopy) replay() string { return waybackBase + "/web/" + c.stamp + "id_/" + c.orig }

// view is the capture in the Archive's own page.
func (c archiveCopy) view() string { return waybackBase + "/web/" + c.stamp + "/" + c.orig }

// waybackCopies lists the captures that match q (the Archive's timemap, which
// answered while its CDX search was down). An answer that is not a list is the
// Archive being down, not a page without captures.
func waybackCopies(ctx context.Context, f *fetch.Fetcher, q url.Values) ([]archiveCopy, error) {
	q.Set("fl", "timestamp,original")
	q.Set("collapse", "timestamp:6")
	q.Add("filter", "statuscode:200")
	u, _ := url.Parse(waybackBase + "/web/timemap/json?" + q.Encode())
	resp, err := f.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", errArchiveDown, err)
	}
	body := bytes.TrimSpace(resp.Body)
	if len(body) == 0 {
		return nil, nil
	}
	var rows [][]string
	if body[0] != '[' || json.Unmarshal(body, &rows) != nil {
		return nil, errArchiveDown
	}
	var out []archiveCopy
	for i, r := range rows {
		if i > 0 && len(r) == 2 {
			out = append(out, archiveCopy{r[0], r[1]})
		}
	}
	return out, nil
}

// futuresCopies lists the captures of a story worth trying, best first: the
// journal's old address (2008–2017) where the id has one, newest first, then
// the present address, whose newest captures are often a paywalled or empty
// page, so those from before 2025 come first.
func futuresCopies(ctx context.Context, f *fetch.Fetcher, id string) ([]archiveCopy, error) {
	var out []archiveCopy
	if m := reFuturesLegacy.FindStringSubmatch(id); m != nil {
		old, err := waybackCopies(ctx, f, url.Values{
			"url":       {"nature.com/nature/journal/v" + m[1] + "/"},
			"matchType": {"prefix"},
			"filter":    {"original:.*full/" + id + `\.html.*`},
		})
		if err != nil {
			return nil, err
		}
		for i := len(old) - 1; i >= 0 && len(out) < 2; i-- {
			out = append(out, old[i])
		}
	}
	cur, err := waybackCopies(ctx, f, url.Values{"url": {"nature.com/articles/" + id}})
	if err != nil {
		return nil, err
	}
	var late []archiveCopy
	taken := 0
	for i := len(cur) - 1; i >= 0 && taken < 3; i-- {
		if cur[i].stamp >= "2025" {
			late = append(late, cur[i])
			continue
		}
		out = append(out, cur[i])
		taken++
	}
	out = append(out, late...)
	if len(out) > 6 {
		out = out[:6]
	}
	return out, nil
}

type futuresStory struct {
	title, author, date, standfirst string
	paras                           []string
	from                            archiveCopy
}

// futuresStoryFor reads the story from the first capture that holds it whole.
func futuresStoryFor(ctx context.Context, f *fetch.Fetcher, id string) (*futuresStory, error) {
	if !reFuturesID.MatchString(id) {
		return nil, errors.New("not a story address: " + id)
	}
	copies, err := futuresCopies(ctx, f, id)
	if err != nil {
		return nil, err
	}
	var errs []string
	for _, c := range copies {
		s, err := futuresRead(ctx, f, c)
		if err == nil {
			return s, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errs = append(errs, c.stamp[:6]+": "+err.Error())
	}
	if len(errs) == 0 {
		return nil, errNoCopy
	}
	return nil, fmt.Errorf("%w (%s)", errNoCopy, strings.Join(errs, "; "))
}

func futuresRead(ctx context.Context, f *fetch.Fetcher, c archiveCopy) (*futuresStory, error) {
	u, err := url.Parse(c.replay())
	if err != nil {
		return nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	r, err := charset.NewReader(bytes.NewReader(resp.Body), resp.ContentType)
	if err != nil {
		return nil, err
	}
	gq, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	s := parseFuturesPage(gq)
	s.from = c
	if n := wordCount(s.paras); n < futuresMinWords || n > futuresMaxWords {
		return nil, fmt.Errorf("%d words, not a whole story", n)
	}
	return s, nil
}

func wordCount(paras []string) int {
	n := 0
	for _, p := range paras {
		n += len(strings.Fields(p))
	}
	return n
}

// parseFuturesPage takes a Nature article page apart. Nature has had three
// layouts over the years: the present one (c-article-body), the one of
// 2015–2017 (<article>), and the one before that (articlebody / articleCol).
func parseFuturesPage(gq *goquery.Document) *futuresStory {
	meta := func(names ...string) string {
		for _, n := range names {
			if v, ok := gq.Find(`meta[name="` + n + `"]`).First().Attr("content"); ok && strings.TrimSpace(v) != "" {
				return strings.Join(strings.Fields(v), " ")
			}
		}
		return ""
	}
	s := &futuresStory{title: meta("citation_title", "dc.title"), date: meta("dc.date", "citation_online_date")}
	var authors []string
	gq.Find(`meta[name="citation_author"]`).Each(func(_ int, m *goquery.Selection) {
		if v, _ := m.Attr("content"); strings.TrimSpace(v) != "" {
			authors = append(authors, strings.Join(strings.Fields(v), " "))
		}
	})
	s.author = strings.Join(authors, ", ")
	if s.author == "" {
		s.author = meta("dc.creator")
	}
	if sf := meta("description", "dc.description"); len([]rune(sf)) <= 200 {
		s.standfirst = sf
	}
	if s.standfirst == "" { // the one line under the title, in the page itself
		s.standfirst = strings.Join(strings.Fields(gq.Find("#abs p.lead, .c-article-section__content--standfirst p").First().Text()), " ")
	}

	var ps *goquery.Selection
	switch {
	case gq.Find(".c-article-body").Length() > 0:
		body := gq.Find(".c-article-body").First()
		if main := body.Find(".main-content").First(); main.Length() > 0 {
			body = main
		}
		ps = body.Find("p")
	case gq.Find("article div.section.second > div.content").Length() > 0:
		ps = gq.Find("article div.section.second > div.content").First().Children().Filter("p")
	case gq.Find("#articlebody").Length() > 0:
		ps = gq.Find("#articlebody").First().Children().Filter("p")
	default:
		ps = gq.Find("div.articleCol").First().Children().Filter("p")
	}
	ps.Each(func(_ int, p *goquery.Selection) {
		if id, _ := p.Attr("id"); id == "aug" || id == "errorcor" {
			return
		}
		if p.HasClass("credit") || p.Closest(`figure, .illustration, .image-full, [class*="__illustration"], [class*="__figure"]`).Length() > 0 {
			return
		}
		if t := strings.Join(strings.Fields(p.Text()), " "); t != "" {
			s.paras = append(s.paras, t)
		}
	})
	return s
}

// futuresRoute builds the page a draw promised.
func futuresRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	q, err := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/futures?"))
	if err != nil {
		return nil, err
	}
	id := q.Get("id")
	s, err := futuresStoryFor(ctx, env.Fetcher, id)
	if err != nil {
		return nil, err
	}
	title := s.title
	if title == "" {
		title = q.Get("t")
	}
	d := &doc.Document{URL: target, Title: title, Lang: "en", Origin: "archive"}
	by := []string{"Nature Futures"}
	if s.author != "" {
		by = append([]string{s.author}, by...)
	}
	if s.date != "" {
		by = append(by, s.date)
	}
	d.Byline = strings.Join(by, " · ")
	d.Links = []doc.Link{
		{Href: s.from.view(), Text: "The Internet Archive's copy"},
		{Href: "https://www.nature.com/articles/" + id, Text: "On Nature (paywalled)"},
	}
	if s.standfirst != "" {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: s.standfirst, Style: doc.Italic}}})
	}
	for _, p := range s.paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{
		{Text: "The Internet Archive's copy", Link: 1}, {Text: "  ·  "}, {Text: "On Nature (paywalled)", Link: 2}}})
	return d, nil
}
