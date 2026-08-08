import AppKit
import SwiftUI

@main
struct TranswarpApp: App {
	@State private var model = AppModel()

	var body: some Scene {
		Window("Transwarp", id: "main") {
			ContentView()
				.environment(model)
				.onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
					model.stop()
				}
		}
		.windowStyle(.titleBar)
		.windowResizability(.contentSize)
		.defaultSize(width: 440, height: 240)

		Settings {
			SettingsView()
				.environment(model)
		}
	}
}
