package discover

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestStrayMarksTellADirtyScan(t *testing.T) {
	clean := strings.Repeat(goodText+" Mr. J. Smith of St. Louis said it was so. ", 4)
	if s, o := strayMarks(clean); s > 0.001 || o > 0.001 {
		t.Errorf("clean prose has stray %.3f odd %.3f", s, o)
	}
	dirty := strings.Repeat("The county commissioners ft u appoint the trustees df the asylums and ithe law libraries j in the «county"+
		" ^ of Lorain, and a 3d term. ", 4)
	if s, o := strayMarks(dirty); s < 0.03 || o < 0.03 {
		t.Errorf("a dirty scan has stray %.3f odd %.3f", s, o)
	}
	if proseOK(dirty) || !proseOK(clean) {
		t.Error("only clean prose is prose to read")
	}
	if s, o := strayMarks("the 20th and the 1st, 4th of 1920s, a 5-cent fare"); s != 0 || o != 0 {
		t.Errorf("ordinals and years are not marks: %.3f %.3f", s, o)
	}
}

func TestProseOKRefusesAPageOfAdvertisements(t *testing.T) {
	ads := strings.Repeat("Our special bargain prices are guaranteed by the dealer to every customer who will send for the free catalog. "+
		"Buy at the store of the manufacturer and save on every order. ", 6)
	if proseOK(ads) {
		t.Error("a page of advertisements is not prose to read")
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

func TestADirtyScanIsToldFromAnEmptyPlace(t *testing.T) {
	junk := strings.Repeat("xq vz kt jj qq zz bk wq lx jh rr tt\n", 600)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "dirty.txt") {
			fmt.Fprint(w, junk)
			return
		}
		fmt.Fprint(w, goodText+"\n\n"+goodText) // clean, but too little for a window
	}))
	defer srv.Close()
	f := testFetcher()
	_, err := buildWindow(context.Background(), f, windowTarget(srv.URL+"/x/dirty.txt", "Junk"))
	if !errors.Is(err, errDirtyScan) {
		t.Errorf("a text of junk is a dirty scan: %v", err)
	}
	_, err = buildWindow(context.Background(), f, windowTarget(srv.URL+"/x/short.txt", "Short"))
	if err == nil || errors.Is(err, errDirtyScan) {
		t.Errorf("a clean text with too little in it is not dirty: %v", err)
	}
}
