package sitecat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRound1ProfilesAndNewDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalogs.toml")
	old := `[[catalog]]
id = "shelf"
name = "Small Shelf"
home = "https://shelf.example/"
[catalog.search]
kind = "none"
template = ""
max_pages = 0
max_results = 0
[catalog.layout]
parent = "html > body > ul.books"
item = "li"
`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	ps, err := LoadAll(path)
	if err != nil || len(ps) != 1 {
		t.Fatalf("load: %v %v", ps, err)
	}
	if ps[0].Search.Kind != "browse" || ps[0].Search.MaxPages != 10 || ps[0].Layout.Item != "li" {
		t.Errorf("round-1 profile: %+v", ps[0])
	}
	e := Profile{Search: SearchSpec{Kind: "engine"}}
	e.ApplyDefaults()
	if e.Search.MaxPages != 3 || e.Search.MaxResults != 60 {
		t.Errorf("engine defaults: %+v", e.Search)
	}
	ix := Profile{Search: SearchSpec{Kind: "index"}}
	ix.ApplyDefaults()
	if ix.Search.MaxFolders != 150 {
		t.Errorf("index defaults: %+v", ix.Search)
	}
	post := Profile{ID: "p", Search: SearchSpec{Kind: "post", Method: "POST", Template: "https://old.example/find.asp",
		Body: "query={q}&section=books", Fields: map[string]string{"author": "author={q}"}}}
	if err := SaveAll(path, []Profile{post}); err != nil {
		t.Fatal(err)
	}
	ps, _ = LoadAll(path)
	if ps[0].Search.Method != "POST" || ps[0].Search.Body != "query={q}&section=books" || ps[0].Search.Fields["author"] != "author={q}" {
		t.Errorf("post round trip: %+v", ps[0].Search)
	}
}
