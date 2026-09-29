package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryLogSurvivesDatabaseDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w5f.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Visit("https://a", "A", "web", "WEB·A·home")
	db.Visit("https://b", "B", "scp", "FIC·SCP·173")
	db.Visit("https://a", "A", "web", "WEB·A·home")
	db.Close()
	os.Remove(path)
	db, _ = Open(path)
	defer db.Close()
	if vs, _ := db.History(0, 0); len(vs) != 0 {
		t.Fatal("new database should start empty")
	}
	n, err := db.RestoreHistory()
	vs, _ := db.History(0, 0)
	if err != nil || n != 2 || len(vs) != 2 || vs[0].Target != "https://a" || vs[0].Opens != 2 || vs[1].Kind != "scp" {
		t.Errorf("restored %d %v %+v", n, err, vs)
	}
	if n, _ := db.RestoreHistory(); n != 0 {
		t.Error("restore only fills an empty history")
	}
}
