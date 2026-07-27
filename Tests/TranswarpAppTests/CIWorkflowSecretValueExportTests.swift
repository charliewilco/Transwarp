import Foundation
import Testing
@testable import TranswarpApp
import TranswarpCore

@Suite
struct CIWorkflowSecretValueExportTests {
	@Test
	func directExportIncludesOnlyRunnerDispatchSecrets() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "keychain://co.charliewil.transwarp/machine-123/shared_token",
			ciAccessClientID: "ci-access-id",
			ciAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/ci_access_secret",
			runnerAccessClientID: "runner-access-id",
			runnerAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/runner_access_secret",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://runner.example.com")
			),
			jobs: [
				BuildJob(
					id: "xcode-debug",
					label: "Xcode Debug",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					checkoutAuthorizationHeader: "Authorization: Bearer checkout-secret",
					command: "/usr/bin/xcodebuild",
					environment: [
						"MATCH_PASSWORD": "signing-secret"
					],
					redactedEnvironmentKeys: ["MATCH_PASSWORD"]
				)
			]
		)

		let text = try CIWorkflowSecretValueExport.text(mode: .direct, configuration: configuration) { value in
			switch value {
			case "keychain://co.charliewil.transwarp/machine-123/shared_token":
				return "runner-token"
			case "keychain://co.charliewil.transwarp/machine-123/runner_access_secret":
				return "runner-access-secret"
			case "keychain://co.charliewil.transwarp/machine-123/ci_access_secret":
				Issue.record("Direct export must not resolve CI callback Access credentials")
				return "ci-access-secret"
			default:
				return value
			}
		}

		#expect(text.contains("TRANSWARP_URL='https://runner.example.com'"))
		#expect(text.contains("TRANSWARP_TOKEN='runner-token'"))
		#expect(text.contains("TRANSWARP_ACCESS_CLIENT_ID='runner-access-id'"))
		#expect(text.contains("TRANSWARP_ACCESS_CLIENT_SECRET='runner-access-secret'"))
		#expect(!text.contains("ci-access-id"))
		#expect(!text.contains("ci-access-secret"))
		#expect(!text.contains("checkout-secret"))
		#expect(!text.contains("signing-secret"))
	}

	@Test
	func coordinatorExportIncludesCoordinatorURLAndPlaceholderToken() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "keychain://co.charliewil.transwarp/machine-123/shared_token",
			ciRegistrationURL: URL(string: "https://coord.example.com/transwarp/register"),
			ciHeartbeatURL: URL(string: "https://coord.example.com/transwarp/heartbeat"),
			ciDeregistrationURL: URL(string: "https://coord.example.com/transwarp/deregister"),
			registrationToken: "registration-token",
			ciAccessClientID: "coordinator-access-id",
			ciAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/ci_access_secret",
			runnerAccessClientID: "runner-access-id",
			runnerAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/runner_access_secret",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://runner.example.com")
			),
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

		let text = try CIWorkflowSecretValueExport.text(mode: .coordinator, configuration: configuration) { value in
			switch value {
			case "keychain://co.charliewil.transwarp/machine-123/shared_token":
				Issue.record("Coordinator export must not resolve the runner bearer token")
				return "runner-token"
			case "keychain://co.charliewil.transwarp/machine-123/ci_access_secret":
				return "coordinator-access-secret"
			case "keychain://co.charliewil.transwarp/machine-123/runner_access_secret":
				Issue.record("Coordinator export must not resolve Runner Access credentials")
				return "runner-access-secret"
			default:
				return value
			}
		}

		#expect(text.contains("TRANSWARP_COORDINATOR_URL='https://coord.example.com'"))
		#expect(text.contains("# TRANSWARP_COORDINATOR_TOKEN=<CI/operator coordinator bearer token>"))
		#expect(text.contains("# TRANSWARP_COORDINATOR_TARGET_TOKEN=<target callback token for coordinator deployment and Mac registration_token; do not add to GitHub Actions>"))
		#expect(text.contains("# TRANSWARP_TOKEN=<runner bearer token for coordinator deployment only; do not add to GitHub Actions>"))
		#expect(text.contains("TRANSWARP_ACCESS_CLIENT_ID='coordinator-access-id'"))
		#expect(text.contains("TRANSWARP_ACCESS_CLIENT_SECRET='coordinator-access-secret'"))
		#expect(!text.contains("TRANSWARP_TOKEN='runner-token'"))
		#expect(!text.contains("runner-access-id"))
		#expect(!text.contains("runner-access-secret"))
		#expect(!text.contains("registration-token"))
	}

	@Test
	func releaseEvidenceExportIncludesOnlyKnownPublicRunnerValues() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "keychain://co.charliewil.transwarp/machine-123/shared_token",
			ciAccessClientID: "ci-access-id",
			ciAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/ci_access_secret",
			runnerAccessClientID: "runner-access-id",
			runnerAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/runner_access_secret",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://runner.example.com")
			),
			jobs: [
				BuildJob(
					id: "release",
					label: "Release",
					workingDirectory: "",
					checkout: true,
					checkoutAuthorizationHeader: "Authorization: Bearer checkout-secret",
					command: "/usr/bin/xcodebuild",
					environment: [
						"MATCH_PASSWORD": "signing-secret"
					],
					redactedEnvironmentKeys: ["MATCH_PASSWORD"]
				)
			]
		)

		let text = try CIWorkflowSecretValueExport.text(mode: .releaseEvidence, configuration: configuration) { value in
			switch value {
			case "keychain://co.charliewil.transwarp/machine-123/shared_token":
				Issue.record("Release export must not resolve the runner bearer token")
				return "runner-token"
			case "keychain://co.charliewil.transwarp/machine-123/runner_access_secret":
				return "runner-access-secret"
			case "keychain://co.charliewil.transwarp/machine-123/ci_access_secret":
				Issue.record("Release export must not resolve CI callback Access credentials")
				return "ci-access-secret"
			default:
				return value
			}
		} cloudflaredVersion: { _ in
			"cloudflared version 2026.7.0"
		}

		#expect(text.contains("TRANSWARP_PUBLIC_URL='https://runner.example.com'"))
		#expect(text.contains("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION='cloudflared version 2026.7.0'"))
		#expect(text.contains("TRANSWARP_ACCESS_CLIENT_ID='runner-access-id'"))
		#expect(text.contains("TRANSWARP_ACCESS_CLIENT_SECRET='runner-access-secret'"))
		#expect(!text.contains("TRANSWARP_TOKEN"))
		#expect(!text.contains("TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN"))
		#expect(!text.contains("TRANSWARP_SIGN_IDENTITY"))
		#expect(!text.contains("TRANSWARP_NOTARIZE"))
		#expect(!text.contains("APPLE_"))
		#expect(!text.contains("ci-access-id"))
		#expect(!text.contains("ci-access-secret"))
		#expect(!text.contains("checkout-secret"))
		#expect(!text.contains("signing-secret"))
	}

	@Test
	func releaseEvidenceExportOmitsUnknownCloudflaredVersion() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .named,
				cloudflaredPath: "/opt/homebrew/bin/cloudflared",
				publicURL: URL(string: "https://runner.example.com")
			),
			jobs: []
		)

		let text = try CIWorkflowSecretValueExport.text(mode: .releaseEvidence, configuration: configuration, resolveSecret: { $0 }) { _ in
			nil
		}

		#expect(text.contains("TRANSWARP_PUBLIC_URL='https://runner.example.com'"))
		#expect(!text.contains("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION"))
	}

	@Test
	func directExportRequiresPublicURL() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
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

		#expect(throws: CIWorkflowSecretValueExportError.self) {
			try CIWorkflowSecretValueExport.text(mode: .direct, configuration: configuration) { $0 }
		}
	}

	@Test
	func directExportRequiresNamedTunnelMode() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .quick,
				publicURL: URL(string: "https://quick.trycloudflare.com")
			),
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

		#expect(throws: CIWorkflowSecretValueExportError.self) {
			try CIWorkflowSecretValueExport.text(mode: .direct, configuration: configuration) { $0 }
		}
	}

	@Test
	func releaseEvidenceExportRequiresPublicURL() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(mode: .named),
			jobs: []
		)

		#expect(throws: CIWorkflowSecretValueExportError.self) {
			try CIWorkflowSecretValueExport.text(mode: .releaseEvidence, configuration: configuration) { $0 }
		}
	}

	@Test
	func releaseEvidenceExportRequiresNamedTunnelMode() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .quick,
				publicURL: URL(string: "https://quick.trycloudflare.com")
			),
			jobs: []
		)

		#expect(throws: CIWorkflowSecretValueExportError.self) {
			try CIWorkflowSecretValueExport.text(mode: .releaseEvidence, configuration: configuration) { $0 }
		}
	}

	@Test
	func canCopyValuesRejectsPartialAccessPairs() {
		var directConfiguration = AgentConfiguration.sample(machineId: "machine-123")
		directConfiguration.runnerAccessClientID = "runner-access-id"
		directConfiguration.runnerAccessClientSecret = ""

		var coordinatorConfiguration = AgentConfiguration.sample(machineId: "machine-123")
		coordinatorConfiguration.ciAccessClientID = "coordinator-access-id"
		coordinatorConfiguration.ciAccessClientSecret = ""

		#expect(!CIWorkflowSecretValueExport.canCopyValues(mode: .direct, configuration: directConfiguration))
		#expect(!CIWorkflowSecretValueExport.canCopyValues(mode: .releaseEvidence, configuration: directConfiguration))
		#expect(!CIWorkflowSecretValueExport.canCopyValues(mode: .coordinator, configuration: coordinatorConfiguration))
	}

	@Test
	func exportEscapesSingleQuotesForShellAssignments() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner'token",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://runner.example.com")
			),
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

		let text = try CIWorkflowSecretValueExport.text(mode: .direct, configuration: configuration) { $0 }

		#expect(text.contains("TRANSWARP_TOKEN='runner'\"'\"'token'"))
	}
}
