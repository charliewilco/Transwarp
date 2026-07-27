#!/bin/sh
set -eu

fail() {
	echo "self-hosted Mac readiness failed: $*" >&2
	exit 1
}

warn() {
	echo "self-hosted Mac readiness warning: $*" >&2
}

IDENTITIES_FILE="$(mktemp "${TMPDIR:-/tmp}/transwarp-codesigning-identities.XXXXXX")"
trap 'rm -f "$IDENTITIES_FILE"' EXIT INT TERM
EVIDENCE_PATH="${TRANSWARP_SELF_HOSTED_EVIDENCE:-}"
EVIDENCE_RECEIPT="$EVIDENCE_PATH"
if [ -z "$EVIDENCE_RECEIPT" ]; then
	EVIDENCE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-self-hosted-evidence.XXXXXX")"
	EVIDENCE_RECEIPT="$EVIDENCE_DIR/self-hosted-mac.json"
else
	EVIDENCE_DIR="$(dirname "$EVIDENCE_RECEIPT")"
	mkdir -p "$EVIDENCE_DIR"
fi
SOURCE_LOG="$EVIDENCE_DIR/self-hosted-readiness.log"

ARCH="$(uname -m)"
[ "$ARCH" = "arm64" ] || fail "expected Apple Silicon arm64, got $ARCH"

OS_NAME="$(sw_vers -productName)"
OS_VERSION="$(sw_vers -productVersion)"
case "$OS_NAME" in
	macOS) ;;
	*) fail "expected macOS, got $OS_NAME" ;;
esac

MAJOR_VERSION="$(printf "%s" "$OS_VERSION" | awk -F. '{print $1}')"
[ "$MAJOR_VERSION" -ge 14 ] || fail "expected macOS 14 or newer, got $OS_VERSION"

command -v xcodebuild >/dev/null 2>&1 || fail "xcodebuild is not available"
XCODE_VERSION="$(xcodebuild -version | tr '\n' ' ' | sed 's/[[:space:]]*$//')"

DEVELOPER_DIR_VALUE="$(xcode-select -p 2>/dev/null || true)"
[ -n "$DEVELOPER_DIR_VALUE" ] || fail "xcode-select does not point at a developer directory"

if [ -n "${GITHUB_ACTIONS:-}" ]; then
	[ -n "${RUNNER_NAME:-}" ] || warn "RUNNER_NAME is not set"
	[ -n "${RUNNER_OS:-}" ] || warn "RUNNER_OS is not set"
	[ "${RUNNER_OS:-macOS}" = "macOS" ] || fail "expected GitHub RUNNER_OS=macOS, got ${RUNNER_OS:-<empty>}"
fi

CODE_SIGNING_IDENTITIES_VISIBLE=false
if security find-identity -v -p codesigning >"$IDENTITIES_FILE" 2>/dev/null; then
	if grep -q "0 valid identities found" "$IDENTITIES_FILE"; then
		warn "no valid code-signing identities found; unsigned builds can still compile, but signing proof is missing"
	else
		CODE_SIGNING_IDENTITIES_VISIBLE=true
		echo "code-signing identities are visible to this runner"
	fi
else
	warn "could not inspect code-signing identities"
fi

cat > "$SOURCE_LOG" <<LOG
self-hosted Mac readiness passed
architecture=$ARCH
macos=$OS_VERSION
developer_dir=$DEVELOPER_DIR_VALUE
xcode=$XCODE_VERSION
code_signing_identities_visible=$CODE_SIGNING_IDENTITIES_VISIBLE
github_actions=$([ -n "${GITHUB_ACTIONS:-}" ] && printf true || printf false)
runner_name=${RUNNER_NAME:-}
runner_os=${RUNNER_OS:-}
LOG
go run ./cmd/transwarp-audit \
	-write-self-hosted-evidence "$EVIDENCE_RECEIPT" \
	-self-hosted-architecture "$ARCH" \
	-self-hosted-macos "$OS_VERSION" \
	-self-hosted-developer-dir "$DEVELOPER_DIR_VALUE" \
	-self-hosted-xcode "$XCODE_VERSION" \
	-self-hosted-code-signing-identities-visible="$CODE_SIGNING_IDENTITIES_VISIBLE" \
	-self-hosted-github-actions="$([ -n "${GITHUB_ACTIONS:-}" ] && printf true || printf false)" \
	-self-hosted-runner-name "${RUNNER_NAME:-}" \
	-self-hosted-runner-os "${RUNNER_OS:-}" \
	-self-hosted-source-log "$SOURCE_LOG"
go run ./cmd/transwarp-audit \
	-evidence-only \
	-self-hosted-evidence "$EVIDENCE_RECEIPT" \
	-summary >/dev/null
if [ -n "$EVIDENCE_PATH" ]; then
	echo "evidence=$EVIDENCE_PATH"
fi

echo "self-hosted Mac readiness passed"
echo "architecture=$ARCH"
echo "macos=$OS_VERSION"
echo "developer_dir=$DEVELOPER_DIR_VALUE"
echo "xcode=$XCODE_VERSION"
