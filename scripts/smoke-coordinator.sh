#!/bin/sh
set -eu
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COORDINATOR_PORT="${TRANSWARP_SMOKE_COORDINATOR_PORT:-18288}"
RUNNER_PORT="${TRANSWARP_SMOKE_RUNNER_PORT:-18188}"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-smoke.XXXXXX")"
CONFIG="$TMPDIR/agent.json"
SLOW_SCRIPT="$TMPDIR/restart-durability-smoke.sh"
CANCEL_SCRIPT="$TMPDIR/restart-cancel-smoke.sh"
STATE="$TMPDIR/coordinator-state.json"
COORDINATOR_LOG="$TMPDIR/coordinator.log"
COORDINATOR_RESTART_LOG="$TMPDIR/coordinator-restart.log"
RUNNER_LOG="$TMPDIR/runner.log"
RESTART_DISPATCH_LOG="$TMPDIR/restart-dispatch.log"
RECONNECT_DISPATCH_LOG="$TMPDIR/reconnect-dispatch.log"
CANCEL_DISPATCH_LOG="$TMPDIR/restart-cancel-dispatch.log"
CANCEL_COMMAND_LOG="$TMPDIR/restart-cancel-command.log"
COORDINATOR_BIN="$TMPDIR/transwarp-coordinator"
CONFIG_BIN="$TMPDIR/transwarp-config"
DIAGNOSE_BIN="$TMPDIR/transwarp-diagnose"
DISPATCH_BIN="$TMPDIR/transwarp-dispatch"
RUNNER_BIN="$TMPDIR/transwarp-runner"
COORDINATOR_PID=""
RUNNER_PID=""

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

fail_with_logs() {
	echo "$1" >&2
	for log in "$COORDINATOR_LOG" "$COORDINATOR_RESTART_LOG" "$RUNNER_LOG" "$RESTART_DISPATCH_LOG" "$RECONNECT_DISPATCH_LOG" "$CANCEL_DISPATCH_LOG" "$CANCEL_COMMAND_LOG"; do
		if [ -f "$log" ]; then
			echo "---- $log" >&2
			sed -n '1,160p' "$log" >&2 || true
		fi
	done
	exit 1
}

wait_for_url() {
	url="$1"
	for _ in $(seq 1 50); do
		if curl -fsS "$url" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done
	curl -fsS "$url" >/dev/null
}

wait_for_file_pattern() {
	path="$1"
	pattern="$2"
	for _ in $(seq 1 100); do
		if [ -f "$path" ] && grep -q "$pattern" "$path"; then
			return 0
		fi
		sleep 0.1
	done
	grep -q "$pattern" "$path"
}

wait_for_results_pattern() {
	pattern="$1"
	for _ in $(seq 1 100); do
		RESULTS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/results" 2>/dev/null || true)"
		if printf "%s" "$RESULTS" | grep -q "$pattern"; then
			return 0
		fi
		sleep 0.1
	done
	curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/results" | grep -q "$pattern"
}

wait_for_target() {
	for _ in $(seq 1 100); do
		TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets" 2>/dev/null || true)"
		if printf "%s" "$TARGETS" | grep -q "local-smoke"; then
			printf "%s\n" "$TARGETS"
			return 0
		fi
		sleep 0.1
	done
	curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets"
	return 1
}

expect_dispatch_interrupted_after_restart() {
	pid="$1"
	log="$2"
	label="$3"
	set +e
	wait "$pid"
	status=$?
	set -e
	if [ "$status" -eq 0 ]; then
		fail_with_logs "$label dispatch unexpectedly survived coordinator restart without reconnect"
	fi
	if ! grep -q "unexpected EOF" "$log"; then
		fail_with_logs "$label dispatch did not record coordinator stream interruption"
	fi
}

start_coordinator() {
	log="$1"
	TRANSWARP_TOKEN="runner-token" "$COORDINATOR_BIN" \
		-listen "127.0.0.1:$COORDINATOR_PORT" \
		-token "coord-token" \
		-target-token "target-token" \
		-public-url "http://127.0.0.1:$COORDINATOR_PORT" \
		-state-path "$STATE" >"$log" 2>&1 &
	COORDINATOR_PID=$!
	wait_for_url "http://127.0.0.1:$COORDINATOR_PORT/health"
}

cancel_dispatch_after_restart() {
	: >"$CANCEL_COMMAND_LOG"
	for attempt in $(seq 1 20); do
		printf "attempt %s\n" "$attempt" >>"$CANCEL_COMMAND_LOG"
		if "$DISPATCH_BIN" \
			-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
			-coordinator-token "coord-token" \
			-cancel \
			-request-id local-smoke-restart-cancel >>"$CANCEL_COMMAND_LOG" 2>&1; then
			cat "$CANCEL_COMMAND_LOG"
			return 0
		fi
		sleep 0.1
	done
	cat "$CANCEL_COMMAND_LOG" >&2
	return 1
}

cd "$ROOT"

go build -o "$COORDINATOR_BIN" ./cmd/transwarp-coordinator
go build -o "$CONFIG_BIN" ./cmd/transwarp-config
go build -o "$DIAGNOSE_BIN" ./cmd/transwarp-diagnose
go build -o "$DISPATCH_BIN" ./cmd/transwarp-dispatch
go build -o "$RUNNER_BIN" ./cmd/transwarp-runner

start_coordinator "$COORDINATOR_LOG"

cat >"$SLOW_SCRIPT" <<'SH'
#!/bin/sh
set -eu
echo restart durable accepted
sleep 2
echo restart durable finished
SH
chmod +x "$SLOW_SCRIPT"

cat >"$CANCEL_SCRIPT" <<'SH'
#!/bin/sh
set -eu
term() {
	echo restart cancel trapped
	exit 143
}
trap term TERM INT
echo restart cancel accepted
i=0
while [ "$i" -lt 30 ]; do
	i=$((i + 1))
	sleep 1
done
echo restart cancel unexpectedly finished
SH
chmod +x "$CANCEL_SCRIPT"

ECHO_JOB="$("$CONFIG_BIN" \
	-print-job-spec \
	-job-id echo \
	-job-label "Echo Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command /bin/echo \
	-job-arg "hello coordinator auto select" \
	-job-timeout-seconds 10)"
SLOW_JOB="$("$CONFIG_BIN" \
	-print-job-spec \
	-job-id slow \
	-job-label "Restart Durability Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command "$SLOW_SCRIPT" \
	-job-timeout-seconds 10)"
CANCEL_JOB="$("$CONFIG_BIN" \
	-print-job-spec \
	-job-id cancel \
	-job-label "Restart Cancel Smoke" \
	-job-working-directory "$TMPDIR" \
	-job-command "$CANCEL_SCRIPT" \
	-job-timeout-seconds 60)"

TRANSWARP_TOKEN=runner-token \
	TRANSWARP_REGISTRATION_TOKEN=target-token \
	"$CONFIG_BIN" \
	-output "$CONFIG" \
	-listen "127.0.0.1:$RUNNER_PORT" \
	-machine-id local-smoke \
	-machine-name "Local Smoke Mac" \
	-workspace-root "$TMPDIR" \
	-ci-registration-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/register" \
	-ci-heartbeat-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/heartbeat" \
	-ci-deregistration-url "http://127.0.0.1:$COORDINATOR_PORT/transwarp/deregister" \
	-heartbeat-seconds 1 \
	-tunnel-mode off \
	-public-url "http://127.0.0.1:$RUNNER_PORT" \
	-job "$ECHO_JOB" \
	-job "$SLOW_JOB" \
	-job "$CANCEL_JOB"

"$RUNNER_BIN" -config "$CONFIG" >"$RUNNER_LOG" 2>&1 &
RUNNER_PID=$!

wait_for_target

"$DIAGNOSE_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-token "runner-token" \
	-machine-id local-smoke \
	-job echo \
	-allow-http

"$DISPATCH_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-job echo \
	-request-id local-smoke-run

"$DISPATCH_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-machine-id local-smoke \
	-job slow \
	-request-id local-smoke-restart-run >"$RESTART_DISPATCH_LOG" 2>&1 &
RESTART_DISPATCH_PID=$!

wait_for_file_pattern "$RESTART_DISPATCH_LOG" '"message":"accepted runner build"'

kill "$COORDINATOR_PID" 2>/dev/null || true
wait "$COORDINATOR_PID" 2>/dev/null || true
COORDINATOR_PID=""

start_coordinator "$COORDINATOR_RESTART_LOG"
wait_for_target

"$DISPATCH_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-machine-id local-smoke \
	-job slow \
	-request-id local-smoke-restart-run >"$RECONNECT_DISPATCH_LOG" 2>&1

expect_dispatch_interrupted_after_restart "$RESTART_DISPATCH_PID" "$RESTART_DISPATCH_LOG" "restart durability"

printf "coordinator restart durability dispatch reconnected after accepted runner build\n"
cat "$RECONNECT_DISPATCH_LOG"
grep -q '"message":"accepted runner build"' "$RECONNECT_DISPATCH_LOG"
grep -q "restart durable finished" "$RECONNECT_DISPATCH_LOG"
grep -q "\[result\] recorded passed" "$RECONNECT_DISPATCH_LOG"

RESULTS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/results")"
printf "%s\n" "$RESULTS"
printf "%s" "$RESULTS" | grep -q "local-smoke-run"
printf "%s" "$RESULTS" | grep -q "local-smoke-restart-run"
printf "%s" "$RESULTS" | grep -q '"status":"passed"'

"$DISPATCH_BIN" \
	-coordinator-url "http://127.0.0.1:$COORDINATOR_PORT" \
	-coordinator-token "coord-token" \
	-machine-id local-smoke \
	-job cancel \
	-request-id local-smoke-restart-cancel >"$CANCEL_DISPATCH_LOG" 2>&1 &
CANCEL_DISPATCH_PID=$!

wait_for_file_pattern "$CANCEL_DISPATCH_LOG" '"message":"accepted runner build"'

kill "$COORDINATOR_PID" 2>/dev/null || true
wait "$COORDINATOR_PID" 2>/dev/null || true
COORDINATOR_PID=""

start_coordinator "$COORDINATOR_RESTART_LOG"
wait_for_target

if ! cancel_dispatch_after_restart; then
	fail_with_logs "coordinator cancel dispatch failed after restart"
fi

expect_dispatch_interrupted_after_restart "$CANCEL_DISPATCH_PID" "$CANCEL_DISPATCH_LOG" "restart cancel"

wait_for_results_pattern "local-smoke-restart-cancel"
wait_for_results_pattern '"status":"canceled"'
RESULTS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/results")"
printf "%s\n" "$RESULTS"
printf "%s" "$RESULTS" | grep -q "local-smoke-restart-cancel"
printf "%s" "$RESULTS" | grep -q '"status":"canceled"'
cat "$CANCEL_DISPATCH_LOG"
grep -q "restart cancel accepted" "$CANCEL_DISPATCH_LOG"
printf "coordinator restart durability cancel forwarded after accepted runner build\n"

kill "$RUNNER_PID" 2>/dev/null || true
wait "$RUNNER_PID" 2>/dev/null || true
RUNNER_PID=""

for _ in $(seq 1 50); do
	TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets" 2>/dev/null || true)"
	if ! printf "%s" "$TARGETS" | grep -q "local-smoke"; then
		break
	fi
	sleep 0.1
done

TARGETS="$(curl -fsS -H "Authorization: Bearer coord-token" "http://127.0.0.1:$COORDINATOR_PORT/transwarp/targets")"
printf "%s\n" "$TARGETS"
if printf "%s" "$TARGETS" | grep -q "local-smoke"; then
	echo "runner did not deregister from coordinator; runner log: $RUNNER_LOG" >&2
	exit 1
fi

printf "logs: %s\n" "$TMPDIR"
