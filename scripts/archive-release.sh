#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
ARCHIVE="${TRANSWARP_RELEASE_ARCHIVE:-$ROOT/.build/Transwarp-release.zip}"
STAGING="$ROOT/.build/release-archive"
RELEASE_ROOT="$STAGING/TranswarpRelease"
VALIDATION_DIR="$RELEASE_ROOT/Validation"
AUDIT_BIN="$ROOT/.build/transwarp-audit/transwarp-audit"
CONFIG_BIN="$ROOT/.build/transwarp-config/transwarp-config"

fail() {
	echo "release archive failed: $*" >&2
	exit 1
}

case "$APP_DIR" in
	/*) ;;
	*) APP_DIR="$(pwd)/$APP_DIR" ;;
esac
APP_DIR="$(cd "$(dirname "$APP_DIR")" && pwd)/$(basename "$APP_DIR")"

case "$ARCHIVE" in
	/*) ;;
	*) ARCHIVE="$(pwd)/$ARCHIVE" ;;
esac

[ -d "$APP_DIR" ] || fail "$APP_DIR does not exist; run scripts/package-app.sh first"
[ -x "$APP_DIR/Contents/MacOS/Transwarp" ] || fail "$APP_DIR is missing the Transwarp executable"
[ -x "$APP_DIR/Contents/Resources/cloudflared" ] || fail "$APP_DIR is missing bundled cloudflared"
[ -x "$ROOT/scripts/clean-mac-validate.sh" ] || fail "scripts/clean-mac-validate.sh is missing or not executable"
[ -x "$ROOT/scripts/validate-clean-mac-status.sh" ] || fail "scripts/validate-clean-mac-status.sh is missing or not executable"

rm -rf "$STAGING"
mkdir -p "$VALIDATION_DIR" "$(dirname "$ARCHIVE")"

GOOS=darwin GOARCH=arm64 go build -o "$AUDIT_BIN" ./cmd/transwarp-audit
GOOS=darwin GOARCH=arm64 go build -o "$CONFIG_BIN" ./cmd/transwarp-config

ditto "$APP_DIR" "$RELEASE_ROOT/Transwarp.app"
cp "$ROOT/scripts/clean-mac-validate.sh" "$VALIDATION_DIR/clean-mac-validate.sh"
cp "$ROOT/scripts/validate-clean-mac-status.sh" "$VALIDATION_DIR/validate-clean-mac-status.sh"
cp "$AUDIT_BIN" "$VALIDATION_DIR/transwarp-audit"
cp "$CONFIG_BIN" "$VALIDATION_DIR/transwarp-config"
chmod 755 "$VALIDATION_DIR/clean-mac-validate.sh"
chmod 755 "$VALIDATION_DIR/validate-clean-mac-status.sh"
chmod 755 "$VALIDATION_DIR/transwarp-audit"
chmod 755 "$VALIDATION_DIR/transwarp-config"

cat > "$VALIDATION_DIR/README.txt" <<'TEXT'
Transwarp clean-Mac validation

Run this on a separate clean Apple Silicon Mac after unpacking the archive:

  TRANSWARP_CLEAN_MAC_EVIDENCE=clean-mac-evidence.json \
    ./Validation/clean-mac-validate.sh ./Transwarp.app | tee clean-mac.log

Attach clean-mac-evidence.json to transwarp-audit with -clean-mac-evidence.
The receipt proves strict signing, stapled notarization, Gatekeeper acceptance,
first launch of the authenticated loopback runner, and a passed local build
request through that app-spawned runner.
TEXT

(
	cd "$RELEASE_ROOT"
	shasum -a 256 \
		Transwarp.app/Contents/MacOS/Transwarp \
		Transwarp.app/Contents/Resources/transwarp-runner \
		Transwarp.app/Contents/Resources/cloudflared \
		Transwarp.app/Contents/Resources/TranswarpManifest.json \
		Validation/clean-mac-validate.sh \
		Validation/validate-clean-mac-status.sh \
		Validation/transwarp-audit \
		Validation/transwarp-config \
		Validation/README.txt \
		> Validation/SHA256SUMS
)

rm -f "$ARCHIVE"
(
	cd "$STAGING"
	ditto -c -k --sequesterRsrc --keepParent TranswarpRelease "$ARCHIVE"
)

unzip -l "$ARCHIVE" >/dev/null
echo "$ARCHIVE"
