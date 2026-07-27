#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18199}"
RUNNER_READY_ATTEMPTS="${TRANSWARP_SMOKE_RUNNER_READY_ATTEMPTS:-300}"
PUBLIC_HEALTH_ATTEMPTS="${TRANSWARP_SMOKE_PUBLIC_HEALTH_ATTEMPTS:-150}"
CLOUDFLARED_PATH="${CLOUDFLARED_PATH:-$(command -v cloudflared || true)}"
TUNNEL_TOKEN="${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-${TRANSWARP_TUNNEL_TOKEN:-}}"
PUBLIC_URL="${TRANSWARP_PUBLIC_URL:-}"
ACCESS_CLIENT_ID="${TRANSWARP_ACCESS_CLIENT_ID:-}"
ACCESS_CLIENT_SECRET="${TRANSWARP_ACCESS_CLIENT_SECRET:-}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-named-tunnel-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
RUNNER_LOG="$TMPDIR/runner.log"
PUBLIC_HEALTH_LOG="$TMPDIR/public-health.log"
DIAGNOSE_BIN="$TMPDIR/transwarp-diagnose"
DISPATCH_BIN="$TMPDIR/transwarp-dispatch"
RUNNER_BIN="$TMPDIR/transwarp-runner"
RUNNER_PID=""

cleanup() {
	if [ -n "$RUNNER_PID" ]; then
		kill "$RUNNER_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
	if [ -f "$CONFIG" ]; then
		: >"$CONFIG"
	fi
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

if [ -z "$CLOUDFLARED_PATH" ] || [ ! -x "$CLOUDFLARED_PATH" ]; then
	echo "cloudflared is required for this smoke; install it or set CLOUDFLARED_PATH" >&2
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

public_health() {
	"$DIAGNOSE_BIN" \
		-url "$PUBLIC_URL" \
		-token "runner-token" \
		-access-client-id "$ACCESS_CLIENT_ID" \
		-access-client-secret "$ACCESS_CLIENT_SECRET" \
		-job echo \
		-timeout 10s >"$PUBLIC_HEALTH_LOG" 2>&1
}

cd "$ROOT"

go build -o "$DISPATCH_BIN" ./cmd/transwarp-dispatch
go build -o "$DIAGNOSE_BIN" ./cmd/transwarp-diagnose
go build -o "$RUNNER_BIN" ./cmd/transwarp-runner

TRANSWARP_TOKEN=runner-token \
	TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN="$TUNNEL_TOKEN" \
	go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id named-tunnel-smoke \
	-machine-name "Named Tunnel Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-heartbeat-seconds 30 \
	-tunnel-mode named \
	-cloudflared-path "$CLOUDFLARED_PATH" \
	-public-url "$PUBLIC_URL" \
	-job-id echo \
	-job-label "Echo Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello from named cloudflare tunnel" \
	-job-timeout-seconds 10

"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
RUNNER_PID=$!

for _ in $(seq 1 "$RUNNER_READY_ATTEMPTS"); do
	STATUS="$(curl -fsS -H "Authorization: Bearer runner-token" "http://127.0.0.1:$RUNNER_PORT/status" 2>/dev/null || true)"
	if printf "%s" "$STATUS" | grep -q '"ready":true'; then
		break
	fi
	sleep 0.2
done

STATUS="$(curl -fsS -H "Authorization: Bearer runner-token" "http://127.0.0.1:$RUNNER_PORT/status")"
printf "%s\n" "$STATUS"
printf "%s" "$STATUS" | grep -q '"ready":true'

HEALTH_READY=0
for _ in $(seq 1 "$PUBLIC_HEALTH_ATTEMPTS"); do
	if public_health; then
		HEALTH_READY=1
		break
	fi
	sleep 0.5
done
if [ "$HEALTH_READY" != "1" ]; then
	echo "named tunnel public health endpoint did not become reachable; runner log: $RUNNER_LOG" >&2
	echo "last public health attempt:" >&2
	cat "$PUBLIC_HEALTH_LOG" >&2 || true
	tail -n 80 "$RUNNER_LOG" >&2 || true
	exit 1
fi

"$DISPATCH_BIN" \
	-url "$PUBLIC_URL" \
	-token "runner-token" \
	-access-client-id "$ACCESS_CLIENT_ID" \
	-access-client-secret "$ACCESS_CLIENT_SECRET" \
	-job echo \
	-request-id named-tunnel-smoke

STATUS="$(curl -fsS -H "Authorization: Bearer runner-token" "http://127.0.0.1:$RUNNER_PORT/status")"
printf "%s\n" "$STATUS"
printf "%s" "$STATUS" | grep -q '"status":"passed"'
printf "logs: %s\n" "$TMPDIR"
