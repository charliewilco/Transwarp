import Foundation
import Testing
@testable import TranswarpCore

@Suite
struct AgentConfigurationTests {
	@Test
	func sampleConfigurationRoundTrips() throws {
		let sample = AgentConfiguration.sample(machineId: "machine-123")
		let data = try JSONEncoder().encode(sample)
		let decoded = try JSONDecoder().decode(AgentConfiguration.self, from: data)

		#expect(decoded == sample)
	}

	@Test
	func configurationStoreCreatesReadableFile() throws {
		let directory = URL(filePath: NSTemporaryDirectory())
			.appending(path: UUID().uuidString, directoryHint: .isDirectory)
		try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
		let path = directory.appending(path: "agent.json")

		try AgentConfigurationStore.save(.sample(machineId: "machine-123"), to: path)
		let decoded = try AgentConfigurationStore.load(from: path)

		#expect(decoded.machineId == "machine-123")
		#expect(decoded.machineName == AgentConfiguration.sample(machineId: "machine-123").machineName)
		#expect(decoded.jobs.first?.id == "xcode-debug")
	}

	@Test
	func configurationStoreUsesEnvironmentPathOverride() throws {
		let directory = URL(filePath: NSTemporaryDirectory())
			.appending(path: UUID().uuidString, directoryHint: .isDirectory)
		let path = directory.appending(path: "custom-agent.json")
		setenv("TRANSWARP_CONFIG_PATH", path.path, 1)
		defer {
			unsetenv("TRANSWARP_CONFIG_PATH")
		}

		let defaultPath = try AgentConfigurationStore.defaultPath()
		try AgentConfigurationStore.save(.starter(machineId: "machine-123", sharedToken: "token-123"), to: defaultPath)

		#expect(defaultPath == path)
		#expect(FileManager.default.fileExists(atPath: path.path))
	}

	@Test
	func starterConfigurationHasGeneratedSecretsAndNoPlaceholders() throws {
		let starter = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")

		#expect(starter.machineId == "machine-123")
		#expect(starter.sharedToken == "token-123")
		#expect(starter.tunnel.mode == .off)
		#expect(starter.tunnel.cloudflaredPath == TunnelConfiguration.bundledCloudflaredPath)
		#expect(starter.jobs.first?.id == "xcode-version")

		let data = try JSONEncoder().encode(starter)
		let text = String(decoding: data, as: UTF8.self)
		#expect(!text.contains("replace-with"))
	}

	@Test
	func storeWritesSnakeCaseStarterConfiguration() throws {
		let directory = URL(filePath: NSTemporaryDirectory())
			.appending(path: UUID().uuidString, directoryHint: .isDirectory)
		try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
		let path = directory.appending(path: "agent.json")

		try AgentConfigurationStore.save(.starter(machineId: "machine-123", sharedToken: "token-123"), to: path)
		let text = try String(contentsOf: path, encoding: .utf8)

		#expect(text.contains("\"machine_id\""))
		#expect(text.contains("\"shared_token\""))
		#expect(!text.contains("\"machineId\""))
	}

	@Test
	func decodingOlderConfigurationDefaultsAllowedReportOrigins() throws {
		let data = Data("""
		{
			"listen_address": "127.0.0.1:8188",
			"machine_id": "machine-123",
			"machine_name": "Mac",
			"shared_token": "token-123",
			"workspace_root": "",
			"prevent_sleep": true,
			"redacted_values": [],
			"heartbeat_seconds": 30,
			"tunnel": {"mode": "off", "cloudflared_path": "@bundle/cloudflared", "token": "", "name": ""},
			"jobs": [{
				"id": "xcode-version",
				"label": "Xcode Version",
				"working_directory": "/tmp",
				"checkout": false,
				"allowed_repositories": [],
				"command": "/usr/bin/xcodebuild",
				"arguments": ["-version"],
				"environment": {},
				"redacted_environment_keys": [],
				"timeout_seconds": 300
			}]
		}
		""".utf8)

		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase
		let configuration = try decoder.decode(AgentConfiguration.self, from: data)

		#expect(configuration.allowedReportOrigins.isEmpty)
		#expect(configuration.ciAccessClientID.isEmpty)
		#expect(configuration.ciAccessClientSecret.isEmpty)
		#expect(configuration.runnerAccessClientID.isEmpty)
		#expect(configuration.runnerAccessClientSecret.isEmpty)
		#expect(configuration.jobs.first?.checkoutAuthorizationHeader == "")
	}

	@Test
	func validationAcceptsStarterConfiguration() {
		let issues = AgentConfigurationValidator.issues(
			for: .starter(machineId: "machine-123", sharedToken: "token-123"),
			checkFileSystem: false
		)

		#expect(issues.isEmpty)
	}

	@Test
	func validationRejectsUnsafeMachineID() {
		var configuration = AgentConfiguration.starter(machineId: "machine/123", sharedToken: "token-123")

		var issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Machine ID may contain only letters, numbers, dots, underscores, or hyphens.")))

		configuration.machineId = String(repeating: "é", count: 65)
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Machine ID is too long.")))
	}

	@Test
	func validationRejectsUnsafeJobID() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].id = "xcode/debug"

		var issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode/debug ID may contain only letters, numbers, dots, underscores, or hyphens.")))

		let tooLongJobID = String(repeating: "é", count: 65)
		configuration.jobs[0].id = tooLongJobID
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job \(tooLongJobID) ID is too long.")))
	}

	@Test
	func validationRejectsAllowedReportOriginWithPath() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.allowedReportOrigins = [URL(string: "https://ci.example.com/transwarp/result")!]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Allowed report origin must not include a path, query, or fragment.")))
	}

	@Test
	func validationRejectsAllowedReportOriginCredentials() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.allowedReportOrigins = [URL(string: "https://user:password@ci.example.com")!]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Allowed report origin must not include credentials.")))
	}

	@Test
	func validationRejectsRemoteHTTPAllowedReportOrigin() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.allowedReportOrigins = [URL(string: "http://ci.example.com")!]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Allowed report origin must use https unless it targets local loopback.")))
	}

	@Test
	func validationRejectsRemoteHTTPCIEndpoint() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciRegistrationURL = URL(string: "http://ci.example.com/transwarp/register")

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("CI registration URL must use https unless it targets local loopback.")))
	}

	@Test
	func validationAllowsLoopbackHTTPCIEndpoint() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciRegistrationURL = URL(string: "http://127.0.0.1:8288/transwarp/register")
		configuration.ciHeartbeatURL = URL(string: "http://localhost:8288/transwarp/heartbeat")
		configuration.ciDeregistrationURL = URL(string: "http://[::1]:8288/transwarp/deregister")
		configuration.registrationToken = "registration-token"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(!issues.contains(.init("CI registration URL must use https unless it targets local loopback.")))
		#expect(!issues.contains(.init("CI heartbeat URL must use https unless it targets local loopback.")))
		#expect(!issues.contains(.init("CI deregistration URL must use https unless it targets local loopback.")))
	}

	@Test
	func validationRequiresRegistrationTokenForCIEndpoints() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciRegistrationURL = URL(string: "https://ci.example.com/transwarp/register")
		configuration.ciDeregistrationURL = URL(string: "https://ci.example.com/transwarp/deregister")

		var issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Registration token is required when CI registration endpoints are configured.")))

		configuration.ciHeartbeatURL = URL(string: "https://ci.example.com/transwarp/heartbeat")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Registration token is required when CI registration endpoints are configured.")))

		configuration.ciHeartbeatURL = nil
		configuration.ciDeregistrationURL = URL(string: "https://ci.example.com/transwarp/deregister")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Registration token is required when CI registration endpoints are configured.")))
	}

	@Test
	func validationRequiresDeregistrationURLForRegistrationLifecycle() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciRegistrationURL = URL(string: "https://ci.example.com/transwarp/register")
		configuration.registrationToken = "registration-token"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("CI deregistration URL is required when CI registration URL is configured.")))
	}

	@Test
	func validationRequiresRegistrationURLForLifecycleEndpoints() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.registrationToken = "registration-token"
		configuration.ciHeartbeatURL = URL(string: "https://ci.example.com/transwarp/heartbeat")

		var issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("CI registration URL is required when CI heartbeat URL is configured.")))

		configuration.ciHeartbeatURL = nil
		configuration.ciDeregistrationURL = URL(string: "https://ci.example.com/transwarp/deregister")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("CI registration URL is required when CI deregistration URL is configured.")))
	}

	@Test
	func validationRejectsCIEndpointCredentials() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciRegistrationURL = URL(string: "https://user:password@ci.example.com/transwarp/register")

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("CI registration URL must not include credentials.")))
	}

	@Test
	func validationRejectsCIEndpointQueryOrFragment() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciRegistrationURL = URL(string: "https://ci.example.com/transwarp/register?token=secret")
		configuration.ciHeartbeatURL = URL(string: "https://ci.example.com/transwarp/heartbeat#runner")

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("CI registration URL must not include query or fragment.")))
		#expect(issues.contains(.init("CI heartbeat URL must not include query or fragment.")))
	}

	@Test
	func validationRequiresCompleteCIAccessCredentials() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.ciAccessClientID = "access-id"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("CI Access client ID and secret must be provided together.")))
	}

	@Test
	func validationRequiresCompleteRunnerAccessCredentials() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.runnerAccessClientID = "access-id"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Runner Access client ID and secret must be provided together.")))
	}

	@Test
	func validationRejectsHeaderControlCharactersInTokens() {
		var configuration = AgentConfiguration.starter(
			machineId: "machine-123",
			sharedToken: "token-123\nInjected: yes"
		)
		configuration.registrationToken = "registration-token\nInjected: yes"
		configuration.ciAccessClientID = "ci-access-id\nInjected: yes"
		configuration.ciAccessClientSecret = "ci-access-secret\nInjected: yes"
		configuration.runnerAccessClientID = "runner-access-id\nInjected: yes"
		configuration.runnerAccessClientSecret = "runner-access-secret\nInjected: yes"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Runner token must be a single HTTP header value.")))
		#expect(issues.contains(.init("Registration token must be a single HTTP header value.")))
		#expect(issues.contains(.init("CI Access client ID must be a single HTTP header value.")))
		#expect(issues.contains(.init("CI Access client secret must be a single HTTP header value.")))
		#expect(issues.contains(.init("Runner Access client ID must be a single HTTP header value.")))
		#expect(issues.contains(.init("Runner Access client secret must be a single HTTP header value.")))
	}

	@Test
	func validationRequiresLoopbackListenAddress() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.listenAddress = "0.0.0.0:8188"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Listen address must bind to loopback.")))
	}

	@Test
	func validationRequiresAbsoluteWorkspaceRoot() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.workspaceRoot = "relative/workspaces"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Workspace root must be an absolute path.")))
	}

	@Test
	func validationRejectsShellCommands() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].command = "/bin/sh"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version command must not invoke a shell directly.")))
	}

	@Test
	func validationRequiresCheckoutAllowlist() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = []

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version requires at least one allowed repository when checkout is enabled.")))
	}

	@Test
	func validationRejectsCheckoutAllowedRepositoryCredentials() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = ["https://token:secret@github.com/example/app.git"]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version allowed repository must not include credentials.")))
	}

	@Test
	func validationRejectsCheckoutAllowedRepositoryQueryOrFragment() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = [
			"https://github.com/example/app.git?token=secret",
			"https://github.com/example/app.git#token"
		]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version allowed repository must not include query or fragment.")))
	}

	@Test
	func validationRejectsCheckoutAllowedRepositoryControlCharacters() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = [
			"https://github.com/example/app.git\nInjected: true"
		]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version allowed repository must not include control characters.")))
	}

	@Test
	func validationRejectsCheckoutAllowedRepositoryThatIsTooLong() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = [
			"https://github.com/example/" + String(repeating: "a", count: 2049) + ".git"
		]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version allowed repository is too long.")))
	}

	@Test
	func validationRejectsCheckoutAllowedRepositoryThatIsTooLongInBytes() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = [
			"https://github.com/example/" + String(repeating: "é", count: 1024) + ".git"
		]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version allowed repository is too long.")))
	}

	@Test
	func validationRequiresCheckoutForAuthorizationHeader() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].checkout = false
		configuration.jobs[0].checkoutAuthorizationHeader = "Authorization: Bearer local-token"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version checkout authorization header requires checkout to be enabled.")))
	}

	@Test
	func validationRejectsMalformedCheckoutAuthorizationHeader() {
		let cases: [(header: String, issue: AgentConfigurationValidationIssue)] = [
			("Authorization Bearer local-token", .init("Job xcode-version checkout authorization header must use Header-Name: value format.")),
			(": Bearer local-token", .init("Job xcode-version checkout authorization header name is invalid.")),
			("Bad Header: local-token", .init("Job xcode-version checkout authorization header name is invalid.")),
			("Authorization:", .init("Job xcode-version checkout authorization header value is required.")),
			("Authorization: Bearer local-token\nInjected: yes", .init("Job xcode-version checkout authorization header must be a single HTTP header value."))
		]

		for testCase in cases {
			var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
			configuration.jobs[0].checkout = true
			configuration.jobs[0].allowedRepositories = ["https://github.com/example/app.git"]
			configuration.jobs[0].checkoutAuthorizationHeader = testCase.header

			let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

			#expect(issues.contains(testCase.issue))
		}
	}

	@Test
	func validationRequiresAbsoluteWorkingDirectoryForLocalJobs() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].workingDirectory = "relative/path"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version working directory must be an absolute path.")))
	}

	@Test
	func validationRejectsInvalidEnvironmentKeys() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.jobs[0].environment = [
			"API TOKEN": "secret",
			"TRANSWARP_REF": "fake"
		]
		configuration.jobs[0].redactedEnvironmentKeys = [
			"1PASSWORD",
			"TRANSWARP_TOKEN"
		]

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Job xcode-version environment key API TOKEN is invalid.")))
		#expect(issues.contains(.init("Job xcode-version redacted environment key 1PASSWORD is invalid.")))
		#expect(issues.contains(.init("Job xcode-version environment key TRANSWARP_REF uses the reserved TRANSWARP_ prefix.")))
		#expect(issues.contains(.init("Job xcode-version redacted environment key TRANSWARP_TOKEN uses the reserved TRANSWARP_ prefix.")))
	}

	@Test
		func validationChecksNamedTunnelRequirements() {
			var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
			configuration.tunnel.mode = .named

			let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

			#expect(issues.contains(.init("Named tunnels require a Cloudflare tunnel token.")))
			#expect(issues.contains(.init("Named tunnels require a stable public URL.")))
		}

	@Test
	func validationChecksNamedTunnelPublicURL() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.tunnel.mode = .named
		configuration.tunnel.token = "tunnel-token"
		configuration.tunnel.publicURL = URL(string: "https://transwarp.example.com")

		var issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.isEmpty)

		configuration.tunnel.publicURL = URL(string: "http://transwarp.example.com")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Named tunnel public URL must be an https URL.")))

		configuration.tunnel.publicURL = URL(string: "https://user:password@transwarp.example.com")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Named tunnel public URL must not include credentials.")))

		configuration.tunnel.publicURL = URL(string: "https://transwarp.example.com/status")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Named tunnel public URL must be a base URL like https://transwarp.example.com.")))

		configuration.tunnel.publicURL = URL(string: "https://transwarp.example.com?token=secret")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Named tunnel public URL must be a base URL like https://transwarp.example.com.")))

		configuration.tunnel.publicURL = URL(string: "https://transwarp.example.com#runner")
		issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)
		#expect(issues.contains(.init("Named tunnel public URL must be a base URL like https://transwarp.example.com.")))
	}

	@Test
	func validationAllowsEmptyCloudflaredPathForDevelopmentFallback() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.tunnel.mode = .quick
		configuration.tunnel.cloudflaredPath = ""

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: true)

		#expect(!issues.contains(.init("cloudflared must exist and be executable.")))
	}

	@Test
	func validationChecksBundledCloudflaredWhenTunnelEnabled() {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "token-123")
		configuration.tunnel.mode = .quick
		configuration.tunnel.cloudflaredPath = TunnelConfiguration.bundledCloudflaredPath

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: true)

		#expect(issues.contains(.init("cloudflared must exist and be executable.")))
	}

	@Test
	func secretReferenceRoundTrips() throws {
		let reference = SecretReference(service: SecretReference.defaultService, account: "machine-123/shared_token")
		let parsed = try #require(SecretReference(reference.rawValue))

		#expect(parsed.service == SecretReference.defaultService)
		#expect(parsed.account == "machine-123/shared_token")
		#expect(SecretReference.isReference(reference.rawValue))
		#expect(!SecretReference.isReference("plain-token"))
	}

	@Test
	func secretReferenceRejectsAmbiguousURLParts() {
		let values = [
			"keychain://user@co.charliewil.transwarp/machine-123/shared_token",
			"keychain://co.charliewil.transwarp:443/machine-123/shared_token",
			"keychain://co.charliewil.transwarp/machine-123/shared_token?copy=true",
			"keychain://co.charliewil.transwarp/machine-123/shared_token#fragment"
		]

		for value in values {
			#expect(SecretReference(value) == nil)
			#expect(!SecretReference.isReference(value))
		}
	}

	@Test
	func validationRejectsMalformedKeychainReference() {
		let configuration = AgentConfiguration.starter(
			machineId: "machine-123",
			sharedToken: "keychain://co.charliewil.transwarp/machine-123/shared_token?copy=true"
		)

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Runner token Keychain reference is invalid.")))
	}

	@Test
	func validationRejectsUnsupportedKeychainService() {
		var configuration = AgentConfiguration.starter(
			machineId: "machine-123",
			sharedToken: "keychain://example.com/machine-123/shared_token"
		)
		configuration.registrationToken = "keychain://example.com/machine-123/registration_token"
		configuration.ciAccessClientSecret = "keychain://example.com/machine-123/ci_access_client_secret"
		configuration.runnerAccessClientSecret = "keychain://example.com/machine-123/runner_access_client_secret"
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = ["https://github.com/example/app.git"]
		configuration.jobs[0].checkoutAuthorizationHeader = "keychain://example.com/machine-123/jobs/xcode-version/checkout_authorization_header"

		let issues = AgentConfigurationValidator.issues(for: configuration, checkFileSystem: false)

		#expect(issues.contains(.init("Runner token must use Keychain service \(SecretReference.defaultService).")))
		#expect(issues.contains(.init("Registration token must use Keychain service \(SecretReference.defaultService).")))
		#expect(issues.contains(.init("CI Access client secret must use Keychain service \(SecretReference.defaultService).")))
		#expect(issues.contains(.init("Runner Access client secret must use Keychain service \(SecretReference.defaultService).")))
		#expect(issues.contains(.init("Job xcode-version checkout authorization header must use Keychain service \(SecretReference.defaultService).")))
	}

	@Test
	func agentStatusDecodesRecentBuilds() throws {
		let data = Data("""
		{
			"machine_id": "machine-123",
			"machine_name": "Mac Studio",
			"listen_address": "127.0.0.1:8188",
			"tunnel_mode": "off",
			"tunnel": {"mode": "off", "state": "disabled", "connected": true, "readiness_error": "waiting for public URL"},
			"registration": {
				"configured": true,
				"state": "registered",
				"last_action": "register",
				"last_success_at": "2026-07-26T03:26:20Z",
				"lease_expires_at": "2026-07-26T03:27:50Z"
			},
			"capabilities": {
				"os": "macOS",
				"os_version": "15.6",
				"architecture": "arm64",
				"cpu_brand": "Apple M3 Max",
				"cpu_count": 16,
				"memory_bytes": 68719476736,
				"xcode_version": "Xcode 16.4 (Build version 16F6)",
				"developer_dir": "/Applications/Xcode.app/Contents/Developer"
			},
			"active_builds": 0,
			"queued_builds": 1,
			"jobs": ["xcode-debug"],
			"recent_builds": [{
				"build_id": "build-123",
				"job_id": "xcode-debug",
				"request_id": "run-123",
				"status": "passed",
				"created_at": "2026-07-26T03:26:21Z",
				"report_status": "reported",
				"result": {
					"build_id": "build-123",
					"job_id": "xcode-debug",
					"request_id": "run-123",
					"repo_url": "https://github.com/example/app.git",
					"ref": "refs/heads/main",
					"commit": "abcdef1234567890",
					"started_at": "2026-07-26T03:26:21Z",
					"ended_at": "2026-07-26T03:26:22Z",
					"exit_code": 0
				}
			}]
		}
		""".utf8)
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase

		let status = try decoder.decode(AgentStatus.self, from: data)

		#expect(status.recentBuilds.count == 1)
		#expect(status.recentBuilds[0].buildId == "build-123")
		#expect(status.recentBuilds[0].status == "passed")
		#expect(status.recentBuilds[0].reportStatus == "reported")
		#expect(status.recentBuilds[0].result?.exitCode == 0)
		#expect(status.recentBuilds[0].result?.repoURL == "https://github.com/example/app.git")
		#expect(status.recentBuilds[0].result?.ref == "refs/heads/main")
		#expect(status.recentBuilds[0].result?.commit == "abcdef1234567890")
		#expect(status.queuedBuilds == 1)
		#expect(status.tunnel.connected == true)
		#expect(status.tunnel.readinessError == "waiting for public URL")
		#expect(status.registration?.configured == true)
		#expect(status.registration?.state == "registered")
		#expect(status.registration?.lastAction == "register")
		#expect(status.capabilities?.os == "macOS")
		#expect(status.capabilities?.architecture == "arm64")
		#expect(status.capabilities?.xcodeVersion == "Xcode 16.4 (Build version 16F6)")
	}
}
