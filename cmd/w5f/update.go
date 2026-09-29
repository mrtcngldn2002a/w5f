package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"w5f/internal/doc"
	"w5f/internal/render"
	"w5f/internal/store"
	"w5f/internal/update"
)

// updateConfig reads [update] repo from config.toml (else the build's repo).
func updateConfig() (update.Config, error) {
	var cfg struct {
		Update struct {
			Repo string `toml:"repo"`
		} `toml:"update"`
	}
	_, _ = toml.DecodeFile(filepath.Join(store.DataDir(), "config.toml"), &cfg)
	repo := strings.TrimSpace(cfg.Update.Repo)
	if repo == "" {
		repo = update.DefaultRepo
	}
	exe, err := update.Executable()
	if err != nil {
		return update.Config{}, err
	}
	// W5F_UPDATE_API points at a test server; signatures are checked all the same.
	return update.Config{Repo: repo, API: os.Getenv("W5F_UPDATE_API"), Exe: exe, Current: version}, nil
}

// runUpdate: w5f update [--check | --rollback]
func runUpdate(args []string) int {
	c, err := updateConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	switch mode {
	case "--rollback":
		if err := update.Rollback(c.Exe); err != nil {
			fmt.Fprintln(os.Stderr, "w5f:", err)
			return 1
		}
		fmt.Println("Rolled back. The version you left is kept; run --rollback again to return to it.")
		return 0
	case "", "--check":
	default:
		fmt.Fprintln(os.Stderr, "usage: w5f update [--check | --rollback]")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	rel, err := c.Check(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	if rel == nil {
		fmt.Println("W5F", version, "is up to date.")
		return 0
	}
	fmt.Printf("New version: %s → %s (%s, %.1f MB)\n", version, rel.Manifest.Version, rel.Manifest.Date, float64(rel.Asset.Size)/(1<<20))
	if mode == "--check" {
		fmt.Println("Install it with: w5f update")
		return 0
	}
	fmt.Println("Downloading and checking…")
	if err := c.Apply(ctx, rel); err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	fmt.Printf("Updated to %s. Restart W5F to use it; 'w5f update --rollback' goes back.\n", rel.Manifest.Version)
	return 0
}

// selftest checks that this binary works, without touching user data: it
// lays out a page and opens an in-memory search index.
func selftest() int {
	d := &doc.Document{Title: "Self-test", Blocks: []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "ğ Ğ ı İ ş Ş ç Ç ö Ö ü Ü ─│┌┐└┘"}}},
		doc.Paragraph{Text: doc.Inline{{Text: strings.Repeat("The archive keeps what the world forgets. ", 40)}}},
	}}
	if l := render.Render(d, render.Options{Width: 72}); len(l.Lines) < 10 {
		fmt.Fprintln(os.Stderr, "selftest: layout failed")
		return 1
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err == nil {
		_, err = db.Exec(`CREATE VIRTUAL TABLE t USING fts5(body); INSERT INTO t VALUES ('archive node'); SELECT * FROM t WHERE t MATCH 'archive'`)
		db.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "selftest: search index:", err)
		return 1
	}
	fmt.Println("w5f", version)
	return 0
}
