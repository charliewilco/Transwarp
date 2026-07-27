#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18198}"
RUNNER_READY_ATTEMPTS="${TRANSWARP_SMOKE_RUNNER_READY_ATTEMPTS:-300}"
PUBLIC_HEALTH_ATTEMPTS="${TRANSWARP_SMOKE_PUBLIC_HEALTH_ATTEMPTS:-150}"
CLOUDFLARED_PATH="${CLOUDFLARED_PATH:-$(command -v cloudflared || true)}"
QUICK_TUNNEL_EVIDENCE="${TRANSWARP_QUICK_TUNNEL_EVIDENCE:-}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-cloudflare-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
RUNNER_LOG="$TMPDIR/runner.log"
PUBLIC_HEALTH_LOG="$TMPDIR/public-health.log"
DISPATCH_LOG="$TMPDIR/dispatch.log"
DIAGNOSE_BIN="$TMPDIR/transwarp-diagnose"
DISPATCH_BIN="$TMPDIR/transwarp-dispatch"
RUNNER_BIN="$TMPDIR/transwarp-runner"
AUDIT_BIN="$TMPDIR/transwarp-audit"
RUNNER_PID=""

cleanup() {
	if [ -n "$RUNNER_PID" ]; then
		kill "$RUNNER_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

if [ -z "$CLOUDFLARED_PATH" ] || [ ! -x "$CLOUDFLARED_PATH" ]; then
	echo "cloudflared is required for this smoke; install it or set CLOUDFLARED_PATH" >&2
	exit 2
fi

cd "$ROOT"

go build -o "$DISPATCH_BIN" ./cmd/transwarp-dispatch
go build -o "$DIAGNOSE_BIN" ./cmd/transwarp-diagnose
go build -o "$RUNNER_BIN" ./cmd/transwarp-runner
go build -o "$AUDIT_BIN" ./cmd/transwarp-audit

public_health() {
	"$DIAGNOSE_BIN" \
		-url "$PUBLIC_URL" \
		-token "runner-token" \
		-job echo \
		-timeout 5s >"$PUBLIC_HEALTH_LOG" 2>&1
}

TRANSWARP_TOKEN=runner-token go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id quick-tunnel-smoke \
	-machine-name "Quick Tunnel Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-heartbeat-seconds 30 \
	-tunnel-mode quick \
	-cloudflared-path "$CLOUDFLARED_PATH" \
	-job-id echo \
	-job-label "Echo Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello from cloudflare tunnel" \
	-job-timeout-seconds 10

"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
RUNNER_PID=$!

PUBLIC_URL=""
for _ in $(seq 1 "$RUNNER_READY_ATTEMPTS"); do
	STATUS="$(curl -fsS -H "Authorization: Bearer runner-token" "http://127.0.0.1:$RUNNER_PORT/status" 2>/dev/null || true)"
	PUBLIC_URL="$(printf "%s" "$STATUS" | sed -n 's/.*"public_url":"\([^"]*\)".*/\1/p' | head -n 1)"
	if [ -n "$PUBLIC_URL" ]; then
		break
	fi
	sleep 0.2
done

if [ -z "$PUBLIC_URL" ]; then
	echo "quick tunnel did not publish a public URL; runner log: $RUNNER_LOG" >&2
	printf "last status: %s\n" "$STATUS" >&2
	tail -n 80 "$RUNNER_LOG" >&2 || true
	exit 1
fi

printf "public url: %s\n" "$PUBLIC_URL"

HEALTH_READY=0
for _ in $(seq 1 "$PUBLIC_HEALTH_ATTEMPTS"); do
	if public_health; then
		HEALTH_READY=1
		break
	fi
	sleep 0.5
done
if [ "$HEALTH_READY" != "1" ]; then
	echo "quick tunnel public health endpoint did not become reachable; runner log: $RUNNER_LOG" >&2
	printf "last status: %s\n" "$STATUS" >&2
	echo "last public health attempt:" >&2
	cat "$PUBLIC_HEALTH_LOG" >&2 || true
	tail -n 80 "$RUNNER_LOG" >&2 || true
	exit 1
fi

"$DISPATCH_BIN" \
	-url "$PUBLIC_URL" \
	-token "runner-token" \
	-job echo \
	-request-id quick-tunnel-smoke >"$DISPATCH_LOG" 2>&1
cat "$DISPATCH_LOG"

STATUS="$(curl -fsS -H "Authorization: Bearer runner-token" "http://127.0.0.1:$RUNNER_PORT/status")"
printf "%s\n" "$STATUS"
printf "%s" "$STATUS" | grep -q '"status":"passed"'

if [ -n "$QUICK_TUNNEL_EVIDENCE" ]; then
	"$AUDIT_BIN" \
		-write-quick-tunnel-evidence "$QUICK_TUNNEL_EVIDENCE" \
		-quick-tunnel-public-url "$PUBLIC_URL" \
		-quick-tunnel-diagnose-log "$PUBLIC_HEALTH_LOG" \
		-quick-tunnel-dispatch-log "$DISPATCH_LOG"
	printf "quick tunnel diagnostic evidence: %s\n" "$QUICK_TUNNEL_EVIDENCE"
fi
printf "logs: %s\n" "$TMPDIR"
