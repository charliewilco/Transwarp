#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/transwarp-notarize-policy.XXXXXX")"
FAKEBIN="$TMPDIR/bin"
APP_DIR="$TMPDIR/Transwarp.app"
ARCHIVE="$TMPDIR/Transwarp-notarization.zip"
FAIL_STDERR="$TMPDIR/password-only.stderr"
PASS_STDOUT="$TMPDIR/profile.stdout"
PASS_STDERR="$TMPDIR/profile.stderr"
NOTARY_ARGS="$TMPDIR/notarytool-args.txt"

cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$FAKEBIN" "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
: > "$APP_DIR/Contents/MacOS/Transwarp"
: > "$APP_DIR/Contents/Resources/transwarp-runner"
: > "$APP_DIR/Contents/Resources/cloudflared"

cat > "$FAKEBIN/codesign" <<'SH'
#!/bin/sh
if [ "${1:-}" = "-dv" ]; then
	echo "Executable=$4"
	echo "flags=runtime"
	echo "TeamIdentifier=SMOKEID"
	exit 0
fi
last=""
for arg in "$@"; do
	last="$arg"
done
echo "$last: valid on disk"
echo "$last: satisfies its Designated Requirement"
exit 0
SH

cat > "$FAKEBIN/ditto" <<'SH'
#!/bin/sh
last=""
for arg in "$@"; do
	last="$arg"
done
printf 'notarization archive\n' > "$last"
exit 0
SH

cat > "$FAKEBIN/xcrun" <<'SH'
#!/bin/sh
if [ "${1:-}" = "notarytool" ] && [ "${2:-}" = "submit" ]; then
	printf '%s\n' "$*" > "$TRANSWARP_FAKE_NOTARY_ARGS"
	case " $* " in
		*" --keychain-profile smoke-profile "*)
			;;
		*)
			echo "notarytool submit did not receive expected keychain profile" >&2
			exit 1
			;;
	esac
	case " $* " in
		*" --password "*)
			echo "notarytool submit must not receive --password" >&2
			exit 1
			;;
	esac
	exit 0
fi
if [ "${1:-}" = "stapler" ] && [ "${2:-}" = "staple" ]; then
	exit 0
fi
if [ "${1:-}" = "stapler" ] && [ "${2:-}" = "validate" ]; then
	echo "The validate action worked!"
	exit 0
fi
echo "unexpected xcrun invocation: $*" >&2
exit 1
SH

cat > "$FAKEBIN/spctl" <<'SH'
#!/bin/sh
echo "$*: accepted"
exit 0
SH

chmod 755 "$FAKEBIN/codesign" "$FAKEBIN/ditto" "$FAKEBIN/xcrun" "$FAKEBIN/spctl"

if PATH="$FAKEBIN:$PATH" \
	TRANSWARP_NOTARIZATION_ARCHIVE="$ARCHIVE" \
	APPLE_ID=apple@example.com \
	APPLE_TEAM_ID=SMOKEID \
	APPLE_APP_SPECIFIC_PASSWORD=notary-password \
	"$ROOT/scripts/notarize-app.sh" "$APP_DIR" 2> "$FAIL_STDERR"; then
	echo "expected password-only notarization to fail" >&2
	exit 1
fi

if ! grep -q "set APPLE_KEYCHAIN_PROFILE" "$FAIL_STDERR"; then
	echo "password-only notarization failure did not explain Keychain profile requirement" >&2
	exit 1
fi

if [ -f "$ARCHIVE" ]; then
	echo "password-only notarization should fail before writing an archive" >&2
	exit 1
fi

PATH="$FAKEBIN:$PATH" \
TRANSWARP_FAKE_NOTARY_ARGS="$NOTARY_ARGS" \
TRANSWARP_NOTARIZATION_ARCHIVE="$ARCHIVE" \
APPLE_KEYCHAIN_PROFILE=smoke-profile \
"$ROOT/scripts/notarize-app.sh" "$APP_DIR" > "$PASS_STDOUT" 2> "$PASS_STDERR" || {
	echo "profile-based notarization smoke failed" >&2
	sed -n '1,160p' "$PASS_STDOUT" >&2 || true
	sed -n '1,160p' "$PASS_STDERR" >&2 || true
	exit 1
}

if [ ! -f "$NOTARY_ARGS" ]; then
	echo "notarytool submit was not invoked" >&2
	exit 1
fi
if ! grep -q -- "--keychain-profile smoke-profile" "$NOTARY_ARGS"; then
	echo "notarytool submit did not record the expected Keychain profile:" >&2
	cat "$NOTARY_ARGS" >&2
	exit 1
fi
if grep -q -- "--password" "$NOTARY_ARGS"; then
	echo "notarytool received --password unexpectedly" >&2
	exit 1
fi

echo "notarize app policy smoke passed"
