import Foundation

public struct SecretReference: Equatable, Sendable {
	public static let scheme = "keychain"
	public static let defaultService = "co.charliewil.transwarp"

	public var service: String
	public var account: String

	public init(service: String, account: String) {
		self.service = service
		self.account = account
	}

	public init?(_ value: String) {
		guard let url = URL(string: value),
			  url.scheme == Self.scheme,
			  let service = url.host,
			  !service.isEmpty,
			  url.user == nil,
			  url.password == nil,
			  url.port == nil,
			  url.query == nil,
			  url.fragment == nil else {
			return nil
		}

		guard url.path.hasPrefix("/") else {
			return nil
		}
		let account = String(url.path.dropFirst())
		guard !account.isEmpty else {
			return nil
		}

		self.service = service
		self.account = account
	}

	public var rawValue: String {
		let account = account
			.split(separator: "/", omittingEmptySubsequences: false)
			.map { String($0).addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? String($0) }
			.joined(separator: "/")
		return "\(Self.scheme)://\(service)/\(account)"
	}

	public static func isReference(_ value: String) -> Bool {
		SecretReference(value) != nil
	}
}
