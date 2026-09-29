#!/bin/sh
# W5F // Archive Node — one report for choosing kmscon or X on the laptop.
#
#   sh measure.sh [label]      e.g. "sh measure.sh kmscon" while W5F is open
#
# Run it from a second console (Ctrl+Alt+F2) while W5F is open on tty1, once
# per variant. It only reads; the report is appended to ~/w5f-measure.txt.
OUT="$HOME/w5f-measure.txt"
W5F=${W5F:-$HOME/.local/bin/w5f}
{
	printf '\n===== %s · %s =====\n' "${1:-snapshot}" "$(date '+%Y-%m-%d %H:%M')"
	echo "-- system"
	uname -srm
	cat /etc/antix-version 2>/dev/null
	grep -m1 'model name' /proc/cpuinfo
	free -m
	echo "-- graphics"
	lspci 2>/dev/null | grep -i -E 'vga|display'
	ls /dev/dri 2>/dev/null || echo "no /dev/dri (no KMS: kmscon cannot run)"
	lsmod 2>/dev/null | grep -E '^i915' || echo "i915 not loaded"
	echo "-- consoles"
	if command -v kmscon >/dev/null; then kmscon --version 2>&1 | head -1; else echo "kmscon not installed"; fi
	if command -v Xorg >/dev/null; then Xorg -version 2>&1 | grep -m1 'X.Org X Server'; else echo "Xorg not installed"; fi
	echo "-- fonts and locale"
	fc-list 2>/dev/null | grep -i -c terminus | sed 's/^/Terminus font files: /'
	locale 2>/dev/null | grep -E '^(LANG|LC_ALL|LC_CTYPE)='
	echo "-- memory of the running pieces (RSS in MB)"
	ps -eo rss=,comm= 2>/dev/null | awk '$2 ~ /^(w5f|kmscon|Xorg|xterm|su|agetty|getty)$/ {printf "%-8s %6.1f\n", $2, $1/1024}'
	echo "-- w5f"
	"$W5F" version
	"$W5F" doctor --bench
} >>"$OUT" 2>&1
echo "Written to $OUT — paste it back into the chat."
