# Site catalogs — design

Date: 2026-09-28 · Status: approved approach (A); revised with full multi-page search and author/title fields, awaiting spec review
Milestone context: add-on to M3 (Library), W5F v0.3.x

## 1. Goal

The owner gives W5F the address of any website that offers **downloadable
books**. W5F inspects the site, works out how to search it, how its result
lists are laid out and where the download links are, and — after the owner
confirms — adds it to the Library's "Find books" section next to Project
Gutenberg and Standard Ebooks. From then on the site is searched, browsed and
downloaded from exactly like the built-in catalogs. No per-site code.

Decisions already taken with the owner:

- Scope is **downloadable books only** (EPUB/PDF/TXT/…). Sites whose texts are
  read on the page are out of scope (the reader already opens those).
- Approach **A, automatic site profile**: detection by heuristics, stored as an
  editable profile. Hand-written recipes (B) and AI-assisted extraction (C)
  are rejected.
- OPDS is one of the detectors, not the foundation.

Success criteria:

1. Adding a well-behaved site takes one address and one confirmation.
2. Searching an added catalog is a **full search**: W5F follows the site's
   result pages (default up to 10 pages / 300 results) and shows one merged,
   de-duplicated list — e.g. "Dracula · 37 results from 4 pages".
3. Searches can target the **author** or the **title** when the site offers
   such fields (`author:Stoker`, `title:Dracula`); plain words search
   everything.
4. Opening a result downloads the best available format into
   `~/Archive/Books` and opens it (EPUB in the reader, others externally).
5. When detection fails, the check page states what was not found; W5F never
   silently shows garbage.
6. Testing and built-in examples use lawful sources only. W5F does not bypass
   bot protection, logins, captchas or JavaScript challenges.

## 2. User flow

```
Library ─▶ "+ add a catalog"  (or  g → catalog-add <url>)
        ─▶ w5f:catalog/check?url=…          CHECK PAGE
             name (from <title>/OpenSearch), home, search method,
             5 sample results for a test word, formats found on result #1,
             problems (if any)
             [✓ add this catalog]  [try another test word: g → catalog-test <word>]
        ─▶ w5f:catalog/add?…               saves the profile, returns to Library
Library "Find books" lists: Gutenberg · Standard Ebooks · <added catalogs>
        ─▶ w5f:catalog/<id>?q=<words>          FULL SEARCH (also g → cat <id> <words>)
             walks the site's result pages, merges and de-duplicates;
             status bar: "searching page 3…"; prefixes author: / title:
        ─▶ w5f:catalog/<id>/item?u=<url>       ITEM: title + download options
        ─▶ w5f:catalog/<id>/get?u=<file>&t=…   downloads, opens the book
```

Library also gets "manage catalogs" (w5f:catalogs): each added catalog with
its status, "re-check" and "remove".

## 3. Components — package `internal/sitecat`

| File | Responsibility | Depends on |
|---|---|---|
| `profile.go` | `Profile` type; load/save `DataDir/catalogs.toml`; ids from host | toml, store.DataDir |
| `probe.go` | `Probe(ctx, f, url) (Report, error)`: find a search method and learn the result layout | fetch, extract |
| `extract.go` | `Results(page, url, layout) []Result`; `LearnLayout(page, url) Layout`; `NextPage(page, url) string` | goquery |
| `search.go` | `Search(ctx, f, profile, query, progress)`: full multi-page search with de-duplication, caps, author/title routing | extract, fetch |
| `downloads.go` | `FindDownloads(ctx, f, itemURL) []Download`; `Fetch(ctx, f, dl) (path, error)` (follows meta-refresh / "download page" one level) | fetch, goquery |
| `docs.go` | Routes `w5f:catalog…` / `w5f:catalogs` into documents | books, store |

`books.Download` is generalised so a direct file URL plus title/author can be
stored in the library (shared by the built-in catalogs and site catalogs).

### 3.1 Profile (editable TOML)

```toml
[[catalog]]
id      = "example"              # derived from the host, unique (illustrative values)
name    = "Example Library"
home    = "https://books.example.org/"
added   = 2026-09-28T12:00:00Z
[catalog.search]
kind     = "form"                # opensearch | opds | form | none
template = "https://books.example.org/search?q={q}"   # {q} = URL-encoded query
# optional field-specific templates, learned when the site offers them
fields   = { author = "https://books.example.org/search?q={q}&in=author",
             title  = "https://books.example.org/search?q={q}&in=title" }
max_pages   = 10                 # full search: result pages followed per query
max_results = 300
[catalog.layout]                 # learned from the sample search
item     = "tr > td > a"         # signature path of one result (see 3.3)
parent   = "table.results"
[catalog.download]
prefer   = ["epub", "pdf", "txt"]
```

Hand-edited values of known fields are preserved on save (the file is re-written from the parsed profiles; unknown keys are not kept).

### 3.2 Search discovery (`probe.go`), first match wins

1. **OpenSearch**: `<link rel="search" type="application/opensearchdescription+xml">`
   → fetch description → `Url[@type="text/html"]@template`,
   `{searchTerms}` → `{q}`, optional params (`{startPage?}` etc.) dropped.
2. **OPDS**: home is (or links to) an Atom feed with a `rel="search"` link;
   results then come from Atom `<entry>` elements and their acquisition links
   (no layout learning needed).
3. **HTML form**: every `<form>` whose method is GET (or empty) and which has
   a text-like input. Score: input `type=search` +3; input name in
   {q, query, s, search, term, keywords, text, title, author} +2; action or
   form id/class/role contains "search" +2; forms inside `<nav>`/`<header>`
   +1. Highest score wins; hidden inputs are kept in the template.
   POST-only forms are reported as unsupported.
4. **WordPress convention**: `<meta name="generator" content="WordPress…">` →
   `home?s={q}`.

**Author / title fields.** While scoring forms, the probe also looks for
field choices and records one template per field:

- a `<select>` inside the search form whose options include author/title-like
  values (option text or value matching `author|creator|writer|yazar` or
  `title|name|book|eser|kitap`) → template with that select set;
- separate text inputs named/labelled author and title in one form → the
  other input left empty;
- separate search forms for author and title on the same page.

Query syntax: `author:<words>` and `title:<words>` use the matching template;
without a prefix, or when the site has no such field, the general template
is used (and the prefix is dropped, not sent to the site). For OpenSearch,
the `{searchTerms}` template is used as is.

If nothing is found: search kind `none`; the catalog can still be added in
**browse-only** mode when the home page itself has a learnable list.

### 3.3 Result layout learning (`extract.go`)

Run the search with a test word (default "history"; changeable). On the
results page:

1. For each element, compute a **signature**: tag + sorted classes
   (e.g. `li.booklink`, `div.result.item`, `tr`).
2. Group elements that share **parent element and signature**.
3. Candidate groups: ≥ 3 members, each member contains at least one `<a href>`
   to the same site whose text is ≥ 3 characters, and members are not all
   navigation (links are not the same across members).
4. Score = members × average member text length, ×1.5 when member links look
   like item pages (path with digits or a slug deeper than the search page),
   ×0.3 inside `nav`, `header`, `footer`, `aside`.
5. Winner's **parent signature path** (up to 3 ancestors) and **member
   signature** are stored as the layout. Later searches select with that path
   first and fall back to re-learning when it yields nothing (sites change).

Per result: title = text of the most prominent link (longest, or inside a
heading); url = that link; extra = the remaining member text (author,
year, format hints), collapsed and trimmed to 160 characters.

Pagination (`NextPage`): `<link rel="next">`, `<a rel="next">`, or a link
whose text is one of `next`, `›`, `»`, `>`, `sonraki`, `→` (case-insensitive).
If none exists but the current URL carries a page-like parameter
(`page`, `p`, `pg`, `start`, `offset`, `start_index`) or the result page links
to URLs differing only in that parameter, the next URL is built by
incrementing it (by 1 for page numbers, by the observed step for offsets).

### 3.3.1 Full search (`search.go`)

`Search(ctx, f, profile, query, progress) (Results, error)`:

1. Build the first URL from the (field) template.
2. Loop: fetch page → extract results with the stored layout → append new
   ones (de-duplicated by normalised item URL, then by title+extra) → find
   the next page.
3. Stop when there is no next page, the next URL was already visited, a page
   adds no new results (sites that repeat their last page), `max_pages` or
   `max_results` is reached, or the context is cancelled (esc).
4. Pages are fetched sequentially through the shared fetcher, so the host
   politeness delay applies; each page is cached, so paging back and
   re-running the same search is instant.
5. `progress(page, found)` drives the status line ("searching page 3 · 41
   found"). The result document states how many pages were read and whether
   the cap stopped the search ("stopped at 10 pages — refine the words or
   raise max_pages").

### 3.4 Download discovery (`downloads.go`)

On an item page, candidate links are anchors where any of:

- URL path (query stripped) ends with a book extension:
  `.epub .pdf .txt .mobi .azw3 .fb2 .djvu .cbz .cbr` (plain `.zip` is not treated as a book),
- `type` attribute is a book MIME type (`application/epub+zip`, `application/pdf`, …),
- link text mentions a format (`EPUB`, `PDF`, `Kindle`, `plain text`) together
  with a download-ish word or an extension.

Each candidate gets a format; the list is ordered by the profile's `prefer`.
If there are none but a link says "download" (any case), fetch that page once
and search again (depth 1). When fetching the file:

- HTML response with `<meta http-equiv="refresh">` → follow (max 2 hops);
- HTML response otherwise → error "the site served a page, not a file";
- 401/403/429 → friendly error, no retries with other identities;
- size limit 100 MB; file type confirmed by magic bytes (zip for EPUB/CBZ,
  `%PDF` for PDF).

Downloaded files are stored via the generalised `books.Download` with
`source = "site:<id>:<item-url>"`, so "✓ in library" works for site catalogs too.

## 4. Error handling and honesty

The check page lists findings as ✓ / ✗ lines, for example:

```
✓ search: HTML form (…/search?q={q})
✓ results: 20 items in "ul.results > li" (sample below)
✗ downloads: no EPUB/PDF link found on the first result
```

- JavaScript-only site: results page has no learnable list but has
  `<noscript>` or a script bundle → "this site needs JavaScript; W5F cannot
  search it".
- Bot wall / captcha / login page detected → reported as such; no bypass.
- A catalog can be added with warnings only when search and results work;
  downloads are re-checked per item anyway.
- `w5f:catalogs` → "re-check" re-runs the probe and updates the layout.

## 5. Testing

Unit (httptest fixtures):

- site with OpenSearch only; site with only a GET form (+ hidden input);
  OPDS catalog; WordPress; POST-only form (reported unsupported).
- full search: a 10-page result set is merged into one list; duplicates
  across pages removed; a site that repeats its last page stops cleanly;
  `max_pages` / `max_results` caps honoured; page-parameter increment when
  there is no next link.
- author/title fields: select-based, two-input and two-form sites produce
  field templates; `author:`/`title:` route correctly; prefix dropped on
  sites without fields.
- layout learning on three shapes: `ul > li` with links, `table > tr`,
  `div.card` grid; plus a page where the nav menu is the biggest link group
  (must lose to the results).
- next-page detection variants.
- downloads: direct `.epub`; `type=` MIME; "Download" intermediate page;
  meta-refresh interstitial; HTML-instead-of-file error; magic-byte check.
- end to end: probe → add → search → get into a temp library → book opens.

Live smoke tests (manual, not in CI): Project Gutenberg (OpenSearch),
Faded Page (form), Internet Archive (JavaScript — expected clean failure).

## 6. Out of scope

Sites that need login, cookies, JavaScript, captchas or POST searches;
read-online collections; automatic periodic re-crawling; parallel downloads.
