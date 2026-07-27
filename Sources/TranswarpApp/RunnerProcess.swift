import Foundation
import Darwin
import TranswarpCore

@MainActor
final class RunnerProcess {
	enum Status: Equatable {
		case stopped
		case starting
		case running(pid: Int32)
		case stopping(pid: Int32)
		case stoppedWithFailure(Int32)

		var label: String {
			switch self {
			case .stopped:
				"Stopped"
			case .starting:
				"Starting"
			case let .running(pid):
				"Running, PID \(pid)"
			case let .stopping(pid):
				"Stopping, PID \(pid)"
			case let .stoppedWithFailure(code):
				"Stopped, exit \(code)"
			}
		}

		var isRunning: Bool {
			if case .running = self {
				return true
			}
			return false
		}

		var isActive: Bool {
			switch self {
			case .starting, .running, .stopping:
				true
			case .stopped, .stoppedWithFailure:
				false
			}
		}
	}

	var onEvent: ((RunnerEvent) -> Void)?
	var onStatusChange: ((Status) -> Void)?

	private let runnerExecutableOverride: URL?
	private let terminateGraceDuration: Duration
	private let killGraceDuration: Duration
	private var process: Process?
	private var outputPipe: Pipe?
	private var outputBuffer = Data()
	private var stopEscalationTask: Task<Void, Never>?

	init(
		runnerExecutable: URL? = nil,
		terminateGraceDuration: Duration = .seconds(5),
		killGraceDuration: Duration = .seconds(5)
	) {
		runnerExecutableOverride = runnerExecutable
		self.terminateGraceDuration = terminateGraceDuration
		self.killGraceDuration = killGraceDuration
	}

	func start(configurationPath: URL) throws {
		guard process == nil else {
			return
		}

		onStatusChange?(.starting)

		let executable = try runnerExecutableURL()
		let runtimeConfiguration = try Self.runtimeConfigurationData(configurationPath: configurationPath)
		let child = Process()
		let pipe = Pipe()
		let input = Pipe()
		child.executableURL = executable
		child.arguments = ["-config", "-"]
		child.environment = runnerEnvironment()
		child.standardInput = input
		child.standardOutput = pipe
		child.standardError = pipe

		pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
			let data = handle.availableData
			guard !data.isEmpty else {
				return
			}
			Task { @MainActor in
				self?.handleOutput(data)
			}
		}

		child.terminationHandler = { [weak self] terminated in
			Task { @MainActor in
				self?.stopEscalationTask?.cancel()
				self?.stopEscalationTask = nil
				self?.outputPipe?.fileHandleForReading.readabilityHandler = nil
				self?.flushOutputBuffer()
				self?.process = nil
				self?.outputPipe = nil

				if terminated.terminationStatus == 0 {
					self?.onStatusChange?(.stopped)
				} else {
					self?.onStatusChange?(.stoppedWithFailure(terminated.terminationStatus))
				}
			}
		}

		outputBuffer.removeAll(keepingCapacity: true)
		try child.run()
		input.fileHandleForWriting.write(runtimeConfiguration)
		input.fileHandleForWriting.closeFile()
		process = child
		outputPipe = pipe
		onStatusChange?(.running(pid: child.processIdentifier))
		onEvent?(.init(kind: .info, message: "Started transwarp-runner"))
	}

	func stop() {
		guard let process else {
			return
		}

		onStatusChange?(.stopping(pid: process.processIdentifier))
		process.interrupt()
		onEvent?(.init(kind: .info, message: "Stopping transwarp-runner"))
		scheduleStopEscalation(for: process)
	}

	private func scheduleStopEscalation(for process: Process) {
		stopEscalationTask?.cancel()
		let terminateGraceDuration = self.terminateGraceDuration
		let killGraceDuration = self.killGraceDuration
		stopEscalationTask = Task { [weak self, weak process] in
			do {
				try await Task.sleep(for: terminateGraceDuration)
				await MainActor.run {
					guard let self,
						  let process,
						  self.process === process,
						  process.isRunning else {
						return
					}
					process.terminate()
					self.onEvent?(.init(kind: .info, message: "Runner did not stop after interrupt; sent terminate"))
				}
				try await Task.sleep(for: killGraceDuration)
				await MainActor.run {
					guard let self,
						  let process,
						  self.process === process,
						  process.isRunning else {
						return
					}
					Darwin.kill(process.processIdentifier, SIGKILL)
					self.onEvent?(.init(kind: .error, message: "Runner did not stop after terminate; sent kill"))
				}
			} catch {
				return
			}
		}
	}

	private func handleOutput(_ data: Data) {
		for event in Self.events(from: data, buffer: &outputBuffer) {
			onEvent?(event)
		}
	}

	private func flushOutputBuffer() {
		for event in Self.flushEvents(buffer: &outputBuffer) {
			onEvent?(event)
		}
	}

	nonisolated static func events(from data: Data, buffer: inout Data) -> [RunnerEvent] {
		buffer.append(data)
		var events: [RunnerEvent] = []
		while let newlineIndex = buffer.firstIndex(where: { $0 == 0x0A || $0 == 0x0D }) {
			let lineData = buffer[..<newlineIndex]
			let newlineByte = buffer[newlineIndex]
			var removeThroughIndex = buffer.index(after: newlineIndex)
			if newlineByte == 0x0D, removeThroughIndex < buffer.endIndex, buffer[removeThroughIndex] == 0x0A {
				removeThroughIndex = buffer.index(after: removeThroughIndex)
			}
			buffer.removeSubrange(buffer.startIndex..<removeThroughIndex)
			guard !lineData.isEmpty else {
				continue
			}
			let line = String(data: lineData, encoding: .utf8) ?? String(decoding: lineData, as: UTF8.self)
			events.append(parseEvent(line) ?? RunnerEvent(kind: .log, message: line))
		}
		return events
	}

	nonisolated static func flushEvents(buffer: inout Data) -> [RunnerEvent] {
		guard !buffer.isEmpty else {
			return []
		}
		let lineData = buffer
		buffer.removeAll(keepingCapacity: true)
		let line = String(data: lineData, encoding: .utf8) ?? String(decoding: lineData, as: UTF8.self)
		return [parseEvent(line) ?? RunnerEvent(kind: .log, message: line)]
	}

	nonisolated static func parseEvent(_ line: String) -> RunnerEvent? {
		guard let data = line.data(using: .utf8),
			  let envelope = try? JSONDecoder().decode(RunnerEnvelope.self, from: data) else {
			return nil
		}

		let kind = RunnerEvent.Kind(rawValue: envelope.kind) ?? .info
		return RunnerEvent(
			date: parseTimestamp(envelope.time) ?? Date(),
			kind: kind,
			message: envelope.message,
			buildId: envelope.buildId,
			jobId: envelope.jobId,
			sequence: envelope.sequence
		)
	}

	private nonisolated static func parseTimestamp(_ value: String?) -> Date? {
		guard let value else {
			return nil
		}
		let fractionalFormatter = ISO8601DateFormatter()
		fractionalFormatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
		if let date = fractionalFormatter.date(from: value) {
			return date
		}
		let formatter = ISO8601DateFormatter()
		formatter.formatOptions = [.withInternetDateTime]
		return formatter.date(from: value)
	}

	private func runnerExecutableURL() throws -> URL {
		if let runnerExecutableOverride {
			return runnerExecutableOverride
		}
		if let bundled = Bundle.main.url(forResource: "transwarp-runner", withExtension: nil) {
			return bundled
		}

		let devURL = URL(filePath: FileManager.default.currentDirectoryPath)
			.appending(path: ".build/transwarp-runner/transwarp-runner")
		if FileManager.default.isExecutableFile(atPath: devURL.path) {
			return devURL
		}

		throw RunnerProcessError.missingRunner
	}

	private func runnerEnvironment() -> [String: String] {
		Self.runnerEnvironment(
			processEnvironment: ProcessInfo.processInfo.environment,
			resourceURL: Bundle.main.resourceURL,
			parentProcessID: ProcessInfo.processInfo.processIdentifier
		)
	}

	nonisolated static func runnerEnvironment(
		processEnvironment: [String: String],
		resourceURL: URL?,
		parentProcessID: Int32,
		isExecutable: (String) -> Bool = FileManager.default.isExecutableFile(atPath:)
	) -> [String: String] {
		var environment: [String: String] = [:]
		for key in ["HOME", "TMPDIR", "USER", "LOGNAME", "SHELL"] {
			if let value = processEnvironment[key], !value.isEmpty {
				environment[key] = value
			}
		}
		environment["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin"
		environment["TRANSWARP_PARENT_PID"] = "\(parentProcessID)"
		if let resourceURL {
			environment["TRANSWARP_BUNDLE_RESOURCES"] = resourceURL.path
			let bundledCloudflared = resourceURL.appending(path: "cloudflared")
			if isExecutable(bundledCloudflared.path) {
				environment["TRANSWARP_CLOUDFLARED_PATH"] = bundledCloudflared.path
			}
		}
		return environment
	}

	private nonisolated static func runtimeConfigurationData(configurationPath: URL) throws -> Data {
		let configuration = try AgentConfigurationStore.load(from: configurationPath)
		let runtimeConfiguration = try resolvedRuntimeConfiguration(configuration)
		return try AgentConfigurationStore.encode(runtimeConfiguration)
	}

	nonisolated static func resolvedRuntimeConfiguration(_ configuration: AgentConfiguration) throws -> AgentConfiguration {
		var resolved = configuration
		resolved.sharedToken = try KeychainSecretStore.resolve(resolved.sharedToken)
		resolved.registrationToken = try KeychainSecretStore.resolve(resolved.registrationToken)
		resolved.ciAccessClientSecret = try KeychainSecretStore.resolve(resolved.ciAccessClientSecret)
		resolved.runnerAccessClientSecret = try KeychainSecretStore.resolve(resolved.runnerAccessClientSecret)
		resolved.tunnel.token = try KeychainSecretStore.resolve(resolved.tunnel.token)
		for index in resolved.redactedValues.indices {
			resolved.redactedValues[index] = try KeychainSecretStore.resolve(resolved.redactedValues[index])
		}

		for jobIndex in resolved.jobs.indices {
			resolved.jobs[jobIndex].checkoutAuthorizationHeader = try KeychainSecretStore.resolve(
				resolved.jobs[jobIndex].checkoutAuthorizationHeader
			)
			for (key, value) in resolved.jobs[jobIndex].environment {
				resolved.jobs[jobIndex].environment[key] = try KeychainSecretStore.resolve(value)
			}
		}

		return resolved
	}
}

private struct RunnerEnvelope: Decodable {
	var kind: String
	var message: String
	var buildId: String?
	var jobId: String?
	var sequence: Int?
	var time: String?

	private enum CodingKeys: String, CodingKey {
		case kind
		case message
		case buildId = "build_id"
		case jobId = "job_id"
		case sequence
		case time
	}
}

enum RunnerProcessError: LocalizedError {
	case missingRunner

	var errorDescription: String? {
		switch self {
		case .missingRunner:
			"Could not find transwarp-runner. Run scripts/build-runner.sh before starting the app."
		}
	}
}
