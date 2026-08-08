import SwiftUI
import TranswarpCore

struct CIWorkflowSettingsView: View {
	@Environment(AppModel.self) private var model
	@State private var mode: GitHubActionWorkflow.Mode = .direct
	@State private var selectedJobID = ""

	var body: some View {
		Section("CI Workflows") {
			Picker("Workflow", selection: $mode) {
				ForEach(GitHubActionWorkflow.Mode.allCases, id: \.self) { mode in
					Text(title(for: mode))
						.tag(mode)
				}
			}

			if mode != .releaseEvidence {
				Picker("Job", selection: $selectedJobID) {
					Text("First configured job")
						.tag("")
					ForEach(model.configuration?.jobs ?? []) { job in
						Text(job.id)
							.tag(job.id)
					}
				}
			}

			let readiness = model.ciWorkflowReadiness(mode: mode, jobID: selectedWorkflowJobID)
			LabeledContent("Readiness") {
				Label(readiness.summary, systemImage: icon(for: readiness.state))
					.foregroundStyle(color(for: readiness.state))
			}

			ForEach(readiness.items) { item in
				LabeledContent {
					Text(item.detail)
						.foregroundStyle(.secondary)
						.fixedSize(horizontal: false, vertical: true)
				} label: {
					Label(item.title, systemImage: icon(for: item.state))
						.foregroundStyle(color(for: item.state))
				}
			}

			HStack {
				Button {
					model.copyGitHubActionWorkflow(mode: mode, jobID: selectedWorkflowJobID)
				} label: {
					Label("Copy Workflow", systemImage: "doc.on.doc")
				}
				.disabled(model.githubActionWorkflow(mode: mode, jobID: selectedWorkflowJobID) == nil)

				Button {
					model.copyGitHubActionSecretChecklist(mode: mode, jobID: selectedWorkflowJobID)
				} label: {
					Label("Copy Secret Names", systemImage: "list.bullet.clipboard")
				}
				.disabled(readiness.secretChecklistText.isEmpty)

				Button {
					model.copyGitHubActionSecretValues(mode: mode)
				} label: {
					Label("Copy Safe Values", systemImage: "key")
				}
				.disabled(!model.canCopyGitHubActionSecretValues(mode: mode))
				.help("Copies only local values Transwarp can safely export for this workflow mode")
			}
		}
		.onChange(of: mode) { _, newMode in
			if newMode == .releaseEvidence {
				selectedJobID = ""
			}
		}
	}

	private var selectedWorkflowJobID: String? {
		let trimmed = selectedJobID.trimmingCharacters(in: .whitespacesAndNewlines)
		return trimmed.isEmpty ? nil : trimmed
	}

	private func title(for mode: GitHubActionWorkflow.Mode) -> String {
		switch mode {
		case .selfHosted:
			return "Self-hosted"
		case .direct:
			return "Direct"
		case .coordinator:
			return "Coordinator"
		case .releaseEvidence:
			return "Release evidence"
		}
	}

	private func icon(for state: CIWorkflowReadiness.State) -> String {
		switch state {
		case .ready:
			return "checkmark.circle.fill"
		case .warning:
			return "exclamationmark.triangle.fill"
		case .blocked:
			return "xmark.circle.fill"
		}
	}

	private func color(for state: CIWorkflowReadiness.State) -> Color {
		switch state {
		case .ready:
			return .green
		case .warning:
			return .orange
		case .blocked:
			return .red
		}
	}
}

#Preview {
	Form {
		CIWorkflowSettingsView()
	}
	.formStyle(.grouped)
	.padding()
	.environment(AppModel.previewAvailable)
}
