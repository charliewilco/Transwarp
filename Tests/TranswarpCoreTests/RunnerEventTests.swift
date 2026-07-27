import Foundation
import Testing
import TranswarpCore

@Suite
struct RunnerEventTests {
	@Test
	func buildStatusReportsRunningOnlyForActiveBuilds() {
		#expect(BuildStatus(buildId: "build-1", jobId: "xcode", status: "running", createdAt: "now").isRunning)
		#expect(!BuildStatus(buildId: "build-2", jobId: "xcode", status: "passed", createdAt: "now").isRunning)
		#expect(!BuildStatus(buildId: "build-3", jobId: "xcode", status: "canceled", createdAt: "now").isRunning)
		#expect(!BuildStatus(buildId: "build-4", jobId: "xcode", status: "queued", createdAt: "now").isRunning)
	}

	@Test
	func buildStatusReportsTerminalOnlyForFinishedBuilds() {
		#expect(!BuildStatus(buildId: "build-1", jobId: "xcode", status: "running", createdAt: "now").isTerminal)
		#expect(!BuildStatus(buildId: "build-2", jobId: "xcode", status: "queued", createdAt: "now").isTerminal)
		#expect(BuildStatus(buildId: "build-3", jobId: "xcode", status: "passed", createdAt: "now").isTerminal)
		#expect(BuildStatus(buildId: "build-4", jobId: "xcode", status: "failed", createdAt: "now").isTerminal)
		#expect(BuildStatus(buildId: "build-5", jobId: "xcode", status: "canceled", createdAt: "now").isTerminal)
	}

	@Test
	func buildStatusReportsQueuedOnlyForQueuedBuilds() {
		#expect(BuildStatus(buildId: "build-1", jobId: "xcode", status: "queued", createdAt: "now").isQueued)
		#expect(!BuildStatus(buildId: "build-2", jobId: "xcode", status: "running", createdAt: "now").isQueued)
		#expect(!BuildStatus(buildId: "build-3", jobId: "xcode", status: "passed", createdAt: "now").isQueued)
	}

	@Test
	func buildStartPayloadUsesRunnerJSONKeys() throws {
		let payload = BuildStartPayload(
			jobId: "xcode-debug",
			requestId: "app-smoke-1",
			repoURL: "https://github.com/example/app.git",
			ref: "refs/heads/main",
			commit: "abc123"
		)
		let data = try JSONEncoder().encode(payload)
		let text = String(decoding: data, as: UTF8.self)

		#expect(text.contains("\"job_id\":\"xcode-debug\""))
		#expect(text.contains("\"request_id\":\"app-smoke-1\""))
		#expect(text.contains("\"repo_url\":\"https:\\/\\/github.com\\/example\\/app.git\""))
		#expect(!text.contains("jobId"))
		#expect(!text.contains("repoURL"))
	}

	@Test
	func buildStartReceiptDecodesRunnerJSONKeys() throws {
		let data = Data("""
		{
			"build_id": "build-123",
			"status": "running",
			"logs_url": "/v1/builds/build-123/logs?after=0&follow=true",
			"cancel_url": "/v1/builds/build-123/cancel"
		}
		""".utf8)

		let receipt = try JSONDecoder().decode(BuildStartReceipt.self, from: data)

		#expect(receipt.buildId == "build-123")
		#expect(receipt.logsURL == "/v1/builds/build-123/logs?after=0&follow=true")
		#expect(receipt.cancelURL == "/v1/builds/build-123/cancel")
	}

	@Test
	func agentStatusReportsAvailableCITargetWhenRegisteredAndTunnelReady() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "running", publicURL: "https://transwarp.example.com", connected: true, ready: true),
			registration: RegistrationStatus(configured: true, state: "registered", leaseExpiresAt: "2999-07-26T03:27:50Z"),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Available to CI")
	}

	@Test
	func agentStatusDoesNotReportAvailableCITargetWithMissingLease() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "running", publicURL: "https://transwarp.example.com", connected: true, ready: true),
			registration: RegistrationStatus(configured: true, state: "registered"),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Missing registration lease")
	}

	@Test
	func agentStatusDoesNotReportAvailableCITargetWithExpiredLease() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "running", publicURL: "https://transwarp.example.com", connected: true, ready: true),
			registration: RegistrationStatus(configured: true, state: "registered", leaseExpiresAt: "2000-07-26T03:27:50Z"),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Registration lease expired")
	}

	@Test
	func agentStatusDoesNotReportAvailableCITargetWhenQueueIsFull() throws {
		let data = Data("""
		{
			"machine_id": "machine-123",
			"machine_name": "Mac",
			"listen_address": "127.0.0.1:8188",
			"tunnel_mode": "named",
			"tunnel": {
				"mode": "named",
				"state": "running",
				"public_url": "https://transwarp.example.com",
				"connected": true,
				"ready": true
			},
			"registration": {
				"configured": true,
				"state": "registered",
				"lease_expires_at": "2999-07-26T03:27:50Z"
			},
			"public_url": "https://transwarp.example.com",
			"active_builds": 1,
			"queued_builds": 25,
			"queued_build_limit": 25,
			"jobs": ["xcode-debug"],
			"recent_builds": []
		}
		""".utf8)
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase

		let status = try decoder.decode(AgentStatus.self, from: data)

		#expect(status.isQueueFull)
		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Queue full")
	}

	@Test
	func agentStatusDoesNotReportAvailableCITargetWhenPaused() throws {
		let data = Data("""
		{
			"machine_id": "machine-123",
			"machine_name": "Mac",
			"listen_address": "127.0.0.1:8188",
			"tunnel_mode": "named",
			"tunnel": {
				"mode": "named",
				"state": "running",
				"public_url": "https://transwarp.example.com",
				"connected": true,
				"ready": true
			},
			"registration": {
				"configured": true,
				"state": "registered",
				"lease_expires_at": "2999-07-26T03:27:50Z"
			},
			"public_url": "https://transwarp.example.com",
			"accepting_builds": false,
			"active_builds": 0,
			"queued_builds": 0,
			"queued_build_limit": 25,
			"jobs": ["xcode-debug"],
			"recent_builds": []
		}
		""".utf8)
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase

		let status = try decoder.decode(AgentStatus.self, from: data)

		#expect(!status.isAcceptingBuilds)
		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Paused")
	}

	@Test
	func agentStatusUsesEffectiveCIAvailabilityWhenPresent() throws {
		let data = Data("""
		{
			"machine_id": "machine-123",
			"machine_name": "Mac",
			"listen_address": "127.0.0.1:8188",
			"tunnel_mode": "named",
			"tunnel": {
				"mode": "named",
				"state": "running",
				"public_url": "https://transwarp.example.com",
				"connected": true,
				"ready": true
			},
			"registration": {
				"configured": true,
				"state": "registered",
				"lease_expires_at": "2999-07-26T03:27:50Z"
			},
			"public_url": "https://transwarp.example.com",
			"accepting_builds": true,
			"ci_accepting_builds": false,
			"active_builds": 0,
			"queued_builds": 0,
			"queued_build_limit": 25,
			"jobs": ["xcode-debug"],
			"recent_builds": []
		}
		""".utf8)
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase

		let status = try decoder.decode(AgentStatus.self, from: data)

		#expect(status.isAcceptingBuilds)
		#expect(!status.isCIAcceptingBuilds)
		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Paused")
	}

	@Test
	func availabilityUpdatePayloadUsesRunnerJSONKey() throws {
		let payload = AvailabilityUpdatePayload(acceptingBuilds: false)
		let data = try JSONEncoder().encode(payload)
		let text = String(decoding: data, as: UTF8.self)

		#expect(text.contains("\"accepting_builds\":false"))
		#expect(!text.contains("acceptingBuilds"))
	}

	@Test
	func registrationLeaseAcceptsFractionalTimestamp() throws {
		let registration = RegistrationStatus(
			configured: true,
			state: "registered",
			leaseExpiresAt: "2999-07-26T15:11:39.361634-04:00"
		)

		#expect(registration.hasLiveLease())
	}

	@Test
	func agentStatusDoesNotReportAvailableCITargetBeforeTunnelReadiness() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "running", connected: true, ready: false, readinessError: "waiting for DNS"),
			registration: RegistrationStatus(configured: true, state: "registered", leaseExpiresAt: "2999-07-26T03:27:50Z"),
			publicURL: URL(string: "https://transwarp.example.com"),
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Waiting for tunnel")
	}

	@Test
	func agentStatusDoesNotReportAvailableCITargetWithoutRegistration() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "off",
			tunnel: TunnelStatus(mode: "off", state: "disabled"),
			registration: RegistrationStatus(configured: false, state: "disabled"),
			publicURL: nil,
			activeBuilds: 0,
			jobs: ["xcode-debug"]
		)

		#expect(!status.isAvailableCITarget)
		#expect(status.ciTargetSummary == "Registration off")
	}
}
