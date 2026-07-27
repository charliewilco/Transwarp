#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
CHECK_EVIDENCE_DIR="$ROOT/.build/check-evidence"
SELF_HOSTED_EVIDENCE="$CHECK_EVIDENCE_DIR/self-hosted-mac.json"
APP_LAUNCH_EVIDENCE="$CHECK_EVIDENCE_DIR/app-launch-evidence.json"

gofmt -w cmd internal
./scripts/check-github-actions.sh
./scripts/check-temp-permissions.sh
./scripts/smoke-github-action.sh
go test ./...
TRANSWARP_SELF_HOSTED_EVIDENCE="$SELF_HOSTED_EVIDENCE" ./scripts/check-self-hosted-mac.sh >/dev/null
./scripts/smoke-diagnose.sh
./scripts/smoke-direct-build.sh
./scripts/smoke-coordinator.sh
./scripts/build-runner.sh
GOOS=linux GOARCH=amd64 go build -o /tmp/transwarp-dispatch-linux-amd64 ./cmd/transwarp-dispatch
GOOS=linux GOARCH=amd64 go build -o /tmp/transwarp-coordinator-linux-amd64 ./cmd/transwarp-coordinator
GOOS=linux GOARCH=amd64 go build -o /tmp/transwarp-diagnose-linux-amd64 ./cmd/transwarp-diagnose
GOOS=linux GOARCH=amd64 go build -o /tmp/transwarp-audit-linux-amd64 ./cmd/transwarp-audit
swift test
swift build
for script in scripts/*.sh script/*.sh; do
	sh -n "$script"
done
./scripts/smoke-package-app.sh
./scripts/smoke-clean-mac-status-validation.sh
./scripts/package-app.sh >/dev/null
./scripts/release-gate.sh >/dev/null
./scripts/smoke-release-gate.sh
./scripts/smoke-notarize-app-policy.sh
./scripts/smoke-clean-mac-validate.sh .build/Transwarp.app
./scripts/archive-release.sh >/dev/null
./scripts/smoke-release-evidence-collector.sh >/dev/null
unzip -l .build/Transwarp-release.zip | grep -q "TranswarpRelease/Validation/clean-mac-validate.sh"
unzip -l .build/Transwarp-release.zip | grep -q "TranswarpRelease/Validation/validate-clean-mac-status.sh"
unzip -l .build/Transwarp-release.zip | grep -q "TranswarpRelease/Validation/transwarp-config"
unzip -l .build/Transwarp-release.zip | grep -q "TranswarpRelease/Transwarp.app/Contents/MacOS/Transwarp"
codesign --verify --deep --strict --verbose=2 .build/Transwarp.app
TRANSWARP_APP_LAUNCH_EVIDENCE="$APP_LAUNCH_EVIDENCE" ./scripts/smoke-app-launch.sh
go run ./cmd/transwarp-audit -summary -allow-incomplete -app .build/Transwarp.app -release-archive .build/Transwarp-release.zip -self-hosted-evidence "$SELF_HOSTED_EVIDENCE" -app-launch-evidence "$APP_LAUNCH_EVIDENCE"
