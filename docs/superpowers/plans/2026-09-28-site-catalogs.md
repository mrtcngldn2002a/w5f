# Site Catalogs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the owner add any book website to the Library by URL: W5F detects the site's search, learns its result list, runs full multi-page searches (with author/title fields) and downloads books into `~/Archive/Books`.

**Architecture:** New package `internal/sitecat` with focused files: `profile.go` (TOML profiles), `extract.go` (result-list learning + next page), `probe.go` (search discovery + check report), `search.go` (full multi-page search), `downloads.go` (download discovery + file fetch), `docs.go` (reader pages). `books` gains `AddFile` (shared by all catalogs) and a hook to list site catalogs; `source`, `tui` and `cmd` route `w5f:catalog…` addresses.

**Tech Stack:** Go 1.26 toolchain, goquery, golang.org/x/net/html, BurntSushi/toml, existing `fetch`, `store`, `books`, `doc` packages. Tests with `httptest`.

**Spec:** `docs/superpowers/specs/2026-09-28-site-catalogs-design.md`

## Global Constraints

- Scope: downloadable books only (EPUB/PDF/TXT/MOBI/AZW3/FB2/DJVU/CBZ/CBR). Plain `.zip` is not a book.
- Full search defaults: `max_pages = 10`, `max_results = 300` (per profile, editable).
- Format preference default: `["epub", "pdf", "txt", "mobi", "azw3", "fb2", "djvu", "cbz", "cbr"]`.
- Never bypass bot protection, logins, captchas, JavaScript challenges; 401/403/429 → friendly message, no retries with other identities.
- Downloaded file size limit 100 MB; type confirmed by magic bytes.
- All HTTP through the shared `fetch.Fetcher` (politeness delay, cache) except file bodies (streamed with the same User-Agent).
- UI text in English; the owner-facing reply in Turkish.
- Pure Go (CGO off); must keep building for `linux/amd64` with `GOAMD64=v1` and `linux/386`.
- Windows editing note: after any Python-based edit run `gofmt -w .` (CRLF). Commit only if the owner asks (repo has no commits yet).

## Notes on the spec

- The spec's `g → catalog-test <word>` is implemented as `g → catalog-add <address> <word>` (same check page, one command less).
- Browse-only catalogs (no search, learnable list on the front page) are covered in Task 5 (probe) and Task 6 (catalog page).

## Review Focus

1. **Result pages whose navigation menu is the largest link group** — the learner must still pick the results list (test in Task 3).
2. **Sites that keep returning the last page for page=N+1** — full search must stop, not loop to max_pages (test in Task 5).
3. **Download links that return an HTML "download started" page** — must follow meta refresh, and fail clearly on a plain HTML page (test in Task 6).
4. **`author:` / `title:` on a site without fields** — prefix dropped and reported, not sent as literal text (test in Task 5).
5. **Relative/protocol-relative/`javascript:`/`#` links in result items** — resolved or ignored, never produce broken result URLs (test in Task 3).

---

### Task 1: `books.AddFile` — shared "store a downloaded file" step

**Files:**
- Modify: `internal/books/catalog.go` (Download uses AddFile)
- Create: `internal/books/addfile.go`
- Test: `internal/books/addfile_test.go`

**Interfaces:**
- Produces: `func AddFile(db *store.DB, tmpPath, format, title, author, src string) (store.Book, error)` — moves `tmpPath` into `LibraryDir()` as "Author - Title.ext" (unique), reads EPUB metadata when `format == "epub"`, records the book with `Source = src`.

- [ ] **Step 1: Write the failing test** (`internal/books/addfile_test.go`)

```go
package books

import (
	"os"
	"path/filepath"
	"testing"

	"w5f/internal/store"
)

func TestAddFileMovesAndRecords(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("W5F_BOOKS", lib)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// EPUB: metadata from the file wins over the given title.
	tmp := filepath.Join(lib, ".download-1.epub")
	writeEPUB(t, tmp)
	b, err := AddFile(db, tmp, "epub", "ignored", "", "site:x:https://x/1")
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "The Shadow of the Torturer" || b.Author != "Gene Wolfe" || b.Chapters != 3 {
		t.Errorf("epub meta: %+v", b)
	}
	if filepath.Base(b.Path) != "Gene Wolfe - The Shadow of the Torturer.epub" {
		t.Errorf("path = %s", b.Path)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file should have been moved")
	}
	if got, ok := db.BookBySource("site:x:https://x/1"); !ok || got.ID != b.ID {
		t.Error("book not findable by source")
	}

	// Non-EPUB with the same name gets a unique path.
	tmp2 := filepath.Join(lib, ".download-2.pdf")
	os.WriteFile(tmp2, []byte("%PDF-1.4"), 0o644)
	p, err := AddFile(db, tmp2, "pdf", "Dracula", "Bram Stoker", "site:x:https://x/2")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p.Path) != "Bram Stoker - Dracula.pdf" || p.Format != "pdf" {
		t.Errorf("pdf: %+v", p)
	}
	tmp3 := filepath.Join(lib, ".download-3.pdf")
	os.WriteFile(tmp3, []byte("%PDF-1.4"), 0o644)
	p2, _ := AddFile(db, tmp3, "pdf", "Dracula", "Bram Stoker", "site:x:https://x/3")
	if filepath.Base(p2.Path) != "Bram Stoker - Dracula (2).pdf" {
		t.Errorf("unique path = %s", p2.Path)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/books -run TestAddFile -v`
Expected: FAIL — `undefined: AddFile`

- [ ] **Step 3: Implement** (`internal/books/addfile.go`)

```go
package books

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"w5f/internal/store"
)

// AddFile moves a downloaded file into the library and records it. For EPUB
// files the title, author and language are read from the book itself.
func AddFile(db *store.DB, tmpPath, format, title, author, src string) (store.Book, error) {
	format = strings.ToLower(strings.TrimPrefix(format, "."))
	b := store.Book{Format: format, Title: strings.TrimSpace(title), Author: strings.TrimSpace(author), Source: src}
	if format == "epub" {
		e, err := OpenEPUB(tmpPath)
		if err != nil {
			return store.Book{}, fmt.Errorf("downloaded file is not a readable EPUB: %w", err)
		}
		if e.Title != "" {
			b.Title = e.Title
		}
		if e.Author != "" {
			b.Author = e.Author
		}
		b.Lang, b.Chapters = e.Lang, len(e.Chapters)
		e.Close()
	}
	if b.Title == "" {
		b.Title = "Untitled"
	}
	dir := LibraryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return store.Book{}, err
	}
	b.Path = uniquePath(filepath.Join(dir, FileName(b.Author, b.Title, "."+format)))
	if err := os.Rename(tmpPath, b.Path); err != nil {
		return store.Book{}, err
	}
	id, err := db.UpsertBook(b)
	if err != nil {
		return store.Book{}, err
	}
	return db.Book(id)
}

// uniquePath appends " (2)", " (3)"… when a file already exists.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(c); os.IsNotExist(err) {
			return c
		}
	}
}
```

Then in `internal/books/catalog.go` replace the block from `e, err := OpenEPUB(tmpName)` to the final `return db.Book(id)` inside `Download` with:

```go
	return AddFile(db, tmpName, "epub", "", "", src)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/books -v`
Expected: PASS (AddFile test and existing library tests)

- [ ] **Step 5: Format**

Run: `gofmt -w . && go vet ./internal/books`

---

### Task 2: `sitecat` profiles

**Files:**
- Create: `internal/sitecat/profile.go`
- Test: `internal/sitecat/profile_test.go`

**Interfaces:**
- Produces:
  - `type Profile struct { ID, Name, Home string; Added time.Time; Search SearchSpec; Layout Layout; Download DownloadSpec }`
  - `type SearchSpec struct { Kind, Template string; Fields map[string]string; MaxPages, MaxResults int }` (Kind: `opensearch|opds|form|wordpress|none`)
  - `type Layout struct { Parent, Item string }`
  - `type DownloadSpec struct { Prefer []string }`
  - `func Path() string`, `func LoadAll(path string) ([]Profile, error)`, `func SaveAll(path string, ps []Profile) error`, `func Find(ps []Profile, id string) int`, `func IDFor(home string, existing []Profile) string`, `func (p *Profile) ApplyDefaults()`

- [ ] **Step 1: Failing test**

```go
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
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run TestProfiles -v` → FAIL (package missing)

- [ ] **Step 3: Implement** (`internal/sitecat/profile.go`)

```go
// Package sitecat turns any book website into a Library catalog: it detects
// how the site is searched, learns how its result lists look, searches all
// result pages and finds download links. What it learns is kept as an
// editable TOML profile per site.
package sitecat

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"w5f/internal/store"
)

// Profile is everything W5F knows about one site catalog.
type Profile struct {
	ID       string       `toml:"id"`
	Name     string       `toml:"name"`
	Home     string       `toml:"home"`
	Added    time.Time    `toml:"added"`
	Search   SearchSpec   `toml:"search"`
	Layout   Layout       `toml:"layout"`
	Download DownloadSpec `toml:"download"`
}

// SearchSpec says how to search the site. Templates contain {q}.
type SearchSpec struct {
	Kind       string            `toml:"kind"` // opensearch | opds | form | wordpress | none
	Template   string            `toml:"template"`
	Fields     map[string]string `toml:"fields,omitempty"` // "author"/"title" → template
	MaxPages   int               `toml:"max_pages"`
	MaxResults int               `toml:"max_results"`
}

// Layout is the learned shape of a result list: the container's signature
// path and the signature of one result element.
type Layout struct {
	Parent string `toml:"parent"`
	Item   string `toml:"item"`
}

// DownloadSpec orders the formats to fetch.
type DownloadSpec struct {
	Prefer []string `toml:"prefer"`
}

// DefaultPrefer is the default format order.
var DefaultPrefer = []string{"epub", "pdf", "txt", "mobi", "azw3", "fb2", "djvu", "cbz", "cbr"}

type file struct {
	Catalogs []Profile `toml:"catalog"`
}

// Path is the profile file in the W5F data directory.
func Path() string { return filepath.Join(store.DataDir(), "catalogs.toml") }

// ApplyDefaults fills unset limits and preferences.
func (p *Profile) ApplyDefaults() {
	if p.Search.MaxPages <= 0 {
		p.Search.MaxPages = 10
	}
	if p.Search.MaxResults <= 0 {
		p.Search.MaxResults = 300
	}
	if len(p.Download.Prefer) == 0 {
		p.Download.Prefer = append([]string{}, DefaultPrefer...)
	}
}

// LoadAll reads all profiles; a missing file means none.
func LoadAll(path string) ([]Profile, error) {
	var f file
	if _, err := toml.DecodeFile(path, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range f.Catalogs {
		f.Catalogs[i].ApplyDefaults()
	}
	return f.Catalogs, nil
}

// SaveAll writes all profiles atomically.
func SaveAll(path string, ps []Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	w, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w.WriteString("# W5F site catalogs — added with g → catalog-add <url>. Safe to edit.\n\n")
	if err := toml.NewEncoder(w).Encode(file{Catalogs: ps}); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find returns the index of a profile id, or -1.
func Find(ps []Profile, id string) int {
	for i := range ps {
		if ps[i].ID == id {
			return i
		}
	}
	return -1
}

// IDFor derives a short unique id from a site address ("books-example-org").
func IDFor(home string, existing []Profile) string {
	base := "catalog"
	if u, err := url.Parse(home); err == nil && u.Hostname() != "" {
		base = strings.ReplaceAll(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), ".", "-")
	}
	id := base
	for i := 2; Find(existing, id) >= 0; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}
```

- [ ] **Step 4: Run** `go test ./internal/sitecat -run TestProfiles -v` → PASS

- [ ] **Step 5: Format** `gofmt -w . && go vet ./internal/sitecat`

---

### Task 3: Result-list learning and next page (`extract.go`)

**Files:**
- Create: `internal/sitecat/extract.go`
- Test: `internal/sitecat/extract_test.go`

**Interfaces:**
- Consumes: `Layout` (Task 2)
- Produces:
  - `type Result struct { Title, URL, Extra string }`
  - `func LearnLayout(body []byte, pageURL string) (Layout, []Result)` — zero Layout when nothing learnable
  - `func Results(body []byte, pageURL string, l Layout) []Result`
  - `func Extract(body []byte, pageURL string, l Layout) ([]Result, Layout)` — stored layout first, re-learns when it yields nothing
  - `func NextPage(body []byte, pageURL string) string` — absolute URL or ""

- [ ] **Step 1: Failing tests** (`internal/sitecat/extract_test.go`)

```go
package sitecat

import (
	"fmt"
	"strings"
	"testing"
)

func page(body string) []byte { return []byte("<html><body>" + body + "</body></html>") }

func navMenu() string {
	var b strings.Builder
	b.WriteString(`<nav><ul class="menu">`)
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, `<li class="item"><a href="/section/%d">Sec %d</a></li>`, i, i)
	}
	b.WriteString(`</ul></nav>`)
	return b.String()
}

func TestLearnLayoutShapes(t *testing.T) {
	cases := map[string]string{
		"ul": navMenu() + `<div class="main"><ul class="results">
<li class="book"><a href="/book/1">Dracula</a> by Bram Stoker, 1897</li>
<li class="book"><a href="/book/2">Dracula's Guest</a> by Bram Stoker</li>
<li class="book"><a href="/book/3">The Jewel of Seven Stars</a> by Bram Stoker</li>
<li class="book"><a href="javascript:void(0)">x</a></li></ul></div>`,
		"table": `<table class="res"><tr><th>Title</th><th>Author</th></tr>
<tr><td><a href="book.php?id=11">Dracula</a></td><td>Stoker, Bram</td></tr>
<tr><td><a href="book.php?id=12">Carmilla</a></td><td>Le Fanu, J. Sheridan</td></tr>
<tr><td><a href="book.php?id=13">The Vampyre</a></td><td>Polidori, John</td></tr></table>`,
		"cards": `<div class="grid"><div class="card"><h3><a href="//books.example.org/b/a">Dracula</a></h3><p>Gothic novel</p><a href="/b/a#reviews">12 reviews</a></div>
<div class="card"><h3><a href="/b/b">Varney the Vampire</a></h3><p>Penny dreadful</p></div>
<div class="card"><h3><a href="/b/c">Uncle Silas</a></h3><p>Mystery</p></div></div>`,
	}
	want := map[string][2]string{
		"ul":    {"Dracula", "https://books.example.org/book/1"},
		"table": {"Dracula", "https://books.example.org/search/book.php?id=11"},
		"cards": {"Dracula", "https://books.example.org/b/a"},
	}
	for name, body := range cases {
		l, rs := LearnLayout(page(body), "https://books.example.org/search/?q=dracula")
		if l.Item == "" || len(rs) < 3 {
			t.Fatalf("%s: layout=%+v results=%d", name, l, len(rs))
		}
		if rs[0].Title != want[name][0] || rs[0].URL != want[name][1] {
			t.Errorf("%s: first = %+v", name, rs[0])
		}
		for _, r := range rs {
			if strings.HasPrefix(r.URL, "javascript:") || r.URL == "" {
				t.Errorf("%s: bad url %q", name, r.URL)
			}
		}
		// The stored layout extracts the same list again.
		if again := Results(page(body), "https://books.example.org/search/?q=dracula", l); len(again) != len(rs) {
			t.Errorf("%s: re-extract %d vs %d", name, len(again), len(rs))
		}
	}
	_, rs := LearnLayout(page(cases["ul"]), "https://books.example.org/search/?q=dracula")
	if !strings.Contains(rs[0].Extra, "Bram Stoker") {
		t.Errorf("extra = %q", rs[0].Extra)
	}
}

func TestExtractRelearnsWhenLayoutBreaks(t *testing.T) {
	body := page(`<ol class="hits"><li><a href="/x/1">One book</a></li><li><a href="/x/2">Two book</a></li><li><a href="/x/3">Three book</a></li></ol>`)
	rs, l := Extract(body, "https://s.example/q", Layout{Parent: "div.gone", Item: "li.old"})
	if len(rs) != 3 || l.Item == "li.old" {
		t.Errorf("relearn: %d %+v", len(rs), l)
	}
}

func TestNextPage(t *testing.T) {
	base := "https://s.example/search?q=dracula"
	cases := []struct{ body, url, want string }{
		{`<a rel="next" href="/search?q=dracula&page=2">2</a>`, base, "https://s.example/search?q=dracula&page=2"},
		{`<a href="?q=dracula&p=3">Next ›</a>`, base, "https://s.example/search?q=dracula&p=3"},
		{`<p>no links</p>`, "https://s.example/search?q=dracula&page=4", "https://s.example/search?page=5&q=dracula"},
		{`<a href="/search?q=dracula&start=25">2</a><a href="/search?q=dracula&start=50">3</a>`, base, "https://s.example/search?q=dracula&start=25"},
		{`<p>nothing</p>`, base, ""},
	}
	for i, c := range cases {
		if got := NextPage(page(c.body), c.url); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'TestLearn|TestExtract|TestNext' -v` → FAIL (undefined)

- [ ] **Step 3: Implement** (`internal/sitecat/extract.go`)

```go
package sitecat

import (
	"bytes"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Result is one entry of a site's result list.
type Result struct {
	Title string
	URL   string
	Extra string // author, year, format hints…
}

// sig is an element's structural signature: tag plus sorted classes.
func sig(n *html.Node) string {
	if n == nil || n.Type != html.ElementNode {
		return ""
	}
	var cls []string
	for _, a := range n.Attr {
		if a.Key == "class" {
			cls = strings.Fields(a.Val)
		}
	}
	sort.Strings(cls)
	if len(cls) == 0 {
		return n.Data
	}
	return n.Data + "." + strings.Join(cls, ".")
}

// pathSig is the signature of n and up to two ancestors, outermost first.
func pathSig(n *html.Node) string {
	var parts []string
	for i := 0; i < 3 && n != nil && n.Type == html.ElementNode; i++ {
		parts = append([]string{sig(n)}, parts...)
		n = n.Parent
	}
	return strings.Join(parts, " > ")
}

var chromeTags = map[string]bool{"nav": true, "header": true, "footer": true, "aside": true}

func inChrome(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && chromeTags[p.Data] {
			return true
		}
	}
	return false
}

func sameSite(a, b *url.URL) bool {
	ha := strings.TrimPrefix(strings.ToLower(a.Hostname()), "www.")
	hb := strings.TrimPrefix(strings.ToLower(b.Hostname()), "www.")
	return ha == hb || strings.HasSuffix(ha, "."+hb) || strings.HasSuffix(hb, "."+ha)
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// bestLink picks a member's main link: one inside a heading if any, else the
// one with the longest text. Links to other sites, fragments and javascript
// are ignored.
func bestLink(member *goquery.Selection, base *url.URL) (title, href string) {
	var best *goquery.Selection
	bestScore := -1
	member.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		raw := strings.TrimSpace(a.AttrOr("href", ""))
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			return
		}
		u, err := base.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !sameSite(u, base) {
			return
		}
		text := collapse(a.Text())
		if utf8.RuneCountInString(text) < 3 {
			return
		}
		score := utf8.RuneCountInString(text)
		if a.ParentsFiltered("h1,h2,h3,h4,h5,h6").Length() > 0 {
			score += 1000
		}
		if score > bestScore {
			best, bestScore = a, score
		}
	})
	if best == nil {
		return "", ""
	}
	u, _ := base.Parse(strings.TrimSpace(best.AttrOr("href", "")))
	u.Fragment = ""
	return collapse(best.Text()), u.String()
}

func resultOf(member *goquery.Selection, base *url.URL) (Result, bool) {
	title, href := bestLink(member, base)
	if href == "" {
		return Result{}, false
	}
	extra := collapse(strings.Replace(collapse(member.Text()), title, "", 1))
	extra = strings.Trim(extra, " ,;·-–—|")
	if r := []rune(extra); len(r) > 160 {
		extra = string(r[:157]) + "…"
	}
	return Result{Title: title, URL: href, Extra: extra}, true
}

type group struct {
	parent  *html.Node
	item    string
	members []*goquery.Selection
}

// LearnLayout finds the most likely result list on a page.
func LearnLayout(body []byte, pageURL string) (Layout, []Result) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Layout{}, nil
	}
	base, _ := url.Parse(pageURL)
	var groups []group
	gq.Find("*").Each(func(_ int, s *goquery.Selection) {
		n := s.Get(0)
		bySig := map[string][]*goquery.Selection{}
		var order []string
		s.Children().Each(func(_ int, c *goquery.Selection) {
			k := sig(c.Get(0))
			if _, ok := bySig[k]; !ok {
				order = append(order, k)
			}
			bySig[k] = append(bySig[k], c)
		})
		for _, k := range order {
			if len(bySig[k]) >= 3 {
				groups = append(groups, group{parent: n, item: k, members: bySig[k]})
			}
		}
	})
	bestScore := 0.0
	var best group
	var bestResults []Result
	for _, g := range groups {
		var rs []Result
		hrefs := map[string]bool{}
		textLen := 0
		itemLike := 0
		for _, m := range g.members {
			r, ok := resultOf(m, base)
			if !ok {
				continue
			}
			rs = append(rs, r)
			hrefs[r.URL] = true
			t := utf8.RuneCountInString(collapse(m.Text()))
			if t > 300 {
				t = 300
			}
			textLen += t
			if u, err := url.Parse(r.URL); err == nil && (strings.ContainsAny(u.Path+u.RawQuery, "0123456789") ||
				strings.Count(strings.Trim(u.Path, "/"), "/") >= strings.Count(strings.Trim(base.Path, "/"), "/")) {
				itemLike++
			}
		}
		if len(rs) < 3 || len(hrefs) < 3 || float64(len(rs)) < 0.6*float64(len(g.members)) {
			continue
		}
		avg := float64(textLen) / float64(len(rs))
		if avg < 8 {
			continue
		}
		score := float64(len(rs)) * avg
		if itemLike*2 >= len(rs) {
			score *= 1.5
		}
		if inChrome(g.members[0].Get(0)) || chromeTags[g.parent.Data] {
			score *= 0.3
		}
		if score > bestScore {
			bestScore, best, bestResults = score, g, rs
		}
	}
	if bestResults == nil {
		return Layout{}, nil
	}
	return Layout{Parent: pathSig(best.parent), Item: best.item}, bestResults
}

// Results extracts results using a stored layout.
func Results(body []byte, pageURL string, l Layout) []Result {
	if l.Item == "" {
		return nil
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	var out []Result
	gq.Find("*").Each(func(_ int, s *goquery.Selection) {
		if pathSig(s.Get(0)) != l.Parent {
			return
		}
		s.Children().Each(func(_ int, c *goquery.Selection) {
			if sig(c.Get(0)) != l.Item {
				return
			}
			if r, ok := resultOf(c, base); ok {
				out = append(out, r)
			}
		})
	})
	return out
}

// Extract uses the stored layout and re-learns when it yields nothing.
func Extract(body []byte, pageURL string, l Layout) ([]Result, Layout) {
	if rs := Results(body, pageURL, l); len(rs) > 0 {
		return rs, l
	}
	nl, rs := LearnLayout(body, pageURL)
	if len(rs) == 0 {
		return nil, l
	}
	return rs, nl
}

var nextWords = map[string]bool{"next": true, "next page": true, "›": true, "»": true, ">": true, "→": true,
	"sonraki": true, "next ›": true, "next »": true, "next >": true, "next →": true, "older": true}

var (
	pageKeys   = []string{"page", "p", "pg"}
	offsetKeys = []string{"start", "offset", "start_index"}
	reDigits   = regexp.MustCompile(`^\d+$`)
)

// NextPage finds the address of the next result page.
func NextPage(body []byte, pageURL string) string {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	abs := func(h string) string {
		u, err := base.Parse(strings.TrimSpace(h))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.String() == base.String() {
			return ""
		}
		u.Fragment = ""
		return u.String()
	}
	if h, ok := gq.Find(`link[rel~="next"], a[rel~="next"]`).First().Attr("href"); ok {
		if u := abs(h); u != "" {
			return u
		}
	}
	var found string
	gq.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		t := strings.ToLower(collapse(a.Text()))
		if nextWords[t] || strings.HasPrefix(t, "next ") {
			if u := abs(a.AttrOr("href", "")); u != "" {
				found = u
				return false
			}
		}
		return true
	})
	if found != "" {
		return found
	}
	q := base.Query()
	// A page number in the current address: increment it.
	for _, k := range pageKeys {
		if v := q.Get(k); reDigits.MatchString(v) {
			n, _ := strconv.Atoi(v)
			q.Set(k, strconv.Itoa(n+1))
			u := *base
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	// Links on the page that differ only in a page/offset parameter: take the
	// smallest value above the current one.
	cur := map[string]int{}
	for _, k := range append(append([]string{}, pageKeys...), offsetKeys...) {
		if v, err := strconv.Atoi(q.Get(k)); err == nil {
			cur[k] = v
		}
	}
	bestVal, best := -1, ""
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		u, err := base.Parse(strings.TrimSpace(a.AttrOr("href", "")))
		if err != nil || u.Path != base.Path || !sameSite(u, base) {
			return
		}
		uq := u.Query()
		for _, k := range append(append([]string{}, pageKeys...), offsetKeys...) {
			v, err := strconv.Atoi(uq.Get(k))
			if err != nil || v <= cur[k] {
				continue
			}
			other := u.Query()
			other.Del(k)
			mine := base.Query()
			mine.Del(k)
			if other.Encode() != mine.Encode() {
				continue
			}
			if bestVal == -1 || v < bestVal {
				u.Fragment = ""
				bestVal, best = v, u.String()
			}
		}
	})
	return best
}
```

- [ ] **Step 4: Run** `go test ./internal/sitecat -run 'TestLearn|TestExtract|TestNext' -v` → PASS

- [ ] **Step 5: Format** `gofmt -w . && go vet ./internal/sitecat`

---

### Task 4: Download discovery and file fetching (`downloads.go`)

**Files:**
- Create: `internal/sitecat/downloads.go`
- Test: `internal/sitecat/downloads_test.go`

**Interfaces:**
- Consumes: `fetch.Fetcher`
- Produces:
  - `type Download struct { URL, Format, Label string }`
  - `func FindIn(body []byte, pageURL string, prefer []string) []Download` (pure)
  - `func FindDownloads(ctx context.Context, f *fetch.Fetcher, itemURL string, prefer []string) ([]Download, error)` (one "download page" hop)
  - `func FetchFile(ctx context.Context, f *fetch.Fetcher, dl Download, dir string) (path, format string, err error)`
  - `var ErrRefused = errors.New(...)`

- [ ] **Step 1: Failing tests**

```go
package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"w5f/internal/fetch"
)

func testFetcher() *fetch.Fetcher {
	f := fetch.New("", "test")
	f.HostGap = 0
	return f
}

func TestFindInFormatsAndOrder(t *testing.T) {
	body := page(`<a href="/files/dracula.pdf">PDF</a>
<a href="/files/dracula.epub?dl=1">EPUB</a>
<a href="/get/77" type="application/x-mobipocket-ebook">Kindle</a>
<a href="/files/archive.zip">ZIP</a>
<a href="/dl/88">Download plain text</a>
<a href="#top">top</a>`)
	ds := FindIn(body, "https://s.example/book/1", DefaultPrefer)
	var formats []string
	for _, d := range ds {
		formats = append(formats, d.Format)
	}
	if fmt.Sprint(formats) != "[epub pdf txt mobi]" {
		t.Errorf("formats = %v", formats)
	}
	if ds[0].URL != "https://s.example/files/dracula.epub?dl=1" {
		t.Errorf("url = %s", ds[0].URL)
	}
}

func TestFindDownloadsFollowsDownloadPage(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/book/1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><a href="/book/1/download">Download</a></body></html>`)
	})
	mux.HandleFunc("/book/1/download", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><a href="/f/1.epub">EPUB</a></body></html>`)
	})
	ds, err := FindDownloads(context.Background(), testFetcher(), srv.URL+"/book/1", DefaultPrefer)
	if err != nil || len(ds) != 1 || ds[0].Format != "epub" {
		t.Fatalf("ds=%+v err=%v", ds, err)
	}
}

func TestFetchFileInterstitialMagicAndErrors(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/f/a.pdf", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("source") != "download" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><meta http-equiv="refresh" content="0; url=/f/a.pdf?source=download"></head></html>`)
			return
		}
		w.Write([]byte("%PDF-1.7 body"))
	})
	mux.HandleFunc("/f/page.epub", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>Please log in</body></html>`)
	})
	mux.HandleFunc("/f/locked.epub", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	mux.HandleFunc("/f/mislabel.epub", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("%PDF-1.4 x")) })
	dir := t.TempDir()
	f := testFetcher()
	ctx := context.Background()

	p, format, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/a.pdf", Format: "pdf"}, dir)
	if err != nil || format != "pdf" {
		t.Fatalf("interstitial: %v %s", err, format)
	}
	if b, _ := os.ReadFile(p); string(b[:4]) != "%PDF" {
		t.Error("wrong content")
	}
	if _, _, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/page.epub", Format: "epub"}, dir); err == nil {
		t.Error("an HTML page must not be accepted as a book")
	}
	if _, _, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/locked.epub", Format: "epub"}, dir); !errors.Is(err, ErrRefused) {
		t.Errorf("403 err = %v", err)
	}
	if _, format, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/mislabel.epub", Format: "epub"}, dir); err != nil || format != "pdf" {
		t.Errorf("magic bytes should win: %s %v", format, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'TestFind|TestFetch' -v` → FAIL

- [ ] **Step 3: Implement** (`internal/sitecat/downloads.go`)

```go
package sitecat

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// Download is one downloadable file of a book.
type Download struct {
	URL    string
	Format string
	Label  string
}

// ErrRefused means the site refused the download (login, bot wall, limit).
var ErrRefused = errors.New("the site refused the download")

var extFormats = map[string]string{".epub": "epub", ".pdf": "pdf", ".txt": "txt", ".mobi": "mobi", ".azw3": "azw3",
	".azw": "azw3", ".fb2": "fb2", ".djvu": "djvu", ".cbz": "cbz", ".cbr": "cbr"}

var mimeFormats = map[string]string{"application/epub+zip": "epub", "application/pdf": "pdf", "text/plain": "txt",
	"application/x-mobipocket-ebook": "mobi", "application/vnd.amazon.ebook": "azw3", "image/vnd.djvu": "djvu",
	"application/x-fictionbook+xml": "fb2", "application/vnd.comicbook+zip": "cbz", "application/vnd.comicbook-rar": "cbr"}

var (
	reFormatWord = regexp.MustCompile(`(?i)\b(epub|pdf|mobi|azw3|fb2|djvu|cbz|cbr|kindle|plain text|txt)\b`)
	reDownload   = regexp.MustCompile(`(?i)download|indir|get\b|/dl/|/get/|/file`)
	reHopText    = regexp.MustCompile(`(?i)download|indir`)
)

func wordFormat(w string) string {
	switch strings.ToLower(w) {
	case "kindle":
		return "mobi"
	case "plain text", "txt":
		return "txt"
	}
	return strings.ToLower(w)
}

// FindIn lists download links on a page, ordered by prefer.
func FindIn(body []byte, pageURL string, prefer []string) []Download {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	seen := map[string]bool{}
	var out []Download
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		raw := strings.TrimSpace(a.AttrOr("href", ""))
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			return
		}
		u, err := base.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		u.Fragment = ""
		label := collapse(a.Text())
		format := extFormats[strings.ToLower(path.Ext(u.Path))]
		if format == "" {
			format = mimeFormats[strings.ToLower(strings.TrimSpace(strings.SplitN(a.AttrOr("type", ""), ";", 2)[0]))]
		}
		if format == "" {
			if m := reFormatWord.FindString(label); m != "" && (reDownload.MatchString(label) || reDownload.MatchString(u.Path)) {
				format = wordFormat(m)
			}
		}
		if format == "" || seen[u.String()] {
			return
		}
		seen[u.String()] = true
		out = append(out, Download{URL: u.String(), Format: format, Label: label})
	})
	rank := func(f string) int {
		for i, p := range prefer {
			if p == f {
				return i
			}
		}
		return len(prefer)
	}
	sortStable(out, func(a, b Download) bool { return rank(a.Format) < rank(b.Format) })
	return out
}

func sortStable(ds []Download, less func(a, b Download) bool) {
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && less(ds[j], ds[j-1]); j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
}

// FindDownloads looks on an item page and, if needed, one "download" page.
func FindDownloads(ctx context.Context, f *fetch.Fetcher, itemURL string, prefer []string) ([]Download, error) {
	u, err := url.Parse(itemURL)
	if err != nil {
		return nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	if ds := FindIn(resp.Body, resp.URL.String(), prefer); len(ds) > 0 {
		return ds, nil
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, nil
	}
	var hop *url.URL
	gq.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if !reHopText.MatchString(collapse(a.Text())) {
			return true
		}
		if h, err := resp.URL.Parse(a.AttrOr("href", "")); err == nil && sameSite(h, resp.URL) && h.String() != resp.URL.String() {
			hop = h
			return false
		}
		return true
	})
	if hop == nil {
		return nil, nil
	}
	resp2, err := f.Get(ctx, hop, fetch.Options{})
	if err != nil {
		return nil, err
	}
	return FindIn(resp2.Body, resp2.URL.String(), prefer), nil
}

var reRefresh = regexp.MustCompile(`(?i)<meta[^>]+http-equiv=["']?refresh["']?[^>]*content=["']?\s*\d+\s*;\s*url=([^"'>]+)`)

// magicFormat identifies a file from its first bytes ("" if unknown).
func magicFormat(head []byte, fallback string) string {
	switch {
	case bytes.HasPrefix(head, []byte("%PDF")):
		return "pdf"
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		if fallback == "cbz" {
			return "cbz"
		}
		return "epub"
	case bytes.HasPrefix(head, []byte("AT&TFORM")):
		return "djvu"
	case bytes.HasPrefix(head, []byte("Rar!")):
		return "cbr"
	case len(head) > 68 && string(head[60:68]) == "BOOKMOBI":
		if fallback == "azw3" {
			return "azw3"
		}
		return "mobi"
	case bytes.Contains(head, []byte("<FictionBook")):
		return "fb2"
	}
	return ""
}

func looksHTML(head []byte, ct string) bool {
	if strings.Contains(strings.ToLower(ct), "html") {
		return true
	}
	h := strings.ToLower(string(bytes.TrimSpace(head)))
	return strings.HasPrefix(h, "<!doctype html") || strings.HasPrefix(h, "<html")
}

// FetchFile downloads a book into dir (as a hidden temp file) and returns its
// path and the verified format. HTML "download started" pages are followed
// through their meta refresh (at most twice); any other HTML is an error.
func FetchFile(ctx context.Context, f *fetch.Fetcher, dl Download, dir string) (string, string, error) {
	target := dl.URL
	client := &http.Client{Timeout: 5 * time.Minute}
	for hop := 0; hop < 3; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("User-Agent", f.UserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return "", "", err
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
			resp.Body.Close()
			return "", "", fmt.Errorf("%w (HTTP %d) — it may need a login or block readers", ErrRefused, resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return "", "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}
		br := bufio.NewReader(io.LimitReader(resp.Body, 100<<20+1))
		head, _ := br.Peek(512)
		if looksHTML(head, resp.Header.Get("Content-Type")) {
			page, _ := io.ReadAll(io.LimitReader(br, 1<<20))
			resp.Body.Close()
			m := reRefresh.FindSubmatch(append(append([]byte{}, head...), page...))
			if m == nil {
				return "", "", errors.New("the site served a web page, not a book file")
			}
			next, err := resp.Request.URL.Parse(strings.TrimSpace(string(m[1])))
			if err != nil {
				return "", "", err
			}
			target = next.String()
			continue
		}
		format := magicFormat(head, dl.Format)
		if format == "" {
			format = dl.Format
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			resp.Body.Close()
			return "", "", err
		}
		tmp, err := os.CreateTemp(dir, ".download-*."+format)
		if err != nil {
			resp.Body.Close()
			return "", "", err
		}
		n, err := io.Copy(tmp, br)
		resp.Body.Close()
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
			return "", "", err
		}
		if n > 100<<20 {
			os.Remove(tmp.Name())
			return "", "", errors.New("the file is larger than 100 MB")
		}
		return tmp.Name(), format, nil
	}
	return "", "", errors.New("too many redirects through download pages")
}
```

- [ ] **Step 4: Run** `go test ./internal/sitecat -run 'TestFind|TestFetch' -v` → PASS

- [ ] **Step 5: Format** `gofmt -w . && go vet ./internal/sitecat`

---

### Task 5: Search discovery and full search (`probe.go`, `search.go`)

**Files:**
- Create: `internal/sitecat/discover.go` (search-method discovery, pure-ish)
- Create: `internal/sitecat/search.go` (query parsing, URL building, full search)
- Create: `internal/sitecat/probe.go` (check report)
- Test: `internal/sitecat/search_test.go`, `internal/sitecat/probe_test.go`

**Interfaces:**
- Consumes: `Profile`, `SearchSpec`, `Layout` (Task 2); `Extract`, `NextPage`, `Result` (Task 3); `FindDownloads`, `Download` (Task 4)
- Produces:
  - `func DiscoverSearch(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) (SearchSpec, string)` — spec + human description
  - `type Query struct { Field, Words string }`, `func ParseQuery(s string) Query`
  - `func BuildURL(p Profile, q Query) (u string, fieldIgnored bool)`
  - `type Progress func(page, found int)`
  - `type Found struct { Results []Result; Pages int; Capped bool; FieldIgnored bool }`
  - `func Search(ctx context.Context, f *fetch.Fetcher, p *Profile, raw string, progress Progress) (*Found, error)` (may update `p.Layout`)
  - `type Finding struct { OK bool; Text string }`
  - `type Report struct { Profile Profile; Findings []Finding; Sample []Result; Formats []string; CanAdd bool }`
  - `func Probe(ctx context.Context, f *fetch.Fetcher, raw, testWord string) (*Report, error)`
  - `var CurrentProgress atomic.Value` (string) — read by the TUI status line

- [ ] **Step 1: Failing tests** (`internal/sitecat/search_test.go`)

```go
package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// fakeSite serves 10 result pages of 5 books each for "dracula"; page 11+
// repeats page 10 (a common site behaviour).
func fakeSite(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><head><title>Fake Library - Home</title></head><body>
<form action="/search" method="get"><input type="search" name="q">
<select name="in"><option value="all">All</option><option value="author">Author</option><option value="title">Title</option></select>
<input type="hidden" name="lang" value="en"></form></body></html>`)
		case "/search":
			p, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if p < 1 {
				p = 1
			}
			if p > 10 {
				p = 10
			}
			var b strings.Builder
			b.WriteString(`<html><body><ul class="results">`)
			for i := 0; i < 5; i++ {
				n := (p-1)*5 + i
				fmt.Fprintf(&b, `<li class="hit"><a href="/book/%d">Dracula volume %d</a> by Bram Stoker</li>`, n, n)
			}
			b.WriteString(`</ul>`)
			fmt.Fprintf(&b, `<a href="/search?q=%s&in=%s&lang=en&page=%d">Next ›</a></body></html>`,
				r.URL.Query().Get("q"), r.URL.Query().Get("in"), p+1)
			fmt.Fprint(w, b.String())
		}
	}))
}

func TestParseQueryAndBuildURL(t *testing.T) {
	if q := ParseQuery("author:Bram Stoker"); q.Field != "author" || q.Words != "Bram Stoker" {
		t.Errorf("%+v", q)
	}
	if q := ParseQuery("Dracula"); q.Field != "" || q.Words != "Dracula" {
		t.Errorf("%+v", q)
	}
	p := Profile{Search: SearchSpec{Template: "https://s.example/find?q={q}"}}
	u, ignored := BuildURL(p, ParseQuery("title:Dracula's Guest"))
	if u != "https://s.example/find?q=Dracula%27s+Guest" || !ignored {
		t.Errorf("no fields: %s %v", u, ignored)
	}
	p.Search.Fields = map[string]string{"title": "https://s.example/find?q={q}&in=title"}
	if u, ignored := BuildURL(p, ParseQuery("title:Dracula")); u != "https://s.example/find?q=Dracula&in=title" || ignored {
		t.Errorf("field: %s %v", u, ignored)
	}
}

func TestFullSearchAcrossPages(t *testing.T) {
	srv := fakeSite(t)
	defer srv.Close()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL+"/", "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	if p.Search.Fields["author"] == "" || p.Search.Fields["title"] == "" {
		t.Errorf("fields not learned: %+v", p.Search)
	}
	pages := 0
	got, err := Search(context.Background(), f, &p, "dracula", func(page, found int) { pages = page })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 50 || got.Pages != 10 || pages != 10 {
		t.Errorf("results=%d pages=%d progress=%d", len(got.Results), got.Pages, pages)
	}
	// Stops on the repeated last page rather than looping to max_pages.
	p.Search.MaxPages = 30
	got, _ = Search(context.Background(), f, &p, "dracula", nil)
	if len(got.Results) != 50 || got.Pages > 11 || got.Capped {
		t.Errorf("repeat stop: results=%d pages=%d capped=%v", len(got.Results), got.Pages, got.Capped)
	}
	// Caps.
	p.Search.MaxPages = 3
	got, _ = Search(context.Background(), f, &p, "dracula", nil)
	if len(got.Results) != 15 || !got.Capped {
		t.Errorf("page cap: %d capped=%v", len(got.Results), got.Capped)
	}
	p.Search.MaxPages, p.Search.MaxResults = 10, 12
	got, _ = Search(context.Background(), f, &p, "dracula", nil)
	if len(got.Results) != 12 || !got.Capped {
		t.Errorf("result cap: %d", len(got.Results))
	}
	// Field routing.
	p.Search.MaxResults = 300
	got, _ = Search(context.Background(), f, &p, "author:Stoker", nil)
	if got.FieldIgnored || len(got.Results) == 0 {
		t.Errorf("author field: %+v", got)
	}
}
```

`internal/sitecat/probe_test.go`:

```go
package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverSearchVariants(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/osd.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
<Url type="text/html" template="`+srv.URL+`/s?query={searchTerms}&amp;page={startPage?}"/></OpenSearchDescription>`)
	})
	mux.HandleFunc("/opds", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Catalog</title>
<link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"/></feed>`)
	})
	ctx := context.Background()
	f := testFetcher()
	cases := []struct{ name, body, kind, tmpl string }{
		{"opds", `<head><link rel="alternate" type="application/atom+xml;profile=opds-catalog" href="/opds"></head>`, "opds", srv.URL + "/s?query={q}"},
		{"opensearch", `<head><link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"></head>`, "opensearch", srv.URL + "/s?query={q}"},
		{"form", `<nav><form action="/find"><input name="term"><input type="hidden" name="x" value="1"></form></nav>`, "form", srv.URL + "/find?term={q}&x=1"},
		{"wordpress", `<head><meta name="generator" content="WordPress 6.6"></head>`, "wordpress", srv.URL + "/?s={q}"},
		{"post-only", `<form action="/p" method="post"><input type="search" name="q"></form>`, "none", ""},
	}
	for _, c := range cases {
		spec, desc := DiscoverSearch(ctx, f, []byte("<html>"+c.body+"<body></body></html>"), srv.URL+"/")
		if spec.Kind != c.kind || spec.Template != c.tmpl {
			t.Errorf("%s: %+v (%s)", c.name, spec, desc)
		}
		if c.name == "post-only" && !strings.Contains(desc, "POST") {
			t.Errorf("post-only should be reported: %s", desc)
		}
	}
}

func TestProbeBrowseOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Small Shelf</title></head><body><ul class="books">
<li><a href="/b/1">The King in Yellow</a> Chambers</li><li><a href="/b/2">The Great God Pan</a> Machen</li>
<li><a href="/b/3">The House on the Borderland</a> Hodgson</li></ul></body></html>`)
	}))
	defer srv.Close()
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "none" || len(rep.Sample) != 3 {
		t.Fatalf("browse-only: %v %+v", err, rep)
	}
}

func TestProbeReportsJavaScriptSite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>JS Books</title></head><body><form action="/search"><input name="q"></form>
<div id="app"></div><noscript>Please enable JavaScript</noscript><script src="/bundle.js"></script></body></html>`)
	}))
	defer srv.Close()
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "history")
	if err != nil {
		t.Fatal(err)
	}
	if rep.CanAdd {
		t.Error("a JavaScript-only site must not be addable")
	}
	text := ""
	for _, fi := range rep.Findings {
		text += fi.Text + "\n"
	}
	if !strings.Contains(text, "JavaScript") {
		t.Errorf("findings:\n%s", text)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'TestParse|TestFullSearch|TestDiscover|TestProbe' -v` → FAIL

- [ ] **Step 3: Implement discovery** (`internal/sitecat/discover.go`)

```go
package sitecat

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

var (
	reAuthorWord = regexp.MustCompile(`(?i)author|creator|writer|yazar`)
	reTitleWord  = regexp.MustCompile(`(?i)title|book|eser|kitap|name`)
	generalNames = map[string]bool{"q": true, "query": true, "s": true, "search": true, "term": true,
		"keywords": true, "keyword": true, "text": true, "k": true}
	reOptParam = regexp.MustCompile(`\{[^}]*\?\}`)
)

// DiscoverSearch finds how a site is searched. The description is for the
// check page.
func DiscoverSearch(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) (SearchSpec, string) {
	base, _ := url.Parse(pageURL)
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return SearchSpec{Kind: "none"}, "the page could not be read"
	}
	// 1. OpenSearch description.
	if h, ok := gq.Find(`link[rel~="search"][type="application/opensearchdescription+xml"]`).First().Attr("href"); ok {
		if t := openSearchTemplate(ctx, f, base, h); t != "" {
			return SearchSpec{Kind: "opensearch", Template: t}, "OpenSearch (" + t + ")"
		}
	}
	// 2. OPDS feed with a search link.
	if h, ok := gq.Find(`link[type*="opds"]`).First().Attr("href"); ok {
		if t := opdsTemplate(ctx, f, base, h); t != "" {
			return SearchSpec{Kind: "opds", Template: t}, "OPDS catalog search (" + t + ")"
		}
	}
	// 3. HTML forms.
	if spec, desc, ok := formSearch(gq, base); ok {
		return spec, desc
	}
	postOnly := false
	gq.Find("form").Each(func(_ int, f *goquery.Selection) {
		if strings.EqualFold(strings.TrimSpace(f.AttrOr("method", "")), "post") {
			postOnly = true
		}
	})
	// 4. WordPress.
	if strings.Contains(strings.ToLower(gq.Find(`meta[name="generator"]`).AttrOr("content", "")), "wordpress") {
		u := *base
		u.Path, u.RawQuery = "/", "s={q}"
		return SearchSpec{Kind: "wordpress", Template: u.String()}, "WordPress search (?s=)"
	}
	if postOnly {
		return SearchSpec{Kind: "none"}, "the site's search form uses POST, which W5F does not submit"
	}
	return SearchSpec{Kind: "none"}, "no search box, OpenSearch or OPDS link found"
}

func openSearchTemplate(ctx context.Context, f *fetch.Fetcher, base *url.URL, href string) string {
	u, err := base.Parse(href)
	if err != nil {
		return ""
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return ""
	}
	var d struct {
		URLs []struct {
			Type     string `xml:"type,attr"`
			Template string `xml:"template,attr"`
		} `xml:"Url"`
	}
	if xml.Unmarshal(resp.Body, &d) != nil {
		return ""
	}
	for _, x := range d.URLs {
		if strings.HasPrefix(x.Type, "text/html") || strings.Contains(x.Type, "atom") {
			return cleanTemplate(x.Template)
		}
	}
	return ""
}

// cleanTemplate turns an OpenSearch template into a {q} template: optional
// parameters are dropped, common required ones get their first value.
func cleanTemplate(t string) string {
	t = strings.ReplaceAll(t, "{searchTerms}", "{q}")
	t = reOptParam.ReplaceAllString(t, "")
	for k, v := range map[string]string{"{startPage}": "1", "{startIndex}": "1", "{count}": "20", "{language}": "*", "{inputEncoding}": "UTF-8", "{outputEncoding}": "UTF-8"} {
		t = strings.ReplaceAll(t, k, v)
	}
	u, err := url.Parse(t)
	if err != nil {
		return t
	}
	q := u.Query()
	for k, vs := range q {
		if len(vs) == 0 || vs[0] == "" {
			q.Del(k)
		}
	}
	u.RawQuery = strings.ReplaceAll(q.Encode(), "%7Bq%7D", "{q}")
	return u.String()
}

func opdsTemplate(ctx context.Context, f *fetch.Fetcher, base *url.URL, href string) string {
	u, err := base.Parse(href)
	if err != nil {
		return ""
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return ""
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return ""
	}
	link := gq.Find(`link[rel="search"]`).First()
	h, ok := link.Attr("href")
	if !ok {
		return ""
	}
	if strings.Contains(h, "{searchTerms}") {
		su, err := resp.URL.Parse(h)
		if err != nil {
			return ""
		}
		return cleanTemplate(strings.ReplaceAll(su.String(), "%7BsearchTerms%7D", "{searchTerms}"))
	}
	return openSearchTemplate(ctx, f, resp.URL, h)
}

type formInput struct {
	name, typ, value, hint string
}

// formSearch scores GET forms and builds general and field templates.
func formSearch(gq *goquery.Document, base *url.URL) (SearchSpec, string, bool) {
	bestScore := 0
	var best SearchSpec
	fields := map[string]string{}
	gq.Find("form").Each(func(_ int, form *goquery.Selection) {
		method := strings.ToLower(strings.TrimSpace(form.AttrOr("method", "get")))
		if method != "get" && method != "" {
			return
		}
		action, err := base.Parse(form.AttrOr("action", ""))
		if err != nil {
			return
		}
		var inputs []formInput
		form.Find("input[name]").Each(func(_ int, in *goquery.Selection) {
			typ := strings.ToLower(in.AttrOr("type", "text"))
			inputs = append(inputs, formInput{name: in.AttrOr("name", ""), typ: typ, value: in.AttrOr("value", ""),
				hint: strings.ToLower(in.AttrOr("id", "") + " " + in.AttrOr("placeholder", "") + " " + in.AttrOr("aria-label", ""))})
		})
		var textIdx []int
		for i, in := range inputs {
			if in.typ == "text" || in.typ == "search" || in.typ == "" {
				textIdx = append(textIdx, i)
			}
		}
		if len(textIdx) == 0 {
			return
		}
		score := 0
		general := textIdx[0]
		for _, i := range textIdx {
			in := inputs[i]
			if in.typ == "search" {
				score += 3
				general = i
			}
			if generalNames[strings.ToLower(in.name)] {
				score += 2
				general = i
			}
		}
		meta := strings.ToLower(form.AttrOr("action", "") + " " + form.AttrOr("id", "") + " " + form.AttrOr("class", "") + " " + form.AttrOr("role", ""))
		if strings.Contains(meta, "search") {
			score += 2
		}
		if form.ParentsFiltered("nav,header").Length() > 0 {
			score++
		}
		build := func(textInput int, set map[string]string) string {
			parts := []string{}
			for i, in := range inputs {
				switch {
				case i == textInput:
					parts = append(parts, url.QueryEscape(in.name)+"={q}")
				case in.typ == "hidden":
					parts = append(parts, url.QueryEscape(in.name)+"="+url.QueryEscape(in.value))
				}
			}
			for k, v := range set {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
			u := *action
			u.RawQuery = strings.Join(parts, "&")
			return u.String()
		}
		// Field choices: a <select> with author/title options…
		selFields := map[string]string{}
		var selName, allValue string
		form.Find("select[name]").Each(func(_ int, sel *goquery.Selection) {
			sel.Find("option").Each(func(_ int, o *goquery.Selection) {
				v, t := o.AttrOr("value", collapse(o.Text())), collapse(o.Text())
				switch {
				case reAuthorWord.MatchString(v + " " + t):
					selFields["author"], selName = v, sel.AttrOr("name", "")
				case reTitleWord.MatchString(v + " " + t):
					selFields["title"], selName = v, sel.AttrOr("name", "")
				case allValue == "":
					allValue = v
				}
			})
		})
		spec := SearchSpec{Kind: "form"}
		localFields := map[string]string{}
		if selName != "" {
			set := map[string]string{}
			if allValue != "" {
				set[selName] = allValue
			}
			spec.Template = build(general, set)
			for field, v := range selFields {
				localFields[field] = build(general, map[string]string{selName: v})
			}
		} else {
			spec.Template = build(general, nil)
			// …or separate author/title inputs in the same form.
			for _, i := range textIdx {
				label := inputs[i].name + " " + inputs[i].hint
				if reAuthorWord.MatchString(label) {
					localFields["author"] = build(i, nil)
				} else if reTitleWord.MatchString(label) && !generalNames[strings.ToLower(inputs[i].name)] {
					localFields["title"] = build(i, nil)
				}
			}
		}
		// Separate author/title forms on the page contribute fields too.
		if len(textIdx) == 1 {
			label := inputs[textIdx[0]].name + " " + inputs[textIdx[0]].hint + " " + meta
			if reAuthorWord.MatchString(label) {
				fields["author"] = spec.Template
			} else if reTitleWord.MatchString(label) && !generalNames[strings.ToLower(inputs[textIdx[0]].name)] {
				fields["title"] = spec.Template
			}
		}
		if score > bestScore || (score == bestScore && best.Template == "") {
			bestScore, best = score, spec
			if len(localFields) > 0 {
				best.Fields = localFields
			}
		}
	})
	if best.Template == "" {
		return SearchSpec{}, "", false
	}
	for k, v := range fields {
		if best.Fields == nil {
			best.Fields = map[string]string{}
		}
		if _, ok := best.Fields[k]; !ok && v != best.Template {
			best.Fields[k] = v
		}
	}
	desc := "search form (" + best.Template + ")"
	if len(best.Fields) > 0 {
		desc += " with fields:"
		for _, k := range []string{"author", "title"} {
			if best.Fields[k] != "" {
				desc += " " + k
			}
		}
	}
	return best, desc, true
}
```

- [ ] **Step 4: Implement full search** (`internal/sitecat/search.go`)

```go
package sitecat

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// Query is a parsed search: an optional field and the words.
type Query struct {
	Field string // "", "author" or "title"
	Words string
}

// ParseQuery reads "author:…" / "title:…" prefixes.
func ParseQuery(s string) Query {
	s = strings.TrimSpace(s)
	for _, f := range []string{"author", "title"} {
		if strings.HasPrefix(strings.ToLower(s), f+":") {
			return Query{Field: f, Words: strings.TrimSpace(s[len(f)+1:])}
		}
	}
	return Query{Words: s}
}

// BuildURL fills the right template. fieldIgnored reports a field prefix the
// site cannot honour (the words are then searched generally).
func BuildURL(p Profile, q Query) (string, bool) {
	t := p.Search.Template
	ignored := false
	if q.Field != "" {
		if ft := p.Search.Fields[q.Field]; ft != "" {
			t = ft
		} else {
			ignored = true
		}
	}
	return strings.ReplaceAll(t, "{q}", url.QueryEscape(q.Words)), ignored
}

// Progress reports search progress (page being read, results so far).
type Progress func(page, found int)

// CurrentProgress holds a short status line for the UI while a search runs.
var CurrentProgress atomic.Value

// Found is a complete search result.
type Found struct {
	Results      []Result
	Pages        int
	Capped       bool
	FieldIgnored bool
}

func normURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Fragment = ""
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// Search runs a full search: it follows the site's result pages until they
// end, repeat, stop adding results, or a cap is reached.
func Search(ctx context.Context, f *fetch.Fetcher, p *Profile, raw string, progress Progress) (*Found, error) {
	p.ApplyDefaults()
	if p.Search.Kind == "none" || p.Search.Template == "" {
		return nil, errors.New("this catalog has no search; browse the site instead")
	}
	q := ParseQuery(raw)
	if q.Words == "" {
		return nil, errors.New("type some words to search")
	}
	next, ignored := BuildURL(*p, q)
	out := &Found{FieldIgnored: ignored}
	seenURL := map[string]bool{}
	seenItem := map[string]bool{}
	defer CurrentProgress.Store("")
	for next != "" && !seenURL[next] {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if out.Pages >= p.Search.MaxPages {
			out.Capped = true
			break
		}
		seenURL[next] = true
		u, err := url.Parse(next)
		if err != nil {
			break
		}
		resp, err := f.Get(ctx, u, fetch.Options{})
		if err != nil {
			if out.Pages == 0 {
				return nil, err
			}
			break
		}
		out.Pages++
		var rs []Result
		if p.Search.Kind == "opds" {
			rs = atomEntries(resp.Body, resp.URL.String())
		} else {
			var nl Layout
			rs, nl = Extract(resp.Body, resp.URL.String(), p.Layout)
			p.Layout = nl
		}
		added := 0
		for _, r := range rs {
			key := normURL(r.URL)
			alt := strings.ToLower(r.Title + "|" + r.Extra)
			if key == "" || seenItem[key] || seenItem[alt] {
				continue
			}
			seenItem[key], seenItem[alt] = true, true
			out.Results = append(out.Results, r)
			added++
			if len(out.Results) >= p.Search.MaxResults {
				out.Capped = true
				break
			}
		}
		if added == 0 {
			if out.Pages > 1 {
				out.Pages-- // a repeated last page does not count
			}
			break
		}
		CurrentProgress.Store("searching page " + itoa(out.Pages) + " · " + itoa(len(out.Results)) + " found")
		if progress != nil {
			progress(out.Pages, len(out.Results))
		}
		if out.Capped {
			break
		}
		if p.Search.Kind == "opds" {
			next = atomNext(resp.Body, resp.URL.String())
		} else {
			next = NextPage(resp.Body, resp.URL.String())
		}
	}
	return out, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// atomEntries reads OPDS/Atom search results.
func atomEntries(body []byte, pageURL string) []Result {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	var out []Result
	gq.Find("entry").Each(func(_ int, e *goquery.Selection) {
		title := collapse(e.Find("title").First().Text())
		href := ""
		e.Find("link").EachWithBreak(func(_ int, l *goquery.Selection) bool {
			rel := l.AttrOr("rel", "")
			if strings.Contains(rel, "acquisition") || rel == "alternate" || rel == "" {
				href = l.AttrOr("href", "")
				return !strings.Contains(rel, "acquisition")
			}
			return true
		})
		if u, err := base.Parse(href); err == nil && title != "" && href != "" {
			out = append(out, Result{Title: title, URL: u.String(), Extra: collapse(e.Find("author name").First().Text())})
		}
	})
	return out
}

func atomNext(body []byte, pageURL string) string {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	base, _ := url.Parse(pageURL)
	if h, ok := gq.Find(`link[rel="next"]`).First().Attr("href"); ok {
		if u, err := base.Parse(h); err == nil {
			return u.String()
		}
	}
	return ""
}
```

- [ ] **Step 5: Implement the check report** (`internal/sitecat/probe.go`)

```go
package sitecat

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// Finding is one ✓/✗ line on the check page.
type Finding struct {
	OK   bool
	Text string
}

// Report is the outcome of inspecting a site.
type Report struct {
	Profile  Profile
	Findings []Finding
	Sample   []Result
	Formats  []string
	CanAdd   bool
}

func (r *Report) add(ok bool, text string) { r.Findings = append(r.Findings, Finding{ok, text}) }

// Probe inspects a site: search method, result layout (with testWord) and
// the download formats of the first result.
func Probe(ctx context.Context, f *fetch.Fetcher, raw, testWord string) (*Report, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if testWord == "" {
		testWord = "history"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	rep := &Report{}
	home := resp.URL.String()
	p := Profile{Home: home, Added: time.Now().UTC(), Name: siteName(resp.Body, resp.URL)}
	p.ApplyDefaults()
	if wall := botWall(resp.Body); wall != "" {
		rep.add(false, wall)
		rep.Profile = p
		return rep, nil
	}
	spec, desc := DiscoverSearch(ctx, f, resp.Body, home)
	spec.MaxPages, spec.MaxResults = p.Search.MaxPages, p.Search.MaxResults
	p.Search = spec
	rep.add(spec.Kind != "none", "search: "+desc)
	if spec.Kind == "none" {
		// Browse-only: the home page itself carries a book list.
		if l, rs := LearnLayout(resp.Body, home); len(rs) > 0 {
			p.Layout = l
			rep.add(true, "browse-only: the front page lists "+itoa(len(rs))+" items; the catalog can be browsed, not searched")
			rep.Sample, rep.CanAdd = rs[:min(5, len(rs))], true
		}
		rep.Profile = p
		return rep, nil
	}
	first, _ := BuildURL(p, Query{Words: testWord})
	fu, _ := url.Parse(first)
	sresp, err := f.Get(ctx, fu, fetch.Options{})
	if err != nil {
		rep.add(false, "test search for “"+testWord+"” failed: "+err.Error())
		rep.Profile = p
		return rep, nil
	}
	var rs []Result
	if spec.Kind == "opds" {
		rs = atomEntries(sresp.Body, sresp.URL.String())
	} else {
		p.Layout, rs = LearnLayout(sresp.Body, sresp.URL.String())
	}
	switch {
	case len(rs) > 0:
		where := ""
		if p.Layout.Item != "" {
			where = " in “" + p.Layout.Parent + " > " + p.Layout.Item + "”"
		}
		rep.add(true, "results: "+itoa(len(rs))+" items for “"+testWord+"”"+where)
	case needsJS(sresp.Body):
		rep.add(false, "results: this site builds its results with JavaScript; W5F cannot search it")
	default:
		rep.add(false, "results: no result list found for “"+testWord+"” — try another test word")
	}
	if len(rs) > 5 {
		rep.Sample = rs[:5]
	} else {
		rep.Sample = rs
	}
	if NextPage(sresp.Body, sresp.URL.String()) != "" {
		rep.add(true, "pages: more result pages found — full search follows them (up to "+itoa(p.Search.MaxPages)+")")
	}
	if len(rs) > 0 {
		ds, err := FindDownloads(ctx, f, rs[0].URL, p.Download.Prefer)
		seen := map[string]bool{}
		for _, d := range ds {
			if !seen[d.Format] {
				seen[d.Format] = true
				rep.Formats = append(rep.Formats, d.Format)
			}
		}
		switch {
		case err != nil:
			rep.add(false, "downloads: could not open the first result ("+err.Error()+")")
		case len(ds) == 0:
			rep.add(false, "downloads: no book files on the first result (other books may still have them)")
		default:
			rep.add(true, "downloads: "+strings.ToUpper(strings.Join(rep.Formats, ", "))+" on the first result")
		}
	}
	rep.CanAdd = len(rs) > 0
	rep.Profile = p
	return rep, nil
}

func siteName(body []byte, u *url.URL) string {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err == nil {
		t := collapse(gq.Find("title").First().Text())
		for _, sep := range []string{" | ", " - ", " — ", " · ", ": "} {
			if i := strings.Index(t, sep); i > 0 {
				t = t[:i]
			}
		}
		if t != "" && len([]rune(t)) <= 50 {
			return t
		}
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// botWall recognises challenge and captcha pages.
func botWall(body []byte) string {
	l := strings.ToLower(string(body))
	switch {
	case strings.Contains(l, "cf-challenge") || strings.Contains(l, "just a moment...") || strings.Contains(l, "name=\"js_challenge\""):
		return "the site shows a bot check; W5F does not get past it"
	case strings.Contains(l, "g-recaptcha") || strings.Contains(l, "h-captcha") || strings.Contains(l, "captcha"):
		return "the site asks for a captcha; W5F does not solve captchas"
	}
	return ""
}

func needsJS(body []byte) bool {
	l := strings.ToLower(string(body))
	return strings.Contains(l, "<noscript") || strings.Count(l, "<script") >= 3
}
```

- [ ] **Step 6: Run** `go test ./internal/sitecat -v` → PASS (all sitecat tests)

- [ ] **Step 7: Format** `gofmt -w . && go vet ./internal/sitecat`

---

### Task 6: Reader pages and wiring (`docs.go`, books/source/tui/cmd)

**Files:**
- Create: `internal/sitecat/docs.go`
- Modify: `internal/books/docs.go` (Env.Catalogs hook; "Find books" lists site catalogs + add/manage hints)
- Modify: `internal/source/source.go` (route `w5f:catalog…`; Resolve `catalog-add`, `cat`, `catalogs`; friendly errors)
- Modify: `internal/tui/tui.go` (loading text, progress tick, esc cancels a running load)
- Test: `internal/sitecat/docs_test.go`

**Interfaces:**
- Consumes: everything above; `books.AddFile`, `books.LibraryDir`
- Produces:
  - `type Env struct { Fetcher *fetch.Fetcher; DB *store.DB; Path string; OpenBook func(ctx context.Context, b store.Book) (*doc.Document, error) }`
  - `func IsTarget(target string) bool`, `func Route(ctx context.Context, target string, env Env) (*doc.Document, error)`
  - `books.Env.Catalogs func() []books.CatalogLink`, `type CatalogLink struct { ID, Name string }`

Addresses: `w5f:catalog/check?url=&w=`, `w5f:catalog/add?url=&w=`, `w5f:catalogs`, `w5f:catalog/<id>`, `w5f:catalog/<id>?q=`, `w5f:catalog/<id>/item?u=&t=`, `w5f:catalog/<id>/get?u=&f=&t=&src=`, `w5f:catalog/<id>/recheck`, `w5f:catalog/<id>/remove`.

- [ ] **Step 1: Failing end-to-end test** (`internal/sitecat/docs_test.go`)

```go
package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		case doc.Notice:
			b.WriteString("[N] " + x.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func TestAddSearchGetEndToEnd(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("W5F_BOOKS", lib)
	mux := http.NewServeMux()
	site := fakeSite(t) // search pages from search_test.go
	defer site.Close()
	mux.Handle("/", site.Config.Handler)
	mux.HandleFunc("/book/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><h1>Dracula</h1><a href="%s.pdf">PDF</a></body></html>`, r.URL.Path)
	})
	mux.HandleFunc("/book/0.pdf", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("%PDF-1.4 dracula")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	opened := ""
	env := Env{Fetcher: testFetcher(), DB: db, Path: filepath.Join(t.TempDir(), "catalogs.toml"),
		OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) {
			opened = b.Path
			return &doc.Document{Title: b.Title}, nil
		}}
	ctx := context.Background()
	q := url.Values{"url": {srv.URL + "/"}, "w": {"dracula"}}.Encode()

	check, err := Route(ctx, "w5f:catalog/check?"+q, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(check), "✓ search") || !strings.Contains(flat(check), "add this catalog") {
		t.Fatalf("check page:\n%s", flat(check))
	}
	if _, err := Route(ctx, "w5f:catalog/add?"+q, env); err != nil {
		t.Fatal(err)
	}
	ps, _ := LoadAll(env.Path)
	if len(ps) != 1 {
		t.Fatalf("profiles = %d", len(ps))
	}
	res, err := Route(ctx, "w5f:catalog/"+ps[0].ID+"?q=dracula", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(res), "50 results from 10 pages") {
		t.Errorf("results page:\n%s", flat(res))
	}
	item, err := Route(ctx, "w5f:catalog/"+ps[0].ID+"/item?"+url.Values{"u": {srv.URL + "/book/0"}, "t": {"Dracula volume 0"}}.Encode(), env)
	if err != nil || !strings.Contains(flat(item), "PDF") {
		t.Fatalf("item page: %v\n%s", err, flat(item))
	}
	var get string
	for _, l := range item.Links {
		if strings.Contains(l.Href, "/get?") {
			get = l.Href
		}
	}
	if _, err := Route(ctx, get, env); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(opened) != lib || filepath.Ext(opened) != ".pdf" {
		t.Errorf("opened %q", opened)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run TestAddSearchGet -v` → FAIL (undefined Env/Route)

- [ ] **Step 3: Implement** (`internal/sitecat/docs.go`)

```go
package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"w5f/internal/books"
	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Env carries what the catalog pages need.
type Env struct {
	Fetcher  *fetch.Fetcher
	DB       *store.DB
	Path     string // catalogs.toml
	OpenBook func(ctx context.Context, b store.Book) (*doc.Document, error)
}

// IsTarget reports whether target is a site-catalog address.
func IsTarget(target string) bool { return strings.HasPrefix(target, "w5f:catalog") }

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func para(spans ...doc.Span) doc.Paragraph { return doc.Paragraph{Text: spans} }

// Route builds the page for a catalog address.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(u.Opaque, "/")
	q := u.Query()
	ps, err := LoadAll(env.Path)
	if err != nil {
		return nil, err
	}
	switch {
	case p == "catalogs":
		return manageDoc(ps, ""), nil
	case p == "catalog/check":
		rep, err := Probe(ctx, env.Fetcher, q.Get("url"), q.Get("w"))
		if err != nil {
			return nil, err
		}
		return checkDoc(rep, q.Get("url"), q.Get("w")), nil
	case p == "catalog/add":
		rep, err := Probe(ctx, env.Fetcher, q.Get("url"), q.Get("w"))
		if err != nil {
			return nil, err
		}
		if !rep.CanAdd {
			return checkDoc(rep, q.Get("url"), q.Get("w")), nil
		}
		pr := rep.Profile
		pr.ID = IDFor(pr.Home, ps)
		ps = append(ps, pr)
		if err := SaveAll(env.Path, ps); err != nil {
			return nil, err
		}
		return manageDoc(ps, "Added “"+pr.Name+"”. Search it with g → cat "+pr.ID+" <words>."), nil
	}
	rest := strings.TrimPrefix(p, "catalog/")
	id, action, _ := strings.Cut(rest, "/")
	i := Find(ps, id)
	if i < 0 {
		return nil, errors.New("unknown catalog " + id + " (see g → catalogs)")
	}
	pr := &ps[i]
	switch action {
	case "":
		if q.Get("q") == "" && pr.Search.Kind == "none" {
			hu, err := url.Parse(pr.Home)
			if err != nil {
				return nil, err
			}
			resp, err := env.Fetcher.Get(ctx, hu, fetch.Options{})
			if err != nil {
				return nil, err
			}
			rs, nl := Extract(resp.Body, resp.URL.String(), pr.Layout)
			pr.Layout = nl
			_ = SaveAll(env.Path, ps)
			return resultsDoc(env, *pr, "front page", &Found{Results: rs, Pages: 1}, nil), nil
		}
		if q.Get("q") == "" {
			return catalogDoc(*pr), nil
		}
		found, err := Search(ctx, env.Fetcher, pr, q.Get("q"), nil)
		if err != nil && (found == nil || errors.Is(err, context.Canceled)) {
			return nil, err
		}
		_ = SaveAll(env.Path, ps) // keep a re-learned layout
		return resultsDoc(env, *pr, q.Get("q"), found, err), nil
	case "item":
		ds, err := FindDownloads(ctx, env.Fetcher, q.Get("u"), pr.Download.Prefer)
		if err != nil {
			return nil, err
		}
		return itemDoc(env, *pr, q.Get("u"), q.Get("t"), ds), nil
	case "get":
		src := "site:" + pr.ID + ":" + q.Get("src")
		if b, ok := env.DB.BookBySource(src); ok {
			return env.OpenBook(ctx, b)
		}
		tmp, format, err := FetchFile(ctx, env.Fetcher, Download{URL: q.Get("u"), Format: q.Get("f")}, books.LibraryDir())
		if err != nil {
			return nil, err
		}
		b, err := books.AddFile(env.DB, tmp, format, q.Get("t"), "", src)
		if err != nil {
			return nil, err
		}
		return env.OpenBook(ctx, b)
	case "recheck":
		rep, err := Probe(ctx, env.Fetcher, pr.Home, "")
		if err != nil {
			return nil, err
		}
		if rep.CanAdd {
			keep := *pr
			*pr = rep.Profile
			pr.ID, pr.Name, pr.Added = keep.ID, keep.Name, keep.Added
			pr.Search.MaxPages, pr.Search.MaxResults, pr.Download = keep.Search.MaxPages, keep.Search.MaxResults, keep.Download
			if err := SaveAll(env.Path, ps); err != nil {
				return nil, err
			}
		}
		return checkDoc(rep, pr.Home, ""), nil
	case "remove":
		name := pr.Name
		ps = append(ps[:i], ps[i+1:]...)
		if err := SaveAll(env.Path, ps); err != nil {
			return nil, err
		}
		return manageDoc(ps, "Removed “"+name+"”. Books already downloaded stay in your library."), nil
	}
	return nil, errors.New("unknown catalog address: " + target)
}

func checkDoc(rep *Report, raw, word string) *doc.Document {
	d := &doc.Document{Title: "Catalog check: " + rep.Profile.Name, URL: raw, Origin: "live", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: rep.Profile.Home}}
	for _, fi := range rep.Findings {
		mark := "✗ "
		if fi.OK {
			mark = "✓ "
		}
		d.Blocks = append(d.Blocks, para(doc.Span{Text: mark, Style: doc.Bold}, doc.Span{Text: fi.Text}))
	}
	if len(rep.Sample) > 0 {
		var items [][]doc.Block
		for _, r := range rep.Sample {
			items = append(items, []doc.Block{para(doc.Span{Text: r.Title, Style: doc.Bold}, doc.Span{Text: "  " + r.Extra, Style: doc.Italic})})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Sample results"}}}, doc.List{Ordered: true, Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{})
	if rep.CanAdd {
		add := "w5f:catalog/add?" + url.Values{"url": {raw}, "w": {word}}.Encode()
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "✓ add this catalog", Style: doc.Bold, Link: link(d, add, "add")}))
	} else {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "This site cannot be added as it is."})
	}
	d.Blocks = append(d.Blocks, para(doc.Span{Text: "Another test word: g → catalog-add " + raw + " <word>", Style: doc.Italic}))
	return d
}

func manageDoc(ps []Profile, note string) *doc.Document {
	d := &doc.Document{Title: "Site catalogs", URL: "w5f:catalogs", Origin: "local", Lang: "en"}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	if len(ps) == 0 {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "No site catalogs yet. Add one with g → catalog-add <address>.", Style: doc.Italic}))
		return d
	}
	var items [][]doc.Block
	for _, p := range ps {
		items = append(items, []doc.Block{
			para(doc.Span{Text: p.Name, Style: doc.Bold, Link: link(d, "w5f:catalog/"+p.ID, p.Name)},
				doc.Span{Text: "  · " + p.ID + " · " + p.Search.Kind, Style: doc.Italic}),
			para(doc.Span{Text: "search: g → cat " + p.ID + " <words>   ", Style: doc.Italic},
				doc.Span{Text: "re-check", Link: link(d, "w5f:catalog/"+p.ID+"/recheck", "re-check")}, doc.Span{Text: "   "},
				doc.Span{Text: "remove", Link: link(d, "w5f:catalog/"+p.ID+"/remove", "remove")}),
		})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items},
		para(doc.Span{Text: "Profiles are kept in " + Path() + " (safe to edit).", Style: doc.Italic}))
	return d
}

func catalogDoc(p Profile) *doc.Document {
	d := &doc.Document{Title: p.Name, URL: "w5f:catalog/" + p.ID, Origin: "local", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: p.Home}}
	d.Blocks = append(d.Blocks,
		para(doc.Span{Text: "Search: g → cat " + p.ID + " <words>", Style: doc.Bold}),
		para(doc.Span{Text: "Only an author: cat " + p.ID + " author:<name> · only a title: cat " + p.ID + " title:<words>", Style: doc.Italic}),
		para(doc.Span{Text: fmt.Sprintf("Full search reads up to %d result pages (%d results).", p.Search.MaxPages, p.Search.MaxResults), Style: doc.Italic}),
		para(doc.Span{Text: "open the site's front page", Link: link(d, p.Home, "front page")}))
	return d
}

func resultsDoc(env Env, p Profile, q string, f *Found, searchErr error) *doc.Document {
	d := &doc.Document{Title: p.Name + ": " + q, URL: "w5f:catalog/" + p.ID + "?" + url.Values{"q": {q}}.Encode(), Origin: "live", Lang: "en"}
	summary := fmt.Sprintf("%d results from %d pages", len(f.Results), f.Pages)
	if f.Pages == 1 {
		summary = fmt.Sprintf("%d results from 1 page", len(f.Results))
	}
	d.Blocks = append(d.Blocks, para(doc.Span{Text: summary, Style: doc.Italic}))
	if f.Capped {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: fmt.Sprintf("Stopped at the limit (%d pages / %d results) — use more specific words or raise max_pages in %s.", p.Search.MaxPages, p.Search.MaxResults, Path())})
	}
	if f.FieldIgnored {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "This site has no separate author/title search; the words were searched everywhere."})
	}
	if searchErr != nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "The search stopped early: " + searchErr.Error()})
	}
	var items [][]doc.Block
	for _, r := range f.Results {
		href := "w5f:catalog/" + p.ID + "/item?" + url.Values{"u": {r.URL}, "t": {r.Title}}.Encode()
		mark := ""
		if _, ok := env.DB.BookBySource("site:" + p.ID + ":" + r.URL); ok {
			mark = "✓ "
		}
		item := []doc.Block{para(doc.Span{Text: mark, Style: doc.Bold}, doc.Span{Text: r.Title, Style: doc.Bold, Link: link(d, href, r.Title)})}
		if r.Extra != "" {
			item = append(item, para(doc.Span{Text: r.Extra, Style: doc.Italic}))
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "Nothing found.", Style: doc.Italic}))
		return d
	}
	d.Blocks = append(d.Blocks, doc.List{Ordered: true, Items: items})
	return d
}

func itemDoc(env Env, p Profile, itemURL, title string, ds []Download) *doc.Document {
	d := &doc.Document{Title: title, URL: itemURL, Origin: "live", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: p.Name}}
	if b, ok := env.DB.BookBySource("site:" + p.ID + ":" + itemURL); ok {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "✓ in your library — ", Style: doc.Bold},
			doc.Span{Text: "open it", Link: link(d, fmt.Sprintf("w5f:book/%d", b.ID), "open")}))
	}
	if len(ds) == 0 {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "No downloadable book file was found on this page."})
	} else {
		var items [][]doc.Block
		for _, dl := range ds {
			get := "w5f:catalog/" + p.ID + "/get?" + url.Values{"u": {dl.URL}, "f": {dl.Format}, "t": {title}, "src": {itemURL}}.Encode()
			label := strings.ToUpper(dl.Format)
			if dl.Label != "" && !strings.EqualFold(dl.Label, dl.Format) {
				label += " — " + dl.Label
			}
			items = append(items, []doc.Block{para(doc.Span{Text: label, Link: link(d, get, label)})})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Download into your library"}}}, doc.List{Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, para(doc.Span{Text: "→ the book's page on the site", Link: link(d, itemURL, "page")}))
	return d
}
```

- [ ] **Step 4: Run** `go test ./internal/sitecat -v` → PASS

- [ ] **Step 5: Wire books home** — in `internal/books/docs.go` add to `Env`:

```go
	// Catalogs lists added site catalogs for the "Find books" section.
	Catalogs func() []CatalogLink
```

and the type:

```go
// CatalogLink is a site catalog shown in the Library.
type CatalogLink struct{ ID, Name string }
```

In `homeDoc`, replace the `doc.List{Items: [][]doc.Block{ … }}` under "Find books" with a list built as:

```go
	find := [][]doc.Block{
		{doc.Paragraph{Text: doc.Inline{{Text: "Project Gutenberg — most downloaded", Link: link(d, "w5f:books/gutenberg", "Gutenberg")},
			{Text: "   search: g → gut <words>", Style: doc.Italic}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "Standard Ebooks — newest", Link: link(d, "w5f:books/se", "Standard Ebooks")},
			{Text: "   search: g → se <words>", Style: doc.Italic}}}},
	}
	if env.Catalogs != nil {
		for _, c := range env.Catalogs() {
			find = append(find, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: c.Name, Link: link(d, "w5f:catalog/"+c.ID, c.Name)},
				{Text: "   search: g → cat " + c.ID + " <words>", Style: doc.Italic}}}})
		}
	}
	find = append(find, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "+ add a site: g → catalog-add <address>", Style: doc.Italic}, {Text: "   "},
		{Text: "manage catalogs", Link: link(d, "w5f:catalogs", "manage")}}}})
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Find books"}}}, doc.List{Items: find})
```

- [ ] **Step 6: Wire source** — in `internal/source/source.go`:

Import `"w5f/internal/sitecat"`. Before the `books.IsTarget` block add:

```go
	if sitecat.IsTarget(target) {
		db, err := store.Default()
		if err != nil {
			return nil, fmt.Errorf("opening the local database: %w", err)
		}
		benv := booksEnv(db)
		return sitecat.Route(ctx, target, sitecat.Env{Fetcher: Fetcher, DB: db, Path: sitecat.Path(),
			OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) {
				return books.Route(ctx, fmt.Sprintf("w5f:book/%d", b.ID), benv)
			}})
	}
```

Replace the books block's `books.Env{Fetcher: Fetcher, DB: db, LoadFile: loadFile}` with `booksEnv(db)` and add:

```go
func booksEnv(db *store.DB) books.Env {
	return books.Env{Fetcher: Fetcher, DB: db, LoadFile: loadFile, Catalogs: func() []books.CatalogLink {
		ps, _ := sitecat.LoadAll(sitecat.Path())
		out := make([]books.CatalogLink, 0, len(ps))
		for _, p := range ps {
			out = append(out, books.CatalogLink{ID: p.ID, Name: p.Name})
		}
		return out
	}}
}
```

In `Resolve`, directly after `case s == "": return ""` add (they must come before the `strings.Contains(s, "://")` pass-through, because `catalog-add` carries an address):

```go
	case strings.HasPrefix(lower, "catalog-add ") && len(strings.Fields(s)) > 1:
		fields := strings.Fields(s[len("catalog-add "):])
		v := url.Values{"url": {fields[0]}}
		if len(fields) > 1 {
			v.Set("w", strings.Join(fields[1:], " "))
		}
		return "w5f:catalog/check?" + v.Encode()
	case lower == "catalogs":
		return "w5f:catalogs"
	case strings.HasPrefix(lower, "cat ") && strings.TrimSpace(s[4:]) != "":
		fields := strings.SplitN(strings.TrimSpace(s[4:]), " ", 2)
		if len(fields) == 1 {
			return "w5f:catalog/" + fields[0]
		}
		return "w5f:catalog/" + fields[0] + "?" + url.Values{"q": {strings.TrimSpace(fields[1])}}.Encode()
```

In `Friendly`, before the `crom` case add:

```go
	case errors.Is(err, sitecat.ErrRefused):
		return "The site refused the download — it may need a login or block readers."
```

- [ ] **Step 7: Wire TUI** — in `internal/tui/tui.go`:

(a) cancellable loads: add package vars and replace `load`:

```go
var (
	loadMu     sync.Mutex
	loadCancel context.CancelFunc
)

func load(target string, replace bool) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	loadMu.Lock()
	if loadCancel != nil {
		loadCancel()
	}
	loadCancel = cancel
	loadMu.Unlock()
	return tea.Batch(func() tea.Msg {
		d, err := source.Load(ctx, target, source.Options{Reload: replace})
		return loadedMsg{target: target, doc: d, err: err, replace: replace}
	}, progressTick())
}

type progressMsg struct{}

func progressTick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return progressMsg{} })
}

func cancelLoad() {
	loadMu.Lock()
	if loadCancel != nil {
		loadCancel()
	}
	loadMu.Unlock()
}
```

(b) in `Update`, handle the tick (keeps the status line fresh while loading):

```go
	case progressMsg:
		if m.loading != "" {
			return m, progressTick()
		}
		return m, nil
```

(c) in `key`, before `if m.cur == nil`: esc cancels a running load:

```go
	if s == "esc" && m.loading != "" {
		cancelLoad()
		m.loading, m.status = "", "cancelled"
		return m, nil
	}
```

and at the very top of the `loadedMsg` case (before `m.loading = ""`, so a superseded load does not clear the indicator of the new one):

```go
	case loadedMsg:
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.loading = ""
```

(d) loading texts — add the helper:

```go
// catalogLoading is the status text while a site-catalog page loads.
func catalogLoading(href string) string {
	switch {
	case !strings.HasPrefix(href, "w5f:catalog/"):
		return ""
	case strings.HasPrefix(href, "w5f:catalog/check"), strings.HasPrefix(href, "w5f:catalog/add"), strings.HasSuffix(href, "/recheck"):
		return "inspecting the site"
	case strings.Contains(href, "/get?"):
		return "downloading the book into your library"
	case strings.Contains(href, "?q="):
		return "searching the site (all result pages)"
	}
	return ""
}
```

In `follow`, after the `w5f:books/get/` check, and in `gotoKey` right after `m.loading = target`, add:

```go
		if t := catalogLoading(href); t != "" { // in gotoKey: catalogLoading(target)
			m.loading = t
		}
```

(e) bottom bar: when loading, append the live progress:

```go
	case m.loading != "":
		text = " loading " + m.loading + " …"
		if p, _ := sitecat.CurrentProgress.Load().(string); p != "" {
			text = " " + p + " …   esc cancels"
		}
```

Imports to add in tui.go: `"errors"`, `"sync"`, `"time"`, `"w5f/internal/sitecat"`.

- [ ] **Step 8: Run everything**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: all packages PASS (fix the welcome/tui tests only if focus counts changed — they are count-independent since M3).

---

### Task 7: Live smoke test, builds and docs

**Files:**
- Modify: `README.md`
- No code unless the smoke test reveals a bug (then: failing test first, fix, re-run).

- [ ] **Step 1: Isolated live run** (temp data and library so the owner's files are untouched)

```bash
cd /c/Users/murat/code/w5f && go build -o bin/w5f-dev.exe ./cmd/w5f
T=$(mktemp -d); export APPDATA="$(cygpath -w $T)" W5F_BOOKS="$(cygpath -w $T)\\Books"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://www.gutenberg.org dracula"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://www.fadedpage.com dracula"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://archive.org dracula"
```

Expected: Gutenberg → ✓ search (OpenSearch or form), ✓ results, ✓ downloads (EPUB); Faded Page → ✓ search via form, results or a clear ✗ line; Internet Archive → ✗ JavaScript/no results, "cannot be added". Record the actual outcomes.

- [ ] **Step 2: Full search on an added catalog**

```bash
./bin/w5f-dev.exe dump -w 76 "w5f:catalog/add?url=https%3A%2F%2Fwww.gutenberg.org&w=dracula"
./bin/w5f-dev.exe dump -w 76 "cat gutenberg-org dracula" | head -20
./bin/w5f-dev.exe dump -w 76 "cat gutenberg-org author:stoker" | head -12
```

Expected: "N results from P pages" with P > 1 when the site paginates; author query either routed or noted as searched everywhere.

- [ ] **Step 3: Builds**

```bash
V=0.3.1-m3 && mv -f bin/w5f.exe bin/w5f.exe.old 2>/dev/null; go build -ldflags "-X main.version=$V" -o bin/w5f.exe ./cmd/w5f
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags "-s -w -X main.version=$V" -o bin/w5f-linux-amd64 ./cmd/w5f
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -trimpath -ldflags "-s -w -X main.version=$V" -o bin/w5f-linux-386 ./cmd/w5f
rm -f bin/w5f-dev.exe; rm -rf "$T"
```

- [ ] **Step 4: README** — under Usage add:

```
w5f "catalog-add <address> [test word]"   add any book website as a Library catalog
w5f "cat <id> dracula"                    full search across all its result pages (author:… / title:…)
w5f catalogs                              manage site catalogs (re-check, remove)
```

and under Layout: `internal/sitecat   site catalogs: search discovery, result-list learning, full search, downloads`.
