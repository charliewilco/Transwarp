#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
ARCHIVE="${TRANSWARP_NOTARIZATION_ARCHIVE:-$ROOT/.build/Transwarp-notarization.zip}"

fail() {
	echo "notarization failed: $*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

require_tool codesign
require_tool ditto
require_tool spctl
require_tool xcrun

[ -d "$APP_DIR" ] || fail "$APP_DIR does not exist; run scripts/package-app.sh first"

if [ -z "${APPLE_KEYCHAIN_PROFILE:-}" ]; then
	fail "set APPLE_KEYCHAIN_PROFILE; create it with xcrun notarytool store-credentials so app-specific passwords are not passed as process arguments"
fi

check_distribution_signature() {
	path="$1"
	[ -e "$path" ] || fail "$path is missing"
	signing_output="$(codesign -dv --verbose=4 "$path" 2>&1 || true)"
	if printf '%s\n' "$signing_output" | grep -q 'Signature=adhoc'; then
		fail "$path is ad-hoc signed; rebuild with SIGN_IDENTITY"
	fi
	if ! printf '%s\n' "$signing_output" | grep -q 'flags=.*runtime'; then
		fail "$path is missing hardened runtime; rebuild with Developer ID signing"
	fi
	if ! printf '%s\n' "$signing_output" | grep -q 'TeamIdentifier='; then
		fail "$path is missing a TeamIdentifier"
	fi
	if printf '%s\n' "$signing_output" | grep -q 'TeamIdentifier=not set'; then
		fail "$path is missing a Developer ID team identifier"
	fi
}

check_distribution_signature "$APP_DIR"
check_distribution_signature "$APP_DIR/Contents/MacOS/Transwarp"
check_distribution_signature "$APP_DIR/Contents/Resources/transwarp-runner"
check_distribution_signature "$APP_DIR/Contents/Resources/cloudflared"

codesign --verify --deep --strict --verbose=2 "$APP_DIR"

rm -f "$ARCHIVE"
ditto -c -k --keepParent "$APP_DIR" "$ARCHIVE"

xcrun notarytool submit "$ARCHIVE" \
	--keychain-profile "$APPLE_KEYCHAIN_PROFILE" \
	--wait

xcrun stapler staple "$APP_DIR"
xcrun stapler validate "$APP_DIR"
spctl -a -vv --type execute "$APP_DIR"

echo "notarization complete for $APP_DIR"
