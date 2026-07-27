import Foundation
import Security

public struct AgentConfiguration: Codable, Equatable, Sendable {
	public var listenAddress: String
	public var machineId: String
	public var machineName: String
	public var sharedToken: String
	public var workspaceRoot: String
	public var preventSleep: Bool
	public var redactedValues: [String]
	public var ciRegistrationURL: URL?
	public var ciHeartbeatURL: URL?
	public var ciDeregistrationURL: URL?
	public var registrationToken: String
	public var ciAccessClientID: String
	public var ciAccessClientSecret: String
	public var runnerAccessClientID: String
	public var runnerAccessClientSecret: String
	public var allowedReportOrigins: [URL]
	public var heartbeatSeconds: Int
	public var tunnel: TunnelConfiguration
	public var jobs: [BuildJob]

	private enum CodingKeys: String, CodingKey {
		case listenAddress
		case machineId
		case machineName
		case sharedToken
		case workspaceRoot
		case preventSleep
		case redactedValues
		case ciRegistrationURL
		case ciHeartbeatURL
		case ciDeregistrationURL
		case registrationToken
		case ciAccessClientID
		case ciAccessClientSecret
		case runnerAccessClientID
		case runnerAccessClientSecret
		case allowedReportOrigins
		case heartbeatSeconds
		case tunnel
		case jobs
	}

	public init(
		listenAddress: String = "127.0.0.1:8188",
		machineId: String = "",
		machineName: String = Host.current().localizedName ?? "Transwarp Mac",
		sharedToken: String = "",
		workspaceRoot: String = "",
		preventSleep: Bool = true,
		redactedValues: [String] = [],
		ciRegistrationURL: URL? = nil,
		ciHeartbeatURL: URL? = nil,
		ciDeregistrationURL: URL? = nil,
		registrationToken: String = "",
		ciAccessClientID: String = "",
		ciAccessClientSecret: String = "",
		runnerAccessClientID: String = "",
		runnerAccessClientSecret: String = "",
		allowedReportOrigins: [URL] = [],
		heartbeatSeconds: Int = 30,
		tunnel: TunnelConfiguration = TunnelConfiguration(),
		jobs: [BuildJob] = []
	) {
		self.listenAddress = listenAddress
		self.machineId = machineId
		self.machineName = machineName
		self.sharedToken = sharedToken
		self.workspaceRoot = workspaceRoot
		self.preventSleep = preventSleep
		self.redactedValues = redactedValues
		self.ciRegistrationURL = ciRegistrationURL
		self.ciHeartbeatURL = ciHeartbeatURL
		self.ciDeregistrationURL = ciDeregistrationURL
		self.registrationToken = registrationToken
		self.ciAccessClientID = ciAccessClientID
		self.ciAccessClientSecret = ciAccessClientSecret
		self.runnerAccessClientID = runnerAccessClientID
		self.runnerAccessClientSecret = runnerAccessClientSecret
		self.allowedReportOrigins = allowedReportOrigins
		self.heartbeatSeconds = heartbeatSeconds
		self.tunnel = tunnel
		self.jobs = jobs
	}

	public init(from decoder: Decoder) throws {
		let container = try decoder.container(keyedBy: CodingKeys.self)
		listenAddress = try container.decode(String.self, forKey: .listenAddress)
		machineId = try container.decode(String.self, forKey: .machineId)
		machineName = try container.decode(String.self, forKey: .machineName)
		sharedToken = try container.decode(String.self, forKey: .sharedToken)
		workspaceRoot = try container.decodeIfPresent(String.self, forKey: .workspaceRoot) ?? ""
		preventSleep = try container.decodeIfPresent(Bool.self, forKey: .preventSleep) ?? true
		redactedValues = try container.decodeIfPresent([String].self, forKey: .redactedValues) ?? []
		ciRegistrationURL = try container.decodeIfPresent(URL.self, forKey: .ciRegistrationURL)
		ciHeartbeatURL = try container.decodeIfPresent(URL.self, forKey: .ciHeartbeatURL)
		ciDeregistrationURL = try container.decodeIfPresent(URL.self, forKey: .ciDeregistrationURL)
		registrationToken = try container.decodeIfPresent(String.self, forKey: .registrationToken) ?? ""
		ciAccessClientID = try container.decodeIfPresent(String.self, forKey: .ciAccessClientID) ?? ""
		ciAccessClientSecret = try container.decodeIfPresent(String.self, forKey: .ciAccessClientSecret) ?? ""
		runnerAccessClientID = try container.decodeIfPresent(String.self, forKey: .runnerAccessClientID) ?? ""
		runnerAccessClientSecret = try container.decodeIfPresent(String.self, forKey: .runnerAccessClientSecret) ?? ""
		allowedReportOrigins = try container.decodeIfPresent([URL].self, forKey: .allowedReportOrigins) ?? []
		heartbeatSeconds = try container.decodeIfPresent(Int.self, forKey: .heartbeatSeconds) ?? 30
		tunnel = try container.decodeIfPresent(TunnelConfiguration.self, forKey: .tunnel) ?? TunnelConfiguration()
		jobs = try container.decodeIfPresent([BuildJob].self, forKey: .jobs) ?? []
	}
}

public struct TunnelConfiguration: Codable, Equatable, Sendable {
	public static let bundledCloudflaredPath = "@bundle/cloudflared"

	public enum Mode: String, Codable, CaseIterable, Sendable {
		case off
		case quick
		case named
	}

	public var mode: Mode
	public var cloudflaredPath: String
	public var token: String
	public var name: String
	public var publicURL: URL?

	public init(
		mode: Mode = .off,
		cloudflaredPath: String = Self.bundledCloudflaredPath,
		token: String = "",
		name: String = "",
		publicURL: URL? = nil
	) {
		self.mode = mode
		self.cloudflaredPath = cloudflaredPath
		self.token = token
		self.name = name
		self.publicURL = publicURL
	}
}

public struct BuildJob: Codable, Equatable, Identifiable, Sendable {
	public var id: String
	public var label: String
	public var workingDirectory: String
	public var checkout: Bool
	public var allowedRepositories: [String]
	public var checkoutAuthorizationHeader: String
	public var command: String
	public var arguments: [String]
	public var environment: [String: String]
	public var redactedEnvironmentKeys: [String]
	public var timeoutSeconds: Int

	private enum CodingKeys: String, CodingKey {
		case id
		case label
		case workingDirectory
		case checkout
		case allowedRepositories
		case checkoutAuthorizationHeader
		case command
		case arguments
		case environment
		case redactedEnvironmentKeys
		case timeoutSeconds
	}

	public init(
		id: String,
		label: String,
		workingDirectory: String,
		checkout: Bool = false,
		allowedRepositories: [String] = [],
		checkoutAuthorizationHeader: String = "",
		command: String,
		arguments: [String] = [],
		environment: [String: String] = [:],
		redactedEnvironmentKeys: [String] = [],
		timeoutSeconds: Int = 3600
	) {
		self.id = id
		self.label = label
		self.workingDirectory = workingDirectory
		self.checkout = checkout
		self.allowedRepositories = allowedRepositories
		self.checkoutAuthorizationHeader = checkoutAuthorizationHeader
		self.command = command
		self.arguments = arguments
		self.environment = environment
		self.redactedEnvironmentKeys = redactedEnvironmentKeys
		self.timeoutSeconds = timeoutSeconds
	}

	public init(from decoder: Decoder) throws {
		let container = try decoder.container(keyedBy: CodingKeys.self)
		id = try container.decode(String.self, forKey: .id)
		label = try container.decodeIfPresent(String.self, forKey: .label) ?? ""
		workingDirectory = try container.decodeIfPresent(String.self, forKey: .workingDirectory) ?? ""
		checkout = try container.decodeIfPresent(Bool.self, forKey: .checkout) ?? false
		allowedRepositories = try container.decodeIfPresent([String].self, forKey: .allowedRepositories) ?? []
		checkoutAuthorizationHeader = try container.decodeIfPresent(String.self, forKey: .checkoutAuthorizationHeader) ?? ""
		command = try container.decode(String.self, forKey: .command)
		arguments = try container.decodeIfPresent([String].self, forKey: .arguments) ?? []
		environment = try container.decodeIfPresent([String: String].self, forKey: .environment) ?? [:]
		redactedEnvironmentKeys = try container.decodeIfPresent([String].self, forKey: .redactedEnvironmentKeys) ?? []
		timeoutSeconds = try container.decodeIfPresent(Int.self, forKey: .timeoutSeconds) ?? 3600
	}
}

public extension AgentConfiguration {
	static func starter(machineId: String = UUID().uuidString, sharedToken: String = RandomToken.generate()) -> AgentConfiguration {
		AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: machineId,
			machineName: Host.current().localizedName ?? "Transwarp Mac",
			sharedToken: sharedToken,
			workspaceRoot: "",
			preventSleep: true,
			redactedValues: [],
			heartbeatSeconds: 30,
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "xcode-version",
					label: "Xcode Version",
					workingDirectory: FileManager.default.homeDirectoryForCurrentUser.path,
					command: "/usr/bin/xcodebuild",
					arguments: ["-version"],
					timeoutSeconds: 300
				)
			]
		)
	}

	static func sample(machineId: String = UUID().uuidString) -> AgentConfiguration {
		AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: machineId,
			machineName: "Charlies-Mac-Studio",
			sharedToken: SecretReference(service: SecretReference.defaultService, account: "\(machineId)/shared_token").rawValue,
			workspaceRoot: "",
			preventSleep: true,
			redactedValues: [],
			ciRegistrationURL: URL(string: "https://ci.example.com/transwarp/register"),
			ciHeartbeatURL: URL(string: "https://ci.example.com/transwarp/heartbeat"),
			ciDeregistrationURL: URL(string: "https://ci.example.com/transwarp/deregister"),
			registrationToken: SecretReference(service: SecretReference.defaultService, account: "\(machineId)/registration_token").rawValue,
			ciAccessClientID: "replace-with-access-client-id",
			ciAccessClientSecret: SecretReference(service: SecretReference.defaultService, account: "\(machineId)/ci_access_client_secret").rawValue,
			runnerAccessClientID: "replace-with-runner-access-client-id",
			runnerAccessClientSecret: SecretReference(service: SecretReference.defaultService, account: "\(machineId)/runner_access_client_secret").rawValue,
			heartbeatSeconds: 30,
			tunnel: TunnelConfiguration(
				mode: .named,
				cloudflaredPath: TunnelConfiguration.bundledCloudflaredPath,
				token: SecretReference(service: SecretReference.defaultService, account: "\(machineId)/cloudflare_tunnel_token").rawValue,
				name: "",
				publicURL: URL(string: "https://transwarp.example.com")
			),
			jobs: [
				BuildJob(
					id: "xcode-debug",
					label: "Xcode Debug Build",
					workingDirectory: "",
					checkout: true,
					allowedRepositories: ["https://github.com/example/app.git"],
					checkoutAuthorizationHeader: SecretReference(
						service: SecretReference.defaultService,
						account: "\(machineId)/jobs/xcode-debug/checkout_authorization_header"
					).rawValue,
					command: "/usr/bin/xcodebuild",
					arguments: ["-scheme", "App", "-configuration", "Debug", "build"],
					redactedEnvironmentKeys: ["MATCH_PASSWORD"],
					timeoutSeconds: 3600
				)
			]
		)
	}
}

public enum RandomToken {
	public static func generate(byteCount: Int = 32) -> String {
		var bytes = [UInt8](repeating: 0, count: byteCount)
		let status = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
		if status != errSecSuccess {
			return UUID().uuidString.replacingOccurrences(of: "-", with: "") + UUID().uuidString.replacingOccurrences(of: "-", with: "")
		}
		return bytes.map { String(format: "%02x", $0) }.joined()
	}
}
