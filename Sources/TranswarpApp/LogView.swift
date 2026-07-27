import SwiftUI
import TranswarpCore

struct LogView: View {
	@Environment(AppModel.self) private var model

	var body: some View {
		VStack(alignment: .leading, spacing: 12) {
			HStack {
				Text("Runner Logs")
					.font(.headline)
				Spacer()
				Text("\(model.events.count) events")
					.font(.caption)
					.foregroundStyle(.secondary)
			}

			ZStack {
				RoundedRectangle(cornerRadius: 8)
					.fill(Color(nsColor: .textBackgroundColor))

				if model.events.isEmpty {
					ContentUnavailableView(
						"No runner logs yet",
						systemImage: "terminal",
						description: Text("Start Transwarp or run a test build to see streamed output here.")
					)
					.padding()
				} else {
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
						.onChange(of: model.events.count) {
							guard let last = model.events.last else {
								return
							}
							proxy.scrollTo(last.id, anchor: .bottom)
						}
					}
				}
			}
			.clipShape(RoundedRectangle(cornerRadius: 8))
		}
		.padding(20)
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
