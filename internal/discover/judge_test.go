package discover

import (
	"slices"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func para(s string) doc.Paragraph { return doc.Paragraph{Text: doc.Inline{{Text: s}}} }

// sentences makes n words of ordinary English prose, ending on a sentence.
func sentences(n int) string {
	words := strings.Fields(strings.Repeat("The old woman walked down to the river and sat there until the evening came. ", n/15+1))
	return strings.Join(words[:n-1], " ") + " end."
}

func story(paras, words int) *doc.Document {
	d := &doc.Document{Title: "The River", URL: "https://example.org/the-river"}
	for range paras {
		d.Blocks = append(d.Blocks, para(sentences(words)))
	}
	return d
}

func reasons(v Verdict) string { return strings.Join(v.Reasons, ", ") }

func TestJudgeWholeStory(t *testing.T) {
	v := Judge(story(12, 80), "fiction")
	if !v.OK() || v.Words != 960 || v.Bar != 600 {
		t.Fatalf("a whole story was turned away: %+v (%s)", v, reasons(v))
	}
	// The same story is short for the fiction shelf at 400 words, and not
	// for the small web.
	if v := Judge(story(5, 80), "fiction"); !slices.Contains(v.Reasons, whyShort) {
		t.Errorf("400 words passed the fiction bar: %s", reasons(v))
	}
	if v := Judge(story(5, 80), "smallweb"); !v.OK() {
		t.Errorf("400 words of small web turned away: %s", reasons(v))
	}
}

func TestJudgeGallery(t *testing.T) {
	// Theoi's "Vase Paintings Gallery": many captions, each a line.
	d := &doc.Document{Title: "Zeus: Vase Paintings"}
	for range 60 {
		d.Blocks = append(d.Blocks, doc.Image{Src: "x.jpg"}, para("Zeus and Ganymede, Athenian red-figure kylix"))
	}
	v := Judge(d, "folklore")
	if !slices.Contains(v.Reasons, whyFragments) {
		t.Errorf("a gallery passed: %+v", v)
	}
}

func TestJudgeDirectory(t *testing.T) {
	// gnosis.org's library page: a few words and many links.
	d := &doc.Document{Title: "The Gnostic Society Library"}
	d.Blocks = append(d.Blocks, para(sentences(120)))
	var items [][]doc.Block
	for i := range 80 {
		d.Links = append(d.Links, doc.Link{Href: "https://gnosis.org/x"})
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "The Gospel of Thomas and other texts", Link: i + 1}}}})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	v := Judge(d, "esoteric")
	if !slices.Contains(v.Reasons, whyLinks) {
		t.Errorf("a directory passed: %+v", v)
	}
}

func TestJudgeContents(t *testing.T) {
	// A contents page: headings and lists, hardly any prose.
	d := &doc.Document{Title: "Book One"}
	for range 10 {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Chapter of the Prophet"}}},
			doc.List{Items: [][]doc.Block{{para("On fasting and prayer in the month")}, {para("On the night journey and the vision")}}})
	}
	d.Blocks = append(d.Blocks, para(sentences(60)))
	v := Judge(d, "sufism")
	if !slices.Contains(v.Reasons, whyLists) {
		t.Errorf("a contents page passed: %+v", v)
	}
}

func TestJudgeTitles(t *testing.T) {
	turnedAway := []string{
		"7 Novels About Grief That Are Actually Funny",
		"Ten Books to Read This Fall",
		"Books About Mothers You Should Read",
		"Review: The Overstory",
		"Event Report: The 2019 Symposium",
		"Vase Paintings Gallery",
		"Classical Texts Library",
		"Encyclopedia P",
		"News roundup for May",
		"Week in Review",
		"ENCYCLOPEDIA P - Greek Gods, Spirits & Monsters",
		"Twilit Grotto -- Esoteric Archives: site map",
		"Z - Greek Gods, Spirits & Monsters",
	}
	for _, title := range turnedAway {
		d := story(12, 80)
		d.Title = title
		if v := Judge(d, "literature"); !slices.Contains(v.Reasons, whyTitle) {
			t.Errorf("%q passed", title)
		}
	}
	kept := []string{"The Library of Babel", "1984", "Twelve Angry Men", "Good News from Outer Space", "The Garden of Forking Paths", "A Report to an Academy",
		"Popular Science Monthly/Volume 10/December 1876/The Laws of Health - Wikisource, the free online library",
		"Kingdoms | Theoi Greek Mythology",
		"HOMER, ILIAD BOOK 5 - Theoi Classical Texts Library",
		"P | A | Jaffray: Introduction to the Kitab al-isfar | Muhyiddin Ibn Arabi Society"}
	for _, title := range kept {
		d := story(12, 80)
		d.Title = title
		if v := Judge(d, "literature"); slices.Contains(v.Reasons, whyTitle) {
			t.Errorf("%q turned away for its title", title)
		}
	}
}

func TestJudgeCutOut(t *testing.T) {
	d := story(12, 80)
	d.Blocks[0] = para("and so they went on " + sentences(80))
	if v := Judge(d, "fiction"); !slices.Contains(v.Reasons, whyMidStart) {
		t.Errorf("a text opening mid-sentence passed: %s", reasons(v))
	}
	d = story(12, 80)
	d.Blocks[11] = para(strings.TrimSuffix(sentences(80), " end.") + " and then the")
	if v := Judge(d, "fiction"); !slices.Contains(v.Reasons, whyMidEnd) {
		t.Errorf("a text cut off at its end passed: %s", reasons(v))
	}
	// Found by the source audit (2026-10-30): an entry that opens on its
	// headword, a placeholder for a picture, a writer who writes in small
	// letters only.
	d = story(12, 80)
	d.Title = "epigraphy | Britannica"
	d.Blocks[0] = para("epigraphy, the study of written matter recorded on hard material. " + sentences(80))
	if v := Judge(d, "encyclopedic"); !v.OK() {
		t.Errorf("an entry opening on its headword: %s", reasons(v))
	}
	d = story(12, 80)
	d.Blocks[0] = para("[image] Adam's Alchemy Weblog " + sentences(80))
	if v := Judge(d, "esoteric"); !v.OK() {
		t.Errorf("a picture's placeholder: %s", reasons(v))
	}
	d = story(12, 80)
	for i := range d.Blocks {
		d.Blocks[i] = para("there is a lot of night life at the pub " + strings.ToLower(sentences(80)))
	}
	if v := Judge(d, "smallweb"); !v.OK() {
		t.Errorf("a writer in small letters: %s", reasons(v))
	}
	// A leaf of a book that opens on its page number and mid-sentence.
	d = story(12, 80)
	d.Blocks[0] = para("723 sections made with a sharp razor ; the ovule should not " + sentences(80))
	if v := Judge(d, "nature"); !slices.Contains(v.Reasons, whyMidStart) {
		t.Errorf("a leaf opening mid-sentence after its page number passed: %s", reasons(v))
	}
	// Ashliman opens on who edited it, in small letters.
	d = story(12, 80)
	d.Blocks = append([]doc.Block{para("folktales of Aarne-Thompson-Uther type 1176 translated and edited by D. L. Ashliman")}, d.Blocks...)
	if v := Judge(d, "folklore"); !v.OK() {
		t.Errorf("an editor's line at the top: %s", reasons(v))
	}
	// A credit line after the last paragraph is no cut.
	d = story(12, 80)
	d.Blocks = append(d.Blocks, para("Translated by A. N. Other"))
	if v := Judge(d, "fiction"); !v.OK() {
		t.Errorf("a credit line at the end turned it away: %s", reasons(v))
	}
}

func TestJudgePoem(t *testing.T) {
	var in doc.Inline
	for i, l := range []string{"I wandered lonely as a cloud", "That floats on high o'er vales and hills,", "When all at once I saw a crowd,", "A host, of golden daffodils;", "Beside the lake, beneath the trees,", "fluttering and dancing in the breeze."} {
		if i > 0 {
			in = append(in, doc.Span{Break: true})
		}
		in = append(in, doc.Span{Text: l})
	}
	d := &doc.Document{Title: "Daffodils", Blocks: []doc.Block{doc.Paragraph{Text: in}}}
	v := Judge(d, "literature")
	if !v.Verse || !v.OK() {
		t.Errorf("a poem was turned away: %+v (%s)", v, reasons(v))
	}
}

// The Alchemy Web Site's art pages set pictures and short captions in
// lines: no poem.
func TestJudgeCaptionsAreNoVerse(t *testing.T) {
	var in doc.Inline
	for i := range 12 {
		if i > 0 {
			in = append(in, doc.Span{Break: true})
		}
		in = append(in, doc.Span{Text: "[image]"}, doc.Span{Break: true}, doc.Span{Text: "Facsimile oil painting by McLean"})
	}
	d := &doc.Document{Title: "Splendor Solis Art Project", Blocks: []doc.Block{doc.Paragraph{Text: in}}}
	if v := Judge(d, "esoteric"); v.Verse || v.OK() {
		t.Errorf("a gallery of captions passed as a poem: %+v", v)
	}
}

// A whole book on one page stays (the owner, 2026-10-30): there is no
// upper bar.
func TestJudgeWholeBookStays(t *testing.T) {
	if v := Judge(story(2500, 80), "esoteric"); !v.OK() {
		t.Errorf("200,000 words on one page was turned away: %s", reasons(v))
	}
}

func TestJudgeTextFile(t *testing.T) {
	text := strings.Repeat(sentences(60)+"\n\n", 5)
	d := &doc.Document{Title: "phreak.txt", Blocks: []doc.Block{doc.Pre{Text: text}}}
	v := Judge(d, "textfiles")
	if !v.OK() || v.Paras != 5 {
		t.Errorf("a text file was turned away: %+v (%s)", v, reasons(v))
	}
}

func TestJudgeGarbled(t *testing.T) {
	d := &doc.Document{Title: "The Valley"}
	for range 8 {
		d.Blocks = append(d.Blocks, para("Adek wandered for some r time tbe cbild bis motber wbere tbe nver ran tlirough tbe valley. "+sentences(60)))
	}
	v := Judge(d, "smallweb")
	if v.Clean == 0 || v.Clean >= 0.95 {
		t.Errorf("clean = %.2f for OCR noise", v.Clean)
	}
}

func TestSourceOf(t *testing.T) {
	for why, want := range map[string]string{
		"literature/The Believer · Some Essay":                "literature/The Believer",
		"textfiles/occult · chaos.txt":                        "textfiles",
		"fiction/r/nosleep · top of all time":                 "fiction/reddit",
		"weird/archived worlds · Some Wiki (tier 2) · A Page": "weird/archived worlds",
		"smallweb/Wiby · example.org":                         "smallweb/Wiby",
		"weird/SCP Foundation":                                "weird/SCP Foundation",
	} {
		if got := SourceOf(Draw{Why: why}); got != want {
			t.Errorf("SourceOf(%q) = %q, want %q", why, got, want)
		}
	}
}

func TestJudgeTitleDashLeaningOnAWord(t *testing.T) {
	d := story(12, 80)
	d.Title = "MEGARA & DEAD ADONIS- Theoi Classical Texts Library"
	if v := Judge(d, "folklore"); slices.Contains(v.Reasons, whyTitle) {
		t.Errorf("a book of the library turned away: %v", v.Reasons)
	}
}

// A plate is its picture and a label: no bar of prose, but no picture or
// no label is turned away.
func TestJudgePlate(t *testing.T) {
	pic := doc.Image{Src: "https://example.org/a.jpg"}
	ok := &doc.Document{Title: "Two Monks practising alchemy", Blocks: []doc.Block{pic}}
	if v := Judge(ok, "plates"); !v.OK() {
		t.Fatalf("plate turned away: %v", v.Reasons)
	}
	if v := Judge(&doc.Document{Title: "Untitled", Blocks: []doc.Block{pic}}, "plates"); v.OK() {
		t.Fatal("a picture without a label passed")
	}
	if v := Judge(&doc.Document{Title: "A long and well labelled page", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "words only"}}}}}, "plates"); v.OK() {
		t.Fatal("a plate without a picture passed")
	}
}
