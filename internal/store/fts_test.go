package store

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func tmpDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPutAndFindDocs(t *testing.T) {
	db := tmpDB(t)
	put := func(target, kind, title, text string) {
		t.Helper()
		if err := db.PutDoc(IndexDoc{Target: target, Kind: kind, Title: title, Text: text, Updated: time.Now(),
			FoldTitle: title, FoldText: text}); err != nil {
			t.Fatal(err)
		}
	}
	put("https://a", "web", "statue", "a page")
	put("https://b", "scp", "other", "a statue in the text")
	put("w5f:item/1", "feed", "feed", "no match here")
	put("https://b", "scp", "other", "the statue text, updated") // replaces
	ds, err := db.FindDocs(`"statue"`, nil, 10, 0)
	if err != nil || len(ds) != 2 || ds[0].Target != "https://a" {
		t.Fatalf("title match should rank first: %v %+v", err, ds)
	}
	if ds[1].Text != "the statue text, updated" {
		t.Errorf("update lost: %+v", ds[1])
	}
	ds, _ = db.FindDocs(`"statue"`, []string{"scp", "wiki"}, 10, 0)
	if len(ds) != 1 || ds[0].Kind != "scp" {
		t.Errorf("kind filter: %+v", ds)
	}
	if db.CountDocs("https://") != 2 || db.CountDocs("w5f:item/") != 1 {
		t.Error("CountDocs")
	}
	if err := db.ClearIndex(); err != nil {
		t.Fatal(err)
	}
	if ds, _ := db.FindDocs(`"statue"`, nil, 10, 0); len(ds) != 0 || db.CountDocs("") != 0 {
		t.Error("ClearIndex left documents")
	}
}

func TestItemsToIndex(t *testing.T) {
	db := tmpDB(t)
	for i, g := range []string{"a", "b"} {
		if _, err := db.UpsertItem(Item{FeedID: "f", GUID: g, Title: "T" + g, Content: "<p>x</p>", Published: time.Unix(int64(1000+i), 0)}); err != nil {
			t.Fatal(err)
		}
	}
	its, err := db.ItemsToIndex(10)
	if err != nil || len(its) != 2 || its[0].Content != "<p>x</p>" || its[0].Published.IsZero() {
		t.Fatalf("items: %v %+v", err, its)
	}
	db.PutDoc(IndexDoc{Target: "w5f:item/" + itoa(its[0].ID), Kind: "feed", Title: "x", Updated: time.Now()})
	if its, _ := db.ItemsToIndex(10); len(its) != 1 {
		t.Errorf("indexed item still listed: %+v", its)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
