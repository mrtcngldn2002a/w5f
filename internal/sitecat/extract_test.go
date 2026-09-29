package sitecat

import (
	"fmt"
	"strings"
	"testing"
)

func page(body string) []byte { return []byte("<html><body>" + body + "</body></html>") }

func navMenu() string {
	var b strings.Builder
	b.WriteString(`<nav><ul class="menu">`)
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, `<li class="item"><a href="/section/%d">Sec %d</a></li>`, i, i)
	}
	b.WriteString(`</ul></nav>`)
	return b.String()
}

func TestLearnLayoutShapes(t *testing.T) {
	cases := map[string]string{
		"ul": navMenu() + `<div class="main"><ul class="results">
<li class="book"><a href="/book/1">Dracula</a> by Bram Stoker, 1897</li>
<li class="book"><a href="/book/2">Dracula's Guest</a> by Bram Stoker</li>
<li class="book"><a href="/book/3">The Jewel of Seven Stars</a> by Bram Stoker</li>
<li class="book"><a href="javascript:void(0)">x</a></li></ul></div>`,
		"table": `<table class="res"><tr><th>Title</th><th>Author</th></tr>
<tr><td><a href="book.php?id=11">Dracula</a></td><td>Stoker, Bram</td></tr>
<tr><td><a href="book.php?id=12">Carmilla</a></td><td>Le Fanu, J. Sheridan</td></tr>
<tr><td><a href="book.php?id=13">The Vampyre</a></td><td>Polidori, John</td></tr></table>`,
		"cards": `<div class="grid"><div class="card"><h3><a href="//books.example.org/b/a">Dracula</a></h3><p>Gothic novel</p><a href="/b/a#reviews">12 reviews</a></div>
<div class="card"><h3><a href="/b/b">Varney the Vampire</a></h3><p>Penny dreadful</p></div>
<div class="card"><h3><a href="/b/c">Uncle Silas</a></h3><p>Mystery</p></div></div>`,
	}
	want := map[string][2]string{
		"ul":    {"Dracula", "https://books.example.org/book/1"},
		"table": {"Dracula", "https://books.example.org/search/book.php?id=11"},
		"cards": {"Dracula", "https://books.example.org/b/a"},
	}
	for name, body := range cases {
		l, rs := LearnLayout(page(body), "https://books.example.org/search/?q=dracula")
		if l.Item == "" || len(rs) < 3 {
			t.Fatalf("%s: layout=%+v results=%d", name, l, len(rs))
		}
		if rs[0].Title != want[name][0] || rs[0].URL != want[name][1] {
			t.Errorf("%s: first = %+v", name, rs[0])
		}
		for _, r := range rs {
			if strings.HasPrefix(r.URL, "javascript:") || r.URL == "" {
				t.Errorf("%s: bad url %q", name, r.URL)
			}
		}
		// The stored layout extracts the same list again.
		if again := Results(page(body), "https://books.example.org/search/?q=dracula", l); len(again) != len(rs) {
			t.Errorf("%s: re-extract %d vs %d", name, len(again), len(rs))
		}
	}
	_, rs := LearnLayout(page(cases["ul"]), "https://books.example.org/search/?q=dracula")
	if !strings.Contains(rs[0].Extra, "Bram Stoker") {
		t.Errorf("extra = %q", rs[0].Extra)
	}
}

func TestExtractRelearnsWhenLayoutBreaks(t *testing.T) {
	body := page(`<ol class="hits"><li><a href="/x/1">One book</a></li><li><a href="/x/2">Two book</a></li><li><a href="/x/3">Three book</a></li></ol>`)
	rs, l := Extract(body, "https://s.example/q", Layout{Parent: "div.gone", Item: "li.old"})
	if len(rs) != 3 || l.Item == "li.old" {
		t.Errorf("relearn: %d %+v", len(rs), l)
	}
}

func TestNextPage(t *testing.T) {
	base := "https://s.example/search?q=dracula"
	cases := []struct{ body, url, want string }{
		{`<a rel="next" href="/search?q=dracula&page=2">2</a>`, base, "https://s.example/search?q=dracula&page=2"},
		{`<a href="?q=dracula&p=3">Next ›</a>`, base, "https://s.example/search?q=dracula&p=3"},
		{`<p>no links</p>`, "https://s.example/search?q=dracula&page=4", "https://s.example/search?page=5&q=dracula"},
		{`<a href="/search?q=dracula&start=25">2</a><a href="/search?q=dracula&start=50">3</a>`, base, "https://s.example/search?q=dracula&start=25"},
		{`<p>nothing</p>`, base, ""},
	}
	for i, c := range cases {
		if got := NextPage(page(c.body), c.url); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestLearnLayoutPrefersResultsOverLongNavMenu(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<header><ul class="menu">`)
	for i := 0; i < 15; i++ {
		fmt.Fprintf(&b, `<li><a href="/topics/%d">Browse the collection of gothic topic %d</a></li>`, i, i)
	}
	b.WriteString(`</ul></header><ol class="found">
<li><a href="/b/1">Dracula</a> Stoker</li><li><a href="/b/2">Carmilla</a> Le Fanu</li><li><a href="/b/3">The Vampyre</a> Polidori</li></ol>`)
	_, rs := LearnLayout(page(b.String()), "https://s.example/search?q=vampire")
	if len(rs) != 3 || rs[0].Title != "Dracula" {
		t.Errorf("picked %d results, first %+v", len(rs), rs)
	}
}
