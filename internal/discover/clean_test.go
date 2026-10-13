package discover

import (
	"strings"
	"testing"
)

func TestMendLinesJoinsWordsAndKeepsCompounds(t *testing.T) {
	for in, want := range map[string]string{
		"twenty-\nfive dollars":    "twenty-five dollars",
		"was elected Gover- nor":   "was elected Governor",
		"for no rea-\nson at all":  "for no reason at all",
		"the pre- and post-war":    "the pre- and post-war",
		"a well-known man":         "a well-known man",
		"to-day it rains":          "to-day it rains",
		"the moun-\n\n tain range": "the mountain range",
		"side-\ntracked by it":     "side-tracked by it",
	} {
		if got := mendLines(in); got != want {
			t.Errorf("mendLines(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMendMarksMendsOCR(t *testing.T) {
	for in, want := range map[string]string{
		"*Five sacks?\" Faye grinned":       "“Five sacks?\" Faye grinned",
		"when w'hen they went":              "when when they went",
		"T HEY fought it out":               "THEY fought it out",
		"to the participants In history":    "to the participants in history",
		"164 from some wagon train.":        "from some wagon train.",
		"\"Pll lay he did,\" she said":      "\"I'll lay he did,\" she said",
		"\"T never put my leg across one\"": "\"I never put my leg across one\"",
		"the road , and then":               "the road, and then",
		"tbe house. Tbe barn is red":        "the house. The barn is red",
	} {
		if got := mendMarks(in); got != want {
			t.Errorf("mendMarks(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepairWordOnlyMendsWhatIsSure(t *testing.T) {
	known := englishWords()
	for _, c := range []struct {
		in, want string
		start    bool
	}{
		{"tbe", "the", false},
		{"Tbe", "The", true},
		{"tlie", "the", false},
		{"Hoyt", "Hoyt", false},   // a name in the middle of a sentence
		{"NVC", "NVC", false},     // capitals
		{"house", "house", false}, // a word
		{"xqzv", "xqzv", false},   // nothing near it
	} {
		if got := repairWord(c.in, known, c.start); got != c.want {
			t.Errorf("repairWord(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCyrillicLookalikesAreLatinAgain(t *testing.T) {
	if got := cyrillicLookalikes.Replace("Сап you come? Tom соте"); got != "Can you come? Tom come" {
		t.Errorf("got %q", got)
	}
}

func TestParagraphsJoinColumnsAndKeepWholeSentences(t *testing.T) {
	text := strings.Join([]string{
		"cut off at the top of the column and then a whole sentence follows it. The old man walked down to the shore and looked at the sea for a long time, as he did every morning of his life.",
		"The tide was coming in over the flat sand, and the birds ran along the edge of the water. He thought of the men who had gone out in the boats and",
		"had not come back, and he sat down on a stone to rest. 47",
		"Mr. Hoyt came home from the boating trip with the girl in his arms, and her hair streamed away from her pale face. The boat had sprung a leak far out from shore and gone down despite his efforts to keep it afloat and",
	}, "\n\n")
	got := paragraphs(text)
	if len(got) != 3 {
		t.Fatalf("paragraphs: %q", got)
	}
	if !strings.HasPrefix(got[0], "The old man walked") {
		t.Errorf("the head of a sentence that began elsewhere is cut: %q", got[0])
	}
	if !strings.Contains(got[1], "and he sat down on a stone to rest.") || !strings.Contains(got[1], "and had not come back") {
		t.Errorf("a sentence cut by a column goes on in the next paragraph: %q", got[1])
	}
	if !strings.HasPrefix(got[2], "Mr. Hoyt came home") || !strings.HasSuffix(got[2], "pale face.") {
		t.Errorf("Mr. is not the end of a sentence, and the tail with no end is cut: %q", got[2])
	}
}

func TestSplitJunkCutsALineOfNoise(t *testing.T) {
	p := "The probability is small in every case that we have looked at so far. \"i u»m je;»iejo nu Sdtptoh Ilimh ■TT\"D J° at random, the holding time is greater than the average one."
	parts := splitJunk(p)
	if len(parts) != 2 || !strings.HasSuffix(wholeSentences(parts[0]), "so far.") || !strings.HasPrefix(parts[1], "at random") {
		t.Errorf("splitJunk: %q", parts)
	}
	if got := splitJunk("A clean sentence, with nothing wrong in it at all."); len(got) != 1 {
		t.Errorf("a clean text is not cut: %q", got)
	}
}

func TestCleanlinessSeparatesPrintFromNoise(t *testing.T) {
	prose := strings.Repeat("The old naturalist walked down to the shore every morning and wrote what he saw in a small book, for he held that nothing seen once is ever seen twice. ", 3)
	if s, _ := cleanliness(prose); s < 0.97 {
		t.Errorf("prose scores %.3f", s)
	}
	dirty := "that CVC13 stitch put upon them is light. The m,ik- ing is .is c.ucfully looked iftei ois the st^'lc and lit — .uid the^ aie puifeot; the neNV '•lock .ind u n i t e }ou to .in inspection wlictlici y o u d e s i i e to buy 01 not"
	if s, _ := cleanliness(dirty); s > 0.7 {
		t.Errorf("noise scores %.3f", s)
	}
	if s, _ := cleanliness("Jerry West y el tesoro indio, los hombres que vinieron de la ciudad con el caballo"); s > 0.5 {
		t.Errorf("another language scores %.3f", s)
	}
}

func TestDropCapLost(t *testing.T) {
	if !dropCapLost("UDGE TUCKER'S sparse gray beard waved like the whiskers of a goat.") {
		t.Error("UDGE is a word that lost its first letter")
	}
	if dropCapLost("THE old man walked down to the shore.") || dropCapLost("The old man walked down to the shore.") {
		t.Error("a word in capitals is not a lost letter")
	}
}

func TestStartsSentence(t *testing.T) {
	for in, want := range map[string]bool{
		"The old man":             true,
		"“The old man”":           true,
		"1. An assumed law":       true,
		"‘only given him one of":  false, // the rest of a sentence, behind a stray quote
		"and then he went":        false,
		"X be given increased":    false, // a lost first letter
		"I be given":              true,
		"A man walked":            true,
		"J. P. Morgan came to us": true,
	} {
		if got := startsSentence(in); got != want {
			t.Errorf("startsSentence(%q) = %v", in, got)
		}
	}
}

func TestUnglueSplitsRunTogetherWords(t *testing.T) {
	known := englishWords()
	for in, want := range map[string]string{"yearsof": "years of", "cityof": "city of", "thana": "than a", "house": "", "xqzvwof": ""} {
		if got := unglue(in, known); got != want {
			t.Errorf("unglue(%q) = %q, want %q", in, got, want)
		}
	}
}
