#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-clean-mac-validate-smoke.XXXXXX")"
FAKEBIN="$TMPDIR/bin"
EVIDENCE_DIR="$TMPDIR/evidence"
CLEAN_MAC_EVIDENCE="$EVIDENCE_DIR/clean-mac-evidence.json"
SMOKE_LOG="$TMPDIR/clean-mac-validate.log"
PORT="${TRANSWARP_CLEAN_MAC_VALIDATE_SMOKE_PORT:-18192}"

cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

cd "$ROOT"

case "$APP_DIR" in
	/*) ;;
	*) APP_DIR="$ROOT/$APP_DIR" ;;
esac
APP_DIR="$(cd "$(dirname "$APP_DIR")" && pwd)/$(basename "$APP_DIR")"
APP_EXEC="$APP_DIR/Contents/MacOS/Transwarp"

if [ ! -x "$APP_EXEC" ]; then
	./scripts/package-app.sh "$APP_DIR" >/dev/null
fi

mkdir -p "$FAKEBIN" "$EVIDENCE_DIR"

cat > "$FAKEBIN/codesign" <<'SH'
#!/bin/sh
last=""
for arg in "$@"; do
	last="$arg"
done
echo "$last: valid on disk"
echo "$last: satisfies its Designated Requirement"
exit 0
SH

cat > "$FAKEBIN/xcrun" <<'SH'
#!/bin/sh
if [ "${1:-}" = "stapler" ] && [ "${2:-}" = "validate" ]; then
	echo "The validate action worked!"
	exit 0
fi
echo "unexpected xcrun invocation: $*" >&2
exit 1
SH

cat > "$FAKEBIN/spctl" <<'SH'
#!/bin/sh
echo "$*: accepted"
exit 0
SH

chmod 755 "$FAKEBIN/codesign" "$FAKEBIN/xcrun" "$FAKEBIN/spctl"

if ! PATH="$FAKEBIN:$PATH" \
	TRANSWARP_CLEAN_MAC_EVIDENCE="$CLEAN_MAC_EVIDENCE" \
	TRANSWARP_CLEAN_MAC_SMOKE_PORT="$PORT" \
	./scripts/clean-mac-validate.sh "$APP_DIR" >"$SMOKE_LOG" 2>&1; then
	echo "clean-mac validation smoke failed; log follows" >&2
	sed -n '1,200p' "$SMOKE_LOG" >&2 || true
	exit 1
fi

go run ./cmd/transwarp-audit \
	-evidence-only \
	-app "$APP_DIR" \
	-clean-mac-evidence "$CLEAN_MAC_EVIDENCE" \
	-summary >"$TMPDIR/clean-mac-audit-summary.log"

echo "clean-mac validation smoke passed"
