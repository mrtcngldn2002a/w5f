# W5F // ARCHIVE NODE

A reading terminal for the textual internet and a personal archive. Built for a
2006 ASUS W5F (Core 2 Duo T7200, 2 GB RAM, GMA 950), portable to any OS.

One Go binary, one document model, one reader: web pages, Wikidot wikis (SCP,
Wanderers' Library, Backrooms), feeds, books and small-web content are all
converted into the same internal format and read with the same keys.

> Status: **M7 — platform + update** (signed `w5f update` / `--rollback` from GitHub Releases, `w5f doctor [--live] [--bench]`, antiX boot shell in `platform/antix`, not yet tried on the laptop) · **M6 — discovery** (deep random `x`, Daily Packet `p`, Gemini & Gopher, archived worlds) · **M5 — internet fiction** (Royal Road, XenForo threadmarks, AO3, WordPress serials, Reddit series; follow for new chapters; FanFicFare bridge) · **M4 — personal layer** · **M3 — library** (reads EPUB, MOBI/AZW/AZW3, FB2/.fb2.zip and PDF text with chapters/contents/resume; Gutenberg + Standard Ebooks, local ~/Archive/Books, DJVU/CBZ/CBR → external viewer) · **M2 — periodicals** (71-feed shelf, full text, read/star state, `w5f sync`) on top of **M1 — web + Wikidot.** Readability article extraction, on-disk
> cache (offline reading, `--offline`), Wayback fallback for dead pages,
> embedded Wikidot blocks inlined, random pages via Crom (SCP, Wanderers'
> Library, Backrooms).
> Planning docs live in the owner's Second Brain vault (`projects/W5F Terminal/`).

## Usage

```
w5f                      welcome page (tour of the reader)
w5f scp-173              shorthand for the SCP Wiki page
w5f w5f:random/scp       random SCP (also: tale, wl, backrooms)
w5f --offline <target>   read from the cache only
w5f r/nosleep            Reddit (connect once: g → reddit-login, paste your own session cookie; read via old.reddit)
w5f eksisozluk.com       Ekşi Sözlük: gündem, topics with entries and paging
w5f w5f:feeds            Periodicals: shelves, unread, starred, feed status
w5f sync                 refresh all feeds (cron/timer friendly)
w5f w5f:books            Library: continue reading, your books, catalogs
w5f "gut lovecraft"      search Project Gutenberg (also: se <words> for Standard Ebooks)
w5f "libgen dracula"     search Library Genesis (also: lg <words>)
w5f "libgen dracula ext:epub lang:english year:1897 sort:title"
w5f "libgen-md5 <hash>"  book details and download by MD5
w5f "libgen-link <hash>" resolve a fresh direct download link
w5f libgen-status        check configured LibGen mirrors
w5f "catalog-add <address> [test word]"  add any book website to the Library (checks it first)
                                         recognises OpenSearch, OPDS (and Calibre /opds), GET and POST search forms,
                                         WordPress, Internet Archive, DSpace 7+, OJS, EPrints, MediaWiki, Blogger,
                                         "Index of /" file listings (indexed locally), and falls back to DuckDuckGo
w5f "cat <id> dracula"                   full search across all its result pages (author:… / title:…)
w5f catalogs                             manage site catalogs (re-check, remove)
w5f "find <words>"       search everything you have read (also: press / in the reader)
w5f queue | notes | history   your reading queue, notes & clippings, history
w5f reindex              rebuild the search index (after moving or deleting the database)
w5f https://scp-wiki.wikidot.com/scp-173
w5f page.html            open a saved page
w5f dump [-w 72] [-open all] <file|url>   render as plain text (debugging, golden tests)
w5f version
```

Keys (Lynx style): `↑/↓` previous / next link or section (scrolls when the next one is off screen) · `→`/`enter` open / fold · `←` back · `l` forward · `space/b` page · `home/end` · `d` dictionary pop-up (EN→TR, install once with `g` → `dict-install`) · `g` go to: address, `scp-173`, `w <words>` Wikipedia, `scp <words>` wiki search, any other words = web search · `r` random page from this wiki · `t` contents (in a book: chapters) · `]`/`[` next/previous chapter · `f` link hints · `+/-` expand / fold all · `o` show address · `ctrl+r` reload · `j/k` scroll a line · `?` help · `q` quit.

Internet Fiction (`g → fiction`): web serials and forum stories read like books — contents (`t`), `]`/`[`, resume, "Continue reading", `/` search — and followed for new chapters (`F`; `w5f sync` or "check now" looks for updates; `g → following`). Royal Road (`g → rr <words>`), SpaceBattles / Sufficient Velocity / Questionable Questing threads with threadmarks, AO3 (its home page menu "Find your favorites", Fandoms by category with a letter index, tag / search / user / series work lists with `]`/`[` for pages and AO3's Sort and Filter as a page of links — sort, include/exclude ratings, warnings, fandoms, characters, relationships and tags, crossovers, completion, language — plus typed filters from the prompt: `g → f tag …`, `f -tag …`, `f words 1000-50000`, `f date 2024-01-01..2025-06-30`, `f q …`, `f clear`; works open as serials; `g → ao3 <words>`; the adult-content warning is shown and only your "Proceed" continues). AO3 often refuses readers that are not web browsers (Cloudflare 525 / time-outs); W5F retries once, says so plainly, and offers three ways in: connect your own account (`w5f ao3-login` or `g → ao3-login`, paste your `_otwarchive_session` cookie — stored only on this computer, sent only to archiveofourown.org — then `g → fiction → My AO3` lists bookmarks, subscriptions, history and opens logged-in-only works), "save whole work (AO3 download)" on a work's page (AO3's own EPUB, one request, into the Library), and AO3 EPUBs you download in a browser, which W5F imports from your Downloads folder (`W5F_DOWNLOADS` or `downloads = "…"` under `[fiction]` in `config.toml`) on the Internet Fiction page and on `w5f sync`; a newer download of the same work replaces the book, independent WordPress serials (`g → serial <contents or first chapter address>`; on the page: Worm, Pact, Twig, Ward, Pale, Claw, Seek, Unsong, Ra, A Practical Guide to Evil, Katalepsis, The Wandering Inn; a site's front page is read through its public WordPress post list, so serials without a contents page such as Twig list every chapter at once). Weird Worlds adds There Is No Antimemetics Division, Sarkicism, Alagadda, Dr. Wondertainment and the Serpent's Hand. Reddit posts that belong to a series (same author, same subreddit, "Part N" titles or next/previous links) get a "Series · part i of n" line and `]`/`[` through the parts. Questionable Questing shows on the page only with `[fiction] mature = true` in `config.toml`. FanFicFare bridge: with `fanficfare` installed (`pip install FanFicFare`), `g → ffr <address>` or "save as EPUB" on a serial page puts the story into the Library. Logins, captchas and bot checks are never bypassed.

Discovery: `x` draws a random page from eight families in turn (esoteric primary texts — Sacred Texts (its official archive), Hermetic Library, gnosis.org, esotericarchives, The Alchemy Web Site, Early Christian Writings; knowledge — Aeon, JSTOR Daily, Quanta, Wikipedia featured articles, Wikisource featured texts, World History Encyclopedia, the Internet Classics Archive, the 1913 Catholic Encyclopedia; textfiles.com; folklore and myth — Ashliman, Theoi; Britannica, Wikipedia EN/TR and the Stanford Encyclopedia of Philosophy; SCP / Wanderers' Library / Backrooms and 25 archived worlds; the small web — Wiby, Gemini via Cosmos, Gopher via Floodgap; internet fiction), with a line saying where it came from; ten draws always span at least six families. `p` opens today's Daily Packet: three unread periodicals from three feeds, one weird world, one esoteric or folklore text, one public-domain discovery, one old-internet text and the next item of your queue — the same issue all day, `]`/`[` through it, "reshuffle", "save this issue" (Markdown in `Saved/`). `g → smallweb` lists Gemini capsules and Gopher holes; any `gemini://` or `gopher://` address opens like a page (Gemini certificates are trusted on first use and a changed one is refused; pages that ask for input are answered with `g → ? <text>`). `g → worlds` lists the Obscure & Archived Worlds. Library of Congress (Chronicling America) refuses W5F (HTTP 403) and is not used. The Small Web page also searches Wiby and Marginalia and opens Marginalia's random small sites, each with a "similar" link to steer (its text-friendly interface; when Marginalia is busy with bots it asks for a few seconds' wait, then the reader presses its continue link). Pages that ask for input take the answer on enter.

Books: W5F reads EPUB, MOBI/AZW/AZW3 (Kindle, including KF8 and combo files), FB2 and `.fb2.zip`, PDF text (10 pages per chapter; uses `pdftotext` when installed — `sudo apt install poppler-utils` — otherwise a built-in reader; every PDF keeps an "external viewer" link), TXT/HTML/Markdown. DJVU, CBZ and CBR open in the external viewer. DRM-protected and KFX books are listed but not opened — W5F never removes DRM.

Personal layer: `/` searches everything you have read (feeds, wikis, web pages, book chapters, notes; `kafatasi` finds `kafatası`) · `a` / `A` add the page / the selected link to the reading queue · `n` note box (ctrl+s saves) · `y` clip paragraphs (↑↓, shift+↑↓ extends, enter saves) · `s` save a Markdown copy · `H` history. Notes live in `~/Archive/Notes` (change with `W5F_NOTES` or `notes = "…"` in `config.toml` in the data folder) as Obsidian-compatible Markdown: `Queue.md`, `Notes/`, `Clippings/YYYY/MM/`, `Saved/`. The database only holds the search index and the reading history, both rebuilt by `w5f reindex`.


Discovery site access: Sacred Texts uses its official archive.sacred-texts.com religion index and stays inside a selected religion to find a text. Britannica selects readable articles from its category index. These hosts use the shared browser-compatible connection for discovery and reading, preserving the usual cache and offline behavior. Hermetic Library is registered, but its Cloudflare challenge still refused live requests on 2026-09-29; failed requests fall through to another esoteric source. Existing Daily Packets remain fixed until reshuffled.

## Library Genesis and custom catalogs

Library Genesis is integrated through a native adapter of the
[halfurness/libgen-cli](https://github.com/halfurness/libgen-cli) libgen.li
protocol: search tables, file/edition metadata, fresh mirror-specific download
keys, Referer, mirror fallback and MD5-verified downloads. EPUBs open in the
reader; other supported formats follow the existing external-viewer flow.
Search accepts `ext:epub,pdf`, `lang:english`, `year:1897`,
`author:"Bram Stoker"`, `title:Dracula`, `publisher:Penguin`, `sort:title`,
`sort:-year`, and page sizes `limit:25`, `limit:50`, `limit:100`. Filters apply
to each page; follow the next-page link for more matches. The library keeps
the canonical `libgen:<md5>` source, avoiding duplicate downloads across mirrors.

Optional `libgen.json` in the W5F data directory configures mirrors and default
formats (the LibGen landing page displays the exact path):

```json
{"search_mirrors":["libgen.li","libgen.vg"],"download_mirrors":["libgen.li","libgen.vg"],"extension":["epub","pdf"]}
```

No separate libgen executable is required. The upstream command-line progress
UI, database dump downloader and unsupported IPFS mode are not embedded.

Custom catalogs use a Chrome-compatible TLS/HTTP2 client, ordered browser
headers and a shared cookie session. Verification pages (including HTTP 200
challenge responses and DDoS-Guard) are never stored as catalog content.
`catalog-add annas-archive.gl history` selects Anna's Archive's own search and
book pages; it never substitutes another catalog. No browser process is used.
The client can initialize DDoS-Guard's standard cookie pixels, but cannot execute
its JavaScript fingerprint challenges or solve CAPTCHAs. If the site still
requires verification, W5F reports that explicitly instead of claiming there
are no results. A known Anna's Archive catalog can still be saved with a clear
verification warning; this does not imply that its search is currently reachable.

## Build

```
go test ./...
go build -o bin/w5f ./cmd/w5f
```

W5F target (the T7200 has no SSE4.2, so the amd64 level must stay at v1):

```
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags "-s -w" -o bin/w5f-linux-amd64 ./cmd/w5f
```

## Update, releases, doctor

- **Update.** `w5f update` installs the latest release from GitHub Releases:
  1. it checks the release signature against the key built into W5F;
  2. it checks the binary's sha256;
  3. it runs the new binary's `selftest`;
  4. only then does it swap the binaries.

  `w5f update --check` only looks; `w5f update --rollback` goes back, and running it twice returns. Only the binary changes, never data, config or notes. The repo comes from the build, or from `config.toml`:

  ```toml
  [update]
  repo = "owner/w5f"
  ```

- **Release** (developer machine):
  - Once: `go run ./cmd/w5f-release keygen`. The private key goes to `%APPDATA%\w5f-release\release.key` (Linux: `~/.config/w5f-release/`). Keep it out of the repo and back it up: without it, installed copies cannot be updated.
  - Each release: `go run ./cmd/w5f-release build -version X.Y.Z -repo owner/w5f`. Always give `-repo`, so the new binary can find the next release. This builds and signs `dist/vX.Y.Z/`; upload every file in it as the assets of release `vX.Y.Z`.
  - Before a push: `sh scripts/secret-scan.sh`.
- **Doctor.** `w5f doctor` checks:
  - folders and config;
  - the database and FTS5;
  - the dictionary;
  - the terminal, locale and font;
  - the update setup.

  `--live` fetches one source of each kind; `--bench` times a 12k-word page against the 150 ms budget. Doctor only reads.
- **Laptop.** `platform/antix/` makes the W5F boot into W5F (kmscon or X); see its README.

Golden files: `go test ./internal/render -update` rewrites
`testdata/golden/` after an intended rendering change — review the diff.

## Layout

```
cmd/w5f            CLI entry point
cmd/w5f-release    release tool: key, cross-builds, signed manifest (developer only)
internal/update    signed self-update and rollback
internal/doctor    install checks, live source checks, render bench
platform/antix     boot shell for the W5F laptop (kmscon or X, Terminus, Amber P3)
internal/doc       document model (blocks, inline spans, links)
internal/htmlconv  HTML → document (generic rules + Wikidot profile)
internal/render    document → terminal lines for a width (pure, tested)
internal/theme     palettes (Amber P3, Paper/Ink, Green P1, Cold Archive)
internal/fetch     HTTP client: cache, conditional requests, politeness delay
internal/crom      Crom GraphQL client (random pages, wiki search)
internal/search    web search (DuckDuckGo HTML) and wiki search result pages
internal/books     library: book readers (EPUB, MOBI/AZW3, FB2, PDF), ~/Archive/Books scan, Gutenberg & Standard Ebooks, progress
internal/libgen    Library Genesis protocol, metadata, mirrors, MD5-verified downloads
internal/sitecat   site catalogs: search discovery, result-list learning, full search, downloads
internal/discover  deep random families, Daily Packet, Obscure & Archived Worlds catalog
internal/smallweb  Gemini (trust on first use) and Gopher clients, gemtext and gophermap conversion
internal/fiction   internet fiction: serial adapters (Royal Road, XenForo, AO3, WordPress), Reddit series, following, FanFicFare bridge
internal/personal  reading queue, notes, clippings, saved pages (Markdown files)
internal/index     full-text search over everything read (SQLite FTS5, Turkish folding)
internal/catalog   call numbers (FIC·SCP·173, PER·…, BK·GUT·…)
internal/dict      pop-up StarDict dictionary
internal/feeds     periodicals: built-in catalog (catalog.toml), sync, shelf/item pages
internal/store     SQLite state (feeds, items, read/starred), pure Go
internal/reddit    Reddit via old.reddit.com with the user's own session (Redlib optional)
internal/source    resolve targets: files, URLs, shorthands, w5f:random, Wayback fallback
internal/tui       Bubble Tea reader
testdata/pages     captured pages used as fixtures
testdata/golden    expected renderings
```
