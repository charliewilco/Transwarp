import Foundation
import TranswarpCore

struct PublicEndpointDiagnosis {
	static func request(
		configuration: AgentConfiguration,
		resolveSecret: (String) throws -> String
	) throws -> URLRequest {
		guard let publicURL = configuration.tunnel.publicURL else {
			throw PublicEndpointDiagnosisError("Named tunnel public URL is not configured.")
		}

		let url = publicURL.appending(path: "status")
		var request = URLRequest(
			url: url,
			cachePolicy: .reloadIgnoringLocalAndRemoteCacheData,
			timeoutInterval: 20
		)
		request.httpMethod = "GET"
		request.setValue("Bearer \(try resolveSecret(configuration.sharedToken))", forHTTPHeaderField: "Authorization")

		let accessClientID = trimmed(configuration.runnerAccessClientID)
		let accessClientSecret = trimmed(configuration.runnerAccessClientSecret)
		if !accessClientID.isEmpty && !accessClientSecret.isEmpty {
			request.setValue(accessClientID, forHTTPHeaderField: "CF-Access-Client-Id")
			request.setValue(try resolveSecret(accessClientSecret), forHTTPHeaderField: "CF-Access-Client-Secret")
		}

		return request
	}

	static func event(for status: AgentStatus) -> RunnerEvent {
		if status.isAvailableCITarget {
			return RunnerEvent(
				kind: .tunnel,
				message: "Public runner endpoint is reachable and available to CI"
			)
		}
		return RunnerEvent(
			kind: .tunnel,
			message: "Public runner endpoint is reachable but not available to CI: \(status.ciTargetSummary)"
		)
	}

	static func validate(status: AgentStatus, configuration: AgentConfiguration) throws {
		let expectedMachineID = trimmed(configuration.machineId)
		if !expectedMachineID.isEmpty && status.machineId != expectedMachineID {
			throw PublicEndpointDiagnosisError("Public runner endpoint reported machine ID \(status.machineId), expected \(expectedMachineID).")
		}

		guard let expectedPublicURL = configuration.tunnel.publicURL else {
			return
		}
		let expected = normalizedURLString(expectedPublicURL.absoluteString)
		let reportedURLs = [
			status.publicURL?.absoluteString,
			status.tunnel.publicURL
		]
			.compactMap { $0 }
			.map(normalizedURLString)

		if !reportedURLs.contains(expected) {
			throw PublicEndpointDiagnosisError("Public runner endpoint did not report configured public URL \(expectedPublicURL.absoluteString).")
		}
	}

	private static func trimmed(_ value: String) -> String {
		value.trimmingCharacters(in: .whitespacesAndNewlines)
	}

	private static func normalizedURLString(_ value: String) -> String {
		trimmed(value).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
	}
}

struct PublicEndpointDiagnosisError: LocalizedError {
	var errorDescription: String?

	init(_ message: String) {
		errorDescription = message
	}
}
