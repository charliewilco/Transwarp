import Testing
@testable import TranswarpApp

@Suite
struct LoginItemServiceTests {
	@Test
	func statusMapsEnabledState() {
		#expect(LoginItemRegistrationStatus.enabled.state == LoginItemState(
			isEnabled: true,
			canToggle: true,
			message: "Enabled"
		))
	}

	@Test
	func statusMapsApprovalState() {
		#expect(LoginItemRegistrationStatus.requiresApproval.state == LoginItemState(
			isEnabled: false,
			canToggle: true,
			message: "Requires approval in System Settings"
		))
	}

	@Test
	func statusMapsUnavailableState() {
		#expect(LoginItemRegistrationStatus.notFound.state == LoginItemState(
			isEnabled: false,
			canToggle: false,
			message: "Unavailable for this app bundle"
		))
	}
}
