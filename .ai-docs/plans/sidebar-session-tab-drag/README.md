# Drag sidebar sessions into workspace tabs

Branch: `compaction-loop-and-ui-cleanup`
Status: Research and proposed implementation plan; no implementation yet.

## Goal

Drag a saved session from the left sidebar into a specific position in the app's top tab bar, on web and desktop. Support the tab bars of existing split panes.

**User decision (2026-09-19): always open another view.** If the session is already open, leave every existing view untouched and create a fresh chat view. This is not a fork or a new daemon session. Ordinary sidebar clicks retain their current reuse/navigation behavior.

## Research findings

The current implementation, not historical proposals, is the authority:

- `packages/app/src/session-sidebar.tsx:289–304`: rows are router links with normal-click handling through `runtime.tabs.open`; the action button is a sibling. Preserve link semantics and exclude action controls from dragging.
- `packages/app/src/session-tabs.ts:437–453`: `open` reuses `preferred`, which can be a chat, REPL, or trace view. It cannot implement always-new chat views. `canOpen(runtimeId, rootId)` also allows reuse at capacity, so creation must use the unconditional capacity check.
- `packages/app/src/session-tabs.ts:547–555,659–681`: tab creation and transfer already belong to the immutable workspace store. Transfer adjusts an insertion index for a removed source; a newly created view must NOT apply that adjustment.
- `packages/app/src/session-tab-routing.ts:40` onward and `docs/frontend.md:717–747`: view IDs distinguish duplicate views even when URLs match. Navigation must use `tabDestination` and its `whipViewId`, not a bare session URL.
- `packages/ui/src/workspace-layout.tsx:107–130,163` onward: one drag provider currently wraps only the workspace. It locates tab-strip and content/split targets. The sidebar is outside it (`packages/app/src/shell.tsx:131–145`).
- `packages/ui/src/workspace-tab-drag.tsx:28–43,60–203`: tab hit testing, animated gaps, floating previews, cancellation, and click suppression already exist. Drag start currently requires a sortable tab, assumes a source strip, and uses source-tab geometry. Sidebar rows are neither sortable tabs nor tab-sized; simply adding a draggable attribute will not work.
- `packages/ui/src/workspace-tabs.tsx:53–59`: existing dnd-kit pointer sensor has a 6px activation threshold, excludes touch/modifiers, and only enables AutoScroller. Default injected feedback styles are deliberately avoided for strict CSP. `@dnd-kit/react` and `@dnd-kit/dom` 0.5.0 are already dependencies of UI.
- `docs/frontend.md:717–747`: duplicate views share session data and recipient drafts/files/locks, while retaining independent reading, selection, and caret state. No new session subscription or draft store is needed. Limits remain 32 views and four panes.
- `packages/app/src/session-actions.tsx:180`: the existing “Open in background tab” action reuses views. It is not an equivalent accessible alternative to the requested operation.

### Prior art and approach

`docs/roadmap.md:144–156` and `docs/features.md:706–708` establish tabs, split panes, and duplicate views as shipped behavior. The session-tabs research (`.ai-docs/plans/session-tabs/README.md:349–361`) explains why drag sensors live in UI and why native HTML drag is not the interaction foundation. Reuse the shipped implementation rather than introducing native DataTransfer handling or a second drag library. This research is a source-code review; no live web/desktop drag verification was performed for this plan.

## Interaction design

1. Grab a sidebar session row. Keep normal click, modified link clicks, context menus, and touch scrolling unchanged. Begin dragging only after the existing 6px threshold; suppress native anchor dragging on this custom-drag surface.
2. Keep the original row in place: this is opening another view, not moving a saved session. Show a compact floating tab-shaped preview with the session title and chat icon. Use existing contour, typography, semantic colors, and restrained shadow rather than a new visual style. No permanent grab handle or hover tooltip clutter.
3. Over a valid tab strip, show the existing animated insertion gap. Use a normal tab-sized preview, not the full sidebar-row width. Match the existing 100ms ease-out movement; reduced-motion removes interpolation.
4. Dropping before/between tabs inserts there; dropping in the usable empty stretch appends. Tab-strip overflow auto-scrolls. Header utility buttons, native traffic-light space, and content below the tab strip are not new drop targets for sidebar sources.
5. On successful drop, create and activate a fresh root chat view in the destination pane. Existing tabs, pane selections outside that pane, and sidebar ordering stay unchanged. Focus follows normal desktop chat activation.
6. Escape, pointer cancellation, window blur, source unmount, or release outside a valid strip cancels without navigation, tab creation, storage writes, or session work. Never navigate merely on drag start or hover.
7. Add an explicit **Open in new tab** session action for keyboard/touch/non-drag access. It uses the same always-new operation in the focused pane, adjacent to its selected tab. Existing tab move/pane actions provide subsequent positioning. Keep the background-tab action's behavior unchanged in this scope.

An existing session remains the same conversation in both views. Each new sidebar-created view begins at Root using ordinary new-chat-view reading defaults; it does not copy another view's child-agent or inspector state.

## Implementation design

### 1. Atomic store operation and routing

Add a focused operation such as `SessionTabs.openChatView(runtimeId, rootId, titleHint, { paneId, index })`:

- Validate runtime/root identity, explicit target-pane existence, and the unconditional 32-view limit before mutating anything.
- Allocate a fresh view ID even if the same root is already open or closed.
- Create a root chat descriptor; insert at the bounded target index, select it, focus the target pane, and persist in one store write. Default placement for the menu action is after the selected tab.
- Return the created view/ID; navigate using the existing destination helper so history identifies this exact view. Report routing failure without retrying creation.
- No schema migration, SDK/backend call, session creation, or fork operation.

Avoid an `open` then `transfer` sequence: it would reuse an existing view and/or expose intermediate state. Keep ordinary `open` unchanged.

### 2. Shared drag ownership without application logic in UI

Extract the existing drag provider/controller boundary into a small shared UI workspace-drag scope, mounted by the shell around both sidebar and workspace. Keep standalone WorkspaceTabs/WorkspaceLayout fixtures working with a local provider fallback. Use one active provider for the integrated shell, not nested competing providers.

Allow two explicit drag-source categories: existing workspace view and external item. UI owns opaque source identity, presentation, geometry, and sensors; app owns the external session payload (`runtimeId`, `rootId`, title hint), validity, and resulting store action. Do not import app types into UI or dnd-kit types into the store.

Adapt `useWorkspaceTabDrag` to accept a non-sortable source. External sources have no source-strip gap to close and no source tab to hide. Use target-tab width/default tab width for preview/gap geometry. Return/resolve the newly created view ID for settlement, or remove the preview cleanly if no destination is available; never try to settle onto a nonexistent source view.

Register the current workspace target resolver with the shared scope. Existing tab drags retain pane-content/split targets; external sidebar drags only resolve tab strips. Recheck the destination and capacity at release. Keep existing click suppression and cleanup. Audit AutoScroller when extending provider scope so a tab drag does not accidentally scroll the sidebar.

Desktop needs special validation: the strip's empty stretch is a native window-drag region. While a session drag is active, make only valid destination strip surfaces opt out as necessary; preserve ordinary window dragging and traffic-light controls when idle. Retain native Browser-surface occlusion for the floating preview.

### 3. Thin application integration

- `session-sidebar.tsx`: wrap the link with the shared external-source primitive; use runtime + root identity, not title or root ID alone. Disable custom dragging in compact/touch navigation. Keep virtualized row identity and source-unmount cancellation safe.
- `session-tab-strip.tsx`: handle external drops using the new store operation and existing routing/error feedback, without changing existing view-transfer behavior.
- `session-actions.tsx` / `session-tab-routing.ts`: share the always-new opening path with the accessible menu action.
- `shell.tsx`: widen only the drag scope. Preserve unrelated in-progress changes already present in this file.
- UI changes: `workspace-layout.tsx`, `workspace-tabs.tsx`, `workspace-tab-drag.tsx`, their StyleX files as needed, and a small scope/source module only if extraction requires it. Expose it through the existing workspace subpaths; no new package/dependency.

Disconnected known hosts should retain existing unavailable-session presentation, rather than inventing a network requirement for local tab creation. A removed host or invalidated target during the gesture must fail safely. Titles are plain-text presentation hints, not identity.

## Non-goals

- Dragging into browser/OS tabs, other windows, or detached windows.
- Creating split panes by dragging a sidebar row onto content edges.
- Reordering sidebar sessions, multi-selection, session forking, or modifying daemon work.
- New touch-drag gestures or a general-purpose application drag framework.
- Changing normal sidebar click reuse, independent/shared draft ownership, or tab limits.

## Validation

Store/routing tests (`packages/app/test/session-tabs.test.ts`, `session-tab-routing.test.ts`): always-new IDs in same/different panes and across hosts; existing chat/REPL/trace untouched; root defaults; first/middle/end insertion; selection/focus; exact duplicate URL history; persistence/reload/close/reopen; 32-view rejection despite a matching existing session; stale pane rejection with no partial state.

Component/action tests: menu and drag share opening semantics, click/modifier/action-button behavior remains intact, title fallback, error ownership, no navigation before drop. Extend the nearest existing sidebar/action tests.

UI browser fixtures (`packages/ui/tests/workspace-tabs.mjs`, `workspace-layout.mjs`): external non-sortable source, gap width, overflow auto-scroll, empty strip, zoom/RTL, canceled gesture, source removal, click suppression, reduced motion, keyboard menu equivalent, strict CSP, and existing reorder/split/transfer regressions in Chromium and Firefox.

App browser fixture (`apps/web/scripts/workspace-layout.mjs`): drag a closed session to a chosen slot; drag an already-open session and verify a second view; drop into another pane; verify original view/reading state survives and duplicate views share the existing draft semantics. Assert no session creation/fork request. Verify reload and Browser Back/Forward select the correct duplicate.

Desktop smoke: drag across sidebar/titlebar and empty strip without moving the window, preserve idle window dragging, traffic lights, hidden sidebar, tab overflow, and preview visibility alongside native Browser panes. Inspect screenshots in light/dark themes at normal and increased contrast.

Run focused tests, `npm run test:web`, `npm run check:web`, UI typecheck/tab/layout/CSP tests, and desktop validation for affected integration. No tests have been run for this documentation-only plan.

## Ordered delivery

- [x] Trace current sidebar, store, routing, drag, and split-view boundaries.
- [x] Confirm already-open behavior with the user: always another view.
- [ ] Approve this plan.
- [ ] Add atomic always-new operation plus store/routing tests.
- [ ] Extract shared drag scope, with existing-tab regression coverage first.
- [ ] Integrate sidebar sources, external target handling, preview, and accessible action.
- [ ] Exercise browser and desktop interaction edge cases; review for minimality.
- [ ] Update `docs/frontend.md` with the drag scope and sidebar click-vs-drag contract; add the behavior/code/test row to `docs/features.md`. Add a specific roadmap follow-up only if tracking is needed; do not reinterpret existing completed tab milestones as this feature already shipping.
