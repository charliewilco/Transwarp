#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIGURATION="${CONFIGURATION:-release}"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
CONTENTS="$APP_DIR/Contents"
MACOS="$CONTENTS/MacOS"
RESOURCES="$CONTENTS/Resources"
MANIFEST="$RESOURCES/TranswarpManifest.json"
APP_VERSION="${TRANSWARP_VERSION:-0.1.0}"
BUILD_NUMBER="${TRANSWARP_BUILD_NUMBER:-1}"
EXPECTED_CLOUDFLARED_VERSION="${TRANSWARP_EXPECTED_CLOUDFLARED_VERSION:-}"
ALLOW_MISSING_CLOUDFLARED="${TRANSWARP_ALLOW_MISSING_CLOUDFLARED:-0}"

fail() {
	echo "package app failed: $*" >&2
	exit 1
}

case "$APP_VERSION" in
	""|*[!0-9A-Za-z.-]*)
		fail "TRANSWARP_VERSION may only contain letters, numbers, dots, and hyphens"
		;;
esac

case "$BUILD_NUMBER" in
	""|*[!0-9A-Za-z.-]*)
		fail "TRANSWARP_BUILD_NUMBER may only contain letters, numbers, dots, and hyphens"
		;;
esac

CLOUDFLARED_SOURCE="${CLOUDFLARED_PATH:-}"
if [ -z "$CLOUDFLARED_SOURCE" ]; then
	CLOUDFLARED_SOURCE="$(command -v cloudflared || true)"
fi
if [ -z "$CLOUDFLARED_SOURCE" ] || [ ! -x "$CLOUDFLARED_SOURCE" ]; then
	case "$ALLOW_MISSING_CLOUDFLARED" in
		1|true|yes)
			echo "warning: cloudflared not bundled; development package cannot open Cloudflare Tunnel" >&2
			CLOUDFLARED_SOURCE=""
			;;
		*)
			fail "cloudflared is required for standalone packaging; install it, set CLOUDFLARED_PATH, or set TRANSWARP_ALLOW_MISSING_CLOUDFLARED=1 for a non-tunnel development bundle"
			;;
	esac
fi

cd "$ROOT"

GOOS=darwin GOARCH=arm64 go build -o "$ROOT/.build/transwarp-runner/transwarp-runner" ./cmd/transwarp-runner
swift build -c "$CONFIGURATION" --arch arm64
BIN_PATH="$(swift build -c "$CONFIGURATION" --arch arm64 --show-bin-path)"

rm -rf "$APP_DIR"
mkdir -p "$MACOS" "$RESOURCES"

cp "$BIN_PATH/Transwarp" "$MACOS/Transwarp"
cp "$ROOT/.build/transwarp-runner/transwarp-runner" "$RESOURCES/transwarp-runner"

if [ -n "$CLOUDFLARED_SOURCE" ]; then
	cp "$CLOUDFLARED_SOURCE" "$RESOURCES/cloudflared"
	chmod 755 "$RESOURCES/cloudflared"
fi

cat > "$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>Transwarp</string>
	<key>CFBundleIdentifier</key>
	<string>co.charliewil.transwarp</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>Transwarp</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$APP_VERSION</string>
	<key>CFBundleVersion</key>
	<string>$BUILD_NUMBER</string>
	<key>LSMinimumSystemVersion</key>
	<string>14.0</string>
	<key>NSPrincipalClass</key>
	<string>NSApplication</string>
</dict>
</plist>
PLIST

SIGN_IDENTITY="${SIGN_IDENTITY:--}"
if [ "${CODESIGN:-1}" = "1" ]; then
	CODESIGN_FLAGS="${CODESIGN_FLAGS:-}"
	if [ "$SIGN_IDENTITY" != "-" ] && [ -z "$CODESIGN_FLAGS" ]; then
		CODESIGN_FLAGS="--options runtime --timestamp"
	fi

	codesign --force --sign "$SIGN_IDENTITY" $CODESIGN_FLAGS "$RESOURCES/transwarp-runner"
	if [ -x "$RESOURCES/cloudflared" ]; then
		codesign --force --sign "$SIGN_IDENTITY" $CODESIGN_FLAGS "$RESOURCES/cloudflared"
	fi
	codesign --force --sign "$SIGN_IDENTITY" $CODESIGN_FLAGS "$MACOS/Transwarp"
fi

json_escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

sha256() {
	shasum -a 256 "$1" | awk '{print $1}'
}

CLOUDFLARED_VERSION=""
CLOUDFLARED_SHA256=""
if [ -x "$RESOURCES/cloudflared" ]; then
	CLOUDFLARED_VERSION="$("$RESOURCES/cloudflared" --version 2>/dev/null || true)"
	CLOUDFLARED_SHA256="$(sha256 "$RESOURCES/cloudflared")"
fi

cat > "$MANIFEST" <<JSON
{
  "schema_version": 1,
  "bundle_identifier": "co.charliewil.transwarp",
  "app_version": "$(json_escape "$APP_VERSION")",
  "build_number": "$(json_escape "$BUILD_NUMBER")",
  "minimum_system_version": "14.0",
  "architecture": "arm64",
  "runner_sha256": "$(sha256 "$RESOURCES/transwarp-runner")",
  "cloudflared_sha256": "$CLOUDFLARED_SHA256",
  "cloudflared_version": "$(json_escape "$CLOUDFLARED_VERSION")",
  "expected_cloudflared_version": "$(json_escape "$EXPECTED_CLOUDFLARED_VERSION")"
}
JSON

if [ "${CODESIGN:-1}" = "1" ]; then
	codesign --force --sign "$SIGN_IDENTITY" $CODESIGN_FLAGS "$APP_DIR"
fi

echo "$APP_DIR"
