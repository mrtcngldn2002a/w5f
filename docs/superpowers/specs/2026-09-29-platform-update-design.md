# M7 — Platform + update (design and plan)

Status: draft for approval · 2026-09-29 · roadmap: `03 Teknik Plan` §7, §10 (M7)

## Goal and finish line

W5F gets itself onto the ASUS W5F and keeps itself current:

- `w5f update` fetches a **signed** release from **GitHub Releases** (public repo), checks it, swaps binaries atomically, and `w5f update --rollback` undoes it.
- `w5f doctor` says what is wrong with an install (dirs, config, database, dictionary, terminal, fonts, update setup), `--live` checks the sources, `--bench` measures render time and memory.
- `platform/antix/` holds the boot shell: autologin on tty1 → W5F, in two variants (kmscon; X + full-screen terminal), plus Terminus font and the Amber P3 palette.

Roadmap finish line: the laptop boots straight into W5F, and updates arrive without a USB stick. **User decision (2026-09-29): laptop work comes later.** This round finishes everything that does not need the laptop. The two shells are written but not measured there, so M7's finish line stays open until the laptop round.

## Decisions (user, 2026-09-29)

| Question | Answer |
|---|---|
| Update source | GitHub Releases |
| Repo visibility | Public: the laptop needs no token |
| Boot shell | Prepare both (kmscon and X), measure on the laptop, then pick one |
| Laptop access | Later. Scripts are written now; the user runs them on the laptop |

## Design

### 1. Release format

Each GitHub release `vX.Y.Z` carries these assets:

- `w5f-linux-amd64`: `GOAMD64=v1`, the W5F laptop's target.
- `w5f-linux-386`
- `w5f-windows-amd64.exe`
- `manifest.json`:
  ```json
  {"version":"0.7.0","date":"2026-09-30","assets":{"linux-amd64":{"name":"w5f-linux-amd64","sha256":"…","size":123}, …}}
  ```
- `manifest.json.sig`: an ed25519 signature of the exact bytes of `manifest.json`, base64 encoded.

Signing uses the standard library's `crypto/ed25519`, with no new dependency.

- The **public key** is embedded in the binary (`internal/update/release.pub`, go:embed).
- The **private key never enters the repo.** It lives at `%APPDATA%\w5f-release\release.key`, and `.gitignore` also blocks `*.key`.

### 2. `w5f update` (`internal/update`)

`w5f update --check`:
1. Ask the GitHub API for the latest release (`GET https://api.github.com/repos/<repo>/releases/latest`; no token for a public repo).
2. Download the manifest and its signature and verify the signature against the embedded key. An unsigned or badly signed release is refused.
3. Report "up to date" or "0.6.4 → 0.7.0".

`w5f update` then continues:
4. Download the asset for this platform to `<exe>.new`, stopping at the size given in the manifest.
5. Check its sha256 against the signed manifest.
6. Run `<exe>.new selftest`, with a 20 s time limit. It must exit 0 and print its version, and that version must match the manifest.
7. Rename `<exe>` → `<exe>.prev`, then `<exe>.new` → `<exe>`. A running binary may be renamed on both Linux and Windows. The file mode is kept (0755).

Rules:
- **No downgrades.** A release older than or equal to the running version does nothing; the way back is `--rollback`.
- **Rollback** swaps `<exe>` and `<exe>.prev`, so running it twice returns to where you started.
- **Only the binary changes.** The data directory, `config.toml`, notes and cache are never touched.
- **No update without a key.** If the binary carries no public key (a dev build), `update` refuses and says why.

The repo comes from `config.toml` `[update] repo = "owner/w5f"`, or from the value set at build time (`-X`). The API base URL is a variable, so tests run against `httptest`.

### 3. `w5f selftest`

This is a quick check with **no user data**:
- lay out the embedded welcome page at 72 columns;
- open an in-memory SQLite database and create an FTS5 table;
- load the theme;
- print `w5f <version>` and exit 0.

The updater uses it to reject a broken binary before switching to it.

### 4. `w5f doctor` (`internal/doctor`)

Each check prints one line, `ok` / `warn` / `fail` plus the reason. The exit code is 1 if anything fails.

- **Base checks:**
  - **Install:** version and the binary's path, and whether that path is writable (needed by `update`).
  - **Folders and config:** the data, cache and notes directories exist and are writable; `config.toml` parses, with the error line shown if it does not.
  - **Database and search:** the database opens, `PRAGMA quick_check` passes, and FTS5 is present; also the item, book and serial counts.
  - **Dictionary:** installed or not (`g → dict-install`).
  - **Terminal:** size (warns under 80×24), colour support, and a UTF-8 locale (`LANG`/`LC_ALL` on Linux).
  - **Font:** Linux only. Whether Terminus is installed (`fc-list`, or the files kmscon uses), and the Turkish / box-drawing test line from the aesthetics plan, printed for a visual check.
  - **Update setup:** the repo is set and a public key is embedded.
- `--live`: one request to each of an RSS feed, SCP via Crom, Gemini (geminiprotocol.net), Gopher (Floodgap) and the GitHub releases API. Each is reported as ok / slow (>5 s) / fail, with timings.
- `--bench`: layout time for the welcome page and a synthetic long page of about 12k words, against the budget of 150 ms; process memory from `runtime.MemStats`; and the time from start to the first layout.

### 5. Release tool (`cmd/w5f-release`, developer only, not shipped)

- `go run ./cmd/w5f-release keygen` writes the key pair once. It refuses to overwrite an existing key.
- `go run ./cmd/w5f-release build -version 0.7.0`:
  - builds the three targets with CGO off and `-trimpath -s -w -X main.version=…`;
  - writes the release files to `dist/v0.7.0/` and signs the manifest;
  - prints the upload steps, both the GitHub web page and `gh release create v0.7.0 dist/v0.7.0/*`.
- It **does not upload.** Publishing to GitHub is the user's action.

### 6. Boot shell (`platform/antix/`, run on the laptop later)

- **`install.sh kmscon|x`:**
  - Installs the packages: kmscon; or xserver-xorg-core, xinit and xterm; plus `fonts-terminus`, `xfonts-terminus` and `fontconfig`.
  - Copies the binary to `~/.local/bin/w5f`, so `w5f update` needs no root.
  - Sets autologin on tty1. antiX uses sysvinit, so this is a `getty --autologin` line in `/etc/inittab`, with the old line backed up.
  - Installs `/etc/sudoers.d/w5f-power`, which allows only `poweroff` and `reboot`.
  - Can be undone with `install.sh --undo`.
- **`w5f-session`** runs W5F in a loop. When W5F quits, it offers a one-key menu: **w**5f again / **s**hell / **r**eboot / **p**ower off.
- **kmscon variant:** `kmscon.conf` with the Terminus font (size 16 or 20) and the Amber P3 palette as the 16 ANSI colours.
- **X variant:** `.xinitrc` starts xterm full screen with no window manager; `.Xresources` sets Terminus and the Amber P3 palette.
- **`measure.sh`** writes one report for the user to paste back:
  - hardware: `free -m`, the GPU and DRM state (`lspci`, `/dev/dri`);
  - software: the kmscon version and the installed fonts;
  - W5F: `w5f doctor --bench` and W5F's memory use.

  It covers what the laptop round needs to decide between kmscon and X.

### 7. Before the public repo (flags for the user, not done by me)

- `.gitignore`: `bin/`, `dist/`, `*.key`, `*.new`, `*.prev`.
- **Scan for secrets:** session cookies and tokens live in the data directory, not in the repo; this gets checked with a script before the first push.
- **Your decision:** a public repo would also publish:
  - the LibGen integration (`internal/libgen`, `books/libgen.go`);
  - the AO3 transport that imitates a browser (`fiction/ao3access.go`);
  - the saved third-party pages in `testdata` (AO3, Marginalia and others).

  I mark these here but do not change them.
- **Your action:** creating the repo, the first commit and push, and uploading releases. I provide the commands and do not run them.

## Plan (tasks; progress reported as N/5)

1. **update:**
   - Code: `internal/update/{manifest.go,update.go,version.go,release.pub}` and `cmd/w5f`: `update [--check|--rollback]`, `selftest`.
   - Tests (httptest fake release): a good release installs and rolls back; bad signature, bad sha256, oversized asset, older version, failing selftest (the old binary stays) and a missing key are refused.
2. **release tool:** `cmd/w5f-release` (keygen, build, sign). Test: keygen refuses to overwrite; the signed manifest verifies with the embedded public key; round trip with task 1.
3. **doctor:** `internal/doctor` + `cmd/w5f doctor [--live] [--bench]`. Tests: fake environments (unwritable dir, broken config, missing dictionary) give the expected ok/warn/fail lines; bench runs under test with a small page.
4. **platform:** `platform/antix/{install.sh,w5f-session,kmscon.conf,xinitrc,Xresources,measure.sh,README.md}`. Palette generated from `internal/theme` (a Go test keeps them in step). `sh -n` syntax check; not run on the laptop yet.
5. **docs + build:** README (update, doctor, platform), `.gitignore`, secret scan script, v0.7.0 build, vault notes.

## Tests the plan pins (review focus)

- A release signed with another key is refused, even with a correct sha256.
- An interrupted download never replaces the binary. The `.new` file is left behind and the next run cleans it up.
- Rollback twice gets back to the starting state; rollback with no `.prev` says so and changes nothing.
- `update` with no network reports it and leaves everything as it was.
- `doctor` never writes into the data directory. It probes writability in a temp file that it removes.
