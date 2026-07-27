import AppKit
import Darwin
import Foundation
import Observation
import TranswarpCore

@MainActor
@Observable
final class AppModel {
	private nonisolated static let minimumSupportedMacOSMajorVersion = 14

	private let process = RunnerProcess()
	private let preferencesDefaults: UserDefaults
	private let logEventsToStandardOutput: Bool

	var configurationPath: URL?
	var configuration: AgentConfiguration?
	var agentStatus: AgentStatus?
	var status: RunnerProcess.Status = .stopped
	var events: [RunnerEvent] = []
	var lastError: String?
	var configurationIssues: [String] = []
	var loginItemState = LoginItemState(isEnabled: false, canToggle: false, message: "Unknown")
	var testBuildInFlight = false
	var publicEndpointDiagnosisInFlight = false
	var startRunnerOnLaunch: Bool {
		didSet {
			AppPreferencesStore.save(
				AppPreferences(startRunnerOnLaunch: startRunnerOnLaunch),
				to: preferencesDefaults
			)
		}
	}

	private var statusPollTask: Task<Void, Never>?
	@ObservationIgnored private var surfacedReportFailureBuildIDs: Set<String> = []

	var isRunning: Bool {
		status.isRunning
	}

	var isActive: Bool {
		status.isActive
	}

	var canStop: Bool {
		isRunning
	}

	var canStart: Bool {
		configuration != nil && configurationIssues.isEmpty && !isActive
	}

	var activeBuild: BuildStatus? {
		Self.activeBuild(in: agentStatus)
	}

	var queuedBuilds: [BuildStatus] {
		Self.queuedBuilds(in: agentStatus)
	}

	var controllableBuilds: [BuildStatus] {
		Self.controllableBuilds(in: agentStatus)
	}

	var queuedBuildCount: Int {
		agentStatus?.queuedBuilds ?? 0
	}

	var activeBuildCount: Int {
		agentStatus?.activeBuilds ?? 0
	}

	var canCancelActiveBuild: Bool {
		isRunning && activeBuild != nil
	}

	func canCancelBuild(_ build: BuildStatus) -> Bool {
		isRunning && (build.isRunning || build.isQueued)
	}

	nonisolated static func activeBuild(in status: AgentStatus?) -> BuildStatus? {
		status?.recentBuilds.first { $0.isRunning }
	}

	nonisolated static func queuedBuilds(in status: AgentStatus?) -> [BuildStatus] {
		status?.recentBuilds.filter(\.isQueued) ?? []
	}

	nonisolated static func controllableBuilds(in status: AgentStatus?) -> [BuildStatus] {
		status?.recentBuilds.filter { $0.isRunning || $0.isQueued } ?? []
	}

	var canRunTestBuild: Bool {
		isRunning && !testBuildInFlight && activeBuildCount == 0 && queuedBuildCount == 0 && localTestJobID != nil
	}

	func canRunTestBuild(_ job: BuildJob) -> Bool {
		isRunning && !testBuildInFlight && activeBuildCount == 0 && queuedBuildCount == 0 && !job.checkout
	}

	var canDiagnosePublicEndpoint: Bool {
		isRunning &&
			!publicEndpointDiagnosisInFlight &&
			configuration?.tunnel.mode == .named &&
			configuration?.tunnel.publicURL != nil
	}

	var canToggleCIAvailability: Bool {
		isRunning && configuration != nil
	}

	var isAcceptingBuilds: Bool {
		agentStatus?.isAcceptingBuilds ?? true
	}

	func githubActionWorkflow(mode: GitHubActionWorkflow.Mode, jobID: String? = nil) -> String? {
		if mode == .releaseEvidence {
			return GitHubActionWorkflow(mode: .releaseEvidence, jobID: "").yaml
		}
		guard let configuration else {
			return nil
		}
		return GitHubActionWorkflow.make(for: configuration, mode: mode, jobID: jobID)?.yaml
	}

	func ciWorkflowReadiness(mode: GitHubActionWorkflow.Mode, jobID: String? = nil) -> CIWorkflowReadiness {
		CIWorkflowReadiness(
			mode: mode,
			configuration: configuration,
			configurationIssues: configurationIssues,
			jobID: jobID
		)
	}

	func canCopyGitHubActionSecretValues(mode: GitHubActionWorkflow.Mode) -> Bool {
		Self.canCopyGitHubActionSecretValues(
			mode: mode,
			configuration: configuration,
			configurationIssues: configurationIssues
		)
	}

	nonisolated static func canCopyGitHubActionSecretValues(
		mode: GitHubActionWorkflow.Mode,
		configuration: AgentConfiguration?,
		configurationIssues: [String] = []
	) -> Bool {
		guard configurationIssues.isEmpty else {
			return false
		}
		return CIWorkflowSecretValueExport.canCopyValues(mode: mode, configuration: configuration)
	}

	var testBuildHelp: String {
		testBuildHelp(for: localTestJob)
	}

	func testBuildHelp(for job: BuildJob?) -> String {
		if !isRunning {
			return "Start the runner before running a test build"
		}
		if testBuildInFlight {
			return "Test build request is in progress"
		}
		if activeBuild != nil {
			return "Wait for the active build to finish"
		}
		if activeBuildCount > 0 {
			return "Wait for the active build report to finish"
		}
		if queuedBuildCount > 0 {
			return "Wait for queued builds to finish"
		}
		if job?.checkout == true {
			return "Local test builds use jobs with checkout disabled"
		}
		if localTestJobID == nil {
			return "Configure a job with checkout disabled for local test builds"
		}
		if let job {
			return "Run \(job.id) through the local runner API"
		}
		return "Run a non-checkout job through the local runner API"
	}

	var publicEndpointDiagnosisHelp: String {
		if !isRunning {
			return "Start the runner before diagnosing the public URL"
		}
		if publicEndpointDiagnosisInFlight {
			return "Public URL diagnosis is in progress"
		}
		if configuration?.tunnel.mode != .named {
			return "Configure a named Cloudflare Tunnel before diagnosing the public URL"
		}
		if configuration?.tunnel.publicURL == nil {
			return "Set the named tunnel public URL before diagnosing it"
		}
		return "Call the public tunnel /status endpoint with runner authentication"
	}

	init(preferencesDefaults: UserDefaults = .standard) {
		self.preferencesDefaults = preferencesDefaults
		logEventsToStandardOutput = ProcessInfo.processInfo.environment["TRANSWARP_APP_LOG_EVENTS_TO_STDOUT"] == "1"
		let preferences = AppPreferencesStore.load(from: preferencesDefaults)
		startRunnerOnLaunch = ProcessInfo.processInfo.environment["TRANSWARP_START_RUNNER_ON_LAUNCH"] == "1" ||
			preferences.startRunnerOnLaunch
		refreshLoginItemState()

		do {
			configurationPath = try Self.ensureSecuredDefaultConfigurationFile()
			reloadConfiguration()
			try migrateConfigurationSecretsToKeychain()
			append(.init(kind: .info, message: "Configuration ready at \(configurationPath?.path ?? "unknown")"))
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: error.localizedDescription))
		}

		process.onEvent = { [weak self] event in
			Task { @MainActor in
				self?.append(event)
			}
		}
		process.onStatusChange = { [weak self] status in
			Task { @MainActor in
				self?.status = status
				if case .running = status {
					self?.startStatusPolling()
				} else {
					self?.stopStatusPolling()
				}
			}
		}

		startRunnerIfNeeded()
	}

	func start() {
		guard let configurationPath else {
			append(.init(kind: .error, message: "Missing configuration path"))
			return
		}

		do {
			reloadConfiguration()
			if !configurationIssues.isEmpty {
				let message = configurationIssues.joined(separator: " ")
				lastError = message
				append(.init(kind: .error, message: "Configuration preflight failed: \(message)"))
				return
			}
			try process.start(configurationPath: configurationPath)
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: error.localizedDescription))
		}
	}

	func setOpensAtLogin(_ isEnabled: Bool) {
		do {
			try LoginItemService.setEnabled(isEnabled)
			refreshLoginItemState()
			lastError = nil
		} catch {
			refreshLoginItemState()
			lastError = error.localizedDescription
			append(.init(kind: .error, message: "Login item update failed: \(error.localizedDescription)"))
		}
	}

	func stop() {
		process.stop()
	}

	func setAcceptingBuilds(_ accepting: Bool) async {
		guard let configuration,
			  let baseURL = URL(string: "http://\(configuration.listenAddress)") else {
			append(.init(kind: .error, message: "CI availability update failed: runner configuration is unavailable"))
			return
		}

		let url = baseURL
			.appending(path: "v1")
			.appending(path: "availability")
		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		do {
			request.setValue("Bearer \(try KeychainSecretStore.resolve(configuration.sharedToken))", forHTTPHeaderField: "Authorization")
			request.httpBody = try JSONEncoder().encode(AvailabilityUpdatePayload(acceptingBuilds: accepting))
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: "CI availability update failed: \(error.localizedDescription)"))
			return
		}

		do {
			let (data, response) = try await NoRedirectURLSession.data(for: request)
			guard let httpResponse = response as? HTTPURLResponse else {
				throw AppModelError("CI availability update failed without an HTTP response")
			}
			guard 200..<300 ~= httpResponse.statusCode else {
				let body = String(data: data, encoding: .utf8) ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
				throw AppModelError("CI availability update failed with \(httpResponse.statusCode): \(body)")
			}
			append(.init(kind: .registration, message: accepting ? "Resumed CI builds" : "Paused CI builds"))
			await refreshAgentStatus()
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: error.localizedDescription))
		}
	}

	func runTestBuild(jobID: String? = nil) async {
		guard let configuration,
			  let job = Self.localTestJob(in: configuration, jobID: jobID),
			  let baseURL = URL(string: "http://\(configuration.listenAddress)") else {
			append(.init(kind: .error, message: "Test build failed: runner configuration is unavailable"))
			return
		}
		let jobID = job.id

		testBuildInFlight = true
		defer {
			testBuildInFlight = false
		}

		let requestID = "app-smoke-\(Int(Date().timeIntervalSince1970))-\(UUID().uuidString.lowercased())"
		let payload = BuildStartPayload(jobId: jobID, requestId: requestID)
		let url = baseURL
			.appending(path: "v1")
			.appending(path: "builds")

		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		do {
			request.setValue("Bearer \(try KeychainSecretStore.resolve(configuration.sharedToken))", forHTTPHeaderField: "Authorization")
			request.httpBody = try JSONEncoder().encode(payload)
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: "Test build failed: \(error.localizedDescription)"))
			return
		}

		do {
			let (data, response) = try await NoRedirectURLSession.data(for: request)
			guard let httpResponse = response as? HTTPURLResponse else {
				throw AppModelError("Test build failed without an HTTP response")
			}
			guard 200..<300 ~= httpResponse.statusCode else {
				let body = String(data: data, encoding: .utf8) ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
				throw AppModelError("Test build failed with \(httpResponse.statusCode): \(body)")
			}

			let receipt = try JSONDecoder().decode(BuildStartReceipt.self, from: data)
			append(.init(kind: .build, message: "Requested test build \(receipt.buildId) for \(jobID)"))
			await refreshAgentStatus()
			await waitForTerminalTestBuild(
				buildID: receipt.buildId,
				jobTimeoutSeconds: job.timeoutSeconds
			)
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: error.localizedDescription))
		}
	}

	func diagnosePublicEndpoint() async {
		guard let configuration else {
			append(.init(kind: .error, message: "Public URL diagnosis failed: runner configuration is unavailable"))
			return
		}

		publicEndpointDiagnosisInFlight = true
		defer {
			publicEndpointDiagnosisInFlight = false
		}

		do {
			let request = try PublicEndpointDiagnosis.request(
				configuration: configuration,
				resolveSecret: KeychainSecretStore.resolve
			)
			let (data, response) = try await NoRedirectURLSession.data(for: request)
			guard let httpResponse = response as? HTTPURLResponse else {
				throw AppModelError("Public URL diagnosis failed without an HTTP response")
			}
			guard 200..<300 ~= httpResponse.statusCode else {
				let body = String(data: data, encoding: .utf8) ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
				throw AppModelError("Public URL diagnosis failed with \(httpResponse.statusCode): \(body)")
			}

			let decoder = JSONDecoder()
			decoder.keyDecodingStrategy = .convertFromSnakeCase
			let status = try decoder.decode(AgentStatus.self, from: data)
			try PublicEndpointDiagnosis.validate(status: status, configuration: configuration)
			agentStatus = status
			append(PublicEndpointDiagnosis.event(for: status))
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: error.localizedDescription))
		}
	}

	func cancelActiveBuild() async {
		guard let build = activeBuild else {
			return
		}
		await cancelBuild(build)
	}

	func cancelBuild(_ build: BuildStatus) async {
		guard canCancelBuild(build) else {
			return
		}
		guard let configuration,
			  let baseURL = URL(string: "http://\(configuration.listenAddress)") else {
			append(.init(kind: .error, message: "Cancel failed: missing runner status"))
			return
		}
		let url = baseURL
			.appending(path: "v1")
			.appending(path: "builds")
			.appending(path: build.buildId)
			.appending(path: "cancel")

		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		do {
			request.setValue("Bearer \(try KeychainSecretStore.resolve(configuration.sharedToken))", forHTTPHeaderField: "Authorization")
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: "Cancel failed: \(error.localizedDescription)"))
			return
		}

		do {
			let (data, response) = try await NoRedirectURLSession.data(for: request)
			guard let httpResponse = response as? HTTPURLResponse else {
				throw AppModelError("Cancel failed without an HTTP response")
			}
			if 200..<300 ~= httpResponse.statusCode {
				append(.init(kind: .build, message: "Cancel requested for \(build.buildId)"))
				await refreshAgentStatus()
				return
			}

			let body = String(data: data, encoding: .utf8) ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
			throw AppModelError("Cancel failed with \(httpResponse.statusCode): \(body)")
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: error.localizedDescription))
		}
	}

	func revealConfiguration() {
		guard let configurationPath else {
			return
		}
		NSWorkspace.shared.activateFileViewerSelecting([configurationPath])
	}

	func openConfiguration() {
		guard let configurationPath else {
			return
		}
		NSWorkspace.shared.open(configurationPath)
	}

	func copyGitHubActionWorkflow(mode: GitHubActionWorkflow.Mode, jobID: String? = nil) {
		guard let workflow = githubActionWorkflow(mode: mode, jobID: jobID) else {
			let selectedJobID = jobID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
			let reason = selectedJobID.isEmpty ? "no jobs are configured" : "selected job \(selectedJobID) is not configured"
			append(.init(kind: .error, message: "GitHub Action workflow unavailable: \(reason)"))
			return
		}

		NSPasteboard.general.clearContents()
		NSPasteboard.general.setString(workflow, forType: .string)
		if let jobID, !jobID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, mode != .releaseEvidence {
			append(.init(kind: .info, message: "Copied \(mode.rawValue) GitHub Action workflow for \(jobID)"))
		} else {
			append(.init(kind: .info, message: "Copied \(mode.rawValue) GitHub Action workflow"))
		}
	}

	func copyGitHubActionSecretChecklist(mode: GitHubActionWorkflow.Mode, jobID: String? = nil) {
		let checklist = ciWorkflowReadiness(mode: mode, jobID: jobID).secretChecklistText
		guard !checklist.isEmpty else {
			append(.init(kind: .info, message: "\(mode.rawValue) workflow does not require GitHub secrets"))
			return
		}

		NSPasteboard.general.clearContents()
		NSPasteboard.general.setString(checklist, forType: .string)
		append(.init(kind: .info, message: "Copied \(mode.rawValue) GitHub Action secret checklist"))
	}

	func copyGitHubActionSecretValues(mode: GitHubActionWorkflow.Mode) {
		guard let configuration else {
			append(.init(kind: .error, message: "GitHub Action secret values unavailable: no configuration is loaded"))
			return
		}

		do {
			let values = try CIWorkflowSecretValueExport.text(
				mode: mode,
				configuration: configuration,
				resolveSecret: KeychainSecretStore.resolve
			)
			guard !values.isEmpty else {
				append(.init(kind: .info, message: "\(mode.rawValue) workflow has no local secret values to copy"))
				return
			}
			NSPasteboard.general.clearContents()
			NSPasteboard.general.setString(values, forType: .string)
			append(.init(kind: .info, message: "Copied \(mode.rawValue) GitHub Action secret values"))
		} catch {
			lastError = error.localizedDescription
			append(.init(kind: .error, message: "GitHub Action secret values unavailable: \(error.localizedDescription)"))
		}
	}

	func reloadConfiguration() {
		guard let configurationPath else {
			return
		}

		do {
			configuration = try AgentConfigurationStore.load(from: configurationPath)
			updateConfigurationIssues()
			lastError = nil
		} catch {
			configurationIssues = [error.localizedDescription]
			lastError = error.localizedDescription
			append(.init(kind: .error, message: "Configuration reload failed: \(error.localizedDescription)"))
		}
	}

	func saveConfiguration(_ configuration: AgentConfiguration) throws {
		guard let configurationPath else {
			throw AppModelError("Missing configuration path")
		}

		var securedConfiguration = configuration
		try KeychainSecretStore.secure(&securedConfiguration)
		try AgentConfigurationStore.save(securedConfiguration, to: configurationPath)
		self.configuration = securedConfiguration
		updateConfigurationIssues()
		lastError = nil
		append(.init(kind: .info, message: "Configuration saved"))
	}

	private func updateConfigurationIssues() {
		guard let configuration else {
			configurationIssues = ["Configuration is unavailable."]
			return
		}
		configurationIssues = []
		if let hostIssue = Self.hostSupportIssue() {
			configurationIssues.append(hostIssue)
		}
		configurationIssues.append(contentsOf: AgentConfigurationValidator
			.issues(for: configuration)
			.map(\.message))
		configurationIssues.append(contentsOf: KeychainSecretStore.issues(for: configuration))
	}

	private func startStatusPolling() {
		stopStatusPolling()

		statusPollTask = Task { [weak self] in
			while !Task.isCancelled {
				await self?.refreshAgentStatus()
				try? await Task.sleep(for: .seconds(3))
			}
		}
	}

	private func stopStatusPolling() {
		statusPollTask?.cancel()
		statusPollTask = nil
		agentStatus = nil
	}

	private func refreshAgentStatus() async {
		do {
			guard let status = try await fetchAgentStatus() else {
				return
			}
			agentStatus = status
			surfaceReportFailures(in: status)
		} catch {
			lastError = error.localizedDescription
		}
	}

	private func fetchAgentStatus() async throws -> AgentStatus? {
		guard let configuration,
			  let url = URL(string: "http://\(configuration.listenAddress)/status") else {
			return nil
		}

		var request = URLRequest(url: url)
		do {
			request.setValue("Bearer \(try KeychainSecretStore.resolve(configuration.sharedToken))", forHTTPHeaderField: "Authorization")
		} catch {
			lastError = error.localizedDescription
			return nil
		}

		let (data, response) = try await NoRedirectURLSession.data(for: request)
		guard let httpResponse = response as? HTTPURLResponse,
			  200..<300 ~= httpResponse.statusCode else {
			return nil
		}
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase
		return try decoder.decode(AgentStatus.self, from: data)
	}

	private func waitForTerminalTestBuild(buildID: String, jobTimeoutSeconds: Int) async {
		let timeout = max(jobTimeoutSeconds + 60, 60)
		let deadline = Date().addingTimeInterval(TimeInterval(timeout))

		while !Task.isCancelled {
			if let status = try? await fetchAgentStatus() {
				agentStatus = status
				surfaceReportFailures(in: status)
				if let build = Self.buildStatus(in: status, buildID: buildID), build.isTerminal {
					append(Self.testBuildCompletionEvent(for: build))
					return
				}
			}

			if Date() >= deadline {
				append(.init(kind: .error, message: "Test build \(buildID) did not reach terminal status within \(timeout) seconds"))
				return
			}
			try? await Task.sleep(for: .seconds(1))
		}
	}

	private func surfaceReportFailures(in status: AgentStatus) {
		for build in status.recentBuilds {
			guard let message = Self.reportFailureMessage(for: build),
				  !surfacedReportFailureBuildIDs.contains(build.buildId) else {
				continue
			}
			surfacedReportFailureBuildIDs.insert(build.buildId)
			append(.init(kind: .error, message: message, buildId: build.buildId, jobId: build.jobId))
		}
	}

	private var localTestJobID: String? {
		Self.localTestJobID(in: configuration)
	}

	private var localTestJob: BuildJob? {
		Self.localTestJob(in: configuration)
	}

	nonisolated static func localTestJobID(in configuration: AgentConfiguration?, jobID: String? = nil) -> String? {
		localTestJob(in: configuration, jobID: jobID)?.id
	}

	nonisolated static func localTestJob(in configuration: AgentConfiguration?, jobID: String? = nil) -> BuildJob? {
		let requestedJobID = jobID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
		guard !requestedJobID.isEmpty else {
			return configuration?.jobs.first { !$0.checkout }
		}
		return configuration?.jobs.first { $0.id == requestedJobID && !$0.checkout }
	}

	nonisolated static func buildStatus(in status: AgentStatus?, buildID: String) -> BuildStatus? {
		status?.recentBuilds.first { $0.buildId == buildID }
	}

	private func append(_ event: RunnerEvent) {
		events.append(event)
		if events.count > 500 {
			events.removeFirst(events.count - 500)
		}
		if logEventsToStandardOutput {
			print(Self.standardOutputLogLine(for: event))
			fflush(stdout)
		}
		lastError = Self.lastError(after: event, current: lastError)
	}

	nonisolated static func standardOutputLogLine(for event: RunnerEvent) -> String {
		var parts = ["[\(event.kind.rawValue)]", event.message]
		if let buildId = event.buildId {
			parts.append("build_id=\(buildId)")
		}
		if let jobId = event.jobId {
			parts.append("job_id=\(jobId)")
		}
		return parts.joined(separator: " ")
	}

	nonisolated static func lastError(after event: RunnerEvent, current: String?) -> String? {
		if event.kind == .error {
			return event.message
		}
		return current
	}

	nonisolated static func reportFailureMessage(for build: BuildStatus) -> String? {
		guard build.reportStatus == "failed" else {
			return nil
		}
		let suffix = build.reportError?.trimmingCharacters(in: .whitespacesAndNewlines)
		if let suffix, !suffix.isEmpty {
			return "CI result report failed for \(build.jobId) \(build.buildId): \(suffix)"
		}
		return "CI result report failed for \(build.jobId) \(build.buildId)"
	}

	nonisolated static func hostSupportIssue(
		osMajorVersion: Int = ProcessInfo.processInfo.operatingSystemVersion.majorVersion,
		architecture: String = currentArchitecture()
	) -> String? {
		if architecture != "arm64" {
			return "Transwarp requires an Apple Silicon Mac."
		}
		if osMajorVersion < minimumSupportedMacOSMajorVersion {
			return "Transwarp requires macOS \(minimumSupportedMacOSMajorVersion) or newer."
		}
		return nil
	}

	nonisolated static func testBuildCompletionEvent(for build: BuildStatus) -> RunnerEvent {
		switch build.status {
		case "passed":
			return RunnerEvent(kind: .build, message: "Test build \(build.buildId) passed", buildId: build.buildId, jobId: build.jobId)
		case "canceled":
			return RunnerEvent(kind: .build, message: "Test build \(build.buildId) canceled", buildId: build.buildId, jobId: build.jobId)
		default:
			let error = build.result?.error?.trimmingCharacters(in: .whitespacesAndNewlines)
			let suffix = if let error, !error.isEmpty {
				": \(error)"
			} else {
				""
			}
			return RunnerEvent(kind: .error, message: "Test build \(build.buildId) failed\(suffix)", buildId: build.buildId, jobId: build.jobId)
		}
	}

	private nonisolated static func currentArchitecture() -> String {
		var size = 0
		guard sysctlbyname("hw.machine", nil, &size, nil, 0) == 0, size > 0 else {
			return compiledArchitecture()
		}
		var buffer = [CChar](repeating: 0, count: size)
		guard sysctlbyname("hw.machine", &buffer, &size, nil, 0) == 0 else {
			return compiledArchitecture()
		}
		let endIndex = buffer.firstIndex(of: 0) ?? buffer.endIndex
		let bytes = buffer[..<endIndex].map { UInt8(bitPattern: $0) }
		return String(decoding: bytes, as: UTF8.self)
	}

	private nonisolated static func compiledArchitecture() -> String {
		#if arch(arm64)
			return "arm64"
		#elseif arch(x86_64)
			return "x86_64"
		#else
			return "unknown"
		#endif
	}

	private func migrateConfigurationSecretsToKeychain() throws {
		guard var configuration else {
			return
		}
		let original = configuration
		try KeychainSecretStore.secure(&configuration)
		if configuration != original, let configurationPath {
			try AgentConfigurationStore.save(configuration, to: configurationPath)
			self.configuration = configuration
			updateConfigurationIssues()
			append(.init(kind: .info, message: "Configuration secrets stored in Keychain"))
		}
	}

	private func refreshLoginItemState() {
		loginItemState = LoginItemService.state()
	}

	private func startRunnerIfNeeded() {
		guard startRunnerOnLaunch else {
			return
		}
		if canStart {
			append(.init(kind: .info, message: "Starting runner because start on launch is enabled"))
			start()
		} else {
			append(.init(kind: .error, message: "Start on launch skipped: \(configurationIssues.first ?? "configuration is not ready")"))
		}
	}

	private static func ensureSecuredDefaultConfigurationFile() throws -> URL {
		let url = try AgentConfigurationStore.defaultPath()
		if !FileManager.default.fileExists(atPath: url.path) {
			var starter = AgentConfiguration.starter()
			try KeychainSecretStore.secure(&starter)
			try AgentConfigurationStore.save(starter, to: url)
		}
		return url
	}
}

struct AppModelError: LocalizedError {
	var errorDescription: String?

	init(_ message: String) {
		errorDescription = message
	}
}
