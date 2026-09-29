# Site Catalogs Round 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the site-catalog adder recognise four more site families — a DuckDuckGo `site:` fallback, "Index of /" directory listings, known platforms (Internet Archive, DSpace 7, OJS/EPrints/MediaWiki/Blogger hints, Calibre `/opds`) and POST search forms — by trying every candidate search method with the test word.

**Architecture:** Every way of searching becomes a backend (`First` / `Page` / `Item`, optionally `SearchLocal`) chosen by `SearchSpec.Kind`; detectors propose candidates in a fixed order and `Probe` tries them until one returns results, logging each attempt. `Search` becomes a generic page loop over a backend's `Request`s (GET or POST).

**Tech Stack:** Go 1.26 toolchain, goquery, golang.org/x/net/html, encoding/json, BurntSushi/toml, existing `fetch`, `search`, `books`, `store`, `doc` packages; httptest fixtures.

**Spec:** `docs/superpowers/specs/2026-09-28-site-catalogs-r2-design.md`

## Global Constraints

- No bypassing of bot checks, captchas, logins or JavaScript challenges; a DuckDuckGo "anomaly" page stops the search with an honest message; login forms (password fields) are never submitted.
- Existing profiles (round 1) keep working unchanged; `kind = "none"` is read as `browse`.
- Candidate chain: at most 6 attempts before the engine fallback, which is always tried when everything else failed.
- A candidate passes with ≥ 1 result (API/OPDS/index/engine) or ≥ 3 results (layout-learned HTML kinds).
- Engine defaults: 3 pages / 60 results. Index defaults: `max_folders` 150, depth ≤ 5, 1 s between folder requests.
- Internet Archive: only `mediatype:texts`, excluding `inlibrary`, `printdisabled`, `lendinglibrary`; items with `access-restricted-item` are "lending only".
- Unit tests never touch the network: `EngineEndpoint` is set to `""` in `TestMain` unless a test points it at a fixture; `ArchiveBase` is pointed at fixtures.
- UI text English; pure Go (CGO off); must keep building for `linux/amd64` `GOAMD64=v1` and `linux/386`.
- Windows editing note: after any Python-based edit run `gofmt -w .`. No commits unless the owner asks (repo has no commits).

## Notes on the spec

- Spec §2 `Result.Downloads` is replaced by a URL rule: a result whose URL is itself a book file (extension in the path, or a query value such as `link.php?file=x.pdf`) is offered as a direct download on its item page (`directDownload`, Task 3). Same effect, no data carried through `w5f:` addresses.
- Spec §2 interface is split into `detector` functions (who proposes candidates) and `backend` values (how a kind searches), because one detector (`discoverAll`) proposes several kinds (opensearch, opds, form, post, wordpress).
- Spec §2.2 searched plain `<words>` sorted by downloads; a live check (2026-09-28) showed full-text matches drown the books ("dracula" → games and comics first). The general query is limited to `title`, `creator` and `subject` fields, still sorted by downloads.
- Spec §2.3 detects DSpace by meta generator; live DSpace 8.2 (MIT) has none, so detection uses the Angular `<ds-app>` element (or a generator, when present) and then confirms `dspaceVersion` at `/server/api`.
- Round-1 test `TestDiscoverSearchVariants` "post-only" case changes: POST forms are now a supported kind (`post`), no longer reported as unsupported.

## Review Focus

1. Directory listings whose links escape the root (`../`, absolute links elsewhere, other hosts, loops like `a/b/a/b`) — the crawler must stay below the root and never revisit a folder (test in Task 9).
2. DuckDuckGo results from other sites and adverts — only the catalog's own host is kept (test in Task 5).
3. A login or newsletter POST form mistaken for a search form — forms with password or email inputs are skipped (test in Task 4).
4. Internet Archive items with hundreds of files and queries containing Lucene syntax (`:()"`) — file list capped at 30, special characters stripped (test in Task 6).
5. Round-1 `catalogs.toml` (kind `none`, no method) — loads as `browse` and front-page browsing still works (test in Task 2).

---

### Task 1: `fetch.PostForm` and `search.RealURL`

**Files:**
- Modify: `internal/fetch/fetch.go` (add `PostForm` after `PostJSON`)
- Modify: `internal/search/search.go` (rename `realURL` → `RealURL`, all call sites in package `search` including tests)
- Test: `internal/fetch/postform_test.go`

**Interfaces:**
- Produces: `func (f *Fetcher) PostForm(ctx context.Context, u string, form url.Values) (*Response, error)`; `func search.RealURL(href string) string`

- [ ] **Step 1: Failing test** (`internal/fetch/postform_test.go`)

```go
package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPostFormSendsBodyAndIdentity(t *testing.T) {
	var method, ua, ct string
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method, ua, ct, got = r.Method, r.UserAgent(), r.Header.Get("Content-Type"), r.PostForm
		fmt.Fprint(w, "<html>results</html>")
	}))
	defer srv.Close()
	f := New("", "test")
	f.HostGap = 0
	resp, err := f.PostForm(context.Background(), srv.URL+"/find", url.Values{"q": {"dracula"}, "lang": {"en"}})
	if err != nil {
		t.Fatal(err)
	}
	if method != "POST" || ct != "application/x-www-form-urlencoded" || got.Get("q") != "dracula" || got.Get("lang") != "en" {
		t.Errorf("method=%s ct=%s form=%v", method, ct, got)
	}
	if ua != f.UserAgent || string(resp.Body) != "<html>results</html>" || resp.URL.Path != "/find" {
		t.Errorf("ua=%q body=%q url=%v", ua, resp.Body, resp.URL)
	}
	f.Offline = true
	if _, err := f.PostForm(context.Background(), srv.URL+"/find", nil); err == nil {
		t.Error("offline must not post")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/fetch -run PostForm` → FAIL (`f.PostForm undefined`)

- [ ] **Step 3: Implement** — append to `internal/fetch/fetch.go` after `PostJSON` (add `"strings"` to imports if missing):

```go
// PostForm submits a form (a site's search) and returns the page. It is
// not cached: the same address answers differently for different bodies.
func (f *Fetcher) PostForm(ctx context.Context, u string, form url.Values) (*Response, error) {
	if f.Offline {
		return nil, fmt.Errorf("%s: %w", u, ErrOffline)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	if pu, err := url.Parse(u); err == nil {
		f.wait(ctx, pu.Host)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, &HTTPError{URL: u, Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return &Response{Body: body, URL: resp.Request.URL, ContentType: resp.Header.Get("Content-Type"), Fetched: time.Now()}, nil
}
```

Rename in package `search`:

```bash
cd /c/Users/murat/code/w5f && sed -i 's/\brealURL(/RealURL(/g; s/^\/\/ realURL unwraps/\/\/ RealURL unwraps/' internal/search/*.go
```

- [ ] **Step 4: Run** `gofmt -w internal/fetch internal/search && go vet ./internal/fetch ./internal/search && go test ./internal/fetch ./internal/search` → PASS

---

### Task 2: Profile fields and round-1 compatibility

**Files:**
- Modify: `internal/sitecat/profile.go` (`SearchSpec` fields, `ApplyDefaults`)
- Modify: `internal/sitecat/search.go:83` and `internal/sitecat/docs.go:81` (`"none"` → `"browse"`)
- Test: `internal/sitecat/profile_r2_test.go`

**Interfaces:**
- Produces: `SearchSpec.Method`, `.Body`, `.API`, `.Collection`, `.Index` (string), `.MaxFolders` (int); kind `browse` replaces `none`; engine defaults 3/60; index `MaxFolders` 150.

- [ ] **Step 1: Failing test** (`internal/sitecat/profile_r2_test.go`)

```go
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
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run Round1Profiles` → FAIL (unknown fields `Method`, `Body`, `MaxFolders`)

- [ ] **Step 3: Implement** — in `profile.go` replace the `SearchSpec` type and `ApplyDefaults`:

```go
// SearchSpec says how to search the site. Templates contain {q}.
type SearchSpec struct {
	// Kind picks the backend: opensearch | opds | form | post | wordpress |
	// archive | dspace | index | browse | engine.
	Kind     string `toml:"kind"`
	Template string `toml:"template"`
	// Fields holds "author"/"title" templates (for POST: body templates).
	Fields     map[string]string `toml:"fields,omitempty"`
	MaxPages   int               `toml:"max_pages"`
	MaxResults int               `toml:"max_results"`
	Method     string            `toml:"method,omitempty"`      // "POST" for post forms; default GET
	Body       string            `toml:"body,omitempty"`        // POST body template with {q}
	API        string            `toml:"api,omitempty"`         // dspace: REST base
	Collection string            `toml:"collection,omitempty"`  // archive: restrict to a collection
	Index      string            `toml:"index,omitempty"`       // index: local file list
	MaxFolders int               `toml:"max_folders,omitempty"` // index: folders read when indexing
}
```

```go
// ApplyDefaults fills unset limits and preferences.
func (p *Profile) ApplyDefaults() {
	if p.Search.Kind == "none" { // round-1 name
		p.Search.Kind = "browse"
	}
	pages, results := 10, 300
	if p.Search.Kind == "engine" { // a search engine's index is shallow
		pages, results = 3, 60
	}
	if p.Search.MaxPages <= 0 {
		p.Search.MaxPages = pages
	}
	if p.Search.MaxResults <= 0 {
		p.Search.MaxResults = results
	}
	if p.Search.Kind == "index" && p.Search.MaxFolders <= 0 {
		p.Search.MaxFolders = 150
	}
	if len(p.Download.Prefer) == 0 {
		p.Download.Prefer = append([]string{}, DefaultPrefer...)
	}
}
```

In `search.go` change `if p.Search.Kind == "none" || p.Search.Template == ""` to `if p.Search.Kind == "browse" || p.Search.Template == ""`.
In `docs.go` change `if q.Get("q") == "" && pr.Search.Kind == "none" {` to `if q.Get("q") == "" && pr.Search.Kind == "browse" {`.

- [ ] **Step 4: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS except `TestProbeBrowseOnly` may still pass (Probe still writes `"none"` until Task 3). All green expected.

---

### Task 3: Backend core, candidate chain, generic search

**Files:**
- Create: `internal/sitecat/backend.go` (types, registry, `doRequest`, `newSite`, `directDownload`, `fileFormatOf`)
- Create: `internal/sitecat/backend_html.go` (html backend; `detectHTML`, `detectBrowse`)
- Create: `internal/sitecat/backend_opds.go` (opds backend; move `atomEntries`, `atomNext` here from `search.go`)
- Modify: `internal/sitecat/discover.go` (add `discoverAll`; `DiscoverSearch` becomes its first element)
- Modify: `internal/sitecat/search.go` (`Search` rewritten over backends; remove `atomEntries`/`atomNext`)
- Modify: `internal/sitecat/probe.go` (`Probe` rewritten as a candidate chain)
- Modify: `internal/sitecat/docs.go` (item page via `itemDownloads`; `itemDoc` gains a `note` argument)
- Modify: `internal/sitecat/probe_test.go` (`TestProbeBrowseOnly` expects kind `browse`)
- Test: `internal/sitecat/chain_test.go`

**Interfaces:**
- Consumes: `fetch.PostForm` (Task 1), `SearchSpec` fields (Task 2)
- Produces:
  - `type Request struct { Method, URL string; Form url.Values }`
  - `type pageOut struct { Results []Result; Next *Request }`
  - `type candidate struct { Spec SearchSpec; Desc string }`
  - `type site struct { F *fetch.Fetcher; Home *url.URL; Body []byte; Doc *goquery.Document; Generator string; Pages []sitePage }`, `type sitePage struct { URL *url.URL; Body []byte }`
  - `type backend interface { First(p *Profile, q Query) (Request, bool); Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error); Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) }`
  - `type localSearcher interface { SearchLocal(p *Profile, q Query) ([]Result, bool, error) }`
  - `type localProber interface { ProbeLocal(ctx context.Context, f *fetch.Fetcher, p *Profile, word string) ([]Result, string, error) }`
  - `type detector func(ctx context.Context, s *site) []candidate`; `var detectors []detector`; `var engineDetector detector` (nil until Task 6)
  - `func backendFor(kind string) backend` (nil for unknown kinds)
  - `func doRequest(ctx context.Context, f *fetch.Fetcher, r Request) (*fetch.Response, error)`
  - `func fileFormatOf(u *url.URL) string`, `func directDownload(itemURL string) (Download, bool)`
  - `func itemDownloads(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error)`
  - `func discoverAll(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) []candidate`
  - `const maxAttempts = 6`

- [ ] **Step 1: Failing tests** (`internal/sitecat/chain_test.go`)

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

// A site whose OpenSearch results are built with JavaScript but whose plain
// form works: the chain must fall through to the form.
func TestProbeTriesNextCandidate(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"></head>
<body><form action="/s"><input type="search" name="q"></form></body></html>`)
	})
	mux.HandleFunc("/osd.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<OpenSearchDescription><Url type="text/html" template="`+srv.URL+`/app?q={searchTerms}"/></OpenSearchDescription>`)
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><div id="app"></div><noscript>Enable JavaScript</noscript></body></html>`)
	})
	mux.HandleFunc("/s", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><ul class="r"><li><a href="/b/1">Dracula</a> Stoker</li><li><a href="/b/2">Dracula's Guest</a> Stoker</li><li><a href="/b/3">Dracula (abridged)</a> Stoker</li></ul></body></html>`)
	})
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	if rep.Profile.Search.Kind != "form" || len(rep.Findings) < 2 {
		t.Fatalf("profile %+v findings %+v", rep.Profile.Search, rep.Findings)
	}
	if rep.Findings[0].OK || !strings.Contains(rep.Findings[0].Text, "JavaScript") || !rep.Findings[1].OK {
		t.Errorf("findings: %+v", rep.Findings)
	}
}

func TestDiscoverAllListsEveryMethod(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/osd.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<OpenSearchDescription><Url type="text/html" template="`+srv.URL+`/os?q={searchTerms}"/></OpenSearchDescription>`)
	})
	body := []byte(`<html><head><link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"></head>
<body><form action="/s"><input type="search" name="q"></form></body></html>`)
	cs := discoverAll(context.Background(), testFetcher(), body, srv.URL+"/")
	if len(cs) != 2 || cs[0].Spec.Kind != "opensearch" || cs[1].Spec.Kind != "form" {
		t.Errorf("candidates: %+v", cs)
	}
}

func TestDirectDownload(t *testing.T) {
	cases := map[string]string{
		"https://x.example/files/a.pdf":              "pdf",
		"https://x.example/link.php?file=123-a5.pdf": "pdf",
		"https://x.example/get/Book%20One.epub":      "epub",
		"https://x.example/book/1":                   "",
		"https://x.example/archive.zip":              "",
	}
	for in, want := range cases {
		d, ok := directDownload(in)
		if ok != (want != "") || d.Format != want {
			t.Errorf("directDownload(%q) = %+v %v, want %q", in, d, ok, want)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'TryNext|DiscoverAll|DirectDownload'` → FAIL (undefined `discoverAll`, `directDownload`)

- [ ] **Step 3: Implement `backend.go`**

```go
package sitecat

import (
	"bytes"
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// Request is one page request: GET, or POST with a form body.
type Request struct {
	Method string // "GET" (default) or "POST"
	URL    string
	Form   url.Values
}

func (r Request) key() string { return r.Method + " " + r.URL + " " + r.Form.Encode() }

// pageOut is what a backend reads from one result page.
type pageOut struct {
	Results []Result
	Next    *Request // nil on the last page
}

// candidate is one way of searching a site, found while checking it.
type candidate struct {
	Spec SearchSpec
	Desc string // for the check page
}

// site is what detectors see while a site is checked.
type site struct {
	F         *fetch.Fetcher
	Home      *url.URL
	Body      []byte
	Doc       *goquery.Document
	Generator string // meta generator, lower case
	// Pages are the pages whose search forms count: the home page and a
	// linked search page ("Find a book"), if any.
	Pages []sitePage
}

type sitePage struct {
	URL  *url.URL
	Body []byte
}

// backend is how one kind of catalog is searched.
type backend interface {
	// First builds the request for page 1; fieldIgnored as in BuildURL.
	First(p *Profile, q Query) (req Request, fieldIgnored bool)
	// Page reads one result page. pageNo starts at 1.
	Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error)
	// Item lists the downloads of one result.
	Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error)
}

// localSearcher is a backend that searches without requests (a file index).
type localSearcher interface {
	SearchLocal(p *Profile, q Query) (rs []Result, fieldIgnored bool, err error)
}

// localProber checks a local-search candidate; note describes what it found.
type localProber interface {
	ProbeLocal(ctx context.Context, f *fetch.Fetcher, p *Profile, word string) (rs []Result, note string, err error)
}

// detector proposes candidates for a site, best first.
type detector func(ctx context.Context, s *site) []candidate

// detectors run in this order when a site is checked.
var detectors = []detector{detectHTML, detectBrowse}

// engineDetector is the last resort, tried only when every other candidate
// failed (set by the search-engine backend).
var engineDetector detector

// maxAttempts caps the candidates tried before the engine fallback.
const maxAttempts = 6

// backendFor returns the backend of a search kind, nil if unknown.
func backendFor(kind string) backend {
	switch kind {
	case "opensearch", "form", "post", "wordpress", "browse":
		return htmlBackend{}
	case "opds":
		return opdsBackend{}
	}
	return nil
}

// doRequest performs a GET (cached) or POST (uncached) page request.
func doRequest(ctx context.Context, f *fetch.Fetcher, r Request) (*fetch.Response, error) {
	if strings.EqualFold(r.Method, "POST") {
		return f.PostForm(ctx, r.URL, r.Form)
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	return f.Get(ctx, u, fetch.Options{})
}

// newSite prepares a checked site: parsed home page, generator and a
// linked search page.
func newSite(ctx context.Context, f *fetch.Fetcher, home *fetch.Response) *site {
	s := &site{F: f, Home: home.URL, Body: home.Body, Pages: []sitePage{{URL: home.URL, Body: home.Body}}}
	if gq, err := goquery.NewDocumentFromReader(bytes.NewReader(home.Body)); err == nil {
		s.Doc = gq
		s.Generator = strings.ToLower(gq.Find(`meta[name="generator"], meta[name="Generator"]`).First().AttrOr("content", ""))
	}
	if su := searchPageLink(home.Body, home.URL); su != nil {
		if sp, err := f.Get(ctx, su, fetch.Options{}); err == nil {
			s.Pages = append(s.Pages, sitePage{URL: sp.URL, Body: sp.Body})
		}
	}
	return s
}

// fileFormatOf names the book format of a file address: by the path's
// extension, or by a query value naming a file ("link.php?file=x.pdf").
func fileFormatOf(u *url.URL) string {
	if f := extFormats[strings.ToLower(path.Ext(u.Path))]; f != "" {
		return f
	}
	for _, vs := range u.Query() {
		for _, v := range vs {
			if f := extFormats[strings.ToLower(path.Ext(v))]; f != "" {
				return f
			}
		}
	}
	return ""
}

// directDownload treats an address that is itself a book file as its own
// download.
func directDownload(itemURL string) (Download, bool) {
	u, err := url.Parse(itemURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Download{}, false
	}
	f := fileFormatOf(u)
	if f == "" {
		return Download{}, false
	}
	name, _ := url.PathUnescape(path.Base(u.Path))
	return Download{URL: itemURL, Format: f, Label: name}, true
}

// itemDownloads lists the downloads of a result for its catalog's kind.
func itemDownloads(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	if d, ok := directDownload(itemURL); ok {
		return []Download{d}, nil
	}
	if b := backendFor(p.Search.Kind); b != nil {
		return b.Item(ctx, f, p, itemURL)
	}
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}
```

- [ ] **Step 4: Implement `backend_html.go`**

```go
package sitecat

import (
	"context"

	"w5f/internal/fetch"
)

// htmlBackend searches sites through their own HTML result pages:
// OpenSearch HTML templates, GET and POST forms, WordPress and browse-only.
type htmlBackend struct{}

func (htmlBackend) First(p *Profile, q Query) (Request, bool) {
	u, ignored := BuildURL(*p, q)
	return Request{URL: u}, ignored
}

func (htmlBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	rs := Results(body, pageURL, p.Layout)
	if len(rs) == 0 && pageNo == 1 {
		// While checking (no layout yet) any list is learned; later only a
		// related one — never a sidebar on a "no results" page.
		if nl, r2 := LearnLayout(body, pageURL); nl.Item != "" &&
			(p.Layout.Item == "" || nl.Parent == p.Layout.Parent || nl.Item == p.Layout.Item) {
			rs, p.Layout = r2, nl
		}
	}
	out := pageOut{Results: rs}
	if n := NextPage(body, pageURL); n != "" {
		out.Next = &Request{URL: n}
	}
	return out, nil
}

func (htmlBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}

// detectHTML proposes every search method found on the home page and on a
// linked search page.
func detectHTML(ctx context.Context, s *site) []candidate {
	var out []candidate
	seen := map[string]bool{}
	for i, pg := range s.Pages {
		for _, c := range discoverAll(ctx, s.F, pg.Body, pg.URL.String()) {
			k := c.Spec.Kind + " " + c.Spec.Template + " " + c.Spec.Body
			if seen[k] {
				continue
			}
			seen[k] = true
			if i > 0 {
				c.Desc += " on " + pg.URL.String()
			}
			out = append(out, c)
		}
	}
	return out
}

// detectBrowse offers the front page's own book list (browse-only).
func detectBrowse(ctx context.Context, s *site) []candidate {
	return []candidate{{Spec: SearchSpec{Kind: "browse"}, Desc: "the front page's book list"}}
}
```

- [ ] **Step 5: Implement `backend_opds.go`** — move `atomEntries` and `atomNext` (unchanged bodies) from `search.go` into this file, then add:

```go
// opdsBackend searches OPDS catalogs (Atom feeds).
type opdsBackend struct{}

func (opdsBackend) First(p *Profile, q Query) (Request, bool) {
	u, ignored := BuildURL(*p, q)
	return Request{URL: u}, ignored
}

func (opdsBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	out := pageOut{Results: atomEntries(body, pageURL)}
	if n := atomNext(body, pageURL); n != "" {
		out.Next = &Request{URL: n}
	}
	return out, nil
}

func (opdsBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}
```

Imports of `backend_opds.go`: `bytes`, `context`, `net/url`, `strings`, `github.com/PuerkitoBio/goquery`, `w5f/internal/fetch`. Remove `bytes` and `goquery` from `search.go` imports if no longer used.

- [ ] **Step 6: `discoverAll` in `discover.go`** — replace `DiscoverSearch` with:

```go
// discoverAll lists every search method a page offers, best first.
func discoverAll(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) []candidate {
	base, _ := url.Parse(pageURL)
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var out []candidate
	// 1. OpenSearch description.
	if h, ok := gq.Find(`link[rel~="search"][type="application/opensearchdescription+xml"]`).First().Attr("href"); ok {
		if t, atom := openSearchTemplate(ctx, f, base, h); t != "" {
			if atom { // results come as an Atom (OPDS) feed
				out = append(out, candidate{SearchSpec{Kind: "opds", Template: t}, "OpenSearch with Atom results (" + t + ")"})
			} else {
				out = append(out, candidate{SearchSpec{Kind: "opensearch", Template: t}, "OpenSearch (" + t + ")"})
			}
		}
	}
	// 2. OPDS feed with a search link.
	if h, ok := gq.Find(`link[type*="opds"]`).First().Attr("href"); ok {
		if t := opdsTemplate(ctx, f, base, h); t != "" {
			out = append(out, candidate{SearchSpec{Kind: "opds", Template: t}, "OPDS catalog search (" + t + ")"})
		}
	}
	// 3. HTML forms.
	if spec, desc, ok := formSearch(gq, base); ok {
		out = append(out, candidate{spec, desc})
	}
	// 4. WordPress.
	if strings.Contains(strings.ToLower(gq.Find(`meta[name="generator"]`).AttrOr("content", "")), "wordpress") {
		u := *base
		u.Path, u.RawQuery = "/", "s={q}"
		out = append(out, candidate{SearchSpec{Kind: "wordpress", Template: u.String()}, "WordPress search (?s=)"})
	}
	return out
}

// DiscoverSearch returns the best search method of a page (kind "none"
// when there is none). The description is for the check page.
func DiscoverSearch(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) (SearchSpec, string) {
	if cs := discoverAll(ctx, f, body, pageURL); len(cs) > 0 {
		return cs[0].Spec, cs[0].Desc
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err == nil {
		post := false
		gq.Find("form").Each(func(_ int, f *goquery.Selection) {
			if strings.EqualFold(strings.TrimSpace(f.AttrOr("method", "")), "post") {
				post = true
			}
		})
		if post {
			return SearchSpec{Kind: "none"}, "the site's search form uses POST, which W5F does not submit"
		}
	}
	return SearchSpec{Kind: "none"}, "no search box, OpenSearch or OPDS link found"
}
```

- [ ] **Step 7: Generic `Search`** — in `search.go` replace the whole `Search` function with:

```go
// Search runs a full search: it follows the site's result pages until they
// end, repeat, stop adding results, or a cap is reached.
func Search(ctx context.Context, f *fetch.Fetcher, p *Profile, raw string, progress Progress) (*Found, error) {
	p.ApplyDefaults()
	b := backendFor(p.Search.Kind)
	if b == nil || p.Search.Kind == "browse" {
		return nil, errors.New("this catalog has no search; browse the site instead")
	}
	q := ParseQuery(raw)
	if q.Words == "" {
		return nil, errors.New("type some words to search")
	}
	if ls, ok := b.(localSearcher); ok {
		rs, ignored, err := ls.SearchLocal(p, q)
		if err != nil {
			return nil, err
		}
		out := &Found{Results: rs, Pages: 1, FieldIgnored: ignored}
		if len(rs) > p.Search.MaxResults {
			out.Results, out.Capped = rs[:p.Search.MaxResults], true
		}
		return out, nil
	}
	first, ignored := b.First(p, q)
	next := &first
	out := &Found{FieldIgnored: ignored}
	seenReq := map[string]bool{}
	seenItem := map[string]bool{}
	defer CurrentProgress.Store("")
	for next != nil && !seenReq[next.key()] {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if out.Pages >= p.Search.MaxPages {
			out.Capped = true
			break
		}
		seenReq[next.key()] = true
		resp, err := doRequest(ctx, f, *next)
		if err != nil {
			if out.Pages == 0 {
				return nil, err
			}
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			return out, err // pages already read are shown with a warning
		}
		out.Pages++
		po, err := b.Page(ctx, f, p, out.Pages, resp.Body, resp.URL.String())
		if err != nil {
			if out.Pages == 1 {
				return nil, err
			}
			out.Pages--
			return out, err
		}
		added := 0
		for _, r := range po.Results {
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
		next = po.Next
	}
	return out, nil
}
```

(Drop the old `p.Search.Template == ""` check: API backends have no template.)

- [ ] **Step 8: Candidate-chain `Probe`** — in `probe.go` replace `Probe` with the following and add `tryCandidate`, `layoutKind`, `checkDownloads` (add `"fmt"` to imports):

```go
// Probe inspects a site: every candidate search method is tried with the
// test word until one returns results; then the downloads of the first
// result are listed (without downloading a book).
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
	p := Profile{Home: resp.URL.String(), Added: time.Now().UTC(), Name: siteName(resp.Body, resp.URL)}
	p.ApplyDefaults()
	if wall := botWall(resp.Body); wall != "" {
		rep.add(false, wall)
		rep.Profile = p
		return rep, nil
	}
	s := newSite(ctx, f, resp)
	var cands []candidate
	for _, d := range detectors {
		cands = append(cands, d(ctx, s)...)
	}
	searchable := false
	for _, c := range cands {
		if c.Spec.Kind != "browse" {
			searchable = true
		}
	}
	if !searchable {
		rep.add(false, "search: no search box, OpenSearch or OPDS link found")
	}
	adopted := false
	for i, c := range cands {
		if i == maxAttempts {
			break
		}
		if tryCandidate(ctx, f, rep, &p, c, testWord, resp) {
			adopted = true
			break
		}
	}
	if !adopted && engineDetector != nil {
		for _, c := range engineDetector(ctx, s) {
			if tryCandidate(ctx, f, rep, &p, c, testWord, resp) {
				adopted = true
				break
			}
		}
	}
	if !adopted {
		if needsJS(resp.Body) {
			rep.add(false, "this site builds its pages with JavaScript; W5F cannot search it")
		}
		rep.Profile = p
		return rep, nil
	}
	checkDownloads(ctx, f, rep, &p)
	rep.CanAdd = true
	rep.Profile = p
	return rep, nil
}

// layoutKind reports kinds whose results come from a learned HTML layout.
func layoutKind(kind string) bool {
	switch kind {
	case "opensearch", "form", "post", "wordpress", "browse":
		return true
	}
	return false
}

// tryCandidate runs one candidate with the test word and records the
// attempt. On success the profile takes the candidate's search.
func tryCandidate(ctx context.Context, f *fetch.Fetcher, rep *Report, p *Profile, c candidate, word string, home *fetch.Response) bool {
	trial := *p
	trial.Search = c.Spec
	trial.Layout = Layout{}
	trial.ApplyDefaults()
	var rs []Result
	more := false
	failure, note := "", ""
	b := backendFor(c.Spec.Kind)
	switch {
	case c.Spec.Kind == "browse":
		// Browse-only: the home page itself carries a book list. Short
		// lists are usually filters or menus, not books.
		l, found := LearnLayout(home.Body, home.URL.String())
		if len(found) < 5 {
			return false // a fallback, not worth a ✗ line
		}
		trial.Layout, rs = l, found
		note = fmt.Sprintf("browse-only: the front page lists %d items (check the samples); the catalog can be browsed, not searched", len(found))
	case b == nil:
		failure = "not supported"
	default:
		if lp, ok := b.(localProber); ok {
			var err error
			rs, note, err = lp.ProbeLocal(ctx, f, &trial, word)
			if err != nil {
				failure = err.Error()
			}
			break
		}
		req, _ := b.First(&trial, Query{Words: word})
		resp, err := doRequest(ctx, f, req)
		if err != nil {
			failure = err.Error()
			break
		}
		po, err := b.Page(ctx, f, &trial, 1, resp.Body, resp.URL.String())
		if err != nil {
			failure = err.Error()
			break
		}
		rs, more = po.Results, po.Next != nil
		if len(rs) == 0 && needsJS(resp.Body) {
			failure = "results are built with JavaScript"
		}
	}
	need := 1
	if layoutKind(c.Spec.Kind) {
		need = 3
	}
	if failure == "" && len(rs) < need {
		failure = fmt.Sprintf("no result list found for “%s”", word)
	}
	if failure != "" {
		rep.add(false, "search: "+c.Desc+" — "+failure)
		return false
	}
	switch {
	case c.Spec.Kind == "browse":
		rep.add(true, note)
	case note != "":
		rep.add(true, "search: "+c.Desc+" — "+note)
	default:
		rep.add(true, fmt.Sprintf("search: %s — %d results for “%s”", c.Desc, len(rs), word))
	}
	if trial.Layout.Item != "" && c.Spec.Kind != "browse" {
		rep.add(true, "results: in “"+trial.Layout.Parent+" > "+trial.Layout.Item+"”")
	}
	if more {
		rep.add(true, "pages: more result pages found — full search follows them (up to "+itoa(trial.Search.MaxPages)+")")
	}
	rep.Sample = rs[:min(5, len(rs))]
	*p = trial
	return true
}

// checkDownloads lists the formats of the first sample result.
func checkDownloads(ctx context.Context, f *fetch.Fetcher, rep *Report, p *Profile) {
	if len(rep.Sample) == 0 {
		return
	}
	ds, err := itemDownloads(ctx, f, p, rep.Sample[0].URL)
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
```

- [ ] **Step 9: Route item via backends** — in `docs.go`:

Replace the `case "item":` block with:

```go
	case "item":
		ds, err := itemDownloads(ctx, env.Fetcher, pr, q.Get("u"))
		if err != nil {
			var nd *notDownloadable
			if errors.As(err, &nd) {
				return itemDoc(env, *pr, q.Get("u"), q.Get("t"), nil, nd.Reason), nil
			}
			return nil, err
		}
		return itemDoc(env, *pr, q.Get("u"), q.Get("t"), ds, ""), nil
```

Change `itemDoc`'s signature to `func itemDoc(env Env, p Profile, itemURL, title string, ds []Download, note string) *doc.Document` and replace its `if len(ds) == 0 {` block with:

```go
	switch {
	case note != "":
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	case len(ds) == 0:
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "No downloadable book file was found on this page."})
	default:
```

(the former `else` body becomes the `default:` body; close the `switch`). Add to `backend.go`:

```go
// notDownloadable explains why a result has no downloads (e.g. an Internet
// Archive lending-library item). It is shown as a note, not an error.
type notDownloadable struct{ Reason string }

func (e *notDownloadable) Error() string { return e.Reason }
```

- [ ] **Step 10: Update round-1 test** — in `internal/sitecat/probe_test.go` `TestProbeBrowseOnly`, change `rep.Profile.Search.Kind != "none"` to `rep.Profile.Search.Kind != "browse"`.

- [ ] **Step 11: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS (all round-1 tests plus the three new ones)

---

### Task 4: POST forms and "next" forms

**Files:**
- Modify: `internal/sitecat/discover.go` (`formSearch` takes a method; skips login/newsletter forms; POST candidates in `discoverAll`)
- Modify: `internal/sitecat/backend_html.go` (`First` for POST; `Page` falls back to `nextForm`; add `nextForm`)
- Modify: `internal/sitecat/probe_test.go` (`TestDiscoverSearchVariants` post-only case)
- Test: `internal/sitecat/post_test.go`

**Interfaces:**
- Consumes: `Request`, `pageOut`, `candidate` (Task 3); `fetch.PostForm` (Task 1)
- Produces: `func formSearch(gq *goquery.Document, base *url.URL, method string) (SearchSpec, string, bool)` (method `"get"` or `"post"`); `func nextForm(body []byte, pageURL string) *Request`

- [ ] **Step 1: Failing tests** (`internal/sitecat/post_test.go`)

```go
package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An old site: POST search, results paged by a "Next" button form.
func postSite(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			fmt.Fprint(w, `<html><body>
<form action="/login" method="post"><input name="user"><input type="password" name="pw"></form>
<form action="/news" method="post"><input type="email" name="mail"></form>
<form action="/find.asp" method="post"><input name="query"><input type="hidden" name="section" value="books"></form>
</body></html>`)
			return
		}
		r.ParseForm()
		if r.PostForm.Get("section") != "books" || r.PostForm.Get("query") == "" {
			http.Error(w, "bad form", 400)
			return
		}
		page := r.PostForm.Get("page")
		if page == "" {
			page = "1"
		}
		fmt.Fprintf(w, `<html><body><ul class="hits">`)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, `<li><a href="/book/%s-%d">%s book number %d</a></li>`, page, i, r.PostForm.Get("query"), i)
		}
		fmt.Fprint(w, `</ul>`)
		if page == "1" {
			fmt.Fprintf(w, `<form action="/find.asp" method="post"><input type="hidden" name="query" value="%s"><input type="hidden" name="section" value="books"><input type="hidden" name="page" value="2"><input type="submit" value="Next"></form>`, r.PostForm.Get("query"))
		}
		fmt.Fprint(w, `</body></html>`)
	}))
}

func TestPostFormSearchAcrossPages(t *testing.T) {
	srv := postSite(t)
	defer srv.Close()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL, "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	sp := rep.Profile.Search
	if sp.Kind != "post" || sp.Method != "POST" || sp.Template != srv.URL+"/find.asp" || sp.Body != "query={q}&section=books" {
		t.Fatalf("spec: %+v", sp)
	}
	p := rep.Profile
	got, err := Search(context.Background(), f, &p, "dracula", nil)
	if err != nil || len(got.Results) != 6 || got.Pages != 2 {
		t.Errorf("search: %v results=%d pages=%d", err, len(got.Results), got.Pages)
	}
}

func TestNextFormGET(t *testing.T) {
	body := page(`<form action="/s"><input type="hidden" name="q" value="x"><input type="hidden" name="p" value="3"><button type="submit">Next ›</button></form>`)
	r := nextForm(body, "https://s.example/s?q=x&p=2")
	if r == nil || r.Method != "GET" || r.URL != "https://s.example/s?p=3&q=x" {
		t.Errorf("next = %+v", r)
	}
	if nextForm(page(`<form action="/s"><input type="submit" value="Search"></form>`), "https://s.example/") != nil {
		t.Error("a plain search button is not a next page")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'PostForm|NextForm'` → FAIL (`nextForm` undefined; POST form not found)

- [ ] **Step 3: `formSearch` with a method** — in `discover.go` change the signature to `func formSearch(gq *goquery.Document, base *url.URL, method string) (SearchSpec, string, bool)` and make these edits inside it:

Replace the method check at the top of the per-form callback:

```go
		m := strings.ToLower(strings.TrimSpace(form.AttrOr("method", "get")))
		if m == "" {
			m = "get"
		}
		if m != method {
			return
		}
		// Login and newsletter forms are never search forms.
		if form.Find(`input[type="password"], input[type="email"]`).Length() > 0 {
			return
		}
```

Replace the `build` closure's last three lines (`u := *action` … `return u.String()`) with:

```go
			if method == "post" {
				return strings.Join(parts, "&") // a POST body template
			}
			u := *action
			u.RawQuery = strings.Join(parts, "&")
			return u.String()
```

Replace `spec := SearchSpec{Kind: "form"}` with:

```go
		spec := SearchSpec{Kind: "form"}
		if method == "post" {
			spec = SearchSpec{Kind: "post", Method: "POST"}
		}
```

After the `if selName != "" { … } else { … }` block (still inside the callback) add:

```go
		if method == "post" { // the address is the action; bodies carry {q}
			spec.Body, spec.Template = spec.Template, action.String()
		}
```

Before `desc := "search form (" + best.Template + ")"` add:

```go
	if method == "post" {
		desc := "POST search form (" + best.Template + ")"
		if len(best.Fields) > 0 {
			desc += " with fields"
		}
		return best, desc, true
	}
```

The "separate author/title forms" block only applies to GET forms (a POST body is not a template for another form): change its opening line `if len(textIdx) == 1 {` to:

```go
		if len(textIdx) == 1 && method == "get" {
```

In `discoverAll` replace step 3 with:

```go
	// 3. HTML forms (GET, then POST).
	if spec, desc, ok := formSearch(gq, base, "get"); ok {
		out = append(out, candidate{spec, desc})
	}
	if spec, desc, ok := formSearch(gq, base, "post"); ok {
		out = append(out, candidate{spec, desc})
	}
```

- [ ] **Step 4: POST requests and next forms** — in `backend_html.go` replace `First` and the tail of `Page`, and add `nextForm` (imports: `bytes`, `net/url`, `regexp`, `strings`, `github.com/PuerkitoBio/goquery`):

```go
func (htmlBackend) First(p *Profile, q Query) (Request, bool) {
	if !strings.EqualFold(p.Search.Method, "POST") {
		u, ignored := BuildURL(*p, q)
		return Request{URL: u}, ignored
	}
	body, ignored := p.Search.Body, false
	if q.Field != "" {
		if fb := p.Search.Fields[q.Field]; fb != "" {
			body = fb
		} else {
			ignored = true
		}
	}
	form, _ := url.ParseQuery(strings.ReplaceAll(body, "{q}", url.QueryEscape(q.Words)))
	return Request{Method: "POST", URL: p.Search.Template, Form: form}, ignored
}
```

In `Page`, replace

```go
	if n := NextPage(body, pageURL); n != "" {
		out.Next = &Request{URL: n}
	}
```

with

```go
	if n := NextPage(body, pageURL); n != "" {
		out.Next = &Request{URL: n}
	} else {
		out.Next = nextForm(body, pageURL)
	}
```

```go
var reNextLabel = regexp.MustCompile(`(?i)^(next|next page|more results|›|»|>|→|sonraki|next ›|next »|next >|next →)$`)

// nextForm finds a "next page" button in a form and returns the request it
// would send (POST searches and DuckDuckGo page this way).
func nextForm(body []byte, pageURL string) *Request {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var out *Request
	gq.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		var submit *goquery.Selection
		form.Find(`input[type="submit"], button`).EachWithBreak(func(_ int, b *goquery.Selection) bool {
			if reNextLabel.MatchString(collapse(b.AttrOr("value", "") + " " + b.Text())) {
				submit = b
				return false
			}
			return true
		})
		if submit == nil {
			return true
		}
		action, err := base.Parse(form.AttrOr("action", ""))
		if err != nil {
			return true
		}
		v := url.Values{}
		form.Find("input[name]").Each(func(_ int, in *goquery.Selection) {
			switch strings.ToLower(in.AttrOr("type", "text")) {
			case "submit", "button", "image", "reset":
				return
			case "checkbox", "radio":
				if _, on := in.Attr("checked"); !on {
					return
				}
			}
			v.Add(in.AttrOr("name", ""), in.AttrOr("value", ""))
		})
		if n := submit.AttrOr("name", ""); n != "" {
			v.Add(n, submit.AttrOr("value", ""))
		}
		if strings.EqualFold(strings.TrimSpace(form.AttrOr("method", "")), "post") {
			out = &Request{Method: "POST", URL: action.String(), Form: v}
		} else {
			action.RawQuery = v.Encode()
			out = &Request{Method: "GET", URL: action.String()}
		}
		return false
	})
	return out
}
```

- [ ] **Step 5: Update round-1 test** — in `probe_test.go` `TestDiscoverSearchVariants`, change the post-only case to:

```go
		{"post-only", `<form action="/p" method="post"><input type="search" name="q"></form>`, "post", srv.URL + "/p"},
```

and delete the `if c.name == "post-only" && !strings.Contains(desc, "POST")` block (keep the `strings` import only if still used).

- [ ] **Step 6: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS

---

### Task 5: Search-engine fallback (DuckDuckGo)

**Files:**
- Create: `internal/sitecat/backend_engine.go`
- Modify: `internal/sitecat/backend.go` (`backendFor` knows `engine`)
- Modify: `internal/sitecat/docs.go` (results page notice for engine catalogs)
- Create: `internal/sitecat/main_test.go` (`TestMain` disables the engine)
- Test: `internal/sitecat/engine_test.go`

**Interfaces:**
- Consumes: `nextForm` (Task 4), `search.RealURL` (Task 1), `engineDetector` (Task 3)
- Produces: `var EngineEndpoint string`; `var ErrEngineCheck error`; `type engineBackend struct{}`; `func detectEngine(ctx context.Context, s *site) []candidate`

- [ ] **Step 1: `TestMain`** (`internal/sitecat/main_test.go`)

```go
package sitecat

import (
	"os"
	"testing"
)

// Unit tests never ask the real DuckDuckGo; tests that need the engine
// point EngineEndpoint at a fixture server.
func TestMain(m *testing.M) {
	EngineEndpoint = ""
	os.Exit(m.Run())
}
```

- [ ] **Step 2: Failing tests** (`internal/sitecat/engine_test.go`)

```go
package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ddg serves DuckDuckGo-shaped pages: page 1 by GET (with an advert, an
// other-site hit, a book page and a direct PDF), page 2 by the POST form.
func ddg(t *testing.T, host string, anomaly bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if anomaly {
			fmt.Fprint(w, `<html><body><div class="anomaly-modal__title">Unfortunately, bots use DuckDuckGo too.</div></body></html>`)
			return
		}
		r.ParseForm()
		if !strings.Contains(r.Form.Get("q"), "site:"+host) {
			t.Errorf("query = %q", r.Form.Get("q"))
		}
		res := func(href, title string) string {
			return `<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=` + href + `">` + title + `</a><a class="result__snippet">A classic.</a></div>`
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `<html><body>`+
				`<div class="result result--ad"><a class="result__a" href="https://ads.example/x">Ad</a></div>`+
				res("https%3A%2F%2Fother.example%2Fdracula", "Elsewhere")+
				res("https%3A%2F%2F"+host+"%2Fshowbook.php%3Fpid%3D1", "Dracula - Faded")+
				res("https%3A%2F%2Fwww."+host+"%2Flink.php%3Ffile%3D1-a5.pdf", "Dracula PDF")+
				`<form action="/html/" method="post"><input type="submit" value="Next"><input type="hidden" name="q" value="`+r.Form.Get("q")+`"><input type="hidden" name="s" value="10"></form></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><body>`+res("https%3A%2F%2F"+host+"%2Fshowbook.php%3Fpid%3D2", "Dracula's Guest")+`</body></html>`)
	}))
}

func TestEngineSearch(t *testing.T) {
	srv := ddg(t, "books.example", false)
	defer srv.Close()
	EngineEndpoint = srv.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	p := Profile{Search: SearchSpec{Kind: "engine", Template: "books.example"}}
	got, err := Search(context.Background(), testFetcher(), &p, "dracula", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 3 || got.Pages != 2 {
		t.Fatalf("results=%+v pages=%d", got.Results, got.Pages)
	}
	for _, r := range got.Results {
		if !strings.Contains(r.URL, "books.example") {
			t.Errorf("foreign result %+v", r)
		}
	}
	if d, ok := directDownload(got.Results[1].URL); !ok || d.Format != "pdf" {
		t.Errorf("file hit not downloadable: %+v", got.Results[1])
	}
	if got, _ := Search(context.Background(), testFetcher(), &p, "author:Bram Stoker", nil); !got.FieldIgnored {
		t.Error("the engine cannot search by field; that must be reported")
	}
}

func TestEngineAnomalyStops(t *testing.T) {
	srv := ddg(t, "books.example", true)
	defer srv.Close()
	EngineEndpoint = srv.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	p := Profile{Search: SearchSpec{Kind: "engine", Template: "books.example"}}
	if _, err := Search(context.Background(), testFetcher(), &p, "dracula", nil); !errors.Is(err, ErrEngineCheck) {
		t.Errorf("err = %v", err)
	}
}

func TestProbeFallsBackToEngine(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>JS Books</title></head><body><div id="app"></div><noscript>JS</noscript></body></html>`)
	}))
	defer site.Close()
	host := strings.TrimPrefix(site.URL, "http://")
	engine := ddg(t, strings.Split(host, ":")[0], false)
	defer engine.Close()
	EngineEndpoint = engine.URL + "/html/"
	defer func() { EngineEndpoint = "" }()
	rep, err := Probe(context.Background(), testFetcher(), site.URL, "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "engine" || rep.Profile.Search.MaxPages != 3 {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	last := rep.Findings[len(rep.Findings)-1].Text
	all := ""
	for _, f := range rep.Findings {
		all += f.Text + "\n"
	}
	if !strings.Contains(all, "DuckDuckGo") {
		t.Errorf("findings:\n%s (last %q)", all, last)
	}
}
```

Note: the fixture's result links point at `books.example`/the site's hostname, which the engine keeps by hostname only (ports ignored by `sameSite`).

- [ ] **Step 3: Run** `go test ./internal/sitecat -run Engine` → FAIL (`EngineEndpoint` undefined)

- [ ] **Step 4: Implement** (`internal/sitecat/backend_engine.go`)

```go
package sitecat

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
	"w5f/internal/search"
)

// EngineEndpoint is DuckDuckGo's HTML endpoint. "" disables the
// search-engine fallback (unit tests).
var EngineEndpoint = search.WebEndpoint

// ErrEngineCheck means DuckDuckGo asked for a human check; W5F stops.
var ErrEngineCheck = errors.New("DuckDuckGo asked for a verification; try again later")

func init() { engineDetector = detectEngine }

// detectEngine offers searching the site through DuckDuckGo (site:host).
func detectEngine(ctx context.Context, s *site) []candidate {
	if EngineEndpoint == "" {
		return nil
	}
	host := strings.TrimPrefix(s.Home.Hostname(), "www.")
	return []candidate{{Spec: SearchSpec{Kind: "engine", Template: host},
		Desc: "search engine (DuckDuckGo, site:" + host + "; results depend on its index)"}}
}

// engineBackend searches one site through DuckDuckGo's HTML results.
type engineBackend struct{}

func (engineBackend) First(p *Profile, q Query) (Request, bool) {
	words := q.Words
	if q.Field != "" { // no field search: keep the words together
		words = `"` + q.Words + `"`
	}
	u, _ := url.Parse(EngineEndpoint)
	u.RawQuery = url.Values{"q": {"site:" + p.Search.Template + " " + words}}.Encode()
	return Request{URL: u.String()}, q.Field != ""
}

func (engineBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	l := strings.ToLower(string(body))
	if strings.Contains(l, "anomaly-modal") || strings.Contains(l, "bots use duckduckgo") {
		return pageOut{}, ErrEngineCheck
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return pageOut{}, err
	}
	host := &url.URL{Host: p.Search.Template}
	var out pageOut
	gq.Find(".result").Each(func(_ int, s *goquery.Selection) {
		if s.HasClass("result--ad") {
			return
		}
		a := s.Find("a.result__a").First()
		href := search.RealURL(a.AttrOr("href", ""))
		u, err := url.Parse(href)
		if href == "" || err != nil || !sameSite(u, host) {
			return
		}
		extra := collapse(s.Find(".result__snippet").First().Text())
		if r := []rune(extra); len(r) > 160 {
			extra = string(r[:157]) + "…"
		}
		out.Results = append(out.Results, Result{Title: collapse(a.Text()), URL: href, Extra: extra})
	})
	out.Next = nextForm(body, pageURL)
	return out, nil
}

func (engineBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}
```

In `backend.go` `backendFor` add:

```go
	case "engine":
		return engineBackend{}
```

In `docs.go` `resultsDoc`, after the `summary` paragraph add:

```go
	if p.Search.Kind == "engine" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "Results via DuckDuckGo (site:" + p.Search.Template + ") — they depend on its index."})
	}
```

- [ ] **Step 5: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS

---

### Task 6: Internet Archive

**Files:**
- Create: `internal/sitecat/platform_archive.go`
- Modify: `internal/sitecat/backend.go` (`detectors` order, `backendFor`)
- Test: `internal/sitecat/archive_test.go`

**Interfaces:**
- Consumes: `candidate`, `backend`, `notDownloadable`, `fileFormatOf`, `sortStable` (downloads.go)
- Produces: `var ArchiveBase string`; `type archiveBackend struct{}`; `func detectArchive(ctx context.Context, s *site) []candidate`; `func archiveQuery(p *Profile, q Query) string`

- [ ] **Step 1: Failing tests** (`internal/sitecat/archive_test.go`)

```go
package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func archiveFixture(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/details/folklore", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Folklore : Internet Archive</title></head><body><app-root></app-root></body></html>`)
	})
	mux.HandleFunc("/advancedsearch.php", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		for _, must := range []string{"mediatype:texts", "-collection:inlibrary", "-collection:printdisabled", "-collection:lendinglibrary", "collection:(folklore)"} {
			if !strings.Contains(q, must) {
				t.Errorf("query %q lacks %q", q, must)
			}
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"response":{"numFound":3,"start":2,"docs":[{"identifier":"c","title":"Third","creator":["A","B"]}]}}`)
			return
		}
		fmt.Fprint(w, `{"response":{"numFound":3,"start":0,"docs":[{"identifier":"dracula1897","title":"Dracula","creator":"Stoker, Bram","year":1897},{"identifier":"lent","title":"Lent Book"}]}}`)
	})
	mux.HandleFunc("/metadata/dracula1897", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"metadata":{"title":"Dracula"},"files":[
{"name":"dracula.pdf","format":"Text PDF","size":"2000000","source":"original"},
{"name":"dracula.epub","format":"EPUB","size":"300000","source":"derivative"},
{"name":"dracula_meta.xml","format":"Metadata","size":"900"},
{"name":"secret.pdf","format":"Text PDF","size":"10","private":"true"},
{"name":"sub dir/extra.txt","format":"DjVuTXT","size":"50000"}]}`)
	})
	mux.HandleFunc("/metadata/lent", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"metadata":{"title":"Lent Book","access-restricted-item":"true"},"files":[{"name":"lent.pdf","size":"1"}]}`)
	})
	mux.HandleFunc("/metadata/big", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"metadata":{},"files":[`)
		for i := 0; i < 200; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"name":"issue%03d.pdf","size":"100"}`, i)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	})
	return srv
}

func TestArchiveCatalog(t *testing.T) {
	srv := archiveFixture(t)
	defer srv.Close()
	ArchiveBase = srv.URL
	defer func() { ArchiveBase = "https://archive.org" }()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL+"/details/folklore", "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "archive" || rep.Profile.Search.Collection != "folklore" {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	got, err := Search(context.Background(), f, &p, "dracula", nil)
	if err != nil || len(got.Results) != 3 || got.Pages != 2 {
		t.Fatalf("search: %v %+v", err, got)
	}
	if r := got.Results[0]; r.Title != "Dracula" || r.URL != srv.URL+"/details/dracula1897" || !strings.Contains(r.Extra, "Stoker") || !strings.Contains(r.Extra, "1897") {
		t.Errorf("first: %+v", r)
	}
	if !strings.Contains(got.Results[2].Extra, "A; B") {
		t.Errorf("creator list: %+v", got.Results[2])
	}
	ds, err := itemDownloads(context.Background(), f, &p, srv.URL+"/details/dracula1897")
	if err != nil || len(ds) != 3 || ds[0].Format != "epub" || ds[0].URL != srv.URL+"/download/dracula1897/dracula.epub" {
		t.Fatalf("downloads: %v %+v", err, ds)
	}
	if ds[2].URL != srv.URL+"/download/dracula1897/sub%20dir/extra.txt" {
		t.Errorf("escaped path: %s", ds[2].URL)
	}
	_, err = itemDownloads(context.Background(), f, &p, srv.URL+"/details/lent")
	var nd *notDownloadable
	if !errors.As(err, &nd) || !strings.Contains(nd.Reason, "lending") {
		t.Errorf("lending item: %v", err)
	}
	ds, _ = itemDownloads(context.Background(), f, &p, srv.URL+"/details/big")
	if len(ds) != 30 {
		t.Errorf("file list not capped: %d", len(ds))
	}
}

func TestArchiveQueryFields(t *testing.T) {
	p := &Profile{}
	if q := archiveQuery(p, Query{Field: "author", Words: `Stoker (Bram) "x":y`}); !strings.HasPrefix(q, "creator:(Stoker Bram x y)") {
		t.Errorf("author: %q", q)
	}
	if q := archiveQuery(p, Query{Field: "title", Words: "Dracula"}); !strings.HasPrefix(q, "title:(Dracula)") {
		t.Errorf("title: %q", q)
	}
	if q := archiveQuery(p, Query{Words: "vampire"}); !strings.HasPrefix(q, "(title:(vampire) OR creator:(vampire) OR subject:(vampire))") {
		t.Errorf("general: %q", q)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run Archive` → FAIL (`ArchiveBase` undefined)

- [ ] **Step 3: Implement** (`internal/sitecat/platform_archive.go`)

```go
package sitecat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"w5f/internal/fetch"
)

// ArchiveBase is the Internet Archive's address (tests use a fixture).
var ArchiveBase = "https://archive.org"

var (
	reArchiveCollection = regexp.MustCompile(`^/details/([^/?#]+)`)
	reLuceneSpecial     = regexp.MustCompile(`[():"\\\[\]{}^~*?!+\-&|/]+`)
)

// detectArchive recognises the Internet Archive; a /details/<collection>
// address restricts searches to that collection.
func detectArchive(ctx context.Context, s *site) []candidate {
	base, err := url.Parse(ArchiveBase)
	if err != nil || strings.TrimPrefix(s.Home.Hostname(), "www.") != strings.TrimPrefix(base.Hostname(), "www.") {
		return nil
	}
	spec := SearchSpec{Kind: "archive"}
	desc := "Internet Archive API (public texts only)"
	if m := reArchiveCollection.FindStringSubmatch(s.Home.Path); m != nil {
		spec.Collection = m[1]
		desc += ", collection " + m[1]
	}
	return []candidate{{Spec: spec, Desc: desc}}
}

// archiveQuery builds the advanced-search query: public texts only.
func archiveQuery(p *Profile, q Query) string {
	w := strings.Join(strings.Fields(reLuceneSpecial.ReplaceAllString(q.Words, " ")), " ")
	var s string
	switch q.Field {
	case "author":
		s = "creator:(" + w + ")"
	case "title":
		s = "title:(" + w + ")"
	default:
		s = "(title:(" + w + ") OR creator:(" + w + ") OR subject:(" + w + "))"
	}
	s += " AND mediatype:texts AND -collection:inlibrary AND -collection:printdisabled AND -collection:lendinglibrary"
	if p.Search.Collection != "" {
		s += " AND collection:(" + p.Search.Collection + ")"
	}
	return s
}

func archiveSearchURL(query string, page int) string {
	v := url.Values{"q": {query}, "fl[]": {"identifier", "title", "creator", "year"}, "rows": {"50"},
		"page": {strconv.Itoa(page)}, "output": {"json"}, "sort[]": {"downloads desc"}}
	return ArchiveBase + "/advancedsearch.php?" + v.Encode()
}

// archiveBackend searches the Internet Archive through its public API.
type archiveBackend struct{}

func (archiveBackend) First(p *Profile, q Query) (Request, bool) {
	return Request{URL: archiveSearchURL(archiveQuery(p, q), 1)}, false
}

// flexString reads a JSON string, number or list of strings.
func flexString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, "; ")
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func (archiveBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	var r struct {
		Response struct {
			NumFound int `json:"numFound"`
			Start    int `json:"start"`
			Docs     []struct {
				Identifier string          `json:"identifier"`
				Title      string          `json:"title"`
				Creator    json.RawMessage `json:"creator"`
				Year       json.RawMessage `json:"year"`
			} `json:"docs"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return pageOut{}, fmt.Errorf("unexpected answer from the Internet Archive API")
	}
	var out pageOut
	for _, d := range r.Response.Docs {
		extra := flexString(d.Creator)
		if y := flexString(d.Year); y != "" {
			if extra != "" {
				extra += " · "
			}
			extra += y
		}
		title := d.Title
		if title == "" {
			title = d.Identifier
		}
		out.Results = append(out.Results, Result{Title: title, URL: ArchiveBase + "/details/" + d.Identifier, Extra: extra})
	}
	if r.Response.Start+len(r.Response.Docs) < r.Response.NumFound && len(r.Response.Docs) > 0 {
		u, err := url.Parse(pageURL)
		if err == nil {
			v := u.Query()
			v.Set("page", strconv.Itoa(pageNo+1))
			u.RawQuery = v.Encode()
			out.Next = &Request{URL: u.String()}
		}
	}
	return out, nil
}

// Item lists an item's public book files (at most 30).
func (archiveBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	iu, err := url.Parse(itemURL)
	if err != nil {
		return nil, err
	}
	id := path.Base(strings.TrimSuffix(iu.Path, "/"))
	mu, _ := url.Parse(ArchiveBase + "/metadata/" + url.PathEscape(id))
	resp, err := f.Get(ctx, mu, fetch.Options{})
	if err != nil {
		return nil, err
	}
	var m struct {
		Metadata map[string]json.RawMessage `json:"metadata"`
		Files    []struct {
			Name    string          `json:"name"`
			Size    json.RawMessage `json:"size"`
			Private json.RawMessage `json:"private"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, fmt.Errorf("unexpected answer from the Internet Archive API")
	}
	if flexString(m.Metadata["access-restricted-item"]) == "true" {
		return nil, &notDownloadable{Reason: "This is a lending-library item on the Internet Archive — it can be borrowed there, not downloaded."}
	}
	var ds []Download
	for _, fl := range m.Files {
		if flexString(fl.Private) == "true" {
			continue
		}
		format := extFormats[strings.ToLower(path.Ext(fl.Name))]
		if format == "" {
			continue
		}
		segs := strings.Split(fl.Name, "/")
		for i := range segs {
			segs[i] = url.PathEscape(segs[i])
		}
		label := fl.Name
		if n, err := strconv.ParseInt(flexString(fl.Size), 10, 64); err == nil && n > 0 {
			label += fmt.Sprintf(" (%.1f MB)", float64(n)/(1<<20))
		}
		ds = append(ds, Download{URL: ArchiveBase + "/download/" + url.PathEscape(id) + "/" + strings.Join(segs, "/"), Format: format, Label: label})
		if len(ds) == 30 {
			break
		}
	}
	rank := func(f string) int {
		for i, x := range p.Download.Prefer {
			if x == f {
				return i
			}
		}
		return len(p.Download.Prefer)
	}
	sortStable(ds, func(a, b Download) bool { return rank(a.Format) < rank(b.Format) })
	return ds, nil
}
```

In `backend.go`: set `var detectors = []detector{detectArchive, detectHTML, detectBrowse}` and in `backendFor` add:

```go
	case "archive":
		return archiveBackend{}
```

Note: `Profile.ApplyDefaults` must be called before `Item` so `Download.Prefer` is filled; `itemDownloads` callers pass profiles loaded via `LoadAll`/`Probe`, which apply defaults. In the test the profile comes from `Probe`.

- [ ] **Step 4: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS

---

### Task 7: DSpace 7+

**Files:**
- Create: `internal/sitecat/platform_dspace.go`
- Modify: `internal/sitecat/backend.go` (`detectors`, `backendFor`)
- Test: `internal/sitecat/dspace_test.go`

**Interfaces:**
- Consumes: `candidate`, `backend`, `fileFormatOf`, `flexString` (Task 6)
- Produces: `type dspaceBackend struct{}`; `func detectDSpace(ctx context.Context, s *site) []candidate`

- [ ] **Step 1: Failing test** (`internal/sitecat/dspace_test.go`)

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

func TestDSpaceCatalog(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	api := srv.URL + "/server/api"
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Open Repository</title></head><body><ds-app></ds-app></body></html>`)
	})
	mux.HandleFunc("/server/api", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"dspaceName":"Open Repository","dspaceVersion":"DSpace 8.2","_links":{}}`)
	})
	mux.HandleFunc("/server/api/discover/search/objects", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("dsoType") != "ITEM" {
			t.Errorf("dsoType = %q", q.Get("dsoType"))
		}
		if strings.HasPrefix(q.Get("query"), "dc.contributor.author:") && q.Get("query") != "dc.contributor.author:(Stoker)" {
			t.Errorf("author query = %q", q.Get("query"))
		}
		obj := func(id, title, author string) string {
			return `{"_embedded":{"indexableObject":{"uuid":"` + id + `","name":"` + title + `","type":"item","metadata":{"dc.title":[{"value":"` + title + `"}],"dc.contributor.author":[{"value":"` + author + `"}],"dc.date.issued":[{"value":"1897"}]}}}}`
		}
		if q.Get("page") == "1" {
			fmt.Fprint(w, `{"_embedded":{"searchResult":{"page":{"number":1,"size":20,"totalPages":2,"totalElements":2},"_embedded":{"objects":[`+obj("u2", "Dracula's Guest", "Stoker, Bram")+`]}}}}`)
			return
		}
		fmt.Fprint(w, `{"_embedded":{"searchResult":{"page":{"number":0,"size":20,"totalPages":2,"totalElements":2},"_embedded":{"objects":[`+obj("u1", "Dracula", "Stoker, Bram")+`]}}}}`)
	})
	mux.HandleFunc("/server/api/core/items/u1/bundles", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"_embedded":{"bundles":[{"name":"THUMBNAIL","_links":{"bitstreams":{"href":"`+api+`/core/bundles/t/bitstreams"}}},{"name":"ORIGINAL","_links":{"bitstreams":{"href":"`+api+`/core/bundles/o/bitstreams"}}}]}}`)
	})
	mux.HandleFunc("/server/api/core/bundles/o/bitstreams", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"_embedded":{"bitstreams":[{"uuid":"b1","name":"dracula.pdf","sizeBytes":2097152,"_links":{"content":{"href":"`+api+`/core/bitstreams/b1/content"}}},{"uuid":"b2","name":"license.txt.xml","sizeBytes":5,"_links":{"content":{"href":"`+api+`/core/bitstreams/b2/content"}}}]}}`)
	})
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL, "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "dspace" || rep.Profile.Search.API != api {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	got, err := Search(context.Background(), f, &p, "dracula", nil)
	if err != nil || len(got.Results) != 2 || got.Pages != 2 {
		t.Fatalf("search: %v %+v", err, got)
	}
	if r := got.Results[0]; r.Title != "Dracula" || r.URL != srv.URL+"/items/u1" || r.Extra != "Stoker, Bram · 1897" {
		t.Errorf("first: %+v", r)
	}
	Search(context.Background(), f, &p, "author:Stoker", nil)
	ds, err := itemDownloads(context.Background(), f, &p, srv.URL+"/items/u1")
	if err != nil || len(ds) != 1 || ds[0].URL != api+"/core/bitstreams/b1/content" || ds[0].Format != "pdf" || !strings.Contains(ds[0].Label, "2.0 MB") {
		t.Errorf("downloads: %v %+v", err, ds)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run DSpace` → FAIL (probe does not pick `dspace`)

- [ ] **Step 3: Implement** (`internal/sitecat/platform_dspace.go`)

```go
package sitecat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"w5f/internal/fetch"
)

// detectDSpace recognises DSpace 7+ repositories (an Angular "ds-app"
// front end) and checks their REST API.
func detectDSpace(ctx context.Context, s *site) []candidate {
	if !strings.Contains(strings.ToLower(string(s.Body)), "<ds-app") && !strings.Contains(s.Generator, "dspace") {
		return nil
	}
	api := s.Home.Scheme + "://" + s.Home.Host + "/server/api"
	u, _ := url.Parse(api)
	resp, err := s.F.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil
	}
	var root struct {
		Version string `json:"dspaceVersion"`
	}
	if json.Unmarshal(resp.Body, &root) != nil || root.Version == "" {
		return nil
	}
	return []candidate{{Spec: SearchSpec{Kind: "dspace", API: api}, Desc: root.Version + " REST API"}}
}

// dspaceBackend searches a DSpace 7+ repository through its REST API.
type dspaceBackend struct{}

func (dspaceBackend) First(p *Profile, q Query) (Request, bool) {
	w := strings.Join(strings.Fields(reLuceneSpecial.ReplaceAllString(q.Words, " ")), " ")
	query := q.Words
	switch q.Field {
	case "author":
		query = "dc.contributor.author:(" + w + ")"
	case "title":
		query = "dc.title:(" + w + ")"
	}
	v := url.Values{"query": {query}, "dsoType": {"ITEM"}, "page": {"0"}, "size": {"20"}}
	return Request{URL: p.Search.API + "/discover/search/objects?" + v.Encode()}, false
}

type dspaceValues map[string][]struct {
	Value string `json:"value"`
}

func (m dspaceValues) all(key string) []string {
	var out []string
	for _, v := range m[key] {
		out = append(out, v.Value)
	}
	return out
}

func (dspaceBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	var r struct {
		Embedded struct {
			SearchResult struct {
				Page struct {
					Number     int `json:"number"`
					TotalPages int `json:"totalPages"`
				} `json:"page"`
				Embedded struct {
					Objects []struct {
						Embedded struct {
							Object struct {
								UUID     string       `json:"uuid"`
								Name     string       `json:"name"`
								Metadata dspaceValues `json:"metadata"`
							} `json:"indexableObject"`
						} `json:"_embedded"`
					} `json:"objects"`
				} `json:"_embedded"`
			} `json:"searchResult"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return pageOut{}, fmt.Errorf("unexpected answer from the DSpace API")
	}
	api, _ := url.Parse(p.Search.API)
	origin := api.Scheme + "://" + api.Host
	sr := r.Embedded.SearchResult
	var out pageOut
	for _, o := range sr.Embedded.Objects {
		obj := o.Embedded.Object
		title := obj.Name
		if t := obj.Metadata.all("dc.title"); len(t) > 0 {
			title = t[0]
		}
		extra := strings.Join(obj.Metadata.all("dc.contributor.author"), "; ")
		if d := obj.Metadata.all("dc.date.issued"); len(d) > 0 {
			if extra != "" {
				extra += " · "
			}
			extra += d[0]
		}
		out.Results = append(out.Results, Result{Title: title, URL: origin + "/items/" + obj.UUID, Extra: extra})
	}
	if sr.Page.Number+1 < sr.Page.TotalPages {
		if u, err := url.Parse(pageURL); err == nil {
			v := u.Query()
			v.Set("page", strconv.Itoa(sr.Page.Number+1))
			u.RawQuery = v.Encode()
			out.Next = &Request{URL: u.String()}
		}
	}
	return out, nil
}

// Item lists the book files of an item's ORIGINAL bundle.
func (dspaceBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	iu, err := url.Parse(itemURL)
	if err != nil {
		return nil, err
	}
	uuid := path.Base(strings.TrimSuffix(iu.Path, "/"))
	getJSON := func(u string, out any) error {
		pu, err := url.Parse(u)
		if err != nil {
			return err
		}
		resp, err := f.Get(ctx, pu, fetch.Options{})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return fmt.Errorf("unexpected answer from the DSpace API")
		}
		return nil
	}
	var bundles struct {
		Embedded struct {
			Bundles []struct {
				Name  string `json:"name"`
				Links struct {
					Bitstreams struct {
						Href string `json:"href"`
					} `json:"bitstreams"`
				} `json:"_links"`
			} `json:"bundles"`
		} `json:"_embedded"`
	}
	if err := getJSON(p.Search.API+"/core/items/"+url.PathEscape(uuid)+"/bundles", &bundles); err != nil {
		return nil, err
	}
	var ds []Download
	for _, b := range bundles.Embedded.Bundles {
		if b.Name != "ORIGINAL" {
			continue
		}
		var bs struct {
			Embedded struct {
				Bitstreams []struct {
					Name  string `json:"name"`
					Size  int64  `json:"sizeBytes"`
					Links struct {
						Content struct {
							Href string `json:"href"`
						} `json:"content"`
					} `json:"_links"`
				} `json:"bitstreams"`
			} `json:"_embedded"`
		}
		if err := getJSON(b.Links.Bitstreams.Href, &bs); err != nil {
			return nil, err
		}
		for _, x := range bs.Embedded.Bitstreams {
			format := extFormats[strings.ToLower(path.Ext(x.Name))]
			if format == "" {
				continue
			}
			label := x.Name
			if x.Size > 0 {
				label += fmt.Sprintf(" (%.1f MB)", float64(x.Size)/(1<<20))
			}
			ds = append(ds, Download{URL: x.Links.Content.Href, Format: format, Label: label})
		}
	}
	return ds, nil
}
```

In `backend.go`: `var detectors = []detector{detectArchive, detectDSpace, detectHTML, detectBrowse}` and in `backendFor`:

```go
	case "dspace":
		return dspaceBackend{}
```

- [ ] **Step 4: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS

---

### Task 8: Platform hints, Calibre `/opds`, smarter download links

**Files:**
- Create: `internal/sitecat/platform_hints.go` (`detectHints`, `detectOPDSPath`)
- Modify: `internal/sitecat/downloads.go` (`FindIn`: `fileFormatOf`, OJS galley rewrite)
- Modify: `internal/sitecat/backend.go` (`detectors` order)
- Test: `internal/sitecat/hints_test.go`

**Interfaces:**
- Consumes: `site`, `candidate`, `opdsTemplate` (discover.go), `fileFormatOf` (Task 3)
- Produces: `func detectHints(ctx context.Context, s *site) []candidate`; `func detectOPDSPath(ctx context.Context, s *site) []candidate`

- [ ] **Step 1: Failing tests** (`internal/sitecat/hints_test.go`)

```go
package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"w5f/internal/fetch"
)

func siteOf(t *testing.T, home string, body string) *site {
	u, _ := url.Parse(home)
	return newSite(context.Background(), testFetcher(), &fetch.Response{URL: u, Body: []byte(body)})
}

func TestPlatformHints(t *testing.T) {
	cases := []struct{ home, body, want string }{
		{"https://journals.example/index.php/folk/index", `<meta name="generator" content="Open Journal Systems 3.3.0.8">`,
			"https://journals.example/index.php/folk/search/search?query={q}"},
		{"https://eprints.example/", `<meta name="generator" content="EPrints 3.4">`,
			"https://eprints.example/cgi/search/simple?q={q}"},
		{"https://wiki.example/wiki/Main_Page", `<meta name="generator" content="MediaWiki 1.41"><link rel="EditURI" type="application/rsd+xml" href="https://wiki.example/w/api.php?action=rsd">`,
			"https://wiki.example/w/index.php?search={q}&title=Special:Search&fulltext=1&ns0=1&ns6=1"},
		{"https://occultblog.example/", `<meta content='blogger' name='generator'/>`,
			"https://occultblog.example/search?q={q}"},
	}
	for _, c := range cases {
		cs := detectHints(context.Background(), siteOf(t, c.home, "<html><head>"+c.body+"</head><body></body></html>"))
		if len(cs) != 1 || cs[0].Spec.Kind != "form" || cs[0].Spec.Template != c.want {
			t.Errorf("%s: %+v", c.home, cs)
		}
	}
	if cs := detectHints(context.Background(), siteOf(t, "https://plain.example/", "<html></html>")); len(cs) != 0 {
		t.Errorf("no generator, no hints: %+v", cs)
	}
}

func TestCalibreOPDSPath(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/opds", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom"><link rel="search" type="application/atom+xml" href="/opds/search/{searchTerms}"/></feed>`)
	})
	s := siteOf(t, srv.URL+"/", `<html><head><title>Calibre-Web | Books</title></head><body></body></html>`)
	cs := detectOPDSPath(context.Background(), s)
	if len(cs) != 1 || cs[0].Spec.Kind != "opds" || cs[0].Spec.Template != srv.URL+"/opds/search/{q}" {
		t.Errorf("candidates: %+v", cs)
	}
	if cs := detectOPDSPath(context.Background(), siteOf(t, srv.URL+"/", "<html><title>Blog</title></html>")); len(cs) != 0 {
		t.Error("sites without calibre/opds markers are not probed")
	}
}

func TestFindInOJSGalleyAndQueryFiles(t *testing.T) {
	body := page(`<a class="obj_galley_link pdf" href="/index.php/folk/article/view/12/34">PDF</a>
<a href="/link.php?file=20170553-a5.pdf">Download (A5)</a>`)
	ds := FindIn(body, "https://journals.example/index.php/folk/article/view/12", DefaultPrefer)
	if len(ds) != 2 || ds[0].URL != "https://journals.example/index.php/folk/article/download/12/34" || ds[0].Format != "pdf" ||
		ds[1].URL != "https://journals.example/link.php?file=20170553-a5.pdf" {
		t.Errorf("downloads: %+v", ds)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'Hints|CalibreOPDS|OJSGalley'` → FAIL (`detectHints` undefined)

- [ ] **Step 3: Implement** (`internal/sitecat/platform_hints.go`)

```go
package sitecat

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var reOJSJournal = regexp.MustCompile(`^(/index\.php/[^/]+)`)

// detectHints adds the known search addresses of common platforms, for
// when their own search form is not found on the page.
func detectHints(ctx context.Context, s *site) []candidate {
	origin := s.Home.Scheme + "://" + s.Home.Host
	var out []candidate
	add := func(t, desc string) {
		out = append(out, candidate{Spec: SearchSpec{Kind: "form", Template: t}, Desc: desc})
	}
	switch g := s.Generator; {
	case strings.Contains(g, "open journal systems"):
		if m := reOJSJournal.FindStringSubmatch(s.Home.Path); m != nil {
			add(origin+m[1]+"/search/search?query={q}", "OJS journal search")
		}
	case strings.Contains(g, "eprints"):
		add(origin+"/cgi/search/simple?q={q}", "EPrints search")
	case strings.Contains(g, "mediawiki"):
		add(origin+mediawikiScript(s)+"/index.php?search={q}&title=Special:Search&fulltext=1&ns0=1&ns6=1", "MediaWiki search (pages and files)")
	case strings.Contains(g, "blogger"):
		add(origin+"/search?q={q}", "Blogger search")
	}
	return out
}

// mediawikiScript finds the wiki's script path ("/w") from its EditURI link.
func mediawikiScript(s *site) string {
	if s.Doc == nil {
		return ""
	}
	if h, ok := s.Doc.Find(`link[rel="EditURI"]`).First().Attr("href"); ok {
		if u, err := s.Home.Parse(h); err == nil {
			return strings.TrimSuffix(path.Dir(u.Path), "/")
		}
	}
	return ""
}

// detectOPDSPath probes the /opds address used by Calibre's content server
// and Calibre-Web, only on pages that mention calibre or OPDS.
func detectOPDSPath(ctx context.Context, s *site) []candidate {
	l := strings.ToLower(string(s.Body))
	if !strings.Contains(l, "calibre") && !strings.Contains(l, "opds") {
		return nil
	}
	u := &url.URL{Scheme: s.Home.Scheme, Host: s.Home.Host, Path: "/opds"}
	if t := opdsTemplate(ctx, s.F, s.Home, u.String()); t != "" {
		return []candidate{{Spec: SearchSpec{Kind: "opds", Template: t}, Desc: "OPDS catalog at " + u.String()}}
	}
	return nil
}
```

In `backend.go`: `var detectors = []detector{detectArchive, detectDSpace, detectHTML, detectHints, detectOPDSPath, detectBrowse}`.

- [ ] **Step 4: `FindIn`** — in `downloads.go` inside `FindIn`, replace

```go
		format := extFormats[strings.ToLower(path.Ext(u.Path))]
```

with

```go
		format := fileFormatOf(u)
		// OJS: a galley's "PDF" link opens a viewer; its download sits at
		// …/article/download/<id>/<galley>.
		if format == "" {
			if m := reOJSView.FindStringSubmatch(u.Path); m != nil {
				if w := reFormatWord.FindString(label); w != "" {
					u.Path = m[1] + "/download/" + m[2]
					format = wordFormat(w)
				}
			}
		}
```

and add to the `var (…)` block in `downloads.go`:

```go
	reOJSView    = regexp.MustCompile(`^(.*/article)/view/(\d+/\d+)/?$`)
```

(If `path` becomes unused in `downloads.go`, the compiler says so; it is still used in `FindDownloads`.)

- [ ] **Step 5: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS

---

### Task 9: Directory listings

**Files:**
- Create: `internal/sitecat/dirindex.go` (listing reader, crawler, index file, folding)
- Create: `internal/sitecat/backend_index.go` (index backend, detector, browse page)
- Modify: `internal/sitecat/backend.go` (`detectors`, `backendFor`)
- Modify: `internal/sitecat/docs.go` (add: build the index; recheck: rebuild; catalog page lists folders; `?dir=` browsing)
- Test: `internal/sitecat/dirindex_test.go`

**Interfaces:**
- Consumes: `candidate`, `localSearcher`, `localProber`, `directDownload`, `fileFormatOf` (Task 3), `CurrentProgress`
- Produces:
  - `type indexFile struct { Path, Size string }`; `type dirIndex struct { Root string; Built time.Time; Partial bool; Folders, Failed int; Files []indexFile }`
  - `func readListing(body []byte, pageURL string, root *url.URL) (folders []string, files []indexFile, ok bool)`
  - `func crawl(ctx context.Context, f *fetch.Fetcher, root string, maxFolders, maxDepth int, progress func(folders, files int)) (*dirIndex, error)`
  - `var crawlGap = time.Second`
  - `func saveIndex(path string, ix *dirIndex) error`, `func loadIndex(path string) (*dirIndex, error)`
  - `func fold(s string) string`
  - `func buildIndex(ctx context.Context, f *fetch.Fetcher, p *Profile) error`
  - `func dirDoc(ctx context.Context, env Env, p Profile, dir string) (*doc.Document, error)`

- [ ] **Step 1: Failing tests** (`internal/sitecat/dirindex_test.go`)

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
)

// A small book archive: Apache table listings, an nginx pre listing, a
// loop back to the root and links that leave the tree.
func archiveTree(t *testing.T) *httptest.Server {
	apache := func(title string, rows ...string) string {
		return `<html><head><title>Index of ` + title + `</title></head><body><h1>Index of ` + title + `</h1><table>
<tr><th><a href="?C=N;O=D">Name</a></th><th><a href="?C=S;O=A">Size</a></th></tr>
<tr><td><a href="../">Parent Directory</a></td><td>-</td></tr>` + strings.Join(rows, "") + `</table></body></html>`
	}
	row := func(href, size string) string { return `<tr><td><a href="` + href + `">` + href + `</a></td><td>` + size + `</td></tr>` }
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><body><a href="/library/">Library</a></body></html>`)
		case "/library/":
			fmt.Fprint(w, apache("/library", row("occult/", "-"), row("folklore/", "-"), row("Readme.html", "1K"),
				row("Lovecraft_-_Supernatural_Horror.pdf", "1.2M"), row("/elsewhere/", "-"), row("http://other.example/x.pdf", "1M")))
		case "/library/occult/":
			fmt.Fprint(w, apache("/library/occult", row("Agrippa%20-%20Üç%20Kitap.epub", "300K"), row("deeper/", "-")))
		case "/library/occult/deeper/":
			fmt.Fprint(w, apache("/library/occult/deeper", row("/library/", "-"), row("Grimoire.djvu", "5M")))
		case "/library/folklore/":
			fmt.Fprint(w, `<html><head><title>Index of /library/folklore/</title></head><body><h1>Index of /library/folklore/</h1><hr><pre><a href="../">../</a>
<a href="Turkish_Tales.txt">Turkish_Tales.txt</a>                  12-Mar-2019 10:21              48213
<a href="notes.docx">notes.docx</a>                         12-Mar-2019 10:21              1000
</pre><hr></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestReadListing(t *testing.T) {
	root, _ := url.Parse("https://b.example/library/")
	body := []byte(`<html><head><title>Index of /library</title></head><body><table>
<tr><td><a href="../">Parent Directory</a></td></tr>
<tr><td><a href="?C=M;O=A">Last modified</a></td></tr>
<tr><td><a href="sub/">sub/</a></td><td>-</td></tr>
<tr><td><a href="Book.epub">Book.epub</a></td><td>2019-03-12 10:21</td><td>1.2M</td></tr></table></body></html>`)
	folders, files, ok := readListing(body, "https://b.example/library/", root)
	if !ok || len(folders) != 1 || folders[0] != "https://b.example/library/sub/" || len(files) != 1 || files[0].Size != "1.2M" {
		t.Errorf("ok=%v folders=%v files=%+v", ok, folders, files)
	}
	if _, _, ok := readListing([]byte(`<html><title>Shop</title><a href="/a">a</a></html>`), "https://b.example/", root); ok {
		t.Error("an ordinary page is not a listing")
	}
}

func TestCrawlStaysInsideAndStopsAtLimits(t *testing.T) {
	srv := archiveTree(t)
	defer srv.Close()
	crawlGap = 0
	defer func() { crawlGap = 1e9 }()
	ix, err := crawl(context.Background(), testFetcher(), srv.URL+"/library/", 150, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range ix.Files {
		names = append(names, f.Path[strings.LastIndex(f.Path, "/")+1:])
	}
	if ix.Folders != 4 || len(ix.Files) != 4 || ix.Partial {
		t.Errorf("folders=%d partial=%v files=%v", ix.Folders, ix.Partial, names)
	}
	ix, _ = crawl(context.Background(), testFetcher(), srv.URL+"/library/", 2, 5, nil)
	if !ix.Partial || ix.Folders != 2 {
		t.Errorf("limit: folders=%d partial=%v", ix.Folders, ix.Partial)
	}
	ix, _ = crawl(context.Background(), testFetcher(), srv.URL+"/library/", 150, 1, nil)
	for _, f := range ix.Files {
		if strings.Contains(f.Path, "/deeper/") {
			t.Errorf("depth limit ignored: %s", f.Path)
		}
	}
}

func TestIndexCatalogEndToEnd(t *testing.T) {
	srv := archiveTree(t)
	defer srv.Close()
	crawlGap = 0
	defer func() { crawlGap = 1e9 }()
	env, db := testEnv(t)
	defer db.Close()
	ctx := context.Background()
	add := "w5f:catalog/add?" + url.Values{"url": {srv.URL + "/library/"}, "w": {"lovecraft"}}.Encode()
	if _, err := Route(ctx, add, env); err != nil {
		t.Fatal(err)
	}
	ps, _ := LoadAll(env.Path)
	if len(ps) != 1 || ps[0].Search.Kind != "index" || filepath.Dir(ps[0].Search.Index) != filepath.Join(filepath.Dir(env.Path), "catalogs") {
		t.Fatalf("profile: %+v", ps)
	}
	p := ps[0]
	for q, want := range map[string]string{"uc kitap": "Agrippa - Üç Kitap", "turkish tales": "Turkish Tales", "grimoire": "Grimoire", "SUPERNATURAL horror": "Lovecraft - Supernatural Horror"} {
		got, err := Search(ctx, env.Fetcher, &p, q, nil)
		if err != nil || len(got.Results) != 1 || got.Results[0].Title != want {
			t.Errorf("%q: %v %+v", q, err, got)
		}
	}
	d, err := Route(ctx, "w5f:catalog/"+p.ID, env)
	if err != nil || !strings.Contains(flat(d), "occult/") || !strings.Contains(flat(d), "4 book files") {
		t.Errorf("catalog page: %v\n%s", err, flat(d))
	}
	d, err = Route(ctx, "w5f:catalog/"+p.ID+"?"+url.Values{"dir": {srv.URL + "/library/occult/"}}.Encode(), env)
	if err != nil || !strings.Contains(flat(d), "Agrippa - Üç Kitap.epub") {
		t.Errorf("folder page: %v\n%s", err, flat(d))
	}
	if _, err := Route(ctx, "w5f:catalog/"+p.ID+"?"+url.Values{"dir": {"https://other.example/"}}.Encode(), env); err == nil {
		t.Error("browsing outside the catalog root must be refused")
	}
}
```

Add this helper to `internal/sitecat/docs_test.go` (used above):

```go
func testEnv(t *testing.T) (Env, *store.DB) {
	t.Helper()
	t.Setenv("W5F_BOOKS", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	return Env{Fetcher: testFetcher(), DB: db, Path: filepath.Join(t.TempDir(), "catalogs.toml"),
		OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) { return &doc.Document{Title: b.Title}, nil }}, db
}
```

- [ ] **Step 2: Run** `go test ./internal/sitecat -run 'ReadListing|Crawl|IndexCatalog'` → FAIL (`readListing` undefined)

- [ ] **Step 3: Implement** (`internal/sitecat/dirindex.go`)

```go
package sitecat

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"net/url"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"w5f/internal/fetch"
)

// indexFile is one book file of a directory catalog.
type indexFile struct {
	Path string `json:"path"` // absolute URL
	Size string `json:"size,omitempty"`
}

// dirIndex is the stored file list of a directory catalog.
type dirIndex struct {
	Root    string      `json:"root"`
	Built   time.Time   `json:"built"`
	Partial bool        `json:"partial"`
	Folders int         `json:"folders"`
	Failed  int         `json:"failed"`
	Files   []indexFile `json:"files"`
}

// crawlGap is the pause between folder requests while indexing.
var crawlGap = time.Second

var (
	reIndexTitle = regexp.MustCompile(`(?i)^(index of|directory listing for) /`)
	reSizeCell   = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s?[KMGT]i?B?$|^\d+$`)
	reSizeTail   = regexp.MustCompile(`(\d+(?:\.\d+)?[KMGT]?)\s*$`)
)

// readListing reads a directory listing: sub-folders below the current
// page and book files below root. ok is false for other pages.
func readListing(body []byte, pageURL string, root *url.URL) (folders []string, files []indexFile, ok bool) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, false
	}
	parent := gq.Find(`a[href="../"], a[href=".."]`).Length() > 0
	if !reIndexTitle.MatchString(collapse(gq.Find("title").First().Text())) &&
		!(parent && gq.Find("pre a[href], table a[href]").Length() >= 3) {
		return nil, nil, false
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, nil, false
	}
	seen := map[string]bool{}
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		raw := strings.TrimSpace(a.AttrOr("href", ""))
		if raw == "" || strings.HasPrefix(raw, "?") || strings.HasPrefix(raw, "#") {
			return // sort links and anchors
		}
		u, err := base.Parse(raw)
		if err != nil || u.Host != root.Host || !strings.HasPrefix(u.Path, root.Path) {
			return // other hosts, or outside the catalog's tree
		}
		u.RawQuery, u.Fragment = "", ""
		if seen[u.String()] {
			return
		}
		seen[u.String()] = true
		if strings.HasSuffix(u.Path, "/") {
			if len(u.Path) > len(base.Path) && strings.HasPrefix(u.Path, base.Path) {
				folders = append(folders, u.String()) // strictly deeper only
			}
			return
		}
		if fileFormatOf(u) != "" {
			files = append(files, indexFile{Path: u.String(), Size: sizeNear(a)})
		}
	})
	return folders, files, true
}

// sizeNear reads a file's size from its table row or pre line.
func sizeNear(a *goquery.Selection) string {
	if tr := a.Closest("tr"); tr.Length() > 0 {
		size := ""
		tr.Find("td").Each(func(_ int, td *goquery.Selection) {
			if t := collapse(td.Text()); reSizeCell.MatchString(t) {
				size = t
			}
		})
		return size
	}
	if n := a.Get(0).NextSibling; n != nil && n.Type == html.TextNode {
		line := strings.TrimSpace(strings.SplitN(n.Data, "\n", 2)[0])
		if m := reSizeTail.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// crawl indexes a listing tree breadth-first within the limits. On
// cancellation the index so far is returned, marked partial.
func crawl(ctx context.Context, f *fetch.Fetcher, root string, maxFolders, maxDepth int, progress func(folders, files int)) (*dirIndex, error) {
	ru, err := url.Parse(root)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(ru.Path, "/") {
		ru.Path = ru.Path[:strings.LastIndex(ru.Path, "/")+1]
	}
	ix := &dirIndex{Root: ru.String(), Built: time.Now().UTC()}
	type folder struct {
		u     string
		depth int
	}
	queue := []folder{{ru.String(), 0}}
	seen := map[string]bool{ru.String(): true}
	for len(queue) > 0 {
		if ix.Folders >= maxFolders {
			ix.Partial = true
			break
		}
		if ix.Folders > 0 && crawlGap > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(crawlGap):
			}
		}
		if err := ctx.Err(); err != nil {
			ix.Partial = true
			return ix, err
		}
		it := queue[0]
		queue = queue[1:]
		u, _ := url.Parse(it.u)
		resp, err := f.Get(ctx, u, fetch.Options{})
		ix.Folders++
		if err != nil {
			if ctx.Err() != nil {
				ix.Partial = true
				return ix, ctx.Err()
			}
			ix.Failed++
			continue
		}
		folders, files, ok := readListing(resp.Body, resp.URL.String(), ru)
		if !ok {
			ix.Failed++
			continue
		}
		ix.Files = append(ix.Files, files...)
		for _, sub := range folders {
			if !seen[sub] && it.depth+1 <= maxDepth {
				seen[sub] = true
				queue = append(queue, folder{sub, it.depth + 1})
			}
		}
		if progress != nil {
			progress(ix.Folders, len(ix.Files))
		}
	}
	return ix, nil
}

func saveIndex(path string, ix *dirIndex) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(ix)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadIndex(path string) (*dirIndex, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ix dirIndex
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, err
	}
	return &ix, nil
}

var foldReplacer = strings.NewReplacer(
	"_", " ", "-", " ", ".", " ", "İ", "i", "I", "i", "ı", "i", "ş", "s", "ğ", "g", "ü", "u", "ö", "o", "ç", "c",
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "õ", "o", "ú", "u", "ù", "u", "û", "u",
	"ñ", "n", "ý", "y", "ÿ", "y", "æ", "ae", "œ", "oe", "ß", "ss")

// fold makes file names and queries comparable: lower case, no accents,
// separators as spaces.
func fold(s string) string {
	return foldReplacer.Replace(strings.ToLower(foldReplacer.Replace(s)))
}
```

(The first `Replace` handles `İ`/`I` before `ToLower` turns `İ` into `i̇`; the second handles lower-cased accents.)

- [ ] **Step 4: Implement** (`internal/sitecat/backend_index.go`)

```go
package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// detectIndex recognises a directory listing ("Index of /…").
func detectIndex(ctx context.Context, s *site) []candidate {
	if _, _, ok := readListing(s.Body, s.Home.String(), s.Home); !ok {
		return nil
	}
	return []candidate{{Spec: SearchSpec{Kind: "index", Template: s.Home.String()}, Desc: "file listing (" + s.Home.String() + ")"}}
}

// indexBackend searches a directory catalog through its stored file list.
type indexBackend struct{}

func (indexBackend) First(p *Profile, q Query) (Request, bool) { return Request{}, false }

func (indexBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	return pageOut{}, nil
}

func (indexBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	if d, ok := directDownload(itemURL); ok {
		return []Download{d}, nil
	}
	return nil, nil
}

// fileResult turns an indexed file into a result: name as title, folder and
// size as extra.
func fileResult(root string, fl indexFile) Result {
	rel := strings.TrimPrefix(fl.Path, root)
	if un, err := url.PathUnescape(rel); err == nil {
		rel = un
	}
	name := path.Base(rel)
	title := strings.TrimSuffix(name, path.Ext(name))
	title = strings.Join(strings.Fields(strings.ReplaceAll(title, "_", " ")), " ")
	extra := strings.TrimSuffix(path.Dir(rel), ".")
	if fl.Size != "" {
		if extra != "" {
			extra += " · "
		}
		extra += fl.Size
	}
	return Result{Title: title, URL: fl.Path, Extra: extra}
}

func (indexBackend) SearchLocal(p *Profile, q Query) ([]Result, bool, error) {
	ix, err := loadIndex(p.Search.Index)
	if err != nil {
		return nil, false, fmt.Errorf("the file list of this catalog is missing — re-check it (g → catalogs)")
	}
	words := strings.Fields(fold(q.Words))
	var out []Result
	for _, fl := range ix.Files {
		rel := strings.TrimPrefix(fl.Path, ix.Root)
		if un, err := url.PathUnescape(rel); err == nil {
			rel = un
		}
		hay := fold(rel)
		all := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				all = false
				break
			}
		}
		if all {
			out = append(out, fileResult(ix.Root, fl))
		}
	}
	return out, q.Field != "", nil
}

// ProbeLocal reads the first folders only (the full index is built when
// the catalog is added).
func (indexBackend) ProbeLocal(ctx context.Context, f *fetch.Fetcher, p *Profile, word string) ([]Result, string, error) {
	ix, err := crawl(ctx, f, p.Search.Template, 10, 2, nil)
	if err != nil {
		return nil, "", err
	}
	if len(ix.Files) == 0 {
		return nil, "", errors.New("no book files in the first folders")
	}
	var rs []Result
	for _, fl := range ix.Files {
		rs = append(rs, fileResult(ix.Root, fl))
	}
	return rs, fmt.Sprintf("%d book files in the first %d folders; the full list is built when the catalog is added (up to %d folders)", len(ix.Files), ix.Folders, p.Search.MaxFolders), nil
}

// buildIndex crawls a directory catalog and stores its file list; a
// cancelled crawl keeps what it found (marked partial).
func buildIndex(ctx context.Context, f *fetch.Fetcher, p *Profile) error {
	defer CurrentProgress.Store("")
	ix, err := crawl(ctx, f, p.Search.Template, p.Search.MaxFolders, 5, func(folders, files int) {
		CurrentProgress.Store(fmt.Sprintf("indexing folder %d · %d files", folders, files))
	})
	if ix != nil {
		if serr := saveIndex(p.Search.Index, ix); serr != nil {
			return serr
		}
	}
	return err
}

// dirDoc lists one folder of a directory catalog (read live).
func dirDoc(ctx context.Context, env Env, p Profile, dir string) (*doc.Document, error) {
	root, err := url.Parse(p.Search.Template)
	if err != nil {
		return nil, err
	}
	du, err := url.Parse(dir)
	if err != nil || du.Host != root.Host || !strings.HasPrefix(du.Path, root.Path) {
		return nil, errors.New("that folder is outside this catalog")
	}
	resp, err := env.Fetcher.Get(ctx, du, fetch.Options{})
	if err != nil {
		return nil, err
	}
	folders, files, ok := readListing(resp.Body, resp.URL.String(), root)
	if !ok {
		return nil, errors.New("this folder no longer shows a file listing")
	}
	rel := strings.TrimPrefix(du.Path, root.Path)
	if un, err := url.PathUnescape(rel); err == nil {
		rel = un
	}
	d := &doc.Document{Title: p.Name + ": /" + rel, URL: "w5f:catalog/" + p.ID + "?" + url.Values{"dir": {dir}}.Encode(), Origin: "live", Lang: "en"}
	if ix, err := loadIndex(p.Search.Index); err == nil && dir == p.Search.Template {
		status := fmt.Sprintf("%d book files indexed in %d folders on %s", len(ix.Files), ix.Folders, ix.Built.Format("2006-01-02"))
		if ix.Failed > 0 {
			status += fmt.Sprintf("; %d folders could not be read", ix.Failed)
		}
		if ix.Partial {
			status += " (partial — re-check to continue)"
		}
		d.Blocks = append(d.Blocks, para(doc.Span{Text: status, Style: doc.Italic}),
			para(doc.Span{Text: "Search file names: g → cat " + p.ID + " <words>", Style: doc.Italic}))
	}
	var items [][]doc.Block
	for _, fo := range folders {
		name := strings.TrimPrefix(fo, du.String())
		if un, err := url.PathUnescape(name); err == nil {
			name = un
		}
		href := "w5f:catalog/" + p.ID + "?" + url.Values{"dir": {fo}}.Encode()
		items = append(items, []doc.Block{para(doc.Span{Text: name, Style: doc.Bold, Link: link(d, href, name)})})
	}
	for _, fl := range files {
		r := fileResult(root.String(), fl)
		name := path.Base(strings.TrimPrefix(fl.Path, du.String()))
		if un, err := url.PathUnescape(name); err == nil {
			name = un
		}
		href := "w5f:catalog/" + p.ID + "/item?" + url.Values{"u": {fl.Path}, "t": {r.Title}}.Encode()
		sp := []doc.Span{{Text: name, Link: link(d, href, name)}}
		if fl.Size != "" {
			sp = append(sp, doc.Span{Text: "  " + fl.Size, Style: doc.Italic})
		}
		items = append(items, []doc.Block{para(sp...)})
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "This folder has no sub-folders or book files.", Style: doc.Italic}))
		return d, nil
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	return d, nil
}
```

In `backend.go`: `var detectors = []detector{detectArchive, detectDSpace, detectHTML, detectHints, detectOPDSPath, detectIndex, detectBrowse}` and in `backendFor`:

```go
	case "index":
		return indexBackend{}
```

- [ ] **Step 5: Routes** — in `docs.go` (add imports `"path/filepath"`):

In `case p == "catalog/add":`, replace

```go
		pr := rep.Profile
		pr.ID = IDFor(pr.Home, ps)
		ps = append(ps, pr)
		if err := SaveAll(env.Path, ps); err != nil {
			return nil, err
		}
```

with

```go
		pr := rep.Profile
		pr.ID = IDFor(pr.Home, ps)
		var buildErr error
		if pr.Search.Kind == "index" {
			pr.Search.Index = filepath.Join(filepath.Dir(env.Path), "catalogs", pr.ID+".index.json")
			buildErr = buildIndex(ctx, env.Fetcher, &pr)
			if buildErr != nil && !errors.Is(buildErr, context.Canceled) {
				return nil, buildErr
			}
		}
		ps = append(ps, pr)
		if err := SaveAll(env.Path, ps); err != nil {
			return nil, err
		}
		if buildErr != nil { // cancelled: the partial list is kept
			return nil, buildErr
		}
```

In `case "":` of the catalog switch, before `if q.Get("q") == "" && pr.Search.Kind == "browse" {` add:

```go
		if pr.Search.Kind == "index" && q.Get("q") == "" {
			dir := q.Get("dir")
			if dir == "" {
				dir = pr.Search.Template
			}
			return dirDoc(ctx, env, *pr, dir)
		}
```

In `case "recheck":`, after `pr.Search.MaxPages, pr.Search.MaxResults, pr.Download = …` add:

```go
			if pr.Search.Kind == "index" {
				pr.Search.MaxFolders = keep.Search.MaxFolders
				pr.Search.Index = filepath.Join(filepath.Dir(env.Path), "catalogs", pr.ID+".index.json")
				if err := buildIndex(ctx, env.Fetcher, pr); err != nil {
					return nil, err
				}
			}
```

- [ ] **Step 6: Run** `gofmt -w internal/sitecat && go vet ./internal/sitecat && go test ./internal/sitecat` → PASS

---

### Task 10: Full suite, live smoke, builds, docs

**Files:**
- Modify: `README.md`, `internal/tui/tui.go` (`catalogLoading`: an `add` that builds an index shows the progress line, already covered by `CurrentProgress`; no change needed unless the smoke test shows otherwise)

- [ ] **Step 1: Full suite** — `gofmt -l . ; go vet ./... && go test ./...` → all packages ok.

- [ ] **Step 2: Live smoke** (temporary data and library; no book file is downloaded)

```bash
cd /c/Users/murat/code/w5f && go build -o bin/w5f-dev.exe ./cmd/w5f
T=$(mktemp -d); export APPDATA="$(cygpath -w $T)" W5F_BOOKS="$(cygpath -w $T)\\Books"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://archive.org/details/folkloreandmythology dracula"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://www.fadedpage.com dracula"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://dspace.mit.edu folklore"
./bin/w5f-dev.exe dump -w 76 "catalog-add https://www.gutenberg.org dracula"
```

Expected: archive ✓ (API, collection), Faded Page ✗ form (JavaScript) then ✓ DuckDuckGo, DSpace ✓ (REST API), Gutenberg ✓ form as in round 1. For a directory listing and an OJS journal, pick a public, lawful example found with DuckDuckGo during the smoke test (e.g. a university "Index of /" of public-domain texts, an OJS folklore journal) and record the outcome. Then add the archive catalog and open one item page (`w5f:catalog/<id>/item?u=…`) to see its files — without downloading.

- [ ] **Step 3: Builds** (version 0.3.2)

```bash
V=0.3.2 && (mv -f bin/w5f.exe bin/w5f.exe.old 2>/dev/null; true) && go build -ldflags "-X main.version=$V" -o bin/w5f.exe ./cmd/w5f
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags "-s -w -X main.version=$V" -o bin/w5f-linux-amd64 ./cmd/w5f
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -trimpath -ldflags "-s -w -X main.version=$V" -o bin/w5f-linux-386 ./cmd/w5f
rm -f bin/w5f.exe.old bin/w5f-dev.exe; rm -rf "$T"
```

- [ ] **Step 4: README** — under the `catalog-add` usage line add:

```
                                         recognises OpenSearch, OPDS (and Calibre /opds), GET and POST search forms,
                                         WordPress, Internet Archive, DSpace 7+, OJS, EPrints, MediaWiki, Blogger,
                                         "Index of /" file listings (indexed locally), and falls back to DuckDuckGo
```
