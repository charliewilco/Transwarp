import Foundation
import Testing
import TranswarpCore
@testable import TranswarpApp

@Suite
struct KeychainSecretStoreTests {
	@Test
	func secureStoresAdditionalRedactedValuesAsKeychainReferences() throws {
		var configuration = AgentConfiguration(
			listenAddress: "127.0.0.1:8188",
			machineId: "machine-123",
			machineName: "Mac",
			sharedToken: "runner-token",
			redactedValues: [
				"literal-signing-secret",
				"keychain://co.charliewil.transwarp/machine-123/redacted_values/existing",
				""
			],
			tunnel: TunnelConfiguration(mode: .off),
			jobs: [
				BuildJob(
					id: "build",
					label: "Build",
					workingDirectory: "/tmp",
					command: "/usr/bin/env",
					timeoutSeconds: 60
				)
			]
		)
		var stored: [String: String] = [:]

		try KeychainSecretStore.secure(&configuration) { value, reference in
			stored[reference.account] = value
		}

		#expect(configuration.redactedValues[0] == "keychain://co.charliewil.transwarp/machine-123/redacted_values/0")
		#expect(configuration.redactedValues[1] == "keychain://co.charliewil.transwarp/machine-123/redacted_values/existing")
		#expect(configuration.redactedValues[2] == "")
		#expect(stored["machine-123/redacted_values/0"] == "literal-signing-secret")
		#expect(stored["machine-123/redacted_values/existing"] == nil)
	}
}
