# Native gate audit — 2026-09-29

This audit starts from core-removal checkpoint `d90668cc3`. It separates the
normal automated gate from milestone evidence; it does not declare Phases 5–7
complete. No installed runtime, user profile, accessibility permission or live
provider was changed. The original development checkout was not used.

## Current acceptance snapshot

The user accepted the measured 72 ms native typing p95 on 2026-09-29 and asked
to close speed optimization. The earlier 50 ms target is superseded for this
redesign acceptance. Historical measurements below retain their original
limits. Reading correctness remains separate. The bounded natural-retention
investigation is complete: old documents collect after navigation, their native
connections close, and settlement releases the temporary Markdown spans. The
full-workload peaks remain recorded, with no invented memory pass threshold.

Reconciled through published source `ad7a4955f` and the local final
follow-through on 2026-09-29. “Implemented” and a passing checkpoint
are distinct from final milestone acceptance. The complete staged Desktop gate
passes at `e85f72203`; the complete normal CLI/TUI result is from `1fa8dbfb2`
and the complete local UI result is from `d9545097d`. Older checkpoints do not
cover subsequent source changes.
The [plan checklist](backend-redesign-plan.md#phase-5--port-retained-product-capabilities)
records Phase 5 implementation complete and final Phase 6–7 acceptance open.
The [chronological record](backend-redesign-development.md) retains the earlier failures and intermediate results.

| Retained family | Current native replacement and evidence | Acceptance still required |
| --- | --- | --- |
| Execution, engines, accounting, recursion, mail and state | [Family disposition](backend-native-core-retirement.md#behavior-and-test-disposition), both-engine store/runtime tests and [real process fixture](../scripts/redesign/v4-fixture.test.mjs); complete package discovery replaces the retired allowlist. All hosted race partitions pass on Linux/macOS at `1edc9d823`. | Final combined aggregate remains open; passing race partitions do not imply aggregate success. |
| Providers, accounts, definitions, hooks, goals and schedules | [Provider setup](models-providers.md#provider-connections-in-settings), immutable declarations and native operation/maintenance paths are mounted in shared/mobile/terminal clients. Settings, executor and ordinary-input fixtures use the real host with deterministic transports. | Current combined client gate and representative live-provider/managed-account workflows; synthetic onboarding is not live readiness. |
| Context, instructions, skills, history and workspace | Exact raw-history/summary boundaries and scoped originals; [image compaction regression](../internal/runtime/compaction_content_test.go), actual REPL/skills/history/restore fixtures. Published/default skill roots and explicit body grants are separate shipped controls. The slash-completion caret race is repaired with four regressions; hosted conversation checks pass in both browsers at `e85f72203`. | The earlier intermittent REPL overlap has not been causally explained; a later unchanged passing gate does not establish its repair. Final combined browser gates remain required. Settings history passes on `b1c9ca965` and `1edc9d823`; no seed optimization is claimed. |
| Files, LSP, shell, permissions/questions and MCP | [Native family tests](backend-native-core-retirement.md#behavior-and-test-disposition), joined process/LSP ownership, direct human actions, scoped grants and real MCP self-host fixtures; mounted controls preserve root/child distinctions. | Final-head automated/lifecycle validation; live external servers remain explicit opt-in evidence. |
| Browser/computer and native helpers | External Chrome and offered Desktop tabs have distinct scoped owners and human controls across clients. Both ambient wrappers are removed: [browser driver disposition](browser-computer-use.md#native-browser-test-ownership), [computer disposition](backend-native-computer-retirement.md). Actual private Chrome covers both drivers/engines; Desktop bridge fixtures cover real IPC. | Headed dedicated/real extension opt-ins, real accessibility/TCC and applicable platform checks are not established by headless or fake-helper tests. |
| SDK, shared app and mobile | At `1fa8dbfb2`: protocol18 checks, SDK205/example5 tests and mobile types/226 tests. Shared app types and all1388 tests pass at the `e85f72203` increment. [Mobile readiness](backend-native-mobile-readiness.md) separately records exports, simulator and Android compilation provenance. | Final combined gates, physical-device UI/lifecycle/accessibility and signed-device checks. Simulator storage evidence does not prove Android or physical-device behavior. |
| Go CLI, TUI and ACP | Complete normal `check:native-cli` passes at `1fa8dbfb2`, including real compiled entrypoints, SSH/update fixtures, both native TUI partitions and pure presentation complement; [CLI](backend-native-cli-disposition.md) and [terminal](native-terminal-retirement.md) dispositions retain exact semantics. Both hosted client gates now pass at `e85f72203`, including the repaired SSH and explicit-Bash packaged-runtime fixtures. | Final combined-source aggregate remains open; earlier failed SSH/Task checks are preserved as history. |
| Shared UI | Complete `check:product-ui` passes locally at `d9545097d` and hosted at `1edc9d823`: Storybook,66 themes/14 interactions, strict CSP, isolated packed consumers, tabs/layout in both browsers and30 model-picker scenarios. Workspace regressions fail before their repairs; no tolerance changes. | Final combined-source gate. This is not all application browser workflows or actual Safari. |
| Desktop and distribution | Complete [staged Desktop gate](backend-native-desktop-readiness.md) passes locally and hosted at exact published `e85f72203`, including166 tests,116 distribution checks, onboarding, normal/failure workspace flows and terminal/editor/browser IPC. Exact local package/helper/renderer hashes and joined cleanup are recorded. | Signed/quarantined release, target/minimum-OS execution, real SSH hardware/account scenarios remain distinct. Typing speed is user-accepted; the bounded natural-retention investigation is complete with full-workload peak limits preserved. |

Hosted run36584226063 at `1edc9d823` finishes with 34 passed jobs, four failed
leaf jobs, two failed aggregates and skipped Desktop. Analysis, evals, mobile, UI,
Settings, product browser and all Linux/macOS race partitions pass. Three failure
classes require follow-through: Linux dedicated Chrome loses Xvfb's display
environment, the Chromium REPL probe reports overlapping observations, and both
client gates reach an unsupported signal trap in Task's embedded shell. The
launch and Task failures have reproduced local repairs; REPL still needs exact
observer lifetime evidence. Skipped Desktop supplies no hosted acceptance.
The earlier `b1c9ca965` run remains failed evidence; its dependency-download
failures were neither product findings nor passing checks.

Dedicated Chrome now explicitly inherits only `DISPLAY` and `XAUTHORITY`; the
shared process environment remains restricted. The regression fails before the
repair and passes as a real process on macOS and Linux, including unrelated-secret
exclusion and unchanged headless/ordinary process isolation. The complete normal
native-browser gate passes locally at `d0769783f` (browser15.430s/runtime12.475s).
Hosted native-browser gates now pass on both Linux and macOS at `e85f72203`.

The packaged-runtime fixture now executes its existing owned-directory/trap/
build/race sequence in explicit Bash. The original block reproduces the trap
failure before any Go command; the repair preserves success and both build/test
failure cleanup without running a later command after failed build. The actual
packaged runtime integration passes in 3.413s with the same two-minute deadline,
race, shuffle and engine assertions.

Hosted #278 run36588313432 at `e85f72203` finishes with 38 successful jobs,
one failed leaf and two propagated aggregate failures; no jobs are skipped.
Both client/native-browser jobs, the complete conversation job and Desktop pass. Its
performance job fails before the workload because a newly required helper test
imports the SDK before it is built. The next increment runs the existing
`web-assets` prerequisite first; asset generation from absent SDK output and all
22 helper contracts pass locally. This setup repair supplies no timing result.
Optional bounded cached-anchor diagnostics are also available, with four passing
lifecycle/bounds tests; enabled diagnostic runs are excluded from performance
acceptance. The earlier child-anchor failure remains unexplained. Hosted #279
at `ad7a4955f` passes the repaired performance gate but exposes two Firefox
failures: the first small upward scroll after streaming leaves no bottom gap,
and Settings assumes a unique provisional-output hint across two operation
panels sharing one cell. The latter fixture now checks exact canonical ownership
and passes both browsers locally; the scroll investigation remains open.

Hosted #279 run `36590021190` finishes at exact `ad7a4955f` with 37 successful
jobs, those two failed leaves, two dependent aggregate failures and no skips.
Desktop, both client jobs and the repaired performance gate pass. The next
increment corrects the Settings ownership assertion and adds bounded passive
scroll diagnostics; its final combined run is still required. Neither the
unexplained earlier REPL overlap nor the cached child-anchor failure is declared
causally repaired by subsequent passing runs.

The command boundaries below remain the normal complete gate; do not replace them
with selected passing subsets. Reading/observation reliability and the final
combined gate remain open. Later leaves are not retroactively covered by earlier
results. The accepted speed result requires no further optimization.

## Gate restoration at the initial checkpoint

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

## Historical gate-restoration validation at the initial checkpoint

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
  corrected-viewport Desktop sample had Event Timing p95 72 ms and natural peak
  aggregate RSS 1,581,968 KiB; all functional groups passing did not close the
  50 ms target or memory investigation. Its final retention sample followed
  forced GC, so that sample is diagnostic and cannot stand in for natural
  end-of-work memory. The peak/after-typing samples preceded GC and remain valid.
  A later focused run retains all seven groups and reports all40 native events,
  but p95 remains72ms. Its end-of-work natural RSS is1,596,864KiB; earlier tab
  checkpoints did force GC. A42.43second inspection pause also followed stream
  startup, so that run does not establish unchanged temporal stream overlap.
  [The performance audit](../apps/web/scripts/native-performance-control-audit.md)
  preserves these boundaries. The harness now reports missing timing entries as
  unknown and places inspection before streams. Root-cause tracing and final
  retention acceptance were pending at that checkpoint. Subsequent user
  acceptance closes the typing-speed target; the350MiB figure is an
  investigation trigger, not a newly invented hard pass threshold. The performance owner is recording
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
prefixes). Current progress summaries distinguish implemented controls from
remaining gates; the removed fixture link is pinned to immutable history. The
driver README describes explicit native Controller/Connection and helper
publication; the unused automatic helper wrapper is removed. Its replacement
coverage is recorded in [the computer disposition](backend-native-computer-retirement.md).

The separate history-seed investigation remains unchanged: the hosted 120-second
failure was real but not reproduced locally. The exact durable workload passed
on macOS and constrained Linux/arm64; no count, timeout, FULL synchronous setting
or production store API was changed. The exact hosted Settings workload now
passes at `b1c9ca965`, as recorded above. This is follow-through, not proof of an
unimplemented optimization or an explanation of the earlier timeout. Detailed
diagnosis remains in `/tmp/whip-native-history-seed-diagnosis-2026-09-29.md`.

## Hosted workflow contract follow-up

The first core-removal distribution job exposed three workflow-test failures:
new checkouts lacked an explicit immutable source, and two assertions still
required the retired standalone CI job names. All new checkouts now specify
`github.sha`. The tests require both distribution matrix platforms, Node/Go/task
and browser dependencies before execution, and the ordinary asset prerequisite
before the installer checks. The aggregate's actual shell body is exercised with
all-success and every individual failure, cancellation, skip and missing result.

The next distribution unit suite also still called the retired generation API.
Its five readiness cases now use native runtime identity, process epoch and web
state, retaining wrong-build, incomplete-gateway and same-PID guarantees. Missing
runtime/epoch evidence is refused instead of counting as a successful restart.
No product lifecycle, install destination, workload or deadline was changed.
All 36 publication/workflow tests passed in 51.502 seconds and all five native
gateway readiness tests passed in 0.002 seconds. The changed workflow also passes
`actionlint`. Logs: `/tmp/whip-native-ci-publish.log` and
`/tmp/whip-native-ci-distributions-unit.log`. The complete hosted distribution
scenario still needs its final integrated rerun.

## Passive transfer memory attribution

The Desktop performance harness now labels the existing periodic RSS samples
and adds at most16 passive boundary samples around upload completion, each
preview removal, document navigation, and the combined content download/native
save. Each records sampling duration, process RSS, browser heap/DOM totals, and
numeric connected light-DOM/Markdown/attachment counts. Traversal counts cap
at100,000 with explicit lower-bound truncation; no DOM nodes or content are
retained by the probe. These sequential reads are not atomic allocation-owner
evidence. They run outside the timed transfer keys and never force GC, change
the workload or delete owners. The user subsequently accepted the measured72ms
typing result; no further latency optimization is required. The350MiB memory
investigation trigger remains an investigation trigger, not a pass/fail limit.

The18 focused Node checks pass, including traversal bounds and diagnostic
connection teardown on failure/stall. A small actual Chromium/CDP fixture also
verified mounted attachment removal and subtree counts; its process-memory
source was stubbed, so it is not a Desktop workload or memory-acceptance result.
No idle period was added to that workload. The subsequent complete untraced
pair records the natural full-workload phase samples. A separate bounded native
follow-up performs three same-root navigations and explicit fixture settlement,
with natural 35-second samples and no forced GC. Every old document returns
from two documents to one; old native transports close, and settlement removes
3,999 fade spans. Renderer RSS finishes at 461.4 MiB versus 459.0 MiB initially.
All five owned processes join and no diagnostic bounds overflow. This satisfies
the old-document retention investigation for that bounded case, without proving
every byte's owner, physical footprint, signed-package or long-duration behavior.
The full-workload peaks remain 1,629,712 KiB baseline / 1,693,136 KiB candidate.
Exact source, hashes, samples and limitations are in the
[retention outcome](../apps/web/scripts/native-performance-control-audit.md#retention-only-native-outcome-2026-09-29).
No further memory or typing experiment is required by this investigation.
