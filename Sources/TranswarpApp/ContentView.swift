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
				.frame(width: 440)
				.background(.regularMaterial)
				.scrollIndicators(.visible)

				Divider()

				LogView()
					.frame(maxWidth: .infinity, maxHeight: .infinity)
			}
		}
		.frame(minWidth: 1040, minHeight: 680)
	}
}

#Preview {
	ContentView()
		.environment(AppModel())
}
