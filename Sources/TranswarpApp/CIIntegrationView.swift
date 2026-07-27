import SwiftUI
import TranswarpCore

struct CIIntegrationView: View {
	@Environment(AppModel.self) private var model
	@State private var mode: GitHubActionWorkflow.Mode = .direct
	@State private var selectedJobID = ""

	private var workflow: String? {
		model.githubActionWorkflow(mode: mode, jobID: selectedWorkflowJobID)
	}

	private var readiness: CIWorkflowReadiness {
		model.ciWorkflowReadiness(mode: mode, jobID: selectedWorkflowJobID)
	}

	private var detailLabel: String {
		mode == .releaseEvidence ? "Evidence" : "Job"
	}

	private var detailValue: String {
		if mode == .releaseEvidence {
			return "Named tunnel + CI dispatch"
		}
		return selectedJobIDValue.isEmpty ? "Unknown" : selectedJobIDValue
	}

	private var jobs: [BuildJob] {
		model.configuration?.jobs ?? []
	}

	private var selectedJobIDValue: String {
		if jobs.contains(where: { $0.id == selectedJobID }) {
			return selectedJobID
		}
		return jobs.first?.id ?? ""
	}

	private var selectedWorkflowJobID: String? {
		guard mode != .releaseEvidence, !selectedJobIDValue.isEmpty else {
			return nil
		}
		return selectedJobIDValue
	}

	var body: some View {
		VStack(alignment: .leading, spacing: 8) {
			Text("CI Workflows")
				.font(.headline)

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
			.frame(maxWidth: .infinity)

			if workflow != nil {
				HStack {
					LabeledContent(detailLabel) {
						detailControl
					}
					Spacer()
					Button {
						model.copyGitHubActionWorkflow(mode: mode, jobID: selectedWorkflowJobID)
					} label: {
						Label("Copy Workflow", systemImage: "doc.on.doc")
					}
				}
				CIWorkflowReadinessView(
					readiness: readiness,
					canCopySecretValues: model.canCopyGitHubActionSecretValues(mode: mode)
				) {
					model.copyGitHubActionSecretChecklist(mode: mode, jobID: selectedWorkflowJobID)
				} copySecretValues: {
					model.copyGitHubActionSecretValues(mode: mode)
				}
			} else {
				ContentUnavailableView("No CI workflow", systemImage: "bolt.horizontal")
					.frame(minHeight: 72)
			}
		}
	}

	@ViewBuilder
	private var detailControl: some View {
		if mode == .releaseEvidence || jobs.count <= 1 {
			Text(detailValue)
				.lineLimit(1)
				.truncationMode(.middle)
				.textSelection(.enabled)
		} else {
			Picker("Job", selection: Binding(
				get: { selectedJobIDValue },
				set: { selectedJobID = $0 }
			)) {
				ForEach(jobs) { job in
					Text(job.id)
						.tag(job.id)
				}
			}
			.labelsHidden()
			.frame(width: 220)
		}
	}
}

#Preview {
	CIIntegrationView()
		.environment(AppModel())
}
