# W5F // ARCHIVE NODE

A reading terminal for the textual internet and a personal archive. Built for a
2006 ASUS W5F (Core 2 Duo T7200, 2 GB RAM, GMA 950), portable to any OS.

One Go binary, one document model, one reader: web pages, Wikidot wikis (SCP,
Wanderers' Library, Backrooms), feeds, books and small-web content are all
converted into the same internal format and read with the same keys.

> Status: **M8 — comics** (local CBZ library with progress, Suwayomi as the following engine, own X11 viewer `w5f view`) · **M7 — platform + update** (signed `w5f update` / `--rollback` from GitHub Releases, `w5f doctor [--live] [--bench]`, antiX boot shell in `platform/antix`: X + xterm on the laptop) · **M6 — discovery** (deep random `x`, Daily Packet `p`, Gemini & Gopher, archived worlds) · **M5 — internet fiction** (Royal Road, XenForo threadmarks, AO3, WordPress serials, Reddit series; follow for new chapters; FanFicFare bridge) · **M4 — personal layer** · **M3 — library** (reads EPUB, MOBI/AZW/AZW3, FB2/.fb2.zip and PDF text with chapters/contents/resume; Gutenberg + Standard Ebooks, local ~/Archive/Books, DJVU/CBZ/CBR → external viewer) · **M2 — periodicals** (71-feed shelf, full text, read/star state, `w5f sync`) on top of **M1 — web + Wikidot.** Readability article extraction, on-disk
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
                         or sign in to Reddit in Chromium and g → reddit-login chromium (AO3: ao3-login chromium)
B / g → chromium <url>   open this page (or the selected link, or an address) in Chromium
w5f eksisozluk.com       Ekşi Sözlük: gündem, topics with entries and paging
w5f w5f:feeds            Periodicals: shelves, unread, starred, feed status
w5f sync                 refresh all feeds (cron/timer friendly)
w5f feeds import x.opml  add another reader's feeds (folders become shelves; in the reader: g → opml-import <file>)
w5f feeds export [file]  your shelves as OPML (in the reader: g → opml-export [file], default ~/w5f-periodicals.opml)
w5f w5f:usenet           Usenet, read only: your groups with new posts, threads, g → usenet <word> finds groups, news:alt.magick opens one
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

Screen: on a wide window (110 columns or more) the library's rooms stay in a side menu on the left — `1` reading room, `2` periodicals, `3` library, `4` fiction, `5` comics, `6` solo RPG, `7` Usenet, `8` discovery, `9` queue, `0` notes — and the page starts beside it, left-aligned, up to 112 columns; `\` hides or shows the menu (kept for next time); narrow windows keep the centred 72-column page. The bottom bar follows the room: the keys that matter there (in the Periodical Gallery `*` and `m`, in a book `t` and `] [`, at the Gaming Table `g → roll / ask / spark`, on a web page `/ a n y d B`), keys bright and what they do dim, `? help` always last. The selected link carries a `»` beside it and the theme's own colours (never the terminal's reverse video). Themes: `g → theme` lists them, `g → theme <name>` puts one on and keeps it — amber (amber chrome over a dark paper page, the default), day (dark ink on cream: for daylight and yellowed panels), cold (grey-blue with a red accent), night (reds only, for the dark). A test holds every theme to WCAG contrast: body text and the selected line at least 7:1, dim text and links 4.5:1.

Ultan's library: W5F is kept by Ultan, after the blind librarian of Gene Wolfe's *The Book of the New Sun*. He does not speak; he leaves notes in the margin, italic and signed "— U.", in a voice after Wolfe's (the words are W5F's own, nothing is quoted). Every note is made by rule from your own reading — never by a model, never invented — and on days with nothing to report he offers one of his sayings. The side menu's rooms are the library's: `1` The Reading Room (home), `2` Periodical Gallery, `3` The Stacks (books), `4` The Serial Hall (internet fiction), `5` The Picture Vault (comics), `6` The Gaming Table (solo RPG), `7` The Newsroom (Usenet), `8` Curiosity Cabinet (every door of discovery), `9` The Lectern (queue), `0` The Scriptorium (notes), `H` The Register (history), `L` Ultan's Ledger. The Reading Room is two columns on a wide page: *On the desk* (what is half-read) and *New on the shelves* (new chapters of what you follow, unread periodicals, the queue) on the left, *Today* (the Daily Packet, the day in the Book of Days, the oracle) and *Ultan's note* on the right — the day's most worth saying: how long you have been away, new chapters, a book left open for days, heavy shelves, a long queue, a reading habit. Deep random adds his note on the shelf a page came from (and how often that shelf has opened this week); the Daily Packet's cover, his note on the issue, with the almanac and the oracle side by side. Ultan's Ledger (`L`, `g → ledger`) counts your reading from history.log: days at the desk, pages opened and different pages over 7 days, 30 days and all told, by kind; the pages most returned to; what is left open.

Internet Fiction (`g → fiction`): web serials and forum stories read like books — contents (`t`), `]`/`[`, resume, "Continue reading", `/` search — and followed for new chapters (`F`; `w5f sync` or "check now" looks for updates; `g → following`). Royal Road (`g → rr <words>`), SpaceBattles / Sufficient Velocity / Questionable Questing threads with threadmarks, AO3 (its home page menu "Find your favorites", Fandoms by category with a letter index, tag / search / user / series work lists with `]`/`[` for pages and AO3's Sort and Filter as a page of links — sort, include/exclude ratings, warnings, fandoms, characters, relationships and tags, crossovers, completion, language — plus typed filters from the prompt: `g → f tag …`, `f -tag …`, `f words 1000-50000`, `f date 2024-01-01..2025-06-30`, `f q …`, `f clear`; works open as serials; `g → ao3 <words>`; the adult-content warning is shown and only your "Proceed" continues). AO3 often refuses readers that are not web browsers (Cloudflare 525 / time-outs); W5F retries once, says so plainly, and offers three ways in: connect your own account (`w5f ao3-login` or `g → ao3-login`, paste your `_otwarchive_session` cookie — stored only on this computer, sent only to archiveofourown.org — then `g → fiction → My AO3` lists bookmarks, subscriptions, history and opens logged-in-only works), "save whole work (AO3 download)" on a work's page (AO3's own EPUB, one request, into the Library), and AO3 EPUBs you download in a browser, which W5F imports from your Downloads folder (`W5F_DOWNLOADS` or `downloads = "…"` under `[fiction]` in `config.toml`) on the Internet Fiction page and on `w5f sync`; a newer download of the same work replaces the book, independent WordPress serials (`g → serial <contents or first chapter address>`; on the page: Worm, Pact, Twig, Ward, Pale, Claw, Seek, Unsong, Ra, A Practical Guide to Evil, Katalepsis, The Wandering Inn; a site's front page is read through its public WordPress post list, so serials without a contents page such as Twig list every chapter at once). Weird Worlds adds There Is No Antimemetics Division, Sarkicism, Alagadda, Dr. Wondertainment and the Serpent's Hand. Reddit posts that belong to a series (same author, same subreddit, "Part N" titles or next/previous links) get a "Series · part i of n" line and `]`/`[` through the parts. Questionable Questing shows on the page only with `[fiction] mature = true` in `config.toml`. FanFicFare bridge: with `fanficfare` installed (`pip install FanFicFare`), `g → ffr <address>` or "save as EPUB" on a serial page puts the story into the Library. Logins, captchas and bot checks are never bypassed.

Discovery: `x` draws a random page from eight families in turn (esoteric primary texts — Sacred Texts (its official archive), Hermetic Library, gnosis.org, esotericarchives, The Alchemy Web Site, Early Christian Writings; knowledge — Aeon, JSTOR Daily, Quanta, Wikipedia featured articles, Wikisource featured texts, World History Encyclopedia, the Internet Classics Archive, the 1913 Catholic Encyclopedia, English translations of the Perseus Digital Library's Greek and Roman texts; textfiles.com; folklore and myth — Ashliman, Theoi; Britannica, Wikipedia EN/TR, the Stanford and the Internet Encyclopedias of Philosophy; SCP / Wanderers' Library / Backrooms and 25 archived worlds; the small web — Wiby, ooh.directory, Kagi Small Web, the GeoCities archive (OoCities), Gemini via Cosmos, Gopher via Floodgap; internet fiction), with a line saying where it came from; ten draws always span at least six families. `p` opens today's Daily Packet: three unread periodicals from three feeds, one weird world, one esoteric or folklore text, one public-domain discovery (Public Domain Review, Project Gutenberg or an old curious book of the Biodiversity Heritage Library, whose own site refuses W5F, through its Internet Archive copy), one old-internet text and the next item of your queue — the same issue all day, `]`/`[` through it, "reshuffle", "save this issue" (Markdown in `Saved/`). Its cover carries two columns: *On this day* (the day's chapter of Chambers's *Book of Days*, 1864, opened in a clean reader page — `g → almanac` — and a few events from the English and Turkish Wikipedias' own selections) and *The oracle*, a tarot card with Waite's *Pictorial Key to the Tarot* one day and an I Ching hexagram cast with three coins, with Legge's translation, the next (draw another any time: `T` or `g → tarot`, `I` or `g → iching`). Each of the 78 cards has its own picture, drawn for W5F in the manner of framed ASCII tarot decks: Waite's word for the card upright runs down its left side, reversed down its right, a sign beneath it; a reversed card's picture turns over while its words stay readable, and every card is the same size. The card's page gives the drawn way's meaning first, the other in italics, and Waite's description; a hexagram's page draws it large, bottom line first, marks the moving lines (○ old yang, × old yin), names its trigrams, shows the hexagram it turns into, and gives Legge's note on the name, the judgment, the moving lines' texts and all six. The texts come from sacred-texts once, cleaned (the book's page numbers taken out, a few scanning slips mended), and are kept: after that they read the same, offline. Every draw has its own address, so back and history bring the same card or cast. `g → smallweb` lists Gemini capsules and Gopher holes; any `gemini://` or `gopher://` address opens like a page (Gemini certificates are trusted on first use and a changed one is refused; pages that ask for input are answered with `g → ? <text>`). `g → worlds` lists the Obscure & Archived Worlds. Library of Congress (Chronicling America) refuses W5F (HTTP 403) and is not used. The Small Web page also searches Wiby and Marginalia and opens Marginalia's random small sites, each with a "similar" link to steer (its text-friendly interface; when Marginalia is busy with bots it asks for a few seconds' wait, then the reader presses its continue link). Pages that ask for input take the answer on enter.

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

## Comics

`g → comics` (or the Comics link on the welcome page).

- **Library.** CBZ, CBR, CB7 and CBT files and image folders in `~/Archive/Comics` (`W5F_COMICS`), one folder per series.
  - `ComicInfo.xml` is used when present; natural page order.
  - Your page is kept.
  - Pictures (JPG, PNG, WebP, GIF, BMP): a folder with at least one is a comic; loose pictures at the top are "Loose images". `w5f view picture.jpg`, or opening a picture in the reader, shows its folder from that picture on.
  - PDF and DJVU open in the system viewer.
- **Following** runs on [Suwayomi-Server](https://github.com/Suwayomi/Suwayomi-Server).
  - Install: `w5f comics server install` downloads the official release and checks it against the release's checksums. Suwayomi needs Java 21.
  - W5F starts it when Comics opens and stops it when W5F closes. On the W5F laptop: about 13 s to start, 352 MB while idle.
  - Settings: headless, local only (`127.0.0.1:4567`); downloads are CBZ into `Comics/Suwayomi`; its Local source is `Comics/Local`.
  - WebView (KCEF) is Suwayomi's own setting (Server settings → Webview; on by default). The first start with it on downloads a Chromium build of 244 MB (529 MB on disk); on the W5F laptop it adds about 170 MB of memory while the server runs. Some extensions use it to get past their sites' bot checks — that is Suwayomi's feature and your choice; W5F itself never does.
  - Pages: followed series, series pages (follow, refresh, download), sources and search, downloads, extensions.
  - CLI: `w5f comics list|update|server start|stop|restart|status|update`, `w5f comics settings [group]`, `w5f comics set <setting> <value>`, `w5f comics login`.
- **Server settings** (`Comics → Server settings`): Suwayomi's own settings, grouped as its launcher's tabs (SOCKS proxy, Downloader, Conversions, Library updates, Authentication, Backup, Cloudflare, OPDS, KOReader, Sync, Database, Misc, …).
  - Read from the running server's schema, so a newer Suwayomi's settings show too; each with the default, range and meaning Suwayomi writes in its server.conf.
  - The Extension tab lists the extension stores (Suwayomi 2.4 keeps them apart from its settings), with remove and add; what W5F gives the server at start (its root directory, the WebUI off) is listed on the settings page.
- **Open in WebView**, as Suwayomi's own clients have it: on a source's page, a series' page, each chapter ("web") and every page a source could not load. It opens the site in Suwayomi's WebView (KCEF), shown through the server's `/api/v1/webview` page in Chromium; a sign-in or a check for people you do there yourself stays in Suwayomi's cookies for its sources. Needs Suwayomi's WebView on (Server settings → Webview).
- **Source settings** (`Comics → Sources → settings`): a source's own settings, as its extension offers them (image quality, languages, site options). Switches, choices and multiple choices are links, text opens a form. Each Suwayomi keeps its own: a choice made on one computer is not on the other.
- **Settings sync**: `w5f comics sync <another server.conf>` compares another Suwayomi's settings (the PC launcher's `%LOCALAPPDATA%\Tachidesk\server.conf`, say) with this server's and lists the differences; `--apply` copies them and adds missing extension stores. What belongs to one computer is kept (addresses, folders, WebUI, database, accounts, the SOCKS proxy) and so are bot-check solver settings (FlareSolverr). Installed extensions and sources' own settings are not copied.
  - on/off and choices are links; anything else opens a form (secrets hidden, lists as `a, b`, conversions as JSON). A change goes to the server at once (`setSettings`), is checked by the server and stays in its server.conf.
  - What W5F gives on the java command line (address, port, folders, CBZ, WebUI off, …) wins over server.conf; those are shown as W5F's and are not changed here.
  - Authentication (Basic, simple login, UI login with tokens): the form changes the server and the account W5F signs in with together, so W5F never locks itself out; when it was changed elsewhere, W5F asks for the account (`w5f:comics/login`, kept in the suwayomi folder, mode 0600).
- **Update**: `check for an update` compares the installed jar with the newest release; updating downloads it, checks it against the release's checksums, stops a server W5F started and starts it again. A server W5F did not start (the launcher) is left alone.
- **Extensions.** W5F ships and pre-configures no extension repositories. You add a repository by its address and choose what to install; the rights of what a source offers are yours to mind.
- **Viewer.** `w5f view` is a full-screen X11 window, pure Go with no OpenGL.

  | Key | Action |
  |---|---|
  | space / b, arrows | next / previous |
  | `f` | fit page / width / height |
  | `d` | two-page spreads |
  | `r` | right to left |
  | `g` + number + enter | go to page |
  | `n` / `p` (also `]` `[`, ğ ü) | next / previous issue |
  | `q` | back to W5F |

  Progress is saved on every page turn, and to Suwayomi for its chapters. On the laptop: about 250 ms per page turn, 66 MB.

## Solo RPG table

`w5f:solo` (or `g → solo`): what playing alone needs at hand, with every throw your own if you want it.

- **Oracle.** A yes/no question at one of five odds — almost certain 90, likely 75, 50/50, unlikely 25, small chance 10 — answered by a d100 at or under the chance; matching digits (11, 22 … 100) make it extreme, or a twist, and a spark comes with it. The odds follow Ironsworn's (Shawn Tomkin, CC BY 4.0). `g → ask likely Is the bridge guarded?`
- **Dice.** `g → roll 2d6+1`, `d100`, `2d20kh1` (keep the highest: advantage), `4d6kl2`. Rolling your own? Add `= 9` (the total) or `= 3 6` (each die) and W5F reads yours; a face your dice cannot show is refused. The oracle takes yours too: `… = 57`.
- **Sparks.** One or two English words from the dictionary (its English side only; `g → dict-install` first), a tarot card (Waite) or an I Ching hexagram (Legge), or a line from your own reading: a clipping, a page you read, a periodical's title. `g → spark words|word|tarot|iching|reading`.
- **Log.** Every throw, answer and spark, with where it came from, is kept as JSON lines (a file a day) under `solo/log` in the data folder; the table shows the latest, `the whole log` the rest.

## Usenet

Read only, over NNTP (`w5f:usenet`, or `g → usenet`).

- **Server.** `freenews.netfront.net`, which lets everyone read (Eternal September now wants an account even for reading). Change it in `usenet.toml` in the data folder.
- **Groups.** Your groups show how many posts are new; a group read for the first time starts with its last 100 as new. `g → usenet <word>` finds groups on the server, with subscribe links; `news:alt.magick` opens a group.
- **Threads.** A group page lists its threads (the last 300 posts, `]` for older), newest activity first. A thread page shows its posts in order, replies indented with `›`. Quotes and signatures fold away; poems, tables and ASCII art keep their lines; web addresses are links. What you open is marked read (kept per server and group, like a .newsrc).
- **Kill file.** "hide this poster" and "hide this thread" add to `[kill]` in `usenet.toml`; posts whose subject or poster contains one of its words are not shown, and the group page says how many were hidden.
- On the laptop: about 2–3 s per page (half a second of it is connecting to the server).

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
internal/comics    comics library (CBZ, ComicInfo, progress) and the Comics pages
internal/comics/suwayomi  Suwayomi-Server client, start/stop, official install
internal/comics/view      the comics viewer (X11, layout, spreads, RTL)
internal/update    signed self-update and rollback
internal/doctor    install checks, live source checks, render bench
platform/antix     boot shell for the W5F laptop (kmscon or X, Terminus, Amber P3)
internal/doc       document model (blocks, inline spans, links)
internal/htmlconv  HTML → document (generic rules + Wikidot profile)
internal/render    document → terminal lines for a width (pure, tested)
internal/theme     themes (amber, day, cold, night) and their contrast test
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
