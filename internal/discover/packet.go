package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
	"w5f/internal/ultan"
)

// Packet is one day's issue of the Daily Packet.
type Packet struct {
	Date    string  `json:"date"` // 2026-09-29
	Number  int     `json:"number"`
	Entries []Entry `json:"entries"`
	// The cover's columns (from v0.9.2; older issues have none).
	Almanac *Almanac `json:"almanac,omitempty"`
	Oracle  *Oracle  `json:"oracle,omitempty"`
}

// Entry is one item of an issue.
type Entry struct {
	Kind   string `json:"kind"` // periodical, weird, esoteric, public, archive, queue
	Title  string `json:"title"`
	Target string `json:"target"`
	Source string `json:"source"` // feed id, family detail
}

// packetDraws are the rotating sections of an issue (replaced in tests).
var packetDraws = map[string]func(context.Context, Env) (Draw, error){
	"weird":        weird{}.Draw,
	"fiction":      fiction{}.Draw,
	"esoteric":     esoteric{}.Draw,
	"folklore":     folklore{}.Draw,
	"knowledge":    knowledge{}.Draw,
	"encyclopedic": encyclopedic{}.Draw,
	"public":       publicDomainDraw,
	"archive":      textfiles{}.Draw,
	"smallweb":     smallwebFamily{}.Draw,
}

var roman = []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"}

func today() string { return time.Now().Format("2006-01-02") }

func loadPacket(db *store.DB, date string) (Packet, bool) {
	var p Packet
	if json.Unmarshal([]byte(db.Get("packet:"+date)), &p) != nil || p.Date == "" {
		return p, false
	}
	return p, true
}

// todayPacket returns today's issue, building it on first use.
func todayPacket(env Env) (Packet, error) {
	if p, ok := loadPacket(env.DB, today()); ok {
		// An issue made before the almanac and the oracle existed gets them.
		if p.Almanac == nil && p.Oracle == nil {
			addColumns(context.Background(), env, &p)
			_ = savePacket(env.DB, p)
		}
		return p, nil
	}
	n, _ := strconv.Atoi(env.DB.Get("packet:count"))
	p := buildPacket(context.Background(), env, today(), n+1)
	if err := savePacket(env.DB, p); err != nil {
		return p, err
	}
	return p, env.DB.Set("packet:count", strconv.Itoa(n+1))
}

func savePacket(db *store.DB, p Packet) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return db.Set("packet:"+p.Date, string(b))
}

// buildPacket assembles an issue: three unread periodicals from three
// shelves, four rotating sections of the Deep Random shelves, and the first
// open item of the reading queue; none of it shown in the last 30 days (see
// packetpick.go). A missing part is skipped.
func buildPacket(ctx context.Context, env Env, date string, number int) Packet {
	p := Packet{Date: date, Number: number}
	// The cover's columns are gathered alongside the entries.
	columns := make(chan Packet, 1)
	go func() {
		c := Packet{Date: date}
		addColumns(ctx, env, &c)
		columns <- c
	}()
	m := loadMemory(env.DB, date)
	for _, it := range pickPeriodicals(env.DB, env.Shelves, m, 3) {
		p.Entries = append(p.Entries, Entry{Kind: "periodical", Title: it.Title, Target: itemTarget(it.ID), Source: it.FeedID})
	}
	p.Entries = append(p.Entries, drawSections(ctx, env, m)...)
	if q, err := personal.LoadQueue(); err == nil {
		for _, e := range q.Entries() {
			if !e.Done {
				p.Entries = append(p.Entries, Entry{Kind: "queue", Title: e.Title, Target: e.URL, Source: "reading queue"})
				break
			}
		}
	}
	c := <-columns
	p.Almanac, p.Oracle = c.Almanac, c.Oracle
	m.remember(env.DB, p, env.Shelves)
	return p
}

// addColumns sets the cover's columns: the day's almanac and the oracle
// (tarot and I Ching on alternate days). Either may fail and be left out.
func addColumns(ctx context.Context, env Env, p *Packet) {
	if env.Fetcher == nil {
		return
	}
	oracle := make(chan *Oracle, 1)
	go func() {
		o, _ := drawOracle(ctx, env.Fetcher, env.DB, oracleKind(p.Date))
		oracle <- o
	}()
	if t, err := time.Parse("2006-01-02", p.Date); err == nil {
		if a, err := buildAlmanac(ctx, env.Fetcher, t.Month(), t.Day()); err == nil {
			p.Almanac = a
		}
	}
	p.Oracle = <-oracle
}

// columnBlocks are the cover's almanac and oracle columns.
func columnBlocks(p Packet, link func(href, text string) int) []doc.Block {
	var bs, oracle []doc.Block
	if a := p.Almanac; a != nil {
		date := p.Date
		if t, err := time.Parse("2006-01-02", p.Date); err == nil {
			date = t.Format("2 January")
		}
		bs = append(bs, doc.Heading{Level: 2, Text: doc.Inline{{Text: "On this day · " + date}}})
		if len(a.Headlines) > 0 || a.Born != "" {
			in := doc.Inline{{Text: "The Book of Days (1864)", Style: doc.Bold, Link: link(AlmanacHref(a.Day), "The Book of Days")}}
			if len(a.Headlines) > 0 {
				in = append(in, doc.Span{Text: ": " + strings.Join(a.Headlines, " · ")})
			}
			bs = append(bs, doc.Paragraph{Text: in})
			if a.Born != "" {
				bs = append(bs, doc.Paragraph{Text: doc.Inline{{Text: "Born: " + a.Born, Style: doc.Italic}}})
			}
		}
		m, d, dayErr := dayOf(a.Day)
		// Issues before 2026-10-02 carry Wikipedia's events, with no heading.
		if (len(a.Birthdays) > 0 || len(a.Science) > 0) && dayErr == nil && len(a.Events) > 0 {
			bs = append(bs, doc.Paragraph{Text: doc.Inline{{Text: "Britannica, On This Day", Style: doc.Bold, Link: link(BritannicaURL(m, d), "Britannica")}}})
		}
		if l := almanacList(a.Events, "", link); l != nil {
			bs = append(bs, *l)
		}
		if len(a.Birthdays) > 0 {
			in := doc.Inline{{Text: "Birthdays: ", Style: doc.Italic}}
			for i, b := range a.Birthdays {
				if i > 0 {
					in = append(in, doc.Span{Text: " · "})
				}
				in = append(in, doc.Span{Text: b.Title, Link: link(b.Target, b.Title)}, doc.Span{Text: fmt.Sprintf(", %s (%d)", b.Text, b.Year), Style: doc.Italic})
			}
			bs = append(bs, doc.Paragraph{Text: in})
		}
		if len(a.Science) > 0 && dayErr == nil {
			bs = append(bs, doc.Paragraph{Text: doc.Inline{{Text: "Today in Science History", Style: doc.Bold, Link: link(ScienceURL(m, d), "Today in Science History")}}})
			if l := almanacList(a.Science, "science", link); l != nil {
				bs = append(bs, *l)
			}
		}
	}
	if p.Oracle != nil {
		oracle = append(oracle, doc.Heading{Level: 2, Text: doc.Inline{{Text: "The oracle"}}})
		oracle = append(oracle, oracleBlocks(p.Oracle, link)...)
	}
	if len(bs) > 0 && len(oracle) > 0 {
		// Side by side on a wide page.
		return []doc.Block{doc.Columns{Cols: [][]doc.Block{bs, oracle}}}
	}
	return append(bs, oracle...)
}

// almanacList is the almanac's events or scientists as a list: the year,
// then a headline or a name with a shortened text.
func almanacList(items []AlmanacItem, kind string, link func(href, text string) int) *doc.List {
	var out [][]doc.Block
	for _, e := range items {
		var in doc.Inline
		when := ""
		if e.Year > 0 {
			when = strconv.Itoa(e.Year)
		}
		if kind == "science" && e.Kind != "event" {
			when = strings.TrimSpace(e.Kind + " " + when)
		}
		if when != "" {
			in = append(in, doc.Span{Text: when, Style: doc.Bold}, doc.Span{Text: " — "})
		}
		switch {
		case kind == "science":
			in = append(in, doc.Span{Text: e.Title, Link: link(e.Target, e.Title)}, doc.Span{Text: ": " + shorten(e.Text, 150)})
		case e.Title != "":
			in = append(in, doc.Span{Text: e.Title, Link: link(e.Target, e.Title)})
		default:
			in = append(in, doc.Span{Text: shorten(e.Text, 160), Link: link(e.Target, e.Text)})
		}
		if e.Lang != "" && e.Lang != "en" {
			in = append(in, doc.Span{Text: "  (" + e.Lang + ")", Style: doc.Italic})
		}
		out = append(out, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(out) == 0 {
		return nil
	}
	return &doc.List{Items: out}
}

var kindLabel = map[string]string{"periodical": "Periodicals", "weird": "Weird Worlds", "fiction": "Fiction", "esoteric": "Esoterica",
	"folklore": "Folklore", "knowledge": "Essays & classics", "encyclopedic": "Encyclopedias", "public": "Public domain",
	"archive": "Old internet", "smallweb": "Small web", "queue": "From your queue"}

func packetHref(date string, rest string) string {
	if rest == "" {
		return "w5f:packet/" + date
	}
	return "w5f:packet/" + date + "/" + rest
}

func coverDoc(p Packet) *doc.Document {
	d := &doc.Document{Title: "The W5F Daily Packet", URL: packetHref(p.Date, ""), Origin: "local", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: fmt.Sprintf("No. %d · %s · %d items", p.Number, p.Date, len(p.Entries))}}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	var items [][]doc.Block
	for i, e := range p.Entries {
		in := doc.Inline{{Text: roman[i] + ".  ", Style: doc.Bold}, {Text: e.Title, Link: link(packetHref(p.Date, strconv.Itoa(i+1)), e.Title)},
			{Text: "  · " + kindLabel[e.Kind] + " · " + e.Source, Style: doc.Italic}}
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing could be gathered today (offline?). Try reshuffle later.", Style: doc.Italic}}})
	} else {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
		var kinds []string
		for _, e := range p.Entries {
			kinds = append(kinds, e.Kind)
		}
		oracle := ""
		if p.Oracle != nil {
			oracle = p.Oracle.Title
		}
		when, _ := time.Parse("2006-01-02", p.Date)
		d.Blocks = append(d.Blocks, ultan.Cover(kinds, oracle, when).Blocks(link)...)
	}
	d.Blocks = append(d.Blocks, columnBlocks(p, link)...)
	actions := doc.Inline{}
	if len(p.Entries) > 0 {
		actions = append(actions, doc.Span{Text: "▶ start reading", Style: doc.Bold, Link: link(packetHref(p.Date, "1"), "start")}, doc.Span{Text: "   "})
	}
	if p.Date == today() {
		actions = append(actions, doc.Span{Text: "reshuffle", Link: link(packetHref(p.Date, "reshuffle"), "reshuffle")}, doc.Span{Text: "   "})
	}
	actions = append(actions, doc.Span{Text: "save this issue", Link: link(packetHref(p.Date, "save"), "save")})
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: actions})
	if p.Date == today() {
		d.Next = packetHref(p.Date, "1")
	}
	return d
}

// entryDoc opens item n of an issue with the packet's navigation.
func entryDoc(ctx context.Context, env Env, p Packet, n int) (*doc.Document, error) {
	if n < 1 || n > len(p.Entries) {
		return coverDoc(p), nil
	}
	e := p.Entries[n-1]
	d, err := env.Load(ctx, e.Target)
	if err != nil {
		d = &doc.Document{Title: e.Title, Blocks: []doc.Block{doc.Notice{Kind: "warn", Text: err.Error()}}}
	}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	line := doc.Inline{{Text: fmt.Sprintf("Daily Packet No. %d · %s of %s · %s", p.Number, roman[n-1], roman[len(p.Entries)-1], kindLabel[e.Kind]), Style: doc.Italic}}
	if n > 1 {
		d.Prev = packetHref(p.Date, strconv.Itoa(n-1))
		line = append(line, doc.Span{Text: " · "}, doc.Span{Text: "‹ previous", Link: link(d.Prev, "previous")})
	}
	line = append(line, doc.Span{Text: " · "}, doc.Span{Text: "cover", Link: link(packetHref(p.Date, ""), "cover")})
	if n < len(p.Entries) {
		d.Next = packetHref(p.Date, strconv.Itoa(n+1))
		line = append(line, doc.Span{Text: " · "}, doc.Span{Text: "next ›", Link: link(d.Next, "next")})
	} else {
		d.Next = ""
	}
	d.Blocks = append([]doc.Block{doc.Paragraph{Text: line}}, d.Blocks...)
	return d, nil
}

// saveIssue writes the whole issue as one Markdown file in the notes folder.
func saveIssue(ctx context.Context, env Env, p Packet) (*doc.Document, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"The W5F Daily Packet No. %d\"\ndate: %s\ntags: [w5f, packet]\n---\n# The W5F Daily Packet No. %d — %s\n\n", p.Number, p.Date, p.Number, p.Date)
	if a := p.Almanac; a != nil {
		b.WriteString("## On this day\n\n")
		if len(a.Headlines) > 0 {
			fmt.Fprintf(&b, "*The Book of Days (1864):* %s\n\n", strings.Join(a.Headlines, " · "))
		}
		for _, e := range append(append([]AlmanacItem{}, a.Events...), a.Science...) {
			text := e.Text
			if e.Title != "" {
				text = e.Title + ": " + e.Text
			}
			fmt.Fprintf(&b, "- **%s** — [%s](%s)\n", strings.TrimSpace(e.Kind+" "+strconv.Itoa(e.Year)), text, e.Target)
		}
		for _, e := range a.Birthdays {
			fmt.Fprintf(&b, "- **born %d** — [%s](%s), %s\n", e.Year, e.Title, e.Target, e.Text)
		}
		b.WriteString("\n")
	}
	if o := p.Oracle; o != nil {
		b.WriteString("## The oracle\n\n")
		if len(o.Lines) == 6 {
			fmt.Fprintf(&b, "```\n%s\n```\n\n", hexagramLines(o.Lines))
		}
		fmt.Fprintf(&b, "[%s](%s) — %s\n\n", o.Title, o.Target, o.Detail)
	}
	for i, e := range p.Entries {
		fmt.Fprintf(&b, "## %s. %s\n\n*%s · %s* — <%s>\n\n", roman[i], e.Title, kindLabel[e.Kind], e.Source, e.Target)
		if d, err := env.Load(ctx, e.Target); err == nil {
			b.WriteString(personal.ToMarkdown(d))
			b.WriteString("\n\n")
		}
	}
	dir := filepath.Join(personal.Dir(), "Saved")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, personal.FileName(fmt.Sprintf("Daily Packet No. %d (%s)", p.Number, p.Date))+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	d := coverDoc(p)
	d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: "Saved: " + path}}, d.Blocks...)
	return d, nil
}

// packetRoute serves w5f:packet and w5f:packet/<date>[/<n>|/reshuffle|/save].
func packetRoute(ctx context.Context, target string, env Env) (*doc.Document, error) {
	if target == "w5f:packet" {
		p, err := todayPacket(env)
		if err != nil {
			return nil, err
		}
		return coverDoc(p), nil
	}
	parts := strings.Split(strings.TrimPrefix(target, "w5f:packet/"), "/")
	p, ok := loadPacket(env.DB, parts[0])
	if !ok {
		if parts[0] != today() {
			return nil, errors.New("no Daily Packet for " + parts[0])
		}
		var err error
		if p, err = todayPacket(env); err != nil {
			return nil, err
		}
	}
	if len(parts) == 1 {
		return coverDoc(p), nil
	}
	switch parts[1] {
	case "reshuffle":
		if p.Date != today() {
			return coverDoc(p), nil
		}
		p = buildPacket(ctx, env, p.Date, p.Number)
		if err := savePacket(env.DB, p); err != nil {
			return nil, err
		}
		return coverDoc(p), nil
	case "save":
		return saveIssue(ctx, env, p)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, errors.New("bad packet address")
	}
	return entryDoc(ctx, env, p, n)
}

// publicDomainDraws are the public-domain sources, tried in random order: a
// recent Public Domain Review article, a random Project Gutenberg book, or
// an old curious natural history book of the Biodiversity Heritage Library.
var publicDomainDraws = []func(context.Context, Env) (Draw, error){
	func(ctx context.Context, env Env) (Draw, error) {
		link, title, err := rssPick(ctx, env.Fetcher, "https://publicdomainreview.org/rss.xml")
		if err != nil {
			return Draw{}, err
		}
		return Draw{Target: link, Why: "public domain/Public Domain Review · " + title}, nil
	},
	gutenbergDraw,
	bhlPick,
}

func publicDomainDraw(ctx context.Context, env Env) (Draw, error) {
	var errs []string
	for _, i := range rand.Perm(len(publicDomainDraws)) {
		d, err := publicDomainDraws[i](ctx, env)
		if err == nil {
			return d, nil
		}
		errs = append(errs, err.Error())
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

func gutenbergDraw(ctx context.Context, env Env) (Draw, error) {
	gq, base, err := get(ctx, env.Fetcher, "https://www.gutenberg.org/ebooks/search/?sort_order=random")
	if err != nil {
		return Draw{}, err
	}
	a := gq.Find("li.booklink a.link").First()
	href, _ := a.Attr("href")
	if href == "" {
		return Draw{}, errors.New("Gutenberg: no random book")
	}
	target, _ := base.Parse(href)
	return Draw{Target: target.String(), Why: "public domain/Project Gutenberg · " + strings.Join(strings.Fields(a.Find(".title").Text()), " ")}, nil
}

// Welcome is the welcome screen's line about today's packet ("" before it exists).
func Welcome(db *store.DB) string {
	if p, ok := loadPacket(db, today()); ok {
		return fmt.Sprintf("Daily Packet No. %d — %d items", p.Number, len(p.Entries))
	}
	return "today's Daily Packet — press p"
}

// TodayItem is a line of the reading room's "Today".
type TodayItem struct{ Label, Text, Href string }

// Today is what the day holds, from today's issue when it is made (offline:
// it never fetches).
func Today(db *store.DB) []TodayItem {
	p, ok := loadPacket(db, today())
	if !ok {
		return []TodayItem{{"Daily Packet", "today's issue is not made yet — press p", "w5f:packet"}}
	}
	out := []TodayItem{{"Daily Packet", fmt.Sprintf("No. %d · %d items", p.Number, len(p.Entries)), "w5f:packet"}}
	if a := p.Almanac; a != nil && len(a.Headlines) > 0 {
		out = append(out, TodayItem{"On this day", a.Headlines[0], AlmanacHref(a.Day)})
	}
	if o := p.Oracle; o != nil {
		target := o.Target
		if strings.HasPrefix(target, "http") {
			target = localOracleHref(o)
		}
		out = append(out, TodayItem{"The oracle", o.Title, target})
	}
	return out
}
