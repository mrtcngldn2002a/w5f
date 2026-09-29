# M4 · Personal layer — design

Date: 2026-09-28 · Status: approach A approved, awaiting spec review
Roadmap: M4 (step 5/10) — "queue, notes, clippings, bookmarks, FTS5 search,
Continue, Obsidian-compatible Markdown". Done when `/` returns results of
three kinds (RSS, SCP, book).

## 1. Decisions taken with the owner

- **Storage (approach A):** the owner's data — queue, notes, clippings,
  saved pages — lives in plain **Markdown files** that Obsidian can open and
  edit. SQLite holds only what can be rebuilt: the full-text index and the
  reading history. `w5f reindex` rebuilds the index from files, cache,
  feeds and books.
- **Folder:** W5F's own folder, default `~/Archive/Notes`, changeable
  (`W5F_NOTES`, or `notes = "…"` in a new optional `<DataDir>/config.toml`;
  the environment variable wins), e.g. to a folder inside the
  Second Brain vault. Moving it into the vault (Syncthing, USB) is the
  owner's step; W5F does not sync.
- **Search scope:** everything the owner reads (web pages, SCP/wiki pages,
  articles, feed items, book chapters) plus notes, clippings and saved pages.
- **Clipping (`y`):** a paragraph-selection mode.
- **Notes (`n`):** a pop-up note box inside W5F.

Success criteria:

1. `/dracula` (or any word) lists matches from feeds, wiki pages, books and
   notes in one list with kind labels and a snippet; Enter opens the match.
2. Turkish text matches without diacritics and vice versa (`kafatasi` finds
   `kafatası`).
3. Queue, notes, clippings and saved pages are readable and editable in
   Obsidian; edits made there show up in W5F (the queue is read back from
   `Queue.md`).
4. The home screen shows "Continue reading" (last unfinished pages and
   books) and `H` lists the full history.
5. Deleting the database loses no personal data; `w5f reindex` restores
   search.

## 2. Files (Obsidian-compatible)

```
~/Archive/Notes/
  Queue.md                      reading queue
  Notes/<Title>.md              one note file per source
  Clippings/YYYY/MM/YYYY-MM-DD.md   the day's clippings
  Saved/<Title>.md              saved pages (readable Markdown copy)
```

File names: title made safe for every OS (the `books.FileName` rules),
at most 80 characters; a clash with a different source gets ` (2)`.

**Queue.md**

```markdown
---
tags: [w5f]
---
# Reading queue

## This week
- [ ] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173
- [ ] [Dracula](w5f:book/3) · BK·GUT·345

## Someday
- [x] [Göbekli Tepe'de yeni bulgular](https://arkeofili.com/…) · PER·ARK·2026-09-24
```

W5F reads the file on every queue view and writes it back when an action
changes it; unknown lines (the owner's own text) are kept in place. A
checked box (`- [x]`) is "done" and listed separately.

**Notes/<Title>.md**

```markdown
---
title: SCP-173
url: https://scp-wiki.wikidot.com/scp-173
catalog: FIC·SCP·173
kind: scp
created: 2026-09-28
tags: [w5f, w5f/note]
---
# SCP-173

## 2026-09-28 10:21
The first SCP; compare with SCP-096's rules.
```

New notes on the same source are appended as new dated sections.

**Clippings/YYYY/MM/YYYY-MM-DD.md**

```markdown
---
date: 2026-09-28
tags: [w5f, w5f/clippings]
---
## 10:21 · [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173
> SCP-173 is to be kept in a locked container at all times.
>
> Second selected paragraph…
```

**Saved/<Title>.md** — frontmatter (title, url, catalog, kind, saved,
tags `[w5f, w5f/saved]`) and the document rendered as Markdown (headings,
paragraphs, lists, quotes, links, tables as pipe tables, collapsibles as
headed sections, images as `![alt](src)`).

## 3. Catalog numbers

`catalog.Number(target string, d *doc.Document) string`, shown in the top bar
and written into queue lines, notes and clippings:

| Source | Number |
|---|---|
| SCP wiki page `scp-173` | `FIC·SCP·173`; other SCP pages `FIC·SCP·<slug>` |
| Wanderers' Library / Backrooms | `FIC·WL·<slug>` / `FIC·BR·<slug>` |
| Feed item | `PER·<FEED>·<YYYY-MM-DD>` (feed id upper-cased, ≤ 6 chars) |
| Library book | `BK·GUT·<n>`, `BK·SE·<slug>`, `BK·<CATALOG>·…`, local files `BK·LOC·<id>` |
| Reddit | `WEB·RDT·<sub>` |
| Other web page | `WEB·<SITE>·<slug>` (site = first host label, ≤ 8 chars) |

Slugs are cut to 24 characters. Pure function, table-tested.

## 4. Reading history (SQLite)

Table `history(target PRIMARY KEY, title, kind, catalog, first, last,
opens, pos REAL)`. A page's `pos` is its scroll position (0–1), saved when
leaving it; books keep their own chapter progress and appear through it.
Only content pages are recorded (see §5 kinds); `w5f:` menus and lists are
not.

- Home screen: **Continue reading** — the 5 most recent entries with
  `0.05 < pos < 0.95` plus books in progress, newest first.
- `H` / `g → history`: the full list, newest first, 50 per page.

## 5. Full-text index (SQLite FTS5)

- Table `docs(id, target UNIQUE, kind, title, catalog, text, updated)` holds
  the original text (≤ 200 KB per document, for snippets).
- `docs_fts` is an FTS5 table (`rowid` = `docs.id`, columns `title`, `body`) holding the **folded** title and text:
  lower case, diacritics removed, Turkish `ı İ ş ğ ü ö ç` mapped to ASCII,
  one rune in → one rune out so positions match the original. Queries are
  folded the same way. SQLite's own `remove_diacritics` does not fold `ı`
  (verified 2026-09-28), hence the Go-side folding.
- Snippets are cut from the original text around the first match (≈ 160
  characters), so Turkish letters display correctly.

Kinds (label on the results page):

| Kind | Label | Indexed when |
|---|---|---|
| `scp` / `wiki` | `[SCP]` / `[WIKI]` | the page is opened |
| `web` | `[WEB]` | a web page or article is opened |
| `reddit` | `[RDT]` | a Reddit thread is opened |
| `feed` | `[RSS]` | feed sync stores the item; again when opened (full text) |
| `book` | `[BK]` | a book enters the library (each chapter a document, in the background) |
| `note` / `clip` / `saved` | `[NOT]` / `[KES]` / `[SAV]` | the file is written; `reindex` scans the folder |

Indexing never blocks the reader: it runs after the page is shown, and book
indexing runs in a background goroutine with its own status line
("indexing Dracula · ch 12/27").

## 6. Search (`/`)

- `/` opens a one-line prompt (like `g`); Enter shows
  `w5f:find?q=<words>` (the existing `w5f:search/…` addresses are web and
  wiki searches and stay).
- Results: kind label, title, catalog number, snippet with the matched words
  in bold; 30 per page, ranked by FTS5 `bm25` (title matches weigh 5×),
  ties broken by the newest `updated`.
- Filter links at the top: `all · RSS · SCP · BK · web · notes`
  (`w5f:find?q=…&kind=feed`).
- Enter opens the target: pages by their address, book matches at their
  chapter, notes/clippings/saved as their Markdown file in the reader.
- Query syntax: plain words (all must match); `"exact phrase"`; FTS
  operators are escaped so user text never breaks the query.

## 7. Keys and screens

| Key | Action |
|---|---|
| `/` | search everything |
| `a` | add the current page to the queue ("This week") |
| `A` | add the focused link to the queue |
| `n` | note box for the current page (Ctrl+S save, Esc cancel) |
| `y` | clipping mode (↑↓ move, Shift+↑↓ extend, Enter save, Esc cancel) |
| `s` | save the page (Markdown copy in `Saved/`) |
| `H` | history |

- **Queue page** (`g → queue`, home link): "This week", "Someday", "Done";
  each entry has links `open · done · → someday / → this week · remove`.
- **Notes page** (`g → notes`, home link): recent notes, clippings (by day)
  and saved pages, each opening its Markdown file.
- **Note box:** overlay like the dictionary: multi-line input, word wrap,
  arrows move, Backspace/Delete, Enter new line, Ctrl+S save, Esc discard
  (asks once if text was typed). Shows the page title and catalog number.
- **Clipping mode:** the renderer records each text block's line range and
  plain text (`Layout.Paras`); the selected range is highlighted; Enter
  appends the selected paragraphs as one clipping. Status bar lists the
  mode's keys.
- Status line after each action: "added to queue", "note saved to
  Notes/SCP-173.md", "clipping saved (2 paragraphs)".

## 8. Components

| Package / file | Responsibility |
|---|---|
| `internal/personal/paths.go` | notes folder (env, config, default), safe file names |
| `internal/personal/queue.go` | read/modify/write `Queue.md`, keeping unknown lines |
| `internal/personal/notes.go` | append notes, clippings; write saved pages |
| `internal/personal/markdown.go` | `doc.Document` → Markdown |
| `internal/personal/docs.go` | `w5f:queue`, `w5f:notes`, `w5f:history` pages |
| `internal/catalog/catalog.go` | catalog numbers |
| `internal/index/fold.go` | rune-preserving folding |
| `internal/index/index.go` | `docs` + FTS tables, `Add`, `Search`, snippets, reindex |
| `internal/index/docs.go` | `w5f:find` results page |
| `internal/store/history.go` | history table |
| `internal/render` | `Layout.Paras` (text block ranges) |
| `internal/tui` | keys, prompt for `/`, note box, clipping mode, history recording, home "Continue reading" |
| `internal/source` | routes; index pages after load; book indexing on add |
| `cmd/w5f` | `w5f reindex` |

## 9. Error handling

- Notes folder missing → created on first write; unwritable → status line
  error, nothing lost from the note box (text stays until Esc).
- `Queue.md` edited into an unexpected shape → unknown lines kept; entries
  are recognised only by `- [ ]`/`- [x]` lines with a Markdown link.
- Index write errors never break reading; they show once in the status line.
- `reindex` reports counts per kind and skipped items (e.g. pages no longer
  in the cache).

## 10. Testing

- catalog numbers (table test over all source types).
- folding: Turkish and Latin diacritics, rune positions preserved.
- index: add/update/search, kind filter, phrase, operator escaping, snippet
  from original text, Turkish query, 200 KB cap, reindex from fixtures.
- queue: add, done, move, remove; unknown lines and owner edits survive.
- notes/clippings/saved: file names, frontmatter, appending, day files,
  Markdown rendering of every block type.
- render `Paras`: ranges for paragraphs, list items, quotes, headings.
- TUI: `/` prompt → results, `a`/`A`, note box save/cancel, clipping mode
  selection and save, history `pos` saved on leave, Continue section.
- end to end: open an SCP fixture page, a feed item and a book chapter;
  `/` finds all three (the M4 criterion).

## 11. Out of scope

`:tag` command line, syncing the folder into the vault, in-page find,
Daily Packet (M6), deleting notes from inside W5F (done in Obsidian).
