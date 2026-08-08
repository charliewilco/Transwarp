import SwiftUI

struct ContentView: View {
	@Environment(AppModel.self) private var model
	@State private var showsActivity = false

	init(showsActivity: Bool = false) {
		_showsActivity = State(initialValue: showsActivity)
	}

	var body: some View {
		VStack(alignment: .leading, spacing: 14) {
			StatusHeaderView()

			if let problem {
				Label(problem, systemImage: "exclamationmark.triangle.fill")
					.font(.caption)
					.foregroundStyle(.orange)
					.fixedSize(horizontal: false, vertical: true)
					.accessibilityLabel("Runner issue: \(problem)")
			}

			if model.agentStatus?.recentBuilds.first != nil {
				Divider()
				BuildSummaryView()
			}

			Divider()

			DisclosureGroup(isExpanded: $showsActivity) {
				LogView()
					.padding(.top, 8)
			} label: {
				HStack {
					Text("Activity")
						.font(.subheadline.weight(.medium))
					Spacer()
					Text("\(model.events.count)")
						.font(.caption.monospacedDigit())
						.foregroundStyle(.secondary)
				}
			}
		}
		.padding(16)
		.frame(width: 440)
	}

	private var problem: String? {
		model.configurationIssues.first ?? model.lastError
	}
}

#Preview("Needs Setup") {
	ContentView()
		.environment(AppModel.previewNeedsSetup)
}

#Preview("Stopped") {
	ContentView()
		.environment(AppModel.previewStopped)
}

#Preview("Available") {
	ContentView()
		.environment(AppModel.previewAvailable)
}

#Preview("Paused") {
	ContentView()
		.environment(AppModel.previewPaused)
}

#Preview("Queued") {
	ContentView()
		.environment(AppModel.previewQueued)
}

#Preview("Running") {
	ContentView()
		.environment(AppModel.previewRunning)
}

#Preview("Passed") {
	ContentView()
		.environment(AppModel.previewPassed)
}

#Preview("Failed") {
	ContentView()
		.environment(AppModel.previewFailed)
}

#Preview("Error") {
	ContentView()
		.environment(AppModel.previewError)
}

#Preview("Expanded Activity") {
	ContentView(showsActivity: true)
		.environment(AppModel.previewExpandedActivity)
}
