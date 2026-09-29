# Site catalogs, round 2 — more site types

Date: 2026-09-28 · Status: approach A approved, awaiting spec review
Builds on: `2026-09-28-site-catalogs-design.md` (round 1, implemented in v0.3.1)

## 1. Goal

Round 1 recognises sites that search through OpenSearch, OPDS, a GET form or
WordPress, plus sites whose front page is a book list. Live tests showed the
gaps: Faded Page and Internet Archive build their results with JavaScript,
old sites search with POST forms, file archives are plain "Index of /"
directory listings, and academic repositories hide files behind platform
conventions.

Round 2 adds four site families, chosen by the owner:

1. **Search-engine fallback** — search the site through DuckDuckGo
   (`site:host words`) when its own search cannot be used.
2. **Directory listings** — "Index of /" style file trees.
3. **Known platforms** — Internet Archive and DSpace 7 through their public
   APIs; OJS, EPrints, MediaWiki, Blogger and Calibre (content server /
   Calibre-Web) through their known URL conventions.
4. **POST search forms.**

Approach A (owner's choice): every way of searching becomes a **backend**
behind one interface, and the check **tries candidates with the test word**
until one really returns results, instead of committing to the first method
it detects.

Success criteria:

1. Internet Archive can be added and searched; only items with public,
   downloadable book files are listed (lending-library items are excluded).
2. Faded Page can be added: its own search fails (JavaScript) and the check
   falls back to DuckDuckGo, clearly labelled as such.
3. A directory listing can be added, browsed folder by folder, and searched
   by file name instantly (local index).
4. A POST-only search form works for page 1 and for following pages when the
   page links them.
5. The check page lists every attempt as a ✓/✗ line, so the owner sees why a
   method was chosen or why a site cannot be added.
6. Existing profiles (round 1) keep working unchanged.
7. Unchanged limits: no bypassing of bot checks, captchas, logins or
   JavaScript challenges; a DuckDuckGo "anomaly" page stops the search with
   an honest message; login forms (password fields) are never submitted.

## 2. Backends

```go
// Request is one page request (GET, or POST with a form body).
type Request struct {
	Method string     // "GET" or "POST"
	URL    string
	Form   url.Values // POST body
}

// backend is one way of searching a site.
type backend interface {
	Kind() string
	// Detect proposes candidate search specs for a site, best first.
	Detect(ctx context.Context, s *site) []SearchSpec
	// First builds the request for page 1; fieldIgnored as in round 1.
	First(spec SearchSpec, q Query) (req Request, fieldIgnored bool)
	// Page reads one response: results and the next page (nil = last).
	Page(ctx context.Context, s *searchState, body []byte, pageURL string) ([]Result, *Request, error)
	// Item lists the downloads of one result.
	Item(ctx context.Context, f *fetch.Fetcher, spec SearchSpec, itemURL string, prefer []string) ([]Download, error)
}
```

`site` carries the home page (body, final URL, parsed document, meta
generator) and the fetcher. `searchState` carries the profile (layout may be
adjusted in memory, as in round 1). Backends register in a fixed order in
`backends.go`.

| Kind | File | Detect | Page / Item |
|---|---|---|---|
| `archive` | `platform_archive.go` | host `archive.org` (a `/details/<collection>` address restricts to that collection) | advancedsearch JSON; item = metadata API files |
| `dspace` | `platform_dspace.go` | meta generator `DSpace 7+`, `/server/api` answers | discover REST JSON; item = ORIGINAL bundle bitstreams |
| `opensearch` | `backend_html.go` | as round 1 | HTML results via layout; item = page downloads |
| `opds` | `backend_opds.go` | round 1, plus probing `/opds` when not linked (Calibre content server, Calibre-Web with anonymous browsing) | Atom entries; item = acquisition links |
| `form` | `backend_html.go` | GET forms as round 1; hint templates for OJS, EPrints, MediaWiki, Blogger | layout; OJS `article/view` → `article/download` rewrite |
| `post` | `backend_html.go` | POST forms without password/email fields | layout; next page only from page links |
| `wordpress` | `backend_html.go` | as round 1 | layout |
| `index` | `backend_index.go` | "Index of /" title or Apache/nginx listing shape | local file-name index; item = the file |
| `browse` | `backend_html.go` | front-page list ≥ 5 items (round 1 browse-only; was `none`) | layout on home |
| `engine` | `backend_engine.go` | always (last resort) | DuckDuckGo HTML; same-site links only |

`Result` gains an optional `Downloads []Download` for backends that know
files directly (index, engine hits on files). The item page uses them when
the result URL is itself a file; otherwise the backend's `Item`.

### 2.1 Candidate chain (probe)

1. Fetch home; bot wall check (round 1).
2. Collect candidates from all backends in registry order (a search-page link
   hop, as in round 1, feeds the HTML backends).
3. Try candidates with the test word, at most 6 attempts before the
   engine fallback (which is always tried when everything else failed): run `First` +
   `Page` for page 1. A candidate **passes** when it returns ≥ 1 result
   (API/OPDS/index/engine) or ≥ 3 results (layout-learned HTML kinds, which
   also learn the layout here). Each attempt adds a finding, e.g.
   `✗ form (…/csearch.php?title={q}): results are built with JavaScript`,
   `✓ search engine: 12 results via DuckDuckGo (depends on its index)`.
4. The first passing candidate becomes the profile's search. For the first
   result, downloads are listed as in round 1 (via `Item`, still without
   downloading a book).
5. `engine` is tried only after all others failed; the check labels it.

### 2.2 Internet Archive

- Search: `https://archive.org/advancedsearch.php?q=<query>&fl[]=identifier&fl[]=title&fl[]=creator&fl[]=year&rows=50&page=N&output=json&sort[]=downloads desc`
  with `<query>` = `(<words>) AND mediatype:texts AND -collection:inlibrary AND -collection:printdisabled AND -collection:lendinglibrary`
  (+ ` AND collection:<c>` when restricted). `author:` → `creator:(<words>)`,
  `title:` → `title:(<words>)`. Next page while `start + rows < numFound`.
- Item: `https://archive.org/metadata/<id>`; items with
  `access-restricted-item = true` show "lending only — not downloadable".
  Files in book formats (by extension, `private` files skipped) become
  downloads at `https://archive.org/download/<id>/<name>`; derivative and
  original files are both offered, ordered by `prefer`.
- Result URL: `https://archive.org/details/<id>`.

### 2.3 DSpace 7

- API base: `<origin>/server/api` (verified by `GET <base>` returning HAL JSON).
- Search: `<base>/discover/search/objects?query=<q>&dsoType=ITEM&page=<n-1>&size=20`;
  `author:` → `query=dc.contributor.author:(<words>)`, `title:` →
  `dc.title:(<words>)`. Title from `dc.title` metadata, extra from
  `dc.contributor.author` and `dc.date.issued`. Next while page < totalPages.
- Item: `<base>/core/items/<uuid>/bundles` → bundle `ORIGINAL` → bitstreams →
  download `<base>/core/bitstreams/<uuid>/content`, format from the bitstream
  name. Result URL: `<origin>/items/<uuid>`.
- DSpace 5/6 (server-rendered) keep working through OpenSearch/forms.

### 2.4 Platform hints (OJS, EPrints, MediaWiki, Blogger)

Detected from meta generator (`Open Journal Systems`, `EPrints`,
`MediaWiki`, `blogger`). They only add candidate templates for the `form`
backend when the page's own form is not found:

- OJS 3: `<journal>/search/search?query={q}` (journal path from the page).
  Download rule: `…/article/view/<id>/<galley>` → `…/article/download/<id>/<galley>`.
- EPrints: `/cgi/search/simple?q={q}`.
- MediaWiki: `<script>/index.php?search={q}&title=Special:Search&fulltext=1&ns0=1&ns6=1`
  (`ns6` = File pages, where PDFs live).
- Blogger: `/search?q={q}`. Posts often link to external file hosts; only
  direct book-file links are downloadable (hosts that need a confirmation
  page are out of scope).

### 2.5 POST forms

- Candidate forms: `method=post`, a text-like input, and **no** password or
  email input (login/newsletter forms). Hidden inputs are kept.
- `SearchSpec.Method = "POST"`, `Template` = action URL, `Body` = form
  template (`q={q}&hidden=…`). Author/title fields as for GET forms.
- Pages after the first: `NextPage` links (GET) or a form whose submit is
  labelled next/›/»/sonraki (re-posted). Otherwise the search is one page.
- Responses are not cached (the fetcher's cache keys by URL); a new
  `Fetcher.PostForm` keeps the politeness delay and User-Agent.

### 2.6 Directory listings

- Detect: `<title>Index of /…</title>`, or a page whose main content is a
  table/pre of links with size and date columns (Apache, nginx, lighttpd
  shapes). The added address is the index root.
- On add, the tree is **indexed**: breadth-first, same host, only below the
  root, parent links and sort links (`?C=N;O=D`) skipped, depth ≤ 5, at most
  `max_folders` folders (default 150), with a 1 s gap per request. Progress:
  "indexing folder 42 · 380 files"; esc stops and keeps a partial index
  (marked partial on the catalog page). Only book-format files are kept.
- Index file: `<DataDir>/catalogs/<id>.index.json` — `{root, built,
  partial, files: [{path, size}]}`. Re-check rebuilds it.
- Search: all query words (case- and accent-insensitive; `_ - .` treated as
  spaces) must appear in the file path. `author:`/`title:` prefixes are
  dropped with the round-1 notice. Results show folder and size.
- Browse: `w5f:catalog/<id>?dir=<path>` lists a folder live (sub-folders
  first, then book files with sizes).
- Item: the file itself (direct download).

### 2.7 Search-engine fallback (DuckDuckGo)

- Request: `GET https://html.duckduckgo.com/html/?q=site:<host> <words>`
  (for `author:`/`title:` the words are quoted; the notice says the engine
  cannot search by field). Next page: DuckDuckGo's own "next" form, re-posted
  (`Fetcher.PostForm`).
- Results: `result__a` links unwrapped from `/l/?uddg=` (existing
  `search.realURL`), kept only when on the catalog's host. Links that are book
  files become results with `Downloads` set.
- Default caps for this kind: 3 pages / 60 results (profile-editable).
- An anomaly/captcha page ends the search: "DuckDuckGo asked for a
  verification; try again later" — no retries, no bypass.
- Result pages are labelled "via DuckDuckGo".

## 3. Profile changes (backward compatible)

```toml
[catalog.search]
kind        = "post"                         # new kinds: archive, dspace, post, index, browse, engine
template    = "https://old.example/find.asp"
method      = "POST"                          # new, default GET
body        = "query={q}&section=books"       # new, POST only
api         = "https://repo.example/server/api"  # new, dspace
collection  = "folkloreandmythology"          # new, archive (optional)
max_folders = 150                             # new, index
```

Round-1 profiles load unchanged; `kind = "none"` is read as `browse`.

## 4. Components

| File | Responsibility |
|---|---|
| `backend.go` | `Request`, `backend` interface, registry, `site`, `searchState` |
| `backend_html.go` | opensearch, form, post, wordpress, browse (moves round-1 logic) |
| `backend_opds.go` | OPDS + `/opds` probing |
| `backend_index.go`, `dirindex.go` | listing detection, crawler, index file, browse |
| `backend_engine.go` | DuckDuckGo fallback |
| `platform_archive.go`, `platform_dspace.go`, `platform_hints.go` | platforms |
| `probe.go` | candidate chain and report (rewritten) |
| `search.go` | generic page loop over a backend (round-1 stop rules kept) |
| `docs.go` | item page uses backend `Item`; browse and index pages |
| `fetch.PostForm` | POST with politeness delay, no cache |

`internal/search` exposes `RealURL` (today `realURL`) for the engine backend.

## 5. Error handling

- Every failed attempt is a ✗ finding with a reason (no results, JavaScript,
  HTTP error, bot wall, login form skipped).
- API JSON that does not parse → ✗ "unexpected answer from the <platform>
  API"; the chain continues.
- Index crawl errors on single folders are counted, not fatal ("3 folders
  could not be read").
- DuckDuckGo anomaly → the engine attempt fails with that reason.

## 6. Testing

Unit (httptest fixtures, no network):

- registry order and candidate chain: first detected method fails (JS page),
  second passes; findings list both; engine only when all fail.
- archive: search JSON → results, lending collections in the query, field
  prefixes, paging by numFound; metadata → downloads; restricted item.
- dspace: API detection, search JSON, bundles → bitstream downloads.
- OJS/EPrints/MediaWiki/Blogger hint templates; OJS download rewrite.
- POST: form found, login form skipped, body template, next via link and via
  next-form; `PostForm` sends the body and User-Agent.
- index: Apache and nginx listing detection; crawl limits (depth, folders,
  parent/sort links, other hosts); partial index; search matching with
  accents and separators; browse page.
- engine: result unwrapping, same-host filter, file hits as downloads, next
  form re-post, anomaly page.
- round-1 profiles (including `kind = "none"`) load and search as before; all
  round-1 tests stay green.

Live smoke (manual): Internet Archive (search + item files, no download),
Faded Page via DuckDuckGo, one public "Index of /" book archive, one DSpace 7
repository, one OJS journal.

## 7. Out of scope

External file hosts that need confirmation pages (Google Drive, MediaFire,
Mega), sites needing login or cookies, JavaScript execution, captcha or
bot-check solving, crawling beyond directory listings, parallel requests.
