import Foundation
import TranswarpCore

struct CIWorkflowReadiness: Equatable {
	enum State: Equatable {
		case ready
		case warning
		case blocked
	}

	struct Item: Equatable, Identifiable {
		var id: String { title }
		var state: State
		var title: String
		var detail: String
	}

	struct SecretGroup: Equatable, Identifiable {
		var id: String { title }
		var title: String
		var names: [String]
	}

	var mode: GitHubActionWorkflow.Mode
	var items: [Item]
	var secretGroups: [SecretGroup]

	var state: State {
		if items.contains(where: { $0.state == .blocked }) {
			return .blocked
		}
		if items.contains(where: { $0.state == .warning }) {
			return .warning
		}
		return .ready
	}

	var summary: String {
		switch state {
		case .ready:
			"Ready to copy"
		case .warning:
			"Needs external proof"
		case .blocked:
			"Configuration needed"
		}
	}

	var secretChecklistText: String {
		secretGroups
			.map { group in
				([group.title] + group.names.map { "- \($0)" }).joined(separator: "\n")
			}
			.joined(separator: "\n\n")
	}

	init(
		mode: GitHubActionWorkflow.Mode,
		configuration: AgentConfiguration?,
		configurationIssues: [String] = [],
		jobID: String? = nil
	) {
		self.mode = mode
		items = Self.items(
			for: mode,
			configuration: configuration,
			configurationIssues: configurationIssues,
			jobID: jobID
		)
		secretGroups = Self.secretGroups(for: mode, configuration: configuration)
	}

	private static func items(
		for mode: GitHubActionWorkflow.Mode,
		configuration: AgentConfiguration?,
		configurationIssues: [String],
		jobID: String?
	) -> [Item] {
		var items: [Item] = []

		if let configurationIssue = configurationIssues.first {
			items.append(.init(
				state: .blocked,
				title: "Preflight",
				detail: configurationIssue
			))
		} else {
			items.append(.init(
				state: .ready,
				title: "Preflight",
				detail: "Saved configuration passes local validation"
			))
		}

		switch mode {
		case .selfHosted:
			items.append(jobItem(configuration, jobID: jobID))
			items.append(.init(
				state: .warning,
				title: "GitHub runner",
				detail: "Register this Mac with labels self-hosted, macOS, ARM64, transwarp-desktop"
			))
			items.append(.init(
				state: .warning,
				title: "Local proof",
				detail: "Workflow records hardware, Xcode, and signing readiness evidence before the build"
			))
		case .direct:
			items.append(jobItem(configuration, jobID: jobID))
			items.append(runnerTokenItem(configuration))
			items.append(namedTunnelItem(configuration))
			items.append(.init(
				state: .warning,
				title: "GitHub secrets",
				detail: "Add the direct dispatch secrets to the repository before running the copied workflow"
			))
		case .coordinator:
			items.append(jobItem(configuration, jobID: jobID))
			items.append(coordinatorRunnerTokenItem(configuration))
			items.append(namedTunnelItem(configuration))
			items.append(registrationItem(configuration))
			items.append(.init(
				state: .warning,
				title: "Coordinator",
				detail: "GitHub uses the CI/operator token; this Mac's registration token must match the coordinator target token and stay out of GitHub"
			))
		case .releaseEvidence:
			items.append(.init(
				state: .warning,
				title: "Self-hosted runner",
				detail: "Run on this Apple Silicon Mac with labels self-hosted, macOS, ARM64, transwarp-desktop"
			))
			items.append(releaseNamedTunnelItem(configuration))
			items.append(.init(
				state: .warning,
				title: "Receipt reuse",
				detail: "Leave collect-named-tunnel enabled unless named-tunnel-evidence and ci-dispatch-evidence both point at existing receipts"
			))
			items.append(.init(
				state: .warning,
				title: "Distribution proof",
				detail: "Developer ID signing, notarization, Gatekeeper, and clean-Mac validation are still external gates"
			))
			items.append(.init(
				state: .warning,
				title: "Clean-Mac receipt",
				detail: "Pass a clean-mac-evidence workflow input after validating the notarized archive on a separate Mac"
			))
		}

		return items
	}

	private static func releaseNamedTunnelItem(_ configuration: AgentConfiguration?) -> Item {
		guard let configuration else {
			return .init(
				state: .warning,
				title: "Named tunnel",
				detail: "Add TRANSWARP_PUBLIC_URL and TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN in GitHub secrets before collecting live evidence"
			)
		}
		guard configuration.tunnel.mode == .named,
			  configuration.tunnel.publicURL != nil else {
			return .init(
				state: .warning,
				title: "Named tunnel",
				detail: "Save a named tunnel public URL to copy TRANSWARP_PUBLIC_URL from the app; otherwise enter it manually in GitHub"
			)
		}
		return .init(
			state: .ready,
			title: "Named tunnel",
			detail: "Saved public URL can be copied to TRANSWARP_PUBLIC_URL; tunnel token still stays in GitHub"
		)
	}

	private static func secretGroups(
		for mode: GitHubActionWorkflow.Mode,
		configuration: AgentConfiguration?
	) -> [SecretGroup] {
		switch mode {
		case .selfHosted:
			return []
		case .direct:
			return [
				.init(title: "Required GitHub secrets", names: [
					"TRANSWARP_URL",
					"TRANSWARP_TOKEN"
				]),
				accessSecretGroup(title: "If Cloudflare Access protects the runner hostname")
			].compactMap { $0 }
		case .coordinator:
			return [
				.init(title: "Required GitHub secrets", names: [
					"TRANSWARP_COORDINATOR_URL",
					"TRANSWARP_COORDINATOR_TOKEN"
				]),
				accessSecretGroup(title: "If Cloudflare Access protects the coordinator hostname")
			].compactMap { $0 }
		case .releaseEvidence:
			return [
				.init(title: "Required GitHub secrets", names: [
					"TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN",
					"TRANSWARP_PUBLIC_URL",
					"TRANSWARP_EXPECTED_CLOUDFLARED_VERSION"
				]),
				.init(title: "Signing and notarization secrets", names: [
					"TRANSWARP_SIGN_IDENTITY",
					"TRANSWARP_NOTARIZE",
					"APPLE_KEYCHAIN_PROFILE"
				]),
				.init(title: "Optional GitHub secrets", names: [
					"TRANSWARP_ACCESS_CLIENT_ID",
					"TRANSWARP_ACCESS_CLIENT_SECRET"
				])
			]
		}
	}

	private static func accessSecretGroup(title: String) -> SecretGroup {
		.init(title: title, names: [
			"TRANSWARP_ACCESS_CLIENT_ID",
			"TRANSWARP_ACCESS_CLIENT_SECRET"
		])
	}

	private static func jobItem(_ configuration: AgentConfiguration?, jobID: String?) -> Item {
		guard let configuration, !configuration.jobs.isEmpty else {
			return .init(
				state: .blocked,
				title: "Job",
				detail: "Configure at least one build job"
			)
		}
		let requestedJobID = jobID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
		let job = requestedJobID.isEmpty
			? configuration.jobs.first
			: configuration.jobs.first { $0.id == requestedJobID }
		guard let job, !job.id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
			return .init(
				state: .blocked,
				title: "Job",
				detail: "Selected job is not configured"
			)
		}
		return .init(
			state: .ready,
			title: "Job",
			detail: "\(job.id) runs \(URL(fileURLWithPath: job.command).lastPathComponent)"
		)
	}

	private static func runnerTokenItem(_ configuration: AgentConfiguration?) -> Item {
		guard let token = configuration?.sharedToken, !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
			return .init(
				state: .blocked,
				title: "Runner token",
				detail: "Generate and save a runner token before CI dispatch"
			)
		}
		return .init(
			state: .ready,
			title: "Runner token",
			detail: "Stored locally; add the resolved value to TRANSWARP_TOKEN in GitHub"
		)
	}

	private static func coordinatorRunnerTokenItem(_ configuration: AgentConfiguration?) -> Item {
		guard let token = configuration?.sharedToken, !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
			return .init(
				state: .blocked,
				title: "Runner token",
				detail: "Generate and save a local runner token before coordinator dispatch"
			)
		}
		return .init(
			state: .ready,
			title: "Runner token",
			detail: "Stored locally; give the resolved value to the coordinator deployment, not GitHub Actions"
		)
	}

	private static func namedTunnelItem(_ configuration: AgentConfiguration?) -> Item {
		guard let tunnel = configuration?.tunnel else {
			return .init(
				state: .blocked,
				title: "Cloudflare Tunnel",
				detail: "Configure a named Cloudflare Tunnel"
			)
		}
		guard tunnel.mode == .named else {
			return .init(
				state: .blocked,
				title: "Cloudflare Tunnel",
				detail: "Use a named tunnel for stable CI dispatch; quick tunnels are demo-only"
			)
		}
		guard tunnel.publicURL != nil else {
			return .init(
				state: .blocked,
				title: "Cloudflare Tunnel",
				detail: "Set the stable HTTPS public URL for the named tunnel"
			)
		}
		return .init(
			state: .ready,
			title: "Cloudflare Tunnel",
			detail: "Named tunnel has a stable public URL"
		)
	}

	private static func registrationItem(_ configuration: AgentConfiguration?) -> Item {
		guard let configuration,
			  configuration.ciRegistrationURL != nil,
			  configuration.ciDeregistrationURL != nil else {
			return .init(
				state: .blocked,
				title: "Registration",
				detail: "Set register and deregister URLs"
			)
		}
		guard !configuration.registrationToken.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
			return .init(
				state: .blocked,
				title: "Registration",
				detail: "Generate and save a local target callback token"
			)
		}
		return .init(
			state: .ready,
			title: "Registration",
			detail: "Register/deregister URLs and local target callback token are configured"
		)
	}
}
