package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheoiTakesAnEntryOrABook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/greek-mythology/encyclopedia-a.html", "/greek-mythology/encyclopedia-c.html":
			fmt.Fprint(w, `<a href="encyclopedia-a.html">A</a><a href="encyclopedia-c.html">C</a><a href="greek-gods.html">Gods</a>
<a href="../Bibliography.html">Bibliography</a><a href="../Galleries.html">Galleries</a><a href="../Text/HomerIliad1.html">Iliad</a>
<a href="../Nymphe/NympheKalypso.html">Kalypso</a><a href="../Daimon/Adikia.html">Adikia</a>`)
		case "/Library.html":
			fmt.Fprint(w, `<a href="Text/HomerIliad1.html">Homer, Iliad</a><a href="Galleries.html">Galleries</a><a href="/articles/x/">Article</a>`)
		case "/Text/HomerIliad1.html":
			fmt.Fprint(w, `<a href="HomerIliad1.html">Book 1</a><a href="HomerIliad2.html">Book 2</a><a href="HomericHymns1.html">Hymns</a><a href="../Library.html">Library</a>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := testFetcher()
	kinds := map[string]bool{}
	for range 40 {
		target, title, err := theoiPick(context.Background(), f, srv.URL+"/")
		if err != nil {
			t.Fatal(err)
		}
		path := strings.TrimPrefix(target, srv.URL)
		switch path {
		case "/Nymphe/NympheKalypso.html", "/Daimon/Adikia.html":
			kinds["entry"] = true
		case "/Text/HomerIliad1.html", "/Text/HomerIliad2.html":
			kinds["book"] = true
			if title != "Homer, Iliad" {
				t.Errorf("a book is called by its work: %q", title)
			}
		default:
			t.Fatalf("%s is neither an entry nor a book of the Iliad", path)
		}
	}
	if !kinds["entry"] || !kinds["book"] {
		t.Errorf("both kinds are drawn: %v", kinds)
	}
}
