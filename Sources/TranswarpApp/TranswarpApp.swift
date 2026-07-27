import AppKit
import SwiftUI

@main
struct TranswarpApp: App {
	@State private var model = AppModel()

	var body: some Scene {
		WindowGroup {
			ContentView()
				.environment(model)
				.onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
					model.stop()
				}
		}
		.windowStyle(.titleBar)

		Settings {
			SettingsView()
				.environment(model)
		}
	}
}
