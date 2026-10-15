package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
)

func TestSiteSearchAsksAndOpensTheResults(t *testing.T) {
	d := &doc.Document{URL: "https://blog.example/post", Title: "A post", Origin: "live",
		Links:    []doc.Link{{Href: "https://blog.example/other", Text: "other"}},
		Blocks:   []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Read the "}, {Text: "other post", Link: 1}}}},
		Searches: []doc.Search{{Action: "https://www.blog.example/", Field: "s", Label: "Search the blog"}}}
	m := sized(New("", "test"))
	next, _ := m.Update(loadedMsg{target: d.URL, doc: d, gen: loadGen.Load()})
	m = next.(Model)
	if !strings.Contains(m.View().Content, "→ search this site") {
		t.Fatalf("no search line:\n%s", m.View().Content)
	}
	if len(d.Blocks) != 1 || len(d.Links) != 1 {
		t.Error("the loaded document itself must not change")
	}
	next, _ = m.follow(siteSearchHref)
	m = next.(Model)
	if m.mode != modeSecret || !strings.Contains(m.View().Content, "SEARCH BLOG.EXAMPLE") || !strings.Contains(m.View().Content, "Search the blog") {
		t.Fatalf("form not open: mode %v\n%s", m.mode, m.View().Content)
	}
	next, _ = m.Update(tea.PasteMsg{Content: "weird fiction"})
	m = press(next.(Model), "enter")
	if m.loading != "https://www.blog.example/?s=weird+fiction" || m.mode != modeRead {
		t.Errorf("loading %q, mode %v", m.loading, m.mode)
	}
}

func TestSiteSearchLineStaysOffSavedCopiesAndW5FPages(t *testing.T) {
	d := &doc.Document{URL: "https://site.example/", Links: []doc.Link{{Href: "https://site.example/a"}},
		Blocks:   []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "a", Link: 1}}}},
		Searches: []doc.Search{{Action: "https://site.example/search", Field: "q"}}}
	v := withSiteSearch(d)
	if len(v.Blocks) != 2 || v.Links[len(v.Links)-1].Href != siteSearchHref {
		t.Fatalf("view: %+v", v)
	}
	back := withoutSiteSearch(v)
	if len(back.Blocks) != 1 || len(back.Links) != 1 || back.Links[0].Href != "https://site.example/a" {
		t.Errorf("saved copy keeps the line: %+v", back)
	}
	if withoutSiteSearch(d) != d {
		t.Error("a page without the line is left alone")
	}
	own := &doc.Document{URL: "w5f:welcome", Searches: d.Searches}
	if withSiteSearch(own) != own {
		t.Error("W5F's own pages get no search line")
	}

	m := sized(New("", "test"))
	plain := &doc.Document{URL: "https://site.example/b", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "b"}}}}}
	next, _ := m.Update(loadedMsg{target: plain.URL, doc: plain, gen: loadGen.Load()})
	next, _ = next.(Model).follow(siteSearchHref)
	if m = next.(Model); m.mode == modeSecret || m.status != "this page has no search box" {
		t.Errorf("mode %v status %q", m.mode, m.status)
	}
}

// The SCP wikis' box is W5F's own wiki search (set by the source layer).
func TestSiteSearchOnTheWikis(t *testing.T) {
	d := &doc.Document{URL: "https://scp-wiki.wikidot.com/scp-173", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "x"}}}},
		Searches: []doc.Search{{Action: "w5f:search/wikis", Field: "q"}}}
	m := sized(New("", "test"))
	next, _ := m.Update(loadedMsg{target: d.URL, doc: d, gen: loadGen.Load()})
	next, _ = next.(Model).follow(siteSearchHref)
	if m = next.(Model); !strings.Contains(m.View().Content, "SEARCH THE WIKIS") {
		t.Fatalf("form:\n%s", m.View().Content)
	}
	next, _ = m.Update(tea.PasteMsg{Content: "statue"})
	if m = press(next.(Model), "enter"); m.loading != "w5f:search/wikis?q=statue" {
		t.Errorf("loading %q", m.loading)
	}
}
