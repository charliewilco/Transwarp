# Transwarp V0 Closure

Transwarp v0 closes when the repository has evidence for the whole direct-dispatch loop: a named Cloudflare Tunnel, a real GitHub Actions dispatch through that tunnel, a signed and notarized Mac app, Gatekeeper acceptance, and a clean-Mac validation receipt.

## Required Evidence

- Named tunnel receipt from `scripts/smoke-cloudflare-named-coordinator.sh` using a stable HTTPS hostname routed to the local runner.
- GitHub Actions run of `.github/workflows/transwarp-build.yml` showing request ID, build ID, streamed logs, terminal status, exit code, and pass/fail result.
- Developer ID signed app with hardened runtime, successful notarization, stapled ticket, and `spctl` acceptance.
- Release archive from `scripts/archive-release.sh`.
- Clean-Mac JSON receipt from `Validation/clean-mac-validate.sh` run from the release archive on a separate Apple Silicon Mac.
- Final `transwarp-audit` report that passes without `-allow-incomplete`.

## Deferred After V0

- Coordinator productization beyond the direct-dispatch proof.
- CI providers beyond GitHub Actions.
- App Store distribution, store screenshots, and App Store-specific review polish.
- Advanced onboarding, multi-machine fleet management, and expanded setup automation.
