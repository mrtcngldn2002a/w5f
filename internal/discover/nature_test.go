package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestEmbeddedBHLList(t *testing.T) {
	bhlOnce.Do(loadBHL)
	if len(bhlItems[1]) < 200 || len(bhlItems[2]) < 2000 {
		t.Fatalf("the embedded list is thin: tier 1 %d, tier 2 %d", len(bhlItems[1]), len(bhlItems[2]))
	}
	for tier := 1; tier <= 2; tier++ {
		for _, it := range bhlItems[tier] {
			if it.id <= 0 || it.title == "" || it.year > 1923 || it.tier != tier {
				t.Fatalf("bad item %+v", it)
			}
		}
	}
	for range 50 {
		if it, ok := pickBHL(); !ok || it.id == 0 {
			t.Fatal("pickBHL")
		}
	}
}

// bhlBook is the text of a book of 30 leaves as the open data keeps it: one
// text per leaf, cut anywhere (in the middle of a sentence), with the running
// head on top, a page number at the foot, and lines of 50 characters.
var places = []string{"Dover", "Whitby", "Cromer", "Penzance", "Lerwick", "Oban", "Tenby", "Deal", "Wells", "Rye"}

func bhlBook() map[int]string {
	var b strings.Builder
	for i := 0; i < 700; i++ {
		fmt.Fprintf(&b, "The old naturalist of %s records the tide, the weather and the birds that came with them to the shore. ", places[i%len(places)])
	}
	text := b.String()
	leaves := map[int]string{}
	for seq := 1; seq <= 30; seq++ {
		from := (seq - 1) * 2200
		chunk := text[from : from+2200]
		var lines []string
		for len(chunk) > 0 {
			n := min(50, len(chunk))
			lines = append(lines, chunk[:n])
			chunk = chunk[n:]
		}
		leaves[seq] = fmt.Sprintf("THE NATURALIST. %d\n\n", seq+100) + strings.Join(lines, "\n") + fmt.Sprintf("\n\n%d\n", seq+100)
	}
	// Leaves 1-3 and 28-30 are short (covers and plates); leaf 12 is an index.
	for _, s := range []int{1, 2, 3, 28, 29, 30} {
		leaves[s] = "PLATE " + fmt.Sprint(s)
	}
	leaves[12] = strings.Repeat("Goldfinch Mexican, 178. Grackle, boat-tailed, 85, 129, 130.\n", 40)
	return leaves
}

func bhlServer(t *testing.T) *httptest.Server {
	t.Helper()
	book := bhlBook()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			fmt.Fprint(w, `<?xml version="1.0"?><ListBucketResult>`)
			for seq := 1; seq <= 30; seq++ {
				fmt.Fprintf(w, `<Contents><Key>ocr/item-000123/item-000123-%08d-%04d.txt</Key><Size>%d</Size></Contents>`, 5000+seq, seq, len(book[seq]))
			}
			fmt.Fprint(w, `</ListBucketResult>`)
			return
		}
		var seq int
		if _, err := fmt.Sscanf(r.URL.Path[strings.LastIndex(r.URL.Path, "-")+1:], "%d.txt", &seq); err != nil || book[seq] == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, book[seq])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTidyPagesJoinsLeavesIntoWholeSentences(t *testing.T) {
	pages := []string{
		"THE NATURALIST. 12\n\nrest of a sentence begun on the page before. The second sentence of the\npage runs over two lines and the word broken at the end is mend-\ned here. A third sentence begins and is cut off at the foot of the",
		"page, and goes on at the top of the next one. Then a last whole sentence. And a fourth that is cut\n\n13\n",
	}
	got := tidyPages(pages)
	text := strings.Join(got, " ")
	if strings.HasPrefix(text, "rest of") {
		t.Errorf("the fragment at the start is cut: %q", text)
	}
	for _, want := range []string{"The second sentence of the page runs over two lines", "mended here.", "foot of the page, and goes on at the top of the next one."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "THE NATURALIST") || strings.Contains(text, "13") || strings.HasSuffix(text, "cut") {
		t.Errorf("running head, page number and unfinished end are dropped: %q", text)
	}
	if !strings.HasSuffix(text, "Then a last whole sentence.") {
		t.Errorf("ends at the last whole sentence: %q", text)
	}
}

func TestIsRunningHead(t *testing.T) {
	for _, s := range []string{"48", "xviii", "THE NATURALIST. 256", "108 DICTIONARY OF NAMES OF BRITISH BIRDS."} {
		if !isRunningHead(s) {
			t.Errorf("%q is a running head", s)
		}
	}
	for _, s := range []string{"", "The roots of the plant are long and white and of a bitter taste.", "THE SNOW-WALKERS continues to the eye the ease and poise of his carriage"} {
		if isRunningHead(s) {
			t.Errorf("%q is text", s)
		}
	}
}

func TestReadableIsEnglishProse(t *testing.T) {
	prose := strings.Repeat("The old naturalist walked down to the shore every morning and wrote what he saw in a small book, for he held that nothing seen once is ever seen twice. ", 3)
	if !readable(prose) {
		t.Error("prose reads")
	}
	for name, text := range map[string]string{
		"an index":      strings.Repeat("Goldfinch Mexican, 178. Grackle, boat-tailed, 85, 129, 130. Grosbeak, blue, 91. ", 8),
		"a table":       strings.Repeat("Aug. 12 | 14.5 | 66 | 3.4 | north | the 12 | 7.1 | 40 |", 30),
		"Latin":         strings.Repeat("Postquam exhausta palus terraque ardore dehiscunt exsilit in siccum flammantia lumina torquens ", 5),
		"long s":        strings.Repeat("The farmer faw the fheep in the foft meadow and the fhepherd fang of fifh and fire ", 4),
		"a list":        strings.Repeat("Cryptocephalus nigro-fasciatus. Inhabits Cayenne. Body cylindrical, rufous. ", 8),
		"scan noise":    strings.Repeat("Biri A94a b znartattas 8i sat 44 hét 8 MLLAKULTTONT TET 1 VAYTAT jat papa ", 6),
		"punctuation":   strings.Repeat("|| -- ~~ // __ ## ** ", 40),
		"a short scrap": "too short",
	} {
		if readable(text) {
			t.Errorf("%s should not read", name)
		}
	}
}

func TestBHLPassageIsWholeAndSteady(t *testing.T) {
	srv := bhlServer(t)
	swap(t, &bhlBucket, srv.URL)
	env := Env{Fetcher: testFetcher()}
	for range 20 {
		target, title, err := bhlPage(context.Background(), env)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(target, "w5f:discover/bhl?") || !strings.Contains(title, ", leaves ") {
			t.Fatalf("target %q title %q", target, title)
		}
		q, _ := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/bhl?"))
		if seq := q.Get("seq"); len(seq) == 1 && seq <= "3" || seq == "12" {
			t.Fatalf("passage starts on leaf %s (a plate or an index)", seq)
		}
		d, err := Route(context.Background(), target, env)
		if err != nil {
			t.Fatal(err)
		}
		if d.Title != q.Get("t") || len(d.Links) != 2 || !strings.Contains(d.Links[0].Href, "biodiversitylibrary.org/page/50") {
			t.Fatalf("document: %q %+v", d.Title, d.Links)
		}
		var text []string
		for _, b := range d.Blocks[:len(d.Blocks)-1] {
			text = append(text, b.(doc.Paragraph).Text[0].Text)
		}
		all := strings.Join(text, " ")
		if len(all) < bhlPassageOK {
			t.Fatalf("passage of %d characters", len(all))
		}
		if r := all[0]; r < 'A' || r > 'Z' {
			t.Fatalf("starts mid-sentence: %q", all[:60])
		}
		if !strings.HasSuffix(all, ".") || strings.Contains(all, "THE NATURALIST") {
			t.Fatalf("ends mid-sentence or keeps a running head: …%q", all[len(all)-60:])
		}
		again, _ := Route(context.Background(), target, env)
		if again == nil || again.Blocks[0].(doc.Paragraph).Text[0].Text != d.Blocks[0].(doc.Paragraph).Text[0].Text {
			t.Fatal("the same address gives the same passage")
		}
	}
}

func TestBHLPageGivesUpOnABookOfNoise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			fmt.Fprint(w, `<ListBucketResult></ListBucketResult>`) // a book with no OCR of its own
			return
		}
	}))
	defer srv.Close()
	swap(t, &bhlBucket, srv.URL)
	if _, _, err := bhlPage(context.Background(), Env{Fetcher: testFetcher()}); err == nil {
		t.Error("no pages: an error is expected")
	}
	if _, err := Route(context.Background(), "w5f:discover/bhl?item=1&seq=40&t=x", Env{Fetcher: testFetcher()}); err == nil {
		t.Error("a leaf without text is an error")
	}
	if _, err := Route(context.Background(), "w5f:discover/bhl?item=x", Env{Fetcher: testFetcher()}); err == nil {
		t.Error("a bad address is an error")
	}
}

func TestPSMPicksAnArticle(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wiki/") {
			fmt.Fprint(w, "ok")
			return
		}
		n++
		path := "/wiki/Popular_Science_Monthly/Volume_30/April_1887/The_History_of_a_Delusion"
		if n%2 == 1 {
			path = "/wiki/Popular_Science_Monthly/Volume_30/April_1887/Notes" // skipped
		}
		http.Redirect(w, r, path, http.StatusFound)
	}))
	defer srv.Close()
	swap(t, &psmRandom, srv.URL+"/random/")
	target, title, err := psmPick(context.Background(), Env{Fetcher: testFetcher()})
	if err != nil || !strings.HasSuffix(target, "/The_History_of_a_Delusion") {
		t.Fatalf("%q %v", target, err)
	}
	if title != "The History of a Delusion (April 1887)" {
		t.Errorf("title %q", title)
	}
}

func TestNatureFamily(t *testing.T) {
	srv := bhlServer(t)
	swap(t, &bhlBucket, srv.URL)
	old := natureSources
	t.Cleanup(func() { natureSources = old })
	natureSources = natureSources[:1] // BHL alone: the Wikisource side needs the network
	d, err := nature{}.Draw(context.Background(), Env{Fetcher: testFetcher()})
	if err != nil || !strings.HasPrefix(d.Why, "nature/Biodiversity Heritage Library · ") || !strings.HasPrefix(d.Target, "w5f:discover/bhl?") {
		t.Fatalf("draw: %+v %v", d, err)
	}
}
