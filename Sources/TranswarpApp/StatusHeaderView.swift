import SwiftUI

struct StatusHeaderView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		HStack(spacing: 14) {
			VStack(alignment: .leading, spacing: 3) {
				Text("Transwarp")
					.font(.title3.weight(.semibold))
				Label(model.status.label, systemImage: statusIcon)
					.font(.subheadline)
					.foregroundStyle(statusColor)
			}

			Spacer()

			SettingsLink {
				Label("Settings", systemImage: "slider.horizontal.3")
			}

			Button {
				model.revealConfiguration()
			} label: {
				Label("Reveal", systemImage: "folder")
			}

			Button {
				Task {
					await model.runTestBuild()
				}
			} label: {
				Label(model.testBuildInFlight ? "Testing" : "Run Test Build", systemImage: "hammer")
			}
			.disabled(!model.canRunTestBuild)
			.help(model.testBuildHelp)

			Button {
				model.canStop ? model.stop() : model.start()
			} label: {
				Label(model.canStop ? "Stop" : "Start", systemImage: model.canStop ? "stop.fill" : "play.fill")
			}
			.keyboardShortcut(.return, modifiers: [.command])
			.buttonStyle(.borderedProminent)
			.disabled(!model.canStop && !model.canStart)
			.help(model.canStart || model.canStop ? "" : model.configurationIssues.first ?? "Configuration is not ready")
		}
		.padding(.horizontal, 24)
		.padding(.vertical, 16)
	}

	private var statusIcon: String {
		if model.status.isRunning {
			return "checkmark.circle.fill"
		}
		if model.status.isActive {
			return "arrow.triangle.2.circlepath"
		}
		return "circle"
	}

	private var statusColor: Color {
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
}

#Preview {
	StatusHeaderView()
		.environment(AppModel())
}
