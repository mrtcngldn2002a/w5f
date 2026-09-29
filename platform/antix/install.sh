#!/bin/sh
# W5F // Archive Node — boot the W5F laptop (antiX) straight into W5F.
#
#   sh install.sh kmscon   tty1 runs kmscon (no X): Terminus, Amber P3
#   sh install.sh x        tty1 logs in and starts X with one full-screen xterm
#   sh install.sh --undo   put tty1, the display manager, sudoers and the profile back
#
# Run it as your own user from this folder, with the w5f-linux-amd64 binary
# next to it (or already at ~/.local/bin/w5f). It uses sudo for the system
# parts. Works with antiX's s6-rc (antiX 26) and with sysvinit's inittab.
# Nothing is changed that --undo cannot restore.
set -eu

if [ "$(id -u)" = 0 ]; then
	echo "Run this as your own user (sh install.sh ...), not with sudo: W5F goes to your home folder. It asks for sudo itself." >&2
	exit 1
fi

HERE=$(cd "$(dirname "$0")" && pwd)
ME=$(id -un)
BIN="$HOME/.local/bin"
SUDOERS=/etc/sudoers.d/w5f-power
FONTCONF=/etc/fonts/conf.d/60-w5f-terminus.conf
MARK="# w5f-session (install.sh)"
S6TTY=/etc/s6-rc/config/tty1.conf
INITTAB=/etc/inittab

say() { printf '\n== %s\n' "$*"; }

s6() { [ -f "$S6TTY" ] && command -v s6-service >/dev/null; }

# The display manager that would cover tty1 (slimski on antiX 26).
s6_dm() {
	for dm in slimski slim lightdm lxdm; do
		if [ -e "/etc/s6-rc/adminsv/enabled-services/contents.d/$dm-srv" ]; then echo "$dm"; return; fi
	done
}

backup() { # $1: a system file, copied once to $1.w5f-backup
	if [ -f "$1" ] && [ ! -f "$1.w5f-backup" ]; then sudo cp "$1" "$1.w5f-backup"; fi
}

restore() { # $1: a system file restored from its backup
	if [ -f "$1.w5f-backup" ]; then sudo mv "$1.w5f-backup" "$1"; echo "restored $1"; fi
}

profile_clean() {
	if [ -f "$HOME/.profile" ]; then sed -i "/$MARK/,/# end w5f-session/d" "$HOME/.profile"; fi
}

set_tty1() { # $1: getty program, $2: its arguments (words, no quoting)
	if s6; then
		backup "$S6TTY"
		printf '# Written by W5F install.sh (backup: %s.w5f-backup)\nSPAWN="yes"\nARGS="%s"\nGETTY="%s"\n' "$S6TTY" "$2" "$1" | sudo tee "$S6TTY" >/dev/null
		cat "$S6TTY"
	else
		backup "$INITTAB"
		sudo sed -i "s|^1:.*|1:2345:respawn:$1 $2|" "$INITTAB"
		grep '^1:' "$INITTAB"
	fi
}

dm_off() {
	if s6; then
		dm=$(s6_dm)
		if [ -n "$dm" ]; then
			say "Display manager $dm: off at boot (it keeps running until the restart)"
			sudo s6-service del enabled-services "$dm-srv" "$dm-log"
			echo "$dm" | sudo tee /etc/s6-rc/w5f-disabled-dm >/dev/null
			sudo s6-db-reload
		fi
	else
		for dm in slim lightdm lxdm xdm; do
			if [ -x "/etc/init.d/$dm" ] && ls /etc/rc[2-5].d/S*"$dm" >/dev/null 2>&1; then
				echo "NOTE: the display manager '$dm' starts at boot and will cover tty1."
				echo "      Turn it off with: sudo update-rc.d $dm disable   (on again: enable)"
			fi
		done
	fi
}

undo() {
	say "Restoring tty1"
	restore "$S6TTY"
	restore "$INITTAB"
	if [ -f /etc/s6-rc/w5f-disabled-dm ]; then
		dm=$(cat /etc/s6-rc/w5f-disabled-dm)
		say "Display manager $dm: on at boot again"
		sudo s6-service add enabled-services "$dm-srv" "$dm-log"
		sudo rm /etc/s6-rc/w5f-disabled-dm
		sudo s6-db-reload
	fi
	sudo rm -f "$SUDOERS"
	restore /etc/kmscon/kmscon.conf
	profile_clean
	if [ -f "$HOME/.xinitrc.w5f-backup" ]; then mv "$HOME/.xinitrc.w5f-backup" "$HOME/.xinitrc"; fi
	echo "Done. Restart to get the normal login back. (W5F itself stays in $BIN; fonts stay installed.)"
}

install_common() {
	say "W5F binary and session in $BIN"
	mkdir -p "$BIN"
	if [ -f "$HERE/w5f-linux-amd64" ]; then
		install -m 0755 "$HERE/w5f-linux-amd64" "$BIN/w5f"
	elif [ ! -x "$BIN/w5f" ]; then
		echo "no w5f-linux-amd64 next to this script and no $BIN/w5f: copy the binary first" >&2
		exit 1
	fi
	install -m 0755 "$HERE/w5f-session" "$BIN/w5f-session"
	"$BIN/w5f" selftest

	say "Packages: Terminus fonts, fontconfig"
	sudo apt-get install -y fonts-terminus fonts-terminus-otb xfonts-terminus fontconfig
	# antiX turns bitmap fonts off for fontconfig; Terminus is let back in.
	printf '%s\n' '<?xml version="1.0"?>' '<!DOCTYPE fontconfig SYSTEM "fonts.dtd">' \
		'<fontconfig><selectfont><acceptfont><pattern><patelt name="family"><string>Terminus</string></patelt></pattern></acceptfont></selectfont></fontconfig>' |
		sudo tee "$FONTCONF" >/dev/null
	sudo fc-cache -f >/dev/null
	echo "Terminus for fontconfig: $(fc-list | grep -ic terminus) files"

	say "Power menu: allow only poweroff and reboot without a password"
	tmp=$(mktemp)
	printf '%s ALL=(root) NOPASSWD: /sbin/poweroff, /sbin/reboot\n' "$ME" >"$tmp"
	sudo visudo -cf "$tmp" >/dev/null
	sudo install -m 0440 -o root -g root "$tmp" "$SUDOERS"
	rm -f "$tmp"
}

case "${1:-}" in
kmscon)
	install_common
	say "kmscon"
	# kmscon comes from Debian backports, with the newer libtsm it needs.
	bpo="$(. /etc/os-release && echo "${VERSION_CODENAME:-}")-backports"
	sudo apt-get install -y -t "$bpo" kmscon || sudo apt-get install -y kmscon
	sudo mkdir -p /etc/kmscon
	backup /etc/kmscon/kmscon.conf
	sudo install -m 0644 "$HERE/kmscon.conf" /etc/kmscon/kmscon.conf
	profile_clean
	say "tty1 → kmscon → W5F"
	set_tty1 "$(command -v kmscon)" "--vt=1 --no-switchvt --login -- /bin/su --pty -l $ME -c $BIN/w5f-session"
	dm_off
	;;
x)
	install_common
	say "X and xterm"
	sudo apt-get install -y xserver-xorg-core xserver-xorg-video-intel xinit xterm x11-xserver-utils x11-utils
	install -m 0644 "$HERE/Xresources" "$HOME/.Xresources"
	if [ -f "$HOME/.xinitrc" ] && [ ! -f "$HOME/.xinitrc.w5f-backup" ]; then cp "$HOME/.xinitrc" "$HOME/.xinitrc.w5f-backup"; fi
	install -m 0755 "$HERE/xinitrc" "$HOME/.xinitrc"
	profile_clean
	cat >>"$HOME/.profile" <<EOF
$MARK
if [ "\$(tty)" = /dev/tty1 ] && [ -z "\${DISPLAY:-}" ]; then exec startx; fi
# end w5f-session
EOF
	say "tty1 → autologin → X → W5F"
	set_tty1 "$(command -v agetty || command -v getty)" "-L -8 --autologin $ME --noclear tty1 115200"
	dm_off
	;;
--undo) undo ;;
*)
	sed -n '2,11p' "$0"
	exit 2
	;;
esac
say "Installed. Restart now (sudo reboot): reloading the service list can leave turnstiled stuck, and new logins (SSH too) hang until the restart. 'sh install.sh --undo' puts everything back."
