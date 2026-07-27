import Foundation
import Testing
@testable import TranswarpApp
import TranswarpCore

@Suite
struct ConfigurationDraftTests {
	@Test
	func draftPartitionsPlainAndSecretEnvironment() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					checkoutAuthorizationHeader: "keychain://co.charliewil.transwarp/machine-123/jobs/build/checkout_authorization_header",
					command: "/usr/bin/env",
					environment: [
						"DEVELOPER_DIR": "/Applications/Xcode.app/Contents/Developer",
						"MATCH_PASSWORD": "keychain://co.charliewil.transwarp/machine-123/jobs/build/environment/MATCH_PASSWORD"
					],
					redactedEnvironmentKeys: ["MATCH_PASSWORD"],
					timeoutSeconds: 60
				)
			]
		)

		let draft = ConfigurationDraft(configuration: configuration)

		#expect(draft.jobEnvironment.contains("DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer"))
		#expect(draft.jobSecretEnvironment.contains("MATCH_PASSWORD=keychain://co.charliewil.transwarp/machine-123/jobs/build/environment/MATCH_PASSWORD"))
		#expect(draft.jobCheckoutAuthorizationHeader == "keychain://co.charliewil.transwarp/machine-123/jobs/build/checkout_authorization_header")
		#expect(draft.jobRedactedEnvironmentKeys == "MATCH_PASSWORD")
	}

	@Test
	func draftTreatsSensitiveEnvironmentKeysAsSecret() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					environment: [
						"API_TOKEN": "plain-token",
						"DEVELOPER_DIR": "/Applications/Xcode.app/Contents/Developer"
					],
					timeoutSeconds: 60
				)
			]
		)

		let draft = ConfigurationDraft(configuration: configuration)

		#expect(draft.jobSecretEnvironment.contains("API_TOKEN=plain-token"))
		#expect(!draft.jobEnvironment.contains("API_TOKEN=plain-token"))
		#expect(draft.jobEnvironment.contains("DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer"))
	}

	@Test
	func draftTreatsSigningAndCredentialEnvironmentKeysAsSecret() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					environment: [
						"SIGN_IDENTITY": "Developer ID Application: Example (TEAMID)",
						"APPLE_KEYCHAIN_PROFILE": "transwarp-notary-profile",
						"CERTIFICATE_P12": "local-certificate-p12",
						"API_CREDENTIAL": "service-credential",
						"NOTARY_PASSPHRASE": "notary-passphrase",
						"DEVELOPER_DIR": "/Applications/Xcode.app/Contents/Developer"
					],
					timeoutSeconds: 60
				)
			]
		)

		let draft = ConfigurationDraft(configuration: configuration)

		for secret in [
			"SIGN_IDENTITY=Developer ID Application: Example (TEAMID)",
			"APPLE_KEYCHAIN_PROFILE=transwarp-notary-profile",
			"CERTIFICATE_P12=local-certificate-p12",
			"API_CREDENTIAL=service-credential",
			"NOTARY_PASSPHRASE=notary-passphrase"
		] {
			#expect(draft.jobSecretEnvironment.contains(secret))
			#expect(!draft.jobEnvironment.contains(secret))
		}
		#expect(draft.jobEnvironment.contains("DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer"))
	}

	@Test
	func makeConfigurationMergesPlainAndSecretEnvironment() throws {
		var draft = ConfigurationDraft()
		draft.machineId = "machine-123"
		draft.machineName = "Mac"
		draft.sharedToken = "token"
		draft.jobId = "build"
		draft.jobLabel = "Build"
		draft.jobWorkingDirectory = "/tmp"
		draft.jobCheckout = false
		draft.jobCheckoutAuthorizationHeader = ""
		draft.jobCommand = "/usr/bin/env"
		draft.jobArguments = ""
		draft.jobEnvironment = "DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer"
		draft.jobSecretEnvironment = "MATCH_PASSWORD=plain-secret"
		draft.jobRedactedEnvironmentKeys = "API_KEY"
		draft.jobTimeoutSeconds = 60

		let configuration = try draft.makeConfiguration()
		let job = try #require(configuration.jobs.first)

		#expect(job.environment["DEVELOPER_DIR"] == "/Applications/Xcode.app/Contents/Developer")
		#expect(job.environment["MATCH_PASSWORD"] == "plain-secret")
		#expect(job.redactedEnvironmentKeys == ["API_KEY", "MATCH_PASSWORD"])
	}

	@Test
	func makeConfigurationPreservesCheckoutAuthorizationHeader() throws {
		var draft = ConfigurationDraft()
		draft.machineId = "machine-123"
		draft.machineName = "Mac"
		draft.sharedToken = "token"
		draft.jobId = "build"
		draft.jobLabel = "Build"
		draft.jobWorkingDirectory = ""
		draft.jobCheckout = true
		draft.jobAllowedRepositories = "https://github.com/example/app.git"
		draft.jobCheckoutAuthorizationHeader = "Authorization: Bearer local-token"
		draft.jobCommand = "/usr/bin/env"
		draft.jobArguments = ""
		draft.jobTimeoutSeconds = 60

		let configuration = try draft.makeConfiguration()
		let job = try #require(configuration.jobs.first)

		#expect(job.checkoutAuthorizationHeader == "Authorization: Bearer local-token")
	}

	@Test
	func makeConfigurationRoundTripsWorkspaceRootAndRedactedValues() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			workspaceRoot: "/Users/charlie/Developer",
			redactedValues: [
				"literal-signing-secret",
				"second-local-secret"
			],
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					timeoutSeconds: 60
				)
			]
		)

		let draft = ConfigurationDraft(configuration: configuration)
		let saved = try draft.makeConfiguration()

		#expect(draft.workspaceRoot == "/Users/charlie/Developer")
		#expect(draft.redactedValues == "literal-signing-secret\nsecond-local-secret")
		#expect(saved.workspaceRoot == configuration.workspaceRoot)
		#expect(saved.redactedValues == configuration.redactedValues)
	}

	@Test
	func makeConfigurationRejectsMalformedRedactedValueKeychainReference() {
		var draft = ConfigurationDraft()
		draft.machineId = "machine-123"
		draft.machineName = "Mac"
		draft.sharedToken = "token"
		draft.redactedValues = "keychain://co.charliewil.transwarp/machine-123/redacted_values/0?copy=true"
		draft.jobId = "build"
		draft.jobLabel = "Build"
		draft.jobWorkingDirectory = "/tmp"
		draft.jobCheckout = false
		draft.jobCommand = "/usr/bin/env"
		draft.jobArguments = ""
		draft.jobTimeoutSeconds = 60

		#expect(throws: AgentConfigurationValidationError.self) {
			try draft.makeConfiguration()
		}
	}

	@Test
	func makeConfigurationRoundTripsAllowedReportOrigins() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			ciAccessClientID: "access-id",
			ciAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/ci_access_client_secret",
			runnerAccessClientID: "runner-access-id",
			runnerAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/runner_access_client_secret",
			allowedReportOrigins: [
				URL(string: "https://ci.example.com")!,
				URL(string: "http://127.0.0.1:8288")!
			],
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					timeoutSeconds: 60
				)
			]
		)

		let draft = ConfigurationDraft(configuration: configuration)
		let saved = try draft.makeConfiguration()

		#expect(draft.allowedReportOrigins == "https://ci.example.com\nhttp://127.0.0.1:8288")
		#expect(draft.ciAccessClientID == "access-id")
		#expect(draft.ciAccessClientSecret == "keychain://co.charliewil.transwarp/machine-123/ci_access_client_secret")
		#expect(draft.runnerAccessClientID == "runner-access-id")
		#expect(draft.runnerAccessClientSecret == "keychain://co.charliewil.transwarp/machine-123/runner_access_client_secret")
		#expect(saved.allowedReportOrigins == configuration.allowedReportOrigins)
		#expect(saved.ciAccessClientID == configuration.ciAccessClientID)
		#expect(saved.ciAccessClientSecret == configuration.ciAccessClientSecret)
		#expect(saved.runnerAccessClientID == configuration.runnerAccessClientID)
		#expect(saved.runnerAccessClientSecret == configuration.runnerAccessClientSecret)
	}

	@Test
	func makeConfigurationRoundTripsCloudflareTunnelTokenReference() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(
				mode: .named,
				cloudflaredPath: "@bundle/cloudflared",
				token: "keychain://co.charliewil.transwarp/machine-123/cloudflare_tunnel_token",
				name: "transwarp-desktop",
				publicURL: URL(string: "https://runner.example.com")
			),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					timeoutSeconds: 60
				)
			]
		)

		let draft = ConfigurationDraft(configuration: configuration)
		let saved = try draft.makeConfiguration()

		#expect(draft.tunnelToken == "keychain://co.charliewil.transwarp/machine-123/cloudflare_tunnel_token")
		#expect(saved.tunnel.token == configuration.tunnel.token)
		#expect(saved.tunnel.name == "transwarp-desktop")
		#expect(saved.tunnel.publicURL == URL(string: "https://runner.example.com"))
	}

	@Test
	func applyCoordinatorBaseURLFillsLifecycleEndpoints() throws {
		var draft = ConfigurationDraft()

		try draft.applyCoordinatorBaseURL("http://127.0.0.1:8288")

		#expect(draft.ciRegistrationURL == "http://127.0.0.1:8288/transwarp/register")
		#expect(draft.ciHeartbeatURL == "http://127.0.0.1:8288/transwarp/heartbeat")
		#expect(draft.ciDeregistrationURL == "http://127.0.0.1:8288/transwarp/deregister")
	}

	@Test
	func applyCoordinatorBaseURLRejectsNonBaseURL() {
		var draft = ConfigurationDraft()

		#expect(throws: ConfigurationDraftError.self) {
			try draft.applyCoordinatorBaseURL("https://ci.example.com/base")
		}
		#expect(throws: ConfigurationDraftError.self) {
			try draft.applyCoordinatorBaseURL("https://user:secret@ci.example.com")
		}
	}

	@Test
	func inferredCoordinatorBaseURLReadsStandardRegistrationEndpoint() {
		let base = ConfigurationDraft.inferredCoordinatorBaseURL(
			registrationURL: "https://ci.example.com:8443/transwarp/register"
		)

		#expect(base == "https://ci.example.com:8443")
		#expect(ConfigurationDraft.inferredCoordinatorBaseURL(registrationURL: "https://ci.example.com/custom/register") == "")
	}

	@Test
	func makeConfigurationPreservesAdditionalJobs() throws {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "primary",
					label: "Primary",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					timeoutSeconds: 60
				),
				BuildJob(
					id: "release",
					label: "Release",
					workingDirectory: "/tmp",
					command: "/usr/bin/xcodebuild",
					arguments: ["-scheme", "App", "archive"],
					timeoutSeconds: 3600
				)
			]
		)

		var draft = ConfigurationDraft(configuration: configuration)
		draft.jobLabel = "Primary Edited"

		let saved = try draft.makeConfiguration()

		#expect(saved.jobs.map(\.id) == ["primary", "release"])
		#expect(saved.jobs[0].label == "Primary Edited")
		#expect(saved.jobs[1] == configuration.jobs[1])
	}
}
