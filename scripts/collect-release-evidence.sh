#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EVIDENCE_DIR="${TRANSWARP_EVIDENCE_DIR:-$ROOT/.build/release-evidence}"
APP_DIR="${TRANSWARP_APP_PATH:-$ROOT/.build/Transwarp.app}"
ARCHIVE="${TRANSWARP_RELEASE_ARCHIVE:-$ROOT/.build/Transwarp-release.zip}"
AUDIT_JSON="$EVIDENCE_DIR/transwarp-audit.json"
AUDIT_STDERR="$EVIDENCE_DIR/transwarp-audit.stderr"
APP_LAUNCH_EVIDENCE="${TRANSWARP_APP_LAUNCH_EVIDENCE:-$EVIDENCE_DIR/app-launch-evidence.json}"
NAMED_TUNNEL_LOG="$EVIDENCE_DIR/named-tunnel-coordinator-smoke.log"
NAMED_TUNNEL_EVIDENCE="${TRANSWARP_NAMED_TUNNEL_EVIDENCE:-$EVIDENCE_DIR/named-tunnel-evidence.json}"
CI_DISPATCH_EVIDENCE="${TRANSWARP_CI_DISPATCH_EVIDENCE:-}"
CI_DISPATCH_RECEIPT="$EVIDENCE_DIR/ci-dispatch-evidence.json"
ALLOW_INCOMPLETE="${TRANSWARP_COLLECT_ALLOW_INCOMPLETE:-0}"
RUN_NAMED_TUNNEL="${TRANSWARP_COLLECT_NAMED_TUNNEL:-auto}"
NOTARIZE_REQUESTED="${TRANSWARP_NOTARIZE_REQUESTED:-0}"
CLEAN_MAC_EVIDENCE="${TRANSWARP_CLEAN_MAC_EVIDENCE:-}"
NAMED_TUNNEL_SMOKE_SCRIPT="${TRANSWARP_NAMED_TUNNEL_SMOKE_SCRIPT:-./scripts/smoke-cloudflare-named-coordinator.sh}"
APP_PACKAGED=0

fail() {
	echo "release evidence failed: $*" >&2
	exit 1
}

is_set() {
	[ -n "${1:-}" ]
}

validate_boolean() {
	name="$1"
	value="$2"
	case "$value" in
		1|true|yes|0|false|no)
			;;
		*)
			fail "$name must be 1, true, yes, 0, false, or no"
			;;
	esac
}

validate_named_tunnel_collection() {
	case "$RUN_NAMED_TUNNEL" in
		auto|1|true|yes|0|false|no)
			;;
		*)
			fail "TRANSWARP_COLLECT_NAMED_TUNNEL must be auto, 1, true, yes, 0, false, or no"
			;;
	esac
}

will_collect_named_tunnel() {
	case "$RUN_NAMED_TUNNEL" in
		1|true|yes)
			return 0
			;;
		auto)
			is_set "${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-}" && is_set "${TRANSWARP_PUBLIC_URL:-}"
			return $?
			;;
		*)
			return 1
			;;
	esac
}

is_true() {
	case "$2" in
		1|true|yes)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

require_named_tunnel_env() {
	is_set "${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-}" || fail "TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN is required for named-tunnel evidence"
	is_set "${TRANSWARP_PUBLIC_URL:-}" || fail "TRANSWARP_PUBLIC_URL is required for named-tunnel evidence"
}

require_ci_dispatch_env() {
	is_set "${GITHUB_RUN_ID:-}" || fail "GITHUB_RUN_ID is required for CI dispatch evidence"
	is_set "${GITHUB_RUN_ATTEMPT:-}" || fail "GITHUB_RUN_ATTEMPT is required for CI dispatch evidence"
	is_set "${GITHUB_WORKFLOW:-}" || fail "GITHUB_WORKFLOW is required for CI dispatch evidence"
	is_set "${GITHUB_JOB:-}" || fail "GITHUB_JOB is required for CI dispatch evidence"
	is_set "${GITHUB_REPOSITORY:-}" || fail "GITHUB_REPOSITORY is required for CI dispatch evidence"
	is_set "${GITHUB_SHA:-}" || fail "GITHUB_SHA is required for CI dispatch evidence"
	is_set "${RUNNER_OS:-}" || fail "RUNNER_OS is required for CI dispatch evidence"
	is_set "${RUNNER_ARCH:-}" || fail "RUNNER_ARCH is required for CI dispatch evidence"
	[ "$RUNNER_OS" = "macOS" ] || fail "CI dispatch evidence must run on RUNNER_OS=macOS, got $RUNNER_OS"
	[ "$RUNNER_ARCH" = "ARM64" ] || fail "CI dispatch evidence must run on RUNNER_ARCH=ARM64, got $RUNNER_ARCH"
}

require_strict_release_policy_env() {
	if ! is_true TRANSWARP_COLLECT_ALLOW_INCOMPLETE "$ALLOW_INCOMPLETE"; then
		is_set "${TRANSWARP_EXPECTED_CLOUDFLARED_VERSION:-}" || fail "TRANSWARP_EXPECTED_CLOUDFLARED_VERSION is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1"
	fi
}

require_strict_distribution_env() {
	if is_true TRANSWARP_COLLECT_ALLOW_INCOMPLETE "$ALLOW_INCOMPLETE"; then
		return
	fi
	is_set "${SIGN_IDENTITY:-}" || fail "SIGN_IDENTITY is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1"
	[ "${SIGN_IDENTITY:-}" != "-" ] || fail "SIGN_IDENTITY must be a Developer ID identity for strict release evidence"
	is_true TRANSWARP_NOTARIZE_REQUESTED "$NOTARIZE_REQUESTED" || fail "TRANSWARP_NOTARIZE_REQUESTED=1 is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1"
	is_set "${APPLE_KEYCHAIN_PROFILE:-}" || fail "APPLE_KEYCHAIN_PROFILE is required for strict release notarization; create it with xcrun notarytool store-credentials"
}

will_generate_ci_dispatch_evidence() {
	[ -n "${GITHUB_ACTIONS:-}" ] || return 1
	case "$RUN_NAMED_TUNNEL" in
		1|true|yes)
			return 0
			;;
		auto)
			is_set "${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-}" && is_set "${TRANSWARP_PUBLIC_URL:-}"
			return $?
			;;
		*)
			return 1
			;;
	esac
}

require_strict_external_evidence_env() {
	if is_true TRANSWARP_COLLECT_ALLOW_INCOMPLETE "$ALLOW_INCOMPLETE"; then
		return
	fi
	if [ ! -f "$NAMED_TUNNEL_EVIDENCE" ]; then
		case "$RUN_NAMED_TUNNEL" in
			1|true|yes)
				;;
			auto)
				is_set "${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-}" && is_set "${TRANSWARP_PUBLIC_URL:-}" || fail "named-tunnel evidence is required; set TRANSWARP_COLLECT_NAMED_TUNNEL=1 with TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN and TRANSWARP_PUBLIC_URL, provide TRANSWARP_NAMED_TUNNEL_EVIDENCE, or set TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1"
				;;
			*)
				fail "named-tunnel evidence is required; set TRANSWARP_COLLECT_NAMED_TUNNEL=1 with TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN and TRANSWARP_PUBLIC_URL, provide TRANSWARP_NAMED_TUNNEL_EVIDENCE, or set TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1"
				;;
		esac
	fi
	if [ ! -f "$CI_DISPATCH_EVIDENCE" ] && ! will_generate_ci_dispatch_evidence; then
		fail "CI dispatch evidence is required; run inside GitHub Actions with named-tunnel collection, provide TRANSWARP_CI_DISPATCH_EVIDENCE, or set TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1"
	fi
	if [ -n "$CLEAN_MAC_EVIDENCE" ] && [ ! -f "$CLEAN_MAC_EVIDENCE" ]; then
		fail "clean-Mac evidence file does not exist: $CLEAN_MAC_EVIDENCE"
	fi
}

validate_receipt_shape() {
	label="$1"
	path="$2"
	expected_kind="$3"
	message="$(go run ./cmd/transwarp-audit -check-receipt "$path" -check-receipt-kind "$expected_kind" 2>&1)" || fail "$label evidence invalid: $message"
}

validate_audit_evidence() {
	label="$1"
	shift
	message="$(TRANSWARP_SELF_HOSTED_EVIDENCE= \
		TRANSWARP_APP_LAUNCH_EVIDENCE= \
		TRANSWARP_NAMED_TUNNEL_EVIDENCE= \
		TRANSWARP_CI_DISPATCH_EVIDENCE= \
		TRANSWARP_CLEAN_MAC_EVIDENCE= \
		go run ./cmd/transwarp-audit -evidence-only -summary "$@" 2>&1)" || fail "$label evidence invalid: $message"
}

preflight_external_evidence() {
	if [ -f "$NAMED_TUNNEL_EVIDENCE" ] && ! will_collect_named_tunnel; then
		validate_audit_evidence "named-tunnel" -named-tunnel-evidence "$NAMED_TUNNEL_EVIDENCE"
	fi
	if [ -n "$CI_DISPATCH_EVIDENCE" ] && [ -f "$CI_DISPATCH_EVIDENCE" ]; then
		validate_audit_evidence "CI dispatch" -ci-dispatch-evidence "$CI_DISPATCH_EVIDENCE"
	fi
	if [ -f "$NAMED_TUNNEL_EVIDENCE" ] && [ -n "$CI_DISPATCH_EVIDENCE" ] && [ -f "$CI_DISPATCH_EVIDENCE" ] && ! will_collect_named_tunnel; then
		validate_audit_evidence "release evidence correlation" -named-tunnel-evidence "$NAMED_TUNNEL_EVIDENCE" -ci-dispatch-evidence "$CI_DISPATCH_EVIDENCE"
	fi
	if [ -n "$CLEAN_MAC_EVIDENCE" ] && [ -f "$CLEAN_MAC_EVIDENCE" ]; then
		validate_receipt_shape "clean-Mac" "$CLEAN_MAC_EVIDENCE" "transwarp-clean-mac-evidence"
	fi
}

should_run_named_tunnel() {
	case "$RUN_NAMED_TUNNEL" in
		1|true|yes)
			require_named_tunnel_env
			return 0
			;;
		0|false|no)
			return 1
			;;
		auto)
			is_set "${TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN:-}" && is_set "${TRANSWARP_PUBLIC_URL:-}"
			return $?
			;;
		*)
			fail "TRANSWARP_COLLECT_NAMED_TUNNEL must be auto, 1, true, yes, 0, false, or no"
			;;
	esac
}

cd "$ROOT"
mkdir -p "$EVIDENCE_DIR" "$(dirname "$ARCHIVE")"
go run ./cmd/transwarp-audit -check-release-collection-inputs >/dev/null
validate_boolean TRANSWARP_COLLECT_ALLOW_INCOMPLETE "$ALLOW_INCOMPLETE"
validate_boolean TRANSWARP_NOTARIZE_REQUESTED "$NOTARIZE_REQUESTED"
validate_named_tunnel_collection
require_strict_release_policy_env
require_strict_distribution_env
require_strict_external_evidence_env
preflight_external_evidence

echo "release evidence started"
echo "evidence_dir=$EVIDENCE_DIR"

if [ -n "${TRANSWARP_SELF_HOSTED_EVIDENCE:-}" ]; then
	echo "checking self-hosted Mac readiness"
	./scripts/check-self-hosted-mac.sh
fi

if should_run_named_tunnel; then
	if [ -n "${GITHUB_ACTIONS:-}" ]; then
		require_ci_dispatch_env
	fi
	if [ "${TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE:-app}" = "app" ]; then
		echo "packaging app"
		./scripts/package-app.sh "$APP_DIR" >/dev/null
		APP_PACKAGED=1
	fi
	echo "running named tunnel coordinator smoke"
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$NAMED_TUNNEL_EVIDENCE" \
		TRANSWARP_NAMED_TUNNEL_APP_PATH="$APP_DIR" \
		"$NAMED_TUNNEL_SMOKE_SCRIPT" 2>&1 | tee "$NAMED_TUNNEL_LOG"
	if [ -n "${GITHUB_ACTIONS:-}" ]; then
		echo "writing CI dispatch evidence receipt"
		grep -q "diagnosis passed" "$NAMED_TUNNEL_LOG" || fail "named tunnel smoke did not record a passed diagnosis"
		grep -q "\[result\] recorded passed" "$NAMED_TUNNEL_LOG" || fail "named tunnel smoke did not record a passed dispatch result"
		go run ./cmd/transwarp-audit \
			-named-tunnel-evidence "$NAMED_TUNNEL_EVIDENCE" \
			-ci-dispatch-source-log "$NAMED_TUNNEL_LOG" \
			-ci-dispatch-source-log-name "$(basename "$NAMED_TUNNEL_LOG")" \
			-write-ci-dispatch-evidence "$CI_DISPATCH_RECEIPT"
		CI_DISPATCH_EVIDENCE="$CI_DISPATCH_RECEIPT"
	fi
else
	echo "named tunnel coordinator smoke skipped; set TRANSWARP_COLLECT_NAMED_TUNNEL=1 with tunnel credentials to require it"
fi

if [ "$APP_PACKAGED" != "1" ]; then
	echo "packaging app"
	./scripts/package-app.sh "$APP_DIR" >/dev/null
fi

if [ -n "$CLEAN_MAC_EVIDENCE" ] && [ -f "$CLEAN_MAC_EVIDENCE" ]; then
	validate_audit_evidence "clean-Mac" -app "$APP_DIR" -clean-mac-evidence "$CLEAN_MAC_EVIDENCE"
fi

echo "checking packaged app launch"
TRANSWARP_APP_LAUNCH_EVIDENCE="$APP_LAUNCH_EVIDENCE" ./scripts/smoke-app-launch.sh "$APP_DIR" >/dev/null

if is_true TRANSWARP_NOTARIZE_REQUESTED "$NOTARIZE_REQUESTED"; then
	echo "notarizing app"
	./scripts/notarize-app.sh "$APP_DIR" 2>&1 | tee "$EVIDENCE_DIR/notarization.log"
else
	echo "notarization skipped; set TRANSWARP_NOTARIZE_REQUESTED=1 to require it"
fi

echo "archiving release"
TRANSWARP_RELEASE_ARCHIVE="$ARCHIVE" ./scripts/archive-release.sh "$APP_DIR" > "$EVIDENCE_DIR/release-archive-path.txt"

set -- -app "$APP_DIR" -release-archive "$ARCHIVE"
if [ -n "${TRANSWARP_SELF_HOSTED_EVIDENCE:-}" ]; then
	set -- "$@" -self-hosted-evidence "$TRANSWARP_SELF_HOSTED_EVIDENCE"
fi
if [ -f "$APP_LAUNCH_EVIDENCE" ]; then
	set -- "$@" -app-launch-evidence "$APP_LAUNCH_EVIDENCE"
fi
if [ -f "$NAMED_TUNNEL_EVIDENCE" ]; then
	set -- "$@" -named-tunnel-evidence "$NAMED_TUNNEL_EVIDENCE"
fi
if [ -n "$CI_DISPATCH_EVIDENCE" ]; then
	set -- "$@" -ci-dispatch-evidence "$CI_DISPATCH_EVIDENCE"
fi
if [ -n "$CLEAN_MAC_EVIDENCE" ]; then
	set -- "$@" -clean-mac-evidence "$CLEAN_MAC_EVIDENCE"
fi

echo "auditing release evidence"
set +e
go run ./cmd/transwarp-audit "$@" > "$AUDIT_JSON" 2> "$AUDIT_STDERR"
audit_status=$?
set -e

go run ./cmd/transwarp-audit -summary -allow-incomplete -report "$AUDIT_JSON" || true

if [ "$audit_status" -ne 0 ] && ! is_true TRANSWARP_COLLECT_ALLOW_INCOMPLETE "$ALLOW_INCOMPLETE"; then
	fail "transwarp-audit is incomplete; inspect $AUDIT_JSON"
fi

echo "release evidence complete: $EVIDENCE_DIR"
