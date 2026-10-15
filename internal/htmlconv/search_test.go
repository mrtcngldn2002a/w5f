package htmlconv

import (
	"testing"
)

func TestSearchFormsWordPressAndWikipedia(t *testing.T) {
	wp := `<html><body><header><form role="search" method="get" action="https://blog.example/">
		<input type="search" name="s" placeholder="Search …"><button type="submit">Search</button></form></header>
		<article><p>Text.</p></article></body></html>`
	got := SearchForms([]byte(wp), "https://blog.example/2026/10/post")
	if len(got) != 1 || got[0].Field != "s" || got[0].Label != "Search …" {
		t.Fatalf("wordpress: %+v", got)
	}
	if u := got[0].URL("weird fiction"); u != "https://blog.example/?s=weird+fiction" {
		t.Errorf("wordpress url = %s", u)
	}

	wiki := `<div id="p-search"><form action="/w/index.php" id="searchform">
		<input type="hidden" name="title" value="Special:Search">
		<input type="search" name="search" placeholder="Search Wikipedia" id="searchInput">
		<input type="submit" name="go" value="Go"></form></div>`
	got = SearchForms([]byte(wiki), "https://en.wikipedia.org/wiki/Gene_Wolfe")
	if len(got) != 1 || got[0].Label != "Search Wikipedia" {
		t.Fatalf("wikipedia: %+v", got)
	}
	if u := got[0].URL("book of the new sun"); u != "https://en.wikipedia.org/w/index.php?title=Special%3ASearch&go=Go&search=book+of+the+new+sun" {
		t.Errorf("wikipedia url = %s", u)
	}
}

func TestSearchFormsKeepsFixedValues(t *testing.T) {
	page := `<form action="search.php?old=1#top"><label for="w">Find a story</label><input id="w" name="q" value="ignored">
		<select name="in"><option value="all">All</option><option value="title" selected>Title</option></select>
		<select name="sort"><option>newest</option><option>oldest</option></select>
		<select name="tag" multiple><option selected>sea</option><option>sky</option><option selected value="ice">Ice</option></select>
		<select name="lang" multiple><option>en</option></select>
		<input type="checkbox" name="exact" checked><input type="checkbox" name="nsfw">
		<input type="text" name="author" disabled></form>`
	got := SearchForms([]byte(page), "http://stories.example/lib/index.html")
	if len(got) != 1 || got[0].Label != "Find a story" {
		t.Fatalf("got %+v", got)
	}
	// The query of the action is replaced, as a browser does.
	if u := got[0].URL("ghost"); u != "http://stories.example/lib/search.php?in=title&sort=newest&tag=sea&tag=ice&exact=on&q=ghost" {
		t.Errorf("url = %s", u)
	}
}

func TestSearchFormsLeavesOtherFormsOut(t *testing.T) {
	for name, page := range map[string]string{
		"login":      `<form action="/search"><input name="q"><input type="password" name="pw"></form>`,
		"post":       `<form method="post" action="/search"><input type="search" name="q"></form>`,
		"comment":    `<form action="/search"><input name="q"><textarea name="body"></textarea></form>`,
		"newsletter": `<form action="/subscribe"><input type="email" name="email"></form>`,
		"wikidot":    `<form id="search-top-box-form" action="dummy"><input name="query" class="search-top-box-input"></form>`,
		"script":     `<form action="javascript:go()"><input type="search" name="q"></form>`,
		"not search": `<form action="/rename"><input name="title"></form>`,
		"two boxes":  `<form action="/register"><input name="first"><input name="last"></form>`,
		"no box":     `<form action="/search"><input type="hidden" name="q" value="x"></form>`,
		"mailto":     `<form action="mailto:a@b.example"><input type="search" name="q"></form>`,
	} {
		if got := SearchForms([]byte(page), "https://site.example/"); len(got) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestSearchFormsOrderAndDuplicates(t *testing.T) {
	page := `<form action="/find"><input name="q"></form>
		<nav role="search"><form action="/search"><input type="search" name="query"></form></nav>
		<div class="mobile"><form action="/search"><input type="search" name="query"></form></div>`
	got := SearchForms([]byte(page), "https://site.example/a")
	if len(got) != 2 {
		t.Fatalf("the mobile copy is the same box: %+v", got)
	}
	if got[0].Action != "https://site.example/search" || got[1].Action != "https://site.example/find" {
		t.Errorf("the search landmark comes first: %+v", got)
	}
}

func TestSearchFormsWithoutActionAskThePage(t *testing.T) {
	got := SearchForms([]byte(`<form class="search"><input name="k"></form>`), "https://site.example/list?page=2")
	if len(got) != 1 || got[0].URL("x") != "https://site.example/list?k=x" {
		t.Fatalf("got %+v", got)
	}
}
