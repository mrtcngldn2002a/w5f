#!/bin/sh
# W5F // Archive Node — boot the W5F laptop (antiX, sysvinit) straight into W5F.
#
#   sh install.sh kmscon   tty1 runs kmscon (no X): Terminus, Amber P3
#   sh install.sh x        tty1 logs in and starts X with one full-screen xterm
#   sh install.sh --undo   put tty1, sudoers and the profile back as they were
#
# Run it as your own user from this folder, with the w5f-linux-amd64 binary
# next to it (or already at ~/.local/bin/w5f). It asks for sudo for the
# system parts. Nothing is removed that --undo cannot restore.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
ME=$(id -un)
BIN="$HOME/.local/bin"
BACKUP=/etc/inittab.w5f-backup
SUDOERS=/etc/sudoers.d/w5f-power
MARK="# w5f-session (install.sh)"

say() { printf '\n== %s\n' "$*"; }

undo() {
	say "Restoring tty1"
	if [ -f "$BACKUP" ]; then sudo cp "$BACKUP" /etc/inittab && sudo rm "$BACKUP"; else echo "no backup of /etc/inittab: nothing to restore"; fi
	sudo rm -f "$SUDOERS"
	if [ -f "$HOME/.profile" ]; then
		sed -i "/$MARK/,/# end w5f-session/d" "$HOME/.profile"
	fi
	if [ -f /etc/kmscon/kmscon.conf.w5f-backup ]; then sudo mv /etc/kmscon/kmscon.conf.w5f-backup /etc/kmscon/kmscon.conf; fi
	if [ -f "$HOME/.xinitrc.w5f-backup" ]; then mv "$HOME/.xinitrc.w5f-backup" "$HOME/.xinitrc"; fi
	echo "Done. Reboot to get the normal login back. (W5F itself stays in $BIN.)"
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

	say "Packages: Terminus font, fontconfig"
	sudo apt-get install -y fonts-terminus xfonts-terminus fontconfig

	say "Power menu: allow only poweroff and reboot without a password"
	tmp=$(mktemp)
	printf '%s ALL=(root) NOPASSWD: /sbin/poweroff, /sbin/reboot\n' "$ME" >"$tmp"
	sudo visudo -cf "$tmp" >/dev/null
	sudo install -m 0440 -o root -g root "$tmp" "$SUDOERS"
	rm -f "$tmp"

	say "tty1 (backup: $BACKUP)"
	if [ ! -f "$BACKUP" ]; then sudo cp /etc/inittab "$BACKUP"; fi
}

set_tty1() { # $1: the new inittab line for tty1
	sudo sed -i "s|^1:.*|$1|" /etc/inittab
	grep '^1:' /etc/inittab
}

display_manager_note() {
	for dm in slim lightdm lxdm xdm; do
		if [ -x "/etc/init.d/$dm" ] && ls /etc/rc[2-5].d/S*"$dm" >/dev/null 2>&1; then
			echo "NOTE: the display manager '$dm' starts at boot and will cover tty1."
			echo "      Turn it off with: sudo update-rc.d $dm disable   (on again: enable)"
		fi
	done
}

case "${1:-}" in
kmscon)
	install_common
	say "kmscon"
	sudo apt-get install -y kmscon
	sudo mkdir -p /etc/kmscon
	if [ -f /etc/kmscon/kmscon.conf ] && [ ! -f /etc/kmscon/kmscon.conf.w5f-backup ]; then
		sudo cp /etc/kmscon/kmscon.conf /etc/kmscon/kmscon.conf.w5f-backup
	fi
	sudo install -m 0644 "$HERE/kmscon.conf" /etc/kmscon/kmscon.conf
	KMSCON=$(command -v kmscon)
	set_tty1 "1:2345:respawn:$KMSCON --vt=1 --no-switchvt --login -- /bin/su -l $ME -c $BIN/w5f-session"
	display_manager_note
	;;
x)
	install_common
	say "X and xterm"
	sudo apt-get install -y xserver-xorg-core xserver-xorg-video-intel xinit xterm x11-xserver-utils x11-utils
	install -m 0644 "$HERE/Xresources" "$HOME/.Xresources"
	if [ -f "$HOME/.xinitrc" ] && [ ! -f "$HOME/.xinitrc.w5f-backup" ]; then cp "$HOME/.xinitrc" "$HOME/.xinitrc.w5f-backup"; fi
	install -m 0755 "$HERE/xinitrc" "$HOME/.xinitrc"
	GETTY=$(command -v agetty || command -v getty)
	set_tty1 "1:2345:respawn:$GETTY --autologin $ME --noclear 38400 tty1"
	if ! grep -q "$MARK" "$HOME/.profile" 2>/dev/null; then
		cat >>"$HOME/.profile" <<EOF
$MARK
if [ "\$(tty)" = /dev/tty1 ] && [ -z "\${DISPLAY:-}" ]; then exec startx; fi
# end w5f-session
EOF
	fi
	display_manager_note
	;;
--undo) undo ;;
*)
	sed -n '2,11p' "$0"
	exit 2
	;;
esac
say "Installed. Reboot to try it; 'sh install.sh --undo' puts everything back."
