import SwiftUI
import TranswarpCore

struct CIIntegrationView: View {
	@Environment(AppModel.self) private var model
	@State private var mode: GitHubActionWorkflow.Mode = .direct

	private var workflow: String? {
		model.githubActionWorkflow(mode: mode)
	}

	private var readiness: CIWorkflowReadiness {
		model.ciWorkflowReadiness(mode: mode)
	}

	private var detailLabel: String {
		mode == .releaseEvidence ? "Evidence" : "Job"
	}

	private var detailValue: String {
		if mode == .releaseEvidence {
			return "Named tunnel + CI dispatch"
		}
		return model.configuration?.jobs.first?.id ?? "Unknown"
	}

	var body: some View {
		VStack(alignment: .leading, spacing: 8) {
			HStack {
				Text("CI Workflows")
					.font(.headline)
				Spacer()
				Picker("Mode", selection: $mode) {
					Text("Self-Hosted")
						.tag(GitHubActionWorkflow.Mode.selfHosted)
					Text("Direct")
						.tag(GitHubActionWorkflow.Mode.direct)
					Text("Coordinator")
						.tag(GitHubActionWorkflow.Mode.coordinator)
					Text("Release")
						.tag(GitHubActionWorkflow.Mode.releaseEvidence)
				}
				.labelsHidden()
				.pickerStyle(.segmented)
				.frame(width: 376)
			}

			if workflow != nil {
				HStack {
					LabeledContent(detailLabel, value: detailValue)
					Spacer()
					Button {
						model.copyGitHubActionWorkflow(mode: mode)
					} label: {
						Label("Copy Workflow", systemImage: "doc.on.doc")
					}
				}
				CIWorkflowReadinessView(
					readiness: readiness,
					canCopySecretValues: model.canCopyGitHubActionSecretValues(mode: mode)
				) {
					model.copyGitHubActionSecretChecklist(mode: mode)
				} copySecretValues: {
					model.copyGitHubActionSecretValues(mode: mode)
				}
			} else {
				ContentUnavailableView("No CI workflow", systemImage: "bolt.horizontal")
					.frame(minHeight: 72)
			}
		}
	}
}

#Preview {
	CIIntegrationView()
		.environment(AppModel())
}
