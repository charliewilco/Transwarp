import Foundation
import Testing
@testable import TranswarpApp
import TranswarpCore

@Suite
struct CIWorkflowReadinessTests {
	@Test
	func directWorkflowBlocksUntilNamedTunnelHasPublicURL() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .quick),
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

		let readiness = CIWorkflowReadiness(mode: .direct, configuration: configuration)

		#expect(readiness.state == .blocked)
		#expect(readiness.items.contains(.init(
			state: .blocked,
			title: "Cloudflare Tunnel",
			detail: "Use a named tunnel for stable CI dispatch; quick tunnels are demo-only"
		)))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_URL"))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_TOKEN"))
	}

	@Test
	func directWorkflowIsWarningWhenLocalConfigurationIsReady() {
		let readiness = CIWorkflowReadiness(
			mode: .direct,
			configuration: AgentConfiguration.sample(machineId: "machine-123")
		)

		#expect(readiness.state == .warning)
		#expect(readiness.summary == "Needs external proof")
		#expect(readiness.items.contains(.init(
			state: .ready,
			title: "Cloudflare Tunnel",
			detail: "Named tunnel has a stable public URL"
		)))
		#expect(readiness.secretGroups.map(\.title).contains("If Cloudflare Access protects the runner hostname"))
	}

	@Test
	func directWorkflowReadinessUsesSelectedAdditionalJob() {
		var configuration = AgentConfiguration.sample(machineId: "machine-123")
		configuration.jobs.append(BuildJob(
			id: "local-release",
			label: "Local Release",
			workingDirectory: "/Users/charlie/App",
			checkout: false,
			command: "/usr/bin/xcodebuild"
		))

		let readiness = CIWorkflowReadiness(
			mode: .direct,
			configuration: configuration,
			jobID: "local-release"
		)

		#expect(readiness.items.contains(.init(
			state: .ready,
			title: "Job",
			detail: "local-release runs xcodebuild"
		)))
		#expect(!readiness.items.contains(.init(
			state: .ready,
			title: "Job",
			detail: "xcode-debug runs xcodebuild"
		)))
	}

	@Test
	func directWorkflowReadinessBlocksForMissingSelectedJob() {
		let readiness = CIWorkflowReadiness(
			mode: .direct,
			configuration: AgentConfiguration.sample(machineId: "machine-123"),
			jobID: "missing"
		)

		#expect(readiness.state == .blocked)
		#expect(readiness.items.contains(.init(
			state: .blocked,
			title: "Job",
			detail: "Selected job is not configured"
		)))
	}

	@Test
	func coordinatorWorkflowRequiresRegistrationLifecycle() {
		var configuration = AgentConfiguration.sample(machineId: "machine-123")
		configuration.ciRegistrationURL = nil
		configuration.ciHeartbeatURL = nil
		configuration.ciDeregistrationURL = nil

		let readiness = CIWorkflowReadiness(mode: .coordinator, configuration: configuration)

		#expect(readiness.state == .blocked)
		#expect(readiness.items.contains(.init(
			state: .blocked,
			title: "Registration",
			detail: "Set register and deregister URLs"
		)))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_COORDINATOR_URL"))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_COORDINATOR_TOKEN"))
		#expect(!readiness.secretChecklistText.contains("TRANSWARP_TOKEN"))
		#expect(!readiness.secretChecklistText.contains("TRANSWARP_COORDINATOR_TARGET_TOKEN"))
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "Coordinator",
			detail: "GitHub uses the CI/operator token; this Mac's registration token must match the coordinator target token and stay out of GitHub"
		)))
	}

	@Test
	func coordinatorWorkflowAllowsRegistrationWithoutHeartbeatURL() {
		var configuration = AgentConfiguration.sample(machineId: "machine-123")
		configuration.ciHeartbeatURL = nil

		let readiness = CIWorkflowReadiness(mode: .coordinator, configuration: configuration)

		#expect(readiness.state == .warning)
		#expect(readiness.items.contains(.init(
			state: .ready,
			title: "Registration",
			detail: "Register/deregister URLs and local target callback token are configured"
		)))
		#expect(readiness.items.contains(.init(
			state: .ready,
			title: "Runner token",
			detail: "Stored locally; give the resolved value to the coordinator deployment, not GitHub Actions"
		)))
		#expect(readiness.secretGroups.map(\.title).contains("If Cloudflare Access protects the coordinator hostname"))
	}

	@Test
	func coordinatorWorkflowNamesMissingRegistrationSecretAsTargetCallbackToken() {
		var configuration = AgentConfiguration.sample(machineId: "machine-123")
		configuration.registrationToken = ""

		let readiness = CIWorkflowReadiness(mode: .coordinator, configuration: configuration)

		#expect(readiness.items.contains(.init(
			state: .blocked,
			title: "Registration",
			detail: "Generate and save a local target callback token"
		)))
	}

	@Test
	func selfHostedWorkflowHasNoGitHubSecretChecklist() {
		let readiness = CIWorkflowReadiness(
			mode: .selfHosted,
			configuration: AgentConfiguration.sample(machineId: "machine-123")
		)

		#expect(readiness.secretGroups.isEmpty)
		#expect(readiness.secretChecklistText.isEmpty)
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "GitHub runner",
			detail: "Register this Mac with labels self-hosted, macOS, ARM64, transwarp-desktop"
		)))
	}

	@Test
	func releaseEvidenceWorkflowListsExternalProofSecrets() throws {
		let readiness = CIWorkflowReadiness(mode: .releaseEvidence, configuration: nil)

		#expect(readiness.state == .warning)
		#expect(readiness.secretChecklistText.contains("TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN"))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION"))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_SIGN_IDENTITY"))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_NOTARIZE"))
		#expect(readiness.secretChecklistText.contains("TRANSWARP_PUBLIC_URL"))
		let signingSecrets = try #require(readiness.secretGroups.first { $0.title == "Signing and notarization secrets" })
		#expect(signingSecrets.names.contains("TRANSWARP_NOTARIZE"))
		#expect(signingSecrets.names.contains("APPLE_KEYCHAIN_PROFILE"))
		#expect(!signingSecrets.names.contains("APPLE_ID"))
		#expect(!signingSecrets.names.contains("APPLE_TEAM_ID"))
		#expect(!signingSecrets.names.contains("APPLE_APP_SPECIFIC_PASSWORD"))
		let optionalSecrets = try #require(readiness.secretGroups.first { $0.title == "Optional GitHub secrets" })
		#expect(!optionalSecrets.names.contains("TRANSWARP_NOTARIZE"))
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "Named tunnel",
			detail: "Add TRANSWARP_PUBLIC_URL and TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN in GitHub secrets before collecting live evidence"
		)))
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "Receipt reuse",
			detail: "Leave collect-named-tunnel enabled unless named-tunnel-evidence and ci-dispatch-evidence both point at existing receipts"
		)))
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "Distribution proof",
			detail: "Developer ID signing, notarization, Gatekeeper, and clean-Mac validation are still external gates"
		)))
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "Clean-Mac receipt",
			detail: "Pass a clean-mac-evidence workflow input after validating the notarized archive on a separate Mac"
		)))
	}

	@Test
	func releaseEvidenceWorkflowShowsSavedNamedTunnelPublicURLAsCopyable() {
		let readiness = CIWorkflowReadiness(
			mode: .releaseEvidence,
			configuration: AgentConfiguration.sample(machineId: "machine-123")
		)

		#expect(readiness.state == .warning)
		#expect(readiness.items.contains(.init(
			state: .ready,
			title: "Named tunnel",
			detail: "Saved public URL can be copied to TRANSWARP_PUBLIC_URL; tunnel token still stays in GitHub"
		)))
	}

	@Test
	func releaseEvidenceWorkflowWarnsWhenSavedNamedTunnelPublicURLIsMissing() {
		let readiness = CIWorkflowReadiness(
			mode: .releaseEvidence,
			configuration: AgentConfiguration(
				machineId: "machine-123",
				machineName: "Mac",
				sharedToken: "token",
				tunnel: TunnelConfiguration(mode: .quick)
			)
		)

		#expect(readiness.state == .warning)
		#expect(readiness.items.contains(.init(
			state: .warning,
			title: "Named tunnel",
			detail: "Save a named tunnel public URL to copy TRANSWARP_PUBLIC_URL from the app; otherwise enter it manually in GitHub"
		)))
	}

	@Test
	func configurationIssuesBlockReadiness() {
		let readiness = CIWorkflowReadiness(
			mode: .direct,
			configuration: AgentConfiguration.sample(machineId: "machine-123"),
			configurationIssues: ["cloudflared must exist and be executable."]
		)

		#expect(readiness.state == .blocked)
		#expect(readiness.items.first == .init(
			state: .blocked,
			title: "Preflight",
			detail: "cloudflared must exist and be executable."
		))
	}
}
