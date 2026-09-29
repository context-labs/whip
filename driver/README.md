# whip-computer (the native macOS driver)

The Swift helper supplies accessibility-tree reads, input, ScreenCaptureKit
capture and macOS permission checks for native `computer.run` batches. The Go
[controller](../internal/computer/controller.go) owns a revocable generation;
its [connection](../internal/computer/connection.go) owns one exact subprocess.
Transport loss ends that generation. An uncertain action is never resent or
restarted automatically. See [computer use](../docs/browser-computer-use.md#computer-use-macos)
for authority, app policy and observation ownership.

## Build and test

```sh
task driver       # build the release helper and stage its exact bytes for embedding
task build        # build the native CLI and packaged renderer
task driver-test  # run the retained Swift XCTest suite
```

The helper builds on macOS with Xcode Command Line Tools. XCTest requires full
Xcode. The normal macOS CI driver job tests the Swift target, builds the release
helper and compiles the Go entry point with it embedded. These checks do not
grant Accessibility or Screen Recording access.

## Explicit installation and permissions

The native host's `computer.use_bundled` action publishes the embedded helper into
the selected runtime's `bin` directory using the displayed configuration
revision. It does not enable control or launch the helper. `computer.configure`
selects an explicit executable and app policy; opening a permitted batch is a
separate operation. Empty development builds report the bundle unavailable.
Status does not extract a binary, search the development tree, or read an
ambient executable override.

Accessibility permits accessibility-tree reads and input; Screen Recording
permits captures. macOS owns those permissions for the selected executable and
signature. A permission request is explicit; the helper can show links to the
system settings while the request waits. Tests use owned fake helpers or pure
Swift checks and do not grant these permissions or alter installed applications.

## Wire protocol

The helper writes `whip-computer/1` before newline-delimited JSON-RPC 2.0 frames
on stdin/stdout. Both the announcement and handshake must match Go's
`computer.ProtocolVersion`. Every request includes the private `params.token`
chosen by its owning connection and supplied through `WHIP_COMPUTER_TOKEN`;
empty or mismatched tokens are rejected. Stdout carries protocol frames only.

The native Go connection bounds request/response bytes, queued calls and
handshake time; cancellation closes and joins its process. Returned helper
errors expose known codes without echoing private token or arbitrary helper text.
The Go protocol, connection, controller and explicit-bundle tests exercise this
boundary with disposable executables instead of driving the user's desktop.
