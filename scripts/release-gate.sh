#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
STRICT="${TRANSWARP_RELEASE_STRICT:-0}"
MANIFEST="$APP_DIR/Contents/Resources/TranswarpManifest.json"

fail() {
	echo "release gate failed: $*" >&2
	exit 1
}

warn() {
	echo "release gate warning: $*" >&2
}

expect_file() {
	path="$1"
	[ -e "$path" ] || fail "missing $path"
}

expect_arm64_only() {
	path="$1"
	info="$(file "$path")"
	echo "$info"
	printf "%s" "$info" | grep -q "Mach-O 64-bit executable arm64" || fail "$path is not a thin arm64 Mach-O executable"
	if printf "%s" "$info" | grep -q "x86_64"; then
		fail "$path contains x86_64"
	fi
}

expect_signed() {
	path="$1"
	codesign --verify --strict --verbose=2 "$path" >/dev/null 2>&1 || fail "$path does not pass strict codesign verification"
}

sha256() {
	shasum -a 256 "$1" | awk '{print $1}'
}

manifest_value() {
	key="$1"
	plutil -extract "$key" raw "$MANIFEST" 2>/dev/null || true
}

expect_manifest_value() {
	key="$1"
	expected="$2"
	actual="$(manifest_value "$key")"
	[ "$actual" = "$expected" ] || fail "manifest $key expected $expected, got ${actual:-<empty>}"
}

expect_manifest_hash() {
	key="$1"
	path="$2"
	expected="$(manifest_value "$key")"
	actual="$(sha256 "$path")"
	[ "$expected" = "$actual" ] || fail "manifest $key does not match $path"
}

check_cloudflared_version_policy() {
	actual="$(printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
	expected="$(printf '%s' "${TRANSWARP_EXPECTED_CLOUDFLARED_VERSION:-}" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
	if [ -z "$expected" ]; then
		expected="$(manifest_value "expected_cloudflared_version" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
	fi

	if [ -z "$expected" ]; then
		if [ "$STRICT" = "1" ]; then
			fail "expected cloudflared version policy is not configured; set TRANSWARP_EXPECTED_CLOUDFLARED_VERSION or manifest expected_cloudflared_version"
		fi
		warn "expected cloudflared version policy is not configured"
		return
	fi

	if [ "$actual" != "$expected" ]; then
		fail "bundled cloudflared version does not match policy: expected $expected, got ${actual:-<empty>}"
	fi

	printf "cloudflared policy: %s\n" "$expected"
}

signature_details() {
	path="$1"
	codesign -dvvv "$path" 2>&1 || true
}

check_distribution_signature() {
	path="$1"
	label="$2"
	signature="$(signature_details "$path")"
	echo "$signature"

	if printf "%s" "$signature" | grep -q "Signature=adhoc"; then
		if [ "$STRICT" = "1" ]; then
			fail "$label is ad-hoc signed; distribution requires Developer ID signing with hardened runtime"
		fi
		warn "$label is ad-hoc signed; this is development-valid but not distribution-ready"
	fi

	if ! printf "%s" "$signature" | grep -q "Runtime Version="; then
		if [ "$STRICT" = "1" ]; then
			fail "$label is missing hardened runtime"
		fi
		warn "$label is missing hardened runtime"
	fi

	if printf "%s" "$signature" | grep -q "TeamIdentifier=not set"; then
		if [ "$STRICT" = "1" ]; then
			fail "$label is missing a Developer ID team identifier"
		fi
		warn "$label is missing a Developer ID team identifier"
	fi
}

expect_file "$APP_DIR/Contents/Info.plist"
expect_file "$APP_DIR/Contents/MacOS/Transwarp"
expect_file "$APP_DIR/Contents/Resources/transwarp-runner"
expect_file "$APP_DIR/Contents/Resources/cloudflared"
expect_file "$MANIFEST"

BUNDLE_ID="$(plutil -extract CFBundleIdentifier raw "$APP_DIR/Contents/Info.plist")"
[ "$BUNDLE_ID" = "co.charliewil.transwarp" ] || fail "unexpected bundle id $BUNDLE_ID"

MIN_SYSTEM="$(plutil -extract LSMinimumSystemVersion raw "$APP_DIR/Contents/Info.plist")"
[ "$MIN_SYSTEM" = "14.0" ] || fail "unexpected LSMinimumSystemVersion $MIN_SYSTEM"

APP_VERSION="$(plutil -extract CFBundleShortVersionString raw "$APP_DIR/Contents/Info.plist")"
[ -n "$APP_VERSION" ] || fail "missing CFBundleShortVersionString"

BUILD_NUMBER="$(plutil -extract CFBundleVersion raw "$APP_DIR/Contents/Info.plist")"
[ -n "$BUILD_NUMBER" ] || fail "missing CFBundleVersion"

expect_manifest_value "schema_version" "1"
expect_manifest_value "bundle_identifier" "$BUNDLE_ID"
expect_manifest_value "app_version" "$APP_VERSION"
expect_manifest_value "build_number" "$BUILD_NUMBER"
expect_manifest_value "minimum_system_version" "$MIN_SYSTEM"
expect_manifest_value "architecture" "arm64"
expect_manifest_hash "runner_sha256" "$APP_DIR/Contents/Resources/transwarp-runner"
expect_manifest_hash "cloudflared_sha256" "$APP_DIR/Contents/Resources/cloudflared"
CLOUDFLARED_VERSION="$(manifest_value "cloudflared_version")"
[ -n "$CLOUDFLARED_VERSION" ] || fail "manifest cloudflared_version is empty"
printf "cloudflared: %s\n" "$CLOUDFLARED_VERSION"
check_cloudflared_version_policy "$CLOUDFLARED_VERSION"

expect_arm64_only "$APP_DIR/Contents/MacOS/Transwarp"
expect_arm64_only "$APP_DIR/Contents/Resources/transwarp-runner"
expect_arm64_only "$APP_DIR/Contents/Resources/cloudflared"

expect_signed "$APP_DIR/Contents/MacOS/Transwarp"
expect_signed "$APP_DIR/Contents/Resources/transwarp-runner"
expect_signed "$APP_DIR/Contents/Resources/cloudflared"
codesign --verify --deep --strict --verbose=2 "$APP_DIR" >/dev/null 2>&1 || fail "$APP_DIR does not pass deep strict codesign verification"

check_distribution_signature "$APP_DIR" "$APP_DIR"
check_distribution_signature "$APP_DIR/Contents/MacOS/Transwarp" "$APP_DIR/Contents/MacOS/Transwarp"
check_distribution_signature "$APP_DIR/Contents/Resources/transwarp-runner" "$APP_DIR/Contents/Resources/transwarp-runner"
check_distribution_signature "$APP_DIR/Contents/Resources/cloudflared" "$APP_DIR/Contents/Resources/cloudflared"

STAPLER_OUTPUT="$(xcrun stapler validate "$APP_DIR" 2>&1 || true)"
echo "$STAPLER_OUTPUT"
if ! printf "%s" "$STAPLER_OUTPUT" | grep -q "The validate action worked"; then
	if [ "$STRICT" = "1" ]; then
		fail "notarization ticket is not stapled or valid for $APP_DIR"
	fi
	warn "notarization ticket is not stapled or valid for $APP_DIR"
fi

SPCTL_OUTPUT="$(spctl -a -vv "$APP_DIR" 2>&1 || true)"
echo "$SPCTL_OUTPUT"
if printf "%s" "$SPCTL_OUTPUT" | grep -q "rejected"; then
	if [ "$STRICT" = "1" ]; then
		fail "Gatekeeper rejected $APP_DIR"
	fi
	warn "Gatekeeper rejected $APP_DIR; notarized Developer ID distribution is not proven"
fi

if [ "$STRICT" = "1" ]; then
	echo "$SPCTL_OUTPUT" | grep -q "accepted" || fail "Gatekeeper did not accept $APP_DIR"
fi

echo "release gate complete for $APP_DIR"
