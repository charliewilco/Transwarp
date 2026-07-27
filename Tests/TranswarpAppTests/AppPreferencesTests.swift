import Foundation
import Testing
@testable import TranswarpApp

@Suite
struct AppPreferencesTests {
	@Test
	func startRunnerOnLaunchRoundTrips() {
		let suiteName = "co.charliewil.transwarp.tests.\(UUID().uuidString)"
		let defaults = UserDefaults(suiteName: suiteName)!
		defer {
			defaults.removePersistentDomain(forName: suiteName)
		}

		#expect(AppPreferencesStore.load(from: defaults).startRunnerOnLaunch == false)

		AppPreferencesStore.save(AppPreferences(startRunnerOnLaunch: true), to: defaults)

		#expect(AppPreferencesStore.load(from: defaults).startRunnerOnLaunch == true)
	}
}
