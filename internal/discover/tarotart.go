package discover

import (
	"embed"
	"strings"
)

// The cards' pictures, drawn for W5F in the manner of framed ASCII tarot
// decks (chosen with the owner, 2026-10-01: every card a full picture,
// line drawing rather than cartoon; a reversed card's picture turns over,
// its words stay readable). Each picture is tarotart/<key>.txt.

//go:embed tarotart/*.txt
var tarotArt embed.FS

// artWidth and artHeight are the picture's space inside the frame: every
// card is the same size.
const (
	artWidth  = 29
	artHeight = 20
)

// cardWords are a card's sign (under the picture) and, down its sides,
// Waite's own word for it upright (left) and reversed (right).
// (Waite gives the Two of Cups no reversed meaning.)
var cardWords = map[string]struct{ sign, upright, reversed string }{
	"ar00": {"Wandering", "folly", "negligence"},
	"ar01": {"Will", "skill", "disquiet"},
	"ar02": {"Veil", "secrets", "passion"},
	"ar03": {"Fertility", "fruitfulness", "light"},
	"ar04": {"Dominion", "stability", "benevolence"},
	"ar05": {"Tradition", "alliance", "concord"},
	"ar06": {"Choice", "attraction", "failure"},
	"ar07": {"Conquest", "triumph", "defeat"},
	"ar08": {"Fortitude", "courage", "despotism"},
	"ar09": {"Solitude", "prudence", "concealment"},
	"ar10": {"Turning", "destiny", "increase"},
	"ar11": {"Balance", "equity", "bias"},
	"ar12": {"Surrender", "sacrifice", "selfishness"},
	"ar13": {"Ending", "mortality", "lethargy"},
	"ar14": {"Harmony", "moderation", "disunion"},
	"ar15": {"Bondage", "violence", "blindness"},
	"ar16": {"Upheaval", "calamity", "tyranny"},
	"ar17": {"Renewal", "hope", "arrogance"},
	"ar18": {"Illusion", "deception", "silence"},
	"ar19": {"Radiance", "contentment", "lesser"},
	"ar20": {"Awakening", "renewal", "weakness"},
	"ar21": {"Completion", "success", "stagnation"},

	"waac": {"Spark", "creation", "decadence"},
	"wa02": {"Command", "riches", "surprise"},
	"wa03": {"Foresight", "enterprise", "cessation"},
	"wa04": {"Homecoming", "harmony", "increase"},
	"wa05": {"Strife", "competition", "litigation"},
	"wa06": {"Victory", "expectation", "apprehension"},
	"wa07": {"Defiance", "valour", "perplexity"},
	"wa08": {"Swiftness", "activity", "jealousy"},
	"wa09": {"Vigil", "strength", "obstacles"},
	"wa10": {"Burden", "oppression", "intrigues"},
	"wapa": {"Message", "envoy", "indecision"},
	"wakn": {"Journey", "departure", "rupture"},
	"waqu": {"Warmth", "loving", "obliging"},
	"waki": {"Leadership", "honesty", "austere"},

	"cuac": {"Overflow", "joy", "instability"},
	"cu02": {"Pledge", "love", ""},
	"cu03": {"Festival", "plenty", "excess"},
	"cu04": {"Apathy", "weariness", "novelty"},
	"cu05": {"Mourning", "loss", "return"},
	"cu06": {"Nostalgia", "memories", "future"},
	"cu07": {"Fantasy", "imagination", "determination"},
	"cu08": {"Withdrawal", "decline", "feasting"},
	"cu09": {"Wish", "satisfaction", "truth"},
	"cu10": {"Home", "contentment", "indignation"},
	"cupa": {"Dream", "meditation", "seduction"},
	"cukn": {"Romance", "invitation", "trickery"},
	"cuqu": {"Reverie", "wisdom", "dishonour"},
	"cuki": {"Composure", "responsible", "dishonest"},

	"swac": {"Clarity", "conquest", "disastrous"},
	"sw02": {"Stalemate", "equipoise", "imposture"},
	"sw03": {"Heartbreak", "division", "confusion"},
	"sw04": {"Repose", "retreat", "circumspection"},
	"sw05": {"Spoils", "degradation", "burial"},
	"sw06": {"Passage", "journey", "confession"},
	"sw07": {"Stealth", "attempt", "counsel"},
	"sw08": {"Confinement", "crisis", "treachery"},
	"sw09": {"Anguish", "despair", "suspicion"},
	"sw10": {"Ruin", "desolation", "advantage"},
	"swpa": {"Alertness", "vigilance", "unprepared"},
	"swkn": {"Charge", "bravery", "imprudence"},
	"swqu": {"Sorrow", "widowhood", "malice"},
	"swki": {"Judgment", "authority", "cruelty"},

	"peac": {"Fortune", "felicity", "riches"},
	"pe02": {"Juggling", "gaiety", "simulated"},
	"pe03": {"Craft", "renown", "mediocrity"},
	"pe04": {"Holding", "possessions", "delay"},
	"pe05": {"Hardship", "destitution", "disorder"},
	"pe06": {"Charity", "gifts", "envy"},
	"pe07": {"Patience", "business", "anxiety"},
	"pe08": {"Diligence", "craftsmanship", "vanity"},
	"pe09": {"Leisure", "accomplishment", "roguery"},
	"pe10": {"Legacy", "family", "robbery"},
	"pepa": {"Study", "scholarship", "prodigality"},
	"pekn": {"Steadfastness", "utility", "idleness"},
	"pequ": {"Nurture", "opulence", "mistrust"},
	"peki": {"Wealth", "success", "corruption"},
}

// turnOver gives a picture a half turn: the rows in the other order, each
// read backwards, and the characters that look different upside down
// turned too ("/" and "\" look the same).
var turnOver = strings.NewReplacer("(", ")", ")", "(", "[", "]", "]", "[", "{", "}", "}", "{", "<", ">", ">", "<",
	"^", "v", "v", "^", "'", ",", ",", "'", "_", "¯", "¯", "_")

func reverseString(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func cardPicture(key string) string {
	b, err := tarotArt.ReadFile("tarotart/" + key + ".txt")
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(b), "\r", "")
}

// HasArt reports a card drawn so far.
func HasArt(key string) bool { return cardPicture(key) != "" }

// CardFrame draws a card: its picture (turned over when reversed) in a
// double frame, numeral and name above, its sign below, Waite's upright
// and reversed words down the sides.
func CardFrame(c Card, reversed bool) string {
	art := cardPicture(c.Key)
	lines := strings.Split(strings.Trim(art, "\n"), "\n")
	if art == "" {
		lines = []string{center("~ drawing to come ~", artWidth)}
	}
	if len(lines) > artHeight {
		lines = lines[:artHeight]
	}
	// ... and in the middle across: the drawing moves as a whole.
	lead, wide := artWidth, 0
	for i, l := range lines {
		l = strings.TrimRight(l, " ")
		lines[i] = l
		if strings.TrimSpace(l) == "" {
			continue
		}
		lead = min(lead, len([]rune(l))-len([]rune(strings.TrimLeft(l, " "))))
		wide = max(wide, len([]rune(l)))
	}
	if shift := (artWidth-(wide-lead))/2 - lead; wide > 0 && shift != 0 {
		for i, l := range lines {
			if shift > 0 {
				lines[i] = strings.Repeat(" ", shift) + l
			} else {
				lines[i] = string([]rune(l)[min(-shift, len([]rune(l))):])
			}
		}
	}
	// Every card the same size: the picture stands in the middle of a
	// space artHeight rows high.
	above := (artHeight - len(lines)) / 2
	lines = append(append(make([]string, above), lines...), make([]string, artHeight-above-len(lines))...)
	for i, l := range lines {
		l = strings.TrimRight(l, " ")
		if n := len([]rune(l)); n < artWidth {
			l += strings.Repeat(" ", artWidth-n)
		} else if n > artWidth {
			l = string([]rune(l)[:artWidth])
		}
		lines[i] = l
	}
	if reversed && art != "" {
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		for i, l := range lines {
			lines[i] = turnOver.Replace(reverseString(l))
		}
	}
	blank := strings.Repeat(" ", artWidth)
	lines = append(append([]string{blank}, lines...), blank)

	w := cardWords[c.Key]
	// The way the card fell reads in capitals, the other way in small
	// letters.
	up, down := strings.ToUpper(w.upright), strings.ToLower(w.reversed)
	if reversed {
		up, down = strings.ToLower(w.upright), strings.ToUpper(w.reversed)
	}
	left, right := downward(up, len(lines)), downward(down, len(lines))
	inner := artWidth + 4
	var b strings.Builder
	top := strings.Repeat("-", inner)
	if reversed {
		t := "[ reversed ]"
		k := (inner - len(t)) / 2
		top = strings.Repeat("-", k) + t + strings.Repeat("-", inner-k-len(t))
	}
	b.WriteString("." + top + ".\n")
	b.WriteString("|" + centerFill(c.Numeral+"--"+strings.ReplaceAll(c.Name, " ", "."), inner, '.') + "|\n")
	b.WriteString("|:." + strings.Repeat("-", artWidth) + ".:|\n")
	for i, l := range lines {
		b.WriteString("|" + left[i] + "|" + l + "|" + right[i] + "|\n")
	}
	b.WriteString("|:'" + strings.Repeat("-", artWidth) + "':|\n")
	sign := ""
	if w.sign != "" {
		sign = "*." + w.sign + ".*"
	}
	b.WriteString("|" + centerFill(sign, inner, '.') + "|\n")
	b.WriteString("'" + strings.Repeat("-", inner) + "'")
	return b.String()
}

// downward spells a word down a column of n rows, in its middle.
func downward(word string, n int) []string {
	col := make([]string, n)
	top := (n - len([]rune(word))) / 2
	for i := range col {
		col[i] = ":"
	}
	for i, r := range []rune(word) {
		if top+i >= 0 && top+i < n {
			col[top+i] = string(r)
		}
	}
	return col
}

func centerFill(s string, n int, fill rune) string {
	k := len([]rune(s))
	if k >= n {
		return string([]rune(s)[:n])
	}
	left := (n - k) / 2
	return strings.Repeat(string(fill), left) + s + strings.Repeat(string(fill), n-k-left)
}

func center(s string, n int) string { return centerFill(s, n, ' ') }
