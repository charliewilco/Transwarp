#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

TMPDIR="${TMPDIR:-/tmp}/transwarp-action-smoke.$$"
ACTION_SCRIPT="$TMPDIR/action-run.sh"
FAKE_BIN="$TMPDIR/bin"
GO_LOG="$TMPDIR/go.log"
GITHUB_OUTPUT_FILE="$TMPDIR/github-output"

cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$FAKE_BIN"

go run ./cmd/transwarp-audit \
	-check-github-actions \
	-write-github-action-script "$ACTION_SCRIPT"

cat > "$FAKE_BIN/go" <<'SH'
#!/bin/sh
set -eu

write_output() {
	name="$1"
	value="$2"
	printf '%s<<TRANSWARP_OUTPUT\n%s\nTRANSWARP_OUTPUT\n' "$name" "$value" >> "$GITHUB_OUTPUT"
}

{
	printf 'cmd=%s\n' "$*"
	printf 'TRANSWARP_URL=%s\n' "${TRANSWARP_URL:-}"
	printf 'TRANSWARP_TOKEN=%s\n' "${TRANSWARP_TOKEN:-}"
	printf 'TRANSWARP_COORDINATOR_URL=%s\n' "${TRANSWARP_COORDINATOR_URL:-}"
	printf 'TRANSWARP_COORDINATOR_TOKEN=%s\n' "${TRANSWARP_COORDINATOR_TOKEN:-}"
	printf 'TRANSWARP_REPORT_URL=%s\n' "${TRANSWARP_REPORT_URL:-}"
	printf 'TRANSWARP_REPORT_TOKEN=%s\n' "${TRANSWARP_REPORT_TOKEN:-}"
	printf 'TRANSWARP_REQUEST_ID=%s\n' "${TRANSWARP_REQUEST_ID:-}"
	printf 'TRANSWARP_BUILD_ID=%s\n' "${TRANSWARP_BUILD_ID:-}"
	printf 'TRANSWARP_REPO_URL=%s\n' "${TRANSWARP_REPO_URL:-}"
	printf 'TRANSWARP_REF=%s\n' "${TRANSWARP_REF:-}"
	printf 'TRANSWARP_COMMIT=%s\n' "${TRANSWARP_COMMIT:-}"
	printf 'TRANSWARP_JOB=%s\n' "${TRANSWARP_JOB:-}"
	printf 'TRANSWARP_MIN_CPU_COUNT=%s\n' "${TRANSWARP_MIN_CPU_COUNT:-}"
	printf 'TRANSWARP_MIN_MEMORY_BYTES=%s\n' "${TRANSWARP_MIN_MEMORY_BYTES:-}"
	printf '%s\n' '---'
} >> "$TRANSWARP_ACTION_GO_LOG"

case "$*" in
	*cmd/transwarp-dispatch*)
		if [ -n "${GITHUB_OUTPUT:-}" ]; then
			if [ -n "${TRANSWARP_REQUEST_ID:-}" ]; then
				write_output request-id "$TRANSWARP_REQUEST_ID"
			fi
			if [ -n "${TRANSWARP_JOB:-}" ]; then
				write_output job-id "$TRANSWARP_JOB"
			fi
			if [ -n "${TRANSWARP_REPO_URL:-}" ]; then
				write_output repo-url "$TRANSWARP_REPO_URL"
			fi
			if [ -n "${TRANSWARP_REF:-}" ]; then
				write_output ref "$TRANSWARP_REF"
			fi
			if [ -n "${TRANSWARP_COMMIT:-}" ]; then
				write_output commit "$TRANSWARP_COMMIT"
			fi
			if echo "$*" | grep -q -- ' -result'; then
				write_output build-id "coordinator-result-build-from-fake-go"
				write_output machine-id "${TRANSWARP_MACHINE_ID:-coordinator-machine-from-fake-go}"
				write_output public-url "https://runner.example.com"
				write_output status "failed"
				write_output exit-code "65"
				write_output error "xcodebuild exited 65"
			elif [ -n "${TRANSWARP_BUILD_ID:-}" ]; then
				write_output build-id "$TRANSWARP_BUILD_ID"
				if ! echo "$*" | grep -q -- ' -cancel'; then
					write_output job-id "xcode-debug"
					write_output status "passed"
					write_output exit-code "0"
				fi
			elif [ -n "${TRANSWARP_COORDINATOR_URL:-}" ]; then
				write_output build-id "coordinator-build-from-fake-go"
				write_output machine-id "${TRANSWARP_MACHINE_ID:-coordinator-machine-from-fake-go}"
				write_output public-url "https://runner.example.com"
			else
				write_output build-id "build-from-fake-go"
				write_output status "passed"
				write_output exit-code "0"
			fi
		fi
		;;
esac
SH
chmod 755 "$FAKE_BIN/go"

run_action() {
	env -i \
		PATH="$FAKE_BIN:/usr/bin:/bin" \
		TRANSWARP_ACTION_GO_LOG="$GO_LOG" \
		GITHUB_OUTPUT="$GITHUB_OUTPUT_FILE" \
		GITHUB_RUN_ID=1234 \
		GITHUB_RUN_ATTEMPT=2 \
		GITHUB_JOB=transwarp-build \
		GITHUB_SERVER_URL=https://github.com \
		GITHUB_REPOSITORY="${GITHUB_REPOSITORY-example/app}" \
		GITHUB_REF=refs/heads/main \
		GITHUB_SHA=abc123 \
		INPUT_MODE="${INPUT_MODE:-direct}" \
		INPUT_VERSION="${INPUT_VERSION:-local}" \
		INPUT_DIAGNOSE="${INPUT_DIAGNOSE:-true}" \
		INPUT_ALLOW_HTTP="${INPUT_ALLOW_HTTP:-false}" \
		INPUT_CANCEL="${INPUT_CANCEL:-false}" \
		INPUT_RESULT="${INPUT_RESULT:-false}" \
		INPUT_TAIL="${INPUT_TAIL:-false}" \
		INPUT_URL="${INPUT_URL:-}" \
		INPUT_TOKEN="${INPUT_TOKEN:-}" \
		INPUT_COORDINATOR_URL="${INPUT_COORDINATOR_URL:-}" \
		INPUT_COORDINATOR_TOKEN="${INPUT_COORDINATOR_TOKEN:-}" \
		INPUT_ACCESS_CLIENT_ID="${INPUT_ACCESS_CLIENT_ID:-}" \
		INPUT_ACCESS_CLIENT_SECRET="${INPUT_ACCESS_CLIENT_SECRET:-}" \
		INPUT_JOB="${INPUT_JOB:-}" \
		INPUT_REQUEST_ID="${INPUT_REQUEST_ID:-}" \
		INPUT_BUILD_ID="${INPUT_BUILD_ID:-}" \
		INPUT_REPO_URL="${INPUT_REPO_URL:-}" \
		INPUT_REF="${INPUT_REF:-}" \
		INPUT_COMMIT="${INPUT_COMMIT:-}" \
		INPUT_CHECKOUT_METADATA="${INPUT_CHECKOUT_METADATA:-true}" \
		INPUT_MACHINE_ID="${INPUT_MACHINE_ID:-}" \
		INPUT_MIN_CPU_COUNT="${INPUT_MIN_CPU_COUNT:-}" \
		INPUT_MIN_MEMORY_BYTES="${INPUT_MIN_MEMORY_BYTES:-}" \
		INPUT_MIN_XCODE_VERSION="${INPUT_MIN_XCODE_VERSION:-}" \
		INPUT_REPORT_URL="${INPUT_REPORT_URL:-}" \
		INPUT_REPORT_TOKEN="${INPUT_REPORT_TOKEN:-}" \
		INPUT_TIMEOUT="${INPUT_TIMEOUT:-30m}" \
		bash "$ACTION_SCRIPT"
}

expect_success() {
	name="$1"
	shift
	: > "$GO_LOG"
	: > "$GITHUB_OUTPUT_FILE"
	if ! ( "$@" ) > "$TMPDIR/$name.out" 2> "$TMPDIR/$name.err"; then
		echo "$name failed unexpectedly" >&2
		cat "$TMPDIR/$name.err" >&2
		exit 1
	fi
}

expect_failure() {
	name="$1"
	expected="$2"
	shift 2
	: > "$GO_LOG"
	: > "$GITHUB_OUTPUT_FILE"
	if ( "$@" ) > "$TMPDIR/$name.out" 2> "$TMPDIR/$name.err"; then
		echo "$name succeeded unexpectedly" >&2
		exit 1
	fi
	if ! grep -q "$expected" "$TMPDIR/$name.err"; then
		echo "$name did not report expected failure: $expected" >&2
		cat "$TMPDIR/$name.err" >&2
		exit 1
	fi
}

expect_output() {
	name="$1"
	value="$2"
	grep -q "^$name<<TRANSWARP_OUTPUT$" "$GITHUB_OUTPUT_FILE"
	grep -q "^$value$" "$GITHUB_OUTPUT_FILE"
}

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_REPORT_URL=https://ci.example.com/transwarp/result
	INPUT_REPORT_TOKEN=report-token
	INPUT_MIN_CPU_COUNT=12
	INPUT_MIN_MEMORY_BYTES=34359738368
	INPUT_TIMEOUT=10m
	expect_success direct_with_report run_action
)
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@local' "$GO_LOG"
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@local -timeout 10m' "$GO_LOG"
grep -q 'TRANSWARP_REPORT_URL=https://ci.example.com/transwarp/result' "$GO_LOG"
grep -q 'TRANSWARP_REPORT_TOKEN=report-token' "$GO_LOG"
grep -q 'TRANSWARP_REQUEST_ID=1234-2-transwarp-build' "$GO_LOG"
grep -q 'TRANSWARP_REPO_URL=https://github.com/example/app.git' "$GO_LOG"
grep -q 'TRANSWARP_MIN_CPU_COUNT=12' "$GO_LOG"
grep -q 'TRANSWARP_MIN_MEMORY_BYTES=34359738368' "$GO_LOG"
expect_output request-id 1234-2-transwarp-build
expect_output build-id build-from-fake-go
expect_output job-id xcode-debug
expect_output repo-url https://github.com/example/app.git
expect_output ref refs/heads/main
expect_output commit abc123
expect_output status passed
expect_output exit-code 0

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=local-debug
	INPUT_CHECKOUT_METADATA=false
	expect_success direct_without_checkout_metadata run_action
)
grep -q '^TRANSWARP_REPO_URL=$' "$GO_LOG"
grep -q '^TRANSWARP_REF=$' "$GO_LOG"
grep -q '^TRANSWARP_COMMIT=$' "$GO_LOG"
if grep -q 'TRANSWARP_REPO_URL=https://github.com/example/app.git' "$GO_LOG"; then
	echo "checkout metadata opt-out should not send the default GitHub repository URL" >&2
	exit 1
fi

(
	GITHUB_REPOSITORY=
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	expect_success direct_without_github_repository run_action
)
grep -q '^TRANSWARP_REPO_URL=$' "$GO_LOG"
if grep -q 'TRANSWARP_REPO_URL=https://github.com/.git' "$GO_LOG"; then
	echo "action synthesized an invalid empty GitHub repository URL" >&2
	exit 1
fi

(
	INPUT_URL=http://127.0.0.1:8188
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_ALLOW_HTTP=yes
	expect_success direct_allow_http run_action
)
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@local -allow-http' "$GO_LOG"

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_JOB=xcode-debug
	expect_success coordinator_without_runner_token run_action
)
grep -q 'TRANSWARP_COORDINATOR_URL=https://coordinator.example.com' "$GO_LOG"
grep -q 'TRANSWARP_COORDINATOR_TOKEN=coordinator-token' "$GO_LOG"
grep -q '^TRANSWARP_TOKEN=$' "$GO_LOG"
expect_output request-id 1234-2-transwarp-build
expect_output build-id coordinator-build-from-fake-go
expect_output job-id xcode-debug
expect_output machine-id coordinator-machine-from-fake-go
expect_output public-url https://runner.example.com

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	expect_success coordinator_with_optional_runner_probe_token run_action
)
grep -q 'TRANSWARP_COORDINATOR_URL=https://coordinator.example.com' "$GO_LOG"
grep -q 'TRANSWARP_COORDINATOR_TOKEN=coordinator-token' "$GO_LOG"
grep -q 'TRANSWARP_TOKEN=runner-token' "$GO_LOG"

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_CANCEL=1
	INPUT_BUILD_ID=build-123
	expect_success direct_cancel run_action
)
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@local -timeout 30m -cancel' "$GO_LOG"
grep -q '^TRANSWARP_BUILD_ID=build-123$' "$GO_LOG"
expect_output build-id build-123
if grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@local' "$GO_LOG"; then
	echo "cancel run should not diagnose before canceling" >&2
	exit 1
fi

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_TAIL=true
	INPUT_BUILD_ID=build-456
	expect_success direct_tail run_action
)
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@local -timeout 30m' "$GO_LOG"
grep -q '^TRANSWARP_JOB=$' "$GO_LOG"
grep -q '^TRANSWARP_BUILD_ID=build-456$' "$GO_LOG"
expect_output build-id build-456
expect_output job-id xcode-debug
expect_output status passed
expect_output exit-code 0
if grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@local' "$GO_LOG"; then
	echo "tail run should not diagnose before reconnecting" >&2
	exit 1
fi

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_CANCEL=true
	INPUT_REQUEST_ID=run-to-cancel
	expect_success coordinator_cancel run_action
)
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@local -timeout 30m -cancel' "$GO_LOG"
grep -q '^TRANSWARP_REQUEST_ID=run-to-cancel$' "$GO_LOG"

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_RESULT=true
	INPUT_REQUEST_ID=run-to-query
	expect_success coordinator_result run_action
)
grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@local -timeout 30m -result' "$GO_LOG"
grep -q '^TRANSWARP_REQUEST_ID=run-to-query$' "$GO_LOG"
if grep -q 'cmd=run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@local' "$GO_LOG"; then
	echo "result run should not diagnose before querying a recorded result" >&2
	exit 1
fi
expect_output request-id run-to-query
expect_output build-id coordinator-result-build-from-fake-go
expect_output status failed
expect_output exit-code 65
expect_output error "xcodebuild exited 65"

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_RESULT=true
	expect_failure coordinator_result_without_request_id 'request-id is required when querying a coordinator result' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_REPORT_URL=https://ci.example.com/transwarp/result
	expect_failure report_url_without_token 'report-token is required when report-url is set' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_REPORT_TOKEN=report-token
	expect_failure report_token_without_url 'report-url is required when report-token is set' run_action
)

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_JOB=xcode-debug
	INPUT_REPORT_URL=https://ci.example.com/transwarp/result
	INPUT_REPORT_TOKEN=report-token
	expect_failure coordinator_report_inputs 'report-url and report-token are only supported in direct mode' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_TAIL=true
	expect_failure direct_tail_without_build_id 'build-id is required when tailing a direct runner build' run_action
)

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_TAIL=true
	INPUT_BUILD_ID=build-456
	expect_failure coordinator_tail 'tail is only supported in direct mode' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_CANCEL=true
	INPUT_TAIL=true
	INPUT_BUILD_ID=build-456
	expect_failure cancel_and_tail 'cancel and tail cannot both be true' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_RESULT=true
	INPUT_REQUEST_ID=run-to-query
	expect_failure direct_result 'result is only supported in coordinator mode' run_action
)

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_CANCEL=true
	INPUT_RESULT=true
	INPUT_REQUEST_ID=run-to-query
	expect_failure cancel_and_result 'cancel and result cannot both be true' run_action
)

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_TAIL=true
	INPUT_RESULT=true
	INPUT_REQUEST_ID=run-to-query
	INPUT_BUILD_ID=build-456
	expect_failure tail_and_result 'tail and result cannot both be true' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_DIAGNOSE=ture
	expect_failure invalid_diagnose 'diagnose must be true, 1, yes, false, 0, or no' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_ALLOW_HTTP=maybe
	expect_failure invalid_allow_http 'allow-http must be true, 1, yes, false, 0, or no' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_CHECKOUT_METADATA=maybe
	expect_failure invalid_checkout_metadata 'checkout-metadata must be true, 1, yes, false, 0, or no' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_MIN_CPU_COUNT=twelve
	expect_failure invalid_min_cpu_count 'min-cpu-count must be an unsigned integer' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_MIN_MEMORY_BYTES=-1
	expect_failure invalid_min_memory_bytes 'min-memory-bytes must be an unsigned integer' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_CANCEL=true
	expect_failure direct_cancel_without_build_id 'build-id is required when canceling a direct runner build' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB='xcode/debug'
	expect_failure invalid_job_id 'job may only contain letters, numbers, dots, underscores, or hyphens' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_JOB=xcode-debug
	INPUT_REQUEST_ID='run/123'
	expect_failure invalid_request_id 'request-id may only contain letters, numbers, dots, underscores, or hyphens' run_action
)

(
	INPUT_URL=https://runner.example.com
	INPUT_TOKEN=runner-token
	INPUT_TAIL=true
	INPUT_BUILD_ID='build/456'
	expect_failure invalid_build_id 'build-id may only contain letters, numbers, dots, underscores, or hyphens' run_action
)

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_JOB=xcode-debug
	INPUT_MACHINE_ID='mac/local'
	expect_failure invalid_machine_id 'machine-id may only contain letters, numbers, dots, underscores, or hyphens' run_action
)

(
	INPUT_MODE=coordinator
	INPUT_COORDINATOR_URL=https://coordinator.example.com
	INPUT_COORDINATOR_TOKEN=coordinator-token
	INPUT_CANCEL=true
	expect_failure coordinator_cancel_without_request_id 'request-id is required when canceling a coordinator dispatch' run_action
)

echo "github action smoke passed"
