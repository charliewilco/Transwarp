#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${TRANSWARP_APP_PATH:-$ROOT/.build/Transwarp.app}"
MANIFEST="$APP_DIR/Contents/Resources/TranswarpManifest.json"
MISSING_STDERR="$ROOT/.build/release-gate-missing-policy.stderr"
MISMATCH_STDERR="$ROOT/.build/release-gate-mismatch.stderr"
MATCH_STDERR="$ROOT/.build/release-gate-match.stderr"

cd "$ROOT"

if [ ! -d "$APP_DIR" ]; then
	./scripts/package-app.sh "$APP_DIR" >/dev/null
fi

actual="$(plutil -extract cloudflared_version raw "$MANIFEST")"

./scripts/release-gate.sh "$APP_DIR" > /dev/null 2> "$MISSING_STDERR"
if ! grep -q "expected cloudflared version policy is not configured" "$MISSING_STDERR"; then
	echo "release gate did not warn about missing cloudflared policy" >&2
	exit 1
fi

if TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke-mismatch" \
	./scripts/release-gate.sh "$APP_DIR" > /dev/null 2> "$MISMATCH_STDERR"; then
	echo "release gate accepted mismatched cloudflared policy" >&2
	exit 1
fi

if ! grep -q "bundled cloudflared version does not match policy" "$MISMATCH_STDERR"; then
	echo "release gate mismatch did not explain cloudflared policy failure" >&2
	exit 1
fi

TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="$actual" ./scripts/release-gate.sh "$APP_DIR" > /dev/null 2> "$MATCH_STDERR"

echo "release gate smoke passed"
