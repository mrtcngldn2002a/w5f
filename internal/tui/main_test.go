package tui

import (
	"os"
	"path/filepath"
	"testing"

	"w5f/internal/store"
)

// TUI tests never touch the owner's database or notes folder.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "w5f-tui")
	if err != nil {
		panic(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		panic(err)
	}
	store.SetDefault(db)
	os.Setenv("W5F_NOTES", filepath.Join(dir, "notes"))
	code := m.Run()
	db.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}
