import SwiftUI

struct ContentView: View {
	@Environment(AppModel.self) private var model
	@State private var showsActivity = false

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

#Preview {
	ContentView()
		.environment(AppModel())
}
