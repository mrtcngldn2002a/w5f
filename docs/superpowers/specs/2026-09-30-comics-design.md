# M8 — Comics and manga (design and plan)

Status: approved 2026-09-30 (task 5 deferred) · 2026-09-30 · roadmap: `03 Teknik Plan` §4.1, §10 (M8)

## Goal and finish line

Two systems in one W5F:

- **(a) Following and getting.** Suwayomi runs on the laptop, headless. W5F is its terminal front end: library, sources, follow, updates, downloads, extensions.
- **(b) Local library and reading.** CBZ/CBR folders and ComicInfo, covers and progress, read with W5F's own viewer, `w5f view`.

Finish line (roadmap): on the laptop a series is followed, a new chapter arrives as a CBZ, and it is read in the same flow. The engine's memory is measured.

## Decisions (user, 2026-09-30)

| Question | Answer |
|---|---|
| Engine | Suwayomi on the laptop; measure first, then decide |
| Sources | Suwayomi's extension system; public domain archives; own files |
| Viewer | Own Go viewer (`w5f view`) |
| Shell (M7) | X + xterm without GLX, so the viewer is an X11 window |

## Boundaries

- **What W5F does with extensions.** It lists and manages Suwayomi's extension repos and extensions: add a repo URL the user types, install, update, uninstall.
- **What W5F never does.** It neither ships nor pre-configures any repo or extension, and I do not install or test with sources that host works without permission. Which repos and extensions are added is the user's choice and responsibility.
- **What tests use.** Suwayomi's built-in **Local source** (the user's own CBZ files) and the public domain archive.
- **Public domain.** Comic Book Plus needs a free account for downloads. The user creates it and gives W5F their own session cookie through the hidden input, as with AO3. Digital Comic Museum refuses W5F (HTTP 403, 2026-09-30) and stays out.

## Design

### 1. Suwayomi on the laptop (measured first)

- **Setup:** `Suwayomi-Server-vX.jar` (official GitHub release, sha256 checked) in `~/.local/share/w5f/suwayomi/`.
- **Run:** Java 21, `-Xmx256m` (384 if too tight).
- **Config** (`server.conf`), everything the laptop does not need turned off:
  - no WebUI, no tray, no browser launch;
  - listens on `127.0.0.1:4567` only;
  - downloads as CBZ into `~/Archive/Comics/Suwayomi`;
  - Local source folder `~/Archive/Comics/Local`.
- **Started by W5F** (`w5f comics server start|stop|status`) when Comics is opened, and stopped when W5F exits. No boot service unless measurement says it is cheap.
- **Measure:**
  - memory while idle, updating a library of 5 series, and downloading;
  - start-up time;
  - CPU on the T7200.

  The results go into `measure.sh`'s report format.
- **Gate.** If memory while idle goes over about 400 MB, or the machine starts swapping while reading, the fallback is the same client against Suwayomi on this PC (roadmap option 3), with no change to W5F's screens. The user decides with the numbers.

### 2. Suwayomi client (`internal/comics/suwayomi`)

A GraphQL client, stdlib only, with a small typed query set:

- **Library:** categories and manga, with unread and downloaded counts.
- **Sources:** list sources and browse them (popular, latest, search).
- **Manga:** details and chapter list; fetch/refresh the chapter list.
- **Follow:** add to or remove from the library.
- **Updates:** update the library, recent chapters.
- **Downloads:** enqueue chapters, queue state, delete.
- **Extensions:** list, fetch the list, install, update, uninstall; repo URLs (settings).

Tests run against a recorded fake server (`httptest`, JSON fixtures written from the schema).

### 3. Comics library (`internal/comics`)

- **Scan:** `~/Archive/Comics/**` for CBZ, CBR (pure-Go `rardecode`) and image folders. The Suwayomi download folder is part of the same tree, so downloads appear in the library without a separate step.
- **Metadata:** `ComicInfo.xml` (series, number, title, writer/artist, summary, manga RTL flag). When it is missing, series and number come from the folder and file names.
- **Store tables:** `comics` (file, series, number, pages, RTL, cover hash) and `comic_progress` (page, finished). The FTS index gets series and titles.
- **Covers:** first page downscaled to a small JPEG in the cache, used by the viewer's shelf page. Terminal pages show no pictures.
- **W5F pages:**
  - `w5f:comics`: Following / Library / Sources / Downloads / Extensions;
  - a series page (issues with read state);
  - an issue page (open in the viewer, mark read);
  - on followed series and in the library, `F` follows and unfollows, `r` refreshes.
- **CLI:**
  - `w5f comics follow|unfollow|update|download|list|server …`;
  - `w5f sync` also asks Suwayomi for updates when its server is running.

### 4. Comic Book Plus (`internal/comics/cbplus`) — deferred to a later round with Digital Comic Museum

- **Account and session:** the user's own session (`w5f cbplus-login`, hidden input, stored mode 0600, sent only to comicbookplus.com).
- **Adapter:** browse categories, search, open a title, **download** it into `~/Archive/Comics/Comic Book Plus/<publisher>/`. A PDF download is kept for the external viewer; a CBZ download is kept as is.
- **Without a session:** browsing works and download says how to log in.
- **Verified live** once the user has an account. Until then the adapter is covered by fixtures only.

### 5. `w5f view` (own X11 viewer)

- **Pure Go:** `github.com/jezek/xgb` (X protocol) and `golang.org/x/image` (draw scaling, WebP). No cgo, no GL, so it works with the M7 no-GLX setup.
- **Window:** one full-screen window (override-redirect, no window manager on the laptop). W5F's TUI waits while it is open and comes back when it closes.
- **Opens** a CBZ/CBR/folder. It decodes pages one ahead in a goroutine, scales to fit and caches two pages.
- **Keys** (lowercase, as the user types them):

  | Key | Action |
  |---|---|
  | space / →, b / ← | next / previous page (←/→ reversed in RTL) |
  | `]` / `[` | next / previous issue in the series |
  | `f` | fit width / height / page |
  | `d` | double page (spreads) |
  | `r` | right-to-left |
  | `g` + number | go to page |
  | `q` / esc | back to W5F |

- **Progress** is written to `comic_progress` on every page turn and when the viewer closes.
- **On the console (kmscon, no X):** it says so and offers the external program if one is installed. Framebuffer drawing is not part of M8.
- **Budget:** page turn under 300 ms for a 1600×2400 JPEG on the T7200, and viewer memory under 80 MB. Both are measured with `w5f doctor --bench` fixtures.

## Plan (tasks; progress N/5 — Comic Book Plus deferred)

1. **Suwayomi bake-off:**
   - download the jar (your permission first), write the config, start, measure;
   - report and decide (gate).
2. **Suwayomi client + server control:**
   - `internal/comics/suwayomi` and `w5f comics server`;
   - fake-server tests.
3. **Library:**
   - scanning, ComicInfo, store tables, covers;
   - `w5f:comics` pages and CLI; follow, update and download flows over the client;
   - tests with small generated CBZ fixtures.
4. **`w5f view`:**
   - xgb window, decode, scale, keys, RTL/double, progress;
   - unit tests for layout and page math; run live on the laptop over SSH (display :0) and on screen.
5. **Comic Book Plus: deferred (user, 2026-09-30).** It gets its own development round together with Digital Comic Museum, later.
6. **Finish:**
   - README, `w5f doctor` (Suwayomi/Java/X checks), v0.8.0 release (your approval);
   - finish-line test on the laptop: follow a series through the Local source or an extension you choose; update; download CBZ; read in `w5f view`.

## Tests the plan pins (review focus)

- A CBZ with no ComicInfo and odd page names (`10.jpg` before `2.jpg`) keeps natural page order.
- A broken or truncated CBZ shows an error page. It does not crash the viewer, and progress is not lost.
- Suwayomi not running or still starting: the Comics pages say so and offer "start"; they do not hang.
- A download finishing while the library page is open appears after refresh, with no duplicate row.
- The viewer run without X (kmscon or SSH without DISPLAY) exits with a clear message and W5F stays usable.
