package ultan

import (
	"fmt"
	"sort"
	"time"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// LedgerTarget is Ultan's ledger: the owner's reading, counted.
const LedgerTarget = "w5f:ledger"

// kindRooms names the kinds of pages the history records.
var kindRooms = []struct{ kind, label string }{
	{"feed", "Periodicals"}, {"book", "Books"}, {"fiction", "Serials and forum stories"},
	{"scp", "SCP and its sister wikis"}, {"wiki", "Other wikis"}, {"reddit", "Reddit"},
	{"smallweb", "Gemini and Gopher"}, {"web", "The open web"},
}

// Ledger is the page of Ultan's ledger.
func Ledger(db *store.DB, now time.Time) (*doc.Document, error) {
	d := &doc.Document{Title: "Ultan's Ledger", URL: LedgerTarget, Origin: "local", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	d.Blocks = append(d.Blocks, Note{Text: "I keep this ledger by touch, a notch for every page you open. The numbers are yours; the hand is mine."}.Blocks(link)...)

	all, err := db.VisitLog(time.Time{})
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "The ledger is empty: nothing has been read yet."}}})
		return d, nil
	}
	spans := []struct {
		label string
		since time.Time
	}{{"7 days", now.AddDate(0, 0, -7)}, {"30 days", now.AddDate(0, 0, -30)}, {"since " + all[0].At.Format("2 Jan 2006"), time.Time{}}}
	count := func(since time.Time, kind string) (opens, pages, days int) {
		seenPage, seenDay := map[string]bool{}, map[string]bool{}
		for _, v := range all {
			if v.At.Before(since) || (kind != "" && v.Kind != kind) {
				continue
			}
			opens++
			seenPage[v.Target] = true
			seenDay[v.At.Format("2006-01-02")] = true
		}
		return opens, len(seenPage), len(seenDay)
	}
	row := func(label string, f func(time.Time) int) []doc.Inline {
		r := []doc.Inline{{{Text: label}}}
		for _, s := range spans {
			r = append(r, doc.Inline{{Text: fmt.Sprint(f(s.since))}})
		}
		return r
	}
	head := []doc.Inline{{{Text: ""}}}
	for _, s := range spans {
		head = append(head, doc.Inline{{Text: s.label, Style: doc.Bold}})
	}
	rows := [][]doc.Inline{head,
		row("Days at the desk", func(t time.Time) int { _, _, n := count(t, ""); return n }),
		row("Pages opened", func(t time.Time) int { n, _, _ := count(t, ""); return n }),
		row("Different pages", func(t time.Time) int { _, n, _ := count(t, ""); return n }),
	}
	for _, k := range kindRooms {
		if n, _, _ := count(time.Time{}, k.kind); n > 0 {
			kind := k.kind
			rows = append(rows, row("  "+k.label, func(t time.Time) int { _, n, _ := count(t, kind); return n }))
		}
	}
	d.Blocks = append(d.Blocks,
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "The count"}}},
		doc.Table{Header: true, Rows: rows},
		doc.Paragraph{Text: doc.Inline{{Text: "By kind, the different pages of each.", Style: doc.Italic}}},
	)

	// The pages most returned to.
	if hs, err := db.History(0, 0); err == nil {
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].Opens > hs[j].Opens })
		var items [][]doc.Block
		for _, h := range hs {
			if len(items) == 5 || h.Opens < 2 {
				break
			}
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{
				{Text: h.Title, Link: link(h.Target, h.Title)}, {Text: fmt.Sprintf("  %d times, last %s", h.Opens, h.Last.Format("2 Jan")), Style: doc.Italic}}}})
		}
		if len(items) > 0 {
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Most returned to"}}}, doc.List{Items: items})
		}
	}
	// What lies open.
	if vs, err := db.Unfinished(5); err == nil && len(vs) > 0 {
		var items [][]doc.Block
		for _, v := range vs {
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{
				{Text: v.Title, Link: link(v.Target, v.Title)},
				{Text: fmt.Sprintf("  %d%%, untouched %s", int(v.Pos*100+0.5), untouched(now.Sub(v.Last))), Style: doc.Italic}}}})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Left open"}}}, doc.List{Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{})
	d.Blocks = append(d.Blocks, Note{Text: pick(sayings, day(now)+1)}.Blocks(link)...)
	return d, nil
}

func untouched(d time.Duration) string {
	switch n := days(d); n {
	case 0:
		return "since today"
	case 1:
		return "one day"
	default:
		return words(n) + " days"
	}
}
