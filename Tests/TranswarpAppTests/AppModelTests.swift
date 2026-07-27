import Testing
@testable import TranswarpApp
import TranswarpCore

@Suite
struct AppModelTests {
	@Test
	func localTestBuildUsesFirstNonCheckoutJob() {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "ci-xcode",
					label: "CI Xcode",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					command: "/usr/bin/xcodebuild",
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

		#expect(AppModel.localTestJobID(in: configuration) == "local-smoke")
	}

	@Test
	func localTestBuildUnavailableWithoutNonCheckoutJob() {
		let configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "token",
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "ci-xcode",
					label: "CI Xcode",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					command: "/usr/bin/xcodebuild",
					timeoutSeconds: 3600
				)
			]
		)

		#expect(AppModel.localTestJobID(in: configuration) == nil)
	}

	@Test
	func runnerErrorEventBecomesVisibleLastError() {
		let current = AppModel.lastError(
			after: RunnerEvent(kind: .error, message: "cloudflared exited unexpectedly"),
			current: "Previous error"
		)

		#expect(current == "cloudflared exited unexpectedly")
	}

	@Test
	func nonErrorRunnerEventPreservesVisibleLastError() {
		let current = AppModel.lastError(
			after: RunnerEvent(kind: .tunnel, message: "started cloudflared"),
			current: "Previous error"
		)

		#expect(current == "Previous error")
	}

	@Test
	func reportFailureMessagePromotesCallbackFailure() {
		let message = AppModel.reportFailureMessage(for: BuildStatus(
			buildId: "build-123",
			jobId: "xcode-debug",
			status: "passed",
			createdAt: "2026-07-26T10:00:00Z",
			reportStatus: "failed",
			reportError: "POST https://ci.example.com/transwarp/result failed"
		))

		#expect(message == "CI result report failed for xcode-debug build-123: POST https://ci.example.com/transwarp/result failed")
	}

	@Test
	func reportFailureMessageIgnoresReportedCallback() {
		let message = AppModel.reportFailureMessage(for: BuildStatus(
			buildId: "build-123",
			jobId: "xcode-debug",
			status: "passed",
			createdAt: "2026-07-26T10:00:00Z",
			reportStatus: "reported"
		))

		#expect(message == nil)
	}

	@Test
	func controllableBuildsIncludeActiveAndQueuedBuilds() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "off",
			tunnel: TunnelStatus(mode: "off", state: "disabled"),
			publicURL: nil,
			activeBuilds: 1,
			queuedBuilds: 1,
			jobs: ["xcode-debug"],
			recentBuilds: [
				BuildStatus(buildId: "build-passed", jobId: "xcode-debug", status: "passed", createdAt: "2026-07-26T10:00:00Z"),
				BuildStatus(buildId: "build-queued", jobId: "xcode-debug", status: "queued", createdAt: "2026-07-26T10:01:00Z"),
				BuildStatus(buildId: "build-running", jobId: "xcode-debug", status: "running", createdAt: "2026-07-26T10:02:00Z")
			]
		)

		#expect(AppModel.activeBuild(in: status)?.buildId == "build-running")
		#expect(AppModel.queuedBuilds(in: status).map(\.buildId) == ["build-queued"])
		#expect(AppModel.controllableBuilds(in: status).map(\.buildId) == ["build-queued", "build-running"])
	}

	@Test
	func buildStatusLookupFindsRequestedBuild() {
		let status = AgentStatus(
			machineName: "Mac",
			machineId: "machine-123",
			listenAddress: "127.0.0.1:8188",
			tunnelMode: "off",
			tunnel: TunnelStatus(mode: "off", state: "disabled"),
			publicURL: nil,
			activeBuilds: 0,
			jobs: ["xcode-debug"],
			recentBuilds: [
				BuildStatus(buildId: "build-older", jobId: "xcode-debug", status: "failed", createdAt: "2026-07-26T10:01:00Z"),
				BuildStatus(buildId: "build-target", jobId: "xcode-debug", status: "passed", createdAt: "2026-07-26T10:02:00Z")
			]
		)

		#expect(AppModel.buildStatus(in: status, buildID: "build-target")?.status == "passed")
		#expect(AppModel.buildStatus(in: status, buildID: "build-missing") == nil)
	}

	@Test
	func testBuildCompletionEventsDescribeTerminalResult() {
		let passed = AppModel.testBuildCompletionEvent(for: BuildStatus(
			buildId: "build-pass",
			jobId: "xcode-debug",
			status: "passed",
			createdAt: "2026-07-26T10:00:00Z"
		))
		let failed = AppModel.testBuildCompletionEvent(for: BuildStatus(
			buildId: "build-fail",
			jobId: "xcode-debug",
			status: "failed",
			createdAt: "2026-07-26T10:00:00Z",
			result: BuildResult(
				buildId: "build-fail",
				jobId: "xcode-debug",
				startedAt: "2026-07-26T10:00:00Z",
				endedAt: "2026-07-26T10:01:00Z",
				exitCode: 65,
				error: "xcodebuild exited with 65"
			)
		))
		let canceled = AppModel.testBuildCompletionEvent(for: BuildStatus(
			buildId: "build-cancel",
			jobId: "xcode-debug",
			status: "canceled",
			createdAt: "2026-07-26T10:00:00Z"
		))

		#expect(passed.kind == .build)
		#expect(passed.message == "Test build build-pass passed")
		#expect(failed.kind == .error)
		#expect(failed.message == "Test build build-fail failed: xcodebuild exited with 65")
		#expect(canceled.kind == .build)
		#expect(canceled.message == "Test build build-cancel canceled")
	}

	@Test
	func copySecretValuesRequiresModeExportableLocalValues() {
		let missingPublicURL = AgentConfiguration(
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
		let ready = AgentConfiguration.sample(machineId: "machine-123")

		#expect(!AppModel.canCopyGitHubActionSecretValues(mode: .direct, configuration: missingPublicURL))
		#expect(!AppModel.canCopyGitHubActionSecretValues(mode: .releaseEvidence, configuration: missingPublicURL))
		#expect(AppModel.canCopyGitHubActionSecretValues(mode: .direct, configuration: ready))
		#expect(AppModel.canCopyGitHubActionSecretValues(mode: .releaseEvidence, configuration: ready))
		#expect(!AppModel.canCopyGitHubActionSecretValues(mode: .selfHosted, configuration: ready))
	}

	@Test
	func copySecretValuesBlockedByConfigurationIssues() {
		let configuration = AgentConfiguration.sample(machineId: "machine-123")

		#expect(!AppModel.canCopyGitHubActionSecretValues(
			mode: .direct,
			configuration: configuration,
			configurationIssues: ["Named tunnel public URL must be a base URL like https://transwarp.example.com."]
		))
	}

	@Test
	func hostSupportIssueAcceptsModernAppleSiliconMac() {
		#expect(AppModel.hostSupportIssue(osMajorVersion: 14, architecture: "arm64") == nil)
		#expect(AppModel.hostSupportIssue(osMajorVersion: 26, architecture: "arm64") == nil)
	}

	@Test
	func hostSupportIssueRejectsIntelMac() {
		#expect(AppModel.hostSupportIssue(osMajorVersion: 15, architecture: "x86_64") == "Transwarp requires an Apple Silicon Mac.")
	}

	@Test
	func hostSupportIssueRejectsOlderMacOS() {
		#expect(AppModel.hostSupportIssue(osMajorVersion: 13, architecture: "arm64") == "Transwarp requires macOS 14 or newer.")
	}
}
