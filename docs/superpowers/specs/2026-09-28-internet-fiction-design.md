# M5 — Internet Fiction — design

Date: 2026-09-28 · Status: design approved in chat, awaiting spec review
Milestone: M5 (Aşama 6/10), W5F v0.5.0

## 1. Goal

Read internet fiction the way W5F reads books: serials and forum stories
with chapters, contents, `]`/`[`, resume and follow-for-updates, and Reddit
series read part by part. One Internet Fiction home page gathers the
sources.

Decisions taken with the owner:

- Sources: **Royal Road**, **AO3**, **XenForo forums (SpaceBattles,
  Sufficient Velocity, Questionable Questing)**, **independent WordPress
  serials** (Wildbow's serials, The Wandering Inn and the like).
- Reading model: **live virtual book** — a serial lives in W5F like a book;
  chapters are fetched when opened and kept in the page cache.
- Reddit: **automatic series detection** (links in the post + the author's
  posts with similar titles).
- **FanFicFare bridge**: included as an optional bridge (used when the
  `fanficfare` command is installed; W5F never installs it).

Rules that stay: English sources only; no logins, captchas, bot or
JavaScript checks are bypassed; a site that demands verification is
reported honestly. Adult content is never opened without the owner's
explicit step (AO3's own warning is shown with a "Proceed" link; QQ is
hidden from the home page unless enabled).

Success criteria (from the roadmap, adapted):

1. A Royal Road serial is followed; after the site adds a chapter, `w5f sync`
   shows the new-chapter count on the Following page and the home page.
2. A Reddit series (e.g. an r/nosleep series from 2019) is read from part 1
   to the last part with `]`, starting from any part.
3. Royal Road, XenForo, AO3 and WordPress serials open with contents (`t`),
   `]`/`[`, resume, "Continue reading" and `/` search.
4. With `fanficfare` installed, a serial can be saved as an EPUB into the
   Library; without it, W5F explains how to install it.

## 2. Model and storage (`internal/store/serials.go`)

```
serials(id, kind, url, title, author, summary, followed, checked_at,
        chapter, pos, opened_at, seen)    -- seen = chapters known when last opened
serial_chapters(serial_id, n, title, url, published)   -- n from 0, reading order
```

`kind` is `royalroad`, `ao3`, `xenforo`, `wordpress` or `reddit`. `url` is the
canonical serial address (unique). Progress (`chapter`, `pos`) works like
book progress. New chapters = `count(serial_chapters) - seen`; `seen` is
set when the Following page or the serial page is opened.

Serials are not rows of the `books` table: the library scan marks rows
without files as missing.

## 3. Adapters (`internal/fiction`)

```go
type Serial struct {
    Kind, URL, Title, Author, Summary string
    Chapters []Chapter              // reading order
}
type Chapter struct{ Title, URL string; Published time.Time }

type Adapter interface {
    Kind() string
    Match(u *url.URL) (serial string, ok bool)   // canonical serial URL for any serial/chapter URL
    Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error)
    Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error)
}
```

- **Royal Road** (`royalroad.com/fiction/<id>/…`, chapters `…/chapter/<id>/…`):
  title, author, description; chapter table rows (`tr.chapter-row`, `data-url`,
  `<time unixtime>`). Chapter text is `.chapter-content`; the author's notes
  (`.author-note`) are kept as folded blocks. Royal Road hides anti-copying
  sentences with a random CSS class declared `display: none` in a `<style>`
  element of the page — every element with such a class is removed.
- **XenForo** (SpaceBattles, SV, QQ; `/threads/<slug>.<id>/…`): the chapter
  list is the thread's threadmarks (`/threadmarks`, every page, category 1 =
  main story); a chapter is the threadmarked post (`/posts/<id>` or
  `#post-<id>`), its `.bbWrapper` converted as HTML; spoilers become
  collapsibles. Threads without threadmarks: the first post only, with a
  notice.
- **AO3** (`archiveofourown.org/works/<id>`, `/chapters/<id>`): meta from the
  work page, chapter list from `/works/<id>/navigate`; chapter text is
  `#chapters .userstuff` plus notes (folded). AO3's adult-content warning is
  shown as a W5F page with AO3's text and a "Proceed (show adult content)"
  link that adds `view_adult=true`; W5F never adds it by itself. When AO3
  answers with a verification page, the error says so and offers the
  FanFicFare bridge.
- **WordPress serials** (any other host, only when asked: the Internet
  Fiction page's presets, or `g → serial <address>`): if the address is a
  table of contents (many same-host links in the entry content), those links
  are the chapters; otherwise chapters are found by following "Next
  Chapter"/`rel=next` links from the given chapter, up to 50 at a time
  ("more…" continues). Chapter text is the entry content
  (`.entry-content`, else the article extraction), with the
  previous/next-chapter link rows removed. Presets: Worm, Pact, Twig, Ward,
  Pale (Wildbow), The Wandering Inn.

Opening any address an adapter matches (Royal Road fiction or chapter, AO3
work or chapter, a XenForo thread or threadmarked post) goes to the serial:
the serial is created or refreshed and the matching chapter opens inside it,
so `]`/`[` work at once. Plain XenForo threads without threadmarks and forum
index pages stay normal web pages.

## 4. Pages (`w5f:` routes)

- `w5f:fiction` — Internet Fiction home: **Following** (followed serials,
  new-chapter counts, "Continue"), **Weird Worlds** (SCP, Wanderers' Library,
  Backrooms, RPC Authority + random links), **Horror** (r/nosleep,
  r/shortscarystories, r/LibraryOfShadows, r/TheCrypticCompendium,
  r/Odd_directions, r/Cryosleep; Creepypasta.com), **Web Serials** (Royal
  Road best rated / latest updates / search, the WordPress presets,
  r/redditserials, r/HFY), **Forum Fiction** (SpaceBattles and SV creative
  writing forums; QQ only when `fiction.mature = true` in `config.toml`),
  **Fanfiction** (AO3 search). `g → fiction`.
- `w5f:serial/<id>` — serial page: title, author, source, summary, follow
  state, "Continue ch N" / "Start", chapter list (newest marked "new"),
  links: follow/unfollow, refresh, save as EPUB (FanFicFare), open on the
  site.
- `w5f:serial/<id>/ch/<n>` — a chapter: title "Serial — Chapter", meta
  "author · chapter n/N · chapter title", Ref `serial:<id>:<n>`, Next/Prev,
  bottom navigation like books, Resume.
- `w5f:serial/<id>/toc` — contents for `t`.
- `w5f:serial/open?u=<address>` — create/refresh from an address, then the
  serial page (or the chapter when the address is a chapter).
- `w5f:following` — followed serials with new counts, last check time,
  "check now".
- Royal Road lists: `w5f:fiction/rr/best`, `w5f:fiction/rr/latest`,
  `w5f:fiction/rr/search?q=` (`g → rr <words>`); AO3 search
  `w5f:fiction/ao3/search?q=` (`g → ao3 <words>`). Results link to
  `w5f:serial/open`.

TUI: `F` follows/unfollows the serial of the page (serial page, chapter, or
a Reddit post that belongs to a series). `t` on a chapter opens the contents
(as for books). Leaving a chapter saves progress (as for books).

## 5. Reddit series (`internal/fiction/reddit.go`, uses `internal/reddit`)

When a Reddit post page is opened (old.reddit with the owner's session):

1. **Links in the post:** links in the post body to other posts whose text
   says part/chapter/next/previous/prev/"Part N" are candidate neighbours.
2. **The author's posts:** the author's submitted listing on old.reddit
   (`/user/<name>/submitted/?sort=new`, first page; a second page only when
   the series reaches past it) filtered to the same subreddit and a title
   whose series key matches. The series key is the title with part markers
   removed: `(Part 3)`, `Part III`, `[Part 2]`, `pt. 4`, `Chapter 5`,
   `#6`, `- Final`, `(Finale)`, `Update`.
3. Parts are ordered by the part number when every part has one, otherwise
   by posting time.

The post page gets a line under the title: "Series · part 2 of 5 · ‹ previous
· contents · next ›", and `Next`/`Prev` so `]`/`[` move through the parts. A
series with at least two parts can be followed (`F`); it is stored as a
`reddit` serial whose chapters are the posts, and its update check reads the
author's submitted page. Without a Reddit session nothing changes (the
existing setup page shows).

## 6. Following and updates

- `w5f sync` (and "check now" on the Following page) refreshes followed
  serials: one chapter-list request per serial (all threadmark pages for
  XenForo; the author page for Reddit), after the feeds, sequentially with
  the fetcher's per-host politeness delays.
- New chapters are appended; a chapter whose URL disappeared is kept (so
  progress survives), a changed title is updated.
- The home screen line and the Internet Fiction page show "Following (N
  new)"; sync prints "serials: 3 checked, 2 new chapters".
- Opened chapters and serial pages go into history and the search index like
  any page (M4).

## 7. FanFicFare bridge (`internal/fiction/ffr.go`)

- Available when the `fanficfare` command is on the PATH (W5F does not try
  `python -m fanficfare`).
- `g → ffr <address>` or "save as EPUB (FanFicFare)" on a serial page runs
  `fanficfare --non-interactive --format=epub <address>` in a temporary
  folder with a 10-minute limit; the produced `.epub` goes into the Library
  through `books.AddFile` (source `ffr:<address>`), and the book opens.
- Progress lines from FanFicFare's output are shown while it runs; esc
  cancels (the process is killed).
- No FanFicFare: a page explaining `pip install FanFicFare` (antiX:
  `pipx install FanFicFare`). W5F never downloads or installs it.
- FanFicFare follows its own settings (adult content, logins) from its own
  `personal.ini`; W5F passes no credentials and no adult-content flags.

## 8. Errors and limits

- Verification pages (Cloudflare, AO3's check), 403/429: "the site asks for
  browser verification — W5F cannot pass it" plus, for serial sites, the
  FanFicFare suggestion. Never retried in a loop.
- Chapters are cached (normal page cache); offline, opened chapters read,
  unopened ones say they are not in the cache.
- Politeness: per-host gap 1 s for royalroad.com, archiveofourown.org and
  the forums (existing HostGaps).
- Damaged or changed site HTML: an adapter that finds no chapter list
  returns an error naming the site ("Royal Road: no chapter list found —
  the site layout may have changed").

## 9. Testing

Fixtures are real pages captured once (with the owner's consent) and trimmed
into `testdata/fiction/`: a Royal Road fiction page and chapter (with the
hidden-class trick), an SV threadmarks page and post, an AO3 work/navigate/
chapter page (and its adult warning), a Wildbow table of contents and
chapter, old.reddit post and user-submitted pages for a series. Tests use a
local HTTP server as for the Reddit tests. FanFicFare is tested with a stub
command (a tiny Go test binary built by the test) that writes an EPUB.

Live smoke (manual): a Royal Road serial (open, follow, sync), an SV
threadmarked story, a Wildbow serial; AO3 only if it answers without
verification. Reddit series by the owner with their session.

## 10. Out of scope

Logins of any kind, Wattpad/Scribble Hub/FictionPress/FFnet adapters (JS or
Cloudflare), downloading whole serials natively (FanFicFare covers it),
comments on serial chapters, Obscure/Archived Worlds (M6), notifications
outside W5F.
