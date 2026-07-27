#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SMOKE_ID="collector-smoke-$$"
EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID"
APP_PATH="$ROOT/.build/Transwarp-$SMOKE_ID.app"
ARCHIVE_PATH="$ROOT/.build/Transwarp-release-$SMOKE_ID.zip"
SELF_HOSTED_EVIDENCE="$EVIDENCE_DIR/self-hosted-mac.json"
AUDIT_JSON="$EVIDENCE_DIR/transwarp-audit.json"
AUDIT_STDERR="$EVIDENCE_DIR/transwarp-audit.stderr"
INVALID_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid"
INVALID_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid.stderr"
INVALID_NAMED_TUNNEL_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-named-tunnel"
INVALID_NAMED_TUNNEL_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-named-tunnel.stderr"
INVALID_NAMED_RECEIPT_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-named-receipt"
INVALID_NAMED_RECEIPT_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-named-receipt.stderr"
INVALID_NAMED_RECEIPT="$INVALID_NAMED_RECEIPT_EVIDENCE_DIR/named-tunnel-evidence.json"
INVALID_NAMED_RECEIPT_CI="$INVALID_NAMED_RECEIPT_EVIDENCE_DIR/ci-dispatch-evidence.json"
INVALID_NAMED_RECEIPT_CLEAN="$INVALID_NAMED_RECEIPT_EVIDENCE_DIR/clean-mac-evidence.json"
INVALID_CI_RECEIPT_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-ci-receipt"
INVALID_CI_RECEIPT_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-ci-receipt.stderr"
INVALID_CI_RECEIPT_NAMED="$INVALID_CI_RECEIPT_EVIDENCE_DIR/named-tunnel-evidence.json"
INVALID_CI_RECEIPT="$INVALID_CI_RECEIPT_EVIDENCE_DIR/ci-dispatch-evidence.json"
INVALID_CI_RECEIPT_CLEAN="$INVALID_CI_RECEIPT_EVIDENCE_DIR/clean-mac-evidence.json"
INVALID_CLEAN_RECEIPT_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-clean-receipt"
INVALID_CLEAN_RECEIPT_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-clean-receipt.stderr"
INVALID_CLEAN_RECEIPT_NAMED="$INVALID_CLEAN_RECEIPT_EVIDENCE_DIR/named-tunnel-evidence.json"
INVALID_CLEAN_RECEIPT_CI="$INVALID_CLEAN_RECEIPT_EVIDENCE_DIR/ci-dispatch-evidence.json"
INVALID_CLEAN_RECEIPT="$INVALID_CLEAN_RECEIPT_EVIDENCE_DIR/clean-mac-evidence.json"
INVALID_RUNNER_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-runner"
INVALID_RUNNER_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-runner.stderr"
INVALID_ACCEPTED_METADATA_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-accepted-metadata"
INVALID_ACCEPTED_METADATA_STDOUT="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-accepted-metadata.stdout"
INVALID_ACCEPTED_METADATA_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-accepted-metadata.stderr"
INVALID_ACCEPTED_METADATA_SMOKE="$ROOT/.build/release-evidence-$SMOKE_ID-invalid-accepted-metadata-smoke.sh"
MISSING_POLICY_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-policy"
MISSING_POLICY_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-policy.stderr"
MANIFEST_POLICY_APP="$ROOT/.build/Transwarp-$SMOKE_ID-manifest-policy.app"
MANIFEST_POLICY_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-manifest-policy"
MANIFEST_POLICY_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-manifest-policy.stderr"
MISSING_SIGNING_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-signing"
MISSING_SIGNING_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-signing.stderr"
MISSING_NOTARIZE_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-notarize"
MISSING_NOTARIZE_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-notarize.stderr"
MISSING_NOTARY_CREDENTIALS_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-notary-credentials"
MISSING_NOTARY_CREDENTIALS_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-notary-credentials.stderr"
MISSING_NAMED_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-named"
MISSING_NAMED_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-named.stderr"
MISSING_CI_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-ci"
MISSING_CI_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-ci.stderr"
MISSING_CI_NAMED_EVIDENCE="$MISSING_CI_EVIDENCE_DIR/named-tunnel-evidence.json"
MISSING_CLEAN_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-clean"
MISSING_CLEAN_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-clean.stderr"
MISSING_CLEAN_NAMED_EVIDENCE="$MISSING_CLEAN_EVIDENCE_DIR/named-tunnel-evidence.json"
MISSING_CLEAN_CI_EVIDENCE="$MISSING_CLEAN_EVIDENCE_DIR/ci-dispatch-evidence.json"
MISSING_CLEAN_INPUT_EVIDENCE_DIR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-clean-input"
MISSING_CLEAN_INPUT_STDERR="$ROOT/.build/release-evidence-$SMOKE_ID-missing-clean-input.stderr"
MISSING_CLEAN_INPUT_NAMED_EVIDENCE="$MISSING_CLEAN_INPUT_EVIDENCE_DIR/named-tunnel-evidence.json"
MISSING_CLEAN_INPUT_CI_EVIDENCE="$MISSING_CLEAN_INPUT_EVIDENCE_DIR/ci-dispatch-evidence.json"

cd "$ROOT"

cleanup() {
	rc=$?
	if [ "$rc" -eq 0 ]; then
		case "${TRANSWARP_KEEP_COLLECTOR_SMOKE_ARTIFACTS:-0}" in
			1|true|yes)
				;;
			*)
				rm -rf "$ROOT/.build/release-evidence-$SMOKE_ID"*
				rm -rf "$ROOT/.build/Transwarp-$SMOKE_ID"*.app*
				rm -rf "$ROOT/.build/Transwarp-release-$SMOKE_ID"*.zip
				;;
		esac
	fi
	exit "$rc"
}
trap cleanup EXIT INT TERM

write_receipt() {
	path="$1"
	kind="$2"
	mkdir -p "$(dirname "$path")"
	printf '{"kind":"%s","schema_version":1,"status":"pass"}\n' "$kind" > "$path"
}

write_named_tunnel_fixture() {
	path="$1"
	label="$2"
	app_path="${3:-$ROOT/.build/Transwarp.app}"
	dir="$(dirname "$path")"
	diagnose_log="$label-diagnose.log"
	dispatch_log="$label-dispatch.log"
	runner_log="$label-runner.log"
	results_json="$label-results.json"
	targets_registered_json="$label-targets-registered.json"
	targets_after_deregister_json="$label-targets-after-deregister.json"
	mkdir -p "$dir"
	cat > "$dir/$diagnose_log" <<'LOG'
[ok] target public_url=https://transwarp.example.com
[ok] selected runner health reachable through public_url
[ok] tunnel mode=named state=running connected=true ready=true
diagnosis passed
LOG
	cat > "$dir/$dispatch_log" <<'LOG'
[build] starting Echo Smoke
{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"machine-123","public_url":"https://transwarp.example.com"}
hello through named coordinator tunnel
[result] recorded passed
[result] request_id request-123
[result] build_id build-123
[result] job_id echo
[result] machine_id machine-123
[result] public_url https://transwarp.example.com
LOG
	cat > "$dir/$runner_log" <<'LOG'
[info] Started transwarp-runner
[tunnel] INF Registered tunnel connection
[tunnel] tunnel ready at https://transwarp.example.com
LOG
	app_log="$label-app.log"
	app_stderr="$label-app.err"
	cat > "$dir/$app_log" <<'LOG'
[info] Started transwarp-runner
[tunnel] INF Registered tunnel connection
[tunnel] tunnel ready at https://transwarp.example.com
LOG
	: > "$dir/$app_stderr"
	cat > "$dir/$targets_registered_json" <<'JSON'
[{"machine_id":"machine-123","public_url":"https://transwarp.example.com","accepting_builds":true,"jobs":["echo"]}]
JSON
	cat > "$dir/$targets_after_deregister_json" <<'JSON'
[]
JSON
	cat > "$dir/$results_json" <<'JSON'
[{"request_id":"request-123","build_id":"build-123","job_id":"echo","status":"passed"}]
JSON
	./scripts/package-app.sh "$app_path" >/dev/null
	go run ./cmd/transwarp-audit \
		-app "$app_path" \
		-write-named-tunnel-evidence "$path" \
		-named-tunnel-launch-mode app \
		-named-tunnel-public-url https://transwarp.example.com \
		-named-tunnel-machine-id machine-123 \
		-named-tunnel-job-id echo \
		-named-tunnel-request-id request-123 \
		-named-tunnel-diagnose-log "$dir/$diagnose_log" \
		-named-tunnel-dispatch-log "$dir/$dispatch_log" \
		-named-tunnel-runner-log "$dir/$runner_log" \
		-named-tunnel-app-log "$dir/$app_log" \
		-named-tunnel-app-stderr "$dir/$app_stderr" \
		-named-tunnel-targets-registered "$dir/$targets_registered_json" \
		-named-tunnel-targets-after-deregister "$dir/$targets_after_deregister_json" \
		-named-tunnel-results "$dir/$results_json"
}

write_ci_dispatch_fixture() {
	path="$1"
	label="$2"
	dir="$(dirname "$path")"
	source_log="$label-source.log"
	mkdir -p "$dir"
	cat > "$dir/$source_log" <<'LOG'
diagnosis passed
{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"machine-123","public_url":"https://transwarp.example.com"}
hello through named coordinator tunnel
[build] passed
[result] recorded passed
[result] request_id request-123
[result] build_id build-123
[result] job_id echo
[result] machine_id machine-123
[result] public_url https://transwarp.example.com
LOG
	cat > "$path" <<JSON
{
	"kind": "transwarp-ci-dispatch-evidence",
	"schema_version": 1,
	"generated_at": "2026-07-27T00:00:00+00:00",
	"status": "pass",
	"github_actions": true,
	"result_recorded": true,
	"run_id": "1234",
	"run_attempt": "1",
	"workflow": "Release Evidence",
	"job": "release-evidence",
	"repository": "charliewilco/transwarp",
	"sha": "0123456789abcdef0123456789abcdef01234567",
	"runner_os": "macOS",
	"runner_arch": "ARM64",
	"public_url": "https://transwarp.example.com",
	"build_id": "build-123",
	"job_id": "echo",
	"request_id": "request-123",
	"machine_id": "machine-123",
	"source_log": "$source_log"
}
JSON
}

if TRANSWARP_EVIDENCE_DIR="$INVALID_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=maybe \
	./scripts/collect-release-evidence.sh 2> "$INVALID_STDERR"; then
	echo "expected invalid boolean value to fail" >&2
	exit 1
fi

if ! grep -q "TRANSWARP_COLLECT_ALLOW_INCOMPLETE must be 1, true, yes, 0, false, or no" "$INVALID_STDERR"; then
	echo "invalid boolean failure did not explain accepted values" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$INVALID_NAMED_TUNNEL_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=sometimes \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1 \
	./scripts/collect-release-evidence.sh 2> "$INVALID_NAMED_TUNNEL_STDERR"; then
	echo "expected invalid named-tunnel collection value to fail" >&2
	exit 1
fi

if ! grep -q "TRANSWARP_COLLECT_NAMED_TUNNEL must be auto, 1, true, yes, 0, false, or no" "$INVALID_NAMED_TUNNEL_STDERR"; then
	echo "invalid named-tunnel collection failure did not explain accepted values" >&2
	exit 1
fi

mkdir -p "$INVALID_NAMED_RECEIPT_EVIDENCE_DIR"
printf '{"kind":"wrong","schema_version":1,"status":"pass"}\n' > "$INVALID_NAMED_RECEIPT"
write_receipt "$INVALID_NAMED_RECEIPT_CI" "transwarp-ci-dispatch-evidence"
write_receipt "$INVALID_NAMED_RECEIPT_CLEAN" "transwarp-clean-mac-evidence"
if TRANSWARP_EVIDENCE_DIR="$INVALID_NAMED_RECEIPT_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$INVALID_NAMED_RECEIPT" \
	TRANSWARP_CI_DISPATCH_EVIDENCE="$INVALID_NAMED_RECEIPT_CI" \
	TRANSWARP_CLEAN_MAC_EVIDENCE="$INVALID_NAMED_RECEIPT_CLEAN" \
	./scripts/collect-release-evidence.sh 2> "$INVALID_NAMED_RECEIPT_STDERR"; then
	echo "expected invalid named-tunnel receipt to fail" >&2
	exit 1
fi

if ! grep -q "named-tunnel evidence invalid: overall=missing" "$INVALID_NAMED_RECEIPT_STDERR" || ! grep -q "Named Cloudflare Tunnel smoke evidence incomplete" "$INVALID_NAMED_RECEIPT_STDERR"; then
	echo "invalid named-tunnel receipt failure did not explain receipt shape problem" >&2
	exit 1
fi

write_named_tunnel_fixture "$INVALID_CI_RECEIPT_NAMED" "invalid-ci"
printf '{"kind":"transwarp-ci-dispatch-evidence","schema_version":1,"status":"fail"}\n' > "$INVALID_CI_RECEIPT"
write_receipt "$INVALID_CI_RECEIPT_CLEAN" "transwarp-clean-mac-evidence"
if TRANSWARP_EVIDENCE_DIR="$INVALID_CI_RECEIPT_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$INVALID_CI_RECEIPT_NAMED" \
	TRANSWARP_CI_DISPATCH_EVIDENCE="$INVALID_CI_RECEIPT" \
	TRANSWARP_CLEAN_MAC_EVIDENCE="$INVALID_CI_RECEIPT_CLEAN" \
	./scripts/collect-release-evidence.sh 2> "$INVALID_CI_RECEIPT_STDERR"; then
	echo "expected invalid CI dispatch receipt to fail" >&2
	exit 1
fi

if ! grep -q "CI dispatch evidence invalid: overall=missing" "$INVALID_CI_RECEIPT_STDERR" || ! grep -q "CI dispatch evidence incomplete" "$INVALID_CI_RECEIPT_STDERR"; then
	echo "invalid CI dispatch receipt failure did not explain receipt shape problem" >&2
	exit 1
fi

write_named_tunnel_fixture "$INVALID_CLEAN_RECEIPT_NAMED" "invalid-clean" "$APP_PATH-invalid-clean"
write_ci_dispatch_fixture "$INVALID_CLEAN_RECEIPT_CI" "invalid-clean"
printf '{"kind":"transwarp-clean-mac-evidence","schema_version":1,"status":"pass"}\n' > "$INVALID_CLEAN_RECEIPT"
if TRANSWARP_EVIDENCE_DIR="$INVALID_CLEAN_RECEIPT_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1 \
	TRANSWARP_NOTARIZE_REQUESTED=0 \
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$INVALID_CLEAN_RECEIPT_NAMED" \
	TRANSWARP_CI_DISPATCH_EVIDENCE="$INVALID_CLEAN_RECEIPT_CI" \
	TRANSWARP_APP_PATH="$APP_PATH-invalid-clean" \
	TRANSWARP_CLEAN_MAC_EVIDENCE="$INVALID_CLEAN_RECEIPT" \
	./scripts/collect-release-evidence.sh > "$INVALID_CLEAN_RECEIPT_EVIDENCE_DIR/stdout.log" 2> "$INVALID_CLEAN_RECEIPT_STDERR"; then
	echo "expected invalid clean-Mac receipt to fail" >&2
	exit 1
fi

if ! grep -q "clean-Mac evidence invalid: overall=missing" "$INVALID_CLEAN_RECEIPT_STDERR" || ! grep -q "Clean-Mac validation evidence incomplete" "$INVALID_CLEAN_RECEIPT_STDERR"; then
	echo "invalid clean-Mac receipt failure did not explain receipt shape problem" >&2
	exit 1
fi

if grep -q "checking packaged app launch" "$INVALID_CLEAN_RECEIPT_EVIDENCE_DIR/stdout.log"; then
	echo "invalid clean-Mac receipt should fail before app launch smoke" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$MISSING_POLICY_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	./scripts/collect-release-evidence.sh 2> "$MISSING_POLICY_STDERR"; then
	echo "expected missing cloudflared version policy to fail" >&2
	exit 1
fi

if ! grep -q "TRANSWARP_EXPECTED_CLOUDFLARED_VERSION is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1" "$MISSING_POLICY_STDERR"; then
	echo "missing cloudflared version policy failure did not explain strict release requirement" >&2
	exit 1
fi

mkdir -p "$MANIFEST_POLICY_APP/Contents/Resources"
cat > "$MANIFEST_POLICY_APP/Contents/Resources/TranswarpManifest.json" <<'JSON'
{
	"expected_cloudflared_version": "cloudflared version smoke"
}
JSON

if TRANSWARP_EVIDENCE_DIR="$MANIFEST_POLICY_EVIDENCE_DIR" \
	TRANSWARP_APP_PATH="$MANIFEST_POLICY_APP" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	./scripts/collect-release-evidence.sh 2> "$MANIFEST_POLICY_STDERR"; then
	echo "expected missing named-tunnel evidence to fail after manifest cloudflared policy" >&2
	exit 1
fi

if ! grep -q "named-tunnel evidence is required" "$MANIFEST_POLICY_STDERR"; then
	echo "manifest cloudflared policy did not advance to strict evidence validation" >&2
	exit 1
fi

if grep -q "TRANSWARP_EXPECTED_CLOUDFLARED_VERSION is required" "$MANIFEST_POLICY_STDERR"; then
	echo "manifest cloudflared policy was not accepted by strict release collection" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$MISSING_SIGNING_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	./scripts/collect-release-evidence.sh 2> "$MISSING_SIGNING_STDERR"; then
	echo "expected missing signing identity to fail" >&2
	exit 1
fi

if ! grep -q "SIGN_IDENTITY is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1" "$MISSING_SIGNING_STDERR"; then
	echo "missing signing failure did not explain strict release requirement" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$MISSING_NOTARIZE_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	./scripts/collect-release-evidence.sh 2> "$MISSING_NOTARIZE_STDERR"; then
	echo "expected missing notarization request to fail" >&2
	exit 1
fi

if ! grep -q "TRANSWARP_NOTARIZE_REQUESTED=1 is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1" "$MISSING_NOTARIZE_STDERR"; then
	echo "missing notarization request failure did not explain strict release requirement" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$MISSING_NOTARY_CREDENTIALS_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	./scripts/collect-release-evidence.sh 2> "$MISSING_NOTARY_CREDENTIALS_STDERR"; then
	echo "expected missing notarization credentials to fail" >&2
	exit 1
fi

if ! grep -q "APPLE_KEYCHAIN_PROFILE is required for strict release notarization" "$MISSING_NOTARY_CREDENTIALS_STDERR"; then
	echo "missing notarization credentials failure did not explain strict release requirement" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$MISSING_NAMED_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	./scripts/collect-release-evidence.sh 2> "$MISSING_NAMED_STDERR"; then
	echo "expected missing named-tunnel evidence to fail" >&2
	exit 1
fi

if ! grep -q "named-tunnel evidence is required" "$MISSING_NAMED_STDERR"; then
	echo "missing named-tunnel failure did not explain strict evidence requirement" >&2
	exit 1
fi

mkdir -p "$(dirname "$MISSING_CI_NAMED_EVIDENCE")"
printf '{}\n' > "$MISSING_CI_NAMED_EVIDENCE"
if TRANSWARP_EVIDENCE_DIR="$MISSING_CI_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$MISSING_CI_NAMED_EVIDENCE" \
	./scripts/collect-release-evidence.sh 2> "$MISSING_CI_STDERR"; then
	echo "expected missing CI dispatch evidence to fail" >&2
	exit 1
fi

if ! grep -q "CI dispatch evidence is required" "$MISSING_CI_STDERR"; then
	echo "missing CI dispatch failure did not explain strict evidence requirement" >&2
	exit 1
fi

mkdir -p "$MISSING_CLEAN_EVIDENCE_DIR"
write_named_tunnel_fixture "$MISSING_CLEAN_NAMED_EVIDENCE" "missing-clean"
write_ci_dispatch_fixture "$MISSING_CLEAN_CI_EVIDENCE" "missing-clean"
if TRANSWARP_EVIDENCE_DIR="$MISSING_CLEAN_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$MISSING_CLEAN_NAMED_EVIDENCE" \
	TRANSWARP_CI_DISPATCH_EVIDENCE="$MISSING_CLEAN_CI_EVIDENCE" \
	TRANSWARP_CLEAN_MAC_EVIDENCE="$MISSING_CLEAN_EVIDENCE_DIR/missing-clean-mac-evidence.json" \
	./scripts/collect-release-evidence.sh 2> "$MISSING_CLEAN_STDERR"; then
	echo "expected missing clean-Mac evidence file to fail" >&2
	exit 1
fi

if ! grep -q "clean-Mac evidence file does not exist" "$MISSING_CLEAN_STDERR"; then
	echo "missing clean-Mac failure did not explain evidence path problem" >&2
	exit 1
fi

mkdir -p "$MISSING_CLEAN_INPUT_EVIDENCE_DIR"
write_named_tunnel_fixture "$MISSING_CLEAN_INPUT_NAMED_EVIDENCE" "missing-clean-input"
write_ci_dispatch_fixture "$MISSING_CLEAN_INPUT_CI_EVIDENCE" "missing-clean-input"
if TRANSWARP_EVIDENCE_DIR="$MISSING_CLEAN_INPUT_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	TRANSWARP_NAMED_TUNNEL_EVIDENCE="$MISSING_CLEAN_INPUT_NAMED_EVIDENCE" \
	TRANSWARP_CI_DISPATCH_EVIDENCE="$MISSING_CLEAN_INPUT_CI_EVIDENCE" \
	./scripts/collect-release-evidence.sh 2> "$MISSING_CLEAN_INPUT_STDERR"; then
	echo "expected missing clean-Mac evidence input to fail" >&2
	exit 1
fi

if ! grep -q "clean-Mac evidence is required" "$MISSING_CLEAN_INPUT_STDERR"; then
	echo "missing clean-Mac input failure did not explain strict evidence requirement" >&2
	exit 1
fi

if TRANSWARP_EVIDENCE_DIR="$INVALID_RUNNER_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=1 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=0 \
	TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN=fake-token \
	TRANSWARP_PUBLIC_URL=https://transwarp.example.com \
	TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version smoke" \
	SIGN_IDENTITY="Developer ID Application: Smoke (TEAMID)" \
	TRANSWARP_NOTARIZE_REQUESTED=1 \
	APPLE_KEYCHAIN_PROFILE=smoke-profile \
	GITHUB_ACTIONS=true \
	GITHUB_RUN_ID=1234 \
	GITHUB_RUN_ATTEMPT=1 \
	GITHUB_WORKFLOW="Release Evidence" \
	GITHUB_JOB=release-evidence \
	GITHUB_REPOSITORY=charliewilco/transwarp \
	GITHUB_SHA=0123456789abcdef0123456789abcdef01234567 \
	RUNNER_OS= \
	RUNNER_ARCH=X64 \
	./scripts/collect-release-evidence.sh 2> "$INVALID_RUNNER_STDERR"; then
	echo "expected missing release runner context to fail" >&2
	exit 1
fi

if ! grep -q "GitHub Actions context incomplete: runner_os" "$INVALID_RUNNER_STDERR"; then
	echo "missing release runner failure did not explain RUNNER_OS requirement" >&2
	exit 1
fi

cat > "$INVALID_ACCEPTED_METADATA_SMOKE" <<'SH'
#!/bin/sh
set -eu
evidence_dir="$(dirname "$TRANSWARP_NAMED_TUNNEL_EVIDENCE")"
cat > "$evidence_dir/diagnose.log" <<'LOG'
[ok] target public_url=https://transwarp.example.com
[ok] selected runner health reachable through public_url
[ok] tunnel mode=named state=running connected=true ready=true
diagnosis passed
LOG
cat > "$evidence_dir/dispatch.log" <<'LOG'
[build] starting Echo Smoke
{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"machine-123","public_url":"https://transwarp.example.com"}
hello through named coordinator tunnel
[result] recorded passed
[result] request_id request-123
[result] build_id build-123
[result] job_id echo
[result] machine_id machine-123
[result] public_url https://transwarp.example.com
LOG
cat > "$evidence_dir/runner.log" <<'LOG'
[info] Started transwarp-runner
[tunnel] INF Registered tunnel connection
[tunnel] tunnel ready at https://transwarp.example.com
LOG
cat > "$evidence_dir/app.log" <<'LOG'
[info] Started transwarp-runner
[tunnel] INF Registered tunnel connection
[tunnel] tunnel ready at https://transwarp.example.com
LOG
: > "$evidence_dir/app.err"
cat > "$evidence_dir/targets-registered.json" <<'JSON'
[{"machine_id":"machine-123","public_url":"https://transwarp.example.com","accepting_builds":true,"jobs":["echo"]}]
JSON
cat > "$evidence_dir/targets-after-deregister.json" <<'JSON'
[]
JSON
cat > "$evidence_dir/results.json" <<'JSON'
[{"request_id":"request-123","build_id":"build-123","job_id":"echo","status":"passed"}]
JSON
go run ./cmd/transwarp-audit \
	-app "$TRANSWARP_NAMED_TUNNEL_APP_PATH" \
	-write-named-tunnel-evidence "$TRANSWARP_NAMED_TUNNEL_EVIDENCE" \
	-named-tunnel-launch-mode app \
	-named-tunnel-public-url https://transwarp.example.com \
	-named-tunnel-machine-id machine-123 \
	-named-tunnel-job-id echo \
	-named-tunnel-request-id request-123 \
	-named-tunnel-diagnose-log "$evidence_dir/diagnose.log" \
	-named-tunnel-dispatch-log "$evidence_dir/dispatch.log" \
	-named-tunnel-runner-log "$evidence_dir/runner.log" \
	-named-tunnel-app-log "$evidence_dir/app.log" \
	-named-tunnel-app-stderr "$evidence_dir/app.err" \
	-named-tunnel-targets-registered "$evidence_dir/targets-registered.json" \
	-named-tunnel-targets-after-deregister "$evidence_dir/targets-after-deregister.json" \
	-named-tunnel-results "$evidence_dir/results.json"
cat "$evidence_dir/diagnose.log"
printf '%s\n' '{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"other-machine","public_url":"https://transwarp.example.com"}'
printf '%s\n' 'hello through named coordinator tunnel'
printf '%s\n' '[result] recorded passed'
SH
chmod 755 "$INVALID_ACCEPTED_METADATA_SMOKE"

if TRANSWARP_EVIDENCE_DIR="$INVALID_ACCEPTED_METADATA_EVIDENCE_DIR" \
	TRANSWARP_COLLECT_NAMED_TUNNEL=1 \
	TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1 \
	TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN=fake-token \
	TRANSWARP_PUBLIC_URL=https://transwarp.example.com \
	SIGN_IDENTITY=- \
	TRANSWARP_NOTARIZE_REQUESTED=0 \
	GITHUB_ACTIONS=true \
	GITHUB_RUN_ID=1234 \
	GITHUB_RUN_ATTEMPT=1 \
	GITHUB_WORKFLOW="Release Evidence" \
	GITHUB_JOB=release-evidence \
	GITHUB_REPOSITORY=charliewilco/transwarp \
	GITHUB_SHA=0123456789abcdef0123456789abcdef01234567 \
	RUNNER_OS=macOS \
	RUNNER_ARCH=ARM64 \
	TRANSWARP_NAMED_TUNNEL_SMOKE_SCRIPT="$INVALID_ACCEPTED_METADATA_SMOKE" \
	./scripts/collect-release-evidence.sh > "$INVALID_ACCEPTED_METADATA_STDOUT" 2> "$INVALID_ACCEPTED_METADATA_STDERR"; then
	echo "expected mismatched accepted-build metadata to fail" >&2
	exit 1
fi

if ! grep -q "CI dispatch source log did not include matching accepted runner build metadata" "$INVALID_ACCEPTED_METADATA_STDERR"; then
	echo "invalid accepted-build metadata failure did not explain source log mismatch" >&2
	exit 1
fi

TRANSWARP_EVIDENCE_DIR="$EVIDENCE_DIR" \
TRANSWARP_APP_PATH="$APP_PATH" \
TRANSWARP_RELEASE_ARCHIVE="$ARCHIVE_PATH" \
TRANSWARP_SELF_HOSTED_EVIDENCE="$SELF_HOSTED_EVIDENCE" \
TRANSWARP_COLLECT_NAMED_TUNNEL=0 \
TRANSWARP_COLLECT_ALLOW_INCOMPLETE=true \
TRANSWARP_NOTARIZE_REQUESTED=false \
./scripts/collect-release-evidence.sh

go run ./cmd/transwarp-audit \
	-check-release-evidence-collector-smoke \
	-release-collector-audit "$AUDIT_JSON" \
	-release-collector-audit-stderr "$AUDIT_STDERR" \
	-release-collector-self-hosted-evidence "$SELF_HOSTED_EVIDENCE" \
	-release-collector-archive "$ARCHIVE_PATH"
