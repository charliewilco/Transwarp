#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18189}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-direct-build-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
RUNNER_LOG="$TMPDIR/runner.log"
DISPATCH_LOG="$TMPDIR/dispatch.log"
RUNNER_BIN="$TMPDIR/transwarp-runner"
RUNNER_PID=""

cleanup() {
	if [ -n "$RUNNER_PID" ]; then
		kill "$RUNNER_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

wait_for_url() {
	url="$1"
	for _ in $(seq 1 100); do
		if curl -fsS "$url" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done
	curl -fsS "$url" >/dev/null
}

cd "$ROOT"

TRANSWARP_TOKEN=runner-token go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id direct-build-smoke \
	-machine-name "Direct Build Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-heartbeat-seconds 30 \
	-tunnel-mode off \
	-job-id echo \
	-job-label "Echo Direct Build Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello direct build smoke" \
	-job-timeout-seconds 10
if [ "$(stat -f "%Lp" "$CONFIG")" != "600" ]; then
	echo "temporary runner config must be owner-readable only" >&2
	exit 1
fi

go build -o "$RUNNER_BIN" ./cmd/transwarp-runner
"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
RUNNER_PID=$!

wait_for_url "http://127.0.0.1:$RUNNER_PORT/health"

go run ./cmd/transwarp-dispatch \
	-url "http://127.0.0.1:$RUNNER_PORT" \
	-token "runner-token" \
	-job echo \
	-request-id direct-build-smoke-run >"$DISPATCH_LOG" 2>&1

grep -q "hello direct build smoke" "$DISPATCH_LOG"
grep -q "passed" "$DISPATCH_LOG"

STATUS="$(curl -fsS -H "Authorization: Bearer runner-token" "http://127.0.0.1:$RUNNER_PORT/status")"
printf "%s\n" "$STATUS"
printf "%s" "$STATUS" | grep -q '"request_id":"direct-build-smoke-run"'
printf "%s" "$STATUS" | grep -q '"status":"passed"'

printf "logs: %s\n" "$TMPDIR"
