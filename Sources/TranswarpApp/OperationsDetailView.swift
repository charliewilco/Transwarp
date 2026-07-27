import SwiftUI

struct OperationsDetailView: View {
	var body: some View {
		VStack(alignment: .leading, spacing: 18) {
			RunnerOverviewView()

			LogView()
				.frame(maxWidth: .infinity, maxHeight: .infinity)
		}
		.padding(24)
	}
}

#Preview {
	OperationsDetailView()
		.environment(AppModel())
}
