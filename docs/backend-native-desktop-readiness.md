# Native Desktop full product gate — 2026-09-29

Result: PASS, exit 0, unchanged `task check:product-desktop` on exact source `1fa8dbfb2a6b93895c65dee820e3a465cdd1e73d`.

Worktree: `/private/tmp/whip-desktop-product-gate` (`codex/backend-redesign-desktop-product-gate`). No source fix or new commit required. Fresh owned `npm ci --ignore-scripts`, explicit Electron dependency install and Playwright FFmpeg setup completed first. Generated tracked package evidence was copied here then restored to HEAD.

## Passed stages

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

## Exact provenance

- Renderer SHA-256: `42344615a60f5d07055ff4d76719d76fb2a8709b7bd351e383c40ba09394a518`
- Bundle SHA-256: `9b9c7592302defc7533a50e863c9f8e962b300ef8e81f487b1ffe512d81513b7`
- whipcode SHA-256: `2a60026d74caf65df90557c251ab6ac3b8b8a8f63a2f4c2325404e2ea786635c` (57170272 bytes)
- whip-computer SHA-256: `55f8b8c02c4d37a64a0d055f4704248dce8aa1560e8b6d9ef55798dcc2d6ae78` (431904 bytes)

Toolchain: Node 24.14.1; npm 11.11.0; Task 3.48.0; Go 1.27.0 darwin/arm64; Xcode 26.6 build 17F113; macOS 26.3.1 arm64. Protocol 4.0/schema 55. Exact lockfile hash is recorded in release-evidence.json.

## Cleanup and limits

Task process and all owned desktop/runtime processes joined. No process referencing this worktree remained. A read-only process inventory found several unrelated onboarding daemons started September 23–24; those predate this run and were left untouched. No installed user runtime, account or browser profile changed.

Signing/notarization variables were unset. Package evidence is `signed:false`, `notarized:false`: disposable ad-hoc acceptance only. Onboarding intentionally uses its reviewed, fail-closed synthetic loopback provider transport overlay; its separate runtime/source digests are recorded. Project-editor smoke uses a separately built disposable fixture runtime; that digest is recorded too. No real provider request, publication, signed/Finder/notarized release, or performance claim. A local pass does not establish the previously intermittent hosted hidden-frame/navigation failures are fixed. Safari opt-in/real-device gates remain separate.
