# Native gate audit — 2026-09-29

This audit starts from core-removal checkpoint `d90668cc3`. It separates the
normal automated gate from milestone evidence; it does not declare Phases 5–7
complete. No installed runtime, user profile, accessibility permission or live
provider was changed. The original development checkout was not used.

## Restored omissions

The Swift manifest previously omitted its entire XCTest target. The advertised
`task driver-test` did not swap manifests and failed with “no tests found”. The
manifest now includes the existing 13 tests, the macOS CI driver job runs them,
and the local full `ci` task includes the macOS-only `driver-test` task. Ordinary
`swift build` still builds the helper without building tests.

These tests cover key parsing, JSON values/protocol rejection, running-application
metadata and the empty accessibility-snapshot guard. `AXTree.resolveApp` only
enumerates `NSWorkspace.shared.runningApplications`; `element` rejects a missing
snapshot before any accessibility operation. The tests never launch or activate
an app, send input, capture a screen, request TCC access or start the helper.
The optional already-running TextEdit assertion is not actual UI-control proof.

Three native Chrome tests previously skipped without
`WHIP_BROWSER_NATIVE_TEST_BINARY`. The required `check:native-browser` gate now
sets and validates an explicit executable before running their unchanged
race/shuffle scenarios: `TestNativeHeadlessOwnedProcess`,
`TestExternalBrowserOwnedHeadlessUploadAndScreenshot` and
`TestExternalBrowserBothEnginesRestartKeepsValuesWithoutControl`. Both browser
drivers and both execution engines remain covered. Their profiles, uploads,
runtime files and process ownership are disposable.

This gate is part of `check:change` and the required Linux/macOS CI matrix.
Local invocation defaults to the installed, lockfile-pinned Playwright Chromium.
Linux CI uses its sandbox-capable system Google Chrome, as the existing browser
fixtures do; macOS CI installs Playwright Chromium. Neither production launch
arguments nor runner security settings are weakened. Missing executables fail
the gate instead of passing through the tests' opt-in skip.

The three injected Safari driver lifecycle tests are now in
`check:product-conversation`, after its existing SDK/assets build prerequisite.
They prove denied creation, ordinary cleanup and forced joining after a stalled
delete. They do not start Safari. Actual Safari remains `check:product-safari`
with an explicitly enabled Remote Automation prerequisite.

## Focused validation

- Existing Swift XCTest target: all 13 passed on macOS/arm64, Xcode 26.6,
  Swift 6.3.3; `/tmp/whip-native-ci-swift.log`. The actual `task driver-test`
  entrypoint also passed all 13; `/tmp/whip-native-ci-task-driver.log`.
- Release helper build passed in 8.78 seconds;
  `/tmp/whip-native-ci-swift-build.log`.
- Actual `task check:native-browser` passed: browser 3.866 seconds and runtime
  11.987 seconds, race/shuffle enabled; `/tmp/whip-native-ci-browser.log`.
  An explicit missing executable failed before Go tests.
- Injected Safari lifecycle suite: all three passed in 6.393 seconds;
  `/tmp/whip-native-ci-safari-lifecycle.log`. An initial standalone invocation
  failed before testing because the new checkout had no built SDK; the ordinary
  SDK build supplied it, without changing assertions or product code.
- `actionlint` passed for the changed CI workflow; `git diff --check` passed.

These are local focused results, not the final integrated Linux/macOS matrix.
The independent dynamic-package shell-loop repair belongs to the integration
checkpoint and is not included here.

## Final gate and remaining evidence

`task ci` (also `task check:phase`) is the full host gate. `check:change` covers
whole-module build/vet, complementary race groups, contracts/SDK/process clients
and native Chrome; `check:analysis` keeps the pinned frozen lint baseline and
reachable-vulnerability scan. The remaining normal tasks cover web, browser,
conversation, UI, content, activity, settings, distributions, examples, evals,
docs, mobile and Desktop. The reusable CI workflow adds Linux/macOS execution,
four Go cross-compilation targets and the platform-specific driver/Desktop/mobile
jobs; its required aggregate rejects failed, cancelled and skipped dependencies.
Final-revision execution, rather than task parsing alone, is required evidence.

The following checks remain distinct from a green automated aggregate:

- **Actual Safari:** run `task check:product-safari` on an explicitly prepared
  macOS test machine. Injected lifecycle tests and Playwright WebKit are not
  substitutes, and the probe does not enable Remote Automation itself.
- **Staged Desktop performance:** run the existing performance entrypoint with
  `WHIP_WEB_PERFORMANCE_HOST=desktop` during a quiet measurement interval.
  The normal `product-performance` job measures web, not staged Desktop. The
  previously recorded Desktop sample had Event Timing p95 72 ms and aggregate
  RSS 1,581,968 KiB; all functional groups passing did not close the 50 ms target
  or memory investigation. The 350 MiB figure is an investigation trigger, not
  a newly invented hard pass threshold. The performance owner is recording
  subsequent measurements separately.
- **Physical mobile and signed artifacts:** native exports and backend fixtures
  do not prove suspension/process death, Wi-Fi/cellular transitions, SecureStore
  and SQLCipher device behavior, accessibility/keyboard or TestFlight/store
  installation. Follow `mobile-release.md` and preserve its owner prerequisites.
- **Release/platform acceptance:** cross-compilation is not execution on every
  architecture or minimum OS. Signed/quarantined Desktop installation,
  update continuity, TCC continuity, sleep/wake, real remote SSH/hardware keys
  and platform accessibility remain governed by `desktop-releases.md`.
  No publishing, deployment or installed-runtime mutation is authorized here.
- **Representative live providers:** deterministic native evals and account
  fixtures do not prove real provider/account behavior. The explicit opt-in
  commands in `evals/rlm/README.md` use a separately prepared native host config
  and disposable data; both engines and managed account workflows need their
  own truthful evidence.

The retirement table's named native test files were checked against the source
tree and exist (the table abbreviates the common `internal/` prefix and package
prefixes). Final documentation should still reconcile the plan's old progress
paragraphs and links to removed daemon/legacy-SDK files with immutable historical
links. The driver README also needs its retired `computer_exec`, `Helper` and
`~/.whip/bin` setup prose replaced by the current explicit helper publication
contract. These are documentation obligations, not new compatibility paths.

The separate history-seed investigation remains unchanged: the hosted 120-second
failure was real but not reproduced locally. The exact durable workload passed
on macOS and constrained Linux/arm64; no count, timeout, FULL synchronous setting
or production store API was changed. Its detailed evidence remains in
`/tmp/whip-native-history-seed-diagnosis-2026-09-29.md`. A later hosted pass must
not be described as proof of an unimplemented optimization.
