import SwiftUI
import TranswarpCore

struct ActiveBuildControlView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		if !model.controllableBuilds.isEmpty {
			VStack(alignment: .leading, spacing: 8) {
				Text("Open Builds")
					.font(.headline)
				ForEach(model.controllableBuilds) { build in
					HStack(alignment: .firstTextBaseline) {
						VStack(alignment: .leading, spacing: 3) {
							HStack(spacing: 6) {
								Text(build.jobId)
									.font(.subheadline.weight(.semibold))
								Text(build.status.capitalized)
									.font(.caption.weight(.medium))
									.foregroundStyle(build.isRunning ? .green : .secondary)
							}
							Text(build.buildId)
								.font(.caption.monospaced())
								.foregroundStyle(.secondary)
								.lineLimit(1)
								.truncationMode(.middle)
								.textSelection(.enabled)
							if let requestId = build.requestId, !requestId.isEmpty {
								Text(requestId)
									.font(.caption)
									.foregroundStyle(.secondary)
									.lineLimit(1)
									.truncationMode(.middle)
									.textSelection(.enabled)
							}
						}
						Spacer()
						Button(role: .destructive) {
							Task {
								await model.cancelBuild(build)
							}
						} label: {
							Label("Cancel", systemImage: "xmark.circle")
						}
						.disabled(!model.canCancelBuild(build))
						.help(build.isQueued ? "Cancel queued build" : "Cancel active build")
					}
				}
			}
		}
	}
}

#Preview {
	ActiveBuildControlView()
		.environment(AppModel())
}
