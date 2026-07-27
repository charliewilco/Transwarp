import Foundation
import TranswarpCore

struct ConfigurationDraft: Equatable {
	var listenAddress = "127.0.0.1:8188"
	var machineId = ""
	var machineName = Host.current().localizedName ?? "Transwarp Mac"
	var sharedToken = ""
	var workspaceRoot = ""
	var preventSleep = true
	var redactedValues = ""
	var ciRegistrationURL = ""
	var ciHeartbeatURL = ""
	var ciDeregistrationURL = ""
	var registrationToken = ""
	var ciAccessClientID = ""
	var ciAccessClientSecret = ""
	var runnerAccessClientID = ""
	var runnerAccessClientSecret = ""
	var allowedReportOrigins = ""
	var heartbeatSeconds = 30
	var tunnelMode = TunnelConfiguration.Mode.off
	var cloudflaredPath = TunnelConfiguration.bundledCloudflaredPath
	var tunnelToken = ""
	var tunnelName = ""
	var publicURL = ""
	var jobId = "xcode-debug"
	var jobLabel = "Xcode Debug Build"
	var jobWorkingDirectory = ""
	var jobCheckout = true
	var jobAllowedRepositories = ""
	var jobCheckoutAuthorizationHeader = ""
	var jobCommand = "/usr/bin/xcodebuild"
	var jobArguments = "-scheme\nApp\n-configuration\nDebug\nbuild"
	var jobEnvironment = ""
	var jobSecretEnvironment = ""
	var jobRedactedEnvironmentKeys = ""
	var jobTimeoutSeconds = 3600
	var additionalJobs: [BuildJob] = []

	init() {}

	init(configuration: AgentConfiguration) {
		listenAddress = configuration.listenAddress
		machineId = configuration.machineId
		machineName = configuration.machineName
		sharedToken = configuration.sharedToken
		workspaceRoot = configuration.workspaceRoot
		preventSleep = configuration.preventSleep
		redactedValues = configuration.redactedValues.joined(separator: "\n")
		ciRegistrationURL = configuration.ciRegistrationURL?.absoluteString ?? ""
		ciHeartbeatURL = configuration.ciHeartbeatURL?.absoluteString ?? ""
		ciDeregistrationURL = configuration.ciDeregistrationURL?.absoluteString ?? ""
		registrationToken = configuration.registrationToken
		ciAccessClientID = configuration.ciAccessClientID
		ciAccessClientSecret = configuration.ciAccessClientSecret
		runnerAccessClientID = configuration.runnerAccessClientID
		runnerAccessClientSecret = configuration.runnerAccessClientSecret
		allowedReportOrigins = configuration.allowedReportOrigins.map(\.absoluteString).joined(separator: "\n")
		heartbeatSeconds = configuration.heartbeatSeconds
		tunnelMode = configuration.tunnel.mode
		cloudflaredPath = configuration.tunnel.cloudflaredPath
		tunnelToken = configuration.tunnel.token
		tunnelName = configuration.tunnel.name
		publicURL = configuration.tunnel.publicURL?.absoluteString ?? ""

		let jobs = configuration.jobs
		additionalJobs = Array(jobs.dropFirst())

		if let job = jobs.first {
			applyPrimaryJob(job)
		}
	}

	mutating func generateMachineId() {
		machineId = UUID().uuidString
	}

	mutating func generateSharedToken() {
		sharedToken = RandomToken.generate()
	}

	mutating func generateRegistrationToken() {
		registrationToken = RandomToken.generate()
	}

	mutating func applyCoordinatorBaseURL(_ value: String) throws {
		let base = try coordinatorBaseURL(value)
		ciRegistrationURL = base.appending(path: "transwarp").appending(path: "register").absoluteString
		ciHeartbeatURL = base.appending(path: "transwarp").appending(path: "heartbeat").absoluteString
		ciDeregistrationURL = base.appending(path: "transwarp").appending(path: "deregister").absoluteString
	}

	mutating func promoteAdditionalJob(id: String) throws {
		guard let index = additionalJobs.firstIndex(where: { $0.id == id }) else {
			throw ConfigurationDraftError("Additional job \(id) is not configured.")
		}
		let currentPrimaryJob = try primaryJob()
		let promotedJob = additionalJobs.remove(at: index)
		additionalJobs.insert(currentPrimaryJob, at: index)
		applyPrimaryJob(promotedJob)
	}

	mutating func removeAdditionalJob(id: String) throws {
		guard let index = additionalJobs.firstIndex(where: { $0.id == id }) else {
			throw ConfigurationDraftError("Additional job \(id) is not configured.")
		}
		additionalJobs.remove(at: index)
	}

	static func inferredCoordinatorBaseURL(registrationURL: String) -> String {
		guard let url = URL(string: registrationURL), url.path == "/transwarp/register" else {
			return ""
		}
		var components = URLComponents()
		components.scheme = url.scheme
		components.host = url.host
		components.port = url.port
		return components.url?.absoluteString ?? ""
	}

	func makeConfiguration() throws -> AgentConfiguration {
		let primaryJob = try primaryJob()

		let configuration = AgentConfiguration(
			listenAddress: trimmed(listenAddress),
			machineId: trimmed(machineId),
			machineName: trimmed(machineName),
			sharedToken: sharedToken,
			workspaceRoot: trimmed(workspaceRoot),
			preventSleep: preventSleep,
			redactedValues: lines(redactedValues),
			ciRegistrationURL: try optionalURL(ciRegistrationURL, field: "CI registration URL"),
			ciHeartbeatURL: try optionalURL(ciHeartbeatURL, field: "CI heartbeat URL"),
			ciDeregistrationURL: try optionalURL(ciDeregistrationURL, field: "CI deregistration URL"),
			registrationToken: registrationToken,
			ciAccessClientID: trimmed(ciAccessClientID),
			ciAccessClientSecret: ciAccessClientSecret,
			runnerAccessClientID: trimmed(runnerAccessClientID),
			runnerAccessClientSecret: runnerAccessClientSecret,
			allowedReportOrigins: try origins(allowedReportOrigins, field: "Allowed report origins"),
			heartbeatSeconds: heartbeatSeconds,
			tunnel: TunnelConfiguration(
				mode: tunnelMode,
				cloudflaredPath: trimmed(cloudflaredPath),
				token: tunnelToken,
				name: trimmed(tunnelName),
				publicURL: try optionalURL(publicURL, field: "Tunnel public URL")
			),
			jobs: [primaryJob] + additionalJobs
		)
		try AgentConfigurationValidator.validate(configuration, checkFileSystem: false)
		return configuration
	}

	private func primaryJob() throws -> BuildJob {
		let plainEnvironment = try environment(jobEnvironment, field: "Environment")
		let secretEnvironment = try environment(jobSecretEnvironment, field: "Secret environment")
		let mergedEnvironment = plainEnvironment.merging(secretEnvironment) { _, secret in secret }
		let redactedKeys = unique(lines(jobRedactedEnvironmentKeys) + Array(secretEnvironment.keys))

		return BuildJob(
			id: trimmed(jobId),
			label: trimmed(jobLabel),
			workingDirectory: trimmed(jobWorkingDirectory),
			checkout: jobCheckout,
			allowedRepositories: lines(jobAllowedRepositories),
			checkoutAuthorizationHeader: jobCheckoutAuthorizationHeader,
			command: trimmed(jobCommand),
			arguments: lines(jobArguments),
			environment: mergedEnvironment,
			redactedEnvironmentKeys: redactedKeys,
			timeoutSeconds: jobTimeoutSeconds
		)
	}

	private mutating func applyPrimaryJob(_ job: BuildJob) {
		jobId = job.id
		jobLabel = job.label
		jobWorkingDirectory = job.workingDirectory
		jobCheckout = job.checkout
		jobAllowedRepositories = job.allowedRepositories.joined(separator: "\n")
		jobCheckoutAuthorizationHeader = job.checkoutAuthorizationHeader
		jobCommand = job.command
		jobArguments = job.arguments.joined(separator: "\n")
		let partitionedEnvironment = Self.partitionEnvironment(job.environment)
		jobEnvironment = Self.environmentText(partitionedEnvironment.plain)
		jobSecretEnvironment = Self.environmentText(partitionedEnvironment.secret)
		jobRedactedEnvironmentKeys = job.redactedEnvironmentKeys.joined(separator: "\n")
		jobTimeoutSeconds = job.timeoutSeconds
	}

	private func optionalURL(_ value: String, field: String) throws -> URL? {
		let value = trimmed(value)
		if value.isEmpty {
			return nil
		}
		guard let url = URL(string: value), let scheme = url.scheme, ["http", "https"].contains(scheme) else {
			throw ConfigurationDraftError("\(field) must be an http or https URL.")
		}
		return url
	}

	private func coordinatorBaseURL(_ value: String) throws -> URL {
		let value = trimmed(value)
		guard let url = URL(string: value), let scheme = url.scheme, ["http", "https"].contains(scheme), url.host != nil else {
			throw ConfigurationDraftError("Coordinator base URL must be an http or https URL.")
		}
		if (!url.path.isEmpty && url.path != "/") || url.query != nil || url.fragment != nil || url.user != nil || url.password != nil {
			throw ConfigurationDraftError("Coordinator base URL must be a base URL without credentials, path, query, or fragment.")
		}
		return url
	}

	private func origins(_ value: String, field: String) throws -> [URL] {
		try lines(value).map { line in
			guard let url = URL(string: line), let scheme = url.scheme, ["http", "https"].contains(scheme), url.host != nil else {
				throw ConfigurationDraftError("\(field) entries must be http or https origins.")
			}
			if (!url.path.isEmpty && url.path != "/") || url.query != nil || url.fragment != nil {
				throw ConfigurationDraftError("\(field) entries must not include a path, query, or fragment.")
			}
			return url
		}
	}

	private func lines(_ value: String) -> [String] {
		value
			.split(whereSeparator: \.isNewline)
			.map { trimmed(String($0)) }
			.filter { !$0.isEmpty }
	}

	private func environment(_ value: String, field: String) throws -> [String: String] {
		var environment: [String: String] = [:]
		for line in value.split(whereSeparator: \.isNewline) {
			let line = trimmed(String(line))
			if line.isEmpty {
				continue
			}
			let parts = line.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
			guard parts.count == 2 else {
				throw ConfigurationDraftError("\(field) entries must use KEY=value.")
			}
			let key = trimmed(String(parts[0]))
			guard !key.isEmpty else {
				throw ConfigurationDraftError("\(field) entries must include a key.")
			}
			environment[key] = String(parts[1])
		}
		return environment
	}

	private func unique(_ values: [String]) -> [String] {
		var seen = Set<String>()
		var uniqueValues: [String] = []
		for value in values {
			if seen.insert(value).inserted {
				uniqueValues.append(value)
			}
		}
		return uniqueValues
	}

	private func trimmed(_ value: String) -> String {
		value.trimmingCharacters(in: .whitespacesAndNewlines)
	}

	private static func partitionEnvironment(_ environment: [String: String]) -> (plain: [String: String], secret: [String: String]) {
		var plain: [String: String] = [:]
		var secret: [String: String] = [:]
		for (key, value) in environment {
			if SecretReference.isReference(value) || isSensitiveEnvironmentKey(key) {
				secret[key] = value
			} else {
				plain[key] = value
			}
		}
		return (plain, secret)
	}

	private static func environmentText(_ environment: [String: String]) -> String {
		environment
			.keys
			.sorted()
			.map { "\($0)=\(environment[$0] ?? "")" }
			.joined(separator: "\n")
	}

	private static func isSensitiveEnvironmentKey(_ key: String) -> Bool {
		SensitiveEnvironmentKeys.contains(key)
	}
}

struct ConfigurationDraftError: LocalizedError {
	var errorDescription: String?

	init(_ message: String) {
		errorDescription = message
	}
}
