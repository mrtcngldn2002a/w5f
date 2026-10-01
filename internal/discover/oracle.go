package discover

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// The oracle (chosen with the owner, 2026-09-30, made its own pages on
// 2026-10-01): a tarot card with A. E. Waite's The Pictorial Key to the
// Tarot (1910), or an I Ching hexagram cast with three coins, with James
// Legge's translation (1882) — both read from the Internet Sacred Text
// Archive, cleaned and kept. The Daily Packet alternates them; T and I
// draw one any time.

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

func drawOracle(ctx context.Context, f *fetch.Fetcher, db *store.DB, kind string) (*Oracle, error) {
	if kind == "iching" {
		return ichingDraw(ctx, f, db, castHexagram())
	}
	c, rev := DrawCard()
	o := &Oracle{Kind: "tarot", Title: c.Name, Target: TarotHref(c.Key, rev), Detail: "upright"}
	if rev {
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

// trigramNames are the trigrams' images, in the table's order.
var trigramNames = []string{"heaven", "thunder", "water", "mountain", "earth", "wind", "fire", "lake"}

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

// ichingDraw names the cast hexagram (Legge's name, read and kept) and the
// one its moving lines turn it into.
func ichingDraw(ctx context.Context, f *fetch.Fetcher, db *store.DB, lines []int) (*Oracle, error) {
	n, then, moving := castNumbers(lines)
	o := &Oracle{Kind: "iching", Target: IChingHref(lines), Lines: lines, Title: fmt.Sprintf("Hexagram %d", n)}
	h, err := HexagramOf(ctx, f, db, n)
	if err != nil {
		return nil, err
	}
	if h.Name != "" {
		o.Title = fmt.Sprintf("Hexagram %d, %s", n, h.Name)
	}
	if len(moving) == 0 {
		o.Detail = "no moving lines"
	} else {
		var ms []string
		for _, m := range moving {
			ms = append(ms, fmt.Sprint(m))
		}
		o.Detail = fmt.Sprintf("moving lines %s → hexagram %d", strings.Join(ms, ", "), then)
	}
	return o, nil
}

// hexagramLines draws the hexagram small, top line first; moving lines are
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

// bigLine is one line of the large figure.
func bigLine(yang bool) string {
	if yang {
		return "━━━━━━━━━━━━━━━"
	}
	return "━━━━━━   ━━━━━━"
}

// hexagramFigure draws the cast hexagram large with its line numbers, its
// trigrams and, beside it, the hexagram its moving lines turn it into.
func hexagramFigure(lines []int) string {
	var now, then [6]bool
	moves := false
	for i, l := range lines {
		now[i] = l == 7 || l == 9
		then[i] = now[i]
		if l == 6 || l == 9 {
			then[i] = !now[i]
			moves = true
		}
	}
	upper := trigramNames[trigramIndex(now[3], now[4], now[5])]
	lower := trigramNames[trigramIndex(now[0], now[1], now[2])]
	var b strings.Builder
	for i := 5; i >= 0; i-- {
		mark := "   "
		switch lines[i] {
		case 9:
			mark = " ○ "
		case 6:
			mark = " × "
		}
		side := ""
		switch i {
		case 4:
			side = upper
		case 1:
			side = lower
		}
		fmt.Fprintf(&b, "%d  %s%s %-9s", i+1, bigLine(now[i]), mark, side)
		if moves {
			b.WriteString(" " + bigLine(then[i]))
		}
		b.WriteString("\n")
		if i == 3 {
			b.WriteString("\n") // the two trigrams apart
		}
	}
	return strings.TrimRight(b.String(), "\n ")
}

// oracleBlocks is the oracle as the Daily Packet's cover shows it.
func oracleBlocks(o *Oracle, link func(href, text string) int) []doc.Block {
	src := "Tarot · A. E. Waite, The Pictorial Key to the Tarot (1910)"
	if o.Kind == "iching" {
		src = "I Ching · James Legge's translation (1882)"
	}
	bs := []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: src, Style: doc.Italic}}}}
	if len(o.Lines) == 6 {
		bs = append(bs, doc.Pre{Text: hexagramLines(o.Lines)})
	}
	target := o.Target
	if strings.HasPrefix(target, "http") { // drawn before 0.9.8: its card page now
		target = localOracleHref(o)
	}
	return append(bs, doc.Paragraph{Text: doc.Inline{{Text: o.Title, Style: doc.Bold, Link: link(target, o.Title)}, {Text: " — " + o.Detail}}})
}

// localOracleHref is the W5F page of a draw kept from before (its Target
// was Waite's or Legge's page).
func localOracleHref(o *Oracle) string {
	if o.Kind == "iching" && len(o.Lines) == 6 {
		return IChingHref(o.Lines)
	}
	if u, err := url.Parse(o.Target); err == nil {
		key := strings.TrimSuffix(strings.TrimPrefix(u.Path[strings.LastIndex(u.Path, "/")+1:], "pkt"), ".htm")
		if _, ok := CardByKey(key); ok {
			return TarotHref(key, o.Detail == "reversed")
		}
	}
	return o.Target
}

// DrawOracle draws now: a tarot card ("tarot") or an I Ching hexagram
// ("iching"), for the solo RPG table among others.
func DrawOracle(ctx context.Context, f *fetch.Fetcher, db *store.DB, kind string) (*Oracle, error) {
	return drawOracle(ctx, f, db, kind)
}

// HexagramText draws a cast hexagram, top line first.
func HexagramText(lines []int) string { return hexagramLines(lines) }
