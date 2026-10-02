# W5F on the ASUS W5F (antiX)

These files make the laptop boot straight into W5F. There are two variants;
both are written but **not yet tried on the laptop**. That round measures them
and keeps one.

| Variant | What runs on tty1 | For | Against |
|---|---|---|---|
| `kmscon` | kmscon (a KMS console, no X) → W5F | least memory, TrueType Terminus, full Unicode | needs KMS on the GMA 950 (i915); pictures and PDFs need X anyway |
| `x` | autologin → `startx` → one full-screen xterm → W5F | proven on this laptop in v1; pictures and PDFs just work | a bit more memory |

## Install

Copy this folder and `w5f-linux-amd64` (from a release) to the laptop, then, as
your own user:

```sh
sh install.sh kmscon    # or: sh install.sh x
```

It asks for sudo for the system parts:
- the Terminus fonts;
- tty1: antiX 26 (s6-rc) in `/etc/s6-rc/config/tty1.conf`, older antiX in `/etc/inittab`, backed up first;
- the display manager (slimski on antiX 26) is taken off the boot list and brought back by `--undo`;
- a fontconfig exception for Terminus, because antiX turns bitmap fonts off;
- a note file `~/.user_session.d/s6-rc-user-session.sh`. It makes turnstile skip antiX's user session script, whose exit trap restarts the user manager every few seconds and hangs logins (SSH too);
- `/etc/sudoers.d/w5f-power`, which allows only `poweroff` and `reboot`, for the menu.

W5F itself goes to `~/.local/bin`, so `w5f update` needs no root.

Undo everything with `sh install.sh --undo`.

Restart right after installing or undoing. On antiX 26 the script reloads the
s6-rc service list, and that can leave `turnstiled` stuck. New logins, SSH
included, then hang until the restart (seen on 2026-09-29).

## Byparr (the bot-check helper)

W5F and the Suwayomi it starts ask a local helper at `127.0.0.1:8191` when a
site answers with a verification wall (see *Bot-check helper* in the main
README). With Byparr in `~/.local/share/w5f/byparr-v*` (the newest one is
used), this runs it as an s6 service, as your own user, started at boot:

```sh
sh install.sh byparr        # byparr-srv, its log in /var/log/byparr
sh install.sh byparr-undo   # takes the service away again (so does --undo)
```

- `w5f-byparr` goes to `~/.local/bin`; `w5f-byparr --check` says which Byparr would start.
- It listens on 127.0.0.1 only; Byparr's own default would be every address.
- A Byparr started by hand is stopped; the service takes the port over.
- Byparr itself is not installed, changed or removed by this; neither is anything in `~/.cache/w5f`.
- On the laptop (2026-10-02): about 100 MB while idle, about 560 MB more while it opens a page; it starts in about 4 s.

## The session

`w5f-session` runs W5F. When W5F closes it offers one key:

- **w** — open W5F again. This is also how an update takes effect.
- **s** — a shell.
- **r** — restart the laptop.
- **p** — power off.

## Measure

With W5F open on tty1, switch to a second console (Ctrl+Alt+F2), log in and run:

```sh
sh measure.sh kmscon    # or: sh measure.sh x
```

The report goes to `~/w5f-measure.txt`: memory, graphics/KMS, fonts,
`w5f doctor --bench`. Do it once per variant and paste the file back.

## Palette and font

Amber P3 as the 16 console colours, and Terminus 16 px, which gives 8×16 cells
and 160×50 on the 1280×800 screen. `kmscon.conf` and `Xresources` carry the
same colours as `internal/theme`, and a Go test keeps them in step.
