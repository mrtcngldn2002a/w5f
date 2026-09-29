# M5 Internet Fiction Implementation Plan

> Execution: Native, in this session; no independent review at the end (owner rule, 2026-09-28). To save tokens this plan gives files, interfaces, test cases and the tricky algorithms; the code itself is written once, in the files, test first (RED → GREEN) per task.

**Goal:** Serials (Royal Road, XenForo, AO3, WordPress) and Reddit series read like books in W5F, with follow-for-updates, an Internet Fiction home page and an optional FanFicFare bridge.

**Spec:** `docs/superpowers/specs/2026-09-28-internet-fiction-design.md`

## Global Constraints

- English sources; no logins, captchas, bot/JS checks bypassed; `fetch.ChallengeReason` pages become the honest verification error. No browser impersonation: the fiction package uses the normal `source.Fetcher`, never `ForCatalog()`.
- Adult content only after an explicit step: AO3 "Proceed" link; QQ hidden on the home page unless `fiction.mature = true`.
- Reddit only through the existing `internal/reddit` (owner's session, old.reddit); W5F code never sees or logs the cookie.
- FanFicFare only if already installed; W5F passes no credentials or adult flags.
- Do not modify libgen/atlas code (`internal/libgen`, `books/libgen.go`, their routes in `source.go`). Two pre-existing failing tests there are ignored.
- Pure Go, CGO off, builds for linux/amd64 GOAMD64=v1 and linux/386. UI English. No commits unless asked.
- Fixtures: pages captured once live (owner agreed), trimmed, saved in `testdata/fiction/` with a `SOURCES.md` line each.

## Review Focus

1. A site layout change (no chapter list found) → a clear error naming the site, never an empty serial saved over a good one (test in Task 2).
2. A chapter removed on the site → kept locally, progress survives; a renamed chapter → title updated (Task 1).
3. Reddit titles without part markers, or parts posted out of order → series key and ordering by time (Task 7).
4. A verification/403 page while syncing follows → that serial reports the error, the others still sync (Task 8).
5. FanFicFare hanging or failing → timeout/cancel, error page with its last output lines, no half file in the Library (Task 9).

---

### Task 1: Storage (`internal/store/serials.go`)

Schema (added to `schemaExtras`):

```sql
CREATE TABLE IF NOT EXISTS serials (
  id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, url TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL DEFAULT '', author TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  followed INTEGER NOT NULL DEFAULT 0, checked INTEGER NOT NULL DEFAULT 0, check_error TEXT NOT NULL DEFAULT '',
  opened INTEGER NOT NULL DEFAULT 0, chapter INTEGER NOT NULL DEFAULT 0, pos REAL NOT NULL DEFAULT 0,
  seen INTEGER NOT NULL DEFAULT 0, added INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS serial_chapters (
  serial_id INTEGER NOT NULL, n INTEGER NOT NULL, title TEXT NOT NULL DEFAULT '', url TEXT NOT NULL,
  published INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(serial_id, n));
CREATE UNIQUE INDEX IF NOT EXISTS serial_chapters_url ON serial_chapters(serial_id, url);
```

Interfaces:

```go
type Serial struct{ ID int64; Kind, URL, Title, Author, Summary string; Followed bool; Checked, Opened time.Time
    CheckError string; Chapter int; Pos float64; Seen, Chapters int }   // Chapters = count
type SerialChapter struct{ N int; Title, URL string; Published time.Time }
func (db *DB) UpsertSerial(s Serial) (int64, error)            // by URL; keeps follow/progress/seen
func (db *DB) Serial(id int64) (Serial, error)
func (db *DB) SerialByURL(u string) (Serial, bool)
func (db *DB) MergeChapters(id int64, chs []SerialChapter) (added int, err error)
func (db *DB) SerialChapters(id int64) ([]SerialChapter, error)
func (db *DB) SetFollowed(id int64, on bool) error
func (db *DB) MarkSeen(id int64) error                           // seen = chapter count
func (db *DB) SaveSerialProgress(id int64, ch int, pos float64) error
func (db *DB) SetChecked(id int64, errText string) error
func (db *DB) Serials(followedOnly bool) ([]Serial, error)       // most recently opened first
func (db *DB) NewChapterTotal() (int, error)                     // sum over followed of max(0, chapters-seen)
```

`MergeChapters` algorithm: existing chapters keyed by URL. Walk the incoming list in order; a known URL updates title/published; unknown URLs are appended after the current last `n` in incoming order. Chapters missing from the incoming list are kept. Returns the number appended. An empty incoming list is an error ("no chapters") and changes nothing.

Tests (`serials_test.go`): upsert keeps follow/progress on refresh; merge appends new, keeps removed, updates renamed titles; empty merge refused; new-chapter total counts only followed serials and respects `seen`.

### Task 2: Fiction core, routes, source and TUI wiring

Files: `internal/fiction/fiction.go` (types, registry, Open/Refresh), `docs.go` (pages), `internal/source/source.go` (targets, Resolve, URL interception), `internal/tui/tui.go` (`F`, `t`, progress), `cmd/w5f/main.go` help text.

```go
package fiction
type Serial struct{ Kind, URL, Title, Author, Summary string; Chapters []Chapter }
type Chapter struct{ Title, URL string; Published time.Time }
type Adapter interface {
    Kind() string
    Match(u *url.URL) (serial string, ok bool)
    Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error)
    Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error)
}
var adapters []Adapter                           // appended by each adapter file's init
type Env struct{ Fetcher *fetch.Fetcher; DB *store.DB; Mature bool; Reddit func(ctx context.Context, u *url.URL) (*doc.Document, error) }
func IsTarget(t string) bool                     // w5f:fiction, w5f:serial/, w5f:following
func Route(ctx context.Context, target string, env Env) (*doc.Document, error)
func Match(u *url.URL) (Adapter, string, bool)   // for source URL interception
func OpenURL(ctx context.Context, env Env, u *url.URL) (*doc.Document, error) // serial page or chapter
func Refresh(ctx context.Context, env Env, id int64) (added int, err error)
func SaveFromRef(db *store.DB, ref string, frac float64)   // "serial:<id>:<n>"
func SerialRef(ref string) (id int64, ok bool)
func ToggleFollow(db *store.DB, id int64) (bool, error)
```

- `OpenURL`: adapter.Match → canonical serial; if known and checked < 1 h ago use stored chapters, else fetch `Serial` and merge (on error with a stored serial: use stored chapters and show a notice). If `u` is a chapter URL present in the list → chapter page; else serial page.
- Chapter page like `books.chapterDoc` (meta, Ref, Next/Prev, nav with contents, resume from stored progress when it is the saved chapter). Chapter fetch errors → notice page with "open on the site" link.
- Source: `fiction.IsTarget` routes before the http branch; for http(s) URLs `fiction.Match` is tried before `loadURL` (Reddit posts are handled in Task 7). Resolve: `fiction` → `w5f:fiction`, `following` → `w5f:following`, `serial <addr>` → `w5f:serial/open?u=`, `rr <w>`, `ao3 <w>`, `ffr <addr>`.
- TUI: `F` → `fiction.SerialRef` (or Reddit series ref from Task 7) → ToggleFollow, status "following ✓"/"unfollowed"; `t` on `serial:` refs → `w5f:serial/<id>/toc`; `leavePage` also calls `fiction.SaveFromRef`. Help lists `F`.

Tests: a fake adapter (`fake.test` host) served from an `httptest` server: open by chapter URL lands in the chapter with Next/Prev; contents; progress save/resume; follow toggle; layout-change error (`Serial` returns an error) keeps the stored chapters and shows a notice; unknown URL not matched; Resolve cases.

### Task 3: Royal Road (`internal/fiction/royalroad.go`)

- Match `royalroad.com/fiction/<id>[/slug[/chapter/<cid>/slug]]` → `https://www.royalroad.com/fiction/<id>`.
- Serial: title `h1`, author `h4 a[href^="/profile/"]` (or `.fic-title h4 a`), summary `.description`, chapters `tr.chapter-row` (`data-url`, first `a` text, `time[unixtime]`).
- Chapter: `.chapter-content` via `htmlconv` fragment conversion; title `h1`; author notes `.author-note-portlet` → folded Collapsible "Author's note"; **hidden classes**: regex over every `<style>` text for `\.([A-Za-z0-9_-]+)\s*\{[^}]*display:\s*none` → remove elements with those classes before conversion.
- Lists: `/fictions/best-rated`, `/fictions/latest-updates`, search `/fictions/search?title=<q>`: `.fiction-list-item` → title link, author/stats line → links to `w5f:serial/open?u=`.
- HostGaps `royalroad.com: 1s`.
- Fixtures: fiction page (trimmed to 5 chapter rows), chapter with the style trick, best-rated list. Tests: meta and chapters, hidden sentence removed, author note folded, list parsing.

### Task 4: XenForo (`internal/fiction/xenforo.go`)

- Hosts: `forums.spacebattles.com`, `forums.sufficientvelocity.com`, `questionablequesting.com`, `forum.questionablequesting.com`. Match `/threads/<slug>.<id>/…` and `/posts/<id>` only when it can be mapped to a thread (post URL → fetch once, follow the redirect to the thread). Canonical `https://host/threads/<slug>.<id>/`.
- Serial: title `h1.p-title-value` (threadmark label spans stripped), author `.p-description .username`; chapters from `/threadmarks?threadmark_category=1`, all pages (`.pageNav-page` last number, `?page=N`), items `.structItem--threadmark .structItem-title a` (href `/threads/…/post-<id>` or `#post-<id>`), date `time[data-time]`. No threadmarks → error `errNotSerial` → source falls back to the normal web page.
- Chapter: fetch the post URL; find `article#js-post-<id>` (or `[data-content="post-<id>"]`) `.bbWrapper`; `.bbCodeSpoiler` → Collapsible with its button text; title = threadmark title.
- Fixtures: SV threadmarks page (2 pages trimmed), a thread page with the post. Tests: list across pages, post extraction by id, spoiler folding, no-threadmark fallback.

### Task 5: AO3 (`internal/fiction/ao3.go`)

- Match `archiveofourown.org/works/<id>[/chapters/<cid>]` → `https://archiveofourown.org/works/<id>`.
- Serial: `/works/<id>/navigate` → `ol.chapter.index li a` + `span.datetime`; meta from `/works/<id>`: `h2.title`, `a[rel=author]`, `.summary .userstuff`. Single-chapter works: one chapter = the work URL.
- Adult warning: a page containing `This work could have adult content` / `p.caution` → `errAdult{proceed}`; the route shows AO3's warning text and "Proceed (show adult content)" → `w5f:serial/open?u=<work>?view_adult=true`; the adapter keeps `view_adult=true` on its requests for that serial only after that step (stored as part of the serial URL query).
- Verification (`fetch.ChallengeReason`) → error text + FanFicFare suggestion link.
- Chapter: `#chapters .userstuff` (drop `h3.landmark`), notes `.notes .userstuff` → folded.
- Fixtures: navigate page, work page, chapter page, adult warning page (captured if AO3 answers; otherwise written by hand from AO3's documented markup — noted in SOURCES.md). Tests accordingly.

### Task 6: WordPress serials (`internal/fiction/wordpress.go`)

- Not auto-matched (any host); used for `serial <address>`, presets and hosts already stored as `wordpress` serials (Match returns true for a stored serial's host+path prefix via a lookup function set by the core).
- Serial from a ToC page: links inside `.entry-content` (else `article`, else `main`) on the same host, excluding category/tag/feed/comment/`#` links, de-duplicated, ≥ 5 links → chapters. Else from a chapter page: follow next links (`a[rel=next]`, or link text matching `(?i)^\s*next( chapter)?\s*[›»>→]*\s*$`) up to 50 fetches; stored as known chapters; refresh continues from the last one.
- Chapter: `.entry-content` minus paragraphs that consist only of previous/next/ToC navigation links; title `h1.entry-title`.
- Presets (`presets.go`): Worm `https://parahumans.wordpress.com/table-of-contents/`, Pact `https://pactwebserial.wordpress.com/table-of-contents/`, Twig `https://twigserial.wordpress.com/table-of-contents/`, Ward `https://www.parahumans.net/table-of-contents/`, Pale `https://palewebserial.wordpress.com/table-of-contents/`, The Wandering Inn `https://wanderinginn.com/table-of-contents/`. (Checked live during the task; a dead one is dropped with a note.)
- Fixtures: Worm ToC (trimmed), a Worm chapter. Tests: ToC links, nav paragraphs removed, next-link walking on a 3-page local site.

### Task 7: Reddit series (`internal/fiction/reddit.go`, small hook in `internal/reddit`)

- `internal/reddit` exposes `PostInfo{Sub, Author, Title, Posted time.Time, URL string; Links []doc.Link}` from `oldPost` (set on the document via a new exported func `reddit.Info(d *doc.Document) (PostInfo, bool)` kept in a package map keyed by URL, or returned by a new `LoadPost`). Chosen: `reddit.LoadWithInfo(ctx, f, u, reload) (*doc.Document, *PostInfo, error)`; `Load` stays.
- Series key: lowercase title, remove bracketed/parenthesised part markers and trailing markers with regex `(?i)[\[(]?\s*(part|pt\.?|chapter|ch\.?|#)\s*([0-9]+|[ivxlc]+)\s*[\])]?|[\[(]?\s*(final|finale|conclusion|update)\s*[\])]?`, collapse punctuation/space. Part number parsed from the same match (roman numerals supported).
- Candidates: body links whose text matches `(?i)part|chapter|next|prev|previous|pt\.` and point to reddit posts; the author's `/user/<a>/submitted/?sort=new` page (plus page 2 when the oldest listed post is newer than the current post) filtered to the same sub and the same series key. Ordering: all have part numbers → by number; else by posted time.
- On the post page: a line after the title "Series · part i of n · ‹ previous · contents · next ›", `Next`/`Prev`, `Ref` `rseries:<author>:<sub>:<key>`; `F` on that ref creates a `reddit` serial (URL `reddit-series:<sub>/<author>/<key>`) with the posts as chapters; its chapters open as the Reddit post pages (chapter fetch = `env.Reddit`).
- Tests with old.reddit fixtures (existing test server pattern in `internal/reddit/old_test.go`): series found via author page when the post has no links; via links; ordering by number vs time; single post = no series line.

### Task 8: Following, sync and the Internet Fiction home

- `fiction.SyncFollowed(ctx, env, progress func(done, total int)) Report{Checked, New, Errors}`: sequential; each error stored with `SetChecked(id, err)`; continues.
- `cmd/w5f` `sync`: after feeds, print `serials: N checked, M new chapters (K errors)`.
- `w5f:following`: rows "Title — author · 3 new · checked 2 h ago" (+ error line), "check now" link (`w5f:following/check`), each opens the serial page; opening marks seen.
- `w5f:fiction` home per spec §4; welcome/home screen shows "Internet Fiction" with "(N new)" (`welcome.go` link line).
- Tests: sync with one failing and one working fake serial; home page sections (QQ hidden unless Mature); new count on the home line.

### Task 9: FanFicFare bridge (`internal/fiction/ffr.go`)

- `var ffrCommand = "fanficfare"`; `lookFFR()` via `exec.LookPath`.
- Route `w5f:fiction/ffr?u=<address>`: no command → install page; else run `fanficfare --non-interactive --format=epub <u>` with `cmd.Dir = tmp`, `context.WithTimeout(10 min)`, capture output; find the newest `*.epub` in tmp → `books.AddFile(db, path, "epub", "", "", "ffr:"+u)` → open the book. Failure → page with the last 15 output lines. Temp dir always removed.
- TUI: the loading line already shows "loading…"; esc cancels through the context (existing load cancellation).
- Test: `TestMain` helper-process pattern (`os.Args[0]` with `GO_WANT_FFR_HELPER=1`) acting as fanficfare: writes a minimal EPUB (reuse books test fixture writer via an exported `fixtures`-style helper in `internal/books/fixtures/epub.go`) or fails with output; missing-command page.

### Task 10: Live smoke, builds, docs

- Live (reading pages only): Royal Road serial open/follow/sync, SV threadmarked story, Worm ToC, AO3 if reachable. Reddit: the owner tests with their session.
- `go vet ./...`, `go test ./...` (libgen tests excepted), builds v0.5.0 ×3, README (Internet Fiction section, keys `F`, `g → fiction/rr/ao3/serial/ffr/following`), vault: 03 Teknik Plan M5 status, Threads, Last-Session, knowledge note, receipt.
