import SwiftUI

struct RunnerOverviewView: View {
	@Environment(AppModel.self) private var model

	private let columns = [
		GridItem(.adaptive(minimum: 168, maximum: 240), spacing: 12)
	]

	var body: some View {
		VStack(alignment: .leading, spacing: 12) {
			HStack(alignment: .firstTextBaseline) {
				VStack(alignment: .leading, spacing: 3) {
					Text("Runner")
						.font(.headline)
					Text(readinessSummary)
						.font(.subheadline)
						.foregroundStyle(.secondary)
						.lineLimit(2)
				}

				Spacer()

				Label(publicEndpointSummary, systemImage: "network")
					.font(.caption.weight(.medium))
					.foregroundStyle(publicEndpointColor)
			}

			LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
				overviewTile(
					"Process",
					value: processValue,
					detail: processDetail,
					systemImage: processIcon,
					tint: processColor
				)

				overviewTile(
					"Tunnel",
					value: tunnelValue,
					detail: tunnelDetail,
					systemImage: tunnelIcon,
					tint: tunnelColor
				)

				overviewTile(
					"Registration",
					value: registrationValue,
					detail: registrationDetail,
					systemImage: registrationIcon,
					tint: registrationColor
				)

				overviewTile(
					"CI Target",
					value: ciTargetValue,
					detail: ciTargetDetail,
					systemImage: ciTargetIcon,
					tint: ciTargetColor
				)

				overviewTile(
					"Builds",
					value: buildValue,
					detail: buildDetail,
					systemImage: "hammer",
					tint: buildColor
				)
			}
		}
	}

	private func overviewTile(
		_ title: String,
		value: String,
		detail: String,
		systemImage: String,
		tint: Color
	) -> some View {
		VStack(alignment: .leading, spacing: 10) {
			HStack(spacing: 8) {
				Image(systemName: systemImage)
					.foregroundStyle(tint)
					.frame(width: 18)
				Text(title)
					.font(.caption.weight(.medium))
					.foregroundStyle(.secondary)
					.lineLimit(1)
				Spacer(minLength: 0)
			}

			VStack(alignment: .leading, spacing: 3) {
				Text(value)
					.font(.title3.weight(.semibold))
					.lineLimit(1)
					.minimumScaleFactor(0.75)
				Text(detail)
					.font(.caption)
					.foregroundStyle(.secondary)
					.lineLimit(2)
					.fixedSize(horizontal: false, vertical: true)
			}
		}
		.padding(14)
		.frame(maxWidth: .infinity, minHeight: 116, alignment: .topLeading)
		.background(.thinMaterial, in: RoundedRectangle(cornerRadius: 8))
		.overlay {
			RoundedRectangle(cornerRadius: 8)
				.strokeBorder(Color(nsColor: .separatorColor).opacity(0.6))
		}
	}

	private var readinessSummary: String {
		if let status = model.agentStatus, status.isAvailableCITarget {
			return "Ready to receive CI dispatch through the configured tunnel."
		}
		if !model.configurationIssues.isEmpty {
			return "Configuration needs attention before the runner can start."
		}
		if !model.isRunning {
			return "Start the runner to open the local listener and tunnel."
		}
		return model.agentStatus?.ciTargetSummary ?? "Waiting for runner status."
	}

	private var processValue: String {
		if model.status.isRunning {
			return "Running"
		}
		if model.status.isActive {
			return model.status.label
		}
		return "Stopped"
	}

	private var processDetail: String {
		switch model.status {
		case let .running(pid):
			return "PID \(pid)"
		case .starting:
			return "Launching transwarp-runner"
		case let .stopping(pid):
			return "Stopping PID \(pid)"
		case let .stoppedWithFailure(code):
			return "Exited with code \(code)"
		case .stopped:
			return model.configuration == nil ? "No local configuration loaded" : "Ready to start locally"
		}
	}

	private var processIcon: String {
		if model.status.isRunning {
			return "play.circle.fill"
		}
		if model.status.isActive {
			return "arrow.triangle.2.circlepath"
		}
		return "stop.circle"
	}

	private var processColor: Color {
		switch model.status {
		case .running:
			return .green
		case .starting, .stopping:
			return .orange
		case .stoppedWithFailure:
			return .red
		case .stopped:
			return .secondary
		}
	}

	private var tunnelValue: String {
		guard let tunnel = model.agentStatus?.tunnel else {
			return model.configuration?.tunnel.mode.rawValue.capitalized ?? "Unknown"
		}
		if tunnel.mode == "off" || tunnel.state == "disabled" {
			return "Off"
		}
		if tunnel.ready == true {
			return "Reachable"
		}
		if tunnel.connected == true {
			return "Connected"
		}
		return tunnel.state.replacingOccurrences(of: "_", with: " ").capitalized
	}

	private var tunnelDetail: String {
		if let error = model.agentStatus?.tunnel.error, !error.isEmpty {
			return error
		}
		if let readinessError = model.agentStatus?.tunnel.readinessError, !readinessError.isEmpty {
			return readinessError
		}
		if let publicURL {
			return publicURL
		}
		if let origin = model.agentStatus?.tunnel.origin {
			return origin
		}
		return "Named tunnel required for stable CI dispatch"
	}

	private var tunnelIcon: String {
		guard let tunnel = model.agentStatus?.tunnel else {
			return "circle"
		}
		if tunnel.state == "failed" || tunnel.error != nil {
			return "exclamationmark.triangle.fill"
		}
		if tunnel.mode == "off" || tunnel.state == "disabled" {
			return "network.slash"
		}
		if tunnel.ready == true {
			return "checkmark.circle.fill"
		}
		return "network"
	}

	private var tunnelColor: Color {
		guard let tunnel = model.agentStatus?.tunnel else {
			return .secondary
		}
		if tunnel.state == "failed" || tunnel.error != nil {
			return .red
		}
		if tunnel.ready == true {
			return .green
		}
		if tunnel.mode == "off" || tunnel.state == "disabled" {
			return .secondary
		}
		return .orange
	}

	private var registrationValue: String {
		guard let registration = model.agentStatus?.registration else {
			return model.configuration?.ciRegistrationURL == nil ? "Off" : "Unknown"
		}
		if !registration.configured || registration.state == "disabled" {
			return "Off"
		}
		if registration.state == "registered" {
			return "Registered"
		}
		return registration.state.replacingOccurrences(of: "_", with: " ").capitalized
	}

	private var registrationDetail: String {
		if let lastError = model.agentStatus?.registration?.lastError, !lastError.isEmpty {
			return lastError
		}
		if let leaseExpiresAt = model.agentStatus?.registration?.leaseExpiresAt, !leaseExpiresAt.isEmpty {
			return "Lease expires \(leaseExpiresAt)"
		}
		if model.configuration?.ciRegistrationURL == nil {
			return "No coordinator registration URL configured"
		}
		return "Heartbeat will keep this Mac registered"
	}

	private var registrationIcon: String {
		guard let registration = model.agentStatus?.registration else {
			return "circle"
		}
		if !registration.configured || registration.state == "disabled" {
			return "link.badge.plus"
		}
		if registration.state == "registered" {
			return "checkmark.seal.fill"
		}
		if registration.state == "failed" || registration.state == "heartbeat_failed" {
			return "exclamationmark.triangle.fill"
		}
		return "arrow.triangle.2.circlepath"
	}

	private var registrationColor: Color {
		guard let registration = model.agentStatus?.registration else {
			return .secondary
		}
		if registration.state == "registered" {
			return .green
		}
		if registration.state == "failed" || registration.state == "heartbeat_failed" {
			return .orange
		}
		return .secondary
	}

	private var ciTargetValue: String {
		model.agentStatus?.ciTargetSummary ?? "Unknown"
	}

	private var ciTargetDetail: String {
		if model.isAcceptingBuilds {
			return "Accepting new build requests when reachable"
		}
		return "Paused for new CI work"
	}

	private var ciTargetIcon: String {
		guard let status = model.agentStatus else {
			return "circle"
		}
		if status.isAvailableCITarget {
			return "checkmark.seal.fill"
		}
		if status.registration?.state == "failed" || status.registration?.state == "heartbeat_failed" {
			return "exclamationmark.triangle.fill"
		}
		return "hourglass"
	}

	private var ciTargetColor: Color {
		guard let status = model.agentStatus else {
			return .secondary
		}
		if status.isAvailableCITarget {
			return .green
		}
		if status.registration?.state == "failed" || status.registration?.state == "heartbeat_failed" {
			return .orange
		}
		return .secondary
	}

	private var buildValue: String {
		if model.activeBuildCount > 0 {
			return "\(model.activeBuildCount) active"
		}
		if model.queuedBuildCount > 0 {
			return "\(model.queuedBuildCount) queued"
		}
		return "Idle"
	}

	private var buildDetail: String {
		if let limit = model.agentStatus?.queuedBuildLimit, limit > 0 {
			return "\(model.queuedBuildCount)/\(limit) queued"
		}
		if model.agentStatus == nil {
			return "No runner status yet"
		}
		return "Ready for the next accepted request"
	}

	private var buildColor: Color {
		if model.activeBuildCount > 0 {
			return .accentColor
		}
		if model.queuedBuildCount > 0 {
			return .orange
		}
		return .secondary
	}

	private var publicEndpointSummary: String {
		publicURL == nil ? "No public endpoint" : "Public endpoint configured"
	}

	private var publicEndpointColor: Color {
		publicURL == nil ? .secondary : .green
	}

	private var publicURL: String? {
		if let statusURL = model.agentStatus?.publicURL?.absoluteString {
			return statusURL
		}
		if let quickURL = model.agentStatus?.tunnel.publicURL {
			return quickURL
		}
		return model.configuration?.tunnel.publicURL?.absoluteString
	}
}

#Preview {
	RunnerOverviewView()
		.environment(AppModel())
}
