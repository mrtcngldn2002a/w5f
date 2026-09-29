package tui

import (
	"context"
	"testing"

	"w5f/internal/doc"
)

func TestCatalogLoadingTexts(t *testing.T) {
	cases := map[string]string{
		"w5f:catalog/check?url=x":               "inspecting the site",
		"w5f:catalog/add?url=x":                 "inspecting the site",
		"w5f:catalog/fadedpage-com/recheck":     "inspecting the site",
		"w5f:catalog/fadedpage-com?q=dracula":   "searching the site (all result pages)",
		"w5f:catalog/fadedpage-com/get?u=x&f=e": "downloading the book into your library",
		"w5f:books":                             "",
	}
	for in, want := range cases {
		if got := catalogLoading(in); got != want {
			t.Errorf("catalogLoading(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebPagesCannotFollowW5FLinks(t *testing.T) {
	m := sized(New("", "test"))
	m.cur = newPage("https://evil.example/post", &doc.Document{Title: "post"})
	next, cmd := m.follow("w5f:catalog/x/remove")
	if cmd != nil || next.(Model).loading != "" {
		t.Error("a web page must not trigger a w5f: action")
	}
	m.cur = newPage("w5f:catalog/x?q=dracula", &doc.Document{Title: "results"})
	if _, cmd := m.follow("w5f:catalog/x/item?u=y"); cmd == nil {
		t.Error("W5F's own pages must keep their links")
	}
}

func TestStaleLoadResultIsDropped(t *testing.T) {
	m := sized(New("", "test"))
	load("w5f:catalog/a?q=old", false) // generation n
	load("w5f:catalog/a?q=new", false) // generation n+1
	m.loading = "searching the site (all result pages)"
	next, _ := m.Update(loadedMsg{target: "w5f:catalog/a?q=old", doc: &doc.Document{Title: "old"}, gen: loadGen.Load() - 1})
	if next.(Model).cur != nil && next.(Model).cur.target == "w5f:catalog/a?q=old" {
		t.Error("a superseded load was shown")
	}
	cancelLoad()
}

func TestCancelledLoadKeepsNewerIndicator(t *testing.T) {
	m := sized(New("", "test"))
	m.loading = "searching the site (all result pages)"
	next, _ := m.Update(loadedMsg{target: "w5f:catalog/a?q=old", err: context.Canceled})
	if got := next.(Model).loading; got != "searching the site (all result pages)" {
		t.Errorf("loading = %q", got)
	}
	if got := next.(Model).status; got != "" {
		t.Errorf("a cancelled load must stay quiet, status = %q", got)
	}
}
