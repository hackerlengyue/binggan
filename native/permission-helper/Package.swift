// swift-tools-version: 6.2

import PackageDescription

let package = Package(
    name: "BingganPermissionHelper",
    platforms: [.macOS(.v13)],
    dependencies: [
        .package(url: "https://github.com/jaywcjlove/PermissionFlow.git", exact: "2.11.2"),
    ],
    targets: [
        .executableTarget(
            name: "binggan-permission-helper",
            dependencies: [
                .product(name: "PermissionFlow", package: "PermissionFlow"),
            ]
        ),
    ]
)
