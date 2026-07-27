import Foundation

public enum AgentConfigurationStore {
	public static func defaultPath() throws -> URL {
		if let override = ProcessInfo.processInfo.environment["TRANSWARP_CONFIG_PATH"], !override.isEmpty {
			return URL(fileURLWithPath: override)
		}

		let base = try FileManager.default.url(
			for: .applicationSupportDirectory,
			in: .userDomainMask,
			appropriateFor: nil,
			create: true
		)
		let directory = base.appending(path: "Transwarp", directoryHint: .isDirectory)
		try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
		return directory.appending(path: "agent.json")
	}

	public static func load(from url: URL) throws -> AgentConfiguration {
		let data = try Data(contentsOf: url)
		return try JSONDecoder.transwarp.decode(AgentConfiguration.self, from: data)
	}

	public static func encode(_ configuration: AgentConfiguration) throws -> Data {
		try JSONEncoder.transwarp.encode(configuration)
	}

	public static func save(_ configuration: AgentConfiguration, to url: URL) throws {
		let data = try encode(configuration)
		try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
		try data.write(to: url, options: [.atomic])
	}

	public static func ensureDefaultFile() throws -> URL {
		let url = try defaultPath()
		if !FileManager.default.fileExists(atPath: url.path) {
			try save(.starter(), to: url)
		}
		return url
	}
}

private extension JSONDecoder {
	static var transwarp: JSONDecoder {
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase
		return decoder
	}
}

private extension JSONEncoder {
	static var transwarp: JSONEncoder {
		let encoder = JSONEncoder()
		encoder.keyEncodingStrategy = .convertToSnakeCase
		encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
		return encoder
	}
}
