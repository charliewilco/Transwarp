#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/.build/transwarp-runner"

mkdir -p "$OUT"
cd "$ROOT"
GOOS=darwin GOARCH=arm64 go build -o "$OUT/transwarp-runner" ./cmd/transwarp-runner
