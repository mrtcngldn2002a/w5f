package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func TestDeck(t *testing.T) {
	d := Deck()
	if len(d) != 78 {
		t.Fatalf("%d cards", len(d))
	}
	seen := map[string]bool{}
	for _, c := range d {
		if seen[c.Key] {
			t.Errorf("twice: %s", c.Key)
		}
		seen[c.Key] = true
	}
	for key, want := range map[string]string{"ar00": "0 The Fool", "ar16": "XVI The Tower", "ar21": "XXI The World", "wa07": "VII Seven of Wands", "cuqu": "Q Queen of Cups", "peac": "I Ace of Pentacles"} {
		c, ok := CardByKey(key)
		if !ok || c.Numeral+" "+c.Name != want {
			t.Errorf("%s: %+v", key, c)
		}
	}
}

func mustCard(key string) Card { c, _ := CardByKey(key); return c }

func TestCardFrame(t *testing.T) {
	c, _ := CardByKey("ar16")
	up := strings.Split(CardFrame(c, false), "\n")
	down := strings.Split(CardFrame(c, true), "\n")
	width := len([]rune(up[0]))
	for _, l := range append(append([]string{}, up...), down...) {
		if len([]rune(l)) != width {
			t.Fatalf("a ragged line (%d, want %d): %q", len([]rune(l)), width, l)
		}
	}
	if !strings.Contains(up[1], "XVI--The.Tower") || up[1] != down[1] || !strings.Contains(up[len(up)-2], "*.Upheaval.*") ||
		!strings.Contains(down[0], "[ reversed ]") || strings.Contains(up[0], "reversed") {
		t.Errorf("names:\n%s\n%s", strings.Join(up[:2], "\n"), strings.Join(down[:2], "\n"))
	}
	// Waite's words read downwards, upright on the left, reversed on the
	// right, the same way up on a reversed card; the way the card fell in
	// capitals.
	sides := func(rows []string) (l, r string) {
		for _, row := range rows[3 : len(rows)-3] {
			rs := []rune(row)
			l, r = l+string(rs[1]), r+string(rs[len(rs)-2])
		}
		return l, r
	}
	if l, r := sides(up); !strings.Contains(l, "CALAMITY") || !strings.Contains(r, "tyranny") {
		t.Errorf("upright sides: %q %q", l, r)
	}
	if l, r := sides(down); !strings.Contains(l, "calamity") || !strings.Contains(r, "TYRANNY") {
		t.Errorf("reversed sides: %q %q", l, r)
	}
	// The picture turns over: the tower's foundation comes first, the
	// crown and the lightning last, turned.
	row := func(rows []string, s string) int {
		for i, r := range rows {
			if strings.Contains(r, s) {
				return i
			}
		}
		return -1
	}
	if f, c := row(up, "___./::::"), row(up, "|______|"); f < 0 || c < 0 || c > f {
		t.Errorf("upright:\n%s", strings.Join(up, "\n"))
	}
	if f, c := row(down, `::::/.¯¯¯`), row(down, "|¯¯¯¯¯¯|"); f < 0 || c < 0 || f > c {
		t.Errorf("turned over:\n%s", strings.Join(down, "\n"))
	}
	// Every card the same size, both ways up.
	for _, c := range Deck() {
		for _, rev := range []bool{false, true} {
			rows := strings.Split(CardFrame(c, rev), "\n")
			if len(rows) != len(up) {
				t.Fatalf("%s: %d rows, want %d", c.Key, len(rows), len(up))
			}
			for _, l := range rows {
				if len([]rune(l)) != width {
					t.Fatalf("%s: ragged %q", c.Key, l)
				}
			}
		}
	}
}

// TestCardPictures holds the drawings to the frame: plain ASCII, within
// the picture's space, each card with its words.
func TestCardPictures(t *testing.T) {
	for _, c := range Deck() {
		art := cardPicture(c.Key)
		if art == "" {
			t.Errorf("%s: not drawn", c.Key)
			continue
		}
		rows := strings.Split(strings.Trim(art, "\n"), "\n")
		// Filling the card (the owner's wish): at least 17 rows of 20.
		if len(rows) > artHeight || len(rows) < 17 {
			t.Errorf("%s: %d rows, want 17 to %d", c.Key, len(rows), artHeight)
		}
		for _, r := range rows {
			if len(r) > artWidth {
				t.Errorf("%s: too wide (%d): %q", c.Key, len(r), r)
			}
			for _, ch := range r {
				if ch < ' ' || ch > '~' || ch == '`' {
					t.Errorf("%s: %q in %q", c.Key, ch, r)
				}
			}
		}
		w, ok := cardWords[c.Key]
		if !ok || w.sign == "" || w.upright == "" {
			t.Errorf("%s: no words", c.Key)
		}
	}
}

const waiteMinor = `<html><body><p>The Pictorial Key</p><h4>WANDS</h4><h4>Seven</h4>
<p align="CENTER"><a href="img/wa07.jpg"><img src="tn/wa07.jpg"><br><font>Click to enlarge</font></a></p>
<p>A young man on a craggy eminence brandishing a staff. <i>Divinatory Meanings</i>: It is a card of valour; discussion, wordy strife. <i>Reversed</i>: Perplexity, embarrassments, anxiety.</p>
<p><hr><center><a href="pktwa06.htm">Next: Six of Wands</a></center></p></body></html>`

const waiteMajor = `<html><body><h3>XVI</h3><h3>The Tower</h3>
<p><a href="img/ar16.jpg"><img src="tn/ar16.jpg">Click to enlarge</a></p>
<p>Occult explanations attached to this card are meagre.</p>
<p>It is the ruin of the House of We, when evil prevails.</p>
<p>Next: XVII. The Star</p><p>Sacred Texts | Tarot</p></body></html>`

const waiteMeanings = `<html><body>
<p>15. THE DEVIL.--Ravage, violence, vehemence. <i>Reversed</i>: Evil fatality, weakness.</p>
<p>16. THE TOWER.--Misery, distress, indigence, adversity, calamity. <i>Reversed</i>: According to one account, the same in a lesser degree.</p>
<p>ZERO. THE FOOL.--Folly, mania, extravagance. <i>Reversed</i>: Negligence, absence, distribution.</p>
<p>10. WHEEL OF FORTUNE.-Destiny, fortune, success. <i>Reversed</i>: Increase, abundance.</p>
<p>8. STRENGTH.--Power, energy, action. <i>Reversed</i>: Despotism, abuse of power.</p></body></html>`

func waiteServer(t *testing.T, hits *int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		switch r.URL.Path {
		case "/tarot/pkt/pktcu05.htm":
			fmt.Fprint(w, strings.ReplaceAll(strings.ReplaceAll(waiteMinor, "Divinatory", "Divanatory"), "a staff", "a staff depicts min in"))
		case "/tarot/pkt/pktwa07.htm":
			fmt.Fprint(w, waiteMinor)
		case "/tarot/pkt/pktar16.htm", "/tarot/pkt/pktar00.htm", "/tarot/pkt/pktar10.htm", "/tarot/pkt/pktar08.htm":
			fmt.Fprint(w, waiteMajor)
		case "/tarot/pkt/pkt0303.htm":
			fmt.Fprint(w, waiteMeanings)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCardTexts(t *testing.T) {
	hits := 0
	srv := waiteServer(t, &hits)
	swap(t, &tarotIndex, srv.URL+"/tarot/pkt/index.htm")
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()

	c, _ := CardByKey("wa07")
	tx, err := CardTextOf(ctx, testFetcher(), db, c)
	if err != nil || tx.Description != "A young man on a craggy eminence brandishing a staff." ||
		tx.Upright != "It is a card of valour; discussion, wordy strife." || tx.Reversed != "Perplexity, embarrassments, anxiety." {
		t.Fatalf("minor: %+v %v", tx, err)
	}
	for key, up := range map[string]string{"ar16": "Misery, distress, indigence, adversity, calamity.", "ar00": "Folly, mania, extravagance.", "ar08": "Power, energy, action.", "ar10": "Destiny, fortune, success."} {
		c, _ := CardByKey(key)
		tx, err := CardTextOf(ctx, testFetcher(), db, c)
		if err != nil || tx.Upright != up || tx.Reversed == "" || !strings.Contains(tx.Description, "ruin of the House of") || strings.Contains(tx.Description, "Sacred Texts") {
			t.Errorf("%s: %+v %v", key, tx, err)
		}
	}
	// "Divanatory", once; the scanning slips are mended only where listed.
	c5, _ := CardByKey("cu05")
	if tx, err := CardTextOf(ctx, testFetcher(), db, c5); err != nil || tx.Upright != "It is a card of valour; discussion, wordy strife." || !strings.Contains(tx.Description, "depicts min in") {
		t.Errorf("cu05: %+v %v", tx, err)
	}
	if tx, _ := CardTextOf(ctx, testFetcher(), db, mustCard("ar16")); !strings.Contains(tx.Description, "the House of Life") {
		t.Errorf("ar16: %q", tx.Description)
	}

	// Kept: read again without the network.
	before := hits
	tx, err = CardTextOf(ctx, testFetcher(), db, c)
	if err != nil || hits != before {
		t.Errorf("the kept text was fetched again (%d → %d): %v", before, hits, err)
	}

	env := Env{Fetcher: testFetcher(), DB: db}
	d, err := Route(ctx, "w5f:discover/tarot/wa07?r=1", env)
	if err != nil {
		t.Fatal(err)
	}
	text := flatText(d)
	if d.Title != "Seven of Wands, reversed" || strings.Index(text, "Perplexity") > strings.Index(text, "It is a card of valour") {
		t.Errorf("the reversed meaning comes first:\n%s", text)
	}
	pre := ""
	for _, b := range d.Blocks {
		if p, ok := b.(doc.Pre); ok {
			pre = p.Text
		}
	}
	if !strings.Contains(pre, "VII--Seven.of.Wands") || !strings.Contains(strings.Split(pre, "\n")[0], "reversed") {
		t.Errorf("card picture:\n%s", pre)
	}
	if _, err := Route(ctx, "w5f:discover/tarot/zz99", env); err == nil {
		t.Error("an unknown card")
	}
}

const leggePage = `<html><body><center><a href="ic58.htm">Previous</a></center><hr>
<p><a name="page_194"><font size=1>p. 194</font></a></p>
<h3 align="CENTER"><a href="#fn_118"><font size="1">LIX</font></a>. THE HW&Acirc;N HEXAGRAM</h3>
<p align="CENTER"><img src="img/hex110010.jpg"></p>
<p>Hw&acirc;n intimates that there will be progress and success. The king goes to his ancestral temple; and it will be advantageous to</p>
<p><a name="page_195"><font size=1>p. 195</font></a></p>
<p>cross the great stream.</p>
<p>1. The first SIX, divided, shows its subject rescuing.</p>
<p>2. The second NINE, undivided, shows its subject hurrying.</p>
<p>3. The third SIX, divided.</p>
<p>4. The fourth SIX, divided.</p>
<p>5. The fifth NINE, undivided, shows its subject issuing his great announcements.</p>
<p><a name="page_196"><font size=1>p. 196</font></a></p>
<p><font size=-2>[paragraph continues]</font> He scatters abroad the accumulations.</p>
<p>6. The topmost NINE, undivided.</p>
<p></p><hr>
<h3 align="CENTER">Footnotes</h3>
<p><a name="fn_118"></a><a href="ic59.htm#fr_118">196:LIX</a> Hw&acirc;n, the name of this hexagram, <a name="page_197"><font size=1>p. 197</font></a> denotes a state of dispersion.</p>
<p>The figure is made up of water and wind.</p></body></html>`

func TestHexagramText(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, strings.ReplaceAll(leggePage, "HW&Acirc;N", "HW&Acirc;N"))
	}))
	defer srv.Close()
	swap(t, &ichingBase, srv.URL+"/ich/")
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	h, err := HexagramOf(context.Background(), testFetcher(), db, 59)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "Hwân" || h.Judgment != "Hwân intimates that there will be progress and success. The king goes to his ancestral temple; and it will be advantageous to cross the great stream." {
		t.Errorf("judgment: %q %q", h.Name, h.Judgment)
	}
	if h.Lines[0] != "The first SIX, divided, shows its subject rescuing." || h.Lines[4] != "The fifth NINE, undivided, shows its subject issuing his great announcements. He scatters abroad the accumulations." || h.Lines[5] == "" {
		t.Errorf("lines: %q", h.Lines)
	}
	if h.Note != "Hwân, the name of this hexagram, denotes a state of dispersion." {
		t.Errorf("note: %q", h.Note)
	}
	before := hits
	HexagramOf(context.Background(), testFetcher(), db, 59)
	if hits != before {
		t.Error("kept, not fetched again")
	}

	// A cast: lines 1 and 4 move (an old yin and an old yang).
	o, err := ichingDraw(context.Background(), testFetcher(), db, []int{6, 7, 8, 9, 7, 7})
	if err != nil || o.Target != "w5f:discover/iching/678977" {
		t.Fatalf("draw: %+v %v", o, err)
	}
	fig := hexagramFigure([]int{6, 7, 8, 9, 7, 7})
	rows := strings.Split(fig, "\n")
	if len(rows) != 7 || !strings.HasPrefix(rows[0], "6  ━━━━━━━━━━━━━━━") || !strings.Contains(rows[2], " ○ ") || !strings.Contains(rows[6], " × ") {
		t.Errorf("figure:\n%s", fig)
	}
	d, err := Route(context.Background(), "w5f:discover/iching/678977", Env{Fetcher: testFetcher(), DB: db})
	if err != nil {
		t.Fatal(err)
	}
	text := flatText(d)
	for _, want := range []string{"Moving lines 1, 4 turn it into", "The judgment", "The moving lines", "1. The first SIX", "4. The fourth SIX"} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q:\n%s", want, text)
		}
	}
	if _, err := Route(context.Background(), "w5f:discover/iching/12", Env{}); err == nil {
		t.Error("not a cast")
	}
	if l := steady(59); len(l) != 6 {
		t.Errorf("steady: %v", l)
	} else if n, _, _ := castNumbers(l); n != 59 {
		t.Errorf("steady(59) is %d", n)
	}
}
