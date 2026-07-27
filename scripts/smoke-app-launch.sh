#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
APP_EXEC="$APP_DIR/Contents/MacOS/Transwarp"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-app-launch-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
APP_LOG="$TMPDIR/app.log"
APP_ERR="$TMPDIR/app.err"
STATUS_JSON="$TMPDIR/status.json"
PUBLIC_DIAGNOSE_LOG="$TMPDIR/public-diagnose.log"
PUBLIC_DISPATCH_LOG="$TMPDIR/public-dispatch.log"
START_JSON="$TMPDIR/start.json"
BUILD_STATUS_JSON="$TMPDIR/build-status.json"
BUILD_LOG="$TMPDIR/build.ndjson"
DIAGNOSE_BIN="$TMPDIR/transwarp-diagnose"
DISPATCH_BIN="$TMPDIR/transwarp-dispatch"
APP_PID=""
MACHINE_ID="launch-smoke-$(date +%s)-$$"
TOKEN="launch-smoke-token-$(date +%s)-$$"
PORT="${TRANSWARP_SMOKE_PORT:-18190}"
REQUEST_ID="app-launch-smoke-run"
APP_LAUNCH_EVIDENCE="${TRANSWARP_APP_LAUNCH_EVIDENCE:-}"
APP_LAUNCH_RECEIPT="${APP_LAUNCH_EVIDENCE:-$TMPDIR/app-launch-evidence.json}"
APP_LAUNCH_TUNNEL_MODE="${TRANSWARP_APP_LAUNCH_TUNNEL_MODE:-off}"
APP_LAUNCH_PUBLIC_ATTEMPTS="${TRANSWARP_APP_LAUNCH_PUBLIC_ATTEMPTS:-180}"
PLUTIL="${PLUTIL:-/usr/bin/plutil}"

fail() {
	echo "app launch smoke failed: $*" >&2
	exit 1
}

json_string_field() {
	path="$1"
	field="$2"
	"$PLUTIL" -extract "$field" raw -o - "$path" 2>/dev/null || true
}

json_bool_field() {
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

cleanup() {
	if [ -n "$APP_PID" ]; then
		kill "$APP_PID" 2>/dev/null || true
		wait "$APP_PID" 2>/dev/null || true
	fi
	security delete-generic-password -s co.charliewil.transwarp -a "$MACHINE_ID/shared_token" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

cd "$ROOT"
[ -x "$PLUTIL" ] || fail "plutil is required at $PLUTIL"
case "$APP_LAUNCH_TUNNEL_MODE" in
	off|quick) ;;
	*) fail "TRANSWARP_APP_LAUNCH_TUNNEL_MODE must be off or quick" ;;
esac

if [ ! -x "$APP_EXEC" ]; then
	./scripts/package-app.sh >/dev/null
fi
if [ "$APP_LAUNCH_TUNNEL_MODE" = "quick" ]; then
	go build -o "$DIAGNOSE_BIN" ./cmd/transwarp-diagnose
	go build -o "$DISPATCH_BIN" ./cmd/transwarp-dispatch
fi

TRANSWARP_TOKEN="$TOKEN" go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$PORT" \
	-machine-id "$MACHINE_ID" \
	-machine-name "Transwarp Launch Smoke" \
	-workspace-root "" \
	-heartbeat-seconds 30 \
	-tunnel-mode "$APP_LAUNCH_TUNNEL_MODE" \
	-cloudflared-path "@bundle/cloudflared" \
	-job-id xcode-version \
	-job-label "Xcode Version" \
	-job-working-directory "$TMPDIR" \
	-job-command /usr/bin/xcodebuild \
	-job-arg=-version \
	-job-timeout-seconds 300

TRANSWARP_CONFIG_PATH="$CONFIG" TRANSWARP_START_RUNNER_ON_LAUNCH=1 "$APP_EXEC" >"$APP_LOG" 2>"$APP_ERR" &
APP_PID=$!

for _ in $(seq 1 100); do
	if [ -f "$CONFIG" ] && grep -q '"shared_token" : "keychain:' "$CONFIG"; then
		break
	fi
	sleep 0.1
done

if [ ! -f "$CONFIG" ]; then
	echo "app did not create agent config; stderr follows" >&2
	sed -n '1,120p' "$APP_ERR" >&2 || true
	fail "app did not create agent config"
fi

if ! grep -q '"shared_token" : "keychain:' "$CONFIG"; then
	echo "app starter config did not store runner token as a Keychain reference" >&2
	sed -n '1,120p' "$CONFIG" >&2
	sed -n '1,120p' "$APP_ERR" >&2 || true
	fail "app starter config did not store runner token as a Keychain reference"
fi

for _ in $(seq 1 100); do
	if curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/status" >"$STATUS_JSON" 2>/dev/null; then
		break
	fi
	sleep 0.1
done
if ! curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/status" >"$STATUS_JSON"; then
	echo "app did not start a runner that accepted the pre-migration token" >&2
	sed -n '1,120p' "$CONFIG" >&2
	sed -n '1,120p' "$APP_LOG" >&2 || true
	sed -n '1,120p' "$APP_ERR" >&2 || true
	fail "app did not start a runner that accepted the pre-migration token"
fi
grep -q '"command" : "\\/usr\\/bin\\/xcodebuild"' "$CONFIG"
[ "$(json_string_field "$CONFIG" tunnel.mode)" = "$APP_LAUNCH_TUNNEL_MODE" ] || fail "app config did not preserve tunnel mode $APP_LAUNCH_TUNNEL_MODE"
grep -q "\"machine_id\":\"$MACHINE_ID\"" "$STATUS_JSON"
grep -q '"xcode-version"' "$STATUS_JSON"

RUNNER_URL="http://127.0.0.1:$PORT"
PUBLIC_URL=""
TUNNEL_READY=false
PUBLIC_STATUS_AUTHENTICATED=false
if [ "$APP_LAUNCH_TUNNEL_MODE" = "quick" ]; then
	for _ in $(seq 1 "$APP_LAUNCH_PUBLIC_ATTEMPTS"); do
		if curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/status" >"$STATUS_JSON" 2>/dev/null; then
			PUBLIC_URL="$(json_string_field "$STATUS_JSON" public_url)"
			TUNNEL_READY="$(json_bool_field "$STATUS_JSON" tunnel.ready)"
			if [ "$TUNNEL_READY" = "true" ] && printf "%s" "$PUBLIC_URL" | grep -q 'trycloudflare\.com'; then
				break
			fi
		fi
		sleep 0.5
	done
	PUBLIC_URL="$(json_string_field "$STATUS_JSON" public_url)"
	TUNNEL_READY="$(json_bool_field "$STATUS_JSON" tunnel.ready)"
	if [ "$TUNNEL_READY" != "true" ] || ! printf "%s" "$PUBLIC_URL" | grep -q 'trycloudflare\.com'; then
		echo "app-spawned runner did not open a ready quick tunnel" >&2
		sed -n '1,120p' "$STATUS_JSON" >&2 || true
		sed -n '1,160p' "$APP_LOG" >&2 || true
		fail "quick tunnel did not become ready"
	fi
		for _ in $(seq 1 "$APP_LAUNCH_PUBLIC_ATTEMPTS"); do
			if "$DIAGNOSE_BIN" -url "$PUBLIC_URL" -token "$TOKEN" -job xcode-version -timeout 5s >"$PUBLIC_DIAGNOSE_LOG" 2>&1; then
				PUBLIC_STATUS_AUTHENTICATED=true
				break
			fi
			sleep 0.5
		done
	if [ "$PUBLIC_STATUS_AUTHENTICATED" != "true" ]; then
		echo "app-spawned quick tunnel did not expose authenticated status" >&2
		sed -n '1,120p' "$STATUS_JSON" >&2 || true
		sed -n '1,120p' "$PUBLIC_DIAGNOSE_LOG" >&2 || true
		sed -n '1,160p' "$APP_LOG" >&2 || true
		fail "public quick tunnel status was not reachable"
	fi
	grep -q "diagnosis passed" "$PUBLIC_DIAGNOSE_LOG"
	grep -q "$MACHINE_ID" "$PUBLIC_DIAGNOSE_LOG"
fi

if [ "$APP_LAUNCH_TUNNEL_MODE" = "quick" ]; then
	printf "public url: %s\n" "$PUBLIC_URL" >"$PUBLIC_DISPATCH_LOG"
	"$DISPATCH_BIN" \
		-url "$PUBLIC_URL" \
		-token "$TOKEN" \
		-job xcode-version \
		-request-id "$REQUEST_ID" >>"$PUBLIC_DISPATCH_LOG" 2>&1
	if ! grep -q "Xcode " "$PUBLIC_DISPATCH_LOG"; then
		echo "public dispatch log did not include Xcode output" >&2
		sed -n '1,200p' "$PUBLIC_DISPATCH_LOG" >&2 || true
		fail "public quick tunnel dispatch did not stream build output"
	fi
	if ! grep -q "\[build\] passed" "$PUBLIC_DISPATCH_LOG"; then
		echo "public dispatch log did not include passed build marker" >&2
		sed -n '1,200p' "$PUBLIC_DISPATCH_LOG" >&2 || true
		fail "public quick tunnel dispatch did not pass"
	fi
	BUILD_ID="$(sed -n 's/^\[result\] build_id //p' "$PUBLIC_DISPATCH_LOG" | tail -n 1)"
else
	curl -fsS \
		-X POST \
		-H "Authorization: Bearer $TOKEN" \
		-H "Content-Type: application/json" \
		-d "{\"job_id\":\"xcode-version\",\"request_id\":\"$REQUEST_ID\"}" \
		"$RUNNER_URL/v1/builds" >"$START_JSON"

	BUILD_ID="$(json_string_field "$START_JSON" build_id)"
fi

if [ -z "$BUILD_ID" ]; then
	echo "app-spawned runner did not return a build_id" >&2
	sed -n '1,120p' "$START_JSON" >&2
	sed -n '1,160p' "$PUBLIC_DISPATCH_LOG" >&2 || true
	fail "app-spawned runner did not return a build_id"
fi
require_stable_identifier "$BUILD_ID" build_id 128

curl -fsS \
	-H "Authorization: Bearer $TOKEN" \
	"$RUNNER_URL/v1/builds/$BUILD_ID/logs?after=0&follow=true" >"$BUILD_LOG"

if ! grep -q '"message":"passed"' "$BUILD_LOG"; then
	echo "local build log did not include passed event" >&2
	sed -n '1,160p' "$BUILD_LOG" >&2 || true
	fail "build log did not include passed event"
fi
if ! grep -q '"message":"Xcode ' "$BUILD_LOG"; then
	echo "local build log did not include Xcode output" >&2
	sed -n '1,160p' "$BUILD_LOG" >&2 || true
	fail "build log did not include Xcode output"
fi

curl -fsS -H "Authorization: Bearer $TOKEN" "$RUNNER_URL/v1/builds/$BUILD_ID" >"$BUILD_STATUS_JSON"
if ! grep -q "\"request_id\":\"$REQUEST_ID\"" "$BUILD_STATUS_JSON"; then
	echo "build status did not include request_id $REQUEST_ID" >&2
	sed -n '1,120p' "$BUILD_STATUS_JSON" >&2 || true
	fail "build status did not include request ID"
fi
if ! grep -q '"status":"passed"' "$BUILD_STATUS_JSON"; then
	echo "build status did not report passed" >&2
	sed -n '1,120p' "$BUILD_STATUS_JSON" >&2 || true
	fail "build status was not passed"
fi

curl -fsS -H "Authorization: Bearer $TOKEN" "$RUNNER_URL/status" >"$STATUS_JSON"
if ! grep -q "\"request_id\":\"$REQUEST_ID\"" "$STATUS_JSON"; then
	echo "runner status did not include request_id $REQUEST_ID" >&2
	sed -n '1,160p' "$STATUS_JSON" >&2 || true
	fail "runner status did not include request ID"
fi
if ! grep -q '"status":"passed"' "$STATUS_JSON"; then
	echo "runner status did not include passed recent build" >&2
	sed -n '1,160p' "$STATUS_JSON" >&2 || true
	fail "runner status did not include passed recent build"
fi

go run ./cmd/transwarp-audit \
	-app "$APP_DIR" \
	-write-app-launch-evidence "$APP_LAUNCH_RECEIPT" \
	-app-launch-tunnel-mode "$APP_LAUNCH_TUNNEL_MODE" \
	-app-launch-public-url "$PUBLIC_URL" \
	-app-launch-machine-id "$MACHINE_ID" \
	-app-launch-job-id xcode-version \
	-app-launch-request-id "$REQUEST_ID" \
	-app-launch-build-id "$BUILD_ID" \
	-app-launch-tunnel-ready="$TUNNEL_READY" \
	-app-launch-public-status-authenticated="$PUBLIC_STATUS_AUTHENTICATED" \
	-app-launch-build-log "$BUILD_LOG" \
	-app-launch-status-json "$STATUS_JSON" \
	-app-launch-build-status-json "$BUILD_STATUS_JSON" \
	-app-launch-public-diagnose-log "$PUBLIC_DIAGNOSE_LOG" \
	-app-launch-public-dispatch-log "$PUBLIC_DISPATCH_LOG" \
	-app-launch-app-log "$APP_LOG" \
	-app-launch-app-stderr "$APP_ERR"
go run ./cmd/transwarp-audit \
	-evidence-only \
	-app "$APP_DIR" \
	-app-launch-evidence "$APP_LAUNCH_RECEIPT" \
	-summary >/dev/null
if [ -n "$APP_LAUNCH_EVIDENCE" ]; then
	printf "app launch evidence: %s\n" "$APP_LAUNCH_EVIDENCE"
fi

printf "app launch smoke passed: %s\n" "$CONFIG"
