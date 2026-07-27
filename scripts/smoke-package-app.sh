#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMPDIR="${TMPDIR:-/tmp}/transwarp-package-smoke.$$"
MISSING_STDERR="$TMPDIR/missing-cloudflared.stderr"

cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$TMPDIR"

if CLOUDFLARED_PATH="$TMPDIR/missing-cloudflared" \
	"$ROOT/scripts/package-app.sh" "$TMPDIR/MissingCloudflared.app" > /dev/null 2> "$MISSING_STDERR"; then
	echo "package-app accepted a missing cloudflared connector" >&2
	exit 1
fi

if ! grep -q "cloudflared is required for standalone packaging" "$MISSING_STDERR"; then
	echo "missing cloudflared failure did not explain standalone packaging requirement" >&2
	cat "$MISSING_STDERR" >&2
	exit 1
fi

echo "package app smoke passed"
