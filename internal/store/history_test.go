package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistory(t *testing.T) {
	db := tmpDB(t)
	db.Visit("https://a", "A", "web", "WEB·A·home")
	db.Visit("https://b", "B", "scp", "FIC·SCP·173")
	db.Visit("https://a", "A again", "web", "WEB·A·home")
	vs, err := db.History(0, 0)
	if err != nil || len(vs) != 2 || vs[0].Target != "https://a" || vs[0].Opens != 2 || vs[0].Title != "A again" {
		t.Fatalf("history: %v %+v", err, vs)
	}
	db.SavePos("https://a", 0.4)
	db.SavePos("https://b", 0.99)
	db.Visit("w5f:book/1/ch/2", "Book", "book", "BK·LOC·1")
	db.SavePos("w5f:book/1/ch/2", 0.5)
	un, _ := db.Unfinished(5)
	if len(un) != 1 || un[0].Target != "https://a" || un[0].Pos != 0.4 {
		t.Errorf("unfinished (books are listed from the library instead): %+v", un)
	}
	if vs, _ := db.History(1, 1); len(vs) != 1 || vs[0].Target != "https://a" {
		t.Errorf("paging: %+v", vs)
	}
}

func TestClearHistoryAndAside(t *testing.T) {
	db := tmpDB(t)
	db.Visit("https://a", "A", "web", "")
	db.Visit("https://b", "B", "web", "")
	db.SavePos("https://a", 0.4)
	db.SavePos("https://b", 0.6)

	// Set aside: off the desk until it is opened again.
	if err := db.SetAside("https://a"); err != nil {
		t.Fatal(err)
	}
	if un, _ := db.Unfinished(5); len(un) != 1 || un[0].Target != "https://b" {
		t.Errorf("set aside still on the desk: %+v", un)
	}
	db.Visit("https://a", "A", "web", "")
	if un, _ := db.Unfinished(0); len(un) != 2 {
		t.Errorf("opened again, back on the desk: %+v", un)
	}

	// Clearing: the table empties, the log is kept under a dated name and
	// a deleted database no longer brings the old history back.
	kept, err := db.ClearHistory(time.Date(2026, 10, 1, 20, 15, 0, 0, time.UTC))
	if err != nil || filepath.Base(kept) != "history-20261001-201500.log" {
		t.Fatalf("clear: %v %q", err, kept)
	}
	if b, err := os.ReadFile(kept); err != nil || !strings.Contains(string(b), "https://a") {
		t.Errorf("kept log: %v", err)
	}
	if vs, _ := db.History(0, 0); len(vs) != 0 {
		t.Errorf("history after clearing: %+v", vs)
	}
	if n, _ := db.RestoreHistory(); n != 0 {
		t.Errorf("restored %d pages after clearing", n)
	}
	if log, _ := db.VisitLog(time.Time{}); len(log) != 0 {
		t.Errorf("visit log after clearing: %+v", log)
	}
	db.Visit("https://c", "C", "web", "")
	if log, _ := db.VisitLog(time.Time{}); len(log) != 1 {
		t.Errorf("a new log begins: %+v", log)
	}
}
