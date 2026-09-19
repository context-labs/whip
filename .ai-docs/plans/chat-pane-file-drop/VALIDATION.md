# Whole-chat attachment drop validation

Implemented 2026-09-18 in the shared desktop/web renderer.

## Delivered behavior

- Each chat body accepts files over its transcript, empty space, queue and
  composer. New Chat stages files locally before the first submission.
- A static theme-derived overlay identifies the target pane. Multiple files
  preserve their order and enter the existing composition path exactly once.
- Split panes route to the pane under the drag without taking keyboard focus.
  Existing host/root/child draft scoping supplies attachment ownership.
- Picker, paste, previews, uploads, bounds, queueing and send behavior reuse the
  existing implementation. No daemon, protocol or SDK changes were needed.
- Unavailable targets show an explanation. Folders and unreadable drops report
  errors. Text/link/tab drags are ignored; unhandled shell file drops cannot
  navigate the app. Portaled dialogs and non-chat views do not admit files.
- Listeners and the cancellation expiry are cleaned up on recipient/view changes
  and unmount. Drag feedback does not change layout or force scrolling.

## Automated results

| Check | Result |
| --- | --- |
| Full shared-app suite (`npm run test:web`) | 866 tests across 78 suites passed. |
| App type checking (`npx tsc -p packages/app/tsconfig.json --noEmit`) | Passed. |
| Shared web and desktop build (`npm run build:desktop`) | Passed; desktop bundle staged. |
| Attachment/drop fixture, Chromium, Firefox and Electron | Passed in all three. Each admitted two batches of three mixed files, with zero duplicate uploads and zero hover scroll drift. |
| Existing composer reading-position probes | Four probes per browser, all measured zero scroll drift. |
| New Chat fixture, Chromium and Firefox | All 12 workflows passed in both. Two-pane drops stayed in the target draft without changing focus or creating a session early; first-message attachment submission passed. |
| Patch whitespace (`git diff --check`) | Passed. |

The nine new component tests cover nested boundaries, pane ownership, current
availability, modal exclusion, scope changes, file-only navigation prevention,
Escape/blur/expiry cleanup, unmount, directories and unreadable/error paths.
Existing attachment tests continue to cover picker, paste, validation and upload
failure. A stale New Chat fixture expectation was corrected: closing the final
New Chat tab intentionally leaves an empty workspace; the test now opens another
tab explicitly. That product behavior was not changed.

The browser fixtures use the production renderer, real SDK and isolated daemon,
with deterministic data and File payloads. They do not submit to live models or
use the user's conversations. Browser events are synthesized; these results do
not establish native OS drag cancellation behavior.

Commands for the browser runs:

```sh
WHIP_WEB_BROWSERS=chromium,firefox,electron WHIP_CHAT_COMPOSER_ONLY=1 \
  WHIP_CHAT_ACTIVITY_RESULTS=/tmp/whip-chat-drop-results \
  node apps/web/scripts/chat-activity.mjs

WHIP_WEB_NEW_CHAT_RESULTS=/tmp/whip-chat-drop-new-results \
  node apps/web/scripts/new-chat-tabs.mjs
```

Renderer digest verified by the three-browser fixture:
`e2836b45e5aacba8bf6071ee91d16ad38561a980d46fe96880b94dca91902ba4`.

Local evidence:

- `/tmp/whip-chat-drop-results/results.json`, screenshots and videos.
- `/tmp/whip-chat-drop-new-results/`, New Chat reports and screenshots.
- `/tmp/whip-chat-drop-all-ui.log`, `/tmp/whip-chat-drop-types.log`,
  `/tmp/whip-chat-drop-build.log`.

Dark, light, increased contrast, large text, reduced motion and narrow-pane
configurations were exercised. The full-pane dark overlay, narrow light overlay
and New Chat layout were visually inspected. The overlay is static and uses
existing theme tokens; it schedules no animation loop.

## Remaining manual checks

A real Finder-to-Electron check was attempted using a temporary app with a
distinct bundle identity and a synthetic image. The isolated SDK bridge reached
its four-minute timeout while arranging the native test windows. A successful
native file admission and Escape cancellation were therefore **not verified**.
The fixture and temporary Finder window were closed. The main daemon and user
conversations were not modified or restarted.

The optional native fixture is repeatable with:

```sh
WHIP_WEB_BROWSERS=electron WHIP_CHAT_NATIVE_DROP=1 \
  WHIP_CHAT_ACTIVITY_RESULTS=/tmp/whip-chat-drop-native-results \
  node apps/web/scripts/chat-activity.mjs
```

Complete the Finder drag and press Enter in that test terminal within the
fixture's four-minute lifetime. Success requires a trusted native drop event,
the image preview and an enabled Send button. Separately check OS Escape while
hovering, window exit, and a native Browser view's file-upload control. Manual
screen-reader announcement verification was not performed; the overlay uses a
polite status region and retains the existing keyboard attachment controls.
