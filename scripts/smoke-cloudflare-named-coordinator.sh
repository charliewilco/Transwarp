#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COORDINATOR_PORT="${TRANSWARP_SMOKE_COORDINATOR_PORT:-18488}"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18498}"
COORDINATOR_READY_ATTEMPTS="${TRANSWARP_SMOKE_COORDINATOR_READY_ATTEMPTS:-100}"
TARGET_READY_ATTEMPTS="${TRANSWARP_SMOKE_TARGET_READY_ATTEMPTS:-300}"
CLOUDFLARED_PATH="${CLOUDFLARED_PATH:-$(command -v cloudflared || true)}"
TUNNEL_TOKEN="${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-${TRANSWARP_TUNNEL_TOKEN:-}}"
PUBLIC_URL="${TRANSWARP_PUBLIC_URL:-}"
ACCESS_CLIENT_ID="${TRANSWARP_ACCESS_CLIENT_ID:-}"
ACCESS_CLIENT_SECRET="${TRANSWARP_ACCESS_CLIENT_SECRET:-}"
NAMED_TUNNEL_EVIDENCE="${TRANSWARP_NAMED_TUNNEL_EVIDENCE:-}"
LAUNCH_MODE="${TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE:-app}"
APP_DIR="${TRANSWARP_NAMED_TUNNEL_APP_PATH:-$ROOT/.build/Transwarp.app}"
APP_EXEC="$APP_DIR/Contents/MacOS/Transwarp"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-named-tunnel-smoke-coordinator.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
STATE="$TMPDIR/coordinator-state.json"
COORDINATOR_LOG="$TMPDIR/coordinator.log"
RUNNER_LOG="$TMPDIR/runner.log"
APP_LOG="$TMPDIR/app.log"
APP_ERR="$TMPDIR/app.err"
DISPATCH_LOG="$TMPDIR/dispatch.log"
DIAGNOSE_LOG="$TMPDIR/diagnose.log"
RESULTS_JSON="$TMPDIR/results.json"
TARGETS_REGISTERED_JSON="$TMPDIR/targets-registered.json"
TARGETS_AFTER_DEREGISTER_JSON="$TMPDIR/targets-after-deregister.json"
COORDINATOR_BIN="$TMPDIR/transwarp-coordinator"
DIAGNOSE_BIN="$TMPDIR/transwarp-diagnose"
DISPATCH_BIN="$TMPDIR/transwarp-dispatch"
RUNNER_BIN="$TMPDIR/transwarp-runner"
COORDINATOR_PID=""
RUNNER_PID=""
REQUEST_ID="named-tunnel-smoke-coordinator-run"
MACHINE_ID="named-tunnel-smoke-coordinator"
JOB_ID="echo"

cleanup() {
	if [ -n "$RUNNER_PID" ]; then
		kill "$RUNNER_PID" 2>/dev/null || true
	fi
	if [ -n "$COORDINATOR_PID" ]; then
		kill "$COORDINATOR_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
	if [ -f "$CONFIG" ]; then
		: >"$CONFIG"
	fi
	security delete-generic-password -s co.charliewil.transwarp -a "$MACHINE_ID/shared_token" >/dev/null 2>&1 || true
	security delete-generic-password -s co.charliewil.transwarp -a "$MACHINE_ID/registration_token" >/dev/null 2>&1 || true
	security delete-generic-password -s co.charliewil.transwarp -a "$MACHINE_ID/tunnel_token" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

validate_public_url() {
	url="$1"
	case "$url" in
		https://*) ;;
		*)
			echo "TRANSWARP_PUBLIC_URL must be an HTTPS base URL like https://transwarp.example.com" >&2
			exit 2
			;;
	esac

	rest="${url#https://}"
	case "$rest" in
		""|"/"*|*"?"*|*"#"*)
			echo "TRANSWARP_PUBLIC_URL must be an HTTPS base URL like https://transwarp.example.com" >&2
			exit 2
			;;
	esac

	host="$rest"
	path=""
	case "$rest" in
		*/*)
			host="${rest%%/*}"
			path="/${rest#*/}"
			;;
	esac
	case "$host" in
		""|*"@"*)
			echo "TRANSWARP_PUBLIC_URL must not include credentials" >&2
			exit 2
			;;
	esac
	if [ -n "$path" ] && [ "$path" != "/" ]; then
		echo "TRANSWARP_PUBLIC_URL must be an HTTPS base URL like https://transwarp.example.com" >&2
		exit 2
	fi
}

case "$LAUNCH_MODE" in
	app|runner) ;;
	*)
		echo "TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE must be app or runner" >&2
		exit 2
		;;
esac
if [ "$LAUNCH_MODE" = "runner" ] && { [ -z "$CLOUDFLARED_PATH" ] || [ ! -x "$CLOUDFLARED_PATH" ]; }; then
	echo "cloudflared is required for runner launch mode; install it or set CLOUDFLARED_PATH" >&2
	exit 2
fi
if [ -z "$TUNNEL_TOKEN" ]; then
	echo "TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN or TRANSWARP_TUNNEL_TOKEN is required" >&2
	exit 2
fi
if [ -z "$PUBLIC_URL" ]; then
	echo "TRANSWARP_PUBLIC_URL is required, for example https://transwarp.example.com" >&2
	exit 2
fi
validate_public_url "$PUBLIC_URL"
if { [ -n "$ACCESS_CLIENT_ID" ] && [ -z "$ACCESS_CLIENT_SECRET" ]; } || { [ -z "$ACCESS_CLIENT_ID" ] && [ -n "$ACCESS_CLIENT_SECRET" ]; }; then
	echo "TRANSWARP_ACCESS_CLIENT_ID and TRANSWARP_ACCESS_CLIENT_SECRET must be provided together" >&2
	exit 2
fi

wait_for_url() {
	url="$1"
	attempts="$2"
	for _ in $(seq 1 "$attempts"); do
		if curl -fsS "$url" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done
	curl -fsS "$url" >/dev/null
}

cd "$ROOT"

go build -o "$COORDINATOR_BIN" ./cmd/transwarp-coordinator
go build -o "$DIAGNOSE_BIN" ./cmd/transwarp-diagnose
go build -o "$DISPATCH_BIN" ./cmd/transwarp-dispatch
if [ "$LAUNCH_MODE" = "runner" ]; then
	go build -o "$RUNNER_BIN" ./cmd/transwarp-runner
fi
if [ "$LAUNCH_MODE" = "app" ] && [ ! -x "$APP_EXEC" ]; then
	./scripts/package-app.sh "$APP_DIR" >/dev/null
fi

TRANSWARP_TOKEN="runner-token" "$COORDINATOR_BIN" \
	-listen "127.0.0.1:$COORDINATOR_PORT" \
	-token "coord-token" \
	-target-token "target-token" \
	-access-client-id "$ACCESS_CLIENT_ID" \
	-access-client-secret "$ACCESS_CLIENT_SECRET" \
	-public-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-state-path "$STATE" \
	-result-wait-timeout 15s >"$COORDINATOR_LOG" 2>&1 &
COORDINATOR_PID=$!

wait_for_url "http://127.0.0.1:$COORDINATOR_PORT/health" "$COORDINATOR_READY_ATTEMPTS"

CONFIG_CLOUDFLARED_PATH="$CLOUDFLARED_PATH"
if [ "$LAUNCH_MODE" = "app" ]; then
	CONFIG_CLOUDFLARED_PATH="@bundle/cloudflared"
fi

TRANSWARP_TOKEN=runner-token \
	TRANSWARP_REGISTRATION_TOKEN=target-token \
	TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN="$TUNNEL_TOKEN" \
	go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id "$MACHINE_ID" \
	-machine-name "Named Tunnel Coordinator Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-ci-registration-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/register" \
	-ci-heartbeat-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/heartbeat" \
	-ci-deregistration-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/deregister" \
	-heartbeat-seconds 1 \
	-tunnel-mode named \
	-cloudflared-path "$CONFIG_CLOUDFLARED_PATH" \
	-public-url "$PUBLIC_URL" \
	-job-id "$JOB_ID" \
	-job-label "Echo Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello through named coordinator tunnel" \
	-job-timeout-seconds 10

if [ "$LAUNCH_MODE" = "app" ]; then
	TRANSWARP_CONFIG_PATH="$CONFIG" \
		TRANSWARP_START_RUNNER_ON_LAUNCH=1 \
		TRANSWARP_APP_LOG_EVENTS_TO_STDOUT=1 \
		"$APP_EXEC" >"$APP_LOG" 2>"$APP_ERR" &
	RUNNER_PID=$!
	RUNNER_LOG="$APP_LOG"
	for _ in $(seq 1 100); do
		if grep -q '"shared_token" : "keychain:' "$CONFIG" &&
			grep -q '"registration_token" : "keychain:' "$CONFIG" &&
			grep -q '"token" : "keychain:' "$CONFIG"; then
			break
		fi
		sleep 0.1
	done
	if ! grep -q '"shared_token" : "keychain:' "$CONFIG" ||
		! grep -q '"registration_token" : "keychain:' "$CONFIG" ||
		! grep -q '"token" : "keychain:' "$CONFIG"; then
		echo "app did not migrate named-tunnel smoke secrets to Keychain references" >&2
		sed -n '1,160p' "$CONFIG" >&2 || true
		sed -n '1,160p' "$APP_ERR" >&2 || true
		exit 1
	fi
else
	"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
	RUNNER_PID=$!
fi

TARGETS=""
for _ in $(seq 1 "$TARGET_READY_ATTEMPTS"); do
	TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets" 2>/dev/null || true)"
	if printf "%s" "$TARGETS" | grep -q "$MACHINE_ID" &&
		printf "%s" "$TARGETS" | grep -Fq "\"public_url\":\"$PUBLIC_URL\""; then
		break
	fi
	sleep 0.5
done

TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets")"
printf "%s\n" "$TARGETS" >"$TARGETS_REGISTERED_JSON"
printf "%s\n" "$TARGETS"
if ! printf "%s" "$TARGETS" | grep -q "$MACHINE_ID"; then
	echo "runner did not register with coordinator; runner log: $RUNNER_LOG" >&2
	tail -n 120 "$RUNNER_LOG" >&2 || true
	tail -n 120 "$APP_ERR" >&2 || true
	exit 1
fi
if ! printf "%s" "$TARGETS" | grep -Fq "\"public_url\":\"$PUBLIC_URL\""; then
	echo "registered target did not advertise expected public_url=$PUBLIC_URL; runner log: $RUNNER_LOG" >&2
	tail -n 120 "$RUNNER_LOG" >&2 || true
	tail -n 120 "$APP_ERR" >&2 || true
	exit 1
fi

"$DIAGNOSE_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-token "runner-token" \
	-access-client-id "$ACCESS_CLIENT_ID" \
	-access-client-secret "$ACCESS_CLIENT_SECRET" \
	-machine-id "$MACHINE_ID" \
	-allow-http \
	-job "$JOB_ID" >"$DIAGNOSE_LOG" 2>&1
cat "$DIAGNOSE_LOG"

"$DISPATCH_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-machine-id "$MACHINE_ID" \
	-job "$JOB_ID" \
	-require-public-url \
	-request-id "$REQUEST_ID" >"$DISPATCH_LOG" 2>&1
cat "$DISPATCH_LOG"
grep -q "hello through named coordinator tunnel" "$DISPATCH_LOG"
grep -q "\[result\] recorded passed" "$DISPATCH_LOG"

RESULTS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/results")"
printf "%s\n" "$RESULTS"
printf "%s\n" "$RESULTS" >"$RESULTS_JSON"
printf "%s" "$RESULTS" | grep -q "$REQUEST_ID"
printf "%s" "$RESULTS" | grep -q '"status":"passed"'

kill "$RUNNER_PID" 2>/dev/null || true
wait "$RUNNER_PID" 2>/dev/null || true
RUNNER_PID=""

for _ in $(seq 1 80); do
	TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets" 2>/dev/null || true)"
	if ! printf "%s" "$TARGETS" | grep -q "$MACHINE_ID"; then
		break
	fi
	sleep 0.25
done

TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets")"
printf "%s\n" "$TARGETS" >"$TARGETS_AFTER_DEREGISTER_JSON"
printf "%s\n" "$TARGETS"
if printf "%s" "$TARGETS" | grep -q "$MACHINE_ID"; then
	echo "runner did not deregister from coordinator; runner log: $RUNNER_LOG" >&2
	tail -n 120 "$RUNNER_LOG" >&2 || true
	exit 1
fi

EVIDENCE_OUTPUT="${NAMED_TUNNEL_EVIDENCE:-$TMPDIR/named-tunnel-evidence.json}"
if [ "$LAUNCH_MODE" = "app" ] || [ -n "$NAMED_TUNNEL_EVIDENCE" ]; then
	go run ./cmd/transwarp-audit \
		-app "$APP_DIR" \
		-write-named-tunnel-evidence "$EVIDENCE_OUTPUT" \
		-named-tunnel-launch-mode "$LAUNCH_MODE" \
		-named-tunnel-public-url "$PUBLIC_URL" \
		-named-tunnel-access-protected="$([ -n "$ACCESS_CLIENT_ID" ] && printf true || printf false)" \
		-named-tunnel-machine-id "$MACHINE_ID" \
		-named-tunnel-job-id "$JOB_ID" \
		-named-tunnel-request-id "$REQUEST_ID" \
		-named-tunnel-diagnose-log "$DIAGNOSE_LOG" \
		-named-tunnel-dispatch-log "$DISPATCH_LOG" \
		-named-tunnel-runner-log "$RUNNER_LOG" \
		-named-tunnel-app-log "$APP_LOG" \
		-named-tunnel-app-stderr "$APP_ERR" \
		-named-tunnel-targets-registered "$TARGETS_REGISTERED_JSON" \
		-named-tunnel-targets-after-deregister "$TARGETS_AFTER_DEREGISTER_JSON" \
		-named-tunnel-results "$RESULTS_JSON"
fi
if [ -n "$NAMED_TUNNEL_EVIDENCE" ]; then
	printf "named tunnel evidence: %s\n" "$NAMED_TUNNEL_EVIDENCE"
fi

printf "public url: %s\n" "$PUBLIC_URL"
printf "logs: %s\n" "$TMPDIR"
