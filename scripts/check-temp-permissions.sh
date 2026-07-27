#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

for script in \
	scripts/clean-mac-validate.sh \
	scripts/smoke-app-launch.sh \
	scripts/smoke-cloudflare-coordinator.sh \
	scripts/smoke-cloudflare-named-coordinator.sh \
	scripts/smoke-cloudflare-named.sh \
	scripts/smoke-cloudflare-quick.sh \
	scripts/smoke-coordinator.sh \
	scripts/smoke-diagnose.sh \
	scripts/smoke-direct-build.sh
do
	if ! sed -n '1,6p' "$script" | grep -q '^umask 077$'; then
		echo "$script must set umask 077 before writing temporary configs" >&2
		exit 1
	fi
done
