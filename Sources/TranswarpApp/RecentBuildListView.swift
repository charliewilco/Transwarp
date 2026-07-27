import SwiftUI
import TranswarpCore

struct RecentBuildListView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(alignment: .leading, spacing: 8) {
			Text("Recent Builds")
				.font(.headline)

			if let builds = model.agentStatus?.recentBuilds, !builds.isEmpty {
				ForEach(builds) { build in
					VStack(alignment: .leading, spacing: 3) {
						HStack {
							Text(build.jobId)
								.font(.subheadline.weight(.semibold))
								.lineLimit(1)
							Spacer()
							Text(build.status.capitalized)
								.font(.caption.weight(.medium))
								.foregroundStyle(color(for: build.status))
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
						if let sourceSummary = build.result?.sourceSummary {
							Text(sourceSummary)
								.font(.caption)
								.foregroundStyle(.secondary)
								.lineLimit(1)
								.truncationMode(.middle)
								.textSelection(.enabled)
						}
						if let resultSummary = build.resultSummary {
							Text(resultSummary)
								.font(.caption)
								.foregroundStyle(.secondary)
								.lineLimit(2)
								.textSelection(.enabled)
						}
						if let reportStatus = build.reportStatus, !reportStatus.isEmpty {
							Label(reportLabel(for: reportStatus), systemImage: reportIcon(for: reportStatus))
								.font(.caption)
								.foregroundStyle(reportColor(for: reportStatus))
						}
						if let reportError = build.reportError, !reportError.isEmpty {
							Text(reportError)
								.font(.caption)
								.foregroundStyle(.orange)
								.lineLimit(2)
								.textSelection(.enabled)
						}
					}
					.padding(.vertical, 4)
				}
			} else {
				ContentUnavailableView("No builds yet", systemImage: "clock")
					.frame(minHeight: 96)
			}
		}
	}

	private func color(for status: String) -> Color {
		switch status {
		case "passed":
			.green
		case "failed", "canceled":
			.red
		default:
			.secondary
		}
	}

	private func reportLabel(for status: String) -> String {
		switch status {
		case "reported":
			"Reported"
		case "failed":
			"Report failed"
		case "pending":
			"Report pending"
		default:
			status
				.replacingOccurrences(of: "_", with: " ")
				.capitalized
		}
	}

	private func reportIcon(for status: String) -> String {
		switch status {
		case "reported":
			"checkmark.circle"
		case "failed":
			"exclamationmark.triangle"
		default:
			"arrow.triangle.2.circlepath"
		}
	}

	private func reportColor(for status: String) -> Color {
		switch status {
		case "reported":
			.green
		case "failed":
			.orange
		default:
			.secondary
		}
	}
}

#Preview {
	RecentBuildListView()
		.environment(AppModel())
}
