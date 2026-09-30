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
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// The almanac (chosen with the owner, 2026-09-30): the day's chapter of
// Chambers's Book of Days (1864) and a few of Wikipedia's events for the
// date, shown as a column on the Daily Packet's cover.

// Almanac is one day's column.
type Almanac struct {
	Day       string        `json:"day"`       // "09-30"
	Headlines []string      `json:"headlines"` // the Book of Days' articles that day
	Born      string        `json:"born"`      // its "Born:" line, shortened
	Events    []AlmanacItem `json:"events"`
}

// AlmanacItem is one of Wikipedia's events.
type AlmanacItem struct {
	Year   int    `json:"year"`
	Text   string `json:"text"`
	Target string `json:"target"`
	Lang   string `json:"lang"`
}

var (
	bookOfDaysBase = "https://www.thebookofdays.com/months/"
	wikiOnThisDay  = "https://%s.wikipedia.org/api/rest_v1/feed/onthisday/%s/%02d/%02d"
	bodMonths      = []string{"jan", "feb", "march", "april", "may", "june", "july", "aug", "sept", "oct", "nov", "dec"}
)

// dayOf reads "MM-DD".
func dayOf(day string) (time.Month, int, error) {
	t, err := time.Parse("01-02", day)
	if err != nil {
		return 0, 0, errors.New("bad day " + day)
	}
	return t.Month(), t.Day(), nil
}

func bookOfDaysURL(m time.Month, d int) string {
	return fmt.Sprintf("%s%s/%d.htm", bookOfDaysBase, bodMonths[m-1], d)
}

// AlmanacHref is the reader's address of a day's Book of Days chapter.
func AlmanacHref(day string) string { return "w5f:almanac/" + day }

// The site's pages lost their dashes and pound signs to U+FFFD long ago:
// before a digit it was a pound sign, elsewhere a dash.
var reLostPound = regexp.MustCompile("�(\\d)")

func mendText(s string) string {
	s = reLostPound.ReplaceAllString(s, "£$1")
	return strings.ReplaceAll(s, "�", "—")
}

var (
	reBodChrome = regexp.MustCompile(`(?i)^(site map|back to top\W*|home .*|.*about: the book of days.*|.*proudly brought to you.*|(january|february|march|april|may|june|july|august|september|october|november|december) \d{1,2}(st|nd|rd|th))$`)
	reBodLabel  = regexp.MustCompile(`^(Born|Died|Feast Day)\s*:\s*`)
)

type bodPart struct {
	heading bool
	label   string // Born / Died / Feast Day
	text    string
	next    string // a long day's next part ("1a")
}

var reBodPart = regexp.MustCompile(`^(\d{1,2}[a-z]?)\.htm$`) // "1.htm" is the first part

// bookOfDaysParts reads a day's page: its paragraphs, with the bold capital
// ones as headings (the site's own FrontPage markup confuses general article
// extraction into one paragraph).
func bookOfDaysParts(body []byte) ([]bodPart, error) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var parts []bodPart
	gq.Find("p").Each(func(_ int, p *goquery.Selection) {
		text := mendText(strings.Join(strings.Fields(strings.ReplaceAll(p.Text(), " ", " ")), " "))
		if text == "" || reBodChrome.MatchString(text) {
			return // the site's menu, its title and the links to other days
		}
		if href, ok := p.Find("a[href]").First().Attr("href"); ok && strings.HasPrefix(text, "Part ") {
			if m := reBodPart.FindStringSubmatch(strings.TrimSpace(href)); m != nil {
				parts = append(parts, bodPart{text: text, next: m[1]})
				return
			}
		}
		if m := reBodLabel.FindStringSubmatch(text); m != nil {
			parts = append(parts, bodPart{label: m[1], text: strings.TrimSpace(text[len(m[0]):])})
			return
		}
		bold := strings.Join(strings.Fields(p.Find("b").Text()), " ")
		if bold != "" && bold == text && len(text) < 90 && strings.ToUpper(text) == text && strings.IndexFunc(text, unicode.IsLetter) >= 0 {
			parts = append(parts, bodPart{heading: true, text: text})
			return
		}
		parts = append(parts, bodPart{text: text})
	})
	if len(parts) == 0 {
		return nil, errors.New("The Book of Days: nothing on this day's page")
	}
	return parts, nil
}

// titleCase turns the site's capital headings into ordinary titles.
func titleCase(s string) string {
	small := map[string]bool{"of": true, "the": true, "and": true, "for": true, "in": true, "on": true, "at": true, "a": true, "an": true, "to": true, "by": true}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if i > 0 && small[w] {
			continue
		}
		r := []rune(w)
		for j, c := range r {
			if unicode.IsLetter(c) {
				r[j] = unicode.ToUpper(c)
				break
			}
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndexAny(cut, ";,"); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + " …"
}

// buildAlmanac gathers a day's column; either half may be missing.
func buildAlmanac(ctx context.Context, f *fetch.Fetcher, m time.Month, d int) (*Almanac, error) {
	a := &Almanac{Day: fmt.Sprintf("%02d-%02d", int(m), d)}
	var errs []string
	if u, err := url.Parse(bookOfDaysURL(m, d)); err == nil {
		if resp, err := f.Get(ctx, u, fetch.Options{}); err != nil {
			errs = append(errs, err.Error())
		} else if parts, err := bookOfDaysParts(resp.Body); err != nil {
			errs = append(errs, err.Error())
		} else {
			for _, p := range parts {
				switch {
				case p.heading && len(a.Headlines) < 4:
					a.Headlines = append(a.Headlines, titleCase(p.text))
				case p.label == "Born" && a.Born == "":
					a.Born = shorten(p.text, 150)
				}
			}
		}
	}
	// Each Wikipedia's own selection for the day; its full list of events
	// (crimes and disasters among them) only when there is no selection.
	for _, lang := range []struct {
		code string
		n    int
	}{{"en", 3}, {"tr", 2}} {
		items, err := wikiEvents(ctx, f, lang.code, "selected", m, d)
		if err != nil {
			items, err = wikiEvents(ctx, f, lang.code, "events", m, d)
		}
		if err != nil {
			errs = append(errs, "Wikipedia "+lang.code+": "+err.Error())
			continue
		}
		for _, i := range rand.Perm(len(items))[:min(lang.n, len(items))] {
			a.Events = append(a.Events, items[i])
		}
	}
	if len(a.Headlines) == 0 && a.Born == "" && len(a.Events) == 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	sortEvents(a.Events)
	return a, nil
}

func sortEvents(es []AlmanacItem) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].Year < es[j-1].Year; j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}

// rePictured drops the notes about the picture on Wikipedia's main page.
var rePictured = regexp.MustCompile(`(?i)\s*\((?:[^()]*\s)?(?:pictured|resimde|görselde)\)`)

// wikiEvents reads Wikipedia's "on this day" feed (kind: selected, events).
func wikiEvents(ctx context.Context, f *fetch.Fetcher, lang, kind string, m time.Month, d int) ([]AlmanacItem, error) {
	u, _ := url.Parse(fmt.Sprintf(wikiOnThisDay, lang, kind, int(m), d))
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	var r map[string][]struct {
		Text  string `json:"text"`
		Year  int    `json:"year"`
		Pages []struct {
			ContentURLs struct {
				Desktop struct {
					Page string `json:"page"`
				} `json:"desktop"`
			} `json:"content_urls"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return nil, errors.New("unexpected answer")
	}
	var out []AlmanacItem
	for _, e := range r[kind] {
		text := strings.Join(strings.Fields(rePictured.ReplaceAllString(e.Text, "")), " ")
		if text == "" || len(e.Pages) == 0 || e.Pages[0].ContentURLs.Desktop.Page == "" {
			continue
		}
		out = append(out, AlmanacItem{Year: e.Year, Text: text, Target: e.Pages[0].ContentURLs.Desktop.Page, Lang: lang})
	}
	if len(out) == 0 {
		return nil, errors.New("no events")
	}
	return out, nil
}

// almanacDoc is the day's Book of Days chapter in the reader; address
// "MM-DD", or "MM-DD/1a" for the further parts of a long day.
func almanacDoc(ctx context.Context, env Env, day string) (*doc.Document, error) {
	day, part, _ := strings.Cut(day, "/")
	m, d, err := dayOf(day)
	if err != nil {
		return nil, err
	}
	src := bookOfDaysURL(m, d)
	if part != "" {
		if !reBodPart.MatchString(part + ".htm") {
			return nil, errors.New("bad almanac part " + part)
		}
		src = fmt.Sprintf("%s%s/%s.htm", bookOfDaysBase, bodMonths[m-1], part)
	}
	u, _ := url.Parse(src)
	resp, err := env.Fetcher.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	parts, err := bookOfDaysParts(resp.Body)
	if err != nil {
		return nil, err
	}
	date := time.Date(2000, m, d, 0, 0, 0, 0, time.UTC).Format("2 January")
	out := &doc.Document{Title: "The Book of Days — " + date, URL: AlmanacHref(day), Origin: "live", Lang: "en",
		Meta: []doc.KV{{Key: "from", Value: "Robert Chambers, The Book of Days (1864), via thebookofdays.com"}}}
	if part != "" {
		out.URL += "/" + part
	}
	out.Links = append(out.Links, doc.Link{Href: src, Text: "the page on thebookofdays.com"})
	for _, p := range parts {
		switch {
		case p.next != "":
			href, arrow := AlmanacHref(day)+"/"+p.next, " ›"
			if p.next == strconv.Itoa(d) { // back to the first part
				href, arrow = AlmanacHref(day), ""
				out.Prev = href
			} else {
				out.Next = href
			}
			out.Links = append(out.Links, doc.Link{Href: href, Text: p.text})
			out.Blocks = append(out.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p.text + arrow, Style: doc.Bold, Link: len(out.Links)}}})
		case p.heading:
			out.Blocks = append(out.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: titleCase(p.text)}}})
		case p.label != "":
			out.Blocks = append(out.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p.label + ": ", Style: doc.Bold}, {Text: p.text}}})
		default:
			out.Blocks = append(out.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p.text}}})
		}
	}
	out.Blocks = append(out.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "Source: ", Style: doc.Italic}, {Text: src, Link: 1}}})
	return out, nil
}
