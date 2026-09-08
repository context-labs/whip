import ApplicationServices
import Foundation

if CommandLine.arguments.dropFirst() == ["--check"] {
    guard ProcessInfo.processInfo.environment["GITHUB_ACTIONS"] == "true", AXIsProcessTrusted() else { exit(1) }
    print("CI accessibility permission verified"); exit(0)
}
// A disposable CI helper: only two buttons on the exact owned app process.
guard CommandLine.arguments.count == 3,
      let pid = Int32(CommandLine.arguments[1]), pid > 0,
      ["Cancel", "Restart to update"].contains(CommandLine.arguments[2]),
      ProcessInfo.processInfo.environment["GITHUB_ACTIONS"] == "true",
      AXIsProcessTrusted() else { fputs("CI accessibility permission is missing\n", stderr); exit(1) }
let title = CommandLine.arguments[2]
let application = AXUIElementCreateApplication(pid)
func attribute(_ node: AXUIElement, _ name: CFString) -> CFTypeRef? {
    var value: CFTypeRef?
    return AXUIElementCopyAttributeValue(node, name, &value) == .success ? value : nil
}
let deadline = Date().addingTimeInterval(25)
while Date() < deadline {
    var queue = [application]
    var visited = 0
    while !queue.isEmpty && visited < 4000 {
        let node = queue.removeFirst(); visited += 1
        if attribute(node, kAXRoleAttribute as CFString) as? String == kAXButtonRole,
           attribute(node, kAXTitleAttribute as CFString) as? String == title {
            guard AXUIElementPerformAction(node, kAXPressAction as CFString) == .success else { exit(2) }
            print("Pressed \(title) on owned process \(pid)"); exit(0)
        }
        for key in [kAXWindowsAttribute, kAXChildrenAttribute] {
            if let children = attribute(node, key as CFString) as? [AXUIElement] { queue.append(contentsOf: children) }
        }
    }
    Thread.sleep(forTimeInterval: 0.1)
}
fputs("Expected update confirmation button was not found\n", stderr); exit(3)
