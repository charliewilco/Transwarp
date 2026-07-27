#!/bin/sh
set -eu

STATUS_JSON="${1:-}"
EXPECTED_MACHINE_ID="${2:-}"
EXPECTED_JOB_ID="${3:-}"
PLUTIL="${PLUTIL:-/usr/bin/plutil}"

fail() {
	echo "clean-mac status validation failed: $*" >&2
	exit 1
}

[ -n "$STATUS_JSON" ] || fail "status JSON path is required"
[ -n "$EXPECTED_MACHINE_ID" ] || fail "expected machine ID is required"
[ -n "$EXPECTED_JOB_ID" ] || fail "expected job ID is required"
[ -f "$STATUS_JSON" ] || fail "$STATUS_JSON does not exist"
[ -x "$PLUTIL" ] || fail "plutil is required at $PLUTIL"

ACTUAL_MACHINE_ID="$("$PLUTIL" -extract machine_id raw -o - "$STATUS_JSON" 2>/dev/null || true)"
[ "$ACTUAL_MACHINE_ID" = "$EXPECTED_MACHINE_ID" ] || fail "expected machine_id=$EXPECTED_MACHINE_ID, got ${ACTUAL_MACHINE_ID:-<missing>}"

JOB_COUNT="$("$PLUTIL" -extract jobs raw -o - "$STATUS_JSON" 2>/dev/null || true)"
case "$JOB_COUNT" in
	""|*[!0-9]*)
		fail "jobs array is missing"
		;;
esac

FOUND_JOB=false
index=0
while [ "$index" -lt "$JOB_COUNT" ]; do
	JOB_ID="$("$PLUTIL" -extract "jobs.$index" raw -o - "$STATUS_JSON" 2>/dev/null || true)"
	if [ "$JOB_ID" = "$EXPECTED_JOB_ID" ]; then
		FOUND_JOB=true
		break
	fi
	index=$((index + 1))
done

[ "$FOUND_JOB" = "true" ] || fail "status did not advertise job $EXPECTED_JOB_ID"

echo "clean-mac status validation passed"
