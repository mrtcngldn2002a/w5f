# M4 Personal Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give W5F a personal layer — reading queue, notes, clippings, saved pages, reading history with "Continue reading", and `/` full-text search over everything read — with the owner's data kept as Obsidian-compatible Markdown.

**Architecture:** Markdown files in a notes folder are the owner's data (`internal/personal`); SQLite holds only rebuildable data: an FTS5 index over folded text (`internal/store/fts.go`, `internal/index`) and the reading history (`internal/store/history.go`). The TUI records every opened content page (history + index) in a background command, and adds keys `/ a A n y s H`, a note box and a clipping mode.

**Tech Stack:** Go 1.26 toolchain, modernc.org/sqlite (FTS5), BurntSushi/toml, Bubble Tea v2, existing `doc`, `render`, `books`, `feeds`, `htmlconv`, `source` packages.

**Spec:** `docs/superpowers/specs/2026-09-28-personal-layer-design.md`

## Global Constraints

- The owner's data (queue, notes, clippings, saved pages) lives only in Markdown files; SQLite holds only the search index and the reading history, both rebuildable (`w5f reindex`).
- Notes folder: `W5F_NOTES`, else `notes = "…"` in `<DataDir>/config.toml`, else `~/Archive/Notes`.
- Files: `Queue.md`, `Notes/<Title>.md`, `Clippings/YYYY/MM/YYYY-MM-DD.md`, `Saved/<Title>.md`; YAML frontmatter with `tags: [w5f, …]`.
- Index text ≤ 200 KB per document; snippets ≈ 160 characters; 30 results per page; ranking `bm25(docs_fts, 5.0, 1.0)` then newest.
- Folding is one rune in → one rune out (Turkish `ı İ ş ğ ü ö ç` and Latin diacritics); SQLite's `remove_diacritics` does not fold `ı`.
- Indexing never blocks the reader: it runs in a Bubble Tea command or a goroutine.
- Tests never touch the owner's database or notes: `internal/tui` gets a `TestMain` with a temporary database (`store.SetDefault`) and `W5F_NOTES`.
- UI text English; pure Go (CGO off); must keep building for `linux/amd64` `GOAMD64=v1` and `linux/386`.
- After Python-based edits run `gofmt -w .`. No commits unless the owner asks (repo has no commits).

## Notes on the spec

- Spec §4 "only content pages are recorded": content = what `index.Kind` classifies (web, wiki/SCP, Reddit, feed items via `Ref item:`, book chapters via `Ref book:`); local files other than notes are not recorded.
- Spec §5 "book … indexed when it enters the library": the Library page (`w5f:books`) and every opened chapter start a background pass that indexes books whose chapters are not indexed yet (new downloads land on a chapter page, so they are indexed right away).
- Spec §5 "feed … when sync stores the item": the Periodicals pages and `w5f sync` index feed items not yet in the index.
- The kind label table lives in `internal/catalog` (`catalog.Label`) so both the search page and the history page use it without an import cycle.

## Review Focus

1. `Queue.md` edited in Obsidian — CRLF line endings, the owner's own lines and sections, entries without a catalog number, URLs with spaces — must still parse and every unknown line must survive a change (test in Task 6).
2. Titles with characters illegal on Windows or in Obsidian links, very long titles, and two different sources with the same title — safe, distinct file names; the same source reuses its file (test in Task 5).
3. Search input containing FTS5 syntax (`"`, `*`, `-`, `AND`, `NEAR(`, unbalanced quotes, only punctuation) — never an SQL error, sensible results (test in Task 3).
4. A 5 MB page — its indexed text is capped at 200 KB (test in Task 3).
5. The database deleted — notes and queue files are intact and `index.Rebuild` restores search from files, cache and stores (test in Task 7).

---

### Task 1: Catalog numbers and folding

**Files:**
- Create: `internal/catalog/catalog.go`, `internal/catalog/catalog_test.go`
- Create: `internal/index/fold.go`, `internal/index/fold_test.go`
- Modify: `internal/doc/doc.go` (field `Catalog`)
- Modify: `internal/feeds/docs.go` (`itemDoc` sets `d.Catalog`), `internal/books/docs.go` (`chapterDoc` sets `d.Catalog` at both `d.Ref` assignments)

**Interfaces:**
- Produces: `doc.Document.Catalog string`; `catalog.Number(target string, d *doc.Document) string`; `catalog.Feed(feedID string, published time.Time) string`; `catalog.Book(source string, id int64) string`; `catalog.Label(kind string) string`; `index.Fold(s string) string`

- [ ] **Step 1: Failing tests**

`internal/catalog/catalog_test.go`:

```go
package catalog

import (
	"testing"
	"time"

	"w5f/internal/doc"
)

func TestNumber(t *testing.T) {
	cases := map[string]string{
		"https://scp-wiki.wikidot.com/scp-173":                                               "FIC·SCP·173",
		"https://scp-wiki.wikidot.com/scp-173-j":                                             "FIC·SCP·scp-173-j",
		"https://wanderers-library.wikidot.com/six-etchings-in-the-basalt-of-olympus-mons":   "FIC·WL·six-etchings-in-the-basa",
		"https://backrooms-wiki.wikidot.com/level-0":                                         "FIC·BR·level-0",
		"https://www.reddit.com/r/nosleep/comments/abc/a_story/":                             "WEB·RDT·nosleep",
		"https://arkeofili.com/gobekli-tepe/":                                                "WEB·ARKEOFIL·gobekli-tepe",
		"https://example.org/":                                                               "WEB·EXAMPLE·home",
		"w5f:feeds":                                                                          "",
	}
	for in, want := range cases {
		if got := Number(in, &doc.Document{}); got != want {
			t.Errorf("Number(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Number("https://x.example/a", &doc.Document{Catalog: "PER·ARKEOF·2026-09-24"}); got != "PER·ARKEOF·2026-09-24" {
		t.Errorf("document catalog not used: %q", got)
	}
	if got := Number("https://scp-wiki.wikidot.com/scp-096", nil); got != "FIC·SCP·096" {
		t.Errorf("nil document: %q", got)
	}
}

func TestFeedBookLabel(t *testing.T) {
	if got := Feed("arkeofili", time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)); got != "PER·ARKEOF·2026-09-24" {
		t.Errorf("Feed = %q", got)
	}
	if got := Feed("nasa", time.Time{}); got != "PER·NASA" {
		t.Errorf("Feed without date = %q", got)
	}
	books := map[string]string{"gutenberg:345": "BK·GUT·345", "se:bram-stoker/dracula": "BK·SE·dracula",
		"site:fadedpage-com:https://x/1": "BK·FADEDPAG·9", "": "BK·LOC·9"}
	for src, want := range books {
		if got := Book(src, 9); got != want {
			t.Errorf("Book(%q) = %q, want %q", src, got, want)
		}
	}
	if Label("feed") != "[RSS]" || Label("clip") != "[KES]" || Label("other") != "[OTHER]" {
		t.Error("labels")
	}
}
```

`internal/index/fold_test.go`:

```go
package index

import (
	"testing"
	"unicode/utf8"
)

func TestFoldKeepsPositions(t *testing.T) {
	cases := map[string]string{
		"Kafatası İSTANBUL Şehir": "kafatasi istanbul sehir",
		"Café Ñandú Øre":          "cafe nandu ore",
		"çğıöşü ÇĞIÖŞÜ":           "cgiosu cgiosu",
		"plain text 173":          "plain text 173",
	}
	for in, want := range cases {
		got := Fold(in)
		if got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(in) {
			t.Errorf("Fold(%q) changed the rune count", in)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/catalog ./internal/index` → FAIL (packages missing)

- [ ] **Step 3: Implement** — add to `doc.Document` (after `Ref string`):

```go
	// Catalog is the library call number when the adapter knows it
	// (feed items, books); pages derive theirs from the address.
	Catalog string
```

`internal/catalog/catalog.go`:

```go
// Package catalog gives what W5F shows a library call number in the style
// of the design (FIC·SCP·173, PER·ARKEOF·2026-09-24, BK·GUT·345).
package catalog

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"w5f/internal/doc"
)

var reSCPNum = regexp.MustCompile(`^scp-(\d{3,4})$`)

// Number returns the call number of a page; "" for W5F's own menus.
func Number(target string, d *doc.Document) string {
	if d != nil && d.Catalog != "" {
		return d.Catalog
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	slug := orHome(lastSegment(u.Path))
	switch {
	case host == "scp-wiki.wikidot.com":
		if m := reSCPNum.FindStringSubmatch(slug); m != nil {
			return "FIC·SCP·" + m[1]
		}
		return "FIC·SCP·" + cut(slug, 24)
	case host == "wanderers-library.wikidot.com":
		return "FIC·WL·" + cut(slug, 24)
	case host == "backrooms-wiki.wikidot.com":
		return "FIC·BR·" + cut(slug, 24)
	case strings.HasSuffix(host, ".wikidot.com"):
		return "FIC·" + code(strings.TrimSuffix(host, ".wikidot.com"), 8) + "·" + cut(slug, 24)
	case host == "reddit.com" || strings.HasSuffix(host, ".reddit.com"):
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "r" {
			return "WEB·RDT·" + cut(strings.ToLower(parts[1]), 24)
		}
		return "WEB·RDT"
	}
	return "WEB·" + code(strings.Split(host, ".")[0], 8) + "·" + cut(slug, 24)
}

// Feed numbers a periodical item.
func Feed(feedID string, published time.Time) string {
	n := "PER·" + code(feedID, 6)
	if !published.IsZero() {
		n += "·" + published.Format("2006-01-02")
	}
	return n
}

// Book numbers a library book from its source ("gutenberg:345", "se:slug",
// "site:<catalog>:<url>"); local files get their library id.
func Book(source string, id int64) string {
	switch {
	case strings.HasPrefix(source, "gutenberg:"):
		return "BK·GUT·" + strings.TrimPrefix(source, "gutenberg:")
	case strings.HasPrefix(source, "se:"):
		return "BK·SE·" + cut(lastSegment(strings.TrimPrefix(source, "se:")), 24)
	case strings.HasPrefix(source, "site:"):
		cat := strings.SplitN(strings.TrimPrefix(source, "site:"), ":", 2)[0]
		return fmt.Sprintf("BK·%s·%d", code(cat, 8), id)
	}
	return fmt.Sprintf("BK·LOC·%d", id)
}

var labels = map[string]string{"scp": "[SCP]", "wiki": "[WIKI]", "web": "[WEB]", "reddit": "[RDT]",
	"feed": "[RSS]", "book": "[BK]", "note": "[NOT]", "clip": "[KES]", "saved": "[SAV]"}

// Label is the short kind tag shown in lists ("[RSS]", "[BK]"…).
func Label(kind string) string {
	if l, ok := labels[kind]; ok {
		return l
	}
	return "[" + strings.ToUpper(kind) + "]"
}

func lastSegment(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	s := path.Base(p)
	if un, err := url.PathUnescape(s); err == nil {
		s = un
	}
	return strings.ToLower(s)
}

func orHome(s string) string {
	if s == "" {
		return "home"
	}
	return s
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func code(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' {
			return -1
		}
		return r
	}, s)
	return cut(strings.ToUpper(s), n)
}
```

`internal/index/fold.go`:

```go
// Package index is W5F's full-text search over everything read: folding,
// indexing documents, searching and the results page.
package index

import (
	"strings"
	"unicode"
)

// foldMap maps a lower-case letter to its base letter, one rune to one rune.
var foldMap = map[rune]rune{
	'ı': 'i', 'ş': 's', 'ğ': 'g', 'ü': 'u', 'ö': 'o', 'ç': 'c',
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'ā': 'a', 'ă': 'a', 'ą': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e', 'ė': 'e', 'ę': 'e', 'ě': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i', 'į': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ø': 'o', 'ō': 'o', 'ő': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ū': 'u', 'ů': 'u', 'ű': 'u', 'ų': 'u',
	'ñ': 'n', 'ń': 'n', 'ň': 'n', 'ý': 'y', 'ÿ': 'y', 'ć': 'c', 'č': 'c', 'ď': 'd',
	'ł': 'l', 'ľ': 'l', 'ř': 'r', 'ś': 's', 'š': 's', 'ť': 't', 'ź': 'z', 'ż': 'z', 'ž': 'z',
	'’': '\'', '‘': '\'',
}

// Fold lower-cases s and removes diacritics (Turkish ı and İ included),
// one rune for one rune, so positions in folded text match the original.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

func foldRune(r rune) rune {
	if r == 'İ' {
		return 'i'
	}
	r = unicode.ToLower(r)
	if f, ok := foldMap[r]; ok {
		return f
	}
	return r
}
```

In `internal/feeds/docs.go` `itemDoc`, after `d := &doc.Document{…Ref: fmt.Sprintf("item:%d", id)}` add:

```go
	d.Catalog = catalog.Feed(it.FeedID, it.Published)
```

In `internal/books/docs.go` `chapterDoc`, after `d.Ref, d.Resume = fmt.Sprintf("book:%d:0", b.ID), resume` add `d.Catalog = catalog.Book(b.Source, b.ID)`, and after `d.Ref = fmt.Sprintf("book:%d:%d", b.ID, n)` add the same line. Import `"w5f/internal/catalog"` in both files.

- [ ] **Step 4: Run** `gofmt -w internal && go vet ./... && go test ./internal/catalog ./internal/index ./internal/feeds ./internal/books` → PASS

---

### Task 2: Store — full-text tables and reading history

**Files:**
- Create: `internal/store/fts.go`, `internal/store/history.go`
- Test: `internal/store/fts_test.go`, `internal/store/history_test.go`

**Interfaces:**
- Produces:
  - `type IndexDoc struct { ID int64; Target, Kind, Title, Catalog, Text string; Updated time.Time; FoldTitle, FoldText string }`
  - `func (db *DB) PutDoc(d IndexDoc) error`
  - `func (db *DB) FindDocs(match string, kinds []string, limit, offset int) ([]IndexDoc, error)`
  - `func (db *DB) CountDocs(prefix string) int`, `func (db *DB) ClearIndex() error`, `func (db *DB) ItemsToIndex(limit int) ([]Item, error)`
  - `type Visit struct { Target, Title, Kind, Catalog string; First, Last time.Time; Opens int; Pos float64 }`
  - `func (db *DB) Visit(target, title, kind, catalog string) error`, `func (db *DB) SavePos(target string, pos float64) error`, `func (db *DB) History(limit, offset int) ([]Visit, error)` (limit ≤ 0 = all), `func (db *DB) Unfinished(limit int) ([]Visit, error)`

- [ ] **Step 1: Failing tests**

`internal/store/fts_test.go`:

```go
package store

import (
	"path/filepath"
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
```

(`itoa` — add to the test file: `func itoa(n int64) string { return strconv.FormatInt(n, 10) }` with `"strconv"` imported.)

`internal/store/history_test.go`:

```go
package store

import "testing"

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
```

- [ ] **Step 2: Run** `go test ./internal/store -run 'Docs|ItemsToIndex|History'` → FAIL (undefined `PutDoc`, `Visit`)

- [ ] **Step 3: Implement** `internal/store/fts.go`:

```go
package store

import (
	"strings"
	"time"
)

const ftsSchema = `
CREATE TABLE IF NOT EXISTS docs (
  id      INTEGER PRIMARY KEY,
  target  TEXT NOT NULL UNIQUE,
  kind    TEXT NOT NULL,
  title   TEXT NOT NULL,
  catalog TEXT NOT NULL DEFAULT '',
  text    TEXT NOT NULL,
  updated INTEGER NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(title, body, tokenize='unicode61');
`

func init() { schemaExtras = append(schemaExtras, ftsSchema) }

// IndexDoc is one searchable document. Title and Text are shown; the folded
// forms are what the full-text index matches.
type IndexDoc struct {
	ID        int64
	Target    string
	Kind      string
	Title     string
	Catalog   string
	Text      string
	Updated   time.Time
	FoldTitle string
	FoldText  string
}

// PutDoc adds a document to the index or replaces the one with its target.
func (db *DB) PutDoc(d IndexDoc) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`INSERT INTO docs(target,kind,title,catalog,text,updated) VALUES(?,?,?,?,?,?)
	  ON CONFLICT(target) DO UPDATE SET kind=excluded.kind, title=excluded.title, catalog=excluded.catalog,
	    text=excluded.text, updated=excluded.updated
	  RETURNING id`, d.Target, d.Kind, d.Title, d.Catalog, d.Text, d.Updated.Unix()).Scan(&id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM docs_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO docs_fts(rowid,title,body) VALUES(?,?,?)`, id, d.FoldTitle, d.FoldText); err != nil {
		return err
	}
	return tx.Commit()
}

// FindDocs runs an FTS5 query (over folded text), best matches first.
func (db *DB) FindDocs(match string, kinds []string, limit, offset int) ([]IndexDoc, error) {
	q := `SELECT d.id,d.target,d.kind,d.title,d.catalog,d.text,d.updated
	  FROM docs_fts JOIN docs d ON d.id=docs_fts.rowid WHERE docs_fts MATCH ?`
	args := []any{match}
	if len(kinds) > 0 {
		q += ` AND d.kind IN (` + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	q += ` ORDER BY bm25(docs_fts, 5.0, 1.0), d.updated DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := db.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IndexDoc
	for rows.Next() {
		var d IndexDoc
		var up int64
		if err := rows.Scan(&d.ID, &d.Target, &d.Kind, &d.Title, &d.Catalog, &d.Text, &up); err != nil {
			return nil, err
		}
		d.Updated = time.Unix(up, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// CountDocs counts indexed documents whose target starts with prefix.
func (db *DB) CountDocs(prefix string) int {
	var n int
	_ = db.sql.QueryRow(`SELECT count(*) FROM docs WHERE substr(target,1,?)=?`, len(prefix), prefix).Scan(&n)
	return n
}

// ClearIndex empties the full-text index (before a rebuild).
func (db *DB) ClearIndex() error {
	if _, err := db.sql.Exec(`DELETE FROM docs`); err != nil {
		return err
	}
	_, err := db.sql.Exec(`DELETE FROM docs_fts`)
	return err
}

// ItemsToIndex lists feed items that are not in the index yet, newest first.
func (db *DB) ItemsToIndex(limit int) ([]Item, error) {
	rows, err := db.sql.Query(`SELECT id,feed_id,url,title,author,published,summary,content FROM items
	  WHERE ('w5f:item/'||id) NOT IN (SELECT target FROM docs) ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var pub int64
		if err := rows.Scan(&it.ID, &it.FeedID, &it.URL, &it.Title, &it.Author, &pub, &it.Summary, &it.Content); err != nil {
			return nil, err
		}
		if pub > 0 {
			it.Published = time.Unix(pub, 0)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
```

`internal/store/history.go`:

```go
package store

import "time"

const historySchema = `
CREATE TABLE IF NOT EXISTS history (
  target  TEXT PRIMARY KEY,
  title   TEXT NOT NULL,
  kind    TEXT NOT NULL,
  catalog TEXT NOT NULL DEFAULT '',
  first   INTEGER NOT NULL,
  last    INTEGER NOT NULL,
  opens   INTEGER NOT NULL DEFAULT 1,
  pos     REAL NOT NULL DEFAULT 0
);`

func init() { schemaExtras = append(schemaExtras, historySchema) }

// Visit is one entry of the reading history.
type Visit struct {
	Target, Title, Kind, Catalog string
	First, Last                  time.Time
	Opens                        int
	Pos                          float64 // scroll position 0–1 when last left
}

// Visit records that a content page was opened.
func (db *DB) Visit(target, title, kind, catalog string) error {
	now := time.Now().UnixNano()
	_, err := db.sql.Exec(`INSERT INTO history(target,title,kind,catalog,first,last,opens) VALUES(?,?,?,?,?,?,1)
	  ON CONFLICT(target) DO UPDATE SET title=excluded.title, kind=excluded.kind, catalog=excluded.catalog,
	    last=excluded.last, opens=opens+1`, target, title, kind, catalog, now, now)
	return err
}

// SavePos remembers how far a page was read.
func (db *DB) SavePos(target string, pos float64) error {
	_, err := db.sql.Exec(`UPDATE history SET pos=? WHERE target=?`, pos, target)
	return err
}

// History lists visits, newest first; limit ≤ 0 lists all.
func (db *DB) History(limit, offset int) ([]Visit, error) {
	if limit <= 0 {
		limit = -1
	}
	return db.visits(`SELECT target,title,kind,catalog,first,last,opens,pos FROM history
	  ORDER BY last DESC, rowid DESC LIMIT ? OFFSET ?`, limit, offset)
}

// Unfinished lists pages left part-way (books are tracked by the library).
func (db *DB) Unfinished(limit int) ([]Visit, error) {
	return db.visits(`SELECT target,title,kind,catalog,first,last,opens,pos FROM history
	  WHERE pos > 0.05 AND pos < 0.95 AND kind <> 'book' ORDER BY last DESC, rowid DESC LIMIT ?`, limit)
}

func (db *DB) visits(q string, args ...any) ([]Visit, error) {
	rows, err := db.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Visit
	for rows.Next() {
		var v Visit
		var first, last int64
		if err := rows.Scan(&v.Target, &v.Title, &v.Kind, &v.Catalog, &first, &last, &v.Opens, &v.Pos); err != nil {
			return nil, err
		}
		v.First, v.Last = time.Unix(0, first), time.Unix(0, last)
		out = append(out, v)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run** `gofmt -w internal/store && go vet ./internal/store && go test ./internal/store` → PASS

---

### Task 3: Index core — text, kinds, search, snippets, results page

**Files:**
- Create: `internal/index/index.go` (Text, Kind, Page, Feeds, put, capText), `internal/index/search.go` (Match, Search, Snippet, Hit), `internal/index/docs.go` (IsTarget, Route)
- Test: `internal/index/index_test.go`

**Interfaces:**
- Consumes: `store.IndexDoc`, `store.PutDoc`, `store.FindDocs`, `store.ItemsToIndex` (Task 2); `catalog.Number/Feed/Label`, `index.Fold` (Task 1)
- Produces:
  - `const MaxText = 200 << 10`
  - `func Text(d *doc.Document) string`
  - `func Kind(target string, d *doc.Document) (kind, key string)`
  - `func Page(db *store.DB, target string, d *doc.Document) error`
  - `func Feeds(db *store.DB) (int, error)`
  - `func put(db *store.DB, key, kind, title, cat, text string) error`
  - `type Hit struct { Target, Kind, Title, Catalog string; Snippet doc.Inline }`
  - `func Match(q string) (match string, terms []string)`
  - `func Search(db *store.DB, q, kind string, limit, offset int) ([]Hit, error)`
  - `func Snippet(text string, terms []string) doc.Inline`
  - `func IsTarget(target string) bool`, `func Route(target string, db *store.DB) (*doc.Document, error)`

- [ ] **Step 1: Failing tests** (`internal/index/index_test.go`)

```go
package index

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func tmpDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func textDoc(title string, paras ...string) *doc.Document {
	d := &doc.Document{Title: title}
	for _, p := range paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	return d
}

func TestKind(t *testing.T) {
	cases := []struct{ target, ref, kind, key string }{
		{"https://scp-wiki.wikidot.com/scp-173", "", "scp", "https://scp-wiki.wikidot.com/scp-173"},
		{"https://wanderers-library.wikidot.com/x", "", "wiki", "https://wanderers-library.wikidot.com/x"},
		{"https://old.reddit.com/r/nosleep/", "", "reddit", "https://old.reddit.com/r/nosleep/"},
		{"https://arkeofili.com/x", "item:12", "feed", "w5f:item/12"},
		{"w5f:book/3/ch/4", "book:3:4", "book", "w5f:book/3/ch/4"},
		{"https://example.org/a", "", "web", "https://example.org/a"},
		{"w5f:feeds", "", "", ""},
		{"C:/notes/a.md", "", "", ""},
	}
	for _, c := range cases {
		k, key := Kind(c.target, &doc.Document{Ref: c.ref})
		if k != c.kind || key != c.key {
			t.Errorf("Kind(%q, %q) = %q %q", c.target, c.ref, k, key)
		}
	}
}

func TestTextCoversBlocksAndIsCapped(t *testing.T) {
	d := &doc.Document{Blocks: []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Head"}}},
		doc.List{Items: [][]doc.Block{{doc.Paragraph{Text: doc.Inline{{Text: "item one"}}}}}},
		doc.Quote{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "quoted"}}}}},
		doc.Collapsible{ID: 1, Show: "more", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "folded away"}}}}},
		doc.Table{Rows: [][]doc.Inline{{{{Text: "a"}}, {{Text: "b"}}}}},
		doc.Pre{Text: "code line"},
	}}
	got := Text(d)
	for _, want := range []string{"Head", "item one", "quoted", "folded away", "a · b", "code line"} {
		if !strings.Contains(got, want) {
			t.Errorf("Text lacks %q: %q", want, got)
		}
	}
	big := textDoc("big", strings.Repeat("şüphe ", 1_000_000)) // ~7 MB
	if n := len(Text(big)); n > MaxText || n < MaxText-8 {
		t.Errorf("text not capped at %d: %d", MaxText, n)
	}
}

func TestSearchAcrossKindsTurkishAndSnippets(t *testing.T) {
	db := tmpDB(t)
	Page(db, "https://scp-wiki.wikidot.com/scp-173", textDoc("SCP-173", "The statue moves when nobody looks."))
	Page(db, "https://arkeofili.com/a", &doc.Document{Title: "Göbekli Tepe", Ref: "item:5",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Kafatası kültü ve bir statue parçası bulundu."}}}}})
	Page(db, "w5f:book/2/ch/1", &doc.Document{Title: "Dracula · Chapter 2", Ref: "book:2:1",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "He stood like a statue."}}}}})
	hits, err := Search(db, "statue", "", 30, 0)
	if err != nil || len(hits) != 3 {
		t.Fatalf("search: %v %+v", err, hits)
	}
	hits, _ = Search(db, "kafatasi", "", 30, 0)
	if len(hits) != 1 || hits[0].Kind != "feed" || hits[0].Target != "w5f:item/5" {
		t.Fatalf("Turkish folding: %+v", hits)
	}
	var sn strings.Builder
	bold := ""
	for _, s := range hits[0].Snippet {
		sn.WriteString(s.Text)
		if s.Style&doc.Bold != 0 {
			bold += s.Text
		}
	}
	if !strings.Contains(sn.String(), "Kafatası") || bold != "Kafatası" {
		t.Errorf("snippet %q bold %q", sn.String(), bold)
	}
	if hits, _ := Search(db, "statue", "book", 30, 0); len(hits) != 1 || hits[0].Kind != "book" {
		t.Errorf("kind filter: %+v", hits)
	}
	if hits, _ := Search(db, `"moves when"`, "", 30, 0); len(hits) != 1 {
		t.Errorf("phrase: %+v", hits)
	}
	if hits, _ := Search(db, "stat", "", 30, 0); len(hits) != 3 {
		t.Errorf("prefix: %+v", hits)
	}
}

func TestSearchInputNeverBreaksTheQuery(t *testing.T) {
	db := tmpDB(t)
	Page(db, "https://example.org/a", textDoc("A", "near the end and more"))
	for _, q := range []string{`"`, `near"the`, `*`, `-`, `AND`, `NEAR(`, `title:x`, `"unbalanced`, `^ ~ ( )`, `...`} {
		if _, err := Search(db, q, "", 30, 0); err != nil {
			t.Errorf("Search(%q): %v", q, err)
		}
	}
	if hits, _ := Search(db, "AND near", "", 30, 0); len(hits) != 1 {
		t.Errorf("operator words are plain words: %+v", hits)
	}
}

func TestFeedsIndexesNewItems(t *testing.T) {
	db := tmpDB(t)
	db.UpsertItem(store.Item{FeedID: "arkeofili", GUID: "g1", Title: "Göbekli Tepe", Content: "<p>Taş <b>heykel</b></p>", Published: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)})
	n, err := Feeds(db)
	if err != nil || n != 1 {
		t.Fatalf("Feeds: %v %d", err, n)
	}
	if n, _ := Feeds(db); n != 0 {
		t.Error("items indexed twice")
	}
	hits, _ := Search(db, "heykel", "", 30, 0)
	if len(hits) != 1 || hits[0].Catalog != "PER·ARKEOF·2026-09-24" {
		t.Errorf("feed hit: %+v", hits)
	}
}

func TestResultsPage(t *testing.T) {
	db := tmpDB(t)
	for i := 0; i < 31; i++ {
		Page(db, "https://example.org/p"+string(rune('a'+i%26))+string(rune('a'+i/26)), textDoc("Page", "statue"))
	}
	d, err := Route("w5f:find?q=statue", db)
	if err != nil {
		t.Fatal(err)
	}
	var hrefs []string
	for _, l := range d.Links {
		hrefs = append(hrefs, l.Href)
	}
	all := strings.Join(hrefs, " ")
	if !strings.Contains(all, "kind=feed") || !strings.Contains(all, "page=2") || !strings.Contains(all, "https://example.org/p") {
		t.Errorf("links: %s", all)
	}
	if d, _ := Route("w5f:find?q=nothingatall", db); !strings.Contains(d.Blocks[len(d.Blocks)-1].(doc.Paragraph).Text.PlainText(), "Nothing found") {
		t.Error("empty result message")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/index` → FAIL (undefined `Kind`, `Text`, `Search`)

- [ ] **Step 3: Implement** `internal/index/index.go`:

```go
package index

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/htmlconv"
	"w5f/internal/store"
)

// MaxText caps the text indexed for one document.
const MaxText = 200 << 10

// Text returns a document's readable text, one block per line: paragraphs,
// headings, list items, quotes, folded sections, tables, code, footnotes.
func Text(d *doc.Document) string {
	var b strings.Builder
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" && b.Len() < MaxText {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			add(x.Text.PlainText())
		case doc.Heading:
			add(x.Text.PlainText())
		case doc.Pre:
			add(x.Text)
		case doc.Table:
			for _, r := range x.Rows {
				var cells []string
				for _, c := range r {
					cells = append(cells, strings.TrimSpace(c.PlainText()))
				}
				add(strings.Join(cells, " · "))
			}
		case doc.Footnotes:
			for _, n := range x.Notes {
				add(n.Text.PlainText())
			}
		}
		return nil, false
	})
	return capText(b.String())
}

// capText cuts text to MaxText bytes on a rune boundary.
func capText(s string) string {
	if len(s) <= MaxText {
		return s
	}
	s = s[:MaxText]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Kind classifies a loaded page and gives the address the index and the
// history use for it. An empty kind means "not recorded" (menus, lists,
// local files).
func Kind(target string, d *doc.Document) (kind, key string) {
	if d != nil {
		switch {
		case strings.HasPrefix(d.Ref, "item:"):
			return "feed", "w5f:item/" + strings.TrimPrefix(d.Ref, "item:")
		case strings.HasPrefix(d.Ref, "book:"):
			parts := strings.SplitN(strings.TrimPrefix(d.Ref, "book:"), ":", 2)
			if len(parts) == 2 {
				return "book", "w5f:book/" + parts[0] + "/ch/" + parts[1]
			}
			return "", ""
		}
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case strings.HasPrefix(host, "scp-") && strings.HasSuffix(host, ".wikidot.com"):
		return "scp", target
	case strings.HasSuffix(host, ".wikidot.com"):
		return "wiki", target
	case host == "reddit.com" || strings.HasSuffix(host, ".reddit.com"):
		return "reddit", target
	}
	return "web", target
}

// Page indexes a document the reader has opened (content pages only).
func Page(db *store.DB, target string, d *doc.Document) error {
	kind, key := Kind(target, d)
	if kind == "" {
		return nil
	}
	return put(db, key, kind, d.Title, catalog.Number(target, d), Text(d))
}

func put(db *store.DB, key, kind, title, cat, text string) error {
	return db.PutDoc(store.IndexDoc{Target: key, Kind: kind, Title: title, Catalog: cat, Text: text,
		Updated: time.Now(), FoldTitle: Fold(title), FoldText: Fold(text)})
}

// Feeds indexes the feed items that are not in the index yet.
func Feeds(db *store.DB) (int, error) {
	n := 0
	for {
		its, err := db.ItemsToIndex(200)
		if err != nil || len(its) == 0 {
			return n, err
		}
		for _, it := range its {
			body := it.Content
			if strings.TrimSpace(body) == "" {
				body = it.Summary
			}
			text := capText(it.Author + "\n" + htmlconv.FragmentText(body))
			if err := put(db, fmt.Sprintf("w5f:item/%d", it.ID), "feed", it.Title, catalog.Feed(it.FeedID, it.Published), text); err != nil {
				return n, err
			}
			n++
		}
	}
}
```

`internal/index/search.go`:

```go
package index

import (
	"regexp"
	"strings"
	"unicode"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// Hit is one search result.
type Hit struct {
	Target, Kind, Title, Catalog string
	Snippet                      doc.Inline
}

var (
	rePhrase = regexp.MustCompile(`"([^"]*)"`)
	// kindGroups are the result filters.
	kindGroups = map[string][]string{"feed": {"feed"}, "scp": {"scp", "wiki"}, "book": {"book"},
		"web": {"web", "reddit"}, "notes": {"note", "clip", "saved"}}
)

// Match builds an FTS5 query from what the owner typed: every word must
// match (as a prefix when it has 3+ letters) and "quoted text" is a phrase.
// FTS syntax in the input is always taken literally. terms are the folded
// words and phrases, for snippets.
func Match(q string) (match string, terms []string) {
	var parts []string
	add := func(t string, prefix bool) {
		t = strings.Join(strings.FieldsFunc(Fold(t), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ")
		if t == "" {
			return
		}
		terms = append(terms, t)
		p := `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
		if prefix && !strings.Contains(t, " ") && len([]rune(t)) >= 3 {
			p += "*"
		}
		parts = append(parts, p)
	}
	for _, m := range rePhrase.FindAllStringSubmatch(q, -1) {
		add(m[1], false)
	}
	for _, w := range strings.Fields(strings.ReplaceAll(rePhrase.ReplaceAllString(q, " "), `"`, " ")) {
		add(w, true)
	}
	return strings.Join(parts, " "), terms
}

// Search finds documents; kind is "" or one of the result filters.
func Search(db *store.DB, q, kind string, limit, offset int) ([]Hit, error) {
	match, terms := Match(q)
	if match == "" {
		return nil, nil
	}
	ds, err := db.FindDocs(match, kindGroups[kind], limit, offset)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(ds))
	for _, d := range ds {
		hits = append(hits, Hit{Target: d.Target, Kind: d.Kind, Title: d.Title, Catalog: d.Catalog, Snippet: Snippet(d.Text, terms)})
	}
	return hits, nil
}

// Snippet cuts about 160 characters of the original text around the first
// match and marks the matched words bold.
func Snippet(text string, terms []string) doc.Inline {
	rt := []rune(strings.Join(strings.Fields(text), " "))
	rf := []rune(Fold(string(rt))) // same length: Fold keeps one rune per rune
	at := -1
	for i := range rf {
		if wordStart(rf, i) && matchAt(rf, i, terms) > 0 {
			at = i
			break
		}
	}
	start := 0
	if at > 60 {
		start = at - 60
	}
	end := min(len(rt), start+160)
	var out doc.Inline
	if start > 0 {
		out = append(out, doc.Span{Text: "…"})
	}
	plain := start
	for i := start; i < end; {
		if n := matchAt(rf, i, terms); n > 0 && wordStart(rf, i) {
			if i > plain {
				out = append(out, doc.Span{Text: string(rt[plain:i])})
			}
			e := min(i+n, end)
			out = append(out, doc.Span{Text: string(rt[i:e]), Style: doc.Bold})
			i, plain = e, e
			continue
		}
		i++
	}
	if plain < end {
		out = append(out, doc.Span{Text: string(rt[plain:end])})
	}
	if end < len(rt) {
		out = append(out, doc.Span{Text: "…"})
	}
	return out
}

func wordStart(rf []rune, i int) bool {
	return i == 0 || !(unicode.IsLetter(rf[i-1]) || unicode.IsDigit(rf[i-1]))
}

func matchAt(rf []rune, i int, terms []string) int {
	for _, t := range terms {
		tr := []rune(t)
		if i+len(tr) <= len(rf) && string(rf[i:i+len(tr)]) == t {
			return len(tr)
		}
	}
	return 0
}
```

(With prefix matching, `stat` also finds `statue`; the snippet bolds `stat` inside it — acceptable. Snippet matching of prefixes marks the typed part only.)

`internal/index/docs.go`:

```go
package index

import (
	"net/url"
	"strconv"
	"strings"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/store"
)

const perPage = 30

var filters = []struct{ kind, label string }{
	{"", "all"}, {"feed", "RSS"}, {"scp", "SCP & wikis"}, {"book", "books"}, {"web", "web"}, {"notes", "notes"},
}

// IsTarget reports whether target is a search of the owner's archive.
func IsTarget(target string) bool { return strings.HasPrefix(target, "w5f:find") }

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func findHref(q, kind string, page int) string {
	v := url.Values{"q": {q}}
	if kind != "" {
		v.Set("kind", kind)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	return "w5f:find?" + v.Encode()
}

// Route shows search results (w5f:find?q=…&kind=…&page=…).
func Route(target string, db *store.DB) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	v := u.Query()
	q, kind := strings.TrimSpace(v.Get("q")), v.Get("kind")
	page, _ := strconv.Atoi(v.Get("page"))
	if page < 1 {
		page = 1
	}
	d := &doc.Document{Title: "Search: " + q, URL: target, Origin: "local", Lang: "en"}
	if q == "" {
		d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Press / and type some words to search everything you have read.", Style: doc.Italic}}}}
		return d, nil
	}
	hits, err := Search(db, q, kind, perPage+1, (page-1)*perPage)
	if err != nil {
		return nil, err
	}
	var bar doc.Inline
	for i, f := range filters {
		if i > 0 {
			bar = append(bar, doc.Span{Text: " · "})
		}
		if f.kind == kind {
			bar = append(bar, doc.Span{Text: f.label, Style: doc.Bold})
			continue
		}
		bar = append(bar, doc.Span{Text: f.label, Link: link(d, findHref(q, f.kind, 1), f.label)})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: bar})
	more := len(hits) > perPage
	if more {
		hits = hits[:perPage]
	}
	if len(hits) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing found in what you have read. All words must appear; try fewer or different words.", Style: doc.Italic}}})
		return d, nil
	}
	var items [][]doc.Block
	for _, h := range hits {
		head := doc.Inline{{Text: catalog.Label(h.Kind) + " ", Style: doc.Bold}, {Text: h.Title, Link: link(d, h.Target, h.Title)}}
		if h.Catalog != "" {
			head = append(head, doc.Span{Text: "  " + h.Catalog, Style: doc.Italic})
		}
		item := []doc.Block{doc.Paragraph{Text: head}}
		if len(h.Snippet) > 0 {
			item = append(item, doc.Paragraph{Text: h.Snippet})
		}
		items = append(items, item)
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	if more {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ more results", Link: link(d, findHref(q, kind, page+1), "more")}}})
	}
	return d, nil
}
```

- [ ] **Step 4: Run** `gofmt -w internal/index && go vet ./internal/index && go test ./internal/index` → PASS

---

### Task 4: Renderer — paragraph ranges

**Files:**
- Modify: `internal/render/render.go` (`Para` type, `Layout.Paras`, `block` records text blocks)
- Test: `internal/render/paras_test.go`

**Interfaces:**
- Produces: `type Para struct { Start, End int; Text string }` (End exclusive); `Layout.Paras []Para`

- [ ] **Step 1: Failing test** (`internal/render/paras_test.go`)

```go
package render

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestParasRecordTextBlocks(t *testing.T) {
	p := func(s string) doc.Paragraph { return doc.Paragraph{Text: doc.Inline{{Text: s}}} }
	d := &doc.Document{Title: "T", Blocks: []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Section"}}},
		p(strings.Repeat("long paragraph words ", 12)),
		doc.List{Items: [][]doc.Block{{p("first item")}, {p("second item")}}},
		doc.Quote{Blocks: []doc.Block{p("quoted text")}},
		doc.Collapsible{ID: 1, Show: "closed", Blocks: []doc.Block{p("hidden")}},
	}}
	l := Render(d, Options{Width: 40})
	var texts []string
	last := -1
	for _, pr := range l.Paras {
		texts = append(texts, pr.Text)
		if pr.Start <= last || pr.End <= pr.Start || pr.End > len(l.Lines) {
			t.Errorf("bad range %+v (last end %d)", pr, last)
		}
		last = pr.End - 1
		if !strings.Contains(strings.ToLower(l.Lines[pr.Start].Text()), strings.ToLower(strings.Fields(pr.Text)[0])) { // H2 is upper-cased
			t.Errorf("range %+v does not start at its text: %q", pr, l.Lines[pr.Start].Text())
		}
	}
	want := "Section|" + strings.TrimSpace(strings.Repeat("long paragraph words ", 12)) + "|first item|second item|quoted text"
	if strings.Join(texts, "|") != want {
		t.Errorf("paras = %q", strings.Join(texts, "|"))
	}
	if l.Paras[1].End-l.Paras[1].Start < 5 {
		t.Errorf("wrapped paragraph should span several lines: %+v", l.Paras[1])
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/render -run Paras` → FAIL (`l.Paras` undefined)

- [ ] **Step 3: Implement** — in `render.go` add after `HeadingRef`:

```go
// Para is a text block's place on screen, for clippings: lines
// [Start, End) and its plain text.
type Para struct {
	Start, End int
	Text       string
}
```

add `Paras []Para` to `Layout` (after `Headings`), and change the start of `block` to record ranges:

```go
func (r *renderer) block(b doc.Block, c ctx) {
	start := len(r.out.Lines)
	defer func() {
		if text := paraText(b); text != "" && len(r.out.Lines) > start {
			r.out.Paras = append(r.out.Paras, Para{Start: start, End: len(r.out.Lines), Text: text})
		}
	}()
	switch b := b.(type) {
```

(the rest of `block` is unchanged) and add:

```go
// paraText is the clippable text of a block; containers (lists, quotes,
// sections) have none of their own — their paragraphs are recorded.
func paraText(b doc.Block) string {
	switch b := b.(type) {
	case doc.Paragraph:
		return strings.TrimSpace(b.Text.PlainText())
	case doc.Heading:
		return strings.TrimSpace(b.Text.PlainText())
	case doc.Pre:
		return strings.TrimRight(b.Text, "\n")
	case doc.Table:
		var rows []string
		for _, r := range b.Rows {
			var cells []string
			for _, c := range r {
				cells = append(cells, strings.TrimSpace(c.PlainText()))
			}
			rows = append(rows, strings.Join(cells, " · "))
		}
		return strings.Join(rows, "\n")
	}
	return ""
}
```

- [ ] **Step 4: Run** `gofmt -w internal/render && go test ./internal/render` → PASS (golden tests unchanged)

---

### Task 5: Personal files — folder, names, notes, clippings, saved pages, Markdown

**Files:**
- Create: `internal/personal/paths.go`, `internal/personal/notes.go`, `internal/personal/markdown.go`
- Test: `internal/personal/notes_test.go`, `internal/personal/markdown_test.go`

**Interfaces:**
- Produces:
  - `func Dir() string`, `func Rel(p string) string`, `func FileURL(p string) string`, `func FileName(title string) string`
  - `type Source struct { Title, URL, Catalog, Kind string }`
  - `func SplitFront(data string) (front map[string]string, body string)`
  - `func AppendNote(s Source, text string, now time.Time) (string, error)`
  - `func AppendClipping(s Source, paras []string, now time.Time) (string, error)`
  - `func SavePage(s Source, d *doc.Document, now time.Time) (string, error)`
  - `func ToMarkdown(d *doc.Document) string`, `func ReadMarkdown(data []byte, fileURL string) *doc.Document`

- [ ] **Step 1: Failing tests**

`internal/personal/notes_test.go`:

```go
package personal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
)

func useDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("W5F_NOTES", d)
	return d
}

var now = time.Date(2026, 9, 28, 10, 21, 0, 0, time.Local)

func TestDirFromEnvConfigDefault(t *testing.T) {
	d := useDir(t)
	if Dir() != d {
		t.Errorf("env: %s", Dir())
	}
	t.Setenv("W5F_NOTES", "")
	data := t.TempDir()
	t.Setenv("APPDATA", data)
	t.Setenv("XDG_DATA_HOME", data)
	cfgDir := filepath.Join(data, "w5f")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(`notes = "/vault/W5F"`), 0o644)
	if Dir() != "/vault/W5F" {
		t.Errorf("config: %s", Dir())
	}
}

func TestFileNamesAreSafeAndDistinct(t *testing.T) {
	useDir(t)
	if got := FileName(`SCP-173: "The Sculpture" / <Ω> #1 [draft]`); got != "SCP-173 The Sculpture Ω 1 draft" {
		t.Errorf("FileName = %q", got)
	}
	if got := FileName(strings.Repeat("ç", 200)); len([]rune(got)) != 80 {
		t.Errorf("long title: %d runes", len([]rune(got)))
	}
	if FileName("...") != "Untitled" {
		t.Error("empty name")
	}
	a := Source{Title: "Dracula", URL: "https://a/dracula"}
	b := Source{Title: "Dracula", URL: "https://b/dracula"}
	pa, _ := AppendNote(a, "first", now)
	pb, _ := AppendNote(b, "other source", now)
	pa2, _ := AppendNote(a, "second", now.Add(time.Hour))
	if filepath.Base(pa) != "Dracula.md" || filepath.Base(pb) != "Dracula (2).md" || pa2 != pa {
		t.Errorf("paths %s %s %s", pa, pb, pa2)
	}
}

func TestNoteClippingSavedFiles(t *testing.T) {
	dir := useDir(t)
	src := Source{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173", Catalog: "FIC·SCP·173", Kind: "scp"}
	p, err := AppendNote(src, "Compare with SCP-096.", now)
	if err != nil {
		t.Fatal(err)
	}
	AppendNote(src, "Second thought.", now.Add(time.Minute))
	b, _ := os.ReadFile(p)
	note := string(b)
	for _, want := range []string{"title: \"SCP-173\"", "url: \"https://scp-wiki.wikidot.com/scp-173\"", "catalog: \"FIC·SCP·173\"",
		"tags: [w5f, w5f/note]", "# SCP-173", "## 2026-09-28 10:21\nCompare with SCP-096.", "## 2026-09-28 10:22\nSecond thought."} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if strings.Count(note, "---") != 2 {
		t.Errorf("frontmatter written twice:\n%s", note)
	}
	cp, err := AppendClipping(src, []string{"SCP-173 is to be kept in a locked container.", "Second paragraph."}, now)
	if err != nil || cp != filepath.Join(dir, "Clippings", "2026", "09", "2026-09-28.md") {
		t.Fatalf("clipping path %s %v", cp, err)
	}
	AppendClipping(Source{Title: "Other", URL: "https://x/y z"}, []string{"More."}, now.Add(time.Hour))
	c, _ := os.ReadFile(cp)
	clip := string(c)
	for _, want := range []string{"date: \"2026-09-28\"", "## 10:21 · [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173",
		"> SCP-173 is to be kept in a locked container.\n>\n> Second paragraph.", "## 11:21 · [Other](<https://x/y z>)\n> More."} {
		if !strings.Contains(clip, want) {
			t.Errorf("clipping lacks %q:\n%s", want, clip)
		}
	}
	sp, err := SavePage(src, &doc.Document{Title: "SCP-173", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Item #: SCP-173"}}}}}, now)
	s, _ := os.ReadFile(sp)
	if err != nil || filepath.Dir(sp) != filepath.Join(dir, "Saved") || !strings.Contains(string(s), "tags: [w5f, w5f/saved]") ||
		!strings.Contains(string(s), "Item #: SCP-173") {
		t.Errorf("saved page %s %v:\n%s", sp, err, s)
	}
	if Rel(cp) != "Clippings/2026/09/2026-09-28.md" {
		t.Errorf("Rel = %q", Rel(cp))
	}
	if !strings.HasPrefix(FileURL(cp), "file:///") {
		t.Errorf("FileURL = %q", FileURL(cp))
	}
	if _, err := AppendNote(src, "  \n ", now); err == nil {
		t.Error("an empty note must be refused")
	}
}
```

`internal/personal/markdown_test.go`:

```go
package personal

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestMarkdownRoundTrip(t *testing.T) {
	d := &doc.Document{Title: "Page", Links: []doc.Link{{Href: "https://example.org/a b", Text: "a link"}}}
	d.Blocks = []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Section"}}},
		doc.Paragraph{Text: doc.Inline{{Text: "Plain "}, {Text: "bold", Style: doc.Bold}, {Text: " and "}, {Text: "a link", Link: 1}, {Text: " with *stars*."}}},
		doc.List{Items: [][]doc.Block{{doc.Paragraph{Text: doc.Inline{{Text: "one"}}}}, {doc.Paragraph{Text: doc.Inline{{Text: "two"}}}}}},
		doc.Quote{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "quoted"}}}}},
		doc.Table{Header: true, Rows: [][]doc.Inline{{{{Text: "h1"}}, {{Text: "h2"}}}, {{{Text: "a|b"}}, {{Text: "c"}}}}},
		doc.Pre{Text: "code"},
		doc.Image{Src: "https://example.org/i.png", Alt: "pic"},
		doc.Collapsible{ID: 1, Show: "More", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "inside"}}}}},
	}
	md := ToMarkdown(d)
	for _, want := range []string{"# Page\n", "## Section\n", "Plain **bold** and [a link](<https://example.org/a b>) with \\*stars\\*.",
		"- one\n- two\n", "> quoted\n", "| h1 | h2 |\n| --- | --- |\n| a\\|b | c |", "```\ncode\n```", "![pic](https://example.org/i.png)", "**More**\n\ninside"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	back := ReadMarkdown([]byte("---\ntitle: \"My note\"\ntags: [w5f]\n---\n# My note\n\n## 2026-09-28 10:21\nSee [SCP-173](https://scp-wiki.wikidot.com/scp-173) now.\n\n> quoted\n>\n> more\n\n- [ ] [Queue item](<https://x/y z>) · FIC·SCP·1\n- [x] done one\n"), "file:///n.md")
	if back.Title != "My note" || back.Origin != "file" {
		t.Errorf("title/origin: %+v", back)
	}
	var kinds []string
	for _, b := range back.Blocks {
		kinds = append(kinds, strings.TrimPrefix(fmtType(b), "doc."))
	}
	if strings.Join(kinds, ",") != "Heading,Paragraph,Quote,List" {
		t.Errorf("blocks: %v", kinds)
	}
	if len(back.Links) != 2 || back.Links[0].Href != "https://scp-wiki.wikidot.com/scp-173" || back.Links[1].Href != "https://x/y z" {
		t.Errorf("links: %+v", back.Links)
	}
	q := back.Blocks[2].(doc.Quote)
	if len(q.Blocks) != 2 {
		t.Errorf("quote paragraphs: %+v", q)
	}
	l := back.Blocks[3].(doc.List)
	if l.Items[1][0].(doc.Paragraph).Text.PlainText() != "☑ done one" {
		t.Errorf("checked item: %q", l.Items[1][0].(doc.Paragraph).Text.PlainText())
	}
}

func fmtType(b doc.Block) string {
	switch b.(type) {
	case doc.Heading:
		return "doc.Heading"
	case doc.Paragraph:
		return "doc.Paragraph"
	case doc.Quote:
		return "doc.Quote"
	case doc.List:
		return "doc.List"
	}
	return "other"
}
```

- [ ] **Step 2: Run** `go test ./internal/personal` → FAIL (package missing)

- [ ] **Step 3: Implement** `internal/personal/paths.go`:

```go
// Package personal keeps the owner's own data — reading queue, notes,
// clippings and saved pages — as Markdown files Obsidian can open.
package personal

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"w5f/internal/store"
)

// Dir is the notes folder: W5F_NOTES, else `notes` in <DataDir>/config.toml,
// else ~/Archive/Notes.
func Dir() string {
	if d := os.Getenv("W5F_NOTES"); d != "" {
		return d
	}
	var cfg struct {
		Notes string `toml:"notes"`
	}
	if _, err := toml.DecodeFile(filepath.Join(store.DataDir(), "config.toml"), &cfg); err == nil && cfg.Notes != "" {
		return cfg.Notes
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Archive", "Notes")
	}
	return "Notes"
}

// Rel shows a path relative to the notes folder ("Notes/SCP-173.md").
func Rel(p string) string {
	if r, err := filepath.Rel(Dir(), p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// FileURL is a file:// address the reader opens.
func FileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(abs), "/")}).String()
}

// Source is what a note, clipping or saved page is about.
type Source struct {
	Title, URL, Catalog, Kind string
}

var reUnsafe = regexp.MustCompile(`[<>:"/\\|?*#^\[\]\x00-\x1f]+`)

// FileName makes a title safe as a file name on every OS and in Obsidian
// links; at most 80 characters.
func FileName(title string) string {
	name := strings.Join(strings.Fields(reUnsafe.ReplaceAllString(title, " ")), " ")
	name = strings.Trim(name, ". ")
	if r := []rune(name); len(r) > 80 {
		name = strings.TrimSpace(string(r[:80]))
	}
	if name == "" {
		name = "Untitled"
	}
	return name
}

// pathFor is the file for a source in folder: "<Title>.md", or
// "<Title> (2).md" … when that name belongs to another source.
func pathFor(folder string, s Source) string {
	base := FileName(s.Title)
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		p := filepath.Join(folder, name+".md")
		data, err := os.ReadFile(p)
		if err != nil {
			return p
		}
		if front, _ := SplitFront(string(data)); front["url"] == s.URL {
			return p
		}
	}
}

// SplitFront separates YAML frontmatter (simple "key: value" lines) from
// the body.
func SplitFront(data string) (front map[string]string, body string) {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	front = map[string]string{}
	if !strings.HasPrefix(data, "---\n") {
		return front, data
	}
	end := strings.Index(data[4:], "\n---")
	if end < 0 {
		return front, data
	}
	for _, ln := range strings.Split(data[4:4+end], "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) {
			if u, err := strconv.Unquote(v); err == nil {
				v = u
			}
		}
		front[strings.TrimSpace(k)] = v
	}
	rest := data[4+end+4:]
	return front, strings.TrimPrefix(rest, "\n")
}

type kv struct{ k, v string }

func frontmatter(kvs []kv, tags string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, x := range kvs {
		if x.v != "" {
			b.WriteString(x.k + ": " + strconv.Quote(x.v) + "\n")
		}
	}
	b.WriteString("tags: [" + tags + "]\n---\n")
	return b.String()
}

func appendFile(p, s string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
```

`internal/personal/notes.go`:

```go
package personal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"w5f/internal/doc"
)

// mdLink is a Markdown link; addresses with spaces or parentheses are
// wrapped in <…>.
func mdLink(title, u string) string {
	if u == "" {
		return mdEscape(title)
	}
	if strings.ContainsAny(u, " ()") {
		u = "<" + u + ">"
	}
	return "[" + mdEscape(title) + "](" + u + ")"
}

// AppendNote adds a dated note to the source's note file.
func AppendNote(s Source, text string, now time.Time) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", errors.New("the note is empty")
	}
	p := pathFor(filepath.Join(Dir(), "Notes"), s)
	var b strings.Builder
	if !exists(p) {
		b.WriteString(frontmatter([]kv{{"title", s.Title}, {"url", s.URL}, {"catalog", s.Catalog}, {"kind", s.Kind},
			{"created", now.Format("2006-01-02")}}, "w5f, w5f/note"))
		b.WriteString("# " + s.Title + "\n")
	}
	b.WriteString("\n## " + now.Format("2006-01-02 15:04") + "\n" + text + "\n")
	return p, appendFile(p, b.String())
}

// AppendClipping adds paragraphs, with their source, to the day's
// clippings file.
func AppendClipping(s Source, paras []string, now time.Time) (string, error) {
	if len(paras) == 0 {
		return "", errors.New("nothing selected")
	}
	p := filepath.Join(Dir(), "Clippings", now.Format("2006"), now.Format("01"), now.Format("2006-01-02")+".md")
	var b strings.Builder
	if !exists(p) {
		b.WriteString(frontmatter([]kv{{"date", now.Format("2006-01-02")}}, "w5f, w5f/clippings"))
	}
	b.WriteString("\n## " + now.Format("15:04") + " · " + mdLink(s.Title, s.URL))
	if s.Catalog != "" {
		b.WriteString(" · " + s.Catalog)
	}
	b.WriteString("\n")
	for i, para := range paras {
		if i > 0 {
			b.WriteString(">\n")
		}
		for _, ln := range strings.Split(strings.TrimSpace(para), "\n") {
			b.WriteString("> " + ln + "\n")
		}
	}
	return p, appendFile(p, b.String())
}

// SavePage writes a readable Markdown copy of a page to Saved/ (the latest
// save replaces the earlier one).
func SavePage(s Source, d *doc.Document, now time.Time) (string, error) {
	p := pathFor(filepath.Join(Dir(), "Saved"), s)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	content := frontmatter([]kv{{"title", s.Title}, {"url", s.URL}, {"catalog", s.Catalog}, {"kind", s.Kind},
		{"saved", now.Format("2006-01-02 15:04")}}, "w5f, w5f/saved") + ToMarkdown(d)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}
```

`internal/personal/markdown.go`:

```go
package personal

import (
	"regexp"
	"strconv"
	"strings"

	"w5f/internal/doc"
)

var mdEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "`", "\\`")

func mdEscape(s string) string { return mdEscaper.Replace(s) }

var reUnescape = regexp.MustCompile("\\\\([\\\\*_\\[\\]`|])")

func mdUnescape(s string) string { return reUnescape.ReplaceAllString(s, "$1") }

// ToMarkdown renders a document as Markdown; the title is the only level-1
// heading, so document headings start at level 2.
func ToMarkdown(d *doc.Document) string {
	var b strings.Builder
	if d.Title != "" {
		b.WriteString("# " + d.Title + "\n\n")
	}
	writeBlocks(&b, d, d.Blocks, "")
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func blankLine(b *strings.Builder, prefix string) { b.WriteString(strings.TrimRight(prefix, " ") + "\n") }

func writeBlocks(b *strings.Builder, d *doc.Document, bs []doc.Block, prefix string) {
	for _, x := range bs {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(prefix + inlineMD(d, x.Text) + "\n")
		case doc.Heading:
			b.WriteString(prefix + strings.Repeat("#", min(max(x.Level, 2), 6)) + " " + mdEscape(strings.TrimSpace(x.Text.PlainText())) + "\n")
		case doc.Quote:
			writeBlocks(b, d, x.Blocks, prefix+"> ")
		case doc.List:
			for i, it := range x.Items {
				marker := "- "
				if x.Ordered {
					marker = strconv.Itoa(i+1) + ". "
				}
				var sub strings.Builder
				writeBlocks(&sub, d, it, "")
				first := true
				for _, l := range strings.Split(strings.TrimRight(sub.String(), "\n"), "\n") {
					if strings.TrimSpace(l) == "" {
						continue
					}
					if first {
						b.WriteString(prefix + marker + l + "\n")
						first = false
					} else {
						b.WriteString(prefix + strings.Repeat(" ", len(marker)) + l + "\n")
					}
				}
			}
		case doc.Table:
			for i, r := range x.Rows {
				var cells []string
				for _, c := range r {
					cells = append(cells, strings.ReplaceAll(inlineMD(d, c), "|", `\|`))
				}
				b.WriteString(prefix + "| " + strings.Join(cells, " | ") + " |\n")
				if i == 0 {
					b.WriteString(prefix + "|" + strings.Repeat(" --- |", len(r)) + "\n")
				}
			}
		case doc.Collapsible:
			b.WriteString(prefix + "**" + mdEscape(x.Show) + "**\n")
			blankLine(b, prefix)
			writeBlocks(b, d, x.Blocks, prefix)
			continue
		case doc.Image:
			b.WriteString(prefix + "![" + mdEscape(x.Alt) + "](" + x.Src + ")\n")
		case doc.Rule:
			b.WriteString(prefix + "---\n")
		case doc.Pre:
			b.WriteString(prefix + "```\n")
			for _, l := range strings.Split(x.Text, "\n") {
				b.WriteString(prefix + l + "\n")
			}
			b.WriteString(prefix + "```\n")
		case doc.Notice:
			b.WriteString(prefix + "> " + mdEscape(x.Text) + "\n")
		case doc.Footnotes:
			for _, n := range x.Notes {
				b.WriteString(prefix + "[" + n.Label + "]: " + inlineMD(d, n.Text) + "\n")
			}
		default:
			continue
		}
		blankLine(b, prefix)
	}
}

func inlineMD(d *doc.Document, in doc.Inline) string {
	var b strings.Builder
	for _, s := range in {
		if s.Break {
			b.WriteString("  \n")
			continue
		}
		t := mdEscape(s.Text)
		switch {
		case s.Style&doc.Code != 0:
			t = "`" + s.Text + "`"
		case s.Style&doc.Bold != 0:
			t = "**" + t + "**"
		case s.Style&doc.Italic != 0:
			t = "*" + t + "*"
		case s.Style&doc.Strike != 0:
			t = "~~" + t + "~~"
		}
		if s.Link > 0 && s.Link <= len(d.Links) {
			t = mdLink(s.Text, d.Links[s.Link-1].Href)
		}
		b.WriteString(t)
	}
	return b.String()
}

var (
	reMDLink   = regexp.MustCompile(`\[((?:\\.|[^\]\\])*)\]\((<[^>]*>|[^)\s]+)\)`)
	reListItem = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+(?:\[([ xX])\]\s+)?(.*)$`)
)

// ReadMarkdown shows a Markdown file in the reader: frontmatter hidden,
// headings, quotes, lists and links kept.
func ReadMarkdown(data []byte, fileURL string) *doc.Document {
	front, body := SplitFront(string(data))
	d := &doc.Document{URL: fileURL, Title: front["title"], Origin: "file"}
	var para, quote []string
	var list [][]doc.Block
	flushPara := func() {
		if len(para) > 0 {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: readInline(d, strings.Join(para, " "))})
			para = nil
		}
	}
	flushQuote := func() {
		if len(quote) == 0 {
			return
		}
		var qb []doc.Block
		var cur []string
		for _, l := range append(quote, "") {
			if l == "" {
				if len(cur) > 0 {
					qb = append(qb, doc.Paragraph{Text: readInline(d, strings.Join(cur, " "))})
					cur = nil
				}
				continue
			}
			cur = append(cur, l)
		}
		d.Blocks = append(d.Blocks, doc.Quote{Blocks: qb})
		quote = nil
	}
	flushList := func() {
		if len(list) > 0 {
			d.Blocks = append(d.Blocks, doc.List{Items: list})
			list = nil
		}
	}
	flushAll := func() { flushPara(); flushQuote(); flushList() }
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimRight(ln, " \t")
		switch {
		case strings.TrimSpace(t) == "":
			flushAll()
		case strings.HasPrefix(t, "#"):
			flushAll()
			level := len(t) - len(strings.TrimLeft(t, "#"))
			text := strings.TrimSpace(t[level:])
			if level == 1 && (d.Title == "" || d.Title == mdUnescape(text)) {
				d.Title = mdUnescape(text)
				continue
			}
			d.Blocks = append(d.Blocks, doc.Heading{Level: min(level, 3), Text: readInline(d, text)})
		case strings.HasPrefix(t, ">"):
			flushPara()
			flushList()
			quote = append(quote, strings.TrimSpace(strings.TrimPrefix(t, ">")))
		case reListItem.MatchString(t):
			flushPara()
			flushQuote()
			m := reListItem.FindStringSubmatch(t)
			text := m[2]
			switch m[1] {
			case "x", "X":
				text = "☑ " + text
			case " ":
				text = "☐ " + text
			}
			list = append(list, []doc.Block{doc.Paragraph{Text: readInline(d, text)}})
		default:
			flushQuote()
			flushList()
			para = append(para, strings.TrimSpace(t))
		}
	}
	flushAll()
	return d
}

// readInline turns Markdown text into spans, keeping links.
func readInline(d *doc.Document, s string) doc.Inline {
	var out doc.Inline
	plain := func(t string) {
		if t = mdUnescape(strings.ReplaceAll(t, "**", "")); t != "" {
			out = append(out, doc.Span{Text: t})
		}
	}
	last := 0
	for _, m := range reMDLink.FindAllStringSubmatchIndex(s, -1) {
		plain(s[last:m[0]])
		text := mdUnescape(s[m[2]:m[3]])
		href := strings.TrimSuffix(strings.TrimPrefix(s[m[4]:m[5]], "<"), ">")
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		out = append(out, doc.Span{Text: text, Link: len(d.Links)})
		last = m[1]
	}
	plain(s[last:])
	return out
}
```

(In `TestMarkdownRoundTrip` the `☐`-prefixed first list item keeps its link, so `Links` has two entries: the paragraph link and the queue item link.)

- [ ] **Step 4: Run** `gofmt -w internal/personal && go vet ./internal/personal && go test ./internal/personal` → PASS

---

### Task 6: Reading queue (`Queue.md`)

**Files:**
- Create: `internal/personal/queue.go`
- Test: `internal/personal/queue_test.go`

**Interfaces:**
- Consumes: `Dir`, `mdLink`, `mdUnescape` (Task 5)
- Produces: `const ThisWeek, Someday`; `type Entry struct { Title, URL, Catalog string; Done bool; Section string }`; `func QueuePath() string`; `type Queue`; `func LoadQueue() (*Queue, error)`; `(q *Queue) Entries() []Entry`, `Add(e Entry, section string) bool`, `SetDone(url string, done bool) bool`, `Move(url, section string) bool`, `Remove(url string) bool`, `Save() error`

- [ ] **Step 1: Failing test** (`internal/personal/queue_test.go`)

```go
package personal

import (
	"os"
	"strings"
	"testing"
)

func TestQueueEditsKeepOwnersLines(t *testing.T) {
	useDir(t)
	// A queue edited in Obsidian on Windows: CRLF, own notes, an own
	// section, an entry without a catalog number, a URL with a space.
	own := "---\r\ntags: [w5f]\r\n---\r\n# Reading queue\r\nMy own intro line.\r\n\r\n## This week\r\n" +
		"- [ ] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173\r\n- [ ] [Plain](https://x.example/a)\r\n\r\n" +
		"## Ideas\r\nread more folklore\r\n\r\n## Someday\r\n- [x] [Spaced](<https://x.example/a b>) · WEB·X·a b\r\n"
	os.WriteFile(QueuePath(), []byte(own), 0o644)
	q, err := LoadQueue()
	if err != nil {
		t.Fatal(err)
	}
	es := q.Entries()
	if len(es) != 3 || es[1].Catalog != "" || es[2].URL != "https://x.example/a b" || !es[2].Done || es[2].Section != Someday {
		t.Fatalf("entries: %+v", es)
	}
	if !q.Add(Entry{Title: "Dracula [1897]", URL: "w5f:book/3", Catalog: "BK·GUT·345"}, ThisWeek) {
		t.Fatal("add refused")
	}
	if q.Add(Entry{Title: "again", URL: "w5f:book/3"}, Someday) {
		t.Error("a queued page must not be added twice")
	}
	q.Move("https://x.example/a", Someday)
	q.SetDone("https://scp-wiki.wikidot.com/scp-173", true)
	q.Remove("https://x.example/a b")
	if err := q.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(QueuePath())
	s := string(b)
	for _, want := range []string{"My own intro line.", "## Ideas\nread more folklore",
		"- [x] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173\n- [ ] [Dracula \\[1897\\]](w5f:book/3) · BK·GUT·345",
		"## Someday\n- [ ] [Plain](https://x.example/a)"} {
		if !strings.Contains(s, want) {
			t.Errorf("queue lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Spaced") || strings.Contains(s, "\r") {
		t.Errorf("removed entry or CRLF left:\n%s", s)
	}
	q, _ = LoadQueue()
	if es := q.Entries(); len(es) != 3 || es[1].Title != "Dracula [1897]" {
		t.Errorf("reloaded: %+v", es)
	}
}

func TestNewQueueFile(t *testing.T) {
	useDir(t)
	q, _ := LoadQueue()
	q.Add(Entry{Title: "A", URL: "https://a"}, Someday)
	q.Add(Entry{Title: "B", URL: "https://b"}, ThisWeek)
	q.Save()
	b, _ := os.ReadFile(QueuePath())
	if !strings.Contains(string(b), "## This week\n- [ ] [B](https://b)\n\n## Someday\n- [ ] [A](https://a)") {
		t.Errorf("new queue:\n%s", b)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/personal -run Queue` → FAIL (undefined `LoadQueue`)

- [ ] **Step 3: Implement** (`internal/personal/queue.go`)

```go
package personal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Queue sections.
const (
	ThisWeek = "This week"
	Someday  = "Someday"
)

// Entry is one line of the reading queue.
type Entry struct {
	Title, URL, Catalog string
	Done                bool
	Section             string
}

const newQueue = "---\ntags: [w5f]\n---\n# Reading queue\n\n## This week\n\n## Someday\n"

var reEntry = regexp.MustCompile(`^- \[( |x|X)\] \[((?:\\.|[^\]\\])*)\]\((<[^>]*>|[^)\s]+)\)(?: · (.*?))?\s*$`)

// Queue is Queue.md as lines; entries are recognised in place so the
// owner's own lines survive every change.
type Queue struct {
	path  string
	lines []string
}

// QueuePath is the queue file.
func QueuePath() string { return filepath.Join(Dir(), "Queue.md") }

// LoadQueue reads the queue (a new one when the file does not exist yet).
func LoadQueue() (*Queue, error) {
	p := QueuePath()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		data, err = []byte(newQueue), nil
	}
	if err != nil {
		return nil, err
	}
	s := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	return &Queue{path: p, lines: strings.Split(s, "\n")}, nil
}

func parseEntry(line string) (Entry, bool) {
	m := reEntry.FindStringSubmatch(strings.TrimRight(line, " "))
	if m == nil {
		return Entry{}, false
	}
	return Entry{Done: m[1] != " ", Title: mdUnescape(m[2]),
		URL: strings.TrimSuffix(strings.TrimPrefix(m[3], "<"), ">"), Catalog: m[4]}, true
}

func entryLine(e Entry) string {
	box := " "
	if e.Done {
		box = "x"
	}
	l := fmt.Sprintf("- [%s] %s", box, mdLink(e.Title, e.URL))
	if e.Catalog != "" {
		l += " · " + e.Catalog
	}
	return l
}

// Entries lists the queue's entries in file order with their section.
func (q *Queue) Entries() []Entry {
	var out []Entry
	section := ""
	for _, l := range q.lines {
		if strings.HasPrefix(l, "## ") {
			section = strings.TrimSpace(l[3:])
			continue
		}
		if e, ok := parseEntry(l); ok {
			e.Section = section
			out = append(out, e)
		}
	}
	return out
}

func (q *Queue) find(u string) int {
	for i, l := range q.lines {
		if e, ok := parseEntry(l); ok && e.URL == u {
			return i
		}
	}
	return -1
}

// Add puts a page at the end of a section; false when it is already queued
// and not done.
func (q *Queue) Add(e Entry, section string) bool {
	for _, x := range q.Entries() {
		if x.URL == e.URL && !x.Done {
			return false
		}
	}
	q.addLine(entryLine(e), section)
	return true
}

func (q *Queue) addLine(line, section string) {
	h := -1
	for i, l := range q.lines {
		if strings.TrimSpace(l) == "## "+section {
			h = i
			break
		}
	}
	if h < 0 {
		if n := len(q.lines); n > 0 && strings.TrimSpace(q.lines[n-1]) != "" {
			q.lines = append(q.lines, "")
		}
		q.lines = append(q.lines, "## "+section, line)
		return
	}
	end := h + 1
	for end < len(q.lines) && !strings.HasPrefix(q.lines[end], "## ") {
		end++
	}
	at := h + 1
	for i := h + 1; i < end; i++ {
		if strings.TrimSpace(q.lines[i]) != "" {
			at = i + 1
		}
	}
	q.lines = append(q.lines[:at], append([]string{line}, q.lines[at:]...)...)
	if at+1 < len(q.lines) && strings.HasPrefix(q.lines[at+1], "## ") {
		q.lines = append(q.lines[:at+1], append([]string{""}, q.lines[at+1:]...)...)
	}
}

// SetDone checks or unchecks an entry.
func (q *Queue) SetDone(u string, done bool) bool {
	i := q.find(u)
	if i < 0 {
		return false
	}
	e, _ := parseEntry(q.lines[i])
	e.Done = done
	q.lines[i] = entryLine(e)
	return true
}

// Move puts an entry at the end of another section.
func (q *Queue) Move(u, section string) bool {
	i := q.find(u)
	if i < 0 {
		return false
	}
	line := q.lines[i]
	q.lines = append(q.lines[:i], q.lines[i+1:]...)
	q.addLine(line, section)
	return true
}

// Remove deletes an entry.
func (q *Queue) Remove(u string) bool {
	i := q.find(u)
	if i < 0 {
		return false
	}
	q.lines = append(q.lines[:i], q.lines[i+1:]...)
	return true
}

// Save writes the queue back (atomically).
func (q *Queue) Save() error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(q.lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}
```

(Note on `addLine`: inserting directly before a following `## ` heading adds a blank line so sections stay separated — `TestNewQueueFile` pins it.)

- [ ] **Step 4: Run** `gofmt -w internal/personal && go test ./internal/personal` → PASS

---

### Task 7: Pages, notes/books indexing, rebuild, routes, commands

**Files:**
- Create: `internal/personal/docs.go` (queue, notes, history pages)
- Create: `internal/index/files.go` (NoteFile, Notes, Books, Rebuild, Progress)
- Modify: `internal/source/source.go` (routes, `Resolve` words, `.md` via `personal.ReadMarkdown`)
- Modify: `cmd/w5f/main.go` (`w5f reindex`; `w5f sync` indexes new items; usage)
- Test: `internal/personal/docs_test.go`, `internal/index/files_test.go`, `internal/source/personal_resolve_test.go`

**Interfaces:**
- Consumes: Tasks 2–6
- Produces:
  - `personal.IsTarget(target string) bool`, `personal.Route(target string, db *store.DB) (*doc.Document, error)`
  - `index.NoteFile(db *store.DB, dir, path string) error`, `index.Notes(db *store.DB, dir string) (int, error)`
  - `index.Books(db *store.DB, load func(path string) (*doc.Document, error)) (int, error)`, `var index.Progress atomic.Value`
  - `type index.Report struct { Pages, Feeds, Books, Notes, Skipped int }`, `index.Rebuild(ctx context.Context, db *store.DB, notesDir string, load func(target string) (*doc.Document, error)) (Report, error)`

- [ ] **Step 1: Failing tests**

`internal/personal/docs_test.go`:

```go
package personal

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString("# " + x.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("[N] " + x.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func TestQueuePageActions(t *testing.T) {
	useDir(t)
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	q, _ := LoadQueue()
	q.Add(Entry{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173", Catalog: "FIC·SCP·173"}, ThisWeek)
	q.Save()
	d, err := Route("w5f:queue", db)
	if err != nil || !strings.Contains(flat(d), "SCP-173  FIC·SCP·173") || !strings.Contains(flat(d), "done · → someday · remove") {
		t.Fatalf("queue page: %v\n%s", err, flat(d))
	}
	u := url.Values{"u": {"https://scp-wiki.wikidot.com/scp-173"}}
	d, _ = Route("w5f:queue/done?"+u.Encode(), db)
	if !strings.Contains(flat(d), "[N] marked done") || !strings.Contains(flat(d), "# Done") {
		t.Errorf("done:\n%s", flat(d))
	}
	d, _ = Route("w5f:queue/remove?"+u.Encode(), db)
	if strings.Contains(flat(d), "SCP-173  FIC") {
		t.Errorf("removed entry still listed:\n%s", flat(d))
	}
}

func TestNotesAndHistoryPages(t *testing.T) {
	useDir(t)
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	src := Source{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173"}
	AppendNote(src, "A note.", now)
	AppendClipping(src, []string{"A clip."}, now)
	d, err := Route("w5f:notes", db)
	if err != nil || !strings.Contains(flat(d), "Notes/SCP-173.md") || !strings.Contains(flat(d), "Clippings/2026/09/2026-09-28.md") {
		t.Errorf("notes page: %v\n%s", err, flat(d))
	}
	db.Visit("https://scp-wiki.wikidot.com/scp-173", "SCP-173", "scp", "FIC·SCP·173")
	db.SavePos("https://scp-wiki.wikidot.com/scp-173", 0.42)
	d, _ = Route("w5f:history", db)
	if !strings.Contains(flat(d), "SCP-173") || !strings.Contains(flat(d), "[SCP] · FIC·SCP·173") || !strings.Contains(flat(d), "42%") {
		t.Errorf("history page:\n%s", flat(d))
	}
}
```

`internal/index/files_test.go`:

```go
package index

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
)

func writeEPUB(t *testing.T, path string) {
	t.Helper()
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	add := func(name, body string) { w, _ := zw.Create(name); w.Write([]byte(body)) }
	add("META-INF/container.xml", `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	add("c.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Carmilla</dc:title><dc:creator>Le Fanu</dc:creator></metadata>
<manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="b.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/><itemref idref="b"/></spine></package>`)
	add("a.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><h2>Prologue</h2><p>A vampire story.</p></body></html>`)
	add("b.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><h2>Chapter I</h2><p>An early fright in Styria.</p></body></html>`)
	zw.Close()
	f.Close()
}

func TestBooksAndNotesIndexing(t *testing.T) {
	db := tmpDB(t)
	p := filepath.Join(t.TempDir(), "Le Fanu - Carmilla.epub")
	writeEPUB(t, p)
	id, _ := db.UpsertBook(store.Book{Path: p, Format: "epub", Title: "Carmilla", Author: "Le Fanu", Source: "gutenberg:10007", Chapters: 2})
	n, err := Books(db, nil)
	if err != nil || n != 1 {
		t.Fatalf("Books: %v %d", err, n)
	}
	hits, _ := Search(db, "styria", "", 30, 0)
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Catalog != "BK·GUT·10007" || hits[0].Target != "w5f:book/"+itoa(id)+"/ch/1" {
		t.Errorf("book hit: %+v", hits)
	}
	if n, _ := Books(db, nil); n != 0 {
		t.Error("book indexed twice")
	}
	dir := t.TempDir()
	t.Setenv("W5F_NOTES", dir)
	np, _ := personal.AppendNote(personal.Source{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173", Catalog: "FIC·SCP·173"}, "Compare with the weeping angels.", now())
	personal.AppendClipping(personal.Source{Title: "X", URL: "https://x"}, []string{"angels in the archive"}, now())
	os.WriteFile(personal.QueuePath(), []byte("# Reading queue\nangels queue\n"), 0o644)
	n, err = Notes(db, dir)
	if err != nil || n != 2 {
		t.Fatalf("Notes: %v %d", err, n)
	}
	hits, _ = Search(db, "angels", "notes", 30, 0)
	if len(hits) != 2 {
		t.Fatalf("notes hits: %+v", hits)
	}
	for _, h := range hits {
		if h.Kind == "note" && (h.Target != personal.FileURL(np) || h.Catalog != "FIC·SCP·173" || h.Title != "SCP-173") {
			t.Errorf("note hit: %+v", h)
		}
	}
}

// Deleting the database loses no personal data: a new database is rebuilt
// from history pages (via load), feeds, books and the notes folder.
func TestRebuildAfterDatabaseLoss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("W5F_NOTES", dir)
	personal.AppendNote(personal.Source{Title: "Note", URL: "https://n"}, "the lighthouse keeper", now())
	db := tmpDB(t) // "new" database after the old one was deleted
	db.Visit("https://example.org/a", "A", "web", "")
	db.Visit("https://example.org/gone", "Gone", "web", "")
	load := func(target string) (*doc.Document, error) {
		if target == "https://example.org/a" {
			return textDoc("A", "the lighthouse at dusk"), nil
		}
		return nil, errors.New("not in the cache")
	}
	r, err := Rebuild(context.Background(), db, dir, load)
	if err != nil || r.Pages != 1 || r.Skipped != 1 || r.Notes != 1 {
		t.Fatalf("rebuild: %v %+v", err, r)
	}
	if hits, _ := Search(db, "lighthouse", "", 30, 0); len(hits) != 2 {
		t.Errorf("after rebuild: %+v", hits)
	}
}
```

Add to `internal/index/index_test.go`:

```go
func now() time.Time { return time.Date(2026, 9, 28, 10, 21, 0, 0, time.Local) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```

(import `"strconv"` there).

`internal/source/personal_resolve_test.go`:

```go
package source

import "testing"

func TestResolvePersonalWords(t *testing.T) {
	cases := map[string]string{
		"queue":                "w5f:queue",
		"notes":                "w5f:notes",
		"history":              "w5f:history",
		"find kafatası kültü":  "w5f:find?q=kafatas%C4%B1+k%C3%BClt%C3%BC",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/personal ./internal/index ./internal/source` → FAIL (undefined `Route`, `Books`, `Rebuild`; Resolve cases)

- [ ] **Step 3: Implement** `internal/personal/docs.go`:

```go
package personal

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/store"
)

// IsTarget reports whether target is one of the personal pages.
func IsTarget(t string) bool {
	return t == "w5f:queue" || strings.HasPrefix(t, "w5f:queue/") || t == "w5f:notes" ||
		t == "w5f:history" || strings.HasPrefix(t, "w5f:history?")
}

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

// Route builds the queue, notes and history pages and applies queue
// actions (w5f:queue/done|undone|move|remove?u=…).
func Route(target string, db *store.DB) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p, q := u.Opaque, u.Query()
	switch {
	case p == "notes":
		return notesDoc()
	case p == "history":
		page, _ := strconv.Atoi(q.Get("page"))
		return historyDoc(db, max(page, 1))
	case p == "queue":
		return queueDoc("")
	case strings.HasPrefix(p, "queue/"):
		qq, err := LoadQueue()
		if err != nil {
			return nil, err
		}
		link := q.Get("u")
		var ok bool
		var note string
		switch strings.TrimPrefix(p, "queue/") {
		case "done":
			ok, note = qq.SetDone(link, true), "marked done"
		case "undone":
			ok, note = qq.SetDone(link, false), "back in the queue"
		case "move":
			ok, note = qq.Move(link, q.Get("to")), "moved to "+q.Get("to")
		case "remove":
			ok, note = qq.Remove(link), "removed"
		default:
			return nil, errors.New("unknown queue action: " + target)
		}
		if !ok {
			note = "that entry is no longer in the queue"
		} else if err := qq.Save(); err != nil {
			return nil, err
		}
		return queueDoc(note)
	}
	return nil, errors.New("unknown address: " + target)
}

func queueDoc(note string) (*doc.Document, error) {
	qq, err := LoadQueue()
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "Reading queue", URL: "w5f:queue", Origin: "local", Lang: "en"}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	es := qq.Entries()
	for _, sec := range []string{ThisWeek, Someday} {
		var items [][]doc.Block
		for _, e := range es {
			if e.Section == sec && !e.Done {
				items = append(items, entryBlocks(d, e))
			}
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: sec}}})
		if len(items) == 0 {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "empty", Style: doc.Italic}}})
		} else {
			d.Blocks = append(d.Blocks, doc.List{Items: items})
		}
	}
	var done [][]doc.Block
	for _, e := range es {
		if e.Done {
			done = append(done, entryBlocks(d, e))
		}
	}
	if len(done) > 0 {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Done"}}}, doc.List{Items: done})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{
		Text: "Press a on any page (A on a selected link) to add it here. The queue is " + QueuePath() + " — you can edit it in Obsidian too.", Style: doc.Italic}}})
	return d, nil
}

func entryBlocks(d *doc.Document, e Entry) []doc.Block {
	head := doc.Inline{{Text: e.Title, Link: link(d, e.URL, e.Title)}}
	if e.Catalog != "" {
		head = append(head, doc.Span{Text: "  " + e.Catalog, Style: doc.Italic})
	}
	var acts doc.Inline
	add := func(label, href string) {
		if len(acts) > 0 {
			acts = append(acts, doc.Span{Text: " · ", Style: doc.Italic})
		}
		acts = append(acts, doc.Span{Text: label, Style: doc.Italic, Link: link(d, href, label)})
	}
	v := url.Values{"u": {e.URL}}
	if e.Done {
		add("back to queue", "w5f:queue/undone?"+v.Encode())
	} else {
		add("done", "w5f:queue/done?"+v.Encode())
		other := Someday
		if e.Section == Someday {
			other = ThisWeek
		}
		add("→ "+strings.ToLower(other), "w5f:queue/move?"+url.Values{"u": {e.URL}, "to": {other}}.Encode())
	}
	add("remove", "w5f:queue/remove?"+v.Encode())
	return []doc.Block{doc.Paragraph{Text: head}, doc.Paragraph{Text: acts}}
}

type fileInfo struct {
	path string
	mod  time.Time
}

// recent lists Markdown files under dir, newest first.
func recent(dir string, n int) []fileInfo {
	var out []fileInfo
	filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		if info, err := de.Info(); err == nil {
			out = append(out, fileInfo{p, info.ModTime()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func notesDoc() (*doc.Document, error) {
	d := &doc.Document{Title: "Notes & clippings", URL: "w5f:notes", Origin: "local", Lang: "en"}
	for _, sec := range []struct{ dir, title, empty string }{
		{"Notes", "Notes", "No notes yet — press n on any page."},
		{"Clippings", "Clippings", "No clippings yet — press y on any page."},
		{"Saved", "Saved pages", "No saved pages yet — press s on any page."},
	} {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: sec.title}}})
		files := recent(filepath.Join(Dir(), sec.dir), 20)
		if len(files) == 0 {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: sec.empty, Style: doc.Italic}}})
			continue
		}
		var items [][]doc.Block
		for _, f := range files {
			rel := Rel(f.path)
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: rel, Link: link(d, FileURL(f.path), rel)},
				{Text: "  " + f.mod.Format("2006-01-02 15:04"), Style: doc.Italic}}}})
		}
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	if _, err := os.Stat(Dir()); err == nil {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "Folder: " + Dir(), Style: doc.Italic}}})
	}
	return d, nil
}

func historyDoc(db *store.DB, page int) (*doc.Document, error) {
	const per = 50
	vs, err := db.History(per+1, (page-1)*per)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "History", URL: "w5f:history", Origin: "local", Lang: "en"}
	if len(vs) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing read yet.", Style: doc.Italic}}})
		return d, nil
	}
	more := len(vs) > per
	if more {
		vs = vs[:per]
	}
	var items [][]doc.Block
	for _, v := range vs {
		sub := catalog.Label(v.Kind)
		if v.Catalog != "" {
			sub += " · " + v.Catalog
		}
		sub += " · " + v.Last.Format("2006-01-02 15:04")
		if v.Pos > 0 {
			sub += fmt.Sprintf(" · %d%%", int(v.Pos*100+0.5))
		}
		items = append(items, []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: v.Title, Link: link(d, v.Target, v.Title)}}},
			doc.Paragraph{Text: doc.Inline{{Text: sub, Style: doc.Italic}}},
		})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	if more {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ older", Link: link(d, fmt.Sprintf("w5f:history?page=%d", page+1), "older")}}})
	}
	return d, nil
}
```

`internal/index/files.go`:

```go
package index

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"w5f/internal/books"
	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
)

// Progress is a short status line while books are indexed in the background.
var Progress atomic.Value

var bookMu sync.Mutex

// Books indexes library books whose chapters are not indexed yet, one
// chapter per document. load opens non-EPUB readable books (text, HTML,
// Markdown); nil skips them. Only one indexer runs at a time.
func Books(db *store.DB, load func(path string) (*doc.Document, error)) (int, error) {
	if !bookMu.TryLock() {
		return 0, nil
	}
	defer bookMu.Unlock()
	defer Progress.Store("")
	bs, err := db.Books("", 0)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range bs {
		prefix := fmt.Sprintf("w5f:book/%d/ch/", b.ID)
		if db.CountDocs(prefix) > 0 {
			continue
		}
		cat := catalog.Book(b.Source, b.ID)
		switch strings.ToLower(filepath.Ext(b.Path)) {
		case ".epub":
			e, err := books.OpenEPUB(b.Path)
			if err != nil {
				continue
			}
			for i := range e.Chapters {
				Progress.Store(fmt.Sprintf("indexing %s · ch %d/%d", b.Title, i+1, len(e.Chapters)))
				cd, err := e.ChapterDoc(i, func(ch int) string { return fmt.Sprintf("%s%d", prefix, ch) })
				if err != nil {
					continue
				}
				title := b.Title
				if cd.Title != "" && cd.Title != b.Title {
					title += " · " + cd.Title
				}
				if err := put(db, fmt.Sprintf("%s%d", prefix, i), "book", title, cat, Text(cd)); err != nil {
					e.Close()
					return n, err
				}
			}
			e.Close()
			n++
		case ".txt", ".md", ".html", ".htm":
			if load == nil {
				continue
			}
			d, err := load(b.Path)
			if err != nil {
				continue
			}
			if err := put(db, prefix+"0", "book", b.Title, cat, Text(d)); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

var noteKinds = map[string]string{"Notes": "note", "Clippings": "clip", "Saved": "saved"}

// NoteFile indexes one file of the notes folder (notes, clippings, saved
// pages; the queue is not indexed).
func NoteFile(db *store.DB, dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return err
	}
	kind := noteKinds[strings.Split(filepath.ToSlash(rel), "/")[0]]
	if kind == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	front, body := personal.SplitFront(string(data))
	title := front["title"]
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if kind == "clip" {
			title = "Clippings " + title
		}
	}
	return put(db, personal.FileURL(path), kind, title, front["catalog"], capText(body))
}

// Notes indexes every Markdown file of the notes folder.
func Notes(db *store.DB, dir string) (int, error) {
	n := 0
	for sub := range noteKinds {
		err := filepath.WalkDir(filepath.Join(dir, sub), func(p string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
				return nil
			}
			if err := NoteFile(db, dir, p); err != nil {
				return err
			}
			n++
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Report counts what a rebuild indexed.
type Report struct{ Pages, Feeds, Books, Notes, Skipped int }

// Rebuild recreates the index: history pages (loaded by load, normally
// from the cache), feed items, library books and the notes folder.
func Rebuild(ctx context.Context, db *store.DB, notesDir string, load func(target string) (*doc.Document, error)) (Report, error) {
	var r Report
	if err := db.ClearIndex(); err != nil {
		return r, err
	}
	vs, err := db.History(0, 0)
	if err != nil {
		return r, err
	}
	for _, v := range vs {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if v.Kind == "book" || v.Kind == "feed" {
			continue // indexed from the library and the feed store below
		}
		d, err := load(v.Target)
		if err != nil {
			r.Skipped++
			continue
		}
		if err := Page(db, v.Target, d); err != nil {
			return r, err
		}
		r.Pages++
	}
	if r.Feeds, err = Feeds(db); err != nil {
		return r, err
	}
	if r.Books, err = Books(db, load); err != nil {
		return r, err
	}
	r.Notes, err = Notes(db, notesDir)
	return r, err
}
```

In `internal/source/source.go`:

In `Load`, after the `w5f:search/` block add:

```go
	if personal.IsTarget(target) || index.IsTarget(target) {
		db, err := store.Default()
		if err != nil {
			return nil, fmt.Errorf("opening the local database: %w", err)
		}
		if index.IsTarget(target) {
			return index.Route(target, db)
		}
		return personal.Route(target, db)
	}
```

In `Resolve`, after `case lower == "feeds" || lower == "periodicals":` add:

```go
	case lower == "queue":
		return "w5f:queue"
	case lower == "notes" || lower == "clippings":
		return "w5f:notes"
	case lower == "history":
		return "w5f:history"
	case strings.HasPrefix(lower, "find "):
		return "w5f:find?" + url.Values{"q": {strings.TrimSpace(s[5:])}}.Encode()
```

In `loadFile`, add a case before `default:`:

```go
	case ".md", ".markdown":
		return personal.ReadMarkdown(data, fileURL.String()), nil
```

Imports: `"w5f/internal/index"`, `"w5f/internal/personal"`.

In `cmd/w5f/main.go`: add usage lines

```
  w5f reindex                 rebuild the search index (pages from the cache, feeds, books, notes)
```

a case `case "reindex": os.Exit(reindex())`, in `syncFeeds` after printing the report:

```go
	if n, err := index.Feeds(env.DB); err == nil && n > 0 {
		fmt.Printf("%d new items indexed for search\n", n)
	}
```

and

```go
func reindex() int {
	source.Init(cacheDir(), true) // pages come from the cache only
	db, err := store.Default()
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	ctx := context.Background()
	load := func(t string) (*doc.Document, error) { return source.Load(ctx, t, source.Options{}) }
	fmt.Println("Rebuilding the search index…")
	r, err := index.Rebuild(ctx, db, personal.Dir(), load)
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	fmt.Printf("%d pages, %d feed items, %d books, %d notes indexed; %d pages are no longer in the cache\n",
		r.Pages, r.Feeds, r.Books, r.Notes, r.Skipped)
	return 0
}
```

(imports `"w5f/internal/doc"`, `"w5f/internal/index"`, `"w5f/internal/personal"`).

- [ ] **Step 4: Run** `gofmt -w internal cmd && go vet ./... && go test ./internal/personal ./internal/index ./internal/source` → PASS

---

### Task 8: TUI — recording, search prompt, queue/save keys, history, Continue

**Files:**
- Create: `internal/tui/main_test.go` (`TestMain`), `internal/tui/personal.go` (record, pageSource, queueAdd, indexFile, find prompt)
- Modify: `internal/tui/tui.go` (modes, keys, loadedMsg, leavePage, bottom bar, help), `internal/tui/welcome.go` (Continue reading, personal links)
- Test: `internal/tui/personal_test.go`

**Interfaces:**
- Consumes: `index.Kind/Page/Books/Feeds/NoteFile/Progress/Search`, `catalog.Number`, `personal.*`, `store.Visit/SavePos/Unfinished/Books`
- Produces: `modeFind`; `func record(target string, d *doc.Document) tea.Cmd`; `type recordedMsg struct{ err error }`; `func pageSource(p *page) personal.Source`; `func queueAdd(s personal.Source) string`; `func indexFile(path string) tea.Cmd`

- [ ] **Step 1: `TestMain`** (`internal/tui/main_test.go`)

```go
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
```

- [ ] **Step 2: Failing tests** (`internal/tui/personal_test.go`)

```go
package tui

import (
	"os"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/store"
)

func textPage(title string, paras ...string) *doc.Document {
	d := &doc.Document{Title: title}
	for _, p := range paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	return d
}

func open(m Model, target string, d *doc.Document) Model {
	next, cmd := m.Update(loadedMsg{target: target, doc: d})
	m = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ = m.Update(msg)
			m = next.(Model)
		}
	}
	return m
}

// The M4 criterion: pages of three kinds become searchable once read.
func TestOpenedPagesAreSearchable(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-173", textPage("SCP-173", "The statue moves when unobserved."))
	m = open(m, "https://arkeofili.com/x", &doc.Document{Title: "Göbekli Tepe", Ref: "item:77", URL: "https://arkeofili.com/x",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "A carved statue of a fox."}}}}})
	m = open(m, "w5f:book/5/ch/2", &doc.Document{Title: "Dracula · Chapter 3", Ref: "book:5:2",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "He stood like a statue in the moonlight."}}}}})
	db, _ := store.Default()
	hits, err := index.Search(db, "statue", "", 30, 0)
	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
	}
	if err != nil || !kinds["scp"] || !kinds["feed"] || !kinds["book"] {
		t.Errorf("hits %v %+v", err, hits)
	}
	vs, _ := db.History(0, 0)
	if len(vs) < 3 || vs[0].Target != "w5f:book/5/ch/2" {
		t.Errorf("history: %+v", vs)
	}
}

func TestSearchPromptQueueSaveHistoryKeys(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-173", textPage("SCP-173", strings.Repeat("Item text. ", 400)))
	m = press(m, "/", "s", "t", "a")
	if m.mode != modeFind || m.findBuf != "sta" {
		t.Fatalf("find prompt: mode %v buf %q", m.mode, m.findBuf)
	}
	m = press(m, "enter")
	if m.mode != modeRead || !strings.Contains(m.loading, "search") {
		t.Errorf("after enter: mode %v loading %q", m.mode, m.loading)
	}
	m.loading = ""
	m = press(m, "a")
	b, _ := os.ReadFile(personal.QueuePath())
	if !strings.Contains(string(b), "- [ ] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173") {
		t.Errorf("queue:\n%s", b)
	}
	if m = press(m, "a"); m.status != "already in the queue" {
		t.Errorf("status %q", m.status)
	}
	m = press(m, "s")
	if !strings.HasPrefix(m.status, "saved to Saved/SCP-173.md") {
		t.Errorf("save status %q", m.status)
	}
	m = press(m, "space", "space")
	m.leavePage()
	db, _ := store.Default()
	vs, _ := db.History(1, 0)
	if len(vs) != 1 || vs[0].Pos <= 0 {
		t.Errorf("position not saved: %+v", vs)
	}
	if m = press(m, "H"); m.loading == "" {
		t.Error("H should open the history")
	}
}

func TestQueueFocusedLink(t *testing.T) {
	m := sized(New("", "test"))
	d := textPage("List")
	d.Links = []doc.Link{{Href: "https://backrooms-wiki.wikidot.com/level-0", Text: "Level 0"}}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Level 0", Link: 1}}})
	m = open(m, "w5f:feeds", d)
	m = press(m, "A")
	b, _ := os.ReadFile(personal.QueuePath())
	if !strings.Contains(string(b), "[Level 0](https://backrooms-wiki.wikidot.com/level-0) · FIC·BR·level-0") {
		t.Errorf("queue:\n%s", b)
	}
}

func TestWelcomeContinueReading(t *testing.T) {
	db, _ := store.Default()
	db.Visit("https://example.org/half", "Half read", "web", "WEB·EXAMPLE·half")
	db.SavePos("https://example.org/half", 0.4)
	d := welcomeDoc("t")
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString(x.Text.PlainText() + "\n")
		}
		return nil, false
	})
	for _, want := range []string{"Continue reading", "Half read", "40%", "Reading queue", "Notes & clippings", "History"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("welcome lacks %q:\n%s", want, b.String())
		}
	}
}
```

- [ ] **Step 3: Run** `go test ./internal/tui` → FAIL (undefined `modeFind`, `findBuf`)

- [ ] **Step 4: Implement** `internal/tui/personal.go`:

```go
package tui

import (
	"context"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/source"
	"w5f/internal/store"
)

// recordedMsg reports the outcome of recording a page (history + index).
type recordedMsg struct{ err error }

// loadForIndex opens non-EPUB books for the background book indexer.
var loadForIndex = func(p string) (*doc.Document, error) {
	return source.Load(context.Background(), p, source.Options{})
}

// record adds an opened content page to the reading history and the search
// index, off the UI goroutine. The Library and Periodicals pages also start
// background indexing of new books and feed items.
func record(target string, d *doc.Document) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Default()
		if err != nil {
			return recordedMsg{err}
		}
		if kind, key := index.Kind(target, d); kind != "" {
			if err := db.Visit(key, d.Title, kind, catalog.Number(target, d)); err != nil {
				return recordedMsg{err}
			}
			if err := index.Page(db, target, d); err != nil {
				return recordedMsg{err}
			}
		}
		switch {
		case target == "w5f:books" || strings.HasPrefix(d.Ref, "book:"):
			go index.Books(db, loadForIndex)
		case strings.HasPrefix(target, "w5f:feeds"):
			go index.Feeds(db)
		}
		return recordedMsg{}
	}
}

// indexFile adds a just-written note, clipping or saved page to the index.
func indexFile(path string) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Default()
		if err != nil {
			return recordedMsg{err}
		}
		return recordedMsg{index.NoteFile(db, personal.Dir(), path)}
	}
}

// pageSource describes the current page for notes, clippings and the queue.
// Web addresses are kept (they open in Obsidian too); W5F's own records use
// their w5f: address.
func pageSource(p *page) personal.Source {
	kind, key := index.Kind(p.target, p.doc)
	u := p.doc.URL
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = key
		if u == "" {
			u = p.target
		}
	}
	title := strings.TrimSpace(p.doc.Title)
	if title == "" {
		title = u
	}
	if kind == "" {
		kind = "page"
	}
	return personal.Source{Title: title, URL: u, Catalog: catalog.Number(p.target, p.doc), Kind: kind}
}

// queueAdd adds a page to "This week" and returns the status line.
func queueAdd(s personal.Source) string {
	q, err := personal.LoadQueue()
	if err != nil {
		return "error: " + err.Error()
	}
	if !q.Add(personal.Entry{Title: s.Title, URL: s.URL, Catalog: s.Catalog}, personal.ThisWeek) {
		return "already in the queue"
	}
	if err := q.Save(); err != nil {
		return "error: " + err.Error()
	}
	return "added to the queue (This week) — g → queue"
}

// --- search prompt (/) ---

func (m Model) findKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeRead
	case "enter":
		m.mode = modeRead
		q := strings.TrimSpace(m.findBuf)
		if q == "" {
			return m, nil
		}
		m.loading = "searching your archive"
		return m, load("w5f:find?"+url.Values{"q": {q}}.Encode(), false)
	case "backspace":
		if r := []rune(m.findBuf); len(r) > 0 {
			m.findBuf = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.findBuf = ""
	default:
		if k.Text != "" {
			m.findBuf += k.Text
		}
	}
	return m, nil
}

// savePage writes the Markdown copy of the current page.
func (m *Model) savePage() tea.Cmd {
	path, err := personal.SavePage(pageSource(m.cur), m.cur.doc, time.Now())
	if err != nil {
		m.status = "error: " + err.Error()
		return nil
	}
	m.status = "saved to " + personal.Rel(path)
	return indexFile(path)
}
```

In `internal/tui/tui.go`:

1. `mode` constants — append `modeFind`.
2. `Model` — add `findBuf string` after `secretBuf`.
3. `loadedMsg` handling — replace the final `return m, nil` of the successful branch with `return m, record(target, np.doc)`.
4. `Update` — add cases:

```go
	case recordedMsg:
		if msg.err != nil && m.status == "" {
			m.status = "error: search index: " + msg.err.Error()
		}
		return m, nil
```

and in the `tea.PasteMsg` switch: `case modeFind: m.findBuf += strings.TrimSpace(msg.Content)`.
5. `key` — the esc-cancels-load condition gets `&& m.mode != modeFind`; the mode switch gets `case modeFind: return m.findKey(k)`; the reading-key switch gets:

```go
	case "/":
		m.mode, m.findBuf = modeFind, ""
		return m, nil
	case "a":
		m.status = queueAdd(pageSource(p))
		return m, nil
	case "A":
		f := m.focused()
		if f == nil || f.Kind != render.FocusLink {
			m.status = "select a link first (↑↓), then press A"
			return m, nil
		}
		l := p.doc.Links[f.Link-1]
		title := strings.TrimSpace(l.Text)
		if title == "" {
			title = l.Href
		}
		m.status = queueAdd(personal.Source{Title: title, URL: l.Href, Catalog: catalog.Number(l.Href, nil), Kind: "link"})
		return m, nil
	case "s":
		return m, m.savePage()
	case "H":
		m.loading = "history"
		return m, load("w5f:history", false)
```

6. `leavePage` — replace with:

```go
// leavePage remembers how far the current page was read (books keep their
// chapter progress; every content page its history position).
func (m *Model) leavePage() {
	if m.cur == nil || m.cur.layout == nil {
		return
	}
	frac := 0.0
	if n := len(m.cur.layout.Lines); n > 0 {
		frac = float64(m.cur.offset) / float64(n)
		if m.cur.offset+m.bodyHeight() >= n {
			frac = 0.999 // read to the end
		}
	}
	db, err := store.Default()
	if err != nil {
		return
	}
	if strings.HasPrefix(m.cur.doc.Ref, "book:") {
		books.SaveFromRef(db, m.cur.doc.Ref, frac)
	}
	if kind, key := index.Kind(m.cur.target, m.cur.doc); kind != "" {
		_ = db.SavePos(key, frac)
	}
}
```

7. `bottomBar` — add before `case m.status != "":`

```go
	case m.mode == modeFind:
		text = " search › " + m.findBuf + "_   (words · \"exact phrase\" · enter: search everything you read · esc: cancel)"
```

and change the default text to `" ↑↓ select · → open · ← back · / search · a queue · n note · y clip · g go · ? help · q quit"`. After the `switch`, when `text` is the default and `index.Progress` holds a non-empty string, show it: add before `case m.status != "":`

```go
	case m.status == "" && progressText() != "":
		text = " " + progressText()
```

with (in personal.go) `func progressText() string { s, _ := index.Progress.Load().(string); return s }`.

8. `helpLines` — add rows:

```go
		{"/", "search everything you have read (feeds, wikis, web pages, books, notes)"},
		{"a / A", "add this page / the selected link to the reading queue (g → queue)"},
		{"n", "write a note about this page"}, {"y", "clip paragraphs (↑↓ choose, shift+↑↓ extend, enter save)"},
		{"s", "save a Markdown copy of this page"}, {"H", "history (also g → history, g → notes)"},
```

9. Imports in `tui.go`: `"w5f/internal/catalog"`, `"w5f/internal/index"`, `"w5f/internal/personal"`.

`internal/tui/welcome.go` — replace the file with:

```go
package tui

import (
	"fmt"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// welcomeDoc is shown when w5f starts without a target. It doubles as a
// hands-on tour of the reader, and lists what was left half-read.
func welcomeDoc(version string) *doc.Document {
	d := &doc.Document{
		Title:  "W5F // ARCHIVE NODE",
		Origin: "local",
		Meta:   []doc.KV{{Key: "version", Value: version}},
		Links: []doc.Link{
			{Href: "https://scp-wiki.wikidot.com/scp-173", Text: "SCP-173"},
			{Href: "https://wanderers-library.wikidot.com/six-etchings-in-the-basalt-of-olympus-mons", Text: "Six Etchings"},
			{Href: "https://backrooms-wiki.wikidot.com/level-0", Text: "Level 0"},
			{Href: "w5f:feeds", Text: "Periodicals"},
			{Href: "w5f:books", Text: "Library"},
			{Href: "w5f:queue", Text: "Reading queue"},
			{Href: "w5f:notes", Text: "Notes & clippings"},
			{Href: "w5f:history", Text: "History"},
		},
	}
	d.Blocks = []doc.Block{
		doc.Paragraph{Text: doc.Inline{
			{Text: "A reading terminal for the textual internet and a personal archive. "},
			{Text: "Milestone M4: your personal layer — queue, notes, clippings and search.", Style: doc.Italic},
		}},
	}
	d.Blocks = append(d.Blocks, continueSection(d)...)
	d.Blocks = append(d.Blocks,
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Try it"}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "↑ ↓", Style: doc.Bold}, {Text: " move from link to link; when the next one is off screen the page scrolls instead."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "→", Style: doc.Bold}, {Text: " or "}, {Text: "enter", Style: doc.Bold}, {Text: " opens the selected link or section, "}, {Text: "←", Style: doc.Bold}, {Text: " goes back — as many times as you like."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "/", Style: doc.Bold}, {Text: " searches everything you have read; "}, {Text: "a", Style: doc.Bold}, {Text: " queues a page, "}, {Text: "n", Style: doc.Bold}, {Text: " writes a note, "}, {Text: "y", Style: doc.Bold}, {Text: " clips paragraphs; "}, {Text: "?", Style: doc.Bold}, {Text: " shows every key."}}}},
		}},
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Start reading"}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "Periodicals — ", Style: doc.Bold}, {Text: "your shelves of magazines and blogs", Link: 4}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Library — ", Style: doc.Bold}, {Text: "your books, Project Gutenberg, Standard Ebooks", Link: 5}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "SCP Foundation — ", Style: doc.Bold}, {Text: "SCP-173", Link: 1}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Wanderers' Library — ", Style: doc.Bold}, {Text: "Six Etchings in the Basalt of Olympus Mons", Link: 2}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "The Backrooms — ", Style: doc.Bold}, {Text: "Level 0", Link: 3}}}},
		}},
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Your archive"}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "Reading queue", Link: 6}, {Text: " — pages you want to read (press a)", Style: doc.Italic}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Notes & clippings", Link: 7}, {Text: " — Markdown files Obsidian can open", Style: doc.Italic}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "History", Link: 8}, {Text: " — everything you have read (H)", Style: doc.Italic}}}},
		}},
		doc.Collapsible{ID: 1, Show: "About collapsible sections", Hide: "Hide", Blocks: []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: "Folded sections start closed, exactly as their authors intended. No spoilers leak before you choose to open them. Press enter on the label to toggle; "}, {Text: "+", Style: doc.Bold}, {Text: " and "}, {Text: "-", Style: doc.Bold}, {Text: " expand or fold every section on the page."}}},
		}},
		doc.Rule{},
		doc.Paragraph{Text: doc.Inline{{Text: "Open any file or URL from the shell: w5f https://… or w5f page.html", Style: doc.Code}}},
	)
	d.Collapsibles = 1
	return d
}

// continueSection lists pages and books left half-read, newest first.
func continueSection(d *doc.Document) []doc.Block {
	db, err := store.Default()
	if err != nil {
		return nil
	}
	var items [][]doc.Block
	add := func(href, title, sub string) {
		d.Links = append(d.Links, doc.Link{Href: href, Text: title})
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: title, Link: len(d.Links)}, {Text: "  " + sub, Style: doc.Italic}}}})
	}
	vs, _ := db.Unfinished(5)
	for _, v := range vs {
		sub := fmt.Sprintf("%d%%", int(v.Pos*100+0.5))
		if v.Catalog != "" {
			sub += " · " + v.Catalog
		}
		add(v.Target, v.Title, sub)
	}
	bs, _ := db.Books("recent", 3)
	for _, b := range bs {
		if b.Chapters > 0 && (b.Chapter < b.Chapters-1 || b.Pos < 0.95) {
			add(fmt.Sprintf("w5f:book/%d", b.ID), b.Title, fmt.Sprintf("chapter %d of %d", b.Chapter+1, b.Chapters))
		}
	}
	if len(items) == 0 {
		return nil
	}
	return []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: "Continue reading"}}}, doc.List{Items: items}}
}
```

- [ ] **Step 5: Run** `gofmt -w internal/tui && go vet ./internal/tui && go test ./internal/tui` → PASS (existing TUI tests included)

---

### Task 9: TUI — note box (`n`)

**Files:**
- Create: `internal/tui/note.go`
- Modify: `internal/tui/tui.go` (`modeNote`, `note noteState` field, key routing, paste, body/bottom bar)
- Test: `internal/tui/note_test.go`

**Interfaces:**
- Consumes: `pageSource`, `indexFile` (Task 8), `personal.AppendNote/Rel`
- Produces: `modeNote`; `type noteState`; `func (m *Model) openNote()`; `func (m Model) noteKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd)`; `func (m Model) overlayNote(page []string) []string`

- [ ] **Step 1: Failing test** (`internal/tui/note_test.go`)

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/personal"
)

func key(m Model, k tea.KeyPressMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

func TestNoteBoxSaveAndDiscard(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-096", textPage("SCP-096", "The shy guy."))
	m = press(m, "n", "h", "i")
	if m.mode != modeNote {
		t.Fatalf("mode %v", m.mode)
	}
	m = press(m, "enter", "s", "e", "c", "o", "n", "d", "left", "left")
	m = key(m, tea.KeyPressMsg{Code: tea.KeyBackspace}) // "seco|nd" → "sec|nd"
	m = key(m, tea.KeyPressMsg{Code: tea.KeyUp})
	m = key(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	m = press(m, "!")
	if got := m.note.text(); got != "hi!\nsecnd" {
		t.Fatalf("text %q", got)
	}
	if !strings.Contains(strings.Join(m.bodyLines(), "\n"), "NOTE") {
		t.Error("note box not drawn")
	}
	m = key(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.mode != modeRead || !strings.HasPrefix(m.status, "note saved to Notes/SCP-096.md") {
		t.Fatalf("after save: mode %v status %q", m.mode, m.status)
	}
	b, _ := os.ReadFile(filepath.Join(personal.Dir(), "Notes", "SCP-096.md"))
	if !strings.Contains(string(b), "hi!\nsecnd") || !strings.Contains(string(b), "catalog: \"FIC·SCP·096\"") {
		t.Errorf("note file:\n%s", b)
	}
	m = press(m, "n", "x", "esc")
	if m.mode != modeNote || !strings.Contains(m.note.msg, "esc again") {
		t.Fatalf("first esc must ask: %v %q", m.mode, m.note.msg)
	}
	m = press(m, "esc")
	if m.mode != modeRead {
		t.Error("second esc discards")
	}
	m = press(m, "n", "esc")
	if m.mode != modeRead {
		t.Error("an empty note box closes on the first esc")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/tui -run NoteBox` → FAIL (undefined `modeNote`)

- [ ] **Step 3: Implement** (`internal/tui/note.go`)

```go
package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"w5f/internal/personal"
)

// noteState is the pop-up note box (key n).
type noteState struct {
	src     personal.Source
	lines   [][]rune
	row     int
	col     int
	confirm bool // esc pressed once with text in the box
	msg     string
}

func (m *Model) openNote() {
	m.mode = modeNote
	m.note = noteState{src: pageSource(m.cur), lines: [][]rune{{}}}
}

func (ns *noteState) text() string {
	parts := make([]string, len(ns.lines))
	for i, l := range ns.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (ns *noteState) empty() bool { return strings.TrimSpace(ns.text()) == "" }

func (ns *noteState) insert(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for i, part := range strings.Split(s, "\n") {
		if i > 0 {
			ns.newline()
		}
		rs := []rune(part)
		ln := ns.lines[ns.row]
		nl := make([]rune, 0, len(ln)+len(rs))
		nl = append(nl, ln[:ns.col]...)
		nl = append(nl, rs...)
		nl = append(nl, ln[ns.col:]...)
		ns.lines[ns.row] = nl
		ns.col += len(rs)
	}
}

func (ns *noteState) newline() {
	ln := ns.lines[ns.row]
	head := append([]rune{}, ln[:ns.col]...)
	tail := append([]rune{}, ln[ns.col:]...)
	ns.lines[ns.row] = head
	ns.lines = append(ns.lines[:ns.row+1], append([][]rune{tail}, ns.lines[ns.row+1:]...)...)
	ns.row, ns.col = ns.row+1, 0
}

func (ns *noteState) backspace() {
	if ns.col > 0 {
		ln := ns.lines[ns.row]
		ns.lines[ns.row] = append(append([]rune{}, ln[:ns.col-1]...), ln[ns.col:]...)
		ns.col--
		return
	}
	if ns.row == 0 {
		return
	}
	prev := ns.lines[ns.row-1]
	ns.col = len(prev)
	ns.lines[ns.row-1] = append(append([]rune{}, prev...), ns.lines[ns.row]...)
	ns.lines = append(ns.lines[:ns.row], ns.lines[ns.row+1:]...)
	ns.row--
}

func (ns *noteState) del() {
	ln := ns.lines[ns.row]
	if ns.col < len(ln) {
		ns.lines[ns.row] = append(append([]rune{}, ln[:ns.col]...), ln[ns.col+1:]...)
		return
	}
	if ns.row+1 < len(ns.lines) {
		ns.lines[ns.row] = append(append([]rune{}, ln...), ns.lines[ns.row+1]...)
		ns.lines = append(ns.lines[:ns.row+1], ns.lines[ns.row+2:]...)
	}
}

func (m Model) noteKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ns := &m.note
	s := k.String()
	if s != "esc" {
		ns.confirm, ns.msg = false, ""
	}
	switch s {
	case "esc":
		if ns.empty() || ns.confirm {
			m.mode = modeRead
			return m, nil
		}
		ns.confirm, ns.msg = true, "press esc again to discard this note"
	case "ctrl+s":
		if ns.empty() {
			ns.msg = "the note is empty"
			return m, nil
		}
		path, err := personal.AppendNote(ns.src, ns.text(), time.Now())
		if err != nil {
			ns.msg = "error: " + err.Error() // the text stays in the box
			return m, nil
		}
		m.mode = modeRead
		m.status = "note saved to " + personal.Rel(path)
		return m, indexFile(path)
	case "enter":
		ns.newline()
	case "backspace":
		ns.backspace()
	case "delete":
		ns.del()
	case "left":
		if ns.col > 0 {
			ns.col--
		} else if ns.row > 0 {
			ns.row--
			ns.col = len(ns.lines[ns.row])
		}
	case "right":
		if ns.col < len(ns.lines[ns.row]) {
			ns.col++
		} else if ns.row < len(ns.lines)-1 {
			ns.row, ns.col = ns.row+1, 0
		}
	case "up":
		if ns.row > 0 {
			ns.row--
			ns.col = min(ns.col, len(ns.lines[ns.row]))
		}
	case "down":
		if ns.row < len(ns.lines)-1 {
			ns.row++
			ns.col = min(ns.col, len(ns.lines[ns.row]))
		}
	case "home":
		ns.col = 0
	case "end":
		ns.col = len(ns.lines[ns.row])
	default:
		if k.Text != "" {
			ns.insert(k.Text)
		}
	}
	return m, nil
}

// noteView lays the text out in rows of at most width runes with a block
// cursor, and returns the row that holds the cursor.
func noteView(ns noteState, width int) ([]string, int) {
	var rows []string
	cur := 0
	for i, ln := range ns.lines {
		r := append([]rune{}, ln...)
		if i == ns.row {
			r = append(r[:ns.col], append([]rune{'█'}, r[ns.col:]...)...)
			cur = len(rows) + ns.col/width
		}
		for {
			if len(r) <= width {
				rows = append(rows, string(r))
				break
			}
			rows = append(rows, string(r[:width]))
			r = r[width:]
		}
	}
	return rows, cur
}

// overlayNote draws the note box over the page lines.
func (m Model) overlayNote(page []string) []string {
	w := m.dictWidth()
	inner := w - 4
	chrome := m.theme.Chrome
	box := lipgloss.NewStyle().Background(chrome.BG).Foreground(chrome.FG)
	dim := lipgloss.NewStyle().Background(chrome.BG).Foreground(chrome.Dim)
	pad := func(s string) string {
		s = ansi.Truncate(s, inner, "…")
		if d := inner - ansi.StringWidth(s); d > 0 {
			s += strings.Repeat(" ", d)
		}
		return s
	}
	row := func(content string) string { return box.Render("│ ") + content + box.Render(" │") }
	title := " NOTE "
	if m.note.src.Catalog != "" {
		title += "· " + m.note.src.Catalog + " "
	}
	lines := []string{box.Render("┌─" + title + strings.Repeat("─", max(0, w-3-ansi.StringWidth(title))) + "┐")}
	lines = append(lines, row(dim.Render(pad(m.note.src.Title))))
	lines = append(lines, box.Render("├"+strings.Repeat("─", w-2)+"┤"))
	text, cur := noteView(m.note, inner)
	body := m.dictBodyHeight()
	start := 0
	if cur >= body {
		start = cur - body + 1
	}
	for i := 0; i < body; i++ {
		if j := start + i; j < len(text) {
			lines = append(lines, row(box.Render(pad(text[j]))))
		} else {
			lines = append(lines, row(box.Render(pad(""))))
		}
	}
	hint := " ctrl+s save · esc discard "
	if m.note.msg != "" {
		hint = " " + m.note.msg + " "
	}
	lines = append(lines, box.Render("└"+strings.Repeat("─", max(0, w-2-ansi.StringWidth(hint)))+hint+"┘"))
	out := append([]string{}, page...)
	for len(out) < m.bodyHeight() {
		out = append(out, "")
	}
	x := strings.Repeat(" ", max(0, (m.width-w)/2))
	for i, l := range lines {
		r := 1 + i
		if r >= len(out) {
			break
		}
		out[r] = x + l
	}
	return out
}
```

In `tui.go`: append `modeNote` to the mode constants; add `note noteState` to `Model`; in `key` add `&& m.mode != modeNote` to the esc-cancels-load condition, `case modeNote: return m.noteKey(k)` to the mode switch and `case "n": m.openNote(); return m, nil` to the reading keys; in the `tea.PasteMsg` switch `case modeNote: m.note.insert(msg.Content)`; in `bodyLines` add

```go
	case modeNote:
		read := m
		read.mode = modeRead
		return m.overlayNote(read.bodyLines())
```

and in `bottomBar` `case m.mode == modeNote: text = " note · type · enter new line · ctrl+s save · esc discard"`.

- [ ] **Step 4: Run** `gofmt -w internal/tui && go vet ./internal/tui && go test ./internal/tui` → PASS

---

### Task 10: TUI — clipping mode (`y`)

**Files:**
- Create: `internal/tui/clip.go`
- Modify: `internal/tui/tui.go` (`modeClip`, `clip clipState`, keys, highlight in `bodyLines`, bottom bar)
- Test: `internal/tui/clip_test.go`

**Interfaces:**
- Consumes: `render.Layout.Paras` (Task 4), `pageSource`, `indexFile` (Task 8), `personal.AppendClipping/Rel`
- Produces: `modeClip`; `type clipState struct { anchor, cur int }`; `func (m *Model) startClip()`; `func (m Model) clipKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd)`

- [ ] **Step 1: Failing test** (`internal/tui/clip_test.go`)

```go
package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
)

func TestClipModeSelectsAndSaves(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-682", textPage("SCP-682", "First paragraph.", "Second paragraph.", "Third paragraph."))
	if m = press(m, "y"); m.mode != modeClip {
		t.Fatalf("mode %v", m.mode)
	}
	m = press(m, "down")
	m = key(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	lo, hi := m.clip.span()
	if lo != 1 || hi != 2 {
		t.Fatalf("selection %d..%d", lo, hi)
	}
	m = press(m, "enter")
	if m.mode != modeRead || !strings.Contains(m.status, "clipping saved (2 paragraphs)") {
		t.Fatalf("status %q", m.status)
	}
	rel := m.status[strings.Index(m.status, " to ")+4:]
	b, err := os.ReadFile(personalPath(rel))
	if err != nil || !strings.Contains(string(b), "> Second paragraph.\n>\n> Third paragraph.") || strings.Contains(string(b), "First") {
		t.Errorf("clipping %v:\n%s", err, b)
	}
	m = open(m, "w5f:feeds", &doc.Document{Title: "Empty"})
	if m = press(m, "y"); m.mode != modeRead || !strings.Contains(m.status, "nothing to clip") {
		t.Errorf("empty page: %v %q", m.mode, m.status)
	}
}
```

Add a helper to `internal/tui/personal_test.go`:

```go
func personalPath(rel string) string { return filepath.Join(personal.Dir(), filepath.FromSlash(rel)) }
```

(import `"path/filepath"` there).

- [ ] **Step 2: Run** `go test ./internal/tui -run ClipMode` → FAIL (undefined `modeClip`)

- [ ] **Step 3: Implement** (`internal/tui/clip.go`)

```go
package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/personal"
)

// clipState is the paragraph selection of the clipping mode (key y).
type clipState struct{ anchor, cur int }

func (cs clipState) span() (int, int) {
	lo, hi := cs.anchor, cs.cur
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

func (m *Model) startClip() {
	paras := m.cur.layout.Paras
	if len(paras) == 0 {
		m.status = "nothing to clip on this page"
		return
	}
	i := 0
	for j, pr := range paras {
		if pr.End > m.cur.offset {
			i = j
			break
		}
	}
	m.clip = clipState{anchor: i, cur: i}
	m.mode = modeClip
}

func (m Model) clipKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	paras := m.cur.layout.Paras
	cs := &m.clip
	switch k.String() {
	case "esc", "q":
		m.mode, m.status = modeRead, "clipping cancelled"
		return m, nil
	case "up", "k":
		if cs.cur > 0 {
			cs.cur--
		}
		cs.anchor = cs.cur
	case "down", "j":
		if cs.cur < len(paras)-1 {
			cs.cur++
		}
		cs.anchor = cs.cur
	case "shift+up", "K":
		if cs.cur > 0 {
			cs.cur--
		}
	case "shift+down", "J":
		if cs.cur < len(paras)-1 {
			cs.cur++
		}
	case "enter":
		lo, hi := cs.span()
		var texts []string
		for _, pr := range paras[lo : hi+1] {
			texts = append(texts, pr.Text)
		}
		m.mode = modeRead
		path, err := personal.AppendClipping(pageSource(m.cur), texts, time.Now())
		if err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		plural := "s"
		if len(texts) == 1 {
			plural = ""
		}
		m.status = fmt.Sprintf("clipping saved (%d paragraph%s) to %s", len(texts), plural, personal.Rel(path))
		return m, indexFile(path)
	}
	pr := paras[cs.cur]
	m.reveal(pr.End - 1)
	m.reveal(pr.Start)
	return m, nil
}
```

In `tui.go`: append `modeClip`; add `clip clipState` to `Model`; `case modeClip: return m.clipKey(k)` in the mode switch; `case "y": m.startClip(); return m, nil` in the reading keys; in `bodyLines`, compute the highlighted range before the line loop:

```go
	selLo, selHi := -1, -1
	if m.mode == modeClip && len(p.layout.Paras) > 0 {
		lo, hi := m.clip.span()
		selLo, selHi = p.layout.Paras[lo].Start, p.layout.Paras[hi].End-1
	}
```

and in the loop replace `for _, ln := range p.layout.Lines[p.offset:end] {` with `for li, ln := range p.layout.Lines[p.offset:end] {` and the segment rendering with:

```go
			sel := p.offset+li >= selLo && p.offset+li <= selHi
			sb.WriteString(m.theme.Seg(sg, sel || (sg.Focus != 0 && sg.Focus == p.focus)).Render(sg.Text))
```

In `bottomBar`: `case m.mode == modeClip: text = " clip · ↑↓ paragraph · shift+↑↓ extend · enter save · esc cancel"`.

- [ ] **Step 4: Run** `gofmt -w internal/tui && go vet ./internal/tui && go test ./internal/tui` → PASS

---

### Task 11: Full suite, live smoke, builds, docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Full suite** — `gofmt -l . ; go vet ./... && go test ./...` → all packages ok.

- [ ] **Step 2: Smoke** (temporary data, notes and library; nothing of the owner's is touched)

```bash
cd /c/Users/murat/code/w5f && go build -o bin/w5f-dev.exe ./cmd/w5f
T=$(mktemp -d); export APPDATA="$(cygpath -w $T)" W5F_BOOKS="$(cygpath -w $T)\\Books" W5F_NOTES="$(cygpath -w $T)\\Notes"
./bin/w5f-dev.exe sync | tail -2                              # indexes feed items
./bin/w5f-dev.exe dump -w 76 "find arkeoloji" | head -20     # [RSS] results with snippets
./bin/w5f-dev.exe dump -w 76 "queue" | head -12
./bin/w5f-dev.exe reindex
```

Expected: `sync` prints "N new items indexed for search"; `find` lists `[RSS]` hits with bold matches; the queue page shows the empty sections; `reindex` prints counts. The interactive keys (`/ a A n y s H`) are covered by the TUI tests; the owner tries them in the terminal.

- [ ] **Step 3: Builds** (version 0.4.0)

```bash
V=0.4.0 && (mv -f bin/w5f.exe bin/w5f.exe.old 2>/dev/null; true) && go build -ldflags "-X main.version=$V" -o bin/w5f.exe ./cmd/w5f
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags "-s -w -X main.version=$V" -o bin/w5f-linux-amd64 ./cmd/w5f
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -trimpath -ldflags "-s -w -X main.version=$V" -o bin/w5f-linux-386 ./cmd/w5f
rm -f bin/w5f.exe.old bin/w5f-dev.exe; rm -rf "$T"
```

- [ ] **Step 4: README** — add under Usage:

```
w5f "find <words>"       search everything you have read (also: press / in the reader)
w5f queue | notes | history   your reading queue, notes & clippings, history
w5f reindex              rebuild the search index (after moving or deleting the database)
```

and a "Personal layer" paragraph: keys `/ a A n y s H`; notes folder `~/Archive/Notes` (`W5F_NOTES` or `notes = "…"` in `config.toml`), Obsidian-compatible; the database holds only the rebuildable index and history. Under Layout: `internal/personal  queue, notes, clippings, saved pages (Markdown)`, `internal/index  full-text search (SQLite FTS5)`, `internal/catalog  call numbers`.
