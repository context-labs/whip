# Whole-chat attachment drop target

Status: implemented, 2026-09-18. See [validation results](VALIDATION.md) for
completed checks and the remaining native Finder/cancellation verification.
`docs/frontend.md` remains the maintained architecture guide. The original
phases below are retained as the implementation record.

## Intended behavior

Dragging local images or existing supported file attachments over a chat shows
a subtle, theme-derived tint over that chat's body and a small **Drop to attach**
label. Dropping anywhere in that body adds the files to its composer. It does not
submit a message, steer a turn, navigate, or jump the transcript to the bottom.

- Cover the transcript, empty space, queue strip and composer below the workspace
  tab strip. Support existing root/child chats and the New Chat screen.
- In a split workspace, only the pane beneath the cursor highlights and receives
  the files, regardless of which pane previously held keyboard focus.
- Keep sidebar, tabs, inspector/dialogs, REPL, terminal and Browser views outside
  the attachment target. Native mobile presentation stays unchanged.
- Accept multiple attachments in drop order, append to existing draft attachments,
  and reuse current thumbnails, loaders, removal and preview behavior.
- Match the screenshot's interaction and restrained overlay, using Whip theme
  tokens rather than copying its purple color. Show a static label within the
  pane; no custom cursor, tracking animation, or layout displacement.
- Retain file picker and paste support. Dragging selected text, links or workspace
  tabs must not activate attachment handling. URL-only image drags do not trigger
  an implicit download; actual File payloads use the normal attachment path.

## Research findings

| Finding | Implementation consequence |
| --- | --- |
| `packages/app/src/composer.tsx` handles dragover/drop on its form and calls its existing `attach` callback. | The small hit area is a renderer boundary issue. Widen the target while keeping attachment admission and error handling in the same path. |
| `packages/app/src/welcome.tsx` has a separate form drop handler. Its `attach` callback stages files locally. | Cover New Chat too. A drop must not create a session or upload before Send. |
| `packages/app/src/compositions.ts` owns previews, scoped upload, cleanup and admission bounds. | Reuse this store unchanged. Current bounds are 16 files per draft, 20 MiB across unsent drafts, and 256 KiB per text attachment. |
| `packages/app/src/session-tab-strip.tsx` mounts each visible pane's selected content; `WorkspaceLayout` supplies pane geometry. | Bind to the concrete chat body and its recipient, not a window-wide selected-session lookup. |
| Workspace tab dragging already has its own pointer-driven path. | Native file dragging does not need the tab drag system or a new dependency. |
| Existing attachment browser coverage exercises upload, previews and reading position, but does not establish whole-pane native-drop behavior. | Extend these fixtures with actual drag events and add a manual OS-file drag check. |

Browser constraints:

- Identify an external file drag through transfer types/items during hover; read
  and copy File objects synchronously inside the drop handler. `files` is not
  readable during hover. [MDN: DataTransfer.files](https://developer.mozilla.org/en-US/docs/Web/API/DataTransfer/files)
- Cancel file dragover to accept drops and cancel handled file drops to prevent
  navigation. OS file drags do not dispatch dragstart/dragend in the page, so
  cleanup cannot depend on dragend alone. [MDN: file drag and drop](https://developer.mozilla.org/en-US/docs/Web/API/HTML_Drag_and_Drop_API/File_drag_and_drop)
- React events from portaled dialogs can bubble through the React parent tree
  even when the dialog is outside the pane's DOM. Use the actual container
  boundary when accepting a drop. [React: createPortal](https://react.dev/reference/react-dom/createPortal)

This requires no daemon, protocol, SDK, permission or upload-transport changes.

## Phase 1 — Share one pane-scoped drop path

1. Add a small app-owned file-drop helper/component, proposed at
   `packages/app/src/chat-file-drop.tsx`. Pass it the concrete container ref,
   current attachment availability and the composer's existing `attach` callback.
   Keep attachment state and validation in `CompositionStore`.
2. Give the chat body in `conversation.tsx` a stable, full-height container/ref
   and pass the ref to `Composer`. Do the equivalent for the full New Chat body
   in `welcome.tsx`, including space outside its centered form. Preserve existing
   flex/min-height/scroll boundaries. Do not remount the timeline on drag changes.
3. Bind native file events to that container. Remove the smaller form drop
   handlers once the pane handler owns drops, so dropping directly onto the
   composer still attaches each file exactly once. Picker/paste/drop call the
   same attachment callback for their respective composer.
4. Recheck availability at drop time. Existing chats retain their current
   connected-host, sending and upload-in-progress restrictions; New Chat retains
   local staging and its submission restrictions. Do not display an accepting
   prompt when the action is unavailable. Provide a short reason if a file is
   dropped while blocked, instead of silently losing the drop.
5. Copy all readable File objects before returning from the event handler.
   Preserve existing file validation and batch budgets; surface errors through
   existing composer feedback. Do not add directory traversal or URL fetching.

Exit check: dropping on transcript text, blank space, queue or composer adds the
same files exactly once to the correct recipient. Dropping on New Chat stages
them without issuing a create-session or upload request.

## Phase 2 — Add stable visual feedback and cleanup

1. Render a pane-local, absolutely positioned overlay using StyleX and existing
   focus/border, surface and text tokens. Keep it out of document flow and use
   `pointer-events: none`. Place it above chat content and below modal UI.
2. Track file entry/exit across descendants without toggling on every nested
   dragleave. Update React only when the visible drag state changes, not on
   every dragover. Use the native copy cursor for an available target and the
   unavailable cursor when blocked.
3. Clear the overlay on drop, true pane/window exit, cancellation where observable,
   window blur/hide, recipient/view changes and unmount. Explicitly test Escape
   cancellation from the OS rather than assuming the page receives dragend.
4. Ignore events originating in portaled dialogs or inert background content.
   Moving between split panes must clear the old pane's overlay before activating
   the new one; it must never change attachment ownership asynchronously.
5. Add one renderer-level fallback that prevents default navigation for otherwise
   unhandled file drops in Whip's shell. It must not attach files, alter normal
   text/link drags, or interfere with a native Browser view's own file uploads.
   Pane handlers own routing; this fallback only prevents accidental navigation.
6. Keep hover and drop from forcing focus, scrolling or submitting. Existing
   attachment insertion can grow the composer through its current sizing path;
   preserve the reading anchor while that happens. Use a brief polite status
   announcement and keep the keyboard-accessible attachment button available.

Exit check: the highlight does not flicker across rows, survive a cancelled drag,
cover neighboring panes, or alter transcript dimensions/reading position.

## Phase 3 — Validate and deliver

Extend focused component tests and the existing
`apps/web/scripts/composer-attachments.mjs`, `chat-activity.mjs` and
`new-chat-tabs.mjs` fixtures where appropriate:

| Scenario | Required outcome |
| --- | --- |
| One image, multiple images, mixed supported files, image-only draft | Original order, one attachment per file, existing previews/loaders work. |
| Drop directly on nested composer children or queued rows | Exactly one admission; no duplicate upload or message send. |
| Long history, streaming, reading older messages | No drag-induced layout shift or forced scroll-to-bottom; attachment growth preserves the reading anchor. |
| Two chat panes, two hosts, selected child agent | Only the drop target's scoped draft receives files. Shared views of the same recipient retain existing shared-draft behavior. |
| New Chat and first-message submission | Local staging, no early session creation; normal adoption/upload on Send. |
| Cross descendants/panes, leave window, Escape, switch view, close pane | Overlay clears and no stale drop handler targets another recipient. |
| Disconnected host, submission underway, upload underway, invalid or oversized batch | Truthful unavailable/error feedback; existing draft preserved; no navigation. |
| Text/link/tab drag, dialog/inspector, REPL and neighboring Browser | No accidental attachment or interference with their interactions. |
| Light/dark, increased contrast, reduced motion, narrow panes and zoom | Readable theme-derived overlay; no clipping or overflow. |

Run focused app/attachment/reading tests, app type checking, and the shared
web/desktop build. Exercise Chromium, Firefox and staged Electron with isolated
fixtures; capture the overlay and resulting attachments. Also perform a real
Finder-to-Electron drag, including cancellation, since synthetic drag events do
not prove every native OS behavior. Report any manual validation not completed.
Use an isolated daemon; do not restart the main daemon or use real conversations
as fixtures. No backend test expansion is needed unless implementation uncovers
a backend defect.

Update `docs/frontend.md` with ownership and drop-boundary behavior. No new
dependency, state store, global attachment-routing service or animation framework
is planned.
