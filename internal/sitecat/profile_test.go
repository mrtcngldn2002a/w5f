package sitecat

import (
	"path/filepath"
	"testing"
	"time"
)

func TestProfilesRoundTripAndIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalogs.toml")
	if ps, err := LoadAll(path); err != nil || len(ps) != 0 {
		t.Fatalf("missing file: %v %v", ps, err)
	}
	p := Profile{ID: IDFor("https://www.books.example.org/", nil), Name: "Example", Home: "https://www.books.example.org/",
		Added: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
		Search: SearchSpec{Kind: "form", Template: "https://books.example.org/s?q={q}",
			Fields: map[string]string{"author": "https://books.example.org/s?q={q}&in=a"}},
		Layout: Layout{Parent: "div.main > ul.results", Item: "li.book"}}
	if p.ID != "books-example-org" {
		t.Errorf("id = %q", p.ID)
	}
	if err := SaveAll(path, []Profile{p}); err != nil {
		t.Fatal(err)
	}
	ps, err := LoadAll(path)
	if err != nil || len(ps) != 1 {
		t.Fatalf("load: %v %v", ps, err)
	}
	got := ps[0]
	if got.Search.Fields["author"] == "" || got.Layout.Item != "li.book" || got.Search.MaxPages != 10 ||
		got.Search.MaxResults != 300 || got.Download.Prefer[0] != "epub" {
		t.Errorf("round trip/defaults: %+v", got)
	}
	if IDFor("https://books.example.org/other", ps) != "books-example-org-2" {
		t.Error("ids must be unique")
	}
	if Find(ps, "books-example-org") != 0 || Find(ps, "nope") != -1 {
		t.Error("find")
	}
}
