#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="${1:-$ROOT/.build/Transwarp.app}"
EVIDENCE="${TRANSWARP_APP_LAUNCH_QUICK_EVIDENCE:-$ROOT/.build/app-launch-quick-evidence.json}"
SUMMARY="$ROOT/.build/app-launch-quick-summary.log"

cd "$ROOT"
mkdir -p "$(dirname "$EVIDENCE")" "$(dirname "$SUMMARY")"
rm -f "$EVIDENCE" "$SUMMARY"

TRANSWARP_APP_LAUNCH_TUNNEL_MODE=quick \
	TRANSWARP_APP_LAUNCH_EVIDENCE="$EVIDENCE" \
	./scripts/smoke-app-launch.sh "$APP_DIR"

go run ./cmd/transwarp-audit \
	-evidence-only \
	-app "$APP_DIR" \
	-app-launch-evidence "$EVIDENCE" \
	-summary >"$SUMMARY"

cat "$SUMMARY"
printf "app-owned quick tunnel evidence: %s\n" "$EVIDENCE"
