# Desktop New session shortcut

Branch: `whip-rlm`
Status: Implemented and reviewed; automated validation passed except the unrelated
repository-wide formatting blocker below. Physical-key acceptance remains unverified.

## Approved scope

The original proposal included a web alternative. The user approved implementation
on 2026-09-23 with an explicit change: **skip the shortcut on web altogether**.

- Desktop: **Command+T** / File > New session opens an independent New Chat draft
  in the focused pane. The menu uses `CmdOrCtrl+T`, matching the existing desktop
  accelerator convention. macOS is the supported desktop target.
- The action works from Settings, the empty workspace, an existing draft/chat,
  REPL/trace, a terminal, or an embedded website. It preserves drafts and work.
- It does not create a Browser tab, a window, or an immediate daemon session.
  The existing first-message creation/promotion flow is unchanged.
- Open dialogs block it; the existing 32-tab limit/error reporting remains intact.
- Web: **no new shortcut**, listener, preference, or terminal passthrough change.
  Native browser Command+T / Control+T is untouched. New session remains available
  through the sidebar and command palette.

## Research and ponytail decision

The creation action already exists: `openNewChat` in
`packages/app/src/session-tab-routing.ts` uses `SessionTabs.openNew`, allocates a
fresh ID, inserts/selects the draft beside the current tab in the focused pane,
uses the existing selected-host defaults, and reports errors via `reportWorkspace`.
The sidebar, tab menu, and command palette already share this path. New Chat's
focused composer already requests autofocus; onboarding retains its own behavior.

The desktop already routes File > Close tab through a typed native event,
optional `AppPlatform` subscription, and shared shell effect. Reuse that pattern;
no command registry, preference schema, dependency, new IPC method, Go/SDK/protocol
change, global OS shortcut, or session-creation rewrite.

A focus-dependent conflict existed: `BrowserManager` intercepted guest
Command/Control+T, called `preventDefault`, and emitted `new-browser`. Electron
specifies that preventing `before-input-event` also suppresses menu accelerators.
Delete that interception and obsolete event case instead of adding another handler.
Explicit Browser-tab UI and palette actions remain available.

Ordinary web pages cannot reliably override the browser's New Tab chord. Chrome's
Keyboard Lock requires JavaScript-initiated fullscreen and permission; neither it,
a browser extension, nor a PWA project belongs in this change. The user's desktop-only
revision avoids that problem entirely.

Sources consulted:

- `docs/frontend.md`: package boundaries, native adapter ownership, web shortcut policy.
- `docs/features.md`, `docs/roadmap.md`: tabs and independent New Chat already ship.
- `docs/learnings/other-harnesses/opencode/opencode-ux.md`: reuse shared actions and
  visible shortcut hints, not a large command registry for one binding.
- [Electron keyboard shortcuts](https://www.electronjs.org/docs/latest/tutorial/keyboard-shortcuts)
- [Electron before-input-event](https://www.electronjs.org/docs/latest/api/web-contents#event-before-input-event)
- [Chrome Keyboard Lock](https://developer.chrome.com/docs/capabilities/web-apis/keyboard-lock)
- [Chrome Keyboard Lock demo](https://chrome.dev/keyboard-lock/)

## Implementation

1. `apps/desktop/src/main.ts`: File > New session with `CmdOrCtrl+T`, reveal/focus
   the window and renderer (including guest-origin activation), then emit `new-session`.
2. `packages/app/src/desktop-bridge.ts`, `platform.ts`, and
   `apps/web/src/platform/desktop.ts`: typed event and optional `onNewSession`
   subscription, with the existing unsubscribe/disposal conventions. Preload
   implementation is unchanged because it already forwards the typed event union.
3. `packages/app/src/shell.tsx`: subscribe once, reject dialog/alertdialog activation,
   and call `openNewChat`. Settings intentionally uses the same behavior as the
   existing palette New session action rather than the Close tab behavior.
4. `apps/desktop/src/browser-manager.ts`, `packages/app/src/browser-types.ts`, and
   `shell.tsx`: delete guest T interception and the obsolete `new-browser` event.
5. Update `docs/frontend.md`, `docs/desktop.md`, `docs/features.md`, and
   `docs/roadmap.md`. The native File menu advertises the shortcut; web does not.

## Regression coverage

- `packages/app/test/desktop-adapter.test.ts`: event isolation, delivery,
  unsubscribe, and disposal/late subscription.
- `packages/app/test/desktop-close-tab.test.tsx`: reuse the existing palette
  creation fixture for native creation across routes, independent draft IDs,
  preserved drafts/connections, empty workspace, capacity failure, both modal
  roles, and cleanup. Existing routing/tab tests cover placement and persistence.
- `apps/desktop/scripts/browser-native-main.ts` via `browser-native.mjs`: real
  production guest WebContents leaves Command/Control+T unprevented and emits no
  competing Browser shortcut. This is not a physical keyboard test.
- `apps/desktop/scripts/terminal-smoke.mjs`: staged production menu -> preload ->
  platform -> shell -> New Chat path with terminal focus; closing the new draft
  returns to the same running terminal with retained output. Asserts the actual
  menu accelerator and verifies a previously closed/hidden window is revealed
  and focused. Calling a menu item is not proof of OS key dispatch.

Review caught the hidden-window case: focusing WebContents alone does not show
the window. The menu now uses the existing `window.show(); window.focus()` pattern
before focusing the renderer; the added production smoke regression passed.

## Validation on 2026-09-23

Passed:

- Focused app tests: **167 passed**.
- `npm run check:web` (types and production build).
- `npm run check:desktop`.
- `npm run test:desktop`.
- `npm run test:web`: **87 files, 1,103 tests passed**.
- `node apps/desktop/scripts/browser-native.mjs`: `NATIVE_MANAGER_OK`, including
  the guest New session non-interception regression.
- `node apps/desktop/scripts/build.mjs --renderer-ready`.
- `node apps/desktop/scripts/terminal-smoke.mjs`: production menu/IPC check and
  terminal preservation passed; no renderer errors. Evidence:
  `/tmp/whip-desktop-terminal-results/desktop-terminal.json`.
- `git diff --check`.

Blocked, unrelated to this change:

`task check` passed model metadata validation, then stopped at its recursive
`gofmt -s -l .` gate because these pre-existing nested-worktree files need formatting:

- `.claude/worktrees/session-trace/internal/daemon/session.go`
- `.claude/worktrees/session-trace/internal/session/otlp_export.go`

Those files were not changed. The remaining repository-wide Go/contract tasks did
not run through `task check`; frontend and desktop checks above were run separately.

Native menu/guest tests use an isolated staged app, not the installed signed app.
Two additional macOS CGEvent spot checks (process-targeted, then HID-tap posting
with a verified frontmost fixture PID) timed out without opening a draft, despite
existing Accessibility permission. They used a temporary variant of the terminal
smoke, not the committed suite; it was removed afterward. These attempts are
**inconclusive**, not passing shortcut evidence. Failure details/screenshots:
`/tmp/whip-desktop-new-session-native-results/desktop-terminal-failure.txt` and
`desktop-terminal-failure.png`. No further blind retries were made.

Physical Command+T acceptance, including key holding and rapid separate presses,
still needs manual verification in desktop with composer, terminal, embedded page,
Settings, and Design UI focus. Do not claim physical-key or signed-release acceptance.
The production menu/IPC smoke was rerun after the hidden-window fix and passed.


## Checklist

- [x] Research the creation path, native bridge, web constraint, and guest conflict.
- [x] Adopt the user's desktop-only scope; remove the proposed web binding.
- [x] Add native menu/event/adapter/shell wiring, reusing existing New Chat behavior.
- [x] Delete the guest shortcut conflict; retain explicit Browser-tab actions.
- [x] Extend focused tests and run native guest/menu integration checks.
- [x] Update current docs and record the unrelated full-check blocker.
- [x] Finish adversarial review; hidden-window finding fixed, no remaining source-level findings.
- [x] Record native-key spot checks as inconclusive; do not claim physical-key acceptance.
- [ ] Manually verify physical Command+T and held/rapid-key behavior in desktop.

The checkout already contained unrelated runtime, icon, README, and documentation
changes. They remain intact. No commits or staging were requested.
