package discover

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// The almanac's sources besides the Book of Days (chosen with the owner,
// 2026-10-02, in place of Wikipedia's events and the Turkish ones):
// Britannica's On This Day, written by its editors, and Ian Ellis's Today in
// Science History, with the scientists born and dead that day and the day's
// events in science.

var (
	britannicaOTD = "https://www.britannica.com/on-this-day/%s-%d" // October-3
	todayInSci    = "https://todayinsci.com/%d/%d_%02d.htm"        // 10/10_03.htm
)

// BritannicaURL and ScienceURL are a day's pages on the two sites.
func BritannicaURL(m time.Month, d int) string { return fmt.Sprintf(britannicaOTD, m.String(), d) }
func ScienceURL(m time.Month, d int) string    { return fmt.Sprintf(todayInSci, int(m), int(m), d) }

var (
	reAside    = regexp.MustCompile(`\s*\[[^\]]*\]\.?`) // "[Take our actors quiz.]"
	reThisDay  = regexp.MustCompile(`this day in (\d{3,4})`)
	reYear     = regexp.MustCompile(`\b(\d{3,4})\b`)
	reInYear   = regexp.MustCompile(`^In (\d{3,4})\b`)
	reNotSpace = regexp.MustCompile(`\s+`)
)

func cleanText(s string) string {
	return strings.TrimSpace(reAside.ReplaceAllString(reNotSpace.ReplaceAllString(strings.ReplaceAll(s, " ", " "), " "), ""))
}

// britannicaDay reads Britannica's page for a day: the day's story, the
// featured event, the further events, and the famous birthdays. Each year
// is the small box before its headline, link or text.
func britannicaDay(ctx context.Context, f *fetch.Fetcher, m time.Month, d int) (events, births []AlmanacItem, err error) {
	gq, base, err := get(ctx, f, BritannicaURL(m, d))
	if err != nil {
		return nil, nil, err
	}
	abs := func(href string) string {
		if u, err := base.Parse(strings.TrimSpace(href)); err == nil && href != "" {
			return u.String()
		}
		return BritannicaURL(m, d)
	}
	gq.Find("div.font-oswald").Each(func(_ int, y *goquery.Selection) {
		year, err := strconv.Atoi(strings.TrimSpace(y.Text()))
		if err != nil {
			return
		}
		next := y.Next()
		switch {
		case goquery.NodeName(next) == "a": // a birthday: the name, then who they were
			births = append(births, AlmanacItem{Year: year, Title: cleanText(next.Text()), Text: cleanText(next.Next().Text()), Target: abs(next.AttrOr("href", "")), Lang: "en"})
		case next.HasClass("font-lora"): // the day's story: headline, text, "read more"
			more := y.Parent().Find(`a[href*="/today-in-history/"]`).First()
			events = append(events, AlmanacItem{Year: year, Title: cleanText(next.Text()), Text: cleanText(next.Next().Text()), Target: abs(more.AttrOr("href", "")), Lang: "en"})
		case cleanText(next.Text()) != "":
			events = append(events, AlmanacItem{Year: year, Text: cleanText(next.Text()), Target: abs(next.Find("a[href]").First().AttrOr("href", "")), Lang: "en"})
		}
	})
	gq.Find("div.day-in-history-card").Each(func(_ int, card *goquery.Selection) {
		if !strings.Contains(card.Prev().Text(), "Featured Event") {
			return // the featured biography is left out
		}
		var texts []string
		card.Children().Each(func(_ int, c *goquery.Selection) {
			if c.Find("img").Length() == 0 && !strings.Contains(c.AttrOr("class", ""), "italic") {
				if t := cleanText(c.Text()); t != "" {
					texts = append(texts, t)
				}
			}
		})
		if len(texts) < 2 {
			return
		}
		e := AlmanacItem{Title: texts[len(texts)-2], Text: texts[len(texts)-1], Target: BritannicaURL(m, d), Lang: "en"}
		if mm := reThisDay.FindStringSubmatch(e.Text); mm != nil {
			e.Year, _ = strconv.Atoi(mm[1])
		}
		events = append(events, e)
	})
	if len(events) == 0 && len(births) == 0 {
		return nil, nil, errors.New("Britannica: nothing found on the day's page")
	}
	return events, births, nil
}

// scienceDay reads Today in Science History's page for a day: its births,
// deaths and events, each under a name heading, in sections marked by
// anchors ("birth", "death", "event").
func scienceDay(ctx context.Context, f *fetch.Fetcher, m time.Month, d int) ([]AlmanacItem, error) {
	src := ScienceURL(m, d)
	gq, _, err := get(ctx, f, src)
	if err != nil {
		return nil, err
	}
	kinds := map[string]string{"birth": "born", "death": "died", "event": "event"}
	section := ""
	var out []AlmanacItem
	gq.Find("div.daysubheading, div.daynameheading").Each(func(_ int, s *goquery.Selection) {
		if s.HasClass("daysubheading") {
			section = kinds[s.Find("a[name]").AttrOr("name", "")]
			return
		}
		if section == "" {
			return
		}
		box := s.Next().Find(`div[style*="margin-left"]`).First()
		box.Find(".footnote, .noprint, .bookline, script").Remove()
		html, err := box.Html()
		if err != nil || strings.TrimSpace(html) == "" {
			return
		}
		// Births and deaths start with their date line, then a <br>.
		head, body := "", html
		if section != "event" {
			if i := strings.Index(strings.ToLower(html), "<br"); i >= 0 {
				head, body = html[:i], html[i:]
				if j := strings.Index(body, ">"); j >= 0 {
					body = body[j+1:]
				}
			}
		}
		text := fragmentText(body)
		item := AlmanacItem{Title: cleanText(s.Text()), Text: text, Kind: section, Lang: "en", Target: src}
		if a := s.Find("a[name]").AttrOr("name", ""); a != "" {
			item.Target += "#" + a
		}
		if section == "event" {
			if mm := reInYear.FindStringSubmatch(text); mm != nil {
				item.Year, _ = strconv.Atoi(mm[1])
			}
		} else if mm := reYear.FindStringSubmatch(fragmentText(head)); mm != nil {
			item.Year, _ = strconv.Atoi(mm[1])
		}
		if item.Title != "" && item.Text != "" {
			out = append(out, item)
		}
	})
	if len(out) == 0 {
		return nil, errors.New("Today in Science History: nothing found on the day's page")
	}
	return out, nil
}

func fragmentText(html string) string {
	gq, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + html + "</div>"))
	if err != nil {
		return ""
	}
	return cleanText(gq.Text())
}

// pickScience takes one of each kind (born, died, event) at random.
func pickScience(items []AlmanacItem) []AlmanacItem {
	var out []AlmanacItem
	for _, k := range []string{"born", "died", "event"} {
		var of []AlmanacItem
		for _, it := range items {
			if it.Kind == k {
				of = append(of, it)
			}
		}
		if len(of) > 0 {
			out = append(out, of[rand.IntN(len(of))])
		}
	}
	return out
}

// pickBritannica keeps the day's story and the featured event (the first
// two found, when there), and two more events at random, in year order.
func pickBritannica(events []AlmanacItem) []AlmanacItem {
	var lead, rest []AlmanacItem
	for _, e := range events {
		if e.Title != "" {
			lead = append(lead, e)
		} else {
			rest = append(rest, e)
		}
	}
	for _, i := range rand.Perm(len(rest))[:min(2, len(rest))] {
		lead = append(lead, rest[i])
	}
	sortEvents(lead)
	return lead
}
