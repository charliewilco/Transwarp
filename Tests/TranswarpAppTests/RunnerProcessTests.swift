import Foundation
import Testing
import TranswarpCore
@testable import TranswarpApp

@Suite
struct RunnerProcessTests {
	@Test
	func stoppingStatusIncludesPID() {
		#expect(RunnerProcess.Status.stopping(pid: 123).label == "Stopping, PID 123")
	}

	@Test
	func stoppingStatusIsActiveButNotRunning() {
		let status = RunnerProcess.Status.stopping(pid: 123)

		#expect(status.isActive)
		#expect(!status.isRunning)
	}

	@Test
	func resolvedRuntimeConfigurationPreservesPlainValues() throws {
		var configuration = AgentConfiguration.starter(machineId: "machine-123", sharedToken: "runner-token")
		configuration.registrationToken = "registration-token"
		configuration.ciAccessClientID = "access-client-id"
		configuration.ciAccessClientSecret = "access-client-secret"
		configuration.runnerAccessClientID = "runner-access-client-id"
		configuration.runnerAccessClientSecret = "runner-access-client-secret"
		configuration.tunnel.token = "tunnel-token"
		configuration.redactedValues = ["literal-redaction-secret"]
		configuration.jobs[0].checkout = true
		configuration.jobs[0].allowedRepositories = ["https://github.com/example/app.git"]
		configuration.jobs[0].checkoutAuthorizationHeader = "Authorization: Bearer local-token"
		configuration.jobs[0].environment = [
			"MATCH_PASSWORD": "match-password",
			"SDKROOT": "macosx"
		]

		let resolved = try RunnerProcess.resolvedRuntimeConfiguration(configuration)
		let data = try AgentConfigurationStore.encode(resolved)
		let text = String(decoding: data, as: UTF8.self)

		#expect(resolved.sharedToken == "runner-token")
		#expect(resolved.registrationToken == "registration-token")
		#expect(resolved.ciAccessClientSecret == "access-client-secret")
		#expect(resolved.runnerAccessClientID == "runner-access-client-id")
		#expect(resolved.runnerAccessClientSecret == "runner-access-client-secret")
		#expect(resolved.tunnel.token == "tunnel-token")
		#expect(resolved.redactedValues == ["literal-redaction-secret"])
		#expect(resolved.jobs[0].checkoutAuthorizationHeader == "Authorization: Bearer local-token")
		#expect(resolved.jobs[0].environment["MATCH_PASSWORD"] == "match-password")
		#expect(text.contains("\"ci_access_client_secret\""))
		#expect(text.contains("\"runner_access_client_secret\""))
		#expect(text.contains("\"shared_token\""))
		#expect(!text.contains("\"sharedToken\""))
	}

	@Test
	func runnerEnvironmentDoesNotInheritArbitrarySecrets() {
		let environment = RunnerProcess.runnerEnvironment(
			processEnvironment: [
				"HOME": "/Users/charlie",
				"PATH": "/tmp/secret-tools:/usr/bin:/bin",
				"USER": "charlie",
				"API_TOKEN": "should-not-cross",
				"TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN": "should-not-cross"
			],
			resourceURL: nil,
			parentProcessID: 123
		)

		#expect(environment["HOME"] == "/Users/charlie")
		#expect(environment["PATH"] == "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin")
		#expect(environment["USER"] == "charlie")
		#expect(environment["TRANSWARP_PARENT_PID"] == "123")
		#expect(environment["API_TOKEN"] == nil)
		#expect(environment["TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN"] == nil)
	}

	@Test
	func runnerEnvironmentAddsBundledCloudflaredPath() throws {
		let resourceURL = URL(filePath: NSTemporaryDirectory())
			.appending(path: "TranswarpResources-\(UUID().uuidString)")
		try FileManager.default.createDirectory(at: resourceURL, withIntermediateDirectories: true)
		defer {
			try? FileManager.default.removeItem(at: resourceURL)
		}

		let environment = RunnerProcess.runnerEnvironment(
			processEnvironment: [:],
			resourceURL: resourceURL,
			parentProcessID: 123,
			isExecutable: { path in path == resourceURL.appending(path: "cloudflared").path }
		)

		#expect(environment["PATH"] == "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin")
		#expect(environment["TRANSWARP_BUNDLE_RESOURCES"] == resourceURL.path)
		#expect(environment["TRANSWARP_CLOUDFLARED_PATH"] == resourceURL.appending(path: "cloudflared").path)
	}

	@Test
	func parseEventPreservesRunnerMetadata() throws {
		let event = try #require(RunnerProcess.parseEvent("""
		{"kind":"build","message":"starting Xcode Debug","build_id":"build-123","job_id":"xcode-debug","sequence":42,"time":"2026-07-26T22:24:02.223431Z"}
		"""))
		let formatter = ISO8601DateFormatter()
		formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
		let expectedDate = try #require(formatter.date(from: "2026-07-26T22:24:02.223431Z"))

		#expect(event.kind == .build)
		#expect(event.message == "starting Xcode Debug")
		#expect(event.buildId == "build-123")
		#expect(event.jobId == "xcode-debug")
		#expect(event.sequence == 42)
		#expect(event.date == expectedDate)
	}

	@Test
	func outputEventsBufferPartialRunnerLines() throws {
		var buffer = Data()
		let first = RunnerProcess.events(
			from: Data(#"{"kind":"build","message":"starting Xcode Debug","build_id":"build-123""#.utf8),
			buffer: &buffer
		)
		let second = RunnerProcess.events(
			from: Data((#","job_id":"xcode-debug","sequence":42}"# + "\nplain log line\n").utf8),
			buffer: &buffer
		)

		#expect(first.isEmpty)
		#expect(second.count == 2)
		#expect(second[0].kind == .build)
		#expect(second[0].buildId == "build-123")
		#expect(second[0].jobId == "xcode-debug")
		#expect(second[0].sequence == 42)
		#expect(second[1].kind == .log)
		#expect(second[1].message == "plain log line")
		#expect(buffer.isEmpty)
	}

	@Test
	func outputEventsFlushUnterminatedLine() {
		var buffer = Data("unterminated log line".utf8)
		let events = RunnerProcess.flushEvents(buffer: &buffer)

		#expect(events.count == 1)
		#expect(events[0].kind == .log)
		#expect(events[0].message == "unterminated log line")
		#expect(buffer.isEmpty)
	}

	@Test
	func outputEventsPreserveMultibyteScalarsAcrossChunks() {
		let bytes = Array("build says 🚀\n".utf8)
		var buffer = Data(bytes[..<13])
		let events = RunnerProcess.events(from: Data(bytes[13...]), buffer: &buffer)

		#expect(events.count == 1)
		#expect(events[0].kind == .log)
		#expect(events[0].message == "build says 🚀")
		#expect(buffer.isEmpty)
	}

	@Test
	@MainActor
	func stopEscalatesStuckRunnerProcess() async throws {
		let directory = URL(filePath: NSTemporaryDirectory())
			.appending(path: "TranswarpRunnerProcess-\(UUID().uuidString)")
		try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
		defer {
			try? FileManager.default.removeItem(at: directory)
		}

		let executable = directory.appending(path: "fake-runner")
		try """
		#!/usr/bin/perl
		$| = 1;
		$SIG{INT} = sub {};
		$SIG{TERM} = sub {};
		print "{\\"kind\\":\\"info\\",\\"message\\":\\"fake runner ready\\"}\\n";
		while (1) {
			select undef, undef, undef, 1;
		}
		""".write(to: executable, atomically: true, encoding: .utf8)
		try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)

		let configurationURL = directory.appending(path: "agent.json")
		try AgentConfigurationStore.save(
			AgentConfiguration.starter(machineId: "test-runner-process", sharedToken: "runner-token"),
			to: configurationURL
		)

		let process = RunnerProcess(
			runnerExecutable: executable,
			terminateGraceDuration: .milliseconds(30),
			killGraceDuration: .milliseconds(30)
		)
		var events: [RunnerEvent] = []
		var statuses: [RunnerProcess.Status] = []
		process.onEvent = { events.append($0) }
		process.onStatusChange = { statuses.append($0) }

		try process.start(configurationPath: configurationURL)
		for _ in 0..<80 where !events.contains(where: { $0.message == "fake runner ready" }) {
			try await Task.sleep(for: .milliseconds(25))
		}
		#expect(events.contains { $0.message == "fake runner ready" })
		process.stop()
		for _ in 0..<100 {
			let sawTerminate = events.contains { $0.message == "Runner did not stop after interrupt; sent terminate" }
			let sawKill = events.contains { $0.message == "Runner did not stop after terminate; sent kill" }
			let sawFailureStatus = statuses.contains {
				if case .stoppedWithFailure = $0 {
					return true
				}
				return false
			}
			if sawTerminate && sawKill && sawFailureStatus {
				break
			}
			try await Task.sleep(for: .milliseconds(25))
		}

		#expect(events.contains { $0.message == "Runner did not stop after interrupt; sent terminate" })
		#expect(events.contains { $0.message == "Runner did not stop after terminate; sent kill" })
		#expect(statuses.contains {
			if case .stoppedWithFailure = $0 {
				return true
			}
			return false
		})
	}
}
