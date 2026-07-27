import Foundation
import Testing
@testable import TranswarpApp
import TranswarpCore

@Suite
struct PublicEndpointDiagnosisTests {
	@Test
	func requestTargetsPublicStatusWithRunnerAndAccessHeaders() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "keychain://co.charliewil.transwarp/machine-123/shared_token",
			ciAccessClientID: "ci-access-client-id",
			ciAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/ci_access_secret",
			runnerAccessClientID: "runner-access-client-id",
			runnerAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/runner_access_secret",
			tunnel: TunnelConfiguration(
				mode: .named,
				token: "keychain://co.charliewil.transwarp/machine-123/cloudflare_tunnel_token",
				publicURL: URL(string: "https://transwarp.example.com")
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

		let request = try PublicEndpointDiagnosis.request(configuration: configuration) { value in
			switch value {
			case "keychain://co.charliewil.transwarp/machine-123/shared_token":
				return "runner-token"
			case "keychain://co.charliewil.transwarp/machine-123/runner_access_secret":
				return "runner-access-client-secret"
			case "keychain://co.charliewil.transwarp/machine-123/ci_access_secret":
				Issue.record("Public endpoint diagnosis must not resolve CI callback Access credentials")
				return "ci-access-client-secret"
			default:
				return value
			}
		}

		#expect(request.url?.absoluteString == "https://transwarp.example.com/status")
		#expect(request.httpMethod == "GET")
		#expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer runner-token")
		#expect(request.value(forHTTPHeaderField: "CF-Access-Client-Id") == "runner-access-client-id")
		#expect(request.value(forHTTPHeaderField: "CF-Access-Client-Secret") == "runner-access-client-secret")
	}

	@Test
	func requestOmitsAccessHeadersWhenPairIsNotConfigured() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://transwarp.example.com")
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

		let request = try PublicEndpointDiagnosis.request(configuration: configuration) { $0 }

		#expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer runner-token")
		#expect(request.value(forHTTPHeaderField: "CF-Access-Client-Id") == nil)
		#expect(request.value(forHTTPHeaderField: "CF-Access-Client-Secret") == nil)
	}

	@Test
	func requestRequiresPublicURL() {
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

		#expect(throws: PublicEndpointDiagnosisError.self) {
			try PublicEndpointDiagnosis.request(configuration: configuration) { $0 }
		}
	}

	@Test
	func validationAcceptsExpectedMachineAndPublicURL() throws {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://transwarp.example.com")
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
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "ready", publicURL: "https://transwarp.example.com/", connected: true, ready: true),
			publicURL: nil,
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		try PublicEndpointDiagnosis.validate(status: status, configuration: configuration)
	}

	@Test
	func validationRejectsUnexpectedMachineID() {
		let configuration = AgentConfiguration(
			machineId: "expected-machine",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://transwarp.example.com")
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
		let status = AgentStatus(
			machineName: "Unexpected Mac",
			machineId: "other-machine",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "ready", publicURL: "https://transwarp.example.com", connected: true, ready: true),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(throws: PublicEndpointDiagnosisError.self) {
			try PublicEndpointDiagnosis.validate(status: status, configuration: configuration)
		}
	}

	@Test
	func validationRejectsUnexpectedPublicURL() {
		let configuration = AgentConfiguration(
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			tunnel: TunnelConfiguration(
				mode: .named,
				publicURL: URL(string: "https://transwarp.example.com")
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
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "ready", publicURL: "https://other.example.com", connected: true, ready: true),
			publicURL: URL(string: "https://other.example.com"),
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(throws: PublicEndpointDiagnosisError.self) {
			try PublicEndpointDiagnosis.validate(status: status, configuration: configuration)
		}
	}

	@Test
	func eventReportsAvailableCITarget() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "ready", connected: true, ready: true),
			registration: RegistrationStatus(
				configured: true,
				state: "registered",
				leaseExpiresAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(300))
			),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			queuedBuilds: 0,
			queuedBuildLimit: 25,
			jobs: ["xcode-debug"]
		)

		let event = PublicEndpointDiagnosis.event(for: status)

		#expect(event.kind == .tunnel)
		#expect(event.message == "Public runner endpoint is reachable and available to CI")
	}

	@Test
	func eventReportsReachableButUnavailableCITarget() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "ready", connected: true, ready: false),
			registration: RegistrationStatus(
				configured: true,
				state: "registered",
				leaseExpiresAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(300))
			),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			queuedBuilds: 0,
			queuedBuildLimit: 25,
			jobs: ["xcode-debug"]
		)

		let event = PublicEndpointDiagnosis.event(for: status)

		#expect(event.kind == .tunnel)
		#expect(event.message == "Public runner endpoint is reachable but not available to CI: Waiting for tunnel")
	}
}
