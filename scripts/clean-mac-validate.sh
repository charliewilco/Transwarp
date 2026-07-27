#!/bin/sh
set -eu
umask 077

APP_DIR="${1:-}"
PORT="${TRANSWARP_CLEAN_MAC_SMOKE_PORT:-18191}"
CLEAN_MAC_EVIDENCE="${TRANSWARP_CLEAN_MAC_EVIDENCE:-}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
STATUS_VALIDATOR="$SCRIPT_DIR/validate-clean-mac-status.sh"
AUDIT_CLI="${TRANSWARP_AUDIT_CLI:-$SCRIPT_DIR/transwarp-audit}"
CONFIG_CLI="${TRANSWARP_CONFIG_CLI:-$SCRIPT_DIR/transwarp-config}"
PLUTIL="${PLUTIL:-/usr/bin/plutil}"

fail() {
	echo "clean-mac-validation failed: $*" >&2
	exit 1
}

if [ -z "$APP_DIR" ]; then
	fail "usage: scripts/clean-mac-validate.sh /path/to/Transwarp.app"
fi

APP_DIR="$(cd "$(dirname "$APP_DIR")" && pwd)/$(basename "$APP_DIR")"
APP_EXEC="$APP_DIR/Contents/MacOS/Transwarp"
RUNNER_EXEC="$APP_DIR/Contents/Resources/transwarp-runner"
CLOUDFLARED_EXEC="$APP_DIR/Contents/Resources/cloudflared"
MANIFEST_JSON="$APP_DIR/Contents/Resources/TranswarpManifest.json"

[ -d "$APP_DIR" ] || fail "$APP_DIR does not exist"
[ -x "$APP_EXEC" ] || fail "$APP_EXEC is not executable"
[ -x "$RUNNER_EXEC" ] || fail "$RUNNER_EXEC is not executable"
[ -x "$CLOUDFLARED_EXEC" ] || fail "$CLOUDFLARED_EXEC is not executable"
[ -f "$MANIFEST_JSON" ] || fail "$MANIFEST_JSON does not exist"
[ -x "$PLUTIL" ] || fail "plutil is required at $PLUTIL"

TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-clean-mac.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
APP_LOG="$TMPDIR/app.log"
APP_ERR="$TMPDIR/app.err"
STATUS_JSON="$TMPDIR/status.json"
START_JSON="$TMPDIR/build-start.json"
BUILD_LOG="$TMPDIR/build-log.ndjson"
BUILD_STATUS_JSON="$TMPDIR/build-status.json"
CODESIGN_LOG="$TMPDIR/codesign.log"
STAPLER_LOG="$TMPDIR/stapler.log"
GATEKEEPER_LOG="$TMPDIR/gatekeeper.log"
APP_PID=""
MACHINE_ID="clean-mac-$(date +%s)-$$"
REQUEST_ID="clean-mac-launch-$(date +%s)-$$"
TOKEN="clean-mac-token-$(date +%s)-$$"
ARCH="$(uname -m)"
MACOS_NAME="$(sw_vers -productName)"
MACOS_VERSION="$(sw_vers -productVersion)"

json_string_field() {
	path="$1"
	field="$2"
	"$PLUTIL" -extract "$field" raw -o - "$path" 2>/dev/null || true
}

require_stable_identifier() {
	value="$1"
	field="$2"
	limit="$3"
	bytes="$(printf "%s" "$value" | wc -c | tr -d " ")"
	case "$value" in
		""|*[!A-Za-z0-9._-]*)
			fail "$field is not a stable identifier"
			;;
	esac
	if [ "$bytes" -gt "$limit" ]; then
		fail "$field is too long"
	fi
}

run_audit() {
	if [ -x "$AUDIT_CLI" ]; then
		"$AUDIT_CLI" "$@"
	else
		(
			cd "$SCRIPT_DIR/.."
			go run ./cmd/transwarp-audit "$@"
		)
	fi
}

run_config() {
	if [ -x "$CONFIG_CLI" ]; then
		"$CONFIG_CLI" "$@"
	else
		(
			cd "$SCRIPT_DIR/.."
			go run ./cmd/transwarp-config "$@"
		)
	fi
}

write_clean_mac_evidence() {
	if [ -z "$CLEAN_MAC_EVIDENCE" ]; then
		return 0
	fi
	EVIDENCE_DIR="$(dirname "$CLEAN_MAC_EVIDENCE")"
	mkdir -p "$EVIDENCE_DIR"
	run_audit \
		-app "$APP_DIR" \
		-write-clean-mac-evidence "$CLEAN_MAC_EVIDENCE" \
		-clean-mac-architecture "$ARCH" \
		-clean-mac-os "$MACOS_NAME $MACOS_VERSION" \
		-clean-mac-machine-id "$MACHINE_ID" \
		-clean-mac-job-id clean-mac-launch \
		-clean-mac-request-id "$REQUEST_ID" \
		-clean-mac-build-id "$BUILD_ID" \
		-clean-mac-status-json "$STATUS_JSON" \
		-clean-mac-build-log "$BUILD_LOG" \
		-clean-mac-build-status-json "$BUILD_STATUS_JSON" \
		-clean-mac-codesign-log "$CODESIGN_LOG" \
		-clean-mac-stapler-log "$STAPLER_LOG" \
		-clean-mac-gatekeeper-log "$GATEKEEPER_LOG" \
		-clean-mac-app-log "$APP_LOG" \
		-clean-mac-app-stderr "$APP_ERR"
	echo "clean-mac evidence: $CLEAN_MAC_EVIDENCE"
}

cleanup() {
	if [ -n "$APP_PID" ]; then
		kill "$APP_PID" 2>/dev/null || true
		wait "$APP_PID" 2>/dev/null || true
	fi
	security delete-generic-password -s co.charliewil.transwarp -a "$MACHINE_ID/shared_token" >/dev/null 2>&1 || true
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

echo "clean-mac-validation started"
echo "app: $APP_DIR"
sw_vers
printf "architecture: %s\n" "$ARCH"

if ! codesign --verify --deep --strict --verbose=2 "$APP_DIR" >"$CODESIGN_LOG" 2>&1; then
	cat "$CODESIGN_LOG" >&2
	fail "strict codesign verification failed"
fi
cat "$CODESIGN_LOG"

STAPLER_OUTPUT="$(xcrun stapler validate "$APP_DIR" 2>&1 || true)"
printf '%s\n' "$STAPLER_OUTPUT" >"$STAPLER_LOG"
printf '%s\n' "$STAPLER_OUTPUT"
printf '%s\n' "$STAPLER_OUTPUT" | grep -q "The validate action worked" || fail "stapled notarization ticket is not valid"

SPCTL_OUTPUT="$(spctl -a -vv --type execute "$APP_DIR" 2>&1 || true)"
printf '%s\n' "$SPCTL_OUTPUT" >"$GATEKEEPER_LOG"
printf '%s\n' "$SPCTL_OUTPUT"
printf '%s\n' "$SPCTL_OUTPUT" | grep -q "accepted" || fail "Gatekeeper did not accept $APP_DIR"
echo "Gatekeeper accepted"

TRANSWARP_TOKEN="$TOKEN" run_config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$PORT" \
	-machine-id "$MACHINE_ID" \
	-machine-name "Transwarp Clean Mac" \
	-workspace-root "" \
	-heartbeat-seconds 30 \
	-tunnel-mode off \
	-cloudflared-path "@bundle/cloudflared" \
	-job-id clean-mac-launch \
	-job-label "Clean Mac Launch" \
	-job-working-directory "$TMPDIR" \
	-job-command /usr/bin/true \
	-job-timeout-seconds 60

TRANSWARP_CONFIG_PATH="$CONFIG" TRANSWARP_START_RUNNER_ON_LAUNCH=1 "$APP_EXEC" >"$APP_LOG" 2>"$APP_ERR" &
APP_PID=$!

for _ in $(seq 1 100); do
	if curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/status" >"$STATUS_JSON"; then
		break
	fi
	sleep 0.1
done

if ! curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/status" >"$STATUS_JSON"; then
	echo "app stdout:" >&2
	sed -n '1,120p' "$APP_LOG" >&2 || true
	echo "app stderr:" >&2
	sed -n '1,120p' "$APP_ERR" >&2 || true
	fail "first launch did not start an authenticated loopback runner"
fi

"$STATUS_VALIDATOR" "$STATUS_JSON" "$MACHINE_ID" clean-mac-launch >/dev/null

curl -fsS \
	-X POST \
	-H "Authorization: Bearer $TOKEN" \
	-H "Content-Type: application/json" \
	-d "{\"job_id\":\"clean-mac-launch\",\"request_id\":\"$REQUEST_ID\"}" \
	"http://127.0.0.1:$PORT/v1/builds" >"$START_JSON"

BUILD_ID="$(json_string_field "$START_JSON" build_id)"
if [ -z "$BUILD_ID" ]; then
	echo "clean-Mac runner did not return a build_id" >&2
	sed -n '1,120p' "$START_JSON" >&2
	fail "first launch did not accept a build request"
fi
require_stable_identifier "$BUILD_ID" build_id 128

curl -fsS \
	-H "Authorization: Bearer $TOKEN" \
	"http://127.0.0.1:$PORT/v1/builds/$BUILD_ID/logs?after=0&follow=true" >"$BUILD_LOG"

grep -q "\"build_id\":\"$BUILD_ID\"" "$BUILD_LOG" || fail "build log did not include accepted build_id"
grep -q '"message":"passed"' "$BUILD_LOG" || fail "build log did not record pass"

curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/v1/builds/$BUILD_ID" >"$BUILD_STATUS_JSON"
grep -q "\"request_id\":\"$REQUEST_ID\"" "$BUILD_STATUS_JSON" || fail "build status did not include request_id"
grep -q "\"build_id\":\"$BUILD_ID\"" "$BUILD_STATUS_JSON" || fail "build status did not include build_id"
grep -q '"status":"passed"' "$BUILD_STATUS_JSON" || fail "build status did not pass"

curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/status" >"$STATUS_JSON"
grep -q "\"request_id\":\"$REQUEST_ID\"" "$STATUS_JSON" || fail "runner status did not record recent build request_id"
grep -q "\"build_id\":\"$BUILD_ID\"" "$STATUS_JSON" || fail "runner status did not record recent build_id"
grep -q '"status":"passed"' "$STATUS_JSON" || fail "runner status did not record passed recent build"

echo "first launch passed"
write_clean_mac_evidence
echo "clean-mac-validation passed"
