import SwiftUI
import TranswarpCore

struct BuildQueueView: View {
	@Environment(AppModel.self) private var model

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
		}
	}
}

#Preview {
	BuildQueueView()
		.environment(AppModel())
}
