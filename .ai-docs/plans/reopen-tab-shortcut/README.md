# Desktop reopen-closed-tab shortcut

Branch: `whip-rlm`

## Goal

On desktop only, Shift+Cmd+T reopens and selects the most recently closed workspace tab. Use CmdOrCtrl+Shift+T in Electron for the platform equivalent. Repeated presses walk backwards through existing closed-tab history. With no closed tabs, do nothing.

## Non-goals

No web shortcut, new history store, dependency, daemon changes, or new restoration semantics. Reopening follows the existing Reopen closed tab command for sessions, drafts and Browser tabs. Closed terminals are deliberately excluded from existing history because closing them ends their shells; this shortcut does not change that policy or resurrect a deleted session.

## Prior art and design

- `packages/app/src/session-tabs.ts`: `reopenView()` already pops the newest retained descriptor, restores its position in the original surviving pane (otherwise the focused pane), enforces workspace/Browser limits, and returns the restored ID. Closed history is already bounded.
- `packages/app/src/session-tab-strip.tsx`: `SessionTabActions.reopen()` already restores, navigates, and reports failures. Call it rather than duplicating this logic.
- `apps/desktop/src/main.ts`: File > New session supplies the native accelerator/event pattern, including revealing a hidden window.
- `packages/app/src/{desktop-bridge,platform}.ts` and `apps/web/src/platform/desktop.ts`: typed desktop event and optional subscription adapter. Add a reopen-closed-tab event/subscription alongside new-session. The browser platform receives no subscription or DOM hotkey.
- `packages/app/src/shell.tsx`: subscribe with cleanup and preserve existing dialog guards; invoke the existing tab action. Verify behavior from Settings as well as workspace/home.
- `apps/desktop/src/browser-manager.ts`: embedded guests currently leave T shortcuts to the native menu. Verify Shift+Cmd+T is not swallowed.
- Existing session-tabs tests cover close/reopen order, surviving panes, drafts, and retained identity. Extend desktop integration tests rather than adding another state implementation.
- `docs/frontend.md`, `docs/features.md`, `docs/roadmap.md`, and the preceding new-session-shortcut plan describe the current shared workspace/native shortcut architecture. No external harness behavior is needed.

## Ordered tasks

- [x] Trace existing restoration and native shortcut paths.
- [x] Confirm implementation scope with user.
- [x] Add File > Reopen closed tab with CmdOrCtrl+Shift+T; reveal/focus the window before emitting.
- [x] Add typed event, optional platform subscription, desktop adapter, and shell listener using existing reopen action.
- [x] Test last-closed order, no-history no-op, modal guard, hidden window, Settings, preserved draft/session identity, capacity failure, and subscription cleanup. Native menu/IPC and embedded Browser non-interception pass; physical-key acceptance remains manual.
- [x] Update desktop shortcut docs, feature map, frontend guide where appropriate, and roadmap.
- [x] Run relevant app/desktop checks and task check; distinguish existing failures from regressions. Review diff for correctness/simplicity and record native-input verification limitations honestly.

## Status

Implemented; final read-only adversarial correctness/ponytail review found no actionable issues. Physical OS accelerator acceptance remains manual.

The tab strip is unmounted on Settings, so the existing reopen operation was
extracted into `reopenClosedTab(runtime, navigate)` in `session-tab-routing.ts`.
The strip still clears its local picker/notice; the native listener calls it when
mounted and the shared helper otherwise. Settings displays workspace failures.
No tab-store changes or new history were needed.

Validation (2026-09-23):
- `npm run test:web -- packages/app/test`: all 1,146 tests passed (88 files),
  including native adapter/shell, routing selection, no-history, modal/capacity
  guards, terminal exclusion, and failed-navigation coverage.
- `git diff --check`: passed.
- `npm run check:web`: passed (app TypeScript and production renderer build).
- `npm run check:desktop`: passed; staged desktop production build passed.
- `npm run test:desktop`: passed, including startup probe self-test.
- `node apps/desktop/scripts/browser-native.mjs`: passed; verifies both T chords
  are not prevented by the native embedded guest input handler.
- `node apps/desktop/scripts/terminal-smoke.mjs`: passed using staged production
  assets. File > Reopen closed tab restored the exact draft URL after native
  window close, revealed/focused that window, and kept the running terminal.
  Artifacts: `/tmp/whip-reopen-tab-terminal-results`.
- `task check`: blocked by pre-existing gofmt issues in
  `.claude/worktrees/session-trace/internal/{daemon/session.go,session/otlp_export.go}`.
  Those unrelated files were left untouched.
- Physical Shift+Cmd+T / OS accelerator acceptance is not claimed. The menu's
  configured accelerator, native event/IPC, renderer restoration and guest
  non-interception are covered; real-key installed-app acceptance remains manual.

Test-discovered clarification: closed terminals never enter existing Reopen
history (`removeViews` deliberately excludes them). Tests and docs now state this
explicitly; the shortcut preserves that behavior rather than starting a new shell.

