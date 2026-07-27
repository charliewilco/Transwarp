# Transwarp Operations

## Local Setup

1. For local development, install Cloudflare's connector on Apple Silicon:

	```sh
	brew install cloudflared
	```

	Packaged app bundles require `cloudflared` and copy it into `Contents/Resources`. Set `CLOUDFLARED_PATH=/path/to/cloudflared` to control the exact connector binary that gets embedded. For narrow non-tunnel development experiments only, set `TRANSWARP_ALLOW_MISSING_CLOUDFLARED=1`; release gates still reject that bundle.

2. Launch the app once to create `~/Library/Application Support/Transwarp/agent.json`, then use Settings to edit the machine identity, runner token, tunnel, CI registration URLs, primary job recipe, and job environment. Open JSON remains available from Settings for advanced edits.

	Keep `machine_id` stable after first setup. CI uses it as the durable build-target identity even if the Mac's display name changes. Machine IDs may contain only letters, numbers, dots, underscores, or hyphens, with a 128-byte limit.

3. Build the Go helper:

	```sh
	./scripts/build-runner.sh
	```

4. Run the app:

	```sh
	swift run Transwarp
	```

On first launch, the app creates a local starter config with a generated `machine_id`, generated runner token, tunneling disabled, and an `xcodebuild -version` smoke job. When settings are saved or an existing config is migrated, the app stores runner, registration, tunnel, checkout, and sensitive environment secrets as Keychain-backed references instead of leaving local credentials in JSON.

For a desktop that should stay available to CI after login, enable **Open Transwarp at login** and **Start runner when Transwarp opens** in Settings. Login item registration uses macOS' app-managed login item service, and the runner only auto-starts when the saved configuration passes preflight; otherwise it records a skipped-start event and waits for you to fix the configuration.

After the runner is started, use **Run Test Build** in the toolbar to dispatch the first configured non-checkout job through the same authenticated local HTTP API that CI uses. The app records the requested build ID, polls runner status until that build passes, fails, cancels, or times out, and shows the mirrored build stream in Activity and Logs. Open active and queued builds appear in the build controls so the desktop owner can cancel local work that arrived from CI before or during execution. This is the fastest local proof that the machine can accept and finish a build before putting Cloudflare Tunnel or a CI workflow in the path. Checkout jobs still belong to CI dispatch because they need repo, ref, and commit metadata.

The Machine panel shows the Mac's CI-target availability separately from the helper process state. **CI Target** becomes available only when registration is configured, the runner is registered with a live lease, the tunnel is ready, the runner is accepting builds, and a public URL is known. Use **Pause** to stop accepting new CI builds without killing the runner or losing local status/log visibility; already accepted active or queued builds remain visible and can be canceled. Use **Resume** to advertise the Mac again on the next status/heartbeat. Pause/resume and build-load changes also request an immediate heartbeat refresh, so coordinators do not have to wait for the next timer tick to learn that the desktop is paused, busy, queued, or available again. The panel also shows the current registration lease and last successful registration/heartbeat time when the runner reports them. When a named tunnel public URL is configured and the runner is running, use **Diagnose** beside the public URL to call the tunneled `/status` endpoint with runner authentication and any configured Runner Access client credentials. The app rejects the diagnosis if the authenticated status response reports a different `machine_id` or a different public URL than the saved configuration, so a stale hostname or copied token cannot look like proof for this desktop.

The app's authenticated runner requests do not follow HTTP redirects. Local status, pause/resume, test-build, cancel, and public runner diagnosis calls all fail at the redirected response so runner bearer tokens and Cloudflare Access service-token headers stay bound to the configured loopback or tunnel URL.

The Go config helper accepts secret defaults from environment variables so setup and release-smoke scripts do not need to put tokens in process arguments. Use `TRANSWARP_TOKEN` or `TRANSWARP_SHARED_TOKEN` for the runner token, `TRANSWARP_REGISTRATION_TOKEN` for CI registration, `TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN` or `TRANSWARP_TUNNEL_TOKEN` for Cloudflare Tunnel, and the matching `TRANSWARP_CI_ACCESS_*` / `TRANSWARP_RUNNER_ACCESS_*` names for Cloudflare Access values.

The same direct build path is covered by the local smoke script:

```sh
./scripts/smoke-direct-build.sh
```

That smoke starts a loopback runner, dispatches a non-checkout build through `transwarp-dispatch`, tails the build logs, and verifies `/status` records the request as passed.

The app's **CI Workflows** panel can copy self-hosted, direct, coordinator, or release-evidence GitHub Actions workflows. The self-hosted workflow runs on `[self-hosted, macOS, ARM64, transwarp-desktop]` and records structured hardware/Xcode/signing readiness evidence before executing the configured command. The direct workflow references GitHub secrets for the runner URL/token and optional Runner Access service token values. The coordinator workflow references only the coordinator URL/token and optional Coordinator Access service token values; it does not put the Mac runner bearer token in GitHub Actions. Use **Copy Names** for the required GitHub secret names and **Copy Values** for only the local dispatch, coordinator endpoint, or release values Transwarp can know. Coordinator exports include placeholders that distinguish the GitHub Actions coordinator token, coordinator target callback token, and coordinator deployment's runner token; the Mac registration token must match that target token and is not copied into GitHub Actions. Release exports include `TRANSWARP_PUBLIC_URL`, optional Runner Access values, and the packaged `cloudflared --version` as `TRANSWARP_EXPECTED_CLOUDFLARED_VERSION` when the saved config uses the bundled connector and the app manifest is present. They still leave the Cloudflare tunnel token, Developer ID signing identity, and notarization credentials as names-only setup. The release workflow runs the named-tunnel coordinator smoke, packages/audits the app, and uploads release evidence. The app does not copy local Keychain-backed secrets into workflow YAML, and it never includes checkout authorization headers, job environment secrets, signing secrets, or notary credentials in copied workflow setup values.

To build a local app bundle with the Go helper and required Cloudflare connector embedded:

```sh
./scripts/package-app.sh
open .build/Transwarp.app
```

This creates an ad-hoc signed development bundle and verifies its nested code signature in `scripts/check.sh`. Packaging also writes `Contents/Resources/TranswarpManifest.json` with the app version/build, signed helper hash, bundled `cloudflared` hash, `cloudflared --version` output, and optional expected connector version. `scripts/check.sh` writes local self-hosted readiness evidence under `.build/check-evidence` and passes it into the final audit summary. Override the default `0.1.0` / `1` bundle metadata with `TRANSWARP_VERSION` and `TRANSWARP_BUILD_NUMBER`. For release candidates, set `TRANSWARP_EXPECTED_CLOUDFLARED_VERSION` to the exact `cloudflared --version` string you intend to ship so the audit can enforce the connector policy. Distribution still needs a Developer ID signature, hardened runtime, notarization, and Gatekeeper validation on a clean Mac.

To create a release archive that carries the app plus clean-Mac validation helpers:

```sh
./scripts/archive-release.sh
```

That writes `.build/Transwarp-release.zip` with `TranswarpRelease/Transwarp.app` and a self-contained `TranswarpRelease/Validation/` folder so the second Mac does not need a repository checkout to produce clean-Mac evidence.

The packaged app launch smoke is also part of `scripts/check.sh`. It seeds a temporary config with a known runner token, launches the app with `TRANSWARP_CONFIG_PATH` and `TRANSWARP_START_RUNNER_ON_LAUNCH=1`, verifies the durable config was migrated to a Keychain reference, checks the spawned runner accepts the original token through its loopback `/status` API, dispatches an app-spawned build, streams its logs, and verifies both the terminal build status and recent-build status record. Set `TRANSWARP_APP_LAUNCH_EVIDENCE=app-launch-evidence.json` to keep a structured receipt plus copied app/build/status logs for `transwarp-audit`.

For opt-in live proof that the packaged app path owns Cloudflare Tunnel too:

```sh
./scripts/smoke-app-launch-quick.sh .build/Transwarp.app
```

That wrapper runs the app launch smoke with `TRANSWARP_APP_LAUNCH_TUNNEL_MODE=quick`, writes `.build/app-launch-quick-evidence.json`, and validates the receipt with `transwarp-audit -evidence-only`. In that mode the app still launches the bundled helper, but the helper opens a quick tunnel, the smoke waits for a ready `trycloudflare.com` public URL, verifies authenticated `/status` through that public URL with `transwarp-diagnose`, then starts and streams the build through that same tunnel URL with `transwarp-dispatch`. The resulting app-launch receipt records `tunnel_mode`, `public_url`, `tunnel_ready`, `public_status_authenticated`, plus copied public diagnose and dispatch logs. This remains outside default checks because quick tunnels depend on external Cloudflare network state and public DNS propagation.

For release validation of the current bundle:

```sh
./scripts/release-gate.sh
TRANSWARP_RELEASE_STRICT=1 ./scripts/release-gate.sh
```

The non-strict gate verifies bundle structure, arm64-only binaries, nested signatures, manifest app version/build provenance, manifest hash integrity, bundled `cloudflared` version provenance, configured `cloudflared` version policy, distribution-signing metadata for the app and bundled helpers, stapled notarization-ticket validation, and current Gatekeeper state while allowing development-only warnings. Strict mode fails unless the app and bundled helpers are Developer ID signed, hardened-runtime enabled, notarization-stapled, accepted by Gatekeeper, and the expected connector version is configured and matched.

For machine-readable release evidence, use the Go audit CLI:

```sh
go run ./cmd/transwarp-audit -app .build/Transwarp.app
```

The audit emits JSON with one check per release/runtime gate. Use `-summary` for a concise terminal view of only the non-passing gates, `-expected-cloudflared-version` or `TRANSWARP_EXPECTED_CLOUDFLARED_VERSION` to enforce the bundled connector version outside the manifest, `-report path/to/transwarp-audit.json` to summarize a stored report without rerunning checks, and `-allow-incomplete` only for local development checks where missing external evidence is expected but hard failures should still fail. It intentionally exits nonzero when evidence is missing, including packaged-app launch/build proof and the external gates that local packaging cannot prove: named Cloudflare Tunnel smoke, real CI dispatch, and clean-Mac validation. Clean-Mac evidence must prove Gatekeeper acceptance, first launch, and a passed local build request through the app-spawned runner. Attach versioned JSON evidence receipts when those runs exist:

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

The production-style named-tunnel coordinator smoke writes `named-tunnel-evidence.json` through the same Go audit CLI after it has collected raw diagnose, dispatch, app, runner, target, and result logs:

```sh
go run ./cmd/transwarp-audit \
	-write-named-tunnel-evidence .build/release-evidence/named-tunnel-evidence.json \
	-named-tunnel-launch-mode app \
	-named-tunnel-public-url https://transwarp.example.com \
	-named-tunnel-machine-id named-tunnel-smoke-coordinator \
	-named-tunnel-job-id echo \
	-named-tunnel-request-id named-tunnel-smoke-coordinator-run \
	-named-tunnel-diagnose-log .build/release-evidence/named-tunnel-diagnose.log \
	-named-tunnel-dispatch-log .build/release-evidence/named-tunnel-dispatch.log \
	-named-tunnel-runner-log .build/release-evidence/named-tunnel-runner.log \
	-named-tunnel-app-log .build/release-evidence/named-tunnel-app.log \
	-named-tunnel-app-stderr .build/release-evidence/named-tunnel-app.err \
	-named-tunnel-targets-registered .build/release-evidence/named-tunnel-targets-registered.json \
	-named-tunnel-targets-after-deregister .build/release-evidence/named-tunnel-targets-after-deregister.json \
	-named-tunnel-results .build/release-evidence/named-tunnel-results.json
```

When `collect-release-evidence.sh` runs inside GitHub Actions, it uses the same Go audit CLI to write `ci-dispatch-evidence.json` from the named-tunnel receipt, the source workflow log, and GitHub's run context:

```sh
go run ./cmd/transwarp-audit \
	-named-tunnel-evidence .build/release-evidence/named-tunnel-evidence.json \
	-ci-dispatch-source-log .build/release-evidence/named-tunnel-coordinator-smoke.log \
	-ci-dispatch-source-log-name named-tunnel-coordinator-smoke.log \
	-write-ci-dispatch-evidence .build/release-evidence/ci-dispatch-evidence.json
```

See `examples/github-actions-release-evidence.yml` for a GitHub Actions workflow that runs the named-tunnel coordinator smoke on a self-hosted Apple Silicon Mac, packages the app, runs `transwarp-audit`, and uploads the evidence receipts, smoke logs, and JSON audit report as a release-evidence artifact. It defaults `collect-named-tunnel` to `true`; set it to `false` only when `named-tunnel-evidence` and `ci-dispatch-evidence` both point at existing receipts in the checked-out workspace. After clean-Mac validation exists in that workspace, pass its path through the `clean-mac-evidence` workflow input so strict collection can attach it to the audit.

For the same release-evidence flow from a self-hosted Mac shell:

```sh
TRANSWARP_COLLECT_NAMED_TUNNEL=1 \
TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN="..." \
TRANSWARP_PUBLIC_URL="https://transwarp.example.com" \
TRANSWARP_EXPECTED_CLOUDFLARED_VERSION="cloudflared version ..." \
SIGN_IDENTITY="Developer ID Application: Example, Inc. (TEAMID)" \
TRANSWARP_NOTARIZE_REQUESTED=1 \
APPLE_KEYCHAIN_PROFILE="transwarp-notary" \
./scripts/collect-release-evidence.sh
```

That script runs the app-owned named-tunnel coordinator smoke, packages, checks packaged app launch/build behavior, optionally notarizes, archives, audits, and writes evidence to `.build/release-evidence`. Strict collection requires `TRANSWARP_EXPECTED_CLOUDFLARED_VERSION`, Developer ID signing, an `APPLE_KEYCHAIN_PROFILE` created with `xcrun notarytool store-credentials`, named-tunnel evidence, and CI-dispatch evidence up front so deterministic policy and proof-routing failures stop before expensive tunnel and packaging work; local incomplete collection may omit them. If `TRANSWARP_CLEAN_MAC_EVIDENCE` is set, the file must already exist, because clean-Mac proof comes from the separate validation step after an archive is produced. The collector always writes `app-launch-evidence.json` for the local packaged app launch/build gate before auditing. Boolean collector flags such as `TRANSWARP_NOTARIZE_REQUESTED` and `TRANSWARP_COLLECT_ALLOW_INCOMPLETE` accept `1`, `true`, or `yes` for enabled and `0`, `false`, or `no` for disabled, which matches typical GitHub secret values; `TRANSWARP_COLLECT_NAMED_TUNNEL` also accepts `auto`. Evidence receipts use `schema_version: 1` and a valid `generated_at` timestamp. Receipt companion files such as source logs, app-owned runner tunnel logs, target snapshots, and status JSON must be relative paths stored beside the receipt, so evidence artifacts are self-contained. Named-tunnel evidence must include `launch_mode: app`, app launch and Keychain-migration proof, the same HTTPS base `public_url` shape accepted by the runner, diagnose, dispatch, and coordinator paths, the CI `job_id` and `request_id`, the accepted runner `build_id`, the selected `machine_id`, copied coordinator target snapshots proving that machine was registered before dispatch and absent after deregistration, plus copied diagnose/dispatch logs with passing markers, a named tunnel status line proving `state=running`, `connected=true`, and `ready=true`, an app-captured runner tunnel log proving the bundled helper started, Cloudflare registered a tunnel connection, and Transwarp marked that exact `public_url` ready, plus a coordinator `accepted runner build` JSON event containing those exact values. CI-dispatch evidence records the same `job_id`, `request_id`, `machine_id`, and `public_url`, and the audit correlates the job ID, request ID, machine ID, tunnel URL, and runner build ID across receipts. When the named-tunnel smoke passes, it writes a structured `named-tunnel-evidence.json` receipt and uses that receipt for the named-tunnel audit gate. For local plumbing checks without external credentials, set `TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1`; the audit JSON will still show which gates are missing.

To check those release-collector inputs without starting packaging, notarization, or tunnel work:

```sh
go run ./cmd/transwarp-audit -check-release-collection-inputs
```

That preflight reads the current environment, validates boolean collector flags, named-tunnel credential/public URL shape, optional Cloudflare Access credential pairing, strict signing/notarization policy inputs, and GitHub Actions runner context when the collector is expected to generate CI-dispatch evidence. It only treats secret-bearing values as present or missing.

`scripts/smoke-release-evidence-collector.sh` runs that collector in local incomplete mode and asserts the generated self-hosted readiness evidence, packaged app launch/build evidence, and companion source logs are included in the audit while named-tunnel, CI-dispatch, and clean-Mac gates remain missing. Successful collector smokes clean their temporary release-evidence directories, app bundles, and archives; set `TRANSWARP_KEEP_COLLECTOR_SMOKE_ARTIFACTS=1` when you want to inspect them. `scripts/smoke-clean-mac-validate.sh` exercises the clean-Mac validator receipt path locally with mocked signing/notarization/Gatekeeper command responses; it proves the harness can launch the packaged app, dispatch a local build, and write an audit-shaped receipt, not that a release is notarized or clean-Mac accepted.

When `collect-release-evidence.sh` runs inside GitHub Actions and the named-tunnel coordinator smoke passes, it writes `.build/release-evidence/ci-dispatch-evidence.json` and passes that receipt to `transwarp-audit`. The CI-dispatch receipt must include GitHub run context (`run_id`, `run_attempt`, `workflow`, `job`, `repository`, `sha`), GitHub runner context (`runner_os`, `runner_arch`), the HTTPS tunnel `public_url`, the CI `job_id`, CI `request_id`, selected `machine_id`, and accepted runner `build_id` from `named-tunnel-evidence.json`, plus a readable source smoke log containing `diagnosis passed`, streamed build output, `[result] recorded passed`, that same `public_url`, and those same IDs. The collector preflights `runner_os=macOS` / `runner_arch=ARM64` before running the named-tunnel smoke; the audit also requires numeric `run_id` / `run_attempt`, an `owner/repo` repository value, a 40-character commit SHA, and that self-hosted release-runner context. A local named-tunnel smoke log is not enough to satisfy the CI dispatch gate by itself.

On a separate clean Apple Silicon Mac, validate the notarized app before attaching clean-Mac evidence:

```sh
unzip Transwarp-release.zip
cd TranswarpRelease
TRANSWARP_CLEAN_MAC_EVIDENCE=clean-mac-evidence.json \
	./Validation/clean-mac-validate.sh ./Transwarp.app | tee clean-mac.log
```

That script verifies strict code signing, the stapled notarization ticket, Gatekeeper acceptance, and a first launch that starts an authenticated loopback runner from a temporary config. It also dispatches a tiny local `clean-mac-launch` build through that app-spawned runner, streams the build log, and verifies the terminal build status plus the recent-build status record. Set `TRANSWARP_CLEAN_MAC_EVIDENCE=clean-mac-evidence.json` to write the structured receipt required by `transwarp-audit -clean-mac-evidence`; the release archive includes `Validation/transwarp-audit` so the clean Mac does not need the repository checkout or a Go toolchain to write that receipt. The receipt includes bundle artifact hashes, path-safe expected machine/job/build/request IDs, copied `codesign`, `stapler`, and Gatekeeper command receipts, a copied `clean-mac-status.json` response, copied build log/status JSON, and copied first-launch stdout/stderr logs. The audit compares those hashes to the app under `-app` and re-reads the status response and companion log paths.

To build with a Developer ID identity, provide `SIGN_IDENTITY`. `scripts/package-app.sh` applies hardened runtime and timestamp options automatically for non-ad-hoc identities:

```sh
SIGN_IDENTITY="Developer ID Application: Example, Inc. (TEAMID)" ./scripts/package-app.sh
```

After building with a Developer ID identity, submit and staple notarization explicitly:

```sh
xcrun notarytool store-credentials transwarp-notary --apple-id you@example.com --team-id TEAMID
APPLE_KEYCHAIN_PROFILE="transwarp-notary" ./scripts/notarize-app.sh
```

The notarization helper refuses ad-hoc or non-hardened bundles, requires a local notary Keychain profile, submits a zip with `notarytool --wait`, staples the ticket, validates the staple, and runs `spctl`.

## Cloudflare Tunnel

The intended product path is a named, token-based Cloudflare Tunnel configured in the Cloudflare dashboard. Point the public hostname at:

```text
http://127.0.0.1:8188
```

Put Cloudflare Access in front of the hostname and let CI authenticate with service tokens. The runner still requires `Authorization: Bearer <shared_token>` so local requests and tunnel traffic have the same defense-in-depth boundary.

For direct CI calls or coordinator-to-runner calls to an Access-protected runner hostname, provide the service token pair as `TRANSWARP_ACCESS_CLIENT_ID` and `TRANSWARP_ACCESS_CLIENT_SECRET`. Transwarp sends them as `CF-Access-Client-Id` and `CF-Access-Client-Secret`. In coordinator-mode GitHub Actions, those same action input names authenticate GitHub to an Access-protected coordinator hostname; use the coordinator Access service token pair there, not the runner pair, unless you also opt into direct selected-runner diagnostics with a runner token. In the Mac app, configure `runner_access_client_id` and `runner_access_client_secret` only when the desktop should diagnose its own Access-protected runner hostname through the Machine panel.

If the CI/coordinator registration or result callback endpoint is also behind Cloudflare Access, configure `ci_access_client_id` and `ci_access_client_secret` in the Mac runner config. The runner sends those headers on registration, heartbeat, deregistration, and terminal result callbacks. Settings stores the client secret as a local Keychain reference when saved.

Quick tunnels are supported for local demos with `"mode": "quick"`, but they are a poor production fit because the URL is not stable enough for CI registration or policy control.

Use `"cloudflared_path": "@bundle/cloudflared"` for the connector embedded in `Transwarp.app`. The app passes its resource directory to the Go runner at launch, and the runner does not fall back to `PATH` for that sentinel. For development or managed installs, set an absolute executable path such as `/opt/homebrew/bin/cloudflared`; leaving the path empty keeps development `PATH` lookup available.

For an opt-in live tunnel smoke on a developer Mac with `cloudflared` installed:

```sh
./scripts/smoke-cloudflare-quick.sh
```

That script starts a quick tunnel, diagnoses the public runner endpoint, dispatches an `echo` job through the `trycloudflare.com` URL, then verifies the runner reports the build as passed. It is intentionally not part of `scripts/check.sh` because account-less quick tunnels and public DNS propagation are external-network dependencies.

Transwarp's Go runner readiness checks, diagnostic CLI, dispatch CLI, and coordinator all try the system resolver first, then fall back to Cloudflare public DNS for tunnel hostnames before declaring a public URL unreachable. If quick-tunnel smoke still fails with a resolution error for the generated `trycloudflare.com` hostname, the local runner may have started `cloudflared` and established a connector, but the public hostname was not reachable enough to use as product evidence. Treat that as insufficient product evidence and use a named tunnel for release validation.

Set `TRANSWARP_QUICK_TUNNEL_EVIDENCE=quick-tunnel-diagnostic.json` on either quick-tunnel smoke to have the Go audit helper write a structured diagnostic receipt plus copied diagnose/dispatch logs. Coordinator quick-tunnel receipts also include the accepted runner `build_id`, CI `request_id`, selected `machine_id`, generated `public_url` validated against the coordinator's `accepted runner build` JSON event, and copied coordinator target snapshots before dispatch and after deregistration. The receipt is intentionally marked `"release_evidence": false` and is not accepted by `transwarp-audit`; use it to preserve live network debugging proof without weakening the named-tunnel release gate.

To prove the fuller CI contract with a local coordinator and an account-less quick tunnel:

```sh
./scripts/smoke-cloudflare-coordinator.sh
```

That smoke starts the reference coordinator, starts a runner that opens a quick tunnel, waits for the runner to register its `trycloudflare.com` URL as an available target, diagnoses the public runner endpoint, dispatches through the coordinator, streams build logs back through the tunnel, and verifies the coordinator records the terminal result callback.

For the production-style named tunnel path, provide a real tunnel token and hostname:

```sh
TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN="..." \
TRANSWARP_PUBLIC_URL="https://transwarp.example.com" \
TRANSWARP_ACCESS_CLIENT_ID="..." \
TRANSWARP_ACCESS_CLIENT_SECRET="..." \
./scripts/smoke-cloudflare-named.sh
```

`TRANSWARP_PUBLIC_URL` must be the HTTPS base URL for the named tunnel route, without embedded credentials, path, query, or fragment. The Access service-token values are optional when the hostname is not Access-protected. The named smoke wipes its temporary runner config on exit so the tunnel token is not left in retained smoke logs.

To prove the production-style named tunnel through the coordinator path as well:

```sh
TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN="..." \
TRANSWARP_PUBLIC_URL="https://transwarp.example.com" \
TRANSWARP_ACCESS_CLIENT_ID="..." \
TRANSWARP_ACCESS_CLIENT_SECRET="..." \
./scripts/smoke-cloudflare-named-coordinator.sh
```

That smoke starts the reference coordinator locally, launches the packaged app by default, verifies the app migrates runner, registration, and tunnel secrets into Keychain references, waits for the app-spawned helper to register with the configured public hostname, diagnoses the selected target through the coordinator and public runner URL, dispatches through the coordinator, verifies streamed logs plus the terminal result callback, and then verifies deregistration removes the target. Set `TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE=runner` only for low-level helper debugging; release evidence requires the default app-owned mode.

## Dispatch API

Authenticated callers can check runner readiness:

```sh
curl \
	--header "Authorization: Bearer $TRANSWARP_TOKEN" \
	http://127.0.0.1:8188/status
```

The status response includes active and queued build counts, the queued-build limit, whether the runner is currently `accepting_builds`, configured job IDs, public URL, live tunnel state, live registration state, recent builds, and a `capabilities` object. Recent builds include the build command status plus `report_status` / `report_error` when a CI result callback was configured, so the app and CI can distinguish "the Mac build passed" from "the CI receipt was reported." After helper restart, terminal recent-build status and the latest CI report callback outcome are restored from the local ledger when no live in-memory builds exist; workspace paths remain local-only. The app also promotes new failed CI result callbacks into the top-level error/activity stream so the desktop owner does not have to spot a small row-level report failure. Capabilities currently include `os`, `os_version`, `architecture`, `cpu_brand`, `cpu_count`, `memory_bytes`, `xcode_version`, and `developer_dir` when available. Tunnel status keeps fatal connector `error` separate from nonfatal `readiness_error`, which explains why a running connector is not ready to advertise yet. Registration status reports whether CI registration is disabled, waiting for tunnel readiness, registering, registered, deregistered, failed, or in a heartbeat-failed state, along with the latest lease timestamp when available. On shutdown, the runner stops heartbeats and retries deregistration within a bounded timeout so coordinators are less likely to keep stale targets until lease expiry.

```sh
curl \
	--header "Authorization: Bearer $TRANSWARP_TOKEN" \
	--header "Content-Type: application/json" \
	--data '{"job_id":"xcode-debug","request_id":"local-smoke","report_url":"https://ci.example.com/transwarp/result","report_token":"ci-result-token"}' \
	http://127.0.0.1:8188/v1/builds
```

Checkout jobs require `repo_url` plus at least one explicit `ref` or `commit`; requests that omit the revision are rejected before the Mac queues local work.

The response is `202 Accepted` with a build handle:

```json
{
	"build_id": "build-abc123",
	"status": "running",
	"logs_url": "/v1/builds/build-abc123/logs?after=0&follow=true",
	"cancel_url": "/v1/builds/build-abc123/cancel"
}
```

`request_id` is required for build starts. Build-start payloads are decoded as a strict JSON object: unknown fields and trailing JSON values are rejected before local work is queued, so typoed CI inputs fail closed. Request IDs are path-safe stable identifiers with letters, numbers, dots, underscores, hyphens, and a 256-byte limit. Repeating the same `request_id` with the same payload returns the existing build handle so CI can safely retry after a dropped response; reusing it with a different job, repo, ref, commit, or report callback is rejected. Accepted `build_id` values use the same shape with a 128-byte limit. The runner intentionally executes only one build at a time, but additional valid requests are accepted into a FIFO queue and stream `queued` until the active build finishes. A build keeps the active slot until any terminal result callback has succeeded or failed, so queued work does not begin before CI has accepted or rejected the previous pass/fail report. The queue is bounded to 25 waiting builds; once full, new build requests are rejected without creating local work, while duplicate `request_id` retries still return the existing build. The runner publishes `queued_build_limit` in `/status`, registration, and heartbeat payloads so CI and coordinators can fail fast before dispatching into a saturated desktop.

The GitHub Action exposes `request-id` for both direct and coordinator dispatches. It also exposes the accepted `build-id` and `job-id` once a Mac creates local work, even if the streamed build later fails; coordinator dispatches additionally expose `machine-id` and `public-url` for the selected runner. Coordinator streams include a small JSON metadata line with the selected runner build ID, job ID, request ID, machine ID, and runner public URL so release evidence can prove which desktop accepted the request. The dispatcher validates streamed coordinator `build_id`, `job_id`, `request_id`, `machine_id`, and `public_url` metadata before exposing those values as CI outputs, and a coordinator stream that appears to pass without accepted-build metadata is treated as an invalid dispatch result. Generated and example workflows give the action step the stable ID `transwarp`, so follow-up steps can read outputs such as `steps.transwarp.outputs.request-id`, `steps.transwarp.outputs.build-id`, `steps.transwarp.outputs.job-id`, and `steps.transwarp.outputs.machine-id`; the Go dispatcher writes those through GitHub's multiline-safe output format and prints matching `[result]` marker lines into the CI log after a successful dispatch. Use `request-id` to cancel coordinator dispatches and `build-id` to cancel or reconnect to direct runner builds.

Tail logs with the build ID. `after` is the last sequence already received, so a CI client can reconnect without replaying the whole stream. The runner keeps a bounded in-memory replay window for recent events; active followers drain from that same recent window, and stale or slow reconnects receive an `info` event when older retained history has been truncated. Followed streams also emit transient `info` keepalive events during quiet builds so CI logs and Cloudflare Tunnel connections do not go idle; keepalives are not retained and do not consume sequence numbers:

```sh
curl --no-buffer \
	--header "Authorization: Bearer $TRANSWARP_TOKEN" \
	"http://127.0.0.1:8188/v1/builds/build-abc123/logs?after=0&follow=true"
```

The log response is `application/x-ndjson`:

```json
{"kind":"build","message":"starting Xcode Debug Build","build_id":"build-abc123","job_id":"xcode-debug","sequence":1}
{"kind":"log","message":"...","build_id":"build-abc123","job_id":"xcode-debug","sequence":2}
{"kind":"build","message":"passed","build_id":"build-abc123","job_id":"xcode-debug","sequence":3}
```

Check or cancel a build:

```sh
curl --header "Authorization: Bearer $TRANSWARP_TOKEN" \
	http://127.0.0.1:8188/v1/builds/build-abc123

curl --request POST \
	--header "Authorization: Bearer $TRANSWARP_TOKEN" \
	http://127.0.0.1:8188/v1/builds/build-abc123/cancel
```

Pause or resume new CI work without stopping the runner:

```sh
curl --request POST \
	--header "Authorization: Bearer $TRANSWARP_TOKEN" \
	--header "Content-Type: application/json" \
	--data '{"accepting_builds":false}' \
	http://127.0.0.1:8188/v1/availability
```

While paused, the runner returns `503 Service Unavailable` for new build requests, continues to report local status/logs, and preserves idempotent retries for already accepted `request_id` values. Registration and heartbeat payloads include `accepting_builds`; the coordinator and diagnostic CLI treat paused targets as unavailable.

If `report_url` is present, Transwarp posts a terminal result receipt after the command exits and keeps the tailed log stream open until the callback succeeds or fails. `report_url` and `report_token` must be provided together. That receipt includes the build ID, job ID, request ID, machine name, ref/commit metadata, exit code, status, timestamps, duration, and error text when the build fails. It does not include local workspace or working-directory paths; those stay in local logs and the local ledger. The callback URL must share an origin with the configured CI registration, heartbeat, or deregistration endpoint, or with an explicit `allowed_report_origins` entry such as `https://ci.example.com`. Callback URLs may include a path, must include a host, must not include embedded credentials, query strings, or fragments, and must use HTTPS unless they target local loopback for smoke tests; use Cloudflare Access service-token headers in addition when the callback endpoint is Access-protected. Registration, heartbeat, deregistration, and result callbacks use the same tunnel-aware HTTP client as diagnose/dispatch, including fallback resolution through Cloudflare public DNS when the system resolver lags a tunnel hostname. Result receipts retry transient delivery failures such as network errors, HTTP `429`, and HTTP `5xx`; HTTP `3xx` redirects and `4xx` client/auth errors fail immediately so CI receipt routing and bad credentials stay visible.

CI lifecycle endpoint redirects fail immediately too. Registration, heartbeat, deregistration, and result callbacks stay bound to the configured endpoint origin, so bearer tokens and Cloudflare Access service-token headers are not sent to redirected URLs.

For jobs with `"checkout": true`, `repo_url` must exactly match one of the job's `allowed_repositories`. Repository URLs in requests and config must not include embedded credentials, query strings, or fragments. Transwarp clones the repository into a temporary workspace, fetches the requested `ref` or `commit`, checks out `commit` when present or the fetched `ref` otherwise, runs the configured argv-only command there, then removes the workspace after the build. Leave `workspace_root` empty to use Transwarp's cache directory, or set it to an absolute path for checkout workspaces. Non-checkout jobs reject `repo_url`, `ref`, and `commit`; they run only against the Mac-local configured working directory and environment.

Private HTTPS repositories should use a Mac-local `checkout_authorization_header` such as `Authorization: Bearer <github-token>`. Store it through Settings as a Keychain-backed checkout authorization header, or hand-edit it as a reference like `keychain://co.charliewil.transwarp/<machine_id>/jobs/xcode-debug/checkout_authorization_header`. The runner injects the resolved value into Git through environment-based `http.<repo_url>.extraHeader` configuration so the secret is not passed by CI, embedded in repository URLs, or placed in `git` command-line arguments.

Job environment values with common token, password, credential, keychain, certificate, provisioning profile, signing identity, or notarization key names are stored locally and redacted from streamed logs by default. Additional literal `redacted_values` are also stored as local Keychain references by the app and resolved only inside the Mac runner. Use `redacted_environment_keys` for project-specific secret names that do not follow those patterns.

Checkout uses a constrained Git subprocess environment rather than inheriting the app or runner environment. It keeps `HOME`, a fixed tool `PATH`, and temp directory access, disables interactive Git credential prompts, ignores user/system Git config, and leaves build/signing/API variables out of the clone step.

Checkout targets are validated before clone: ordinary branch names, tag names, full refs such as `refs/heads/main`, `refs/tags/v1.0.0`, GitHub pull refs such as `refs/pull/123/merge`, and commit hashes are accepted. Option-like targets, whitespace/control characters, ref traversal (`..`), reflog syntax, and common Git metacharacters are rejected before any job command runs.

## Rollout Path

Start with a normal GitHub Actions self-hosted runner on the Mac when all you need is "run this workflow on my desktop." That path proves the hardware, Xcode install, signing state, and repository build command before Transwarp adds a tunnel, registration, dispatch, retained logs, and CI callback contract.

See `examples/github-actions-self-hosted.yml` for that first stage, or copy a project-local starter workflow from the app's **CI Workflows** panel. Label the GitHub runner with a machine-specific label such as `transwarp-desktop`, then target `[self-hosted, macOS, ARM64, transwarp-desktop]`. The Transwarp repository example runs `scripts/check-self-hosted-mac.sh` before building so the job records Apple Silicon architecture, macOS version, selected Xcode developer directory, Xcode version, and whether code-signing identities are visible to the runner. Set `TRANSWARP_SELF_HOSTED_EVIDENCE` to also write a JSON receipt plus `self-hosted-readiness.log` through the Go audit helper; both can be uploaded from Actions or passed to `transwarp-audit -self-hosted-evidence`. App-generated project starter workflows use `actions/setup-go` and `go run github.com/charliewilco/transwarp/cmd/transwarp-audit@main` for the same receipt contract without requiring the project to vendor Transwarp.

Move to Transwarp when you want the Mac app to own availability, Cloudflare Tunnel, local job allowlists, log streaming, cancellation, and result reporting instead of letting the Actions runner execute workflow steps directly on the machine.

## GitHub Actions Dispatch

The repository includes a composite GitHub Action at the repo root so CI can dispatch to Transwarp with one `uses:` step after Go is available on the runner:

```yaml
jobs:
  dispatch:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - id: transwarp
        uses: charliewilco/transwarp@main
        with:
          url: ${{ secrets.TRANSWARP_URL }}
          token: ${{ secrets.TRANSWARP_TOKEN }}
          access-client-id: ${{ secrets.TRANSWARP_ACCESS_CLIENT_ID }}
          access-client-secret: ${{ secrets.TRANSWARP_ACCESS_CLIENT_SECRET }}
          tail: ${{ inputs.tail }}
          build-id: ${{ inputs.build-id }}
          job: xcode-debug
          report-url: ${{ secrets.TRANSWARP_REPORT_URL }}
          report-token: ${{ secrets.TRANSWARP_REPORT_TOKEN }}
          min-cpu-count: 12
          min-memory-bytes: 34359738368
          min-xcode-version: '16.4'
```

For coordinator mode:

```yaml
- id: transwarp
  uses: charliewilco/transwarp@main
  with:
    mode: coordinator
    coordinator-url: ${{ secrets.TRANSWARP_COORDINATOR_URL }}
    coordinator-token: ${{ secrets.TRANSWARP_COORDINATOR_TOKEN }}
    access-client-id: ${{ secrets.TRANSWARP_ACCESS_CLIENT_ID }}
    access-client-secret: ${{ secrets.TRANSWARP_ACCESS_CLIENT_SECRET }}
    job: xcode-debug
    min-cpu-count: 12
    min-memory-bytes: 34359738368
    min-xcode-version: '16.4'
```

The action runs `transwarp-diagnose` before new dispatches by default, derives `request-id`, `repo-url`, `ref`, and `commit` from the GitHub workflow context when not provided, then runs the Go dispatch CLI so the workflow exits with the desktop build result. The default request ID is `GITHUB_RUN_ID-GITHUB_RUN_ATTEMPT-GITHUB_JOB`, so multiple Transwarp jobs in one workflow attempt do not collide; pass `request-id` explicitly when canceling or reconnecting to a known coordinator dispatch. The action rejects unsafe `request-id`, `job`, `build-id`, and `machine-id` values before diagnostics or dispatch, using the same letters/numbers/dot/underscore/hyphen identifier shape that the runner, coordinator, dispatcher, and release-evidence audit expect. If `repo-url` is omitted outside a normal GitHub repository context, the action leaves it empty instead of synthesizing an invalid clone URL; checkout jobs should provide `repo-url` explicitly in that case. Optional direct-mode `report-url` and `report-token` inputs wire the CI result callback; configure them together so the workflow fails if the Mac build passes but the result receipt cannot be delivered. Optional `min-cpu-count`, `min-memory-bytes`, and `min-xcode-version` inputs fail diagnostics when the selected runner is underpowered; in coordinator mode they also filter candidate Macs before dispatch. `min-cpu-count` and `min-memory-bytes` must be unsigned integer strings, and invalid environment values are rejected by the Go CLIs instead of being treated as no constraint. Boolean action inputs such as `diagnose`, `allow-http`, `tail`, and `cancel` accept `true`, `1`, or `yes` for enabled and `false`, `0`, or `no` for disabled. Set `tail: true` with `build-id` to reconnect to an existing direct runner build without starting new work. Set `cancel: true` to run the dispatcher as a cancel operation instead of starting work; direct mode also requires `build-id`, while coordinator mode requires the existing `request-id`. Diagnose, dispatch, tail, and cancel requests fail on HTTP redirects instead of following them, so runner/coordinator bearer tokens and Cloudflare Access service-token headers stay on the configured tunnel endpoint. It intentionally uses the Go CLIs instead of a JavaScript action; keep `actions/setup-go` before it. See `examples/github-actions.yml` and `examples/github-actions-coordinator.yml` for complete workflows.

`scripts/smoke-github-action.sh` extracts the composite action script and runs it locally with a stubbed `go` command. That keeps the action preflight contract covered without needing a live GitHub runner for every local check.

Before dispatching a build through a named tunnel, use the diagnostic CLI from CI to fail fast on DNS, Cloudflare Access, runner auth, tunnel status, and job-advertisement problems:

```sh
go run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@main \
	-url "$TRANSWARP_URL" \
	-token "$TRANSWARP_TOKEN" \
	-access-client-id "$TRANSWARP_ACCESS_CLIENT_ID" \
	-access-client-secret "$TRANSWARP_ACCESS_CLIENT_SECRET" \
	-job xcode-debug
```

The diagnostic requires remote endpoints to be HTTPS base URLs with no embedded credentials, path, query, or fragment. Use `-allow-http` only for local loopback checks. Runner diagnostics print registration state and the current registration lease, and fail fast when the runner reports a registration failure, heartbeat failure, missing lease, or expired lease. Pass `-machine-id` when CI must pin a specific desktop; direct runner diagnostics fail if the authenticated `/status` response comes from a different Mac, and coordinator diagnostics fail if the selected target or selected runner probe does not match the requested machine.

Use the Go dispatch CLI from CI so the workflow exits with the desktop build result:

```sh
go run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@main \
	-url "$TRANSWARP_URL" \
	-token "$TRANSWARP_TOKEN" \
	-access-client-id "$TRANSWARP_ACCESS_CLIENT_ID" \
	-access-client-secret "$TRANSWARP_ACCESS_CLIENT_SECRET" \
	-job xcode-debug \
	-min-cpu-count 12 \
	-min-memory-bytes 34359738368 \
	-min-xcode-version 16.4 \
	-request-id "$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$GITHUB_JOB" \
	-repo-url "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY.git" \
	-ref "$GITHUB_REF" \
	-commit "$GITHUB_SHA"
```

The CLI requires remote runner and coordinator URLs to be HTTPS base URLs with no embedded credentials, path, query, or fragment, while allowing `http` for loopback smoke checks such as `http://127.0.0.1:8188`. It starts the build, tails logs by `build_id`, reconnects with `after=<last_sequence>` if the tail drops, and exits nonzero if Transwarp reports a failed or canceled build. Direct tail/cancel rejects malformed `build_id` values before building runner URLs. In direct mode, if the CLI receives `SIGINT` or `SIGTERM` while tailing a build, it asks the runner to cancel that build before exiting so canceled CI jobs do not leave local work running. In coordinator mode, the CLI asks the coordinator to cancel the active `request_id`, and the coordinator forwards cancellation to the selected runner build once the runner has accepted it. When `report_url` and `report_token` are configured, the same tailed stream includes the result callback outcome and the CLI exits nonzero if reporting the result back to CI fails. Use a path-safe `request_id` that is stable for one CI attempt and unique across parallel Transwarp jobs, such as `$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$GITHUB_JOB`. Use `-build-id <id>` to tail an existing direct runner build, `-cancel -build-id <id>` to cancel a direct runner build, and `-coordinator-url <url> -cancel -request-id <id>` to cancel an active coordinator dispatch.

When CI talks to the reference coordinator instead of a runner tunnel directly, diagnose the coordinator first. This verifies coordinator DNS/auth, active target registration with a live lease, advertised Apple Silicon/macOS capabilities, job advertisement, and that the selected target has a base tunnel `public_url` unless you are doing a local `-allow-http` check:

```sh
go run github.com/charliewilco/transwarp/cmd/transwarp-diagnose@main \
	-coordinator-url "$TRANSWARP_COORDINATOR_URL" \
	-coordinator-token "$TRANSWARP_COORDINATOR_TOKEN" \
	-access-client-id "$TRANSWARP_ACCESS_CLIENT_ID" \
	-access-client-secret "$TRANSWARP_ACCESS_CLIENT_SECRET" \
	-job xcode-debug
```

The runner token is optional for coordinator diagnosis. Pass `-token "$TRANSWARP_TOKEN"` only when you deliberately want the diagnostic to probe the selected target's `public_url` and authenticated `/status` endpoint, catching stale tunnel URLs, Access failures, and runner-token mismatches before dispatch. Without it, the diagnostic stays coordinator-only and reports that the runner probe was skipped.

Then use the dispatch CLI in coordinator mode:

```sh
go run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@main \
	-coordinator-url "$TRANSWARP_COORDINATOR_URL" \
	-coordinator-token "$TRANSWARP_COORDINATOR_TOKEN" \
	-job xcode-debug \
	-min-cpu-count 12 \
	-min-memory-bytes 34359738368 \
	-min-xcode-version 16.4 \
	-request-id "$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$GITHUB_JOB" \
	-repo-url "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY.git" \
	-ref "$GITHUB_REF" \
	-commit "$GITHUB_SHA"
```

Add `-machine-id "$TRANSWARP_MACHINE_ID"` only when CI must pin a specific registered Mac. Without it, the coordinator chooses an active target that advertises the requested job. Workflows copied from the app include manual `workflow_dispatch` cancel inputs so an operator can cancel an active direct runner build by `build-id` or an active coordinator dispatch by `request-id`; direct workflows also include `tail` for reconnecting to an existing build stream by `build-id`.

Cancel an active coordinator dispatch by its stable CI request ID:

```sh
go run github.com/charliewilco/transwarp/cmd/transwarp-dispatch@main \
	-coordinator-url "$TRANSWARP_COORDINATOR_URL" \
	-coordinator-token "$TRANSWARP_COORDINATOR_TOKEN" \
	-cancel \
	-request-id "$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$GITHUB_JOB"
```

## Reference Coordinator

For local development and CI-contract testing, `transwarp-coordinator` provides the minimal CI-side API that Transwarp expects:

- `POST /transwarp/register`
- `POST /transwarp/heartbeat`
- `POST /transwarp/deregister`
- `GET /transwarp/targets`
- `POST /transwarp/dispatch`
- `POST /transwarp/dispatches/{request_id}/cancel`
- `POST /transwarp/result`
- `GET /transwarp/results`

The coordinator requires its own CI/operator bearer token (`-token` or `TRANSWARP_COORDINATOR_TOKEN`), a target callback token for Mac registration and result callbacks (`-target-token` or `TRANSWARP_COORDINATOR_TARGET_TOKEN`), the runner bearer token it uses for dispatch (`-transwarp-token` or `TRANSWARP_TOKEN`), and a public URL it can place in result callbacks. If `-target-token` is omitted, the reference coordinator falls back to the CI/operator token for local compatibility, but production deployments should keep them separate so a desktop registration secret cannot dispatch CI work. Remote coordinator public URLs must be HTTPS base URLs with no embedded credentials, path, query, or fragment; `http` is accepted only for loopback local smokes. Do not expose a coordinator without scoped tokens configured.

Run it locally:

```sh
TRANSWARP_TOKEN="$TRANSWARP_TOKEN" \
go run ./cmd/transwarp-coordinator \
	-token "$TRANSWARP_COORDINATOR_TOKEN" \
	-target-token "$TRANSWARP_COORDINATOR_TARGET_TOKEN" \
	-access-client-id "$TRANSWARP_ACCESS_CLIENT_ID" \
	-access-client-secret "$TRANSWARP_ACCESS_CLIENT_SECRET" \
	-result-wait-timeout 30s \
	-state-path "$HOME/.config/Transwarp/coordinator-state.json" \
	-public-url http://127.0.0.1:8288
```

Point the Mac runner config at `http://127.0.0.1:8288/transwarp/register`, `/heartbeat`, and `/deregister`, and set its `registration_token` to the coordinator target token; in the app Settings, enter the coordinator base URL and choose **Use Coordinator** to fill those lifecycle endpoints together. The coordinator stores active target leases and build results by `machine_id`/`request_id`, persists them to `-state-path`, expires stale machines when targets are listed, caps far-future leases, normalizes persisted target, active dispatch, and result state on restart, and uses the optional Cloudflare Access service token pair when it dispatches through a registered runner tunnel URL. Coordinator-to-runner dispatch, tail, and cancel calls also fail on HTTP redirects, keeping the runner bearer token and Access headers bound to the selected target URL. Persisted target keys must match their `machine_id`, just as result keys must match `request_id`, so restarted coordinators do not keep listable targets that cannot be pinned, deregistered, or reserved by their canonical identity. Registered targets retain the runner's advertised capabilities and current build load; the coordinator rejects invalid `machine_id` values, non-base or remote `http` target `public_url` values, non-loopback `listen` fallbacks, missing capability payloads, non-macOS or non-Apple-Silicon registrations, and missing, unparseable, or unsupported macOS versions. `/transwarp/targets` lists only active, supported targets that are accepting builds; paused, stale, or unsupported targets stay unavailable for discovery and unpinned dispatch, and pinned dispatch rejects unavailable targets instead of silently sending work to the wrong hardware class. While a coordinator dispatch is active, it reserves one queue slot on the selected target in memory so concurrent dispatches see that pressure before the runner's next heartbeat, then remembers the selected runner URL and validated `build_id` after the runner accepts the build. Accepted active dispatches are persisted, so a restarted coordinator can still accept the terminal result callback, reconnect to the selected build, or forward a cancel request; pre-acceptance reservations stay memory-only because there is not yet a safe runner build handle. `POST /transwarp/dispatches/{request_id}/cancel` validates the path-safe request ID and forwards cancellation to that selected runner build; before a runner build is accepted, it returns a conflict instead of pretending local work was stopped. Repeating a dispatch after a result has already been recorded returns the recorded result when the payload matches. Repeating the same in-flight dispatch after the runner has accepted a build reconnects to that selected runner build and waits for the existing result callback instead of starting duplicate desktop work; repeating it before a runner build is accepted still returns a conflict because there is not yet a safe build handle to reconnect to. First result callbacks must use the target token, include valid `request_id`, `build_id`, `job_id`, and `machine_id` values, and match the active dispatch's job/repo/ref/commit metadata, selected machine, accepted runner `build_id`, and selected runner public URL before they are recorded; callbacks that arrive before the coordinator has an accepted runner build are rejected. Exact duplicate receipts are accepted after recording, but orphan receipts, changed terminal results, and receipts that mismatch the active dispatch are rejected.

The local coordinator smoke covers the lifecycle without Cloudflare in the path, including a coordinator restart after the runner has accepted a build, a reconnect that proves the restarted coordinator records the terminal result, and a separate restart-after-acceptance cancel path that proves the restarted coordinator can still forward cancellation and record the canceled result:

```sh
./scripts/smoke-coordinator.sh
```

It starts the coordinator, starts a loopback runner, verifies registration, dispatches a build through the coordinator, verifies the terminal result receipt, then stops the runner and verifies deregistration removes the target.

Dispatch through the coordinator with a `job_id` and `request_id`. Dispatch rejects invalid `job_id` and pinned `machine_id` values before creating active coordinator state. `machine_id` is optional; omit it to let the coordinator choose among active registered targets that advertise the job, or include it when CI must pin a specific Mac. Dispatch can also include `min_cpu_count`, `min_memory_bytes`, and `min_xcode_version` constraints. For unpinned dispatch, the coordinator filters out targets that do not satisfy those constraints, then prefers lower-load targets first by active build count, queued build count, and machine ID. Pinned dispatch rejects the named Mac when it misses the requested constraints. If one target rejects build start before accepting a build ID, the coordinator tries the next matching target instead of failing the workflow on a busy or stale Mac. Once any target is selected for a build attempt, the active dispatch is bound to that target's `machine_id`, and the terminal result callback must report the same machine. After runner log streaming completes, coordinator dispatch waits for the terminal `/transwarp/result` callback before printing `[result] recorded passed`; if the callback never lands before `-result-wait-timeout` or `TRANSWARP_COORDINATOR_RESULT_WAIT_TIMEOUT`, it prints `dispatch failed:` so the Go dispatch CLI exits nonzero. Prefer the Go dispatch CLI from CI so streamed build failures and missing result receipts become a nonzero process exit; `curl` is useful for local inspection:

```sh
curl --no-buffer \
	--header "Authorization: Bearer $TRANSWARP_COORDINATOR_TOKEN" \
	--header "Content-Type: application/json" \
	--data '{"job_id":"xcode-debug","request_id":"github-123"}' \
	http://127.0.0.1:8288/transwarp/dispatch
```

## Safety Model

Transwarp treats CI requests as untrusted. A request can choose a configured `job_id`, but the executable path, argv array, working directory, timeout, checkout allowlist, and local environment are controlled by the Mac. The runner uses `exec.CommandContext` directly and does not invoke `/bin/sh -c`.

Non-checkout jobs must use an absolute `working_directory`. Checkout jobs leave `working_directory` empty and run inside the per-build workspace Transwarp creates after repository/ref validation. Workspace directory names include bounded job/request context, so max-length valid CI identifiers do not exceed macOS filename limits.

Shell executables such as `sh`, `bash`, and `zsh` are rejected as configured job commands. If a project needs a script, point the job at a checked-in executable script or a build tool directly.

Config values can reference local Keychain items with `keychain://co.charliewil.transwarp/<account>`. Keychain references must not include credentials, ports, query strings, or fragments; malformed `keychain:` values fail validation instead of becoming literal tokens. The app stores runner, registration, CI Access client secret, Runner Access client secret, Cloudflare tunnel tokens, and additional literal redaction values under that service when Settings saves them, then writes references such as `keychain://co.charliewil.transwarp/<machine_id>/shared_token` to `agent.json`. When the app starts its bundled helper, it resolves those references itself and passes the Go runner a transient runtime config over stdin, so durable config remains reference-only. The helper process receives a minimal environment rather than the app's full launch environment, keeping unrelated local variables out of the runner and `cloudflared` connector. The Go runner also starts `cloudflared` with a fixed connector environment plus only the configured `TUNNEL_TOKEN` for named tunnels, instead of inheriting arbitrary local variables. Standalone `transwarp-runner -config agent.json` can also resolve the same references locally before validation. CI never receives those secrets and they do not need to be committed or copied into workflow files. Other Keychain service names are rejected by default.

Job environment values and checkout authorization headers also support `keychain://` references. Environment keys must use conventional variable names such as `DEVELOPER_DIR`, `MATCH_PASSWORD`, or `API_TOKEN_1`: start with a letter or underscore, then use only letters, digits, and underscores. The `TRANSWARP_` prefix is reserved for runner-supplied metadata such as workspace, repo, ref, and commit. Literal checkout authorization headers must use valid `Header-Name: value` syntax with a non-empty HTTP header name and value. Settings has separate plain and secret environment editors; secret entries, checkout authorization headers, and sensitive key names shaped like tokens, passwords, credentials, keychain profiles, certificates, provisioning profiles, signing identities, or notarization secrets are stored as local Keychain references when saved. Use this for signing passwords, Git tokens, API keys, or project-specific build secrets that must stay on the Mac.

Build logs are redacted before streaming. Transwarp masks configured tokens, checkout authorization headers, values listed in `redacted_values`, CI result callback tokens, and job environment values whose keys look sensitive, including token, password, credential, keychain, certificate, provisioning profile, signing identity, and notarization key names. Cloudflare connector stdout/stderr is passed through the same redactor before it reaches the app log, so a tunnel token echoed by `cloudflared` is masked. Use `redacted_environment_keys` for project-specific names that do not follow those patterns.

When `prevent_sleep` is enabled, the runner starts a macOS `caffeinate -i -w <build-pid>` assertion while a build command is active. The assertion ends when the build exits.

Only one build runs at a time in this MVP. That is intentional for a desktop Xcode workload; up to 25 additional accepted requests wait in the runner queue and can be canceled before they start. Concurrency and queue depth should become explicit settings after there is resource accounting, cancellation, and a persistent job UI.

Build commands run in their own process group on macOS and Linux. Canceling a running build, stopping the runner, or hitting its timeout sends termination to the whole group, so checked-in wrapper scripts and build tools do not leave child processes running after CI has been told the run was canceled. Canceling a queued build records and reports it as canceled without starting the command; if a result callback is configured, its tailed stream stays open through the callback outcome just like a running build. Once a command has produced a terminal pass/fail/canceled result, later cancel requests are rejected even if result reporting is still in progress. On shutdown, the runner cancels active and queued builds, then gives open runs a bounded window to stop and send terminal result callbacks before exiting.

Registration is lease-shaped: `ci_registration_url` receives the current machine identity, public tunnel URL, job list, build-load counts and queue limit, and hardware/toolchain capability payload; `ci_heartbeat_url` refreshes it every `heartbeat_seconds`; and `ci_deregistration_url` is called on normal shutdown. A configured `ci_registration_url` requires `ci_deregistration_url` so normal shutdown removes the target; `ci_heartbeat_url` is optional and falls back to the registration URL. Heartbeat and deregistration URLs require `ci_registration_url`, because that registration URL is the switch that enables the lifecycle. Remote registration endpoints must use HTTPS; HTTP is accepted only for local loopback coordinator smokes. Any configured registration, heartbeat, or deregistration endpoint requires `registration_token`, and Transwarp sends it as a bearer token on every lifecycle request. When using the reference coordinator, this should be the target callback token, not the CI/operator coordinator token. After the tunnel is ready, Transwarp retries initial registration until it succeeds or the runner stops, so a transient CI-side outage does not permanently hide an otherwise available Mac. The authenticated `/status` response and app summary include the current registration state, latest action, latest successful lease, latest registration error, build-load counts and queue limit, and the same advertised capability snapshot. Pause/resume, accepted builds, queued builds, queue removal, and active-slot release coalesce immediate heartbeat refresh requests so the registered target state tracks local changes without waiting for the periodic heartbeat interval. The reference coordinator skips unpinned targets whose declared queue is full and rejects pinned full targets with an explicit queue-full conflict. The CI side should still expire leases server-side because app quit hooks and network teardown are not guaranteed.

Registration, heartbeat, status, and result receipts all include `machine_id` so CI can reconcile a lease, a dispatch stream, and a terminal result back to the same desktop target.

CI registration, heartbeat, and deregistration endpoints must use `http` or `https` and must not include embedded URL credentials, query strings, or fragments; hand-edited runner config is rejected the same way the app Settings preflight rejects unsupported schemes.

Runner bearer tokens, CI registration/target callback tokens, Cloudflare Access client IDs/secrets, and per-build `report_token` values must be single HTTP header values. Hand-edited config or dispatch payloads with control characters are rejected before any request is accepted or callback is attempted.

When registration URLs are configured, result callback URLs are accepted only on the same URL origin as those CI endpoints. That keeps a token-bearing CI caller from turning the Mac into a generic outbound callback client.

For direct runner mode without registration, configure `allowed_report_origins` before accepting CI-supplied `report_url` callbacks. If no registration URL or explicit report origin is configured, build requests with `report_url` are rejected; requests that include only one of `report_url` or `report_token` are also rejected. CI can still stream logs and observe the terminal build status through `transwarp-dispatch`.

When registration is configured, Transwarp waits and retries until tunnel readiness is proven before advertising the Mac. A slow Cloudflare connector startup or delayed DNS propagation does not permanently skip registration for that runner process. For named tunnels, readiness means `cloudflared` has logged a registered tunnel connection and the configured `public_url` resolves. For quick tunnels, it waits for a generated `trycloudflare.com` URL, a registered connection, and DNS resolution. Status JSON keeps `connected`, `ready`, and `readiness_error` separate: `connected` means the connector registered with Cloudflare, `ready` means the public hostname has become usable enough for CI diagnostics and registration, and `readiness_error` carries the current nonfatal reason when readiness is still pending.
