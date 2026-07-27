import Foundation
import ServiceManagement

struct LoginItemState: Equatable {
	var isEnabled: Bool
	var canToggle: Bool
	var message: String
}

enum LoginItemRegistrationStatus: Equatable {
	case enabled
	case notRegistered
	case requiresApproval
	case notFound
	case unknown

	init(status: SMAppService.Status) {
		switch status {
		case .enabled:
			self = .enabled
		case .notRegistered:
			self = .notRegistered
		case .requiresApproval:
			self = .requiresApproval
		case .notFound:
			self = .notFound
		@unknown default:
			self = .unknown
		}
	}

	var state: LoginItemState {
		switch self {
		case .enabled:
			LoginItemState(isEnabled: true, canToggle: true, message: "Enabled")
		case .notRegistered:
			LoginItemState(isEnabled: false, canToggle: true, message: "Off")
		case .requiresApproval:
			LoginItemState(isEnabled: false, canToggle: true, message: "Requires approval in System Settings")
		case .notFound:
			LoginItemState(isEnabled: false, canToggle: false, message: "Unavailable for this app bundle")
		case .unknown:
			LoginItemState(isEnabled: false, canToggle: false, message: "Unknown login item status")
		}
	}
}

enum LoginItemService {
	static func state() -> LoginItemState {
		LoginItemRegistrationStatus(status: SMAppService.mainApp.status).state
	}

	static func setEnabled(_ isEnabled: Bool) throws {
		if isEnabled {
			if SMAppService.mainApp.status != .enabled {
				try SMAppService.mainApp.register()
			}
		} else if SMAppService.mainApp.status == .enabled || SMAppService.mainApp.status == .requiresApproval {
			try SMAppService.mainApp.unregister()
		}
	}
}
