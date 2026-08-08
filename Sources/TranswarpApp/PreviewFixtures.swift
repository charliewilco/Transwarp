import Foundation
import TranswarpCore

extension AgentConfiguration {
	static var previewReady: AgentConfiguration {
		AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "preview-mac",
			machineName: "Preview Mac",
			sharedToken: SecretReference(service: SecretReference.defaultService, account: "preview/shared_token").rawValue,
			ciRegistrationURL: URL(string: "https://ci.example.com/transwarp/register"),
			ciHeartbeatURL: URL(string: "https://ci.example.com/transwarp/heartbeat"),
			ciDeregistrationURL: URL(string: "https://ci.example.com/transwarp/deregister"),
			registrationToken: SecretReference(service: SecretReference.defaultService, account: "preview/registration_token").rawValue,
			tunnel: TunnelConfiguration(
				mode: .named,
				cloudflaredPath: TunnelConfiguration.bundledCloudflaredPath,
				token: SecretReference(service: SecretReference.defaultService, account: "preview/cloudflare_tunnel_token").rawValue,
				publicURL: URL(string: "https://transwarp.example.com")
			),
			jobs: [
				BuildJob(
					id: "xcode-debug",
					label: "Xcode Debug",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					command: "/usr/bin/xcodebuild",
					arguments: ["-scheme", "App", "-configuration", "Debug", "build"],
					timeoutSeconds: 3600
				),
				BuildJob(
					id: "local-smoke",
					label: "Local Smoke",
					workingDirectory: "/tmp",
					command: "/usr/bin/xcodebuild",
					arguments: ["-version"],
					timeoutSeconds: 300
				)
			]
		)
	}
}

extension AppModel {
	static func preview(
		status: RunnerProcess.Status = .stopped,
		configuration: AgentConfiguration = .previewReady,
		agentStatus: AgentStatus? = nil,
		configurationIssues: [String] = [],
		events: [RunnerEvent] = []
	) -> AppModel {
		let model = AppModel(dependencies: .fixture(configuration: configuration))
		model.status = status
		model.agentStatus = agentStatus
		model.configurationIssues = configurationIssues
		model.events = events
		model.lastError = events.last(where: { $0.kind == .error })?.message
		return model
	}

	static var previewNeedsSetup: AppModel {
		preview(
			configuration: AgentConfiguration.starter(machineId: "preview-needs-setup", sharedToken: ""),
			configurationIssues: ["Runner token is required."]
		)
	}

	static var previewStopped: AppModel {
		preview(status: .stopped)
	}

	static var previewAvailable: AppModel {
		preview(status: .running(pid: 12345), agentStatus: .previewAvailable)
	}

	static var previewPaused: AppModel {
		preview(status: .running(pid: 12345), agentStatus: .previewPaused)
	}

	static var previewQueued: AppModel {
		preview(status: .running(pid: 12345), agentStatus: .previewQueued)
	}

	static var previewRunning: AppModel {
		preview(status: .running(pid: 12345), agentStatus: .previewRunning)
	}

	static var previewPassed: AppModel {
		preview(status: .running(pid: 12345), agentStatus: .previewPassed)
	}

	static var previewFailed: AppModel {
		preview(status: .running(pid: 12345), agentStatus: .previewFailed)
	}

	static var previewError: AppModel {
		preview(
			status: .stoppedWithFailure(1),
			agentStatus: .previewFailed,
			events: [.init(kind: .error, message: "cloudflared exited before tunnel readiness")]
		)
	}

	static var previewExpandedActivity: AppModel {
		preview(
			status: .running(pid: 12345),
			agentStatus: .previewRunning,
			events: [
				.init(kind: .info, message: "Started transwarp-runner"),
				.init(kind: .tunnel, message: "Named tunnel connected"),
				.init(kind: .registration, message: "Registered Preview Mac"),
				.init(kind: .build, message: "starting xcode-debug", buildId: "build-running", jobId: "xcode-debug")
			]
		)
	}
}

extension AgentStatus {
	static var previewAvailable: AgentStatus {
		AgentStatus(
			machineName: "Preview Mac",
			machineId: "preview-mac",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "named",
			tunnel: TunnelStatus(mode: "named", state: "running", publicURL: "https://transwarp.example.com", connected: true, ready: true),
			registration: RegistrationStatus(configured: true, state: "registered", leaseExpiresAt: "2099-01-01T00:00:00Z"),
			publicURL: URL(string: "https://transwarp.example.com"),
			acceptingBuilds: true,
			ciAcceptingBuilds: true,
			activeBuilds: 0,
			queuedBuilds: 0,
			jobs: ["xcode-debug", "local-smoke"]
		)
	}

	static var previewPaused: AgentStatus {
		var status = previewAvailable
		status.ciAcceptingBuilds = false
		return status
	}

	static var previewQueued: AgentStatus {
		var status = previewAvailable
		status.activeBuilds = 1
		status.queuedBuilds = 1
		status.recentBuilds = [
			BuildStatus(buildId: "build-queued", jobId: "xcode-debug", status: "queued", createdAt: "2026-08-08T12:00:00Z")
		]
		return status
	}

	static var previewRunning: AgentStatus {
		var status = previewAvailable
		status.activeBuilds = 1
		status.recentBuilds = [
			BuildStatus(buildId: "build-running", jobId: "xcode-debug", status: "running", createdAt: "2026-08-08T12:00:00Z")
		]
		return status
	}

	static var previewPassed: AgentStatus {
		var status = previewAvailable
		status.recentBuilds = [
			BuildStatus(
				buildId: "build-passed",
				jobId: "xcode-debug",
				status: "passed",
				createdAt: "2026-08-08T12:00:00Z",
				result: BuildResult(
					buildId: "build-passed",
					jobId: "xcode-debug",
					startedAt: "2026-08-08T12:00:00Z",
					endedAt: "2026-08-08T12:01:00Z",
					exitCode: 0
				)
			)
		]
		return status
	}

	static var previewFailed: AgentStatus {
		var status = previewAvailable
		status.recentBuilds = [
			BuildStatus(
				buildId: "build-failed",
				jobId: "xcode-debug",
				status: "failed",
				createdAt: "2026-08-08T12:00:00Z",
				result: BuildResult(
					buildId: "build-failed",
					jobId: "xcode-debug",
					startedAt: "2026-08-08T12:00:00Z",
					endedAt: "2026-08-08T12:01:00Z",
					exitCode: 65,
					error: "xcodebuild exited with 65"
				)
			)
		]
		return status
	}
}
