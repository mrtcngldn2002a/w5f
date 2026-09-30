package discover

import (
	"context"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// The daily oracle (chosen with the owner, 2026-09-30): on alternate days a
// tarot card with A. E. Waite's The Pictorial Key to the Tarot (1910), or an
// I Ching hexagram cast with three coins, with James Legge's translation
// (1882) — both at the Internet Sacred Text Archive.

// Oracle is one draw.
type Oracle struct {
	Kind   string `json:"kind"` // tarot | iching
	Title  string `json:"title"`
	Target string `json:"target"`
	Detail string `json:"detail"`
	Lines  []int  `json:"lines,omitempty"` // I Ching: 6–9 from the bottom line up
}

var (
	tarotIndex = "https://archive.sacred-texts.com/tarot/pkt/index.htm"
	ichingBase = "https://archive.sacred-texts.com/ich/"
	reCardPage = regexp.MustCompile(`(?i)^pkt(ar\d\d|(wa|cu|sw|pe)\w\w)\.htm$`)
	reHexTitle = regexp.MustCompile(`(?i)\b[IVXLC]+\.\s+THE\s+(.+?)\s+HEXAGRAM`)
)

// oracleKind alternates by day: tarot on even days since 2000-01-01.
func oracleKind(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "tarot"
	}
	if int(t.Sub(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).Hours()/24)%2 == 0 {
		return "tarot"
	}
	return "iching"
}

func drawOracle(ctx context.Context, f *fetch.Fetcher, kind string) (*Oracle, error) {
	if kind == "iching" {
		return ichingDraw(ctx, f, castHexagram())
	}
	return tarotDraw(ctx, f)
}

// tarotDraw picks one of the 78 cards of Waite's book, upright or reversed.
func tarotDraw(ctx context.Context, f *fetch.Fetcher) (*Oracle, error) {
	gq, base, err := get(ctx, f, tarotIndex)
	if err != nil {
		return nil, err
	}
	type card struct{ href, name string }
	var cards []card
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !reCardPage.MatchString(strings.TrimSpace(href)) {
			return
		}
		u, err := base.Parse(strings.TrimSpace(href))
		if err != nil {
			return
		}
		cards = append(cards, card{u.String(), strings.Join(strings.Fields(a.Text()), " ")})
	})
	if len(cards) < 22 {
		return nil, fmt.Errorf("tarot: only %d cards on Waite's contents page", len(cards))
	}
	c := pick(cards)
	o := &Oracle{Kind: "tarot", Title: strings.TrimPrefix(c.name, "Zero. "), Target: c.href, Detail: "upright"}
	if rand.IntN(2) == 0 {
		o.Detail = "reversed"
	}
	return o, nil
}

// castHexagram throws three coins six times: heads count 3, tails 2, so a
// line is 6 (old yin, moving), 7 (yang), 8 (yin) or 9 (old yang, moving).
func castHexagram() []int {
	lines := make([]int, 6)
	for i := range lines {
		for c := 0; c < 3; c++ {
			lines[i] += 2 + rand.IntN(2)
		}
	}
	return lines
}

// kingWen numbers a hexagram (lines from the bottom, true = yang) in the
// received order: rows are the upper trigram, columns the lower, both in
// the order Qian, Zhen, Kan, Gen, Kun, Xun, Li, Dui.
var kingWenTable = [8][8]int{
	{1, 25, 6, 33, 12, 44, 13, 10},
	{34, 51, 40, 62, 16, 32, 55, 54},
	{5, 3, 29, 39, 8, 48, 63, 60},
	{26, 27, 4, 52, 23, 18, 22, 41},
	{11, 24, 7, 15, 2, 46, 36, 19},
	{9, 42, 59, 53, 20, 57, 37, 61},
	{14, 21, 64, 56, 35, 50, 30, 38},
	{43, 17, 47, 31, 45, 28, 49, 58},
}

// trigramIndex: bottom, middle, top line (yang = true) → position in the
// table's order.
func trigramIndex(b, m, t bool) int {
	key := [3]bool{b, m, t}
	for i, k := range [8][3]bool{
		{true, true, true},    // Qian ☰
		{true, false, false},  // Zhen ☳
		{false, true, false},  // Kan ☵
		{false, false, true},  // Gen ☶
		{false, false, false}, // Kun ☷
		{false, true, true},   // Xun ☴
		{true, false, true},   // Li ☲
		{true, true, false},   // Dui ☱
	} {
		if k == key {
			return i
		}
	}
	return 0
}

func kingWen(yang [6]bool) int {
	return kingWenTable[trigramIndex(yang[3], yang[4], yang[5])][trigramIndex(yang[0], yang[1], yang[2])]
}

// ichingDraw names the cast hexagram (from Legge's page) and the one its
// moving lines turn it into.
func ichingDraw(ctx context.Context, f *fetch.Fetcher, lines []int) (*Oracle, error) {
	var now, then [6]bool
	var moving []string
	for i, l := range lines {
		now[i] = l == 7 || l == 9
		then[i] = now[i]
		if l == 6 || l == 9 {
			then[i] = !now[i]
			moving = append(moving, fmt.Sprint(i+1))
		}
	}
	n := kingWen(now)
	o := &Oracle{Kind: "iching", Target: fmt.Sprintf("%sic%02d.htm", ichingBase, n), Lines: lines}
	o.Title = fmt.Sprintf("Hexagram %d", n)
	if gq, _, err := get(ctx, f, o.Target); err == nil {
		if m := reHexTitle.FindStringSubmatch(gq.Text()); m != nil {
			o.Title = fmt.Sprintf("Hexagram %d, %s", n, titleCase(m[1]))
		}
	} else {
		return nil, err
	}
	if len(moving) == 0 {
		o.Detail = "no moving lines"
	} else {
		o.Detail = fmt.Sprintf("moving lines %s → hexagram %d", strings.Join(moving, ", "), kingWen(then))
	}
	return o, nil
}

// hexagramLines draws the hexagram, top line first; moving lines are
// marked (○ old yang, × old yin).
func hexagramLines(lines []int) string {
	var b strings.Builder
	for i := len(lines) - 1; i >= 0; i-- {
		switch lines[i] {
		case 9:
			b.WriteString("━━━━━━━━━  ○\n")
		case 7:
			b.WriteString("━━━━━━━━━\n")
		case 6:
			b.WriteString("━━━━   ━━━━  ×\n")
		default:
			b.WriteString("━━━━   ━━━━\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// oracleBlocks is the oracle as it is shown on a page.
func oracleBlocks(o *Oracle, link func(href, text string) int) []doc.Block {
	src := "Tarot · A. E. Waite, The Pictorial Key to the Tarot (1910)"
	if o.Kind == "iching" {
		src = "I Ching · James Legge's translation (1882)"
	}
	bs := []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: src, Style: doc.Italic}}}}
	if len(o.Lines) == 6 {
		bs = append(bs, doc.Pre{Text: hexagramLines(o.Lines)})
	}
	return append(bs, doc.Paragraph{Text: doc.Inline{{Text: o.Title, Style: doc.Bold, Link: link(o.Target, o.Title)}, {Text: " — " + o.Detail}}})
}

// oracleRoute draws now (g → tarot, g → iching) and opens the text.
func oracleRoute(ctx context.Context, env Env, kind string) (*doc.Document, error) {
	o, err := drawOracle(ctx, env.Fetcher, kind)
	if err != nil {
		return nil, err
	}
	page, err := env.Load(ctx, o.Target)
	if err != nil {
		return nil, err
	}
	head := oracleBlocks(o, func(href, text string) int {
		page.Links = append(page.Links, doc.Link{Href: href, Text: text})
		return len(page.Links)
	})
	head = append(head, doc.Paragraph{Text: doc.Inline{{Text: "Open it again for another draw.", Style: doc.Italic}}}, doc.Rule{})
	page.Blocks = append(head, page.Blocks...)
	return page, nil
}
