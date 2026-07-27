#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-clean-mac-status-smoke.XXXXXX")"
STATUS_JSON="$TMPDIR/status.json"
BAD_STATUS_JSON="$TMPDIR/bad-status.json"

cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

cat > "$STATUS_JSON" <<'JSON'
{
  "machine_id": "clean-mac-smoke",
  "jobs": ["build", "clean-mac-launch"]
}
JSON

"$ROOT/scripts/validate-clean-mac-status.sh" "$STATUS_JSON" clean-mac-smoke clean-mac-launch >/dev/null

cat > "$BAD_STATUS_JSON" <<'JSON'
{
  "machine_id": "clean-mac-smoke",
  "jobs": ["build"]
}
JSON

if "$ROOT/scripts/validate-clean-mac-status.sh" "$BAD_STATUS_JSON" clean-mac-smoke clean-mac-launch >/dev/null 2>&1; then
	echo "clean-mac status validation accepted missing job" >&2
	exit 1
fi

echo "clean-mac status validation smoke passed"
