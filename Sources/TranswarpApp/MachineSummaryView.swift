import SwiftUI
import TranswarpCore

struct MachineSummaryView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(alignment: .leading, spacing: 8) {
			HStack {
				Text("Machine")
					.font(.headline)
				Spacer()
				Button {
					model.reloadConfiguration()
				} label: {
					Label("Reload", systemImage: "arrow.clockwise")
				}
				.labelStyle(.iconOnly)
				.help("Reload configuration")
			}

			LabeledContent("Name", value: model.configuration?.machineName ?? "Unknown")
			LabeledContent("ID", value: model.configuration?.machineId ?? "Unknown")
			LabeledContent("Listen", value: model.configuration?.listenAddress ?? "Unknown")
			LabeledContent("Tunnel") {
				Label(tunnelSummary, systemImage: tunnelIcon)
					.foregroundStyle(tunnelColor)
			}
			LabeledContent("Registration") {
				Label(registrationSummary, systemImage: registrationIcon)
					.foregroundStyle(registrationColor)
			}
			LabeledContent("CI Target") {
				HStack {
					Label(ciTargetSummary, systemImage: ciTargetIcon)
						.foregroundStyle(ciTargetColor)
					Button {
						Task {
							await model.setAcceptingBuilds(!model.isAcceptingBuilds)
						}
					} label: {
						Label(model.isAcceptingBuilds ? "Pause" : "Resume", systemImage: model.isAcceptingBuilds ? "pause.circle" : "play.circle")
					}
					.disabled(!model.canToggleCIAvailability)
					.help(model.isAcceptingBuilds ? "Pause new CI builds without stopping the runner" : "Resume accepting new CI builds")
				}
			}
			if let capabilities = model.agentStatus?.capabilities {
				LabeledContent("Platform") {
					Text(platformSummary(capabilities))
						.lineLimit(1)
						.truncationMode(.middle)
						.textSelection(.enabled)
				}
				if let xcodeVersion = capabilities.xcodeVersion, !xcodeVersion.isEmpty {
					LabeledContent("Xcode") {
						Text(xcodeVersion)
							.lineLimit(1)
							.truncationMode(.middle)
							.textSelection(.enabled)
					}
				}
			}

			if let leaseExpiresAt = model.agentStatus?.registration?.leaseExpiresAt, !leaseExpiresAt.isEmpty {
				LabeledContent("Lease Expires") {
					Text(leaseExpiresAt)
						.lineLimit(1)
						.truncationMode(.middle)
						.textSelection(.enabled)
				}
			}

			if let lastSuccessAt = model.agentStatus?.registration?.lastSuccessAt, !lastSuccessAt.isEmpty {
				LabeledContent("Last Registration") {
					Text(lastSuccessAt)
						.lineLimit(1)
						.truncationMode(.middle)
						.textSelection(.enabled)
				}
			}

			LabeledContent("Builds") {
				Text(buildCountSummary)
			}

			if let origin = model.agentStatus?.tunnel.origin {
				LabeledContent("Origin") {
					Text(origin)
						.lineLimit(1)
						.truncationMode(.middle)
						.textSelection(.enabled)
				}
			}

			if let publicURL {
				LabeledContent("Public URL") {
					HStack {
						Text(publicURL)
							.lineLimit(1)
							.truncationMode(.middle)
							.textSelection(.enabled)
						Button {
							Task {
								await model.diagnosePublicEndpoint()
							}
						} label: {
							Label(model.publicEndpointDiagnosisInFlight ? "Diagnosing" : "Diagnose", systemImage: "network")
						}
						.disabled(!model.canDiagnosePublicEndpoint)
						.help(model.publicEndpointDiagnosisHelp)
					}
				}
			}

			if let tunnelError = model.agentStatus?.tunnel.error, !tunnelError.isEmpty {
				Text(tunnelError)
					.font(.caption)
					.foregroundStyle(.red)
			}

			if let readinessError = model.agentStatus?.tunnel.readinessError, !readinessError.isEmpty {
				Text(readinessError)
					.font(.caption)
					.foregroundStyle(.orange)
			}

			if let registrationError = model.agentStatus?.registration?.lastError, !registrationError.isEmpty {
				Text(registrationError)
					.font(.caption)
					.foregroundStyle(.orange)
			}

			if !model.configurationIssues.isEmpty {
				VStack(alignment: .leading, spacing: 4) {
					Text("Preflight")
						.font(.subheadline.weight(.semibold))
					ForEach(model.configurationIssues, id: \.self) { issue in
						Label(issue, systemImage: "exclamationmark.triangle")
							.font(.caption)
							.foregroundStyle(.orange)
					}
				}
			}

			if let lastError = model.lastError {
				Text(lastError)
					.font(.caption)
					.foregroundStyle(.red)
			}
		}
	}

	private var tunnelSummary: String {
		if let tunnel = model.agentStatus?.tunnel {
			if tunnel.mode == "off" || tunnel.state == "disabled" {
				return "Off"
			}
			if tunnel.ready == true {
				return "Reachable"
			}
			if tunnel.connected == true {
				return "\(tunnel.mode), connected"
			}
			if let pid = tunnel.pid {
				return "\(tunnel.mode), \(tunnel.state), PID \(pid)"
			}
			return "\(tunnel.mode), \(tunnel.state)"
		}
		return model.configuration?.tunnel.mode.rawValue ?? "Unknown"
	}

	private var tunnelIcon: String {
		if let tunnel = model.agentStatus?.tunnel {
			if tunnel.state == "failed" || tunnel.error != nil {
				return "exclamationmark.triangle"
			}
			if tunnel.mode == "off" || tunnel.state == "disabled" {
				return "network.slash"
			}
			if tunnel.ready == true {
				return "checkmark.circle"
			}
			return "network"
		}
		return "circle"
	}

	private var registrationSummary: String {
		guard let registration = model.agentStatus?.registration else {
			return model.configuration?.ciRegistrationURL == nil ? "Off" : "Unknown"
		}
		if !registration.configured || registration.state == "disabled" {
			return "Off"
		}
		if registration.state == "registered" {
			return "Registered"
		}
		if registration.state == "heartbeat_failed" {
			return "Heartbeat failed"
		}
		return registration.state
			.replacingOccurrences(of: "_", with: " ")
			.capitalized
	}

	private var registrationIcon: String {
		guard let registration = model.agentStatus?.registration else {
			return "circle"
		}
		if !registration.configured || registration.state == "disabled" {
			return "link.badge.plus"
		}
		if registration.state == "registered" {
			return "checkmark.circle"
		}
		if registration.state == "failed" || registration.state == "heartbeat_failed" {
			return "exclamationmark.triangle"
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

	private var ciTargetSummary: String {
		model.agentStatus?.ciTargetSummary ?? "Unknown"
	}

	private var ciTargetIcon: String {
		guard let status = model.agentStatus else {
			return "circle"
		}
		if status.isAvailableCITarget {
			return "checkmark.seal"
		}
		if status.registration?.state == "failed" || status.registration?.state == "heartbeat_failed" {
			return "exclamationmark.triangle"
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

	private func platformSummary(_ capabilities: RunnerCapabilities) -> String {
		var parts = [capabilities.os]
		if let osVersion = capabilities.osVersion, !osVersion.isEmpty {
			parts.append(osVersion)
		}
		parts.append(capabilities.architecture)
		if let cpuBrand = capabilities.cpuBrand, !cpuBrand.isEmpty {
			parts.append(cpuBrand)
		}
		return parts.joined(separator: " / ")
	}

	private var buildCountSummary: String {
		let active = model.agentStatus?.activeBuilds ?? 0
		let queued = model.agentStatus?.queuedBuilds ?? 0
		if let limit = model.agentStatus?.queuedBuildLimit, limit > 0 {
			return "\(active) active, \(queued)/\(limit) queued"
		}
		if queued > 0 {
			return "\(active) active, \(queued) queued"
		}
		return "\(active) active"
	}

	private var tunnelColor: Color {
		if let tunnel = model.agentStatus?.tunnel {
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
		return .secondary
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
	MachineSummaryView()
		.environment(AppModel())
}
