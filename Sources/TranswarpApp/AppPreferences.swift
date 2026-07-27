import Foundation

struct AppPreferences: Equatable {
	var startRunnerOnLaunch: Bool

	init(startRunnerOnLaunch: Bool = false) {
		self.startRunnerOnLaunch = startRunnerOnLaunch
	}
}

enum AppPreferencesStore {
	static let startRunnerOnLaunchKey = "start_runner_on_launch"

	static func load(from defaults: UserDefaults = .standard) -> AppPreferences {
		AppPreferences(
			startRunnerOnLaunch: defaults.bool(forKey: startRunnerOnLaunchKey)
		)
	}

	static func save(_ preferences: AppPreferences, to defaults: UserDefaults = .standard) {
		defaults.set(preferences.startRunnerOnLaunch, forKey: startRunnerOnLaunchKey)
	}
}
