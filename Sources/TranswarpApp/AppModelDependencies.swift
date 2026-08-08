import AppKit
import Foundation
import TranswarpCore

@MainActor
protocol RunnerControlling: AnyObject {
	var onEvent: ((RunnerEvent) -> Void)? { get set }
	var onStatusChange: ((RunnerProcess.Status) -> Void)? { get set }

	func start(configurationPath: URL) throws
	func stop()
}

@MainActor
struct AppModelDependencies {
	var makeRunnerProcess: @MainActor () -> RunnerControlling
	var preferencesDefaults: UserDefaults
	var environment: [String: String]
	var ensureConfigurationFile: @MainActor () throws -> URL
	var loadConfiguration: @MainActor (URL) throws -> AgentConfiguration
	var saveConfiguration: @MainActor (AgentConfiguration, URL) throws -> Void
	var secureConfiguration: @MainActor (inout AgentConfiguration) throws -> Void
	var resolveSecret: @MainActor (String) throws -> String
	var secretIssues: @MainActor (AgentConfiguration) -> [String]
	var loginItemState: @MainActor () -> LoginItemState
	var setLoginItemEnabled: @MainActor (Bool) throws -> Void
	var revealConfiguration: @MainActor (URL) -> Void
	var openConfiguration: @MainActor (URL) -> Void
	var copyToPasteboard: @MainActor (String) -> Void
	var dataForRequest: @MainActor (URLRequest) async throws -> (Data, URLResponse)

	static let live = AppModelDependencies(
		makeRunnerProcess: { RunnerProcess() },
		preferencesDefaults: .standard,
		environment: ProcessInfo.processInfo.environment,
		ensureConfigurationFile: AppModel.ensureSecuredDefaultConfigurationFile,
		loadConfiguration: AgentConfigurationStore.load(from:),
		saveConfiguration: AgentConfigurationStore.save(_:to:),
		secureConfiguration: KeychainSecretStore.secure(_:),
		resolveSecret: KeychainSecretStore.resolve(_:),
		secretIssues: KeychainSecretStore.issues(for:),
		loginItemState: LoginItemService.state,
		setLoginItemEnabled: LoginItemService.setEnabled(_:),
		revealConfiguration: { NSWorkspace.shared.activateFileViewerSelecting([$0]) },
		openConfiguration: { NSWorkspace.shared.open($0) },
		copyToPasteboard: {
			NSPasteboard.general.clearContents()
			NSPasteboard.general.setString($0, forType: .string)
		},
		dataForRequest: NoRedirectURLSession.data(for:)
	)

	static func fixture(configuration: AgentConfiguration = .previewReady) -> AppModelDependencies {
		let path = URL(filePath: NSTemporaryDirectory())
			.appending(path: "TranswarpPreview-\(UUID().uuidString)")
			.appending(path: "agent.json")
		var savedConfiguration = configuration

		return AppModelDependencies(
			makeRunnerProcess: { PreviewRunnerProcess() },
			preferencesDefaults: UserDefaults(suiteName: "co.charliewil.transwarp.preview.\(UUID().uuidString)") ?? .standard,
			environment: [:],
			ensureConfigurationFile: { path },
			loadConfiguration: { _ in savedConfiguration },
			saveConfiguration: { configuration, _ in savedConfiguration = configuration },
			secureConfiguration: { _ in },
			resolveSecret: { value in
				SecretReference.isReference(value) ? "preview-secret" : value
			},
			secretIssues: { _ in [] },
			loginItemState: { LoginItemState(isEnabled: false, canToggle: true, message: "Off") },
			setLoginItemEnabled: { _ in },
			revealConfiguration: { _ in },
			openConfiguration: { _ in },
			copyToPasteboard: { _ in },
			dataForRequest: { _ in throw AppModelError("Preview networking is disabled") }
		)
	}
}

@MainActor
private final class PreviewRunnerProcess: RunnerControlling {
	var onEvent: ((RunnerEvent) -> Void)?
	var onStatusChange: ((RunnerProcess.Status) -> Void)?

	func start(configurationPath: URL) {
		onStatusChange?(.running(pid: 12345))
		onEvent?(.init(kind: .info, message: "Preview runner started"))
	}

	func stop() {
		onStatusChange?(.stopped)
		onEvent?(.init(kind: .info, message: "Preview runner stopped"))
	}
}
