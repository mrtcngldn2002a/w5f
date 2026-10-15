package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sufiSite serves small copies of the four Sufi sources: an author's entry
// list on the TDV encyclopedia, a Masnavi book index, the Ibn Arabi
// Society's list of papers, and a Sacred Texts book with its chapters.
func sufiSite(t *testing.T) *httptest.Server {
	t.Helper()
	long := strings.Repeat("Let reason go, for his light burns reason up from head to foot. ", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/muellif/"):
			fmt.Fprint(w, `<html><body><a href="/hilye">HİLYE</a><a href="/hakkinda/ilim-dali--tasavvuf">Tasavvuf</a>
<div class="article-list-title">Müellifin telif ettiği maddeler</div>
<div class="madde_liste_satir"><a href="/abdal#1-tasavvuf">ABDAL / 1. Bölüm: TASAVVUF</a><br><span>Evliya zümresi.</span></div>
<div class="madde_liste_satir"><a href="/abdal#1-tasavvuf">ABDAL</a></div></body></html>`)
		case r.URL.Path == "/book2.html":
			fmt.Fprint(w, `<html><body><A HREF="masnavi.html">Masnavi</A><A HREF="book3.html">Book 3</A>
<A HREF="n-II-0697.html">The Real Beloved Is Not the Form (8/00)</A><a href="pdf/rumi.pdf">PDF</a></body></html>`)
		case r.URL.Path == "/articles/":
			fmt.Fprint(w, `<html><body><nav><a href="/events/">Events</a></nav><div class="entry-content">
<a href="/qunawi-on-the-one-wujud-william-chittick/">Qunawi on the One Wujud</a>
<a href="/wp-content/uploads/paper.pdf">PDF</a><a href="/contact/">Contact</a><a href="https://example.org/x/">Elsewhere</a>
</div></body></html>`)
		case r.URL.Path == "/isl/srg/index.htm":
			fmt.Fprint(w, `<html><body><a href="srg12.htm">Part VIII. Reason and Free-Will</a></body></html>`)
		case r.URL.Path == "/isl/srg/srg12.htm":
			fmt.Fprintf(w, `<html><head><title>The Secret Rose Garden: Part VIII</title></head><body><p>%s</p><a href="srg13.htm">Next</a></body></html>`, long)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSufiPicks(t *testing.T) {
	srv := sufiSite(t)
	oldTDV, oldMasnavi, oldIdx, oldMIAS, oldBase, oldBooks := tdvBase, masnaviBase, masnaviIndexes, miasArticles, sufiBase, sufiBooks
	defer func() {
		tdvBase, masnaviBase, masnaviIndexes, miasArticles, sufiBase, sufiBooks = oldTDV, oldMasnavi, oldIdx, oldMIAS, oldBase, oldBooks
	}()
	tdvBase, masnaviBase, masnaviIndexes = srv.URL, srv.URL+"/", []string{"book2.html"}
	miasArticles, sufiBase, sufiBooks = srv.URL+"/articles/", srv.URL+"/isl/", []string{"srg/index.htm"}
	env := Env{Fetcher: testFetcher()}
	for _, c := range []struct {
		name        string
		pick        func(context.Context, Env) (string, string, error)
		path, title string
	}{
		{"tdv", tdvPick, "/abdal", "ABDAL / 1. Bölüm: TASAVVUF"},
		{"masnavi", masnaviPick, "/n-II-0697.html", "The Real Beloved Is Not the Form (8/00)"},
		{"mias", miasPick, "/qunawi-on-the-one-wujud-william-chittick/", "Qunawi on the One Wujud"},
		{"sacred texts", sufiBookPick, "/isl/srg/srg12.htm", "The Secret Rose Garden: Part VIII"},
	} {
		target, title, err := c.pick(context.Background(), env)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if target != srv.URL+c.path || title != c.title {
			t.Errorf("%s: got %s [%s], want %s [%s]", c.name, target, title, srv.URL+c.path, c.title)
		}
	}
}

func TestSufismDrawSaysWhereFrom(t *testing.T) {
	srv := sufiSite(t)
	old := sufiSources
	defer func() { sufiSources = old }()
	oldMIAS := miasArticles
	defer func() { miasArticles = oldMIAS }()
	miasArticles = srv.URL + "/articles/"
	sufiSources = []litSource{
		{"Down", 1, func(context.Context, Env) (string, string, error) { return "", "", fmt.Errorf("down") }},
		{"Ibn Arabi Society", 1, miasPick},
	}
	for range 5 {
		d, err := sufism{}.Draw(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil {
			t.Fatal(err)
		}
		if d.Why != "sufism/Ibn Arabi Society · Qunawi on the One Wujud" {
			t.Errorf("why = %q", d.Why)
		}
	}
}

func TestSufiBookSkipsBackMatter(t *testing.T) {
	long := strings.Repeat("Abu Bakr al-Sufi Ibn Arabi Rumi Hafiz Attar Jami Sadi Sanai Ghazali, 1, 2, 3. ", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/isl/omy/index.htm":
			fmt.Fprint(w, `<html><body><a href="omy24.htm">Index</a><a href="omy.txt.gz">Text [Zipped]</a></body></html>`)
		case "/isl/omy/omy24.htm":
			fmt.Fprintf(w, `<html><head><title>Oriental Mysticism: Index | Internet Sacred Text Archive</title></head><body><p>%s</p></body></html>`, long)
		case "/isl/moi/moi.htm":
			fmt.Fprintf(w, `<html><body><p>%s</p></body></html>`, long)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBase, oldBooks := sufiBase, sufiBooks
	defer func() { sufiBase, sufiBooks = oldBase, oldBooks }()
	sufiBase, sufiBooks = srv.URL+"/isl/", []string{"omy/index.htm"}
	env := Env{Fetcher: testFetcher()}
	if target, title, err := sufiBookPick(context.Background(), env); err == nil {
		t.Errorf("an index was drawn: %s [%s]", target, title)
	}
	sufiBooks = []string{"moi/moi.htm"}
	if target, _, err := sufiBookPick(context.Background(), env); err != nil || target != srv.URL+"/isl/moi/moi.htm" {
		t.Errorf("a book on one page: %s, %v", target, err)
	}
}
