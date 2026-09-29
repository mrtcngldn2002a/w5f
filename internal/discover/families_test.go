package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func longText(n int) string {
	return strings.Repeat("The adept speaks of the hidden fire and the seven seals. ", n)
}

func TestDescendFindsATextPage(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			var b strings.Builder
			for i := 0; i < 12; i++ {
				fmt.Fprintf(&b, `<li><a href="/sect/">Section %d</a></li>`, i)
			}
			fmt.Fprintf(w, `<html><body><h1>Library</h1><ul>%s<li><a href="https://elsewhere.example/x">off-site</a></li><li><a href="/pic.jpg">image</a></li></ul></body></html>`, b.String())
		case "/sect/":
			var b strings.Builder
			for i := 0; i < 8; i++ {
				fmt.Fprintf(&b, `<li><a href="text%d.htm">Text %d</a></li>`, i, i)
			}
			fmt.Fprintf(w, `<html><body><ul>%s</ul></body></html>`, b.String())
		default:
			if strings.HasPrefix(r.URL.Path, "/sect/text") {
				fmt.Fprintf(w, `<html><head><title>The Hymn</title></head><body><p>%s</p><a href="/">home</a></body></html>`, longText(60))
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	target, title, err := descend(context.Background(), f, srv.URL+"/", 3)
	if err != nil || !strings.Contains(target, "/sect/text") || title != "The Hymn" {
		t.Fatalf("descend: %q %q %v", target, title, err)
	}
}

func TestTextfilesPicksAFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/occult/" {
			fmt.Fprint(w, `<html><body><TABLE><TR><TD><A HREF="chaos.txt">chaos.txt</A><TD>12345<TD>The Chaos Magick Primer (1993)
<TR><TD><A HREF="SUBDIR/">SUBDIR</A><TD>[DIR]<TD>more
<TR><TD><A HREF="tarot.txt">tarot.txt</A><TD>2222<TD>Notes on the Tarot</TABLE></body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	d, err := textfilesDraw(context.Background(), f, srv.URL, []string{"occult"})
	if err != nil || !strings.HasSuffix(d.Target, ".txt") || !strings.HasPrefix(d.Why, "textfiles/occult · ") {
		t.Fatalf("textfiles: %+v %v", d, err)
	}
	if !strings.Contains(d.Why, "Chaos Magick") && !strings.Contains(d.Why, "Tarot") {
		t.Errorf("the file's description belongs in the why line: %q", d.Why)
	}
}

func TestBrokenCommentPageStillGivesItsLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Browse/index.html" {
			// An unterminated comment swallows the rest of the page in HTML5 parsing.
			fmt.Fprint(w, `<HTML><HEAD><!--Copyright (C) 1994 -- all rights reserved <TITLE>Browse</TITLE></HEAD><BODY><A HREF="/Author/work.html">Work</A></BODY></HTML>`)
			return
		}
		fmt.Fprintf(w, `<html><head><title>The Work</title></head><body><p>%s</p></body></html>`, longText(60))
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	target, title, err := descendIn(context.Background(), f, srv.URL+"/Browse/index.html", "/", 3)
	if err != nil || !strings.HasSuffix(target, "/Author/work.html") || title != "The Work" {
		t.Errorf("broken page: %q %q %v", target, title, err)
	}
}

func TestWibySurprise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><meta http-equiv="refresh" content="0; URL='http://oldsite.example/home.html'"></head></html>`)
	}))
	defer srv.Close()
	old := wibySurprise
	wibySurprise = srv.URL + "/surprise/"
	defer func() { wibySurprise = old }()
	f := fetch.New("", "test")
	f.HostGap = 0
	d, err := wibyDraw(context.Background(), f)
	if err != nil || d.Target != "http://oldsite.example/home.html" || !strings.Contains(d.Why, "oldsite.example") {
		t.Errorf("wiby: %+v %v", d, err)
	}
}

func TestRandomRedirectIsResolvedFresh(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wiki/Special:Random" {
			n++
			http.Redirect(w, r, fmt.Sprintf("/wiki/Page_%d", n), http.StatusFound)
			return
		}
		fmt.Fprint(w, "<html><body>ok</body></html>")
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := fetch.New(dir, "test") // with a cache: the random address must not be served from it
	f.HostGap = 0
	a, _ := resolveRandom(context.Background(), f, srv.URL+"/wiki/Special:Random")
	b, _ := resolveRandom(context.Background(), f, srv.URL+"/wiki/Special:Random")
	if a == b || !strings.Contains(a, "Page_") {
		t.Errorf("random pages: %q %q", a, b)
	}
}

func TestWibyRouteOpensTheSurprise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><meta http-equiv="refresh" content="0; URL='http://oldsite.example/home.html'"></head><body>You asked for it!</body></html>`)
	}))
	defer srv.Close()
	old := wibySurprise
	wibySurprise = srv.URL + "/surprise/"
	defer func() { wibySurprise = old }()
	f := fetch.New("", "test")
	f.HostGap = 0
	var loaded string
	env := Env{Fetcher: f, Load: func(_ context.Context, target string) (*doc.Document, error) {
		loaded = target
		return &doc.Document{Title: "Old site"}, nil
	}}
	d, err := Route(context.Background(), "w5f:discover/wiby", env)
	if err != nil || loaded != "http://oldsite.example/home.html" || len(d.Blocks) == 0 {
		t.Fatalf("wiby route: %v %q", err, loaded)
	}
}
