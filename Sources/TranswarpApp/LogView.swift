import SwiftUI
import TranswarpCore

struct LogView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(alignment: .leading, spacing: 12) {
			Text("Runner Logs")
				.font(.headline)

			ScrollViewReader { proxy in
				ScrollView {
					LazyVStack(alignment: .leading, spacing: 6) {
						ForEach(model.events) { event in
							Text(logLine(for: event))
								.font(.system(.caption, design: .monospaced))
								.textSelection(.enabled)
								.frame(maxWidth: .infinity, alignment: .leading)
								.id(event.id)
						}
					}
					.padding(12)
				}
				.background(Color(nsColor: .textBackgroundColor))
				.clipShape(RoundedRectangle(cornerRadius: 6))
				.onChange(of: model.events.count) {
					guard let last = model.events.last else {
						return
					}
					proxy.scrollTo(last.id, anchor: .bottom)
				}
			}
		}
		.padding(16)
	}

	private func logLine(for event: RunnerEvent) -> String {
		let metadata = [
			event.jobId,
			event.buildId,
			event.sequence.map { "#\($0)" }
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
