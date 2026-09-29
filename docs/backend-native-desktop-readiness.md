# Native Desktop product-gate evidence

These checkpoints record local, disposable Desktop functional acceptance. They
do not mark Phases 5–7 complete. Reading-anchor and performance investigations,
hosted gates, and separately required platform acceptance retain their own status.

## Latest checkpoint: `e85f72203` — 2026-09-29

**PASS, exit 0:** unchanged `task check:product-desktop` on exact published source
`e85f72203b8d6610dbf9487f91057b393e466468` (#278), including the native browser
wrapper retirement and bounded skill-completion parser improvement.

Fresh isolated worktree: `/private/tmp/whip-desktop-gate-278`, branch
`codex/backend-redesign-desktop-gate-278`. The previous checkpoint's checkout and
evidence were preserved. Fresh owned `npm ci --ignore-scripts`, explicit Electron
dependency installation and Playwright FFmpeg preparation preceded the gate.
There were no source edits or assertion changes while it ran. Generated tracked
package evidence was copied before restoring its one tracked file to HEAD; the
checkout was clean after the gate and before this documentation-only update.

### Passed stages

- Desktop types and SDK build.
- Initial Desktop tests: 156 passed, 10 optional real-SSH cases skipped before a
  staged executable existed. Distribution/release/startup suite: 116 passed,
  plus the startup self-test.
- Isolated canonical-provider onboarding transport regression: 1 passed.
- Full production renderer/native helper build, Forge darwin-arm64 package and
  exact bundle verification.
- Fresh staged-helper rerun: all 166 Desktop tests passed, zero skipped,
  including real SSH fixtures. Distribution suite: 116 passed; startup self-test
  passed again.
- Actual native BrowserManager, production preload/IPC and native daemon
  discovery: metadata admission, visibility/zoom/overlays, CDP/screenshots,
  consent, cancellation uncertainty, navigation, epoch/owner rejection, atomic
  transfer, detach/release and partial-resource cleanup. Discovery's eight
  reported workflows included denial without an effect and release preserving
  the human page.
- Actual staged onboarding: explicit first-run installation, synthetic provider
  rejection and validation, model/default selection, title/compaction, streamed
  worker execution and draft/daemon reopen.
- Actual staged workspace in ordinary and deliberate failed-turn modes:
  retained drafts/tabs, split geometry, host disconnect/reconnect, GUI exit with
  daemon survival and same-daemon reopen. A failed turn persisted, then cleared
  after successful work.
- Native terminal keyboard/output, selection/Copy/paste, cursor recovery across
  reload and explicit close.
- Project-editor native IPC: local/runtime ownership, URL separation, required
  aliases and cancelled verification. Actual Finder launch remained disabled.

All recorded renderer-error arrays were empty. Expected negative IPC tests
logged rejected-operation errors; the complete gate passed. The full log is
`/tmp/whip-desktop-gate-278.log`. The evidence index and copied JSON reports are in
`/tmp/whip-desktop-gate-278-evidence/README.md`; ordinary and failed-turn workspace
reports were both preserved before their common output path was overwritten.
Screenshots and videos remain in the worktree's
`test-results/redesign/desktop/` directory.

### Exact provenance

| Artifact | SHA-256 |
| --- | --- |
| Renderer | `0af75b87cce53a45cc1fb60227912395b09dfe601c899c4abfeb0e0d79cdc39d` |
| Bundle | `f8b258c49eeccacc438f1e0d13a11749be23033952d991ff45c0cebc0d0b362c` |
| whipcode (56,101,392 bytes) | `14d0e4bd856633a39639583885b8b55db1f70045115015e86ef0b6bf0c8fed85` |
| whip-computer (431,840 bytes) | `0f358fbd23755e6c207fc63df8288d41d119866804daf679e0cbf80c904f25d7` |
| Root lockfile | `2ba06c3e0888a15fd2df57828f754e85f0e16423b48cf85758dbf79533a00ae2` |

Toolchain: Node 24.14.1; npm 11.11.0; Task 3.48.0; Go 1.27.0 darwin/arm64;
Xcode 26.6 build 17F113; macOS 26.3.1 arm64. Protocol 4.0/schema 55. Bundle
provenance reports the exact source commit with `dirty:false`. Onboarding's
reviewed synthetic transport overlay has its separately recorded runtime digest
`4b74192a3965508091d571e0d4f7075b14ea27b83ddd182e356edb0fb5f93ffb`; editor IPC
also records its dedicated disposable fixture provenance.

### Cleanup and limits

The Task process and all owned Desktop/runtime fixtures joined. A read-only
process inventory found no process referencing this worktree or its new fixture
paths. Unrelated onboarding daemons started September 23–24 were left untouched.
No installed runtime, user browser profile, real account or editor application
was changed or launched.

Signing/notarization and Finder-launch opt-ins were explicitly unset. Package
metadata records `signed:false`, `notarized:false`: disposable ad-hoc acceptance,
not signed, Finder-launched or notarized distribution. Provider behavior uses the
reviewed, fail-closed synthetic loopback overlay; no real provider request was
made. Publisher tests use mocked/disposable targets; no publication occurred.

This result makes no typing-latency, memory, physical-display, Safari or
real-device claim. It does not resolve the independent hosted performance-helper
build-order failure or the incomplete cached-child reading-anchor baseline.
A local pass alone does not establish that prior intermittent hosted
hidden-frame/navigation failures are fixed.

## Previous checkpoint: `1fa8dbfb2` — 2026-09-29

Result: PASS, exit 0, unchanged `task check:product-desktop` on exact source `1fa8dbfb2a6b93895c65dee820e3a465cdd1e73d`.

Worktree: `/private/tmp/whip-desktop-product-gate` (`codex/backend-redesign-desktop-product-gate`). No source fix or new commit required. Fresh owned `npm ci --ignore-scripts`, explicit Electron dependency install and Playwright FFmpeg setup completed first. Generated tracked package evidence was copied here then restored to HEAD.

### Passed stages

- Desktop types and SDK build.
- Initial desktop tests: 156 passed, 10 optional real-SSH cases skipped before a native executable existed.
- Distribution/release/startup fixture suite: 116 passed plus startup probe self-test. Publishing tests used their own mocked/disposable targets; no publication was performed.
- Isolated canonical-provider onboarding transport regression: 1 passed.
- Full production renderer/native helper build, Forge darwin-arm64 package, exact bundle verification.
- Desktop test rerun against freshly staged helper: 166 passed, zero skipped (all real SSH cases included); distribution 116 passed; startup self-test passed.
- Actual native BrowserManager/preload/IPC/runtime acceptance: guest visibility/layout, scoped CDP/screenshot/consent, navigation, cancellation uncertainty, owner/epoch rejection, transfer/retirement and cleanup. The hidden-guest first-frame case passed locally.
- Actual staged onboarding: fresh setup, rejection/validated synthetic key, selected-model/default confirmation, first streamed execution, title/helper and real compaction, draft reload and daemon restart.
- Actual staged workspace smoke: normal and deliberate failed-turn modes, draft/tab/sidebar split geometry, multi-host detach/reconnect, GUI exit/runtime survival and reopen. Failure remains saved then clears after successful work.
- Native terminal smoke: real shell keystrokes/output, native Copy/paste/selection, cursor recovery across reload, native window/menu behavior and explicit close stopping shell.
- Native project-editor IPC smoke: discovery, owner/runtime checks, local/URL separation, required alias and cancellation. Finder application launch intentionally false.

All recorded renderer error arrays were empty. Full log: `/tmp/whip-desktop-product-gate.log`. Detailed JSON copies are alongside this note; screenshots and videos remain under the worktree's `test-results/redesign/desktop/`. Normal workspace JSON was copied before the deliberate-failure pass overwrote the gate's common workspace output.

### Exact provenance

- Renderer SHA-256: `42344615a60f5d07055ff4d76719d76fb2a8709b7bd351e383c40ba09394a518`
- Bundle SHA-256: `9b9c7592302defc7533a50e863c9f8e962b300ef8e81f487b1ffe512d81513b7`
- whipcode SHA-256: `2a60026d74caf65df90557c251ab6ac3b8b8a8f63a2f4c2325404e2ea786635c` (57170272 bytes)
- whip-computer SHA-256: `55f8b8c02c4d37a64a0d055f4704248dce8aa1560e8b6d9ef55798dcc2d6ae78` (431904 bytes)

Toolchain: Node 24.14.1; npm 11.11.0; Task 3.48.0; Go 1.27.0 darwin/arm64; Xcode 26.6 build 17F113; macOS 26.3.1 arm64. Protocol 4.0/schema 55. Exact lockfile hash is recorded in release-evidence.json.

### Cleanup and limits

Task process and all owned desktop/runtime processes joined. No process referencing this worktree remained. A read-only process inventory found several unrelated onboarding daemons started September 23–24; those predate this run and were left untouched. No installed user runtime, account or browser profile changed.

Signing/notarization variables were unset. Package evidence is `signed:false`, `notarized:false`: disposable ad-hoc acceptance only. Onboarding intentionally uses its reviewed, fail-closed synthetic loopback provider transport overlay; its separate runtime/source digests are recorded. Project-editor smoke uses a separately built disposable fixture runtime; that digest is recorded too. No real provider request, publication, signed/Finder/notarized release, or performance claim. A local pass does not establish the previously intermittent hosted hidden-frame/navigation failures are fixed. Safari opt-in/real-device gates remain separate.
