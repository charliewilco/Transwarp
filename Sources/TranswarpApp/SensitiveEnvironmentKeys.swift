import Foundation

enum SensitiveEnvironmentKeys {
	private static let substringMarkers = [
		"TOKEN",
		"SECRET",
		"PASSWORD",
		"PRIVATE_KEY",
		"API_KEY",
		"ACCESS_KEY",
		"SECRET_KEY"
	]

	private static let segmentMarkers: Set<String> = [
		"AUTH",
		"AUTHORIZATION",
		"CREDENTIAL",
		"CREDENTIALS",
		"KEYCHAIN",
		"CERT",
		"CERTIFICATE",
		"P12",
		"PKCS12",
		"PROFILE",
		"IDENTITY",
		"NOTARY",
		"NOTARIZATION",
		"PASSPHRASE",
		"PASSCODE"
	]

	static func contains(_ key: String) -> Bool {
		let upper = key.uppercased()
		if substringMarkers.contains(where: upper.contains) {
			return true
		}

		return segments(in: upper).contains { segmentMarkers.contains($0) }
	}

	private static func segments(in key: String) -> [String] {
		key.split { character in
			!character.isLetter && !character.isNumber
		}.map(String.init)
	}
}
