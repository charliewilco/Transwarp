// swift-tools-version: 6.2

import PackageDescription

let package = Package(
	name: "Transwarp",
	platforms: [
		.macOS(.v14)
	],
	products: [
		.executable(name: "Transwarp", targets: ["TranswarpApp"]),
		.library(name: "TranswarpCore", targets: ["TranswarpCore"])
	],
	targets: [
		.target(name: "TranswarpCore"),
		.executableTarget(
			name: "TranswarpApp",
			dependencies: ["TranswarpCore"],
			resources: [
				.copy("Resources")
			]
		),
		.testTarget(
			name: "TranswarpCoreTests",
			dependencies: ["TranswarpCore"]
		),
		.testTarget(
			name: "TranswarpAppTests",
			dependencies: ["TranswarpApp"]
		)
	]
)
