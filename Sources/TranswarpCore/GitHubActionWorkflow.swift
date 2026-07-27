public struct GitHubActionWorkflow: Equatable, Sendable {
	public enum Mode: String, CaseIterable, Sendable {
		case selfHosted
		case direct
		case coordinator
		case releaseEvidence
	}

	public var mode: Mode
	public var jobID: String
	public var actionRef: String
	public var jobCommand: String
	public var jobArguments: [String]
	public var jobCheckout: Bool
	public var jobTimeoutSeconds: Int

	public init(
		mode: Mode,
		jobID: String,
		actionRef: String = "charliewilco/transwarp@main",
		jobCommand: String = "",
		jobArguments: [String] = [],
		jobCheckout: Bool = true,
		jobTimeoutSeconds: Int = 3600
	) {
		self.mode = mode
		self.jobID = jobID
		self.actionRef = actionRef
		self.jobCommand = jobCommand
		self.jobArguments = jobArguments
		self.jobCheckout = jobCheckout
		self.jobTimeoutSeconds = jobTimeoutSeconds
	}

	public static func make(
		for configuration: AgentConfiguration,
		mode: Mode,
		actionRef: String = "charliewilco/transwarp@main",
		jobID: String? = nil
	) -> GitHubActionWorkflow? {
		if mode == .releaseEvidence {
			return GitHubActionWorkflow(mode: mode, jobID: "", actionRef: actionRef)
		}
		let requestedJobID = jobID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
		let job = requestedJobID.isEmpty
			? configuration.jobs.first
			: configuration.jobs.first { $0.id == requestedJobID }
		guard let job, !job.id.isEmpty else {
			return nil
		}
		return GitHubActionWorkflow(
			mode: mode,
			jobID: job.id,
			actionRef: actionRef,
			jobCommand: job.command,
			jobArguments: job.arguments,
			jobCheckout: job.checkout,
			jobTimeoutSeconds: job.timeoutSeconds
		)
	}

	public var yaml: String {
		switch mode {
		case .selfHosted:
			"""
			name: Self-Hosted Mac Build

			on:
			  workflow_dispatch:

			jobs:
			  build-on-desktop:
			    runs-on: [self-hosted, macOS, ARM64, transwarp-desktop]
			    timeout-minutes: \(timeoutMinutes)
			    steps:
			\(checkoutStep)\(selfHostedGoSetupStep)\(selfHostedVerificationStep)
			      - name: Build with local Xcode
			        run: |
			          set -euo pipefail
			          \(shellCommand)

			      - name: Upload self-hosted evidence
			        uses: actions/upload-artifact@v4
			        if: always()
			        with:
			          name: transwarp-self-hosted-mac-${{ github.run_id }}-${{ github.run_attempt }}
			          path: |
			            evidence/self-hosted-mac.json
			            evidence/self-hosted-readiness.log
			"""
		case .direct:
			"""
			name: Transwarp Build

			on:
			  workflow_dispatch:
			    inputs:
			      cancel:
			        description: Cancel an existing direct runner build instead of starting a new one.
			        required: false
			        default: 'false'
			      tail:
			        description: Tail an existing direct runner build instead of starting a new one.
			        required: false
			        default: 'false'
			      build-id:
			        description: Build ID to cancel or tail.
			        required: false

			jobs:
			  dispatch:
			    runs-on: ubuntu-latest
			    steps:
			      - uses: actions/setup-go@v5
			        with:
			          go-version: '1.26'

			      - id: transwarp
			        uses: \(actionRef)
			        with:
			          url: ${{ secrets.TRANSWARP_URL }}
			          token: ${{ secrets.TRANSWARP_TOKEN }}
			          access-client-id: ${{ secrets.TRANSWARP_ACCESS_CLIENT_ID }}
			          access-client-secret: ${{ secrets.TRANSWARP_ACCESS_CLIENT_SECRET }}
			          cancel: ${{ inputs.cancel }}
			          tail: ${{ inputs.tail }}
			          build-id: ${{ inputs.build-id }}
			          job: \(Self.yamlSingleQuoted(jobID))
			\(checkoutMetadataInput)
			          # report-url: ${{ secrets.TRANSWARP_REPORT_URL }}
			          # report-token: ${{ secrets.TRANSWARP_REPORT_TOKEN }}
			          # min-cpu-count: 12
			          # min-memory-bytes: 34359738368
			          # min-xcode-version: '16.4'
			"""
		case .coordinator:
			"""
			name: Transwarp Coordinator Build

			on:
			  workflow_dispatch:
			    inputs:
			      cancel:
			        description: Cancel an existing coordinator dispatch instead of starting a new one.
			        required: false
			        default: 'false'
			      request-id:
			        description: Existing Transwarp request ID to cancel.
			        required: false

			jobs:
			  dispatch:
			    runs-on: ubuntu-latest
			    steps:
			      - uses: actions/setup-go@v5
			        with:
			          go-version: '1.26'

			      - id: transwarp
			        uses: \(actionRef)
			        with:
			          mode: coordinator
			          coordinator-url: ${{ secrets.TRANSWARP_COORDINATOR_URL }}
			          coordinator-token: ${{ secrets.TRANSWARP_COORDINATOR_TOKEN }}
			          access-client-id: ${{ secrets.TRANSWARP_ACCESS_CLIENT_ID }}
			          access-client-secret: ${{ secrets.TRANSWARP_ACCESS_CLIENT_SECRET }}
			          cancel: ${{ inputs.cancel }}
			          request-id: ${{ inputs.request-id }}
			          job: \(Self.yamlSingleQuoted(jobID))
			\(checkoutMetadataInput)
			          # min-cpu-count: 12
			          # min-memory-bytes: 34359738368
			          # min-xcode-version: '16.4'

			      - name: Summarize selected runner
			        if: always() && steps.transwarp.outputs['build-id'] != ''
			        run: |
			          {
			            echo "### Transwarp dispatch"
			            echo ""
			            echo "- Request ID: ${{ steps.transwarp.outputs['request-id'] }}"
			            echo "- Build ID: ${{ steps.transwarp.outputs['build-id'] }}"
			            echo "- Job ID: ${{ steps.transwarp.outputs['job-id'] }}"
			            echo "- Machine ID: ${{ steps.transwarp.outputs['machine-id'] }}"
			            echo "- Public URL: ${{ steps.transwarp.outputs['public-url'] }}"
			          } >> "$GITHUB_STEP_SUMMARY"
			"""
		case .releaseEvidence:
			"""
			name: Transwarp Release Evidence

			on:
			  workflow_dispatch:
			    inputs:
			      collect-named-tunnel:
			        description: Run the live named-tunnel coordinator smoke in this workflow.
			        required: false
			        default: 'true'
			      named-tunnel-evidence:
			        description: Existing named-tunnel receipt path; use only with collect-named-tunnel=false.
			        required: false
			      ci-dispatch-evidence:
			        description: Existing CI-dispatch receipt path; required with named-tunnel-evidence when collect-named-tunnel=false.
			        required: false
			      clean-mac-evidence:
			        description: Required path to a clean-Mac evidence JSON receipt already present in the workspace.
			        required: true

			jobs:
			  named-tunnel-coordinator-smoke:
			    runs-on: [self-hosted, macOS, ARM64, transwarp-desktop]
			    timeout-minutes: 45
			    steps:
			      - name: Check out Transwarp
			        uses: actions/checkout@v4

			      - name: Set up Go
			        uses: actions/setup-go@v5
			        with:
			          go-version: '1.26'

			      - name: Preflight release inputs
			        env:
			          TRANSWARP_COLLECT_NAMED_TUNNEL: ${{ inputs.collect-named-tunnel }}
			          TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE: app
			          TRANSWARP_SELF_HOSTED_EVIDENCE: .build/release-evidence/self-hosted-mac.json
			          TRANSWARP_NAMED_TUNNEL_EVIDENCE: ${{ inputs.named-tunnel-evidence }}
			          TRANSWARP_CI_DISPATCH_EVIDENCE: ${{ inputs.ci-dispatch-evidence }}
			          TRANSWARP_NOTARIZE_REQUESTED: ${{ secrets.TRANSWARP_NOTARIZE }}
			          TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN: ${{ secrets.TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN }}
			          TRANSWARP_PUBLIC_URL: ${{ secrets.TRANSWARP_PUBLIC_URL }}
			          TRANSWARP_EXPECTED_CLOUDFLARED_VERSION: ${{ secrets.TRANSWARP_EXPECTED_CLOUDFLARED_VERSION }}
			          TRANSWARP_ACCESS_CLIENT_ID: ${{ secrets.TRANSWARP_ACCESS_CLIENT_ID }}
			          TRANSWARP_ACCESS_CLIENT_SECRET: ${{ secrets.TRANSWARP_ACCESS_CLIENT_SECRET }}
			          TRANSWARP_CLEAN_MAC_EVIDENCE: ${{ inputs.clean-mac-evidence }}
			          SIGN_IDENTITY: ${{ secrets.TRANSWARP_SIGN_IDENTITY }}
			          APPLE_KEYCHAIN_PROFILE: ${{ secrets.APPLE_KEYCHAIN_PROFILE }}
			        run: go run ./cmd/transwarp-audit -check-release-collection-inputs

			      - name: Collect release evidence
			        env:
			          TRANSWARP_COLLECT_NAMED_TUNNEL: ${{ inputs.collect-named-tunnel }}
			          TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE: app
			          TRANSWARP_SELF_HOSTED_EVIDENCE: .build/release-evidence/self-hosted-mac.json
			          TRANSWARP_NAMED_TUNNEL_EVIDENCE: ${{ inputs.named-tunnel-evidence }}
			          TRANSWARP_CI_DISPATCH_EVIDENCE: ${{ inputs.ci-dispatch-evidence }}
			          TRANSWARP_NOTARIZE_REQUESTED: ${{ secrets.TRANSWARP_NOTARIZE }}
			          TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN: ${{ secrets.TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN }}
			          TRANSWARP_PUBLIC_URL: ${{ secrets.TRANSWARP_PUBLIC_URL }}
			          TRANSWARP_EXPECTED_CLOUDFLARED_VERSION: ${{ secrets.TRANSWARP_EXPECTED_CLOUDFLARED_VERSION }}
			          TRANSWARP_ACCESS_CLIENT_ID: ${{ secrets.TRANSWARP_ACCESS_CLIENT_ID }}
			          TRANSWARP_ACCESS_CLIENT_SECRET: ${{ secrets.TRANSWARP_ACCESS_CLIENT_SECRET }}
			          TRANSWARP_CLEAN_MAC_EVIDENCE: ${{ inputs.clean-mac-evidence }}
			          SIGN_IDENTITY: ${{ secrets.TRANSWARP_SIGN_IDENTITY }}
			          APPLE_KEYCHAIN_PROFILE: ${{ secrets.APPLE_KEYCHAIN_PROFILE }}
			        run: ./scripts/collect-release-evidence.sh

			      - name: Show audit summary
			        if: always()
			        run: |
			          test -f .build/release-evidence/transwarp-audit.json || {
			            echo "transwarp-audit.json was not produced" >&2
			            exit 1
			          }
			          go run ./cmd/transwarp-audit -summary -allow-incomplete -report .build/release-evidence/transwarp-audit.json

			      - name: Upload evidence
			        uses: actions/upload-artifact@v4
			        if: always()
			        with:
			          name: transwarp-release-evidence-${{ github.run_id }}-${{ github.run_attempt }}
			          path: |
			            .build/release-evidence/
			            .build/Transwarp-release.zip
			"""
		}
	}

	private var timeoutMinutes: Int {
		max(1, (jobTimeoutSeconds + 59) / 60)
	}

	private var shellCommand: String {
		([jobCommand] + jobArguments)
			.filter { !$0.isEmpty }
			.map(Self.shellSingleQuoted)
			.joined(separator: " ")
	}

	private var checkoutStep: String {
		guard jobCheckout else {
			return ""
		}
		return """
			      - name: Check out repository
			        uses: actions/checkout@v4

			"""
	}

	private var checkoutMetadataInput: String {
		guard !jobCheckout else {
			return ""
		}
		return "          checkout-metadata: 'false'\n"
	}

	private var selfHostedGoSetupStep: String {
		"""
			      - uses: actions/setup-go@v5
			        with:
			          go-version: '1.26'

			"""
	}

	private var selfHostedVerificationStep: String {
		"""
			      - name: Verify self-hosted Mac
			        run: |
			          set -euo pipefail
			          mkdir -p evidence
			          identities_file="$(mktemp)"
			          trap 'rm -f "$identities_file"' EXIT
			          architecture="$(uname -m)"
			          test "$architecture" = "arm64"
			          macos="$(sw_vers -productVersion)"
			          developer_dir="$(xcode-select -p)"
			          xcode="$(xcodebuild -version | tr '\\n' ' ' | sed 's/[[:space:]]*$//')"
			          code_signing_identities_visible=false
			          if security find-identity -v -p codesigning >"$identities_file" 2>/dev/null && ! grep -q "0 valid identities found" "$identities_file"; then
			            code_signing_identities_visible=true
			          fi
			          cat > evidence/self-hosted-readiness.log <<LOG
			          self-hosted Mac readiness passed
			          architecture=$architecture
			          macos=$macos
			          developer_dir=$developer_dir
			          xcode=$xcode
			          code_signing_identities_visible=$code_signing_identities_visible
			          github_actions=true
			          runner_name=${RUNNER_NAME:-}
			          runner_os=${RUNNER_OS:-}
			          LOG
			          go run github.com/charliewilco/transwarp/cmd/transwarp-audit@main \\
			            -write-self-hosted-evidence evidence/self-hosted-mac.json \\
			            -self-hosted-architecture "$architecture" \\
			            -self-hosted-macos "$macos" \\
			            -self-hosted-developer-dir "$developer_dir" \\
			            -self-hosted-xcode "$xcode" \\
			            -self-hosted-code-signing-identities-visible="$code_signing_identities_visible" \\
			            -self-hosted-github-actions=true \\
			            -self-hosted-runner-name "${RUNNER_NAME:-}" \\
			            -self-hosted-runner-os "${RUNNER_OS:-}" \\
			            -self-hosted-source-log evidence/self-hosted-readiness.log

			"""
	}

	private static func yamlSingleQuoted(_ value: String) -> String {
		"'\(value.replacingOccurrences(of: "'", with: "''"))'"
	}

	private static func shellSingleQuoted(_ value: String) -> String {
		"'\(value.replacingOccurrences(of: "'", with: "'\"'\"'"))'"
	}
}
