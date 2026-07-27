import SwiftUI

struct ContentView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(spacing: 0) {
			StatusHeaderView()
			Divider()
			HStack(spacing: 0) {
				ScrollView {
					BuildQueueView()
						.frame(maxWidth: .infinity, alignment: .leading)
						.padding(20)
				}
				.frame(width: 420)
				.background(.regularMaterial)
				.scrollIndicators(.visible)

				Divider()

				OperationsDetailView()
					.frame(maxWidth: .infinity, maxHeight: .infinity)
			}
		}
		.frame(minWidth: 1120, minHeight: 720)
	}
}

#Preview {
	ContentView()
		.environment(AppModel())
}
