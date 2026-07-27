#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18187}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-diagnose-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
RUNNER_LOG="$TMPDIR/runner.log"
RUNNER_BIN="$TMPDIR/transwarp-runner"
RUNNER_PID=""

cleanup() {
	if [ -n "$RUNNER_PID" ]; then
		kill "$RUNNER_PID" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

cd "$ROOT"

TRANSWARP_TOKEN=runner-token go run ./cmd/transwarp-config \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id diagnose-smoke \
	-machine-name "Diagnose Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-heartbeat-seconds 30 \
	-tunnel-mode off \
	-job-id echo \
	-job-label "Echo Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello from diagnose smoke" \
	-job-timeout-seconds 10

go build -o "$RUNNER_BIN" ./cmd/transwarp-runner
"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
RUNNER_PID=$!

for _ in $(seq 1 100); do
	if curl -fsS "http://127.0.0.1:$RUNNER_PORT/health" >/dev/null 2>&1; then
		break
	fi
	sleep 0.1
done

go run ./cmd/transwarp-diagnose \
	-url "http://127.0.0.1:$RUNNER_PORT" \
	-token "runner-token" \
	-job echo \
	-allow-http

printf "logs: %s\n" "$TMPDIR"
