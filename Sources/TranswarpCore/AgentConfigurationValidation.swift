import Foundation

public struct AgentConfigurationValidationIssue: Equatable, Sendable {
	public var message: String

	public init(_ message: String) {
		self.message = message
	}
}

public enum AgentConfigurationValidator {
	private static let maxMachineIDLength = 128
	private static let maxJobIDLength = 128
	private static let maxRepositoryURLLength = 2048

	public static func issues(for configuration: AgentConfiguration, checkFileSystem: Bool = true) -> [AgentConfigurationValidationIssue] {
		var issues: [AgentConfigurationValidationIssue] = []

		if trimmed(configuration.listenAddress).isEmpty {
			issues.append(.init("Listen address is required."))
		} else if !configuration.listenAddress.hasPrefix("127.0.0.1:") && !configuration.listenAddress.hasPrefix("localhost:") {
			issues.append(.init("Listen address must bind to loopback."))
		}
		if trimmed(configuration.machineName).isEmpty {
			issues.append(.init("Machine name is required."))
		}
		if trimmed(configuration.machineId).isEmpty {
			issues.append(.init("Machine ID is required."))
		} else if configuration.machineId.utf8.count > maxMachineIDLength {
			issues.append(.init("Machine ID is too long."))
		} else if !isValidStableIdentifier(configuration.machineId) {
			issues.append(.init("Machine ID may contain only letters, numbers, dots, underscores, or hyphens."))
		}
		if trimmed(configuration.sharedToken).isEmpty {
			issues.append(.init("Runner token is required."))
		} else {
			validateSecretReference(configuration.sharedToken, field: "Runner token", issues: &issues)
			validateHeaderValue(configuration.sharedToken, field: "Runner token", issues: &issues)
		}
		if !trimmed(configuration.workspaceRoot).isEmpty && !configuration.workspaceRoot.hasPrefix("/") {
			issues.append(.init("Workspace root must be an absolute path."))
		}
		for value in configuration.redactedValues {
			validateSecretReference(value, field: "Additional redacted value", issues: &issues)
		}
		if configuration.jobs.isEmpty {
			issues.append(.init("At least one build job is required."))
		}
		if configuration.heartbeatSeconds < 0 {
			issues.append(.init("Heartbeat seconds must not be negative."))
		}

		validateURL(configuration.ciRegistrationURL, field: "CI registration URL", issues: &issues)
		validateURL(configuration.ciHeartbeatURL, field: "CI heartbeat URL", issues: &issues)
		validateURL(configuration.ciDeregistrationURL, field: "CI deregistration URL", issues: &issues)
		if configuration.ciRegistrationURL == nil, configuration.ciHeartbeatURL != nil {
			issues.append(.init("CI registration URL is required when CI heartbeat URL is configured."))
		}
		if configuration.ciRegistrationURL == nil, configuration.ciDeregistrationURL != nil {
			issues.append(.init("CI registration URL is required when CI deregistration URL is configured."))
		}
		if configuration.ciRegistrationURL != nil, configuration.ciDeregistrationURL == nil {
			issues.append(.init("CI deregistration URL is required when CI registration URL is configured."))
		}
		if hasCIRegistrationEndpoint(configuration), trimmed(configuration.registrationToken).isEmpty {
			issues.append(.init("Registration token is required when CI registration endpoints are configured."))
		}
		if trimmed(configuration.ciAccessClientID).isEmpty != trimmed(configuration.ciAccessClientSecret).isEmpty {
			issues.append(.init("CI Access client ID and secret must be provided together."))
		}
		if !trimmed(configuration.ciAccessClientSecret).isEmpty {
			validateSecretReference(configuration.ciAccessClientSecret, field: "CI Access client secret", issues: &issues)
		}
		if !trimmed(configuration.ciAccessClientID).isEmpty {
			validateHeaderValue(configuration.ciAccessClientID, field: "CI Access client ID", issues: &issues)
			validateHeaderValue(configuration.ciAccessClientSecret, field: "CI Access client secret", issues: &issues)
		}
		if trimmed(configuration.runnerAccessClientID).isEmpty != trimmed(configuration.runnerAccessClientSecret).isEmpty {
			issues.append(.init("Runner Access client ID and secret must be provided together."))
		}
		if !trimmed(configuration.runnerAccessClientSecret).isEmpty {
			validateSecretReference(configuration.runnerAccessClientSecret, field: "Runner Access client secret", issues: &issues)
		}
		if !trimmed(configuration.runnerAccessClientID).isEmpty {
			validateHeaderValue(configuration.runnerAccessClientID, field: "Runner Access client ID", issues: &issues)
			validateHeaderValue(configuration.runnerAccessClientSecret, field: "Runner Access client secret", issues: &issues)
		}
		for origin in configuration.allowedReportOrigins {
			validateOrigin(origin, field: "Allowed report origin", issues: &issues)
		}

		if configuration.tunnel.mode == .named {
			if trimmed(configuration.tunnel.token).isEmpty {
				issues.append(.init("Named tunnels require a Cloudflare tunnel token."))
			} else {
				validateSecretReference(configuration.tunnel.token, field: "Cloudflare tunnel token", issues: &issues)
			}
			if configuration.tunnel.publicURL == nil {
				issues.append(.init("Named tunnels require a stable public URL."))
			}
			if let publicURL = configuration.tunnel.publicURL {
				validateNamedTunnelPublicURL(publicURL, issues: &issues)
			}
		}
		if !trimmed(configuration.registrationToken).isEmpty {
			validateSecretReference(configuration.registrationToken, field: "Registration token", issues: &issues)
			validateHeaderValue(configuration.registrationToken, field: "Registration token", issues: &issues)
		}
		if configuration.tunnel.mode != .off {
			if checkFileSystem &&
				!trimmed(configuration.tunnel.cloudflaredPath).isEmpty &&
				!isExecutableCloudflared(configuration.tunnel.cloudflaredPath) {
				issues.append(.init("cloudflared must exist and be executable."))
			}
		}

		var seenJobIds = Set<String>()
		for job in configuration.jobs {
			let jobLabel = trimmed(job.id).isEmpty ? "Job" : "Job \(job.id)"
			if trimmed(job.id).isEmpty {
				issues.append(.init("Job ID is required."))
			} else if job.id.utf8.count > maxJobIDLength {
				issues.append(.init("\(jobLabel) ID is too long."))
			} else if !isValidStableIdentifier(job.id) {
				issues.append(.init("\(jobLabel) ID may contain only letters, numbers, dots, underscores, or hyphens."))
			} else if seenJobIds.contains(job.id) {
				issues.append(.init("Duplicate job ID \(job.id)."))
			}
			seenJobIds.insert(job.id)

			if !job.checkout {
				if trimmed(job.workingDirectory).isEmpty {
					issues.append(.init("\(jobLabel) requires a working directory when checkout is disabled."))
				} else if !job.workingDirectory.hasPrefix("/") {
					issues.append(.init("\(jobLabel) working directory must be an absolute path."))
				}
			}
			if job.checkout && job.allowedRepositories.isEmpty {
				issues.append(.init("\(jobLabel) requires at least one allowed repository when checkout is enabled."))
			}
			for repository in job.allowedRepositories {
				validateRepositoryURL(repository, jobLabel: jobLabel, issues: &issues)
			}
			if !trimmed(job.checkoutAuthorizationHeader).isEmpty {
				if !job.checkout {
					issues.append(.init("\(jobLabel) checkout authorization header requires checkout to be enabled."))
				}
				if SecretReference(job.checkoutAuthorizationHeader) == nil {
					validateHeaderLine(job.checkoutAuthorizationHeader, field: "\(jobLabel) checkout authorization header", issues: &issues)
				}
				validateSecretReference(job.checkoutAuthorizationHeader, field: "\(jobLabel) checkout authorization header", issues: &issues)
			}
			if trimmed(job.command).isEmpty {
				issues.append(.init("\(jobLabel) command is required."))
			} else {
				validateCommand(job.command, jobLabel: jobLabel, issues: &issues)
			}
			if job.timeoutSeconds <= 0 {
				issues.append(.init("\(jobLabel) timeout must be greater than zero."))
			}
			for key in job.redactedEnvironmentKeys {
				if !isValidEnvironmentKey(key) {
					issues.append(.init("\(jobLabel) redacted environment key \(key) is invalid."))
				}
				if isReservedEnvironmentKey(key) {
					issues.append(.init("\(jobLabel) redacted environment key \(key) uses the reserved TRANSWARP_ prefix."))
				}
			}
			for (key, value) in job.environment {
				if !isValidEnvironmentKey(key) {
					issues.append(.init("\(jobLabel) environment key \(key) is invalid."))
				}
				if isReservedEnvironmentKey(key) {
					issues.append(.init("\(jobLabel) environment key \(key) uses the reserved TRANSWARP_ prefix."))
				}
				validateSecretReference(value, field: "\(jobLabel) environment \(key)", issues: &issues)
			}
		}

		return issues
	}

	public static func validate(_ configuration: AgentConfiguration, checkFileSystem: Bool = true) throws {
		let issues = issues(for: configuration, checkFileSystem: checkFileSystem)
		if let issue = issues.first {
			throw AgentConfigurationValidationError(issue.message)
		}
	}

	private static func validateRepositoryURL(_ repository: String, jobLabel: String, issues: inout [AgentConfigurationValidationIssue]) {
		if trimmed(repository).isEmpty {
			issues.append(.init("\(jobLabel) allowed repositories must not include empty values."))
			return
		}
		if repository.utf8.count > maxRepositoryURLLength {
			issues.append(.init("\(jobLabel) allowed repository is too long."))
		}
		if repository.utf8.contains(where: { $0 < 0x20 || $0 == 0x7f }) {
			issues.append(.init("\(jobLabel) allowed repository must not include control characters."))
		}
		guard let components = URLComponents(string: repository) else {
			return
		}
		if components.user != nil || components.password != nil {
			issues.append(.init("\(jobLabel) allowed repository must not include credentials."))
		}
		if components.query != nil || components.fragment != nil {
			issues.append(.init("\(jobLabel) allowed repository must not include query or fragment."))
		}
	}

	private static func validateHeaderValue(_ value: String, field: String, issues: inout [AgentConfigurationValidationIssue]) {
		for scalar in value.unicodeScalars where scalar.value < 0x20 || scalar.value == 0x7f {
			issues.append(.init("\(field) must be a single HTTP header value."))
			return
		}
	}

	private static func validateHeaderLine(_ line: String, field: String, issues: inout [AgentConfigurationValidationIssue]) {
		guard let colonIndex = line.firstIndex(of: ":") else {
			issues.append(.init("\(field) must use Header-Name: value format."))
			return
		}
		let name = String(line[..<colonIndex])
		let value = line[line.index(after: colonIndex)...].trimmingCharacters(in: CharacterSet(charactersIn: " \t"))
		if !isValidHeaderName(name) {
			issues.append(.init("\(field) name is invalid."))
			return
		}
		if value.isEmpty {
			issues.append(.init("\(field) value is required."))
			return
		}
		validateHeaderValue(value, field: field, issues: &issues)
	}

	private static func hasCIRegistrationEndpoint(_ configuration: AgentConfiguration) -> Bool {
		configuration.ciRegistrationURL != nil ||
			configuration.ciHeartbeatURL != nil ||
			configuration.ciDeregistrationURL != nil
	}

	private static func validateURL(_ url: URL?, field: String, issues: inout [AgentConfigurationValidationIssue]) {
		guard let url else {
			return
		}
		guard let scheme = url.scheme, ["http", "https"].contains(scheme), url.host != nil else {
			issues.append(.init("\(field) must be an http or https URL."))
			return
		}
		if scheme == "http", let host = url.host, !isLoopbackHost(host) {
			issues.append(.init("\(field) must use https unless it targets local loopback."))
		}
		if url.user != nil || url.password != nil {
			issues.append(.init("\(field) must not include credentials."))
		}
		if url.query != nil || url.fragment != nil {
			issues.append(.init("\(field) must not include query or fragment."))
		}
	}

	private static func validateOrigin(_ url: URL, field: String, issues: inout [AgentConfigurationValidationIssue]) {
		guard let scheme = url.scheme, ["http", "https"].contains(scheme), url.host != nil else {
			issues.append(.init("\(field) must be an http or https origin."))
			return
		}
		if scheme == "http", let host = url.host, !isLoopbackHost(host) {
			issues.append(.init("\(field) must use https unless it targets local loopback."))
		}
		if url.user != nil || url.password != nil {
			issues.append(.init("\(field) must not include credentials."))
		}
		if (!url.path.isEmpty && url.path != "/") || url.query != nil || url.fragment != nil {
			issues.append(.init("\(field) must not include a path, query, or fragment."))
		}
	}

	private static func validateNamedTunnelPublicURL(_ url: URL, issues: inout [AgentConfigurationValidationIssue]) {
		guard url.scheme == "https", url.host != nil else {
			issues.append(.init("Named tunnel public URL must be an https URL."))
			return
		}
		if url.user != nil || url.password != nil {
			issues.append(.init("Named tunnel public URL must not include credentials."))
		}
		if (!url.path.isEmpty && url.path != "/") || url.query != nil || url.fragment != nil {
			issues.append(.init("Named tunnel public URL must be a base URL like https://transwarp.example.com."))
		}
	}

	private static func validateCommand(_ command: String, jobLabel: String, issues: inout [AgentConfigurationValidationIssue]) {
		if !command.hasPrefix("/") {
			issues.append(.init("\(jobLabel) command must be an absolute executable path."))
		}
		if command.rangeOfCharacter(from: .whitespacesAndNewlines) != nil {
			issues.append(.init("\(jobLabel) command must not contain whitespace; put arguments in Arguments."))
		}
		if command.rangeOfCharacter(from: CharacterSet(charactersIn: ";&|`$<>")) != nil {
			issues.append(.init("\(jobLabel) command must be an executable path, not shell text."))
		}
		if ["sh", "bash", "zsh", "fish", "csh", "tcsh"].contains(URL(fileURLWithPath: command).lastPathComponent) {
			issues.append(.init("\(jobLabel) command must not invoke a shell directly."))
		}
	}

	private static func validateSecretReference(_ value: String, field: String, issues: inout [AgentConfigurationValidationIssue]) {
		guard let reference = SecretReference(value) else {
			if trimmed(value).hasPrefix("\(SecretReference.scheme):") {
				issues.append(.init("\(field) Keychain reference is invalid."))
			}
			return
		}
		if reference.service != SecretReference.defaultService {
			issues.append(.init("\(field) must use Keychain service \(SecretReference.defaultService)."))
		}
	}

	private static func isValidEnvironmentKey(_ key: String) -> Bool {
		guard let first = key.utf8.first else {
			return false
		}
		guard isEnvironmentKeyLetter(first) || first == CharacterByte.underscore else {
			return false
		}

		for byte in key.utf8.dropFirst() {
			if isEnvironmentKeyLetter(byte) || isEnvironmentKeyDigit(byte) || byte == CharacterByte.underscore {
				continue
			}
			return false
		}
		return true
	}

	private static func isValidStableIdentifier(_ value: String) -> Bool {
		for byte in value.utf8 {
			if isEnvironmentKeyLetter(byte) || isEnvironmentKeyDigit(byte) ||
				byte == CharacterByte.dot ||
				byte == CharacterByte.hyphen ||
				byte == CharacterByte.underscore {
				continue
			}
			return false
		}
		return true
	}

	private static func isValidHeaderName(_ value: String) -> Bool {
		guard !value.isEmpty else {
			return false
		}
		for byte in value.utf8 {
			if isEnvironmentKeyLetter(byte) || isEnvironmentKeyDigit(byte) {
				continue
			}
			if CharacterByte.headerNamePunctuation.contains(byte) {
				continue
			}
			return false
		}
		return true
	}

	private static func isReservedEnvironmentKey(_ key: String) -> Bool {
		key.hasPrefix("TRANSWARP_")
	}

	private static func isEnvironmentKeyLetter(_ byte: UInt8) -> Bool {
		(byte >= CharacterByte.uppercaseA && byte <= CharacterByte.uppercaseZ) ||
			(byte >= CharacterByte.lowercaseA && byte <= CharacterByte.lowercaseZ)
	}

	private static func isEnvironmentKeyDigit(_ byte: UInt8) -> Bool {
		byte >= CharacterByte.zero && byte <= CharacterByte.nine
	}

	private static func isExecutableCloudflared(_ path: String) -> Bool {
		if path == TunnelConfiguration.bundledCloudflaredPath {
			guard let bundled = Bundle.main.url(forResource: "cloudflared", withExtension: nil) else {
				return false
			}
			return isExecutableFile(bundled.path)
		}
		return isExecutableFile(path)
	}

	private static func isExecutableFile(_ path: String) -> Bool {
		FileManager.default.isExecutableFile(atPath: path)
	}

	private static func isLoopbackHost(_ host: String) -> Bool {
		["localhost", "127.0.0.1", "::1"].contains(host.lowercased())
	}

	private static func trimmed(_ value: String) -> String {
		value.trimmingCharacters(in: .whitespacesAndNewlines)
	}
}

private enum CharacterByte {
	static let uppercaseA = UInt8(ascii: "A")
	static let uppercaseZ = UInt8(ascii: "Z")
	static let lowercaseA = UInt8(ascii: "a")
	static let lowercaseZ = UInt8(ascii: "z")
	static let zero = UInt8(ascii: "0")
	static let nine = UInt8(ascii: "9")
	static let dot = UInt8(ascii: ".")
	static let hyphen = UInt8(ascii: "-")
	static let underscore = UInt8(ascii: "_")
	static let headerNamePunctuation = Set("!#$%&'*+-.^_`|~".utf8)
}

public struct AgentConfigurationValidationError: LocalizedError {
	public var errorDescription: String?

	public init(_ message: String) {
		errorDescription = message
	}
}
