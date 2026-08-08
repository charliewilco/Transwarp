import AppKit
import SwiftUI
import Testing
@testable import TranswarpApp

@MainActor
@Suite
struct ContentViewSnapshotTests {
	@Test
	func compactStatesRenderForVisualInspection() throws {
		let outputDirectory = URL(filePath: FileManager.default.currentDirectoryPath)
			.appending(path: ".build/visual-evidence", directoryHint: .isDirectory)
		try FileManager.default.createDirectory(at: outputDirectory, withIntermediateDirectories: true)

		let states: [(name: String, model: AppModel, expanded: Bool)] = [
			("needs-setup", .previewNeedsSetup, false),
			("stopped", .previewStopped, false),
			("available", .previewAvailable, false),
			("paused", .previewPaused, false),
			("queued", .previewQueued, false),
			("running", .previewRunning, false),
			("passed", .previewPassed, false),
			("failed", .previewFailed, false),
			("error", .previewError, false),
			("expanded-activity", .previewExpandedActivity, true),
		]

		for state in states {
			let image = try render(
				ContentView(showsActivity: state.expanded)
					.environment(state.model),
				size: CGSize(width: 440, height: state.expanded ? 420 : 240)
			)
			let data = try #require(image.representation(using: .png, properties: [:]))
			try data.write(to: outputDirectory.appending(path: "\(state.name).png"), options: [.atomic])
			#expect(image.size.width == 440)
			#expect(image.size.height == (state.expanded ? 420 : 240))
			#expect(!isBlank(image))
		}
	}

	@Test
	func ciWorkflowSettingsRenderForVisualInspection() throws {
		let outputDirectory = URL(filePath: FileManager.default.currentDirectoryPath)
			.appending(path: ".build/visual-evidence", directoryHint: .isDirectory)
		try FileManager.default.createDirectory(at: outputDirectory, withIntermediateDirectories: true)

		let image = try render(
			Form {
				CIWorkflowSettingsView()
			}
			.formStyle(.grouped)
			.padding()
			.environment(AppModel.previewAvailable),
			size: CGSize(width: 680, height: 520)
		)
		let data = try #require(image.representation(using: .png, properties: [:]))
		try data.write(to: outputDirectory.appending(path: "settings-ci-workflows.png"), options: [.atomic])
		#expect(image.size.width == 680)
		#expect(image.size.height == 520)
		#expect(!isBlank(image))
	}

	private func render<Content: View>(_ content: Content, size: CGSize) throws -> NSBitmapImageRep {
		let root = content
			.frame(width: size.width, height: size.height, alignment: .topLeading)
			.background(Color(nsColor: .windowBackgroundColor))
		let hostingView = NSHostingView(rootView: root)
		hostingView.frame = NSRect(origin: .zero, size: size)
		hostingView.appearance = NSAppearance(named: .darkAqua)
		hostingView.layoutSubtreeIfNeeded()

		let bitmap = try #require(hostingView.bitmapImageRepForCachingDisplay(in: hostingView.bounds))
		hostingView.cacheDisplay(in: hostingView.bounds, to: bitmap)
		return bitmap
	}

	private func isBlank(_ image: NSBitmapImageRep) -> Bool {
		guard let first = image.colorAt(x: 0, y: 0) else {
			return true
		}

		let stepX = max(1, image.pixelsWide / 12)
		let stepY = max(1, image.pixelsHigh / 12)
		for y in stride(from: 0, to: image.pixelsHigh, by: stepY) {
			for x in stride(from: 0, to: image.pixelsWide, by: stepX) {
				if image.colorAt(x: x, y: y) != first {
					return false
				}
			}
		}
		return true
	}
}
