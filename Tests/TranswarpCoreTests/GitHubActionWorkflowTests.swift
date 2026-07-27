import Testing
import TranswarpCore

@Suite
struct GitHubActionWorkflowTests {
	@Test
	func selfHostedWorkflowUsesConfiguredCommandWithoutLocalSecrets() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "local-runner-token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "xcode-debug",
					label: "Xcode Debug",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					command: "/usr/bin/xcodebuild",
					arguments: ["-scheme", "Charlie's App", "-configuration", "Debug", "build"],
					environment: ["MATCH_PASSWORD": "local-signing-secret"],
					redactedEnvironmentKeys: ["MATCH_PASSWORD"],
					timeoutSeconds: 3661
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .selfHosted))
		let yaml = workflow.yaml

		#expect(yaml.contains("runs-on: [self-hosted, macOS, ARM64, transwarp-desktop]"))
		#expect(yaml.contains("timeout-minutes: 62"))
		#expect(yaml.contains("uses: actions/checkout@v4"))
		#expect(yaml.contains("uses: actions/setup-go@v5"))
		#expect(yaml.contains("go-version: '1.26'"))
		#expect(yaml.contains("'Charlie'\"'\"'s App'"))
		#expect(yaml.contains("security find-identity -v -p codesigning"))
		#expect(yaml.contains("test \"$architecture\" = \"arm64\""))
		#expect(yaml.contains("go run github.com/charliewilco/transwarp/cmd/transwarp-audit@main"))
		#expect(yaml.contains("-write-self-hosted-evidence evidence/self-hosted-mac.json"))
		#expect(yaml.contains("-self-hosted-source-log evidence/self-hosted-readiness.log"))
		#expect(yaml.contains("self-hosted Mac readiness passed"))
		#expect(yaml.contains("path: |"))
		#expect(yaml.contains("evidence/self-hosted-mac.json"))
		#expect(yaml.contains("evidence/self-hosted-readiness.log"))
		#expect(!yaml.contains("python3"))
		#expect(!yaml.contains("json.dump"))
		#expect(!yaml.contains("local-runner-token"))
		#expect(!yaml.contains("local-signing-secret"))
	}

	@Test
	func directWorkflowUsesActionAndSecretReferences() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "local-runner-token",
			tunnel: TunnelConfiguration(mode: .named),
			jobs: [
				BuildJob(
					id: "xcode-debug",
					label: "Xcode Debug",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					command: "/usr/bin/xcodebuild"
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .direct))
		let yaml = workflow.yaml

		#expect(yaml.contains("uses: charliewilco/transwarp@main"))
		#expect(yaml.contains("id: transwarp"))
		#expect(yaml.contains("url: ${{ secrets.TRANSWARP_URL }}"))
		#expect(yaml.contains("token: ${{ secrets.TRANSWARP_TOKEN }}"))
		#expect(yaml.contains("cancel: ${{ inputs.cancel }}"))
		#expect(yaml.contains("tail: ${{ inputs.tail }}"))
		#expect(yaml.contains("build-id: ${{ inputs.build-id }}"))
		#expect(yaml.contains("job: 'xcode-debug'"))
		#expect(yaml.contains("# report-url: ${{ secrets.TRANSWARP_REPORT_URL }}"))
		#expect(yaml.contains("# report-token: ${{ secrets.TRANSWARP_REPORT_TOKEN }}"))
		#expect(yaml.contains("# min-cpu-count: 12"))
		#expect(yaml.contains("# min-memory-bytes: 34359738368"))
		#expect(yaml.contains("# min-xcode-version: '16.4'"))
		#expect(!yaml.contains("checkout-metadata:"))
		assertWorkflowInputIndentation(yaml, keys: [
			"url:",
				"token:",
					"access-client-id:",
					"access-client-secret:",
					"cancel:",
					"tail:",
					"build-id:",
					"job:",
			"# report-url:",
			"# report-token:",
			"# min-cpu-count:",
			"# min-memory-bytes:",
			"# min-xcode-version:"
		])
		#expect(!yaml.contains("local-runner-token"))
	}

	@Test
	func directWorkflowDisablesCheckoutMetadataForLocalJobs() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "local-runner-token",
			tunnel: TunnelConfiguration(mode: .named),
			jobs: [
				BuildJob(
					id: "local-debug",
					label: "Local Debug",
					workingDirectory: "/Users/charlie/App",
					checkout: false,
					command: "/usr/bin/xcodebuild"
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .direct))
		let yaml = workflow.yaml

		#expect(yaml.contains("job: 'local-debug'"))
		#expect(yaml.contains("checkout-metadata: 'false'"))
		assertWorkflowInputIndentation(yaml, keys: [
			"job:",
			"checkout-metadata:",
			"# report-url:"
		])
	}

	@Test
	func directWorkflowCanTargetConfiguredAdditionalJob() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "local-runner-token",
			tunnel: TunnelConfiguration(mode: .named),
			jobs: [
				BuildJob(
					id: "xcode-debug",
					label: "Xcode Debug",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					command: "/usr/bin/xcodebuild"
				),
				BuildJob(
					id: "local-release",
					label: "Local Release",
					workingDirectory: "/Users/charlie/App",
					checkout: false,
					command: "/usr/bin/xcodebuild",
					arguments: ["-scheme", "App", "archive"]
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(
			for: configuration,
			mode: .direct,
			jobID: "local-release"
		))
		let yaml = workflow.yaml

		#expect(yaml.contains("job: 'local-release'"))
		#expect(yaml.contains("checkout-metadata: 'false'"))
		#expect(!yaml.contains("job: 'xcode-debug'"))
	}

	@Test
	func workflowUnavailableForUnknownSelectedJob() {
		let configuration = AgentConfiguration.sample(machineId: "machine-123")

		#expect(GitHubActionWorkflow.make(for: configuration, mode: .direct, jobID: "missing") == nil)
	}

	@Test
	func coordinatorWorkflowIncludesCoordinatorInputs() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "local-runner-token",
			tunnel: TunnelConfiguration(mode: .named),
			jobs: [
				BuildJob(
					id: "release",
					label: "Release",
					workingDirectory: "",
					checkout: true,
					command: "/usr/bin/xcodebuild"
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .coordinator))
		let yaml = workflow.yaml

		#expect(yaml.contains("mode: coordinator"))
		#expect(yaml.contains("id: transwarp"))
		#expect(yaml.contains("coordinator-url: ${{ secrets.TRANSWARP_COORDINATOR_URL }}"))
		#expect(yaml.contains("coordinator-token: ${{ secrets.TRANSWARP_COORDINATOR_TOKEN }}"))
		#expect(!yaml.contains("token: ${{ secrets.TRANSWARP_TOKEN }}"))
		#expect(yaml.contains("cancel: ${{ inputs.cancel }}"))
		#expect(yaml.contains("request-id: ${{ inputs.request-id }}"))
		#expect(yaml.contains("job: 'release'"))
		#expect(yaml.contains("# min-cpu-count: 12"))
		#expect(yaml.contains("# min-memory-bytes: 34359738368"))
		#expect(yaml.contains("# min-xcode-version: '16.4'"))
		#expect(!yaml.contains("checkout-metadata:"))
		#expect(yaml.contains("name: Summarize selected runner"))
		#expect(yaml.contains("if: always() && steps.transwarp.outputs['build-id'] != ''"))
		#expect(yaml.contains("### Transwarp dispatch"))
		#expect(yaml.contains("steps.transwarp.outputs['request-id']"))
		#expect(yaml.contains("steps.transwarp.outputs['build-id']"))
		#expect(yaml.contains("steps.transwarp.outputs['job-id']"))
		#expect(yaml.contains("steps.transwarp.outputs['machine-id']"))
		#expect(yaml.contains("steps.transwarp.outputs['public-url']"))
		#expect(yaml.contains("$GITHUB_STEP_SUMMARY"))
		assertWorkflowInputIndentation(yaml, keys: [
			"mode:",
			"coordinator-url:",
			"coordinator-token:",
			"access-client-id:",
			"access-client-secret:",
			"cancel:",
			"request-id:",
			"job:",
			"# min-cpu-count:",
			"# min-memory-bytes:",
			"# min-xcode-version:"
		])
	}

	@Test
	func coordinatorWorkflowDisablesCheckoutMetadataForLocalJobs() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "local-runner-token",
			tunnel: TunnelConfiguration(mode: .named),
			jobs: [
				BuildJob(
					id: "local-release",
					label: "Local Release",
					workingDirectory: "/Users/charlie/App",
					checkout: false,
					command: "/usr/bin/xcodebuild"
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .coordinator))
		let yaml = workflow.yaml

		#expect(yaml.contains("mode: coordinator"))
		#expect(yaml.contains("job: 'local-release'"))
		#expect(yaml.contains("checkout-metadata: 'false'"))
		assertWorkflowInputIndentation(yaml, keys: [
			"job:",
			"checkout-metadata:",
			"# min-cpu-count:"
		])
	}

	@Test
	func workflowQuotesJobIDsForYaml() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			jobs: [
				BuildJob(
					id: "ios:Charlie",
					label: "Quoted",
					workingDirectory: "/tmp",
					command: "/usr/bin/true"
				)
			]
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .direct))

		#expect(workflow.yaml.contains("job: 'ios:Charlie'"))
	}

	@Test
	func releaseEvidenceWorkflowUsesNamedTunnelAndCIProofInputsWithoutJobs() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			jobs: []
		)

		let workflow = try #require(GitHubActionWorkflow.make(for: configuration, mode: .releaseEvidence))
		let yaml = workflow.yaml

		#expect(yaml.contains("name: Transwarp Release Evidence"))
		#expect(yaml.contains("collect-named-tunnel:"))
		#expect(yaml.contains("default: 'true'"))
		#expect(yaml.contains("named-tunnel-evidence:"))
		#expect(yaml.contains("Existing named-tunnel receipt path; use only with collect-named-tunnel=false."))
		#expect(yaml.contains("ci-dispatch-evidence:"))
		#expect(yaml.contains("Existing CI-dispatch receipt path; required with named-tunnel-evidence when collect-named-tunnel=false."))
		#expect(yaml.contains("clean-mac-evidence:"))
		#expect(yaml.contains("Required path to a clean-Mac evidence JSON receipt already present in the workspace."))
		#expect(yaml.contains("runs-on: [self-hosted, macOS, ARM64, transwarp-desktop]"))
		#expect(yaml.contains("go-version: '1.26'"))
		#expect(yaml.contains("TRANSWARP_COLLECT_NAMED_TUNNEL: ${{ inputs.collect-named-tunnel }}"))
		#expect(yaml.contains("TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE: app"))
		#expect(yaml.contains("TRANSWARP_NAMED_TUNNEL_EVIDENCE: ${{ inputs.named-tunnel-evidence }}"))
		#expect(yaml.contains("TRANSWARP_CI_DISPATCH_EVIDENCE: ${{ inputs.ci-dispatch-evidence }}"))
		#expect(yaml.contains("TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN: ${{ secrets.TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN }}"))
		#expect(yaml.contains("TRANSWARP_PUBLIC_URL: ${{ secrets.TRANSWARP_PUBLIC_URL }}"))
		#expect(yaml.contains("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION: ${{ secrets.TRANSWARP_EXPECTED_CLOUDFLARED_VERSION }}"))
		#expect(yaml.contains("TRANSWARP_CLEAN_MAC_EVIDENCE: ${{ inputs.clean-mac-evidence }}"))
		#expect(yaml.contains("TRANSWARP_NOTARIZE_REQUESTED: ${{ secrets.TRANSWARP_NOTARIZE }}"))
		#expect(yaml.contains("SIGN_IDENTITY: ${{ secrets.TRANSWARP_SIGN_IDENTITY }}"))
		#expect(yaml.contains("APPLE_KEYCHAIN_PROFILE: ${{ secrets.APPLE_KEYCHAIN_PROFILE }}"))
		#expect(!yaml.contains("APPLE_ID:"))
		#expect(!yaml.contains("APPLE_TEAM_ID:"))
		#expect(!yaml.contains("APPLE_APP_SPECIFIC_PASSWORD:"))
		#expect(yaml.contains("name: Preflight release inputs"))
		#expect(yaml.contains("run: go run ./cmd/transwarp-audit -check-release-collection-inputs"))
		#expect(yaml.contains("run: ./scripts/collect-release-evidence.sh"))
		#expect(yaml.contains("go run ./cmd/transwarp-audit -summary -allow-incomplete -report .build/release-evidence/transwarp-audit.json"))
		#expect(yaml.contains(".build/release-evidence/"))
		#expect(yaml.contains(".build/Transwarp-release.zip"))
		#expect(!yaml.contains("json.loads"))
		#expect(!yaml.contains("job:"))
		#expect(!yaml.contains("machine-123"))
		#expect(!yaml.contains("machine_name"))
		#expect(!yaml.contains("machineName"))
		#expect(!yaml.contains("token\n"))
	}

	@Test
	func workflowUnavailableWithoutJobs() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			jobs: []
		)

		#expect(GitHubActionWorkflow.make(for: configuration, mode: .direct) == nil)
	}

	private func assertWorkflowInputIndentation(_ yaml: String, keys: [String], sourceLocation: SourceLocation = #_sourceLocation) {
		for key in keys {
			let expectedPrefix = "          \(key)"
			let hasExpectedLine = yaml
				.split(separator: "\n", omittingEmptySubsequences: false)
				.contains { $0.hasPrefix(expectedPrefix) }
			#expect(hasExpectedLine, "Expected \(key) to be indented as an action input", sourceLocation: sourceLocation)
		}
	}
}
