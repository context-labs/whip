import XCTest
@testable import WhipComputerCore

final class KeyMapTests: XCTestCase {
    func testPlainLetter() throws {
        let c = try KeyMap.parse("a")
        XCTAssertEqual(c.keyCode, 0x00)
        XCTAssertTrue(c.flags.isEmpty)
    }

    func testUppercaseImpliesShift() throws {
        let c = try KeyMap.parse("A")
        XCTAssertEqual(c.keyCode, 0x00)
        XCTAssertTrue(c.flags.contains(.maskShift))
    }

    func testSuperC() throws {
        let c = try KeyMap.parse("super+c")
        XCTAssertEqual(c.keyCode, 0x08)
        XCTAssertTrue(c.flags.contains(.maskCommand))
    }

    func testNamedKeys() throws {
        XCTAssertEqual(try KeyMap.parse("Return").keyCode, 0x24)
        XCTAssertEqual(try KeyMap.parse("Tab").keyCode, 0x30)
        XCTAssertEqual(try KeyMap.parse("Up").keyCode, 0x7E)
        XCTAssertEqual(try KeyMap.parse("KP_0").keyCode, 0x52)
    }

    func testModifierAliases() throws {
        XCTAssertTrue(try KeyMap.parse("cmd+a").flags.contains(.maskCommand))
        XCTAssertTrue(try KeyMap.parse("ctrl+a").flags.contains(.maskControl))
        XCTAssertTrue(try KeyMap.parse("alt+a").flags.contains(.maskAlternate))
    }

    func testUnknownKeyFails() {
        XCTAssertThrowsError(try KeyMap.parse("Foosball"))
    }

    func testUnknownModifierFails() {
        XCTAssertThrowsError(try KeyMap.parse("hyper+a"))
    }
}

final class AnyCodableTests: XCTestCase {
    func testRoundTrip() throws {
        let req = #"{"jsonrpc":"2.0","id":1,"method":"state","params":{"app":"TextEdit","gen":3}}"#
        let r = try JSONDecoder().decode(Request.self, from: Data(req.utf8))
        XCTAssertEqual(r.method, "state")
        XCTAssertEqual(r.params?["app"]?.stringValue, "TextEdit")
        XCTAssertEqual(r.params?["gen"]?.intValue, 3)

        let resp = Response.ok(r.id, .object(["generation": .int(4)]))
        let data = try JSONEncoder().encode(resp)
        let decoded = try JSONDecoder().decode(Response.self, from: data)
        XCTAssertNil(decoded.error)
        XCTAssertEqual(decoded.result?.objectValue?["generation"]?.intValue, 4)
    }

    func testErrorResponse() throws {
        let resp = Response.err(.int(7), RPCError(.staleGeneration, "state changed"))
        let data = try JSONEncoder().encode(resp)
        let decoded = try JSONDecoder().decode(Response.self, from: data)
        XCTAssertEqual(decoded.error?.code, RPCErrorCode.staleGeneration.rawValue)
        XCTAssertEqual(decoded.error?.message, "state changed")
    }
}

final class AXTreeTests: XCTestCase {
    // These tests inspect running-app metadata and an empty snapshot guard only.
    // They do not read AX state, request TCC permission, or launch/control apps.

    func testResolveUnknownAppFails() {
        let tree = AXTree()
        XCTAssertThrowsError(try tree.resolveApp("definitely-not-a-real-app-name-xyz")) { err in
            guard case let e as RPCError = err, e.code == RPCErrorCode.unknownApp.rawValue else {
                return XCTFail("expected unknownApp RPCError, got \(err)")
            }
        }
    }

    func testResolveTextEditIfRunning() throws {
        // Inspect an already-running TextEdit, if present; never launch it.
        let tree = AXTree()
        if let (_, pid) = try? tree.resolveApp("TextEdit") {
            XCTAssertGreaterThan(pid, 0)
        }
    }

    func testStaleElementWithoutSnapshot() {
        let tree = AXTree()
        XCTAssertThrowsError(try tree.element(app: "TextEdit", index: 0, gen: nil)) { err in
            guard case let e as RPCError = err, e.code == RPCErrorCode.staleGeneration.rawValue else {
                return XCTFail("expected staleGeneration, got \(err)")
            }
        }
    }
}

final class JSONRPCTests: XCTestCase {
    func testParseGarbageIsRejected() {
        let dec = JSONDecoder()
        XCTAssertThrowsError(try dec.decode(Request.self, from: Data("not json".utf8)))
    }
}
