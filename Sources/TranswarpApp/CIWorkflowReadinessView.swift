import SwiftUI
import TranswarpCore

struct CIWorkflowReadinessView: View {
	var readiness: CIWorkflowReadiness
	var canCopySecretValues: Bool
	var copySecretNames: () -> Void
	var copySecretValues: () -> Void

	var body: some View {
		VStack(alignment: .leading, spacing: 8) {
			HStack {
				Label(readiness.summary, systemImage: icon(for: readiness.state))
					.font(.subheadline.weight(.semibold))
					.foregroundStyle(color(for: readiness.state))
				Spacer()
				if !readiness.secretGroups.isEmpty {
					Button {
						copySecretNames()
					} label: {
						Label("Copy Names", systemImage: "list.clipboard")
					}
					Button {
						copySecretValues()
					} label: {
						Label("Copy Values", systemImage: "key")
					}
					.disabled(!canCopySecretValues)
				}
			}

			ForEach(readiness.items) { item in
				Label {
					VStack(alignment: .leading, spacing: 2) {
						Text(item.title)
							.font(.caption.weight(.semibold))
						Text(item.detail)
							.font(.caption)
							.foregroundStyle(.secondary)
							.fixedSize(horizontal: false, vertical: true)
					}
				} icon: {
					Image(systemName: icon(for: item.state))
						.foregroundStyle(color(for: item.state))
				}
			}

			if !readiness.secretGroups.isEmpty {
				Divider()
				ForEach(readiness.secretGroups) { group in
					VStack(alignment: .leading, spacing: 3) {
						Text(group.title)
							.font(.caption.weight(.semibold))
						Text(group.names.joined(separator: ", "))
							.font(.caption.monospaced())
							.foregroundStyle(.secondary)
							.textSelection(.enabled)
							.fixedSize(horizontal: false, vertical: true)
					}
				}
			}
		}
	}

	private func icon(for state: CIWorkflowReadiness.State) -> String {
		switch state {
		case .ready:
			"checkmark.circle"
		case .warning:
			"exclamationmark.triangle"
		case .blocked:
			"xmark.octagon"
		}
	}

	private func color(for state: CIWorkflowReadiness.State) -> Color {
		switch state {
		case .ready:
			.green
		case .warning:
			.orange
		case .blocked:
			.red
		}
	}
}

#Preview {
	CIWorkflowReadinessView(
		readiness: CIWorkflowReadiness(
			mode: .direct,
			configuration: .sample(machineId: "machine-123")
		),
		canCopySecretValues: true,
		copySecretNames: {},
		copySecretValues: {}
	)
	.padding()
}
