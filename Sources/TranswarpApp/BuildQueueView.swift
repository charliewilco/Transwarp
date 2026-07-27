import SwiftUI
import TranswarpCore

struct BuildQueueView: View {
	@Environment(AppModel.self) private var model

	var buildEvents: [RunnerEvent] {
		model.events.filter { $0.kind == .build || $0.kind == .registration || $0.kind == .tunnel }
	}

	var body: some View {
		VStack(alignment: .leading, spacing: 12) {
			MachineSummaryView()
			Divider()
			JobRecipeListView()
			Divider()
			CIIntegrationView()
			if !model.controllableBuilds.isEmpty {
				Divider()
				ActiveBuildControlView()
			}
			Divider()
			RecentBuildListView()
			Divider()
			Text("Activity")
				.font(.headline)

			if buildEvents.isEmpty {
				ContentUnavailableView("No activity yet", systemImage: "hammer")
					.frame(maxHeight: .infinity)
			} else {
				List(buildEvents) { event in
					VStack(alignment: .leading, spacing: 4) {
						Text(event.message)
							.lineLimit(2)
						Text(event.date, style: .time)
							.font(.caption)
							.foregroundStyle(.secondary)
					}
					.padding(.vertical, 4)
				}
				.listStyle(.inset)
			}
		}
		.padding(16)
	}
}

#Preview {
	BuildQueueView()
		.environment(AppModel())
}
