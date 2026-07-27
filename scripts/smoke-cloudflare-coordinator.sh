#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COORDINATOR_PORT="${TRANSWARP_SMOKE_COORDINATOR_PORT:-18388}"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18398}"
COORDINATOR_READY_ATTEMPTS="${TRANSWARP_SMOKE_COORDINATOR_READY_ATTEMPTS:-100}"
TARGET_READY_ATTEMPTS="${TRANSWARP_SMOKE_TARGET_READY_ATTEMPTS:-240}"
PUBLIC_HEALTH_ATTEMPTS="${TRANSWARP_SMOKE_PUBLIC_HEALTH_ATTEMPTS:-300}"
CLOUDFLARED_PATH="${CLOUDFLARED_PATH:-$(command -v cloudflared || true)}"
QUICK_TUNNEL_EVIDENCE="${TRANSWARP_QUICK_TUNNEL_EVIDENCE:-}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-cloudflare-coordinator-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
STATE="$TMPDIR/coordinator-state.json"
COORDINATOR_LOG="$TMPDIR/coordinator.log"
RUNNER_LOG="$TMPDIR/runner.log"
DISPATCH_LOG="$TMPDIR/dispatch.log"
DIAGNOSE_LOG="$TMPDIR/diagnose.log"
PUBLIC_HEALTH_LOG="$TMPDIR/public-health.log"
TARGETS_BEFORE_DISPATCH_LOG="$TMPDIR/targets-before-dispatch.json"
TARGETS_AFTER_DEREGISTER_LOG="$TMPDIR/targets-after-deregister.json"
RESULTS_LOG="$TMPDIR/results.json"
COORDINATOR_BIN="$TMPDIR/transwarp-coordinator"
DIAGNOSE_BIN="$TMPDIR/transwarp-diagnose"
DISPATCH_BIN="$TMPDIR/transwarp-dispatch"
RUNNER_BIN="$TMPDIR/transwarp-runner"
AUDIT_BIN="$TMPDIR/transwarp-audit"
COORDINATOR_PID=""
RUNNER_PID=""
REQUEST_ID="quick-tunnel-coordinator-run"
MACHINE_ID="quick-tunnel-coordinator-smoke"

cleanup() {
	if [ -n "$RUNNER_PID" ]; then
		kill "$RUNNER_PID" 2>/dev/null || true
	fi
	if [ -n "$COORDINATOR_PID" ]; then
		kill "$COORDINATOR_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

if [ -z "$CLOUDFLARED_PATH" ] || [ ! -x "$CLOUDFLARED_PATH" ]; then
	echo "cloudflared is required for this smoke; install it or set CLOUDFLARED_PATH" >&2
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

public_health() {
	"$DIAGNOSE_BIN" \
		-url "$PUBLIC_URL" \
		-token "runner-token" \
		-job echo \
		-timeout 5s >"$PUBLIC_HEALTH_LOG" 2>&1
}

cd "$ROOT"

go build -o "$COORDINATOR_BIN" ./cmd/transwarp-coordinator
go build -o "$DIAGNOSE_BIN" ./cmd/transwarp-diagnose
go build -o "$DISPATCH_BIN" ./cmd/transwarp-dispatch
go build -o "$RUNNER_BIN" ./cmd/transwarp-runner
go build -o "$AUDIT_BIN" ./cmd/transwarp-audit

TRANSWARP_TOKEN="runner-token" "$COORDINATOR_BIN" \
	-listen "127.0.0.1:$COORDINATOR_PORT" \
	-token "coord-token" \
	-target-token "target-token" \
	-public-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-state-path "$STATE" \
	-result-wait-timeout 10s >"$COORDINATOR_LOG" 2>&1 &
COORDINATOR_PID=$!

wait_for_url "http://127.0.0.1:$COORDINATOR_PORT/health" "$COORDINATOR_READY_ATTEMPTS"

TRANSWARP_TOKEN=runner-token \
	TRANSWARP_REGISTRATION_TOKEN=target-token \
	go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id "$MACHINE_ID" \
	-machine-name "Quick Tunnel Coordinator Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-ci-registration-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/register" \
	-ci-heartbeat-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/heartbeat" \
	-ci-deregistration-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/deregister" \
	-heartbeat-seconds 1 \
	-tunnel-mode quick \
	-cloudflared-path "$CLOUDFLARED_PATH" \
	-job-id echo \
	-job-label "Echo Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello through coordinator and cloudflare tunnel" \
	-job-timeout-seconds 10

"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
RUNNER_PID=$!

TARGETS=""
PUBLIC_URL=""
for _ in $(seq 1 "$TARGET_READY_ATTEMPTS"); do
	TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets" 2>/dev/null || true)"
	if printf "%s" "$TARGETS" | grep -q "$MACHINE_ID"; then
		PUBLIC_URL="$(printf "%s" "$TARGETS" | sed -n 's/.*"public_url":"\([^"]*\)".*/\1/p' | head -n 1)"
		if [ -n "$PUBLIC_URL" ]; then
			break
		fi
	fi
	sleep 0.5
done

TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets")"
PUBLIC_URL="$(printf "%s" "$TARGETS" | sed -n 's/.*"public_url":"\([^"]*\)".*/\1/p' | head -n 1)"
printf "%s\n" "$TARGETS"
printf "%s\n" "$TARGETS" > "$TARGETS_BEFORE_DISPATCH_LOG"
if ! printf "%s" "$TARGETS" | grep -q "$MACHINE_ID"; then
	echo "runner did not register with coordinator; runner log: $RUNNER_LOG" >&2
	tail -n 120 "$RUNNER_LOG" >&2 || true
	exit 1
fi
if [ -z "$PUBLIC_URL" ] || ! printf "%s" "$PUBLIC_URL" | grep -q 'trycloudflare\.com'; then
	echo "registered target did not include a quick tunnel URL; public_url=$PUBLIC_URL runner log: $RUNNER_LOG" >&2
	tail -n 120 "$RUNNER_LOG" >&2 || true
	exit 1
fi

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
	echo "last public health attempt:" >&2
	cat "$PUBLIC_HEALTH_LOG" >&2 || true
	tail -n 120 "$RUNNER_LOG" >&2 || true
	exit 1
fi

DIAGNOSE_READY=0
for _ in $(seq 1 30); do
	if "$DIAGNOSE_BIN" \
		-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
		-coordinator-token "coord-token" \
		-token "runner-token" \
		-machine-id "$MACHINE_ID" \
		-allow-http \
		-job echo >"$DIAGNOSE_LOG" 2>&1; then
		DIAGNOSE_READY=1
		break
	fi
	sleep 0.5
done
cat "$DIAGNOSE_LOG"
if [ "$DIAGNOSE_READY" != "1" ]; then
	exit 1
fi

"$DISPATCH_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-machine-id "$MACHINE_ID" \
	-job echo \
	-request-id "$REQUEST_ID" >"$DISPATCH_LOG" 2>&1
cat "$DISPATCH_LOG"
grep -q "hello through coordinator and cloudflare tunnel" "$DISPATCH_LOG"
grep -q "\[result\] recorded passed" "$DISPATCH_LOG"

RESULTS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/results")"
printf "%s\n" "$RESULTS"
printf "%s\n" "$RESULTS" > "$RESULTS_LOG"
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
printf "%s\n" "$TARGETS"
printf "%s\n" "$TARGETS" > "$TARGETS_AFTER_DEREGISTER_LOG"
if printf "%s" "$TARGETS" | grep -q "$MACHINE_ID"; then
	echo "runner did not deregister from coordinator; runner log: $RUNNER_LOG" >&2
	tail -n 120 "$RUNNER_LOG" >&2 || true
	exit 1
fi

QUICK_TUNNEL_RECEIPT="${QUICK_TUNNEL_EVIDENCE:-$TMPDIR/quick-tunnel-coordinator-evidence.json}"
"$AUDIT_BIN" \
	-write-quick-tunnel-evidence "$QUICK_TUNNEL_RECEIPT" \
	-quick-tunnel-public-url "$PUBLIC_URL" \
	-quick-tunnel-coordinator \
	-quick-tunnel-machine-id "$MACHINE_ID" \
	-quick-tunnel-job-id echo \
	-quick-tunnel-request-id "$REQUEST_ID" \
	-quick-tunnel-diagnose-log "$DIAGNOSE_LOG" \
	-quick-tunnel-dispatch-log "$DISPATCH_LOG" \
	-quick-tunnel-targets-before-dispatch "$TARGETS_BEFORE_DISPATCH_LOG" \
	-quick-tunnel-targets-after-deregister "$TARGETS_AFTER_DEREGISTER_LOG" \
	-quick-tunnel-results "$RESULTS_LOG"
"$AUDIT_BIN" \
	-check-receipt "$QUICK_TUNNEL_RECEIPT" \
	-check-receipt-kind transwarp-quick-tunnel-diagnostic
if [ -n "$QUICK_TUNNEL_EVIDENCE" ]; then
	printf "quick tunnel diagnostic evidence: %s\n" "$QUICK_TUNNEL_EVIDENCE"
fi

printf "public url: %s\n" "$PUBLIC_URL"
printf "logs: %s\n" "$TMPDIR"
