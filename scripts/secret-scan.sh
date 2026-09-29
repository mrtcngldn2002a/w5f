#!/bin/sh
# Look for secrets before a push: private keys, tokens, session cookies.
# Usage: sh scripts/secret-scan.sh   (exit 1 when something looks secret)
cd "$(dirname "$0")/.." || exit 2

files=$(find . -type f \( -name '*.key' -o -name '*.pem' -o -name '*_session*' -o -name '.env' \) \
	-not -path './.git/*' -not -path './bin/*' -not -path './dist/*' \
	-not -name '*.go' -not -name '*.md' 2>/dev/null)

lines=$(grep -rInE --exclude-dir=.git --exclude-dir=bin --exclude-dir=dist \
	-e '-----BEGIN [A-Z ]*PRIVATE KEY-----' \
	-e 'gh[pousr]_[A-Za-z0-9]{30,}' -e 'github_pat_[A-Za-z0-9_]{30,}' \
	-e 'reddit_session=[A-Za-z0-9._%-]{20,}' -e '_otwarchive_session=[A-Za-z0-9._%-]{20,}' \
	-e '(api[_-]?key|secret|token)["'"'"' ]*[:=]["'"'"' ]*[A-Za-z0-9_-]{24,}' \
	. 2>/dev/null)

if [ -z "$files$lines" ]; then
	echo "no secrets found"
	exit 0
fi
if [ -n "$files" ]; then printf 'SECRET? file %s\n' $files; fi
if [ -n "$lines" ]; then printf '%s\n' "$lines" | sed 's/^/SECRET? /'; fi
exit 1
