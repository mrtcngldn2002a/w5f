# M6 — Discovery (Deep Random, Daily Packet, Small Web, Archived Worlds) — design and plan

Date: 2026-09-29 · Status: decisions taken in chat, awaiting review · Milestone M6 (Aşama 7/10), W5F v0.6.0
Execution: Native, tests first, no independent review, compact plan (code written once, in the files).

## 1. Decisions (owner, 2026-09-29)

- **Deep Random (`X`)** draws from seven families: esoteric primary texts, Textfiles, folklore & mythology,
  encyclopedic random, Weird Worlds + archived worlds, small web, internet fiction. (Public domain was not
  chosen for `X`.)
- **Daily Packet (`D`)**, "to my taste": 3 unread periodicals · 1 Weird Worlds · 1 esoteric/folklore text
  (instead of the plan's short horror) · 1 public-domain discovery · 1 archive/old-internet text · 1 from the
  reading queue.
- **Gemini and Gopher**: full clients.
- Found while checking: Chronicling America (loc.gov) answers W5F with HTTP 403 everywhere — left out.
  `www.sacred-texts.com` gives 403, `sacred-texts.com` works.

Success criterion (roadmap): ten `X` draws come from at least six different families.

## 2. Deep Random (`internal/discover`)

```go
type Draw struct{ Target, Family, Why string }       // Why: "Deep random · Textfiles/occult · chaos.txt"
type Family interface {
    Name() string
    Draw(ctx context.Context, env Env) (Draw, error)
}
type Env struct{ Fetcher *fetch.Fetcher; DB *store.DB; Reddit bool; Load func(ctx context.Context, target string) (*doc.Document, error) }
```

- **Shuffle bag:** families are drawn from a bag refilled when empty; the last family drawn is never first in
  the new bag. Seven families → any ten draws cover at least six (with one family failing, still six). The bag
  lives in the store (`kv`), so it survives restarts. A failing family is skipped (up to three tries per `X`).
- **esoteric:** a random site among `sacred-texts.com`, `gnosis.org/library.html`, `esotericarchives.com`;
  from its index a random internal link; a page that is mostly links is an index and is descended into, up to
  three levels; the first page with enough text wins.
- **textfiles:** a random directory from a fixed list (occult, ufo, stories, fun, humor, conspiracy, science,
  magazines — each checked live when implemented), a random file from its listing, opened as plain text.
- **folklore:** Ashliman's folktexts index (`sites.pitt.edu/~dash/folktexts.html`) or Theoi (`theoi.com`),
  same descent as esoteric.
- **encyclopedic:** Wikipedia EN, Wikipedia TR, Stanford Encyclopedia of Philosophy random entry (their own
  random endpoints; the redirect gives the page).
- **weird:** Crom random (SCP / tale / Wanderers' Library / Backrooms) or a random archived world (§5).
- **smallweb:** Wiby "surprise me" (follow its meta refresh), a random entry of the Antenna Gemini feed, a random
  item of a Gopher hole from a small fixed list.
- **fiction:** a random top-of-all-time post of r/WeirdLit, r/LibraryOfShadows, r/shortstories (with the Reddit
  session), else a random Crom tale.
- The page opens with a first line: *Deep random · <family>/<detail> · <date if known>* and "X again".
- TUI: `X` → `w5f:discover/random`. `R` keeps its current meaning (random page of this wiki).

## 3. Daily Packet

- `w5f:packet` = today's issue, built once per day and stored (`kv`, JSON: date, number, entries with target,
  title, kind). `w5f:packet/<date>` older issues; the issue number counts days with a packet.
- Entries: 3 unread feed items from three different feeds (newest first), 1 Weird Worlds (Crom), 1 esoteric or
  folklore draw, 1 public-domain discovery (a random recent Public Domain Review article, else a random
  Gutenberg book page), 1 textfiles draw, 1 first open item of `Queue.md`. A missing kind is skipped, not
  replaced by filler.
- Cover page (numbered I–VII, as in the aesthetic plan's newspaper frame drawn with the reader's own blocks),
  links: "start reading", "reshuffle" (rebuilds today's issue), "save this issue".
- Reading: entries open as `w5f:packet/<date>/<n>`: the item itself with a packet line on top
  ("Daily Packet No. 42 · III of VII · ‹ previous · cover · next ›") and `]`/`[` through the issue. Feed items
  are marked read as usual.
- "Save this issue": one Markdown file `Saved/Daily Packet No. N (YYYY-MM-DD).md` with every entry's text
  (M4 Markdown writer), in the notes folder.
- Welcome screen: "Daily Packet No. N — 7 items" line; key `D`.

## 4. Small web (`internal/smallweb`)

- **Gemini:** TLS 1.2+, trust on first use (fingerprints in `<data>/gemini_hosts`, a changed certificate is
  refused with a clear message and the fingerprints shown), status codes: 1x input (page says what is asked;
  `g → ? <text>` answers), 2x (text/gemini converted, text/* shown as text, others: "binary, not shown"), 3x
  redirects (max 5, never to another scheme silently), 4x/5x errors, 6x "client certificates not supported".
  Responses up to 2 MB, 20 s time-out, cached like web pages (15 min fresh, readable offline).
- **gemtext → blocks:** headings, list items, quotes, preformatted blocks (kept as Pre), link lines as linked
  paragraphs (relative links resolved).
- **Gopher:** menus (types 0 text, 1 menu, 7 search → `g → ? <words>`, h `URL:` links, i info lines, other types
  listed without links), text files as preformatted text; port and selector from the URL; 2 MB / 20 s.
- **Menu `w5f:smallweb`:** Gemini — geminiprotocol.net, Antenna, Kennedy search, BBS (geminispace); Gopher —
  Floodgap, Veronica-2 search, SDF; Web — Wiby, Marginalia (links). `g → smallweb`.
- **Safety:** gemini:// and gopher:// pages are web pages for the reader's rule — their `w5f:` links are
  never opened (the TUI check covers every non-w5f scheme, not only http/https). Only W5F-built pages (menu,
  input prompts) carry `w5f:` addresses.

## 5. Obscure / Archived Worlds (`internal/discover/worlds.toml`, embedded)

- Entries: name, url, family (confic / backrooms / fiction), status (live / archived / dead), tier (1 main,
  2 good, 3 obscure), note. Page `w5f:worlds`: cards grouped by family, status and tier shown; opening a world
  goes live → cache → Wayback (existing fallback).
- Verified live on 2026-09-29 (wikidot): SCP Commune, Chaos Insurgency, Antisphere, F.A.S.A, Timeless Places,
  The Liminal Files, Forgotten Journals, Spatial Records, Backrooms Apeir, Forgotten Places, The Middlerooms,
  Backrooms: Pantheon, RPC Authority, Liminal Archives, SCP International, The Holders.
- Not found at `<name>.wikidot.com`: Psychotronics Division, New Dawn Initiative, Gideon Keys, Anomalous
  Reaction Corps, Void Observation Group, Entity Archive, AO/E/L Database, Perennial Realms, Story Laboratory,
  Mystery Authorization, CTA Database, Librorum Infinitum, The Emptyrooms, Backrooms Archive Unit, The Backrooms
  Classified, Wayward Society, Containment Fiction Archive, Slender Man Arkive, Confic Wiki — looked up during
  implementation (site search, the confic wikis' own link lists, Wayback); an entry whose address cannot be
  verified is left out and listed in the report. `darkrooms.wikidot.com` is an unrelated Chinese wiki and
  `liminal-wanderings` is an empty template: both left out unless the real sites are found.
- Deep Random's weird family picks archived worlds by tier weight (1: 3, 2: 2, 3: 1).

## 6. Tasks (tests first in each)

1. **discover core + shuffle bag** — `Family`, `Draw`, bag in kv, route `w5f:discover/random`, why-line
   decoration; tests with fake families (ten draws ≥ six families; failing family skipped).
2. **Families: esoteric, textfiles, folklore, encyclopedic** — fixtures captured once (index + text pages),
   descent rule tested; live check of each.
3. **Families: weird, smallweb, fiction** — use existing Crom/Reddit code; Wiby meta refresh; Antenna/Gopher via
   Task 5 (so smallweb family lands after Task 5).
4. **Archived Worlds** — resolve the unverified addresses, `worlds.toml`, `w5f:worlds` page, tier weighting;
   tests on parsing and weighting.
5. **Gemini client** — TOFU store, statuses, gemtext converter, input prompt, cache; tests with a local TLS test
   server (self-signed cert) and gemtext fixtures.
6. **Gopher client** — menus, text, search prompt; tests with a local TCP test server.
7. **Small Web menu, source/TUI wiring** — schemes in `source.Load`, `g → ? <text>`, `g → smallweb`,
   non-http schemes treated as web pages for the `w5f:` rule (red→green test).
8. **Daily Packet** — build/store/reshuffle, cover, entry pages with `]`/`[`, save issue, welcome line, `D`;
   tests with a fake draw source and a store with feed items and a queue.
9. **Live smoke, builds v0.6.0, README, vault** — ten real `X` draws (families counted), one packet, gemini and
   gopher pages; builds for Windows, linux/amd64 v1, linux/386.

## 7. Out of scope

Chronicling America (403), Marginalia's API (needs a key; linked only), Usenet (M9), 16colo.rs ANSI art,
GeoCities archives, client certificates for Gemini, Gemini/Gopher uploads (Titan).
