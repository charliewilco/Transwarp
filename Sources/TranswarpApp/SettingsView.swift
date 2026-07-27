import SwiftUI
import TranswarpCore

struct SettingsView: View {
	@Environment(AppModel.self) private var model
	@State private var draft = ConfigurationDraft()
	@State private var draftError: String?
	@State private var coordinatorBaseURL = ""

	var body: some View {
		@Bindable var model = model

		Form {
			Section("Configuration File") {
				LabeledContent("Path") {
					Text(model.configurationPath?.path ?? "Unavailable")
						.lineLimit(1)
						.truncationMode(.middle)
						.textSelection(.enabled)
				}

				HStack {
					Button {
						model.openConfiguration()
					} label: {
						Label("Open JSON", systemImage: "doc.text")
					}

					Button {
						model.revealConfiguration()
					} label: {
						Label("Reveal", systemImage: "folder")
					}

					Button {
						reload()
					} label: {
						Label("Reload", systemImage: "arrow.clockwise")
					}
				}
			}

			Section("Machine") {
				TextField("Machine Name", text: $draft.machineName)
				generatableField("Machine ID", text: $draft.machineId) {
					draft.generateMachineId()
				}
				TextField("Listen Address", text: $draft.listenAddress)
				TextField("Workspace Root", text: $draft.workspaceRoot)
				Toggle("Prevent sleep during builds", isOn: $draft.preventSleep)
			}

			Section("Runner Security") {
				generatableSecureField("Runner Token", text: $draft.sharedToken) {
					draft.generateSharedToken()
				}
				stackedEditor("Additional Redacted Values", text: $draft.redactedValues, height: 64)
				Toggle("Start runner when Transwarp opens", isOn: $model.startRunnerOnLaunch)
				Toggle("Open Transwarp at login", isOn: Binding(
					get: { model.loginItemState.isEnabled },
					set: { model.setOpensAtLogin($0) }
				))
				.disabled(!model.loginItemState.canToggle)
				if model.loginItemState.message != "Enabled" && model.loginItemState.message != "Off" {
					Text(model.loginItemState.message)
						.font(.caption)
						.foregroundStyle(.secondary)
				}
			}

			Section("Cloudflare Tunnel") {
				Picker("Mode", selection: $draft.tunnelMode) {
					ForEach(TunnelConfiguration.Mode.allCases, id: \.self) { mode in
						Text(mode.rawValue.capitalized)
							.tag(mode)
					}
				}
				.pickerStyle(.segmented)

				TextField("cloudflared Path", text: $draft.cloudflaredPath)
				TextField("Public URL", text: $draft.publicURL)

				if draft.tunnelMode == .named {
					SecureField("Tunnel Token", text: $draft.tunnelToken)
					TextField("Tunnel Name", text: $draft.tunnelName)
				}
				TextField("Runner Access Client ID", text: $draft.runnerAccessClientID)
				SecureField("Runner Access Client Secret", text: $draft.runnerAccessClientSecret)
			}

			Section("CI Registration") {
				LabeledContent("Coordinator Base URL") {
					HStack {
						TextField("https://ci.example.com", text: $coordinatorBaseURL)
						Button {
							applyCoordinatorBaseURL()
						} label: {
							Label("Use Coordinator", systemImage: "link")
						}
						.help("Fill register, heartbeat, and deregister URLs from this coordinator base URL")
					}
				}
				TextField("Register URL", text: $draft.ciRegistrationURL)
				TextField("Heartbeat URL", text: $draft.ciHeartbeatURL)
				TextField("Deregister URL", text: $draft.ciDeregistrationURL)
				generatableSecureField("Registration Token", text: $draft.registrationToken) {
					draft.generateRegistrationToken()
				}
				TextField("CI Access Client ID", text: $draft.ciAccessClientID)
				SecureField("CI Access Client Secret", text: $draft.ciAccessClientSecret)
				stackedEditor("Allowed Report Origins", text: $draft.allowedReportOrigins, height: 64)
				Stepper("Heartbeat: \(draft.heartbeatSeconds) seconds", value: $draft.heartbeatSeconds, in: 5...600, step: 5)
			}

			Section("Primary Job") {
				if !draft.additionalJobs.isEmpty {
					LabeledContent("Additional Jobs", value: "\(draft.additionalJobs.count)")
						.help("Additional JSON-defined jobs are preserved when settings are saved.")
				}
				TextField("Job ID", text: $draft.jobId)
				TextField("Label", text: $draft.jobLabel)
				TextField("Working Directory", text: $draft.jobWorkingDirectory)
				Toggle("Clone allowed repository before running", isOn: $draft.jobCheckout)
				TextField("Command", text: $draft.jobCommand)
				Stepper("Timeout: \(draft.jobTimeoutSeconds) seconds", value: $draft.jobTimeoutSeconds, in: 60...86400, step: 60)
				stackedEditor("Arguments", text: $draft.jobArguments, height: 96)
				stackedEditor("Allowed Repositories", text: $draft.jobAllowedRepositories, height: 80)
				SecureField("Checkout Authorization Header", text: $draft.jobCheckoutAuthorizationHeader)
				stackedEditor("Environment", text: $draft.jobEnvironment, height: 80)
				stackedEditor("Secret Environment", text: $draft.jobSecretEnvironment, height: 80)
				stackedEditor("Redacted Environment Keys", text: $draft.jobRedactedEnvironmentKeys, height: 64)
			}

			if model.isActive {
				Label("Stop the runner before saving settings.", systemImage: "exclamationmark.triangle")
					.foregroundStyle(.orange)
			}

			if let draftError {
				Text(draftError)
					.foregroundStyle(.red)
			}

			HStack {
				Spacer()
				Button("Save") {
					save()
				}
				.keyboardShortcut("s", modifiers: [.command])
				.buttonStyle(.borderedProminent)
				.disabled(model.isActive)
			}
		}
		.formStyle(.grouped)
		.padding()
		.frame(width: 680)
		.frame(minHeight: 720)
		.onAppear(perform: reload)
	}

	private func generatableField(_ title: String, text: Binding<String>, action: @escaping () -> Void) -> some View {
		LabeledContent(title) {
			HStack {
				TextField(title, text: text)
				Button {
					action()
				} label: {
					Label("Generate", systemImage: "arrow.triangle.2.circlepath")
				}
				.labelStyle(.iconOnly)
				.help("Generate \(title)")
			}
		}
	}

	private func generatableSecureField(_ title: String, text: Binding<String>, action: @escaping () -> Void) -> some View {
		LabeledContent(title) {
			HStack {
				SecureField(title, text: text)
				Button {
					action()
				} label: {
					Label("Generate", systemImage: "arrow.triangle.2.circlepath")
				}
				.labelStyle(.iconOnly)
				.help("Generate \(title)")
			}
		}
	}

	private func stackedEditor(_ title: String, text: Binding<String>, height: CGFloat) -> some View {
		VStack(alignment: .leading, spacing: 6) {
			Text(title)
				.font(.subheadline)
			TextEditor(text: text)
				.font(.system(.body, design: .monospaced))
				.frame(minHeight: height)
				.overlay {
					RoundedRectangle(cornerRadius: 6)
						.stroke(.separator)
				}
		}
	}

	private func reload() {
		model.reloadConfiguration()
		if let configuration = model.configuration {
			draft = ConfigurationDraft(configuration: configuration)
			coordinatorBaseURL = ConfigurationDraft.inferredCoordinatorBaseURL(registrationURL: draft.ciRegistrationURL)
			draftError = nil
		}
	}

	private func applyCoordinatorBaseURL() {
		do {
			try draft.applyCoordinatorBaseURL(coordinatorBaseURL)
			draftError = nil
		} catch {
			draftError = error.localizedDescription
		}
	}

	private func save() {
		do {
			try model.saveConfiguration(draft.makeConfiguration())
			if let configuration = model.configuration {
				draft = ConfigurationDraft(configuration: configuration)
				coordinatorBaseURL = ConfigurationDraft.inferredCoordinatorBaseURL(registrationURL: draft.ciRegistrationURL)
			}
			draftError = nil
		} catch {
			draftError = error.localizedDescription
		}
	}
}

#Preview {
	SettingsView()
		.environment(AppModel())
}
