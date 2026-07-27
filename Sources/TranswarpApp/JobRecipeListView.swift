import SwiftUI

struct JobRecipeListView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(alignment: .leading, spacing: 8) {
			Text("Allowed Jobs")
				.font(.headline)

			if let jobs = model.configuration?.jobs, !jobs.isEmpty {
				ForEach(jobs) { job in
					VStack(alignment: .leading, spacing: 3) {
						Text(job.label)
							.font(.subheadline.weight(.semibold))
							.lineLimit(1)
						Text(job.id)
							.font(.caption)
							.foregroundStyle(.secondary)
							.textSelection(.enabled)
						Text(job.command)
							.font(.caption.monospaced())
							.foregroundStyle(.secondary)
							.lineLimit(1)
							.truncationMode(.middle)
							.textSelection(.enabled)
						if job.checkout {
							Label("Checkout required", systemImage: "arrow.down.doc")
								.font(.caption)
								.foregroundStyle(.secondary)
						}
					}
					.padding(.vertical, 4)
				}
			} else {
				ContentUnavailableView("No jobs configured", systemImage: "hammer")
					.frame(minHeight: 96)
			}
		}
	}
}

#Preview {
	JobRecipeListView()
		.environment(AppModel())
}
