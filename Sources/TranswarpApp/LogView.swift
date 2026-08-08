import SwiftUI
import TranswarpCore

struct LogView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		Group {
			if model.events.isEmpty {
				Text("No activity yet")
					.font(.caption)
					.foregroundStyle(.secondary)
					.frame(maxWidth: .infinity, alignment: .leading)
			} else {
				ScrollViewReader { proxy in
					ScrollView {
						LazyVStack(alignment: .leading, spacing: 5) {
							ForEach(model.events) { event in
								Text(logLine(for: event))
									.font(.system(.caption, design: .monospaced))
									.textSelection(.enabled)
									.frame(maxWidth: .infinity, alignment: .leading)
									.id(event.id)
							}
						}
						.padding(10)
					}
					.background(Color(nsColor: .textBackgroundColor))
					.clipShape(RoundedRectangle(cornerRadius: 6))
					.frame(height: 160)
					.onChange(of: model.events.count) {
						guard let last = model.events.last else {
							return
						}
						proxy.scrollTo(last.id, anchor: .bottom)
					}
				}
			}
		}
	}

	private func logLine(for event: RunnerEvent) -> String {
		let metadata = [
			event.jobId,
			event.buildId,
			event.sequence.map { "#\($0)" },
		]
		.compactMap { $0 }
		.joined(separator: " ")
		if metadata.isEmpty {
			return "[ \(event.kind.rawValue) ] \(event.message)"
		}
		return "[ \(event.kind.rawValue) \(metadata) ] \(event.message)"
	}
}

#Preview {
	LogView()
		.environment(AppModel())
}
