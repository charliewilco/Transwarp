import Foundation
import Security
import TranswarpCore

enum KeychainSecretStore {
	static let service = SecretReference.defaultService

	static func secure(_ configuration: inout AgentConfiguration) throws {
		try secure(&configuration, store: write)
	}

	static func secure(
		_ configuration: inout AgentConfiguration,
		store: (String, SecretReference) throws -> Void
	) throws {
		let machineID = configuration.machineId
		configuration.sharedToken = try storeIfNeeded(configuration.sharedToken, account: "\(machineID)/shared_token", store: store)
		configuration.registrationToken = try storeIfNeeded(configuration.registrationToken, account: "\(machineID)/registration_token", store: store)
		configuration.ciAccessClientSecret = try storeIfNeeded(configuration.ciAccessClientSecret, account: "\(machineID)/ci_access_client_secret", store: store)
		configuration.runnerAccessClientSecret = try storeIfNeeded(configuration.runnerAccessClientSecret, account: "\(machineID)/runner_access_client_secret", store: store)
		configuration.tunnel.token = try storeIfNeeded(configuration.tunnel.token, account: "\(machineID)/cloudflare_tunnel_token", store: store)

		for index in configuration.redactedValues.indices {
			configuration.redactedValues[index] = try storeIfNeeded(
				configuration.redactedValues[index],
				account: "\(machineID)/redacted_values/\(index)",
				store: store
			)
		}

		for jobIndex in configuration.jobs.indices {
			let jobID = configuration.jobs[jobIndex].id
			configuration.jobs[jobIndex].checkoutAuthorizationHeader = try storeIfNeeded(
				configuration.jobs[jobIndex].checkoutAuthorizationHeader,
				account: "\(machineID)/jobs/\(jobID)/checkout_authorization_header",
				store: store
			)
			let configuredSecretKeys = Set(configuration.jobs[jobIndex].redactedEnvironmentKeys)
			let sensitiveKeys = configuration.jobs[jobIndex].environment.keys.filter(isSensitiveEnvironmentKey)
			let secretKeys = configuredSecretKeys.union(sensitiveKeys)
			for key in secretKeys {
				guard let value = configuration.jobs[jobIndex].environment[key], !value.isEmpty else {
					continue
				}
				configuration.jobs[jobIndex].environment[key] = try storeIfNeeded(
					value,
					account: "\(machineID)/jobs/\(jobID)/environment/\(key)",
					store: store
				)
			}
		}
	}

	static func resolve(_ value: String) throws -> String {
		guard let reference = SecretReference(value) else {
			return value
		}
		guard reference.service == service else {
			throw KeychainSecretStoreError.unsupportedService(reference.service)
		}
		return try read(reference)
	}

	static func issues(for configuration: AgentConfiguration) -> [String] {
		var issues: [String] = []
		check(configuration.sharedToken, label: "Runner token", issues: &issues)
		check(configuration.registrationToken, label: "Registration token", issues: &issues)
		check(configuration.ciAccessClientSecret, label: "CI Access client secret", issues: &issues)
		check(configuration.runnerAccessClientSecret, label: "Runner Access client secret", issues: &issues)
		check(configuration.tunnel.token, label: "Cloudflare tunnel token", issues: &issues)
		for (index, value) in configuration.redactedValues.enumerated() {
			check(value, label: "Additional redacted value \(index + 1)", issues: &issues)
		}
		for job in configuration.jobs {
			check(job.checkoutAuthorizationHeader, label: "Job \(job.id) checkout authorization header", issues: &issues)
			for (key, value) in job.environment {
				check(value, label: "Job \(job.id) environment \(key)", issues: &issues)
			}
		}
		return issues
	}

	private static func storeIfNeeded(
		_ value: String,
		account: String,
		store: (String, SecretReference) throws -> Void
	) throws -> String {
		if value.isEmpty || SecretReference.isReference(value) {
			return value
		}

		let reference = SecretReference(service: service, account: account)
		try store(value, reference)
		return reference.rawValue
	}

	private static func write(_ value: String, reference: SecretReference) throws {
		let data = Data(value.utf8)
		let query: [String: Any] = [
			kSecClass as String: kSecClassGenericPassword,
			kSecAttrService as String: reference.service,
			kSecAttrAccount as String: reference.account,
			kSecUseDataProtectionKeychain as String: false
		]
		let attributes: [String: Any] = [
			kSecValueData as String: data,
			kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
		]

		let updateStatus = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
		if updateStatus == errSecSuccess {
			return
		}
		if updateStatus != errSecItemNotFound {
			throw KeychainSecretStoreError.status(updateStatus)
		}

		var addQuery = query
		for (key, value) in attributes {
			addQuery[key] = value
		}
		let addStatus = SecItemAdd(addQuery as CFDictionary, nil)
		if addStatus != errSecSuccess {
			throw KeychainSecretStoreError.status(addStatus)
		}
	}

	private static func read(_ reference: SecretReference) throws -> String {
		let query: [String: Any] = [
			kSecClass as String: kSecClassGenericPassword,
			kSecAttrService as String: reference.service,
			kSecAttrAccount as String: reference.account,
			kSecUseDataProtectionKeychain as String: false,
			kSecReturnData as String: true,
			kSecMatchLimit as String: kSecMatchLimitOne
		]

		var result: CFTypeRef?
		let status = SecItemCopyMatching(query as CFDictionary, &result)
		guard status == errSecSuccess, let data = result as? Data, let value = String(data: data, encoding: .utf8) else {
			throw KeychainSecretStoreError.status(status)
		}
		return value
	}

	private static func check(_ value: String, label: String, issues: inout [String]) {
		guard SecretReference.isReference(value) else {
			return
		}
		do {
			_ = try resolve(value)
		} catch {
			issues.append("\(label) Keychain reference is unavailable: \(error.localizedDescription)")
		}
	}

	private static func isSensitiveEnvironmentKey(_ key: String) -> Bool {
		SensitiveEnvironmentKeys.contains(key)
	}
}

enum KeychainSecretStoreError: LocalizedError {
	case status(OSStatus)
	case unsupportedService(String)

	var errorDescription: String? {
		switch self {
		case let .status(status):
			"Keychain operation failed with status \(status)."
		case let .unsupportedService(service):
			"Unsupported Keychain service \(service)."
		}
	}
}
