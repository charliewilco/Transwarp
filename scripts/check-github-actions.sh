#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

go run ./cmd/transwarp-audit -check-github-actions -check-github-actions-root "$ROOT"
