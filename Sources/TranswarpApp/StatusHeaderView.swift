import SwiftUI

struct StatusHeaderView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		HStack(spacing: 16) {
			VStack(alignment: .leading, spacing: 4) {
				Text("Transwarp")
					.font(.title2.weight(.semibold))
				Text(model.status.label)
					.foregroundStyle(.secondary)
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
		.padding(20)
	}
}

#Preview {
	StatusHeaderView()
		.environment(AppModel())
}
