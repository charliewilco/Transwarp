import Foundation
import TranswarpCore

struct CIWorkflowSecretValueExport {
	static func canCopyValues(
		mode: GitHubActionWorkflow.Mode,
		configuration: AgentConfiguration?
	) -> Bool {
		guard let configuration else {
			return false
		}
		switch mode {
		case .direct:
			return hasNamedTunnelPublicURL(configuration) &&
				!trimmed(configuration.sharedToken).isEmpty &&
				hasCompleteAccessPair(id: configuration.runnerAccessClientID, secret: configuration.runnerAccessClientSecret)
		case .coordinator:
			return inferredCoordinatorBaseURL(configuration: configuration) != nil &&
				hasCompleteAccessPair(id: configuration.ciAccessClientID, secret: configuration.ciAccessClientSecret)
		case .releaseEvidence:
			return hasNamedTunnelPublicURL(configuration) &&
				hasCompleteAccessPair(id: configuration.runnerAccessClientID, secret: configuration.runnerAccessClientSecret)
		case .selfHosted:
			return false
		}
	}

	static func text(
		mode: GitHubActionWorkflow.Mode,
		configuration: AgentConfiguration,
		resolveSecret: (String) throws -> String,
		cloudflaredVersion: (AgentConfiguration) -> String? = packagedCloudflaredVersion
	) throws -> String {
		switch mode {
		case .direct:
			return try directText(configuration: configuration, resolveSecret: resolveSecret)
		case .coordinator:
			return try coordinatorText(configuration: configuration, resolveSecret: resolveSecret)
		case .releaseEvidence:
			return try releaseEvidenceText(
				configuration: configuration,
				resolveSecret: resolveSecret,
				cloudflaredVersion: cloudflaredVersion
			)
		case .selfHosted:
			return ""
		}
	}

	private static func directText(
		configuration: AgentConfiguration,
		resolveSecret: (String) throws -> String
	) throws -> String {
		let publicURL = try namedTunnelPublicURL(configuration: configuration, label: "Direct workflow")

		var lines = [
			"TRANSWARP_URL=\(shellValue(publicURL))",
			"TRANSWARP_TOKEN=\(shellValue(try resolveSecret(configuration.sharedToken)))"
		]
		try appendRunnerAccessValues(configuration: configuration, lines: &lines, resolveSecret: resolveSecret)
		return lines.joined(separator: "\n")
	}

	private static func releaseEvidenceText(
		configuration: AgentConfiguration,
		resolveSecret: (String) throws -> String,
		cloudflaredVersion: (AgentConfiguration) -> String?
	) throws -> String {
		let publicURL = try namedTunnelPublicURL(configuration: configuration, label: "Release evidence")

		var lines = [
			"TRANSWARP_PUBLIC_URL=\(shellValue(publicURL))"
		]
		if let version = cloudflaredVersion(configuration)?.trimmingCharacters(in: .whitespacesAndNewlines),
		   !version.isEmpty {
			lines.append("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION=\(shellValue(version))")
		}
		try appendRunnerAccessValues(configuration: configuration, lines: &lines, resolveSecret: resolveSecret)
		return lines.joined(separator: "\n")
	}

	private static func namedTunnelPublicURL(configuration: AgentConfiguration, label: String) throws -> String {
		guard hasNamedTunnelPublicURL(configuration),
			  let publicURL = configuration.tunnel.publicURL?.absoluteString else {
			throw CIWorkflowSecretValueExportError("\(label) secret values need a named tunnel public URL.")
		}
		return publicURL
	}

	private static func coordinatorText(
		configuration: AgentConfiguration,
		resolveSecret: (String) throws -> String
	) throws -> String {
		guard let coordinatorURL = inferredCoordinatorBaseURL(configuration: configuration) else {
			throw CIWorkflowSecretValueExportError("Coordinator workflow secret values need a standard coordinator registration URL.")
		}

		var lines = [
			"TRANSWARP_COORDINATOR_URL=\(shellValue(coordinatorURL))",
			"# TRANSWARP_COORDINATOR_TOKEN=<CI/operator coordinator bearer token>",
			"# TRANSWARP_COORDINATOR_TARGET_TOKEN=<target callback token for coordinator deployment and Mac registration_token; do not add to GitHub Actions>",
			"# TRANSWARP_TOKEN=<runner bearer token for coordinator deployment only; do not add to GitHub Actions>"
		]
		try appendAccessValues(
			id: configuration.ciAccessClientID,
			secret: configuration.ciAccessClientSecret,
			label: "Coordinator Access",
			lines: &lines,
			resolveSecret: resolveSecret
		)
		return lines.joined(separator: "\n")
	}

	private static func appendRunnerAccessValues(
		configuration: AgentConfiguration,
		lines: inout [String],
		resolveSecret: (String) throws -> String
	) throws {
		try appendAccessValues(
			id: configuration.runnerAccessClientID,
			secret: configuration.runnerAccessClientSecret,
			label: "Runner Access",
			lines: &lines,
			resolveSecret: resolveSecret
		)
	}

	private static func appendAccessValues(
		id: String,
		secret: String,
		label: String,
		lines: inout [String],
		resolveSecret: (String) throws -> String
	) throws {
		let accessClientID = trimmed(id)
		let accessClientSecret = trimmed(secret)
		if accessClientID.isEmpty && accessClientSecret.isEmpty {
			return
		}
		if accessClientID.isEmpty || accessClientSecret.isEmpty {
			throw CIWorkflowSecretValueExportError("\(label) client ID and secret must both be configured before copying values.")
		}
		lines.append("TRANSWARP_ACCESS_CLIENT_ID=\(shellValue(accessClientID))")
		lines.append("TRANSWARP_ACCESS_CLIENT_SECRET=\(shellValue(try resolveSecret(accessClientSecret)))")
	}

	private static func inferredCoordinatorBaseURL(configuration: AgentConfiguration) -> String? {
		guard let url = configuration.ciRegistrationURL, url.path == "/transwarp/register" else {
			return nil
		}
		var components = URLComponents()
		components.scheme = url.scheme
		components.host = url.host
		components.port = url.port
		return components.url?.absoluteString
	}

	private static func shellValue(_ value: String) -> String {
		"'\(value.replacingOccurrences(of: "'", with: "'\"'\"'"))'"
	}

	private static func trimmed(_ value: String) -> String {
		value.trimmingCharacters(in: .whitespacesAndNewlines)
	}

	private static func hasNamedTunnelPublicURL(_ configuration: AgentConfiguration) -> Bool {
		configuration.tunnel.mode == .named &&
			configuration.tunnel.publicURL?.absoluteString.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false
	}

	private static func hasCompleteAccessPair(id: String, secret: String) -> Bool {
		let accessClientID = trimmed(id)
		let accessClientSecret = trimmed(secret)
		return (accessClientID.isEmpty && accessClientSecret.isEmpty) ||
			(!accessClientID.isEmpty && !accessClientSecret.isEmpty)
	}

	private static func packagedCloudflaredVersion(configuration: AgentConfiguration) -> String? {
		guard configuration.tunnel.cloudflaredPath == TunnelConfiguration.bundledCloudflaredPath,
			  let manifestURL = Bundle.main.url(forResource: "TranswarpManifest", withExtension: "json"),
			  let data = try? Data(contentsOf: manifestURL),
			  let manifest = try? JSONDecoder().decode(TranswarpManifest.self, from: data) else {
			return nil
		}
		return manifest.cloudflaredVersion
	}
}

struct CIWorkflowSecretValueExportError: LocalizedError {
	var errorDescription: String?

	init(_ message: String) {
		errorDescription = message
	}
}

private struct TranswarpManifest: Decodable {
	let cloudflaredVersion: String

	private enum CodingKeys: String, CodingKey {
		case cloudflaredVersion = "cloudflared_version"
	}
}
