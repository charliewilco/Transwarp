import Foundation

public struct RunnerEvent: Codable, Equatable, Identifiable, Sendable {
	public enum Kind: String, Codable, Sendable {
		case info
		case tunnel
		case registration
		case build
		case log
		case error
	}

	public var id: UUID
	public var date: Date
	public var kind: Kind
	public var message: String
	public var buildId: String?
	public var jobId: String?
	public var sequence: Int?

	public init(
		id: UUID = UUID(),
		date: Date = Date(),
		kind: Kind,
		message: String,
		buildId: String? = nil,
		jobId: String? = nil,
		sequence: Int? = nil
	) {
		self.id = id
		self.date = date
		self.kind = kind
		self.message = message
		self.buildId = buildId
		self.jobId = jobId
		self.sequence = sequence
	}
}

public struct AgentStatus: Codable, Equatable, Sendable {
	public var machineId: String
	public var machineName: String
	public var listenAddress: String
	public var tunnelMode: String
	public var tunnel: TunnelStatus
	public var registration: RegistrationStatus?
	public var capabilities: RunnerCapabilities?
	public var publicURL: URL?
	public var acceptingBuilds: Bool?
	public var activeBuilds: Int
	public var queuedBuilds: Int?
	public var queuedBuildLimit: Int?
	public var jobs: [String]
	public var recentBuilds: [BuildStatus]

	private enum CodingKeys: String, CodingKey {
		case machineId
		case machineName
		case listenAddress
		case tunnelMode
		case tunnel
		case registration
		case capabilities
		case publicURL = "publicUrl"
		case acceptingBuilds
		case activeBuilds
		case queuedBuilds
		case queuedBuildLimit
		case jobs
		case recentBuilds
	}

	public init(
		machineName: String,
		machineId: String,
		listenAddress: String,
		tunnelMode: String,
		tunnel: TunnelStatus,
		registration: RegistrationStatus? = nil,
		capabilities: RunnerCapabilities? = nil,
		publicURL: URL?,
		acceptingBuilds: Bool? = nil,
		activeBuilds: Int,
		queuedBuilds: Int? = nil,
		queuedBuildLimit: Int? = nil,
		jobs: [String],
		recentBuilds: [BuildStatus] = []
	) {
		self.machineId = machineId
		self.machineName = machineName
		self.listenAddress = listenAddress
		self.tunnelMode = tunnelMode
		self.tunnel = tunnel
		self.registration = registration
		self.capabilities = capabilities
		self.publicURL = publicURL
		self.acceptingBuilds = acceptingBuilds
		self.activeBuilds = activeBuilds
		self.queuedBuilds = queuedBuilds
		self.queuedBuildLimit = queuedBuildLimit
		self.jobs = jobs
		self.recentBuilds = recentBuilds
	}

	public var isQueueFull: Bool {
		guard let queuedBuildLimit, queuedBuildLimit > 0 else {
			return false
		}
		return (queuedBuilds ?? 0) >= queuedBuildLimit
	}

	public var isAcceptingBuilds: Bool {
		acceptingBuilds ?? true
	}

	public var isAvailableCITarget: Bool {
		guard let registration,
			  registration.configured,
			  registration.state == "registered",
			  registration.hasLiveLease(),
			  tunnel.ready == true,
			  isAcceptingBuilds,
			  !isQueueFull else {
			return false
		}
		return publicURL != nil || !(tunnel.publicURL ?? "").isEmpty
	}

	public var ciTargetSummary: String {
		guard let registration, registration.configured else {
			return "Registration off"
		}
		if registration.state == "failed" {
			return "Registration failed"
		}
		if registration.state == "heartbeat_failed" {
			return "Heartbeat failed"
		}
		if registration.state != "registered" {
			return registration.state
				.replacingOccurrences(of: "_", with: " ")
				.capitalized
		}
		if registration.leaseExpiresAt == nil {
			return "Missing registration lease"
		}
		if !registration.hasLiveLease() {
			return "Registration lease expired"
		}
		if tunnel.ready != true {
			return "Waiting for tunnel"
		}
		if !isAcceptingBuilds {
			return "Paused"
		}
		if publicURL == nil && (tunnel.publicURL ?? "").isEmpty {
			return "Missing public URL"
		}
		if isQueueFull {
			return "Queue full"
		}
		return "Available to CI"
	}
}

public struct RunnerCapabilities: Codable, Equatable, Sendable {
	public var os: String
	public var osVersion: String?
	public var architecture: String
	public var cpuBrand: String?
	public var cpuCount: Int?
	public var memoryBytes: UInt64?
	public var xcodeVersion: String?
	public var developerDir: String?

	public init(
		os: String,
		osVersion: String? = nil,
		architecture: String,
		cpuBrand: String? = nil,
		cpuCount: Int? = nil,
		memoryBytes: UInt64? = nil,
		xcodeVersion: String? = nil,
		developerDir: String? = nil
	) {
		self.os = os
		self.osVersion = osVersion
		self.architecture = architecture
		self.cpuBrand = cpuBrand
		self.cpuCount = cpuCount
		self.memoryBytes = memoryBytes
		self.xcodeVersion = xcodeVersion
		self.developerDir = developerDir
	}
}

public struct RegistrationStatus: Codable, Equatable, Sendable {
	public var configured: Bool
	public var state: String
	public var lastAction: String?
	public var lastSuccessAt: String?
	public var leaseExpiresAt: String?
	public var lastError: String?

	public init(
		configured: Bool,
		state: String,
		lastAction: String? = nil,
		lastSuccessAt: String? = nil,
		leaseExpiresAt: String? = nil,
		lastError: String? = nil
	) {
		self.configured = configured
		self.state = state
		self.lastAction = lastAction
		self.lastSuccessAt = lastSuccessAt
		self.leaseExpiresAt = leaseExpiresAt
		self.lastError = lastError
	}

	public func hasLiveLease(now: Date = Date()) -> Bool {
		guard let leaseExpiresAt,
			  let expiresAt = Self.parseTimestamp(leaseExpiresAt) else {
			return false
		}
		return expiresAt > now
	}

	private static func parseTimestamp(_ value: String) -> Date? {
		let fractionalFormatter = ISO8601DateFormatter()
		fractionalFormatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
		if let date = fractionalFormatter.date(from: value) {
			return date
		}
		let formatter = ISO8601DateFormatter()
		formatter.formatOptions = [.withInternetDateTime]
		return formatter.date(from: value)
	}
}

public struct BuildStatus: Codable, Equatable, Identifiable, Sendable {
	public var buildId: String
	public var jobId: String
	public var requestId: String?
	public var status: String
	public var createdAt: String
	public var reportStatus: String?
	public var reportError: String?
	public var result: BuildResult?

	public var id: String {
		buildId
	}

	public var isRunning: Bool {
		status == "running"
	}

	public var isQueued: Bool {
		status == "queued"
	}

	public var isTerminal: Bool {
		status == "passed" || status == "failed" || status == "canceled"
	}

	public init(buildId: String, jobId: String, requestId: String? = nil, status: String, createdAt: String, reportStatus: String? = nil, reportError: String? = nil, result: BuildResult? = nil) {
		self.buildId = buildId
		self.jobId = jobId
		self.requestId = requestId
		self.status = status
		self.createdAt = createdAt
		self.reportStatus = reportStatus
		self.reportError = reportError
		self.result = result
	}
}

public struct BuildResult: Codable, Equatable, Sendable {
	public var buildId: String
	public var jobId: String
	public var requestId: String?
	public var startedAt: String
	public var endedAt: String
	public var exitCode: Int
	public var error: String?

	public init(buildId: String, jobId: String, requestId: String? = nil, startedAt: String, endedAt: String, exitCode: Int, error: String? = nil) {
		self.buildId = buildId
		self.jobId = jobId
		self.requestId = requestId
		self.startedAt = startedAt
		self.endedAt = endedAt
		self.exitCode = exitCode
		self.error = error
	}
}

public struct TunnelStatus: Codable, Equatable, Sendable {
	public var mode: String
	public var state: String
	public var origin: String?
	public var publicURL: String?
	public var connected: Bool?
	public var ready: Bool?
	public var pid: Int?
	public var error: String?
	public var readinessError: String?

	private enum CodingKeys: String, CodingKey {
		case mode
		case state
		case origin
		case publicURL = "publicUrl"
		case connected
		case ready
		case pid
		case error
		case readinessError
	}

	public init(mode: String, state: String, origin: String? = nil, publicURL: String? = nil, connected: Bool? = nil, ready: Bool? = nil, pid: Int? = nil, error: String? = nil, readinessError: String? = nil) {
		self.mode = mode
		self.state = state
		self.origin = origin
		self.publicURL = publicURL
		self.connected = connected
		self.ready = ready
		self.pid = pid
		self.error = error
		self.readinessError = readinessError
	}
}

public struct BuildStartPayload: Codable, Equatable, Sendable {
	public var jobId: String
	public var requestId: String
	public var repoURL: String?
	public var ref: String?
	public var commit: String?

	private enum CodingKeys: String, CodingKey {
		case jobId = "job_id"
		case requestId = "request_id"
		case repoURL = "repo_url"
		case ref
		case commit
	}

	public init(jobId: String, requestId: String, repoURL: String? = nil, ref: String? = nil, commit: String? = nil) {
		self.jobId = jobId
		self.requestId = requestId
		self.repoURL = repoURL
		self.ref = ref
		self.commit = commit
	}
}

public struct BuildStartReceipt: Codable, Equatable, Sendable {
	public var buildId: String
	public var status: String
	public var logsURL: String
	public var cancelURL: String

	private enum CodingKeys: String, CodingKey {
		case buildId = "build_id"
		case status
		case logsURL = "logs_url"
		case cancelURL = "cancel_url"
	}

	public init(buildId: String, status: String, logsURL: String, cancelURL: String) {
		self.buildId = buildId
		self.status = status
		self.logsURL = logsURL
		self.cancelURL = cancelURL
	}
}

public struct AvailabilityUpdatePayload: Codable, Equatable, Sendable {
	public var acceptingBuilds: Bool

	private enum CodingKeys: String, CodingKey {
		case acceptingBuilds = "accepting_builds"
	}

	public init(acceptingBuilds: Bool) {
		self.acceptingBuilds = acceptingBuilds
	}
}
