# Native migration: first version for human verification

Closeout date: 2026-09-29. This is the first native migration candidate for human
verification, in an unmerged draft stack. The user explicitly directed us to
record Firefox/REPL issues for later, document other failures and continue,
skip signed install/update, and stop performance optimization. That direction
supersedes treating those remaining checks as blockers to this handoff.

## Candidate and validation

- Product/source candidate: `674347705b7d3fc162146a4bfd5a7174ff57de13`,
  [draft #280](https://github.com/context-labs/whip/pull/280), branch
  `codex/backend-redesign-final-followthrough`.
- The closeout branch `codex/backend-redesign-human-verification` adds only
  documentation to that candidate; it does not change executable code, packages,
  test assertions, dependencies or CI requirements.
- Native Go backend, both engines, generated protocol v4, SDK, shared Web/Desktop,
  mobile, Go client, CLI/TUI, ACP and examples are ported. Retired execution/core,
  legacy SDK/protocol and temporary package exclusions are removed. Host config
  is version 21; fresh store schema is 55.
- Combined normal validation:
  [run 36594047984](https://github.com/context-labs/whip/actions/runs/36594047984)
  at the exact candidate: **38 successful jobs, one failed Desktop leaf, two
  dependent aggregate failures, zero skips** (41 jobs). The aggregate is failed,
  not green. NATIVE-04 below records the failure; the user-directed closeout
  proceeds with that known issue. Earlier failed runs remain failed history.
- Renderer SHA-256:
  `0af75b87cce53a45cc1fb60227912395b09dfe601c899c4abfeb0e0d79cdc39d`.
  Desktop consumes the verified renderer artifact from this same hosted run.
  Synthetic onboarding/editor fixtures identify their separate overlays in
  their evidence; they are not live-provider or installed-product evidence.

The [complete candidate job and artifact inventory](backend-native-candidate-validation.md)
preserves every result. The [family disposition](backend-native-core-retirement.md),
[domain contract](backend-domain.md), [frontend guide](frontend.md),
[SDK guide](../packages/sdk/README.md), and
[chronological development record](backend-redesign-development.md) retain the
implementation and exact earlier checkpoints. The normal gate is unchanged;
this handoff does not turn a failed, skipped or unverified check into a pass.

## Deferred issues

These issues are recorded for subsequent engineering and human reproduction.
None is being silently described as fixed by a later passing run.

| ID | Symptom and evidence | Disposition and next verification |
| --- | --- | --- |
| NATIVE-01 — Firefox small upward scroll | In [#279 run 36590021190](https://github.com/context-labs/whip/actions/runs/36590021190), activity job `109480377610`, the first upward 12 px wheel leaves gap 0 for five seconds (scrollTop 8301, viewport 656, content 8957). Latest is visible; no page/CSP error. Later complete local and #280 activity checks pass. | Deferred by user. Root cause remains unknown. Candidate records capped passive wheel, scroll, resize and frame evidence around the unchanged assertion. During a held stream, scroll slightly upward, then continue output; the reader should stay detached. Retain the new trace if it recurs. |
| NATIVE-02 — REPL overlapping-observation assertion | [#277 run 36584226063](https://github.com/context-labs/whip/actions/runs/36584226063) reports overlapping observations. Its artifact lacked the document/epoch/owner identities needed to prove a product duplicate. Subsequent complete browser workflows pass. | Deferred by user. Source review found that the fixture's raw socket counter can include an already locally retired observer until physical close. A tested fixture-only correction is preserved at **unintegrated** commit `fde8bd22020b42ff32977df4c985ccde4545c98e`; it is not in this candidate. Eleven focused tests and ten unchanged workflow groups in each browser pass on that leaf. It distinguishes document/runtime/epoch/session ownership, retains raw diagnostics and rejects real active overlap. Review it later; the original incident's specific cause remains unproven. |
| NATIVE-03 — cached child reading position | A native run stopped at cached switch 7: child message 006 at −5 px became message 084 at −29 px. The previous seven restores and root anchor passed; no new child history page was read. The failure's text contains no Latest control. Later bounded probes and full workloads pass. | Deferred under the user's direction to record issues and continue. No supported product fix was found. The optional bounded anchor trace is retained. A tiny Chromium/Firefox history test did not reproduce nested-scroll interference, so no history setting was changed. Manually switch root/child and use Back repeatedly after scrolling each to a different place. |
| NATIVE-04 — Desktop native browser navigation | Candidate [Desktop job 109501057523](https://github.com/context-labs/whip/actions/runs/36594047984/job/109501057523) fails after the eight-tab creation-limit checks at `whip:browser:act` → `navigate`. The page-3 load records `ERR_ABORTED (-3)` and then the same URL commits, while `BrowserManager.navigate` throws `Browser navigation failed`. The native browser fixture exits 1. | Deferred under the user's instruction to record failures and continue. This is a real failed candidate gate, distinct from the earlier hidden-first-frame screenshot issue. Types, package verification, 166 native/unit and 116 distribution checks completed before this stage; later onboarding/workspace/terminal/editor stages in this job did not run. Prior exact Desktop passes remain earlier evidence only. The failure's bounded diagnostic JSON is in the job log; the configured artifact directory had no files, so there is no Desktop evidence archive for this run. Reproduce from a disposable candidate package and retain navigation/owner/document events. |

Local source artifacts, which may expire, are supplemental to the durable
records above:

- NATIVE-01: `/tmp/whip-279-activity-artifact/`,
  `/tmp/whip-firefox-small-scroll-investigation/findings.md`, and
  `/tmp/whip-firefox-small-scroll-passive/`.
- NATIVE-02: `/private/tmp/whip-repl-observation-ownership/`,
  `/tmp/whip-repl-observation-ownership-results/results.json`, and
  `/tmp/whip-repl-observation-ownership-browser.log`.
- NATIVE-03: `/tmp/whip-performance-native-paired-before/run-GN5GYV/performance-failure.json`,
  `/tmp/whip-cached-anchor-source-followthrough.md`, and
  `/tmp/whip-nested-history-audit/README.md`.
- NATIVE-04: `/tmp/whip-280-job-109501057523.log` (bounded failure JSON at lines
  1371–1829) and `/tmp/whip-280-final-summary.md`.

Typing p95 of **72 ms is user-accepted**. The bounded natural-retention follow-up
completed with old documents collected and native connections closed. Exact
full-workload peaks and measurement limits remain in the
[performance audit](../apps/web/scripts/native-performance-control-audit.md).
No further optimization or performance investigation is part of this closeout.

## Environment acceptance and explicit limits

The ordinary automated matrix exercises actual native processes, both engines,
Chromium/Firefox, Linux/macOS client/race/build gates, four distribution build
targets, mobile exports/backend fixtures, UI, docs, and staged Desktop packaging
and IPC. Deterministic providers make those checks reproducible; they do not
establish real provider availability or account behavior.

| Environment | Evidence available | First-version disposition |
| --- | --- | --- |
| Web, SDK, Go client, CLI/TUI and ACP | Complete candidate hosted checks; native receipt/restart, explicit cancellation, scoped content and lifecycle fixtures. | Ready for human workflow verification with the known issues above. |
| Desktop | Candidate types/package/native-unit/distribution stages pass, but native browser navigation fails (NATIVE-04). [Earlier exact local packaging/lifecycle evidence](backend-native-desktop-readiness.md) covers synthetic onboarding, normal/failed turns, terminal/editor/browser IPC and disposable runtime restart on its recorded source. | Candidate full Desktop gate failed and later stages were not reached. Real hardware/accessibility, sleep/wake and minimum-OS behavior remain human checks. Signing is skipped. |
| Mobile | Candidate types/tests/backend fixtures and both Hermes exports; [earlier exact iOS simulator launch/storage/relaunch and Android native compilation](backend-native-mobile-readiness.md). | Physical network/suspension/process-death, key-loss/backup, accessibility and keyboard behavior remain unverified. Earlier native build artifacts are labeled by their source, not relabeled as this candidate. |
| Actual Safari | Prepared native Safari fixture and driver tests; WebKit or injected driver tests are not actual Safari acceptance. | Deferred: current preference is absent (not conclusive alone), and the latest actual native WebDriver attempt rejected missing Remote Automation. No new session or settings change. |
| Live providers and managed accounts | Both engines have deterministic native adapter/account coverage. Live evals require an explicitly prepared native host declaration and selected credentials. | No provider/credential selection was supplied for the prior bounded smoke request. No billable request, real login or account mutation is included. Human owner must prepare the target environment before running the documented opt-ins. |
| Real Chrome extension / computer permissions | Headed dedicated Chrome already passes the normal native-browser gate on both OSes. Real extension and platform TCC/accessibility are separate opt-ins. | Defer real extension/TCC actions to a prepared human-owned test environment. |
| Signed install/update and release publication | Disposable distribution/update contracts are covered by normal CI. | **Explicitly skipped by user.** No signed/notarized/quarantined installation, installed runtime update, merge, deployment or publication. |

### Current environment preflight

Read-only metadata checks completed at **2026-09-29T16:21:19Z** on macOS
26.3.1/Xcode 26.6/Safari 26.3.1. Safari's `AllowRemoteAutomation` preference is
absent; that does not alone prove effective state. The latest actual native
WebDriver attempt explicitly rejected the missing opt-in, so actual Safari
remains unverified. `WHIP_RLM_EVAL_DIRECTORY` is unset: no approved native host
configuration is selected, and no credentials were discovered or read.

Two paired iPhone records have unavailable/disconnected tunnels and no available
developer services; there is no usable connected target. Twenty-five simulators
are available and none is booted. No adb server is listening, and none was
started; connected Android availability remains unverified. The existing iOS
simulator and Android build records above are reused without relabeling their
source. No device, provider or settings action followed these missing prerequisites.
Bounded local evidence: `/tmp/whip-v1-environment-audit/current-preflight.json`.
These checks document environment limits and complete the available preflight;
they do not claim actual Safari, live-provider or physical-device acceptance.

## Human verification starting point

Use the exact candidate in a separate checkout. The original development
checkout has unrelated user changes; do not reset, clean or overwrite them.
The designated backend-redesign handoff checkout is also preserved. Review
source and documentation through their draft PRs; none is merged.

For a disposable browser session with a fake provider:

```sh
# From the isolated candidate checkout, with Go 1.27.0, Node 24 and Task 3.48.0:
npm ci --no-audit --no-fund
task dev:fixture -- --minutes=10
```

Open the printed **Web conversation** URL. The fixture owns its temporary
runtime/provider/storage and expires; Ctrl-C cleans it up. This is suitable for
UI verification without a real account or installed daemon. Use `hold:manual`
for held work, `permission:manual` for a test write, and `question:single` or
`question:batch` for decisions. For SDK-specific
manual work, use the separate private-directory example in the
[SDK guide](../packages/sdk/README.md). For mobile, follow the
[native development guide](mobile.md); physical-device
connectivity needs a prepared origin rather than silently weakening transport
checks. A native development client is required; Expo Go is insufficient.

| Area | Minimum human verification |
| --- | --- |
| Web and Desktop conversation | Send a message, observe streaming/settlement, interrupt, and submit another. Preserve a draft across tab/child/navigation/reload changes. Reproduce NATIVE-01/03 without changing scroll thresholds. |
| REPL and execution | Open live and completed cells, expand/collapse output, switch views, reload and reconnect. Confirm committed evidence replaces previews without duplicated panels. Record exact runtime/epoch/session/document evidence for NATIVE-02. |
| Permissions, children and content | Exercise Ask/deny/allow/question flow; inspect a child, return to root; upload/read/download scoped content; cancel observation versus explicitly stop work. |
| Settings and recovery | Check provider/model/permission choices on fresh test configuration. Disconnect/reconnect and restart the disposable host; verify accepted input and history are not duplicated and drafts remain local. |
| Desktop native resources | Use the candidate staged package, fresh test data and explicit consent to exercise terminal, editor and browser controls. Follow [Desktop readiness](backend-native-desktop-readiness.md) and [release runbook](desktop-releases.md); installed/signed update is outside this pass. |
| Mobile | Use a native development client and explicit test host. Verify send/cancel, background/resume/kill/reopen, Wi-Fi changes, storage failures, large text, rotation and keyboard/accessibility on selected devices. Simulator launch and export success alone do not establish these behaviors. |
| CLI/TUI, ACP and SDK | Use the candidate executable/client with a fresh explicit `WHIPCODE_HOME` and runtime. Verify ordinary input, root/child selection, cancellation, reconnect/restart and scoped output through the [CLI disposition](backend-native-cli-disposition.md), [terminal guide](native-terminal-retirement.md) and SDK examples. |

For each finding record the candidate commit, client/OS/browser/device, runtime
identity/epoch, action sequence, expected/actual behavior, and bounded logs or
screenshots with secrets removed. Keep these deferred items open until there is
specific evidence to close them. Human results and signed release readiness are
subsequent work; this document closes the requested first-version engineering
handoff without claiming those results.
