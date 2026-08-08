import SwiftUI
import TranswarpCore

struct BuildSummaryView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		if let build = model.agentStatus?.recentBuilds.first {
			HStack(spacing: 10) {
				Image(systemName: icon(for: build))
					.foregroundStyle(color(for: build))
					.frame(width: 18)

				VStack(alignment: .leading, spacing: 2) {
					Text(build.jobId)
						.font(.subheadline.weight(.medium))
						.lineLimit(1)
					Text(summary(for: build))
						.font(.caption)
						.foregroundStyle(.secondary)
						.lineLimit(1)
				}

				Spacer()

				if model.canCancelBuild(build) {
					Button(role: .destructive) {
						Task {
							await model.cancelBuild(build)
						}
					} label: {
						Label("Cancel Build", systemImage: "xmark")
					}
					.labelStyle(.iconOnly)
					.help("Cancel \(build.jobId)")
				}
			}
		}
	}

	private func summary(for build: BuildStatus) -> String {
		if let resultSummary = build.resultSummary, !resultSummary.isEmpty {
			return resultSummary
		}
		return build.status
			.replacingOccurrences(of: "_", with: " ")
			.capitalized
	}

	private func icon(for build: BuildStatus) -> String {
		switch build.status {
		case "passed":
			return "checkmark.circle.fill"
		case "failed", "canceled":
			return "xmark.circle.fill"
		case "running":
			return "hammer.fill"
		case "queued":
			return "clock.fill"
		default:
			return "circle.fill"
		}
	}

	private func color(for build: BuildStatus) -> Color {
		switch build.status {
		case "passed":
			return .green
		case "failed", "canceled":
			return .red
		case "running":
			return .accentColor
		case "queued":
			return .orange
		default:
			return .secondary
		}
	}
}

#Preview {
	BuildSummaryView()
		.environment(AppModel())
}
