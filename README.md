# Transwarp

Transwarp is a standalone Mac app that lets CI dispatch a build to your own desktop through Cloudflare Tunnel. The app owns local availability, tunnel startup, build execution, log streaming, and result reporting; CI only asks for a configured job to run.

The target is modern Apple Silicon Macs. The app is SwiftUI and package-first, with Go used for the runner, coordinator, dispatch, diagnose, config, and audit CLIs. There is no XcodeGen project.

## What Works

- Native macOS app for configuring and running a local build target.
- Go runner with authenticated local HTTP, allowlisted jobs, checkout support, queueing, cancellation, log streaming, and terminal pass/fail results.
- Cloudflare quick and named tunnel modes through `cloudflared`.
- Registration, heartbeat, deregistration, and paused/available state for CI target discovery.
- GitHub composite action, examples, local packaging, audit, and release-evidence tooling.
- Local tokens, signing material, checkout headers, and sensitive environment values stay on the Mac through Keychain-backed references.

Still requiring real external proof before this should be treated as release-ready:

- Real Cloudflare named-tunnel smoke.
- Real hosted CI dispatch through that tunnel.
- Developer ID signing, hardened runtime, notarization, and Gatekeeper acceptance.
- Clean-Mac validation evidence from a separate Mac.

## Quick Start

Install Cloudflare's connector:

```sh
brew install cloudflared
```

Build and run the app:

```sh
./scripts/package-app.sh
open .build/Transwarp.app
```

Or run directly from SwiftPM while developing:

```sh
swift run Transwarp
```

On first launch, Transwarp creates:

```text
~/Library/Application Support/Transwarp/agent.json
```

Use Settings to configure the machine identity, runner token, Cloudflare Tunnel, CI registration URLs, job recipes, and local job environment.

## Local Checks

Run the main local gate and focused checks:

```sh
./scripts/check.sh
swift test
go test ./...
./scripts/smoke-direct-build.sh
./scripts/smoke-coordinator.sh
./scripts/smoke-github-action.sh
./scripts/smoke-app-launch.sh .build/Transwarp.app
```

`scripts/check.sh` is allowed to report missing external evidence during local development. Missing named-tunnel, real CI dispatch, clean-Mac, signing, notarization, and Gatekeeper proof means the PRD is not fully proven yet.

## Cloudflare Tunnel

Production dispatch should use a named Cloudflare Tunnel with a stable HTTPS public hostname pointed at the local runner:

```text
http://127.0.0.1:8188
```

The runner still requires `Authorization: Bearer <shared_token>`. If the hostname is protected by Cloudflare Access, configure the Access service-token pair as `TRANSWARP_ACCESS_CLIENT_ID` and `TRANSWARP_ACCESS_CLIENT_SECRET`.

Quick tunnels are useful for demos and diagnostics, but they are not a release proof because the hostname is temporary.

## GitHub Actions

The repository includes a composite action in `action.yml`. CI jobs set up Go, then use direct mode against a runner URL or coordinator mode against a CI-side coordinator:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: '1.26'

- id: transwarp
  uses: charliewilco/transwarp@main
  with:
    url: ${{ secrets.TRANSWARP_URL }}
    token: ${{ secrets.TRANSWARP_TOKEN }}
    job: xcode-debug
    tail: true
```

See `examples/` for complete direct, coordinator, self-hosted, and release-evidence workflows.

## Release Evidence

Local release collection:

```sh
TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1 ./scripts/collect-release-evidence.sh
```

Strict release evidence also needs real named-tunnel, GitHub Actions, signing, notarization, Gatekeeper, and clean-Mac receipts:

```sh
go run ./cmd/transwarp-audit \
	-app .build/Transwarp.app \
	-release-archive .build/Transwarp-release.zip \
	-self-hosted-evidence ./self-hosted-mac.json \
	-app-launch-evidence ./app-launch-evidence.json \
	-named-tunnel-evidence ./named-tunnel-evidence.json \
	-ci-dispatch-evidence ./ci-dispatch-evidence.json \
	-clean-mac-evidence ./clean-mac-evidence.json
```

## More Docs

- [Operations guide](docs/operations.md)
- [Direct GitHub Actions example](examples/github-actions.yml)
- [Coordinator GitHub Actions example](examples/github-actions-coordinator.yml)
- [Release evidence workflow](examples/github-actions-release-evidence.yml)

## License

Transwarp's source code is released under the Unlicense. See [LICENSE](LICENSE).
