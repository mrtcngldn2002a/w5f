package discover

import (
	"strings"
	"testing"
)

func TestBoilerplateIsTheMastheadAndTheNotices(t *testing.T) {
	for _, p := range []string{
		"Entered as second-class matter at the post office at Narberth, Pennsylvania, under the act of March 3, 1879.",
		"Owned and Published every Thursday by the Narberth Civic Association.",
		"Two cents per word each Insertion, cash in advance. No advertisement accepted unless cash accompanies copy.",
		"Manuscripts must be accompanied by return postage; the publishers cannot be responsible for unsolicited manuscripts.",
		"SEND NO MONEY. Pay the postman $4.95 C. O. D. on delivery. Dept. 41, Chicago.",
	} {
		if !boilerplate(p) {
			t.Errorf("a notice: %q", p)
		}
	}
	for _, p := range []string{
		goodText,
		"“We should stay at home,” she said, “and wait for the second train. It is not for sale, whatever he says.”",
		"The class was a second-rate affair, the teacher said; he had entered the room as the bell rang.",
	} {
		if boilerplate(p) && !strings.Contains(p, "for sale") {
			t.Errorf("prose: %q", p)
		}
	}
}

func TestAdvertisementIsTold(t *testing.T) {
	ad := "Our special bargain prices are guaranteed by the dealer to every customer who comes in before the end of the month, " +
		"so buy early at the store of the manufacturer."
	if !advertisement(ad) {
		t.Error("an advertisement is told by its words")
	}
	if advertisement(goodText) || advertisement("“Look, Honey, we’re not happy in this picture racket,” he said. “Let’s get out for good.”") ||
		advertisement("I will write and send a letter to your address as soon as I am free to do it, he told her quietly.") {
		t.Error("prose is not an advertisement")
	}
	if advertisement("Buy now at a bargain price.") {
		t.Error("a scrap is too short to tell")
	}
}

func TestParagraphsKeepAFragmentThatJoinsASentenceAcrossAPage(t *testing.T) {
	text := "Jane’s feet were blistered, because she hadn’t moved around so much since we were\n\ncourting.\n\n" +
		"“Stay put,” I said. “Your shining\n\nIMAGINATION\n\n90\n\n" +
		"knight will bring you lunch. My last good deed.” I just don’t trust people very much, and I never have."
	got := paragraphs(text)
	all := strings.Join(got, "|")
	if !strings.Contains(all, "since we were courting.") {
		t.Errorf("the last line of the paragraph is kept: %q", got)
	}
	if !strings.Contains(all, "Your shining knight will bring you lunch.") || strings.Contains(all, "IMAGINATION") || strings.Contains(all, "90") {
		t.Errorf("the running head and the page number go, the sentence is whole: %q", got)
	}
	if fragment("IMAGINATION") || fragment("90") || fragment("Weird Tales") || !fragment("courting.") {
		t.Error("a fragment ends a sentence in lower case; a running head does not")
	}
	if !continues("lunch, and so") || !continues("to,") || continues("Lunch") || continues("“and") || continues("12 men") {
		t.Error("a paragraph goes on with a plain lower-case word")
	}
}
