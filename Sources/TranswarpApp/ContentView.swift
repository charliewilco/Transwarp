import SwiftUI

struct ContentView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(spacing: 0) {
			StatusHeaderView()
			Divider()
			HSplitView {
				BuildQueueView()
					.frame(minWidth: 280, idealWidth: 320)
				LogView()
					.frame(minWidth: 520)
			}
		}
		.frame(minWidth: 860, minHeight: 560)
	}
}

#Preview {
	ContentView()
		.environment(AppModel())
}
