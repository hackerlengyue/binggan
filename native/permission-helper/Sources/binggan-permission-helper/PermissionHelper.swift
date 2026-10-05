import AppKit
import PermissionFlow

@MainActor
private final class PermissionHelper: NSObject {
    private let appURL: URL
    private let controller: PermissionFlowController
    private var panelWasVisible = false
    private let startedAt = Date()

    init(appURL: URL) {
        self.appURL = appURL
        self.controller = PermissionFlow.makeController(
            configuration: .init(
                requiredAppURLs: [appURL],
                promptForAccessibilityTrust: false,
                localeIdentifier: "zh-Hans"
            )
        )
    }

    func run() {
        NSApp.setActivationPolicy(.accessory)
        let mouse = NSEvent.mouseLocation
        let source = CGRect(x: mouse.x - 16, y: mouse.y - 16, width: 32, height: 32)
        controller.authorize(
            pane: .accessibility,
            suggestedAppURLs: [appURL],
            sourceFrameInScreen: source
        )
        Timer.scheduledTimer(
            timeInterval: 1,
            target: self,
            selector: #selector(closeWhenPanelEnds),
            userInfo: nil,
            repeats: true
        )
        NSApp.run()
    }

    @objc private func closeWhenPanelEnds() {
        let visible = NSApp.windows.contains { $0 is NSPanel && $0.isVisible }
        if visible {
            panelWasVisible = true
        } else if panelWasVisible || Date().timeIntervalSince(startedAt) > 1800 {
            NSApp.terminate(nil)
        }
    }
}

@main
private enum PermissionHelperMain {
    @MainActor
    static func main() {
        guard CommandLine.arguments.count == 3,
              CommandLine.arguments[1] == "--app",
              CommandLine.arguments[2].hasSuffix(".app"),
              FileManager.default.fileExists(atPath: CommandLine.arguments[2]) else {
            fputs("usage: binggan-permission-helper --app /path/to/Binggan.app\n", stderr)
            exit(2)
        }
        _ = NSApplication.shared
        let helper = PermissionHelper(
            appURL: URL(fileURLWithPath: CommandLine.arguments[2], isDirectory: true)
        )
        helper.run()
    }
}
