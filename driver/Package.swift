// swift-tools-version: 5.10
import PackageDescription

// Building the helper does not build tests. Running the XCTest target requires
// full Xcode; the existing tests do not request TCC access or control any apps.
let targets: [Target] = [
    .target(
        name: "WhipComputerCore",
        path: "Sources/WhipComputerCore"
    ),
    .executableTarget(
        name: "WhipComputer",
        dependencies: ["WhipComputerCore"],
        path: "Sources/WhipComputer"
    ),
    .testTarget(
        name: "WhipComputerTests",
        dependencies: ["WhipComputerCore"],
        path: "Tests/WhipComputerTests"
    ),
]

let package = Package(
    name: "whip-computer",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "whip-computer", targets: ["WhipComputer"]),
    ],
    targets: targets
)
