# The optional Byparr helper

`w5f solver install` downloads Byparr on the computer where it will run. It is
not bundled with a W5F release. Linux amd64, Windows amd64 and macOS (Apple
silicon and Intel) are supported; Linux 386 keeps the reader and
external-helper support, but cannot use this installer. Windows installation,
BHL page opening and a fresh Suwayomi's helper settings were tested on
2026-10-02. macOS was added on 2026-10-04 before a live test on a Mac: the
same steps with uv's own macOS build (pinned by SHA-256 like the others), no
Xvfb (Byparr runs headless and its browser, which ships macOS builds, hides
its own window), and the kernel's process table in place of /proc for owning
and stopping the helper. `DarwinEnabled = "no"` in `internal/solver` turns it
off if a Mac shows otherwise.

W5F asks once on first launch if neither a helper nor an installation exists.
`n` only keeps the question quiet. Installation remains available from the
shell and `g → solver`. The page's **ask me again** link, or
`w5f solver ask-again`, resets the question for the next launch.

```sh
w5f solver install         # confirmation, default No
w5f solver install --yes   # unattended installation
w5f solver status          # version, folder, size, readiness and owner
w5f solver start           # explicitly keep a W5F-owned helper running
w5f solver stop            # stop only a helper in W5F's validated process record
w5f solver update          # update separately, after confirmation
w5f solver remove          # confirmation, default No; only listed files
```

Installation has five visible steps. Ctrl+C or Esc cancels; W5F waits for child
processes and cleanup before exiting. Allow about 1.1 GB installed and at least
1.5 GB free for temporary downloads. Windows measured about 0.9 GB; Linux about
1.0 GB for the pinned version. Network conditions and upstream GeoIP releases
can change these measurements.

## Downloads and ownership

- Byparr v3.0.4 is downloaded as its official tag archive, pinned by SHA-256.
- uv 0.12.22 is downloaded for the platform, pinned by SHA-256 from its official
  release checksum. uv's Python download metadata includes SHA-256; its source
  verifies the Python archive during extraction.
- Python 3.14 is private to the installation. `uv sync --frozen --no-dev`
  installs packages from Byparr's untouched lock file and checks their hashes.
- Firefox is fetched by invisible-playwright's own CLI, checked against the
  SHA-256 in invisible-core's seal. GeoIP uses that library's own latest-release
  download without a pinned checksum, as agreed in the installation plan.
- The Python runtime, uv binaries, Firefox and GeoIP live inside
  `<data>/byparr-v3.0.4/`. Temporary uv caches are outside the page cache and are
  removed after success or failure. Existing `~/.cache/w5f/uv`,
  `flaresolverr-install` and `byparr-install` are never used or touched.
- `w5f-install.json` lists installed files and directories.
  Firefox's stable font list is generated before this list is written, so a
  first browser session does not leave that runtime file behind on removal.
  Removal validates
  the whole list first, rejects paths or symbolic-link parents that escape the
  installation, and leaves unlisted user files in place. If such files remain,
  it reports this and retains the list. A manual Byparr installation has no W5F
  list: it is adopted, never overwritten or removed.

Byparr's source is unchanged. On Windows, the archive's internal `.dockerignore`
link is materialized as the same `.gitignore` contents because Windows links
may require elevated rights. This does not change the application code.

## Starting and stopping

The helper listens on `127.0.0.1:8191` with `BLOCK_MEDIA=true`. Readiness checks
read `/openapi.json` and its `/v1` path; they never call Byparr's browser-opening
`/health`. An unknown service is not sent a page request. Already-running
Byparr or FlareSolverr is reused, with no second process.

When the reader or `dump` starts a helper, it owns that process until exit.
Process records include a creation identity to prevent an old pid file from
killing a reused PID. Another reader that only reused the helper never stops
it. `solver start` deliberately persists until `solver stop`. The laptop's s6
service is external and remains running when W5F exits.

Linux needs Xvfb. If missing, `doctor` and the solver page show an apt/dnf/pacman
hint; W5F does not run privileged package-manager commands. The laptop's existing
wrapper is kept. Else W5F creates its own launcher under `<data>/solver/bin` to
use Xvfb's local Unix socket without a TCP listener. Helper logs and process
records are also under `<data>/solver/`, separate from the installed payload.
Shutdown includes separately-sessioned Xvfb descendants of W5F's own helper.

`[fetch] solver_url` / `W5F_SOLVER_URL` configures the endpoint; an empty value
turns solving off. Suwayomi's helper setting is initialized once on a server
W5F starts; later user choices remain unchanged. HathiTrust is still excluded.

## Updating

W5F updates never install, replace or remove Byparr. A newer W5F pin is reported
by `doctor` and the solver page. `solver update` installs the pinned candidate
alongside the old version, starts it on another local port, checks its API and
only then changes the active selection. A failed check keeps the old selection.
One previous W5F-owned version is kept for recovery; manual installations and
versions potentially used by external services are not pruned. Restart an
external s6 service yourself after an update; its existing launcher picks the
newest installation.

Primary verification sources:

- [Byparr v3.0.4](https://github.com/ThePhaseless/Byparr/tree/v3.0.4)
- [uv 0.12.22 releases](https://github.com/astral-sh/uv/releases/tag/0.12.22)
- [uv Python archive verification](https://github.com/astral-sh/uv/blob/0.12.22/crates/uv-python/src/downloads.rs)
