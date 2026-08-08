import SwiftUI

struct StatusHeaderView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		HStack(spacing: 12) {
			Image(systemName: statusIcon)
				.font(.title2)
				.foregroundStyle(statusColor)
				.frame(width: 24)

			VStack(alignment: .leading, spacing: 2) {
				Text(statusTitle)
					.font(.headline)
				Text(statusDetail)
					.font(.caption)
					.foregroundStyle(.secondary)
					.lineLimit(1)
					.truncationMode(.middle)
			}

			Spacer()

			SettingsLink {
				Label("Settings", systemImage: "gearshape")
			}
			.labelStyle(.iconOnly)
			.help("Settings")

			Menu {
				Button {
					Task {
						await model.runTestBuild()
					}
				} label: {
					Label(model.testBuildInFlight ? "Testing" : "Run Test Build", systemImage: "hammer")
				}
				.disabled(!model.canRunTestBuild)

				Button {
					Task {
						await model.setAcceptingBuilds(!model.isAcceptingBuilds)
					}
				} label: {
					Label(
						model.isAcceptingBuilds ? "Pause New Builds" : "Resume New Builds",
						systemImage: model.isAcceptingBuilds ? "pause.circle" : "play.circle"
					)
				}
				.disabled(!model.canToggleCIAvailability)

				Divider()

				Button {
					model.reloadConfiguration()
				} label: {
					Label("Reload Configuration", systemImage: "arrow.clockwise")
				}

				Button {
					model.revealConfiguration()
				} label: {
					Label("Reveal Configuration", systemImage: "folder")
				}

				Button {
					Task {
						await model.diagnosePublicEndpoint()
					}
				} label: {
					Label(
						model.publicEndpointDiagnosisInFlight ? "Diagnosing Endpoint" : "Diagnose Endpoint",
						systemImage: "network"
					)
				}
				.disabled(!model.canDiagnosePublicEndpoint)
			} label: {
				Label("More", systemImage: "ellipsis.circle")
			}
			.menuStyle(.borderlessButton)

			Button(model.canStop ? "Stop" : "Start") {
				model.canStop ? model.stop() : model.start()
			}
			.keyboardShortcut(.return, modifiers: [.command])
			.buttonStyle(.borderedProminent)
			.disabled(!model.canStop && !model.canStart)
			.help(model.canStart || model.canStop ? "" : model.configurationIssues.first ?? "Configuration is not ready")
		}
	}

	private var statusTitle: String {
		if !model.configurationIssues.isEmpty {
			return "Needs Setup"
		}
		if case .stoppedWithFailure = model.status {
			return "Runner Failed"
		}
		if model.agentStatus?.isAvailableCITarget == true {
			return "Available"
		}
		if model.isRunning && model.agentStatus?.isCIAcceptingBuilds == false {
			return "Paused"
		}
		switch model.status {
		case .stopped:
			return "Stopped"
		case .starting:
			return "Starting"
		case .running:
			return "Running"
		case .stopping:
			return "Stopping"
		case .stoppedWithFailure:
			return "Runner Failed"
		}
	}

	private var statusDetail: String {
		if !model.configurationIssues.isEmpty {
			return "Open Settings"
		}
		if model.activeBuildCount == 1 {
			return "1 build running"
		}
		if model.activeBuildCount > 1 {
			return "\(model.activeBuildCount) builds running"
		}
		if model.queuedBuildCount == 1 {
			return "1 build queued"
		}
		if model.queuedBuildCount > 1 {
			return "\(model.queuedBuildCount) builds queued"
		}
		if case let .stoppedWithFailure(code) = model.status {
			return "Exit code \(code)"
		}
		if model.agentStatus?.isAvailableCITarget == true {
			return "Accepting CI builds"
		}
		if model.isRunning && model.agentStatus?.isCIAcceptingBuilds == false {
			return "New CI builds paused"
		}
		switch model.status {
		case .stopped:
			return model.configuration == nil ? "No configuration" : "Offline"
		case .starting:
			return "Launching runner"
		case .running(let pid):
			return "Runner active, PID \(pid)"
		case .stopping:
			return "Stopping runner"
		case .stoppedWithFailure(let code):
			return "Exit code \(code)"
		}
	}

	private var statusIcon: String {
		if !model.configurationIssues.isEmpty {
			return "exclamationmark.triangle.fill"
		}
		if case .stoppedWithFailure = model.status {
			return "xmark.circle.fill"
		}
		if model.agentStatus?.isAvailableCITarget == true {
			return "checkmark.circle.fill"
		}
		if model.status.isActive {
			return "arrow.triangle.2.circlepath"
		}
		return "circle"
	}

	private var statusColor: Color {
		if !model.configurationIssues.isEmpty {
			return .orange
		}
		if case .stoppedWithFailure = model.status {
			return .red
		}
		if model.agentStatus?.isAvailableCITarget == true {
			return .green
		}
		switch model.status {
		case .running, .stopped:
			return .secondary
		case .starting, .stopping:
			return .orange
		case .stoppedWithFailure:
			return .red
		}
	}
}

#Preview {
	StatusHeaderView()
		.environment(AppModel.previewAvailable)
}
