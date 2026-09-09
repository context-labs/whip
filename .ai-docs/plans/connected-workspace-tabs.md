# Connected workspace tabs and direct drag feedback

Implemented, 2026-09-08. The original phased plan follows the implementation record.

## Implementation record

- The shared tab shell now joins the content background, with 12px upper corners,
  small lower shoulders, and separators between inactive tabs.
- `packages/ui/src/workspace-tab-drag.tsx` supplies one inert portal preview and
  temporary sibling transforms for standalone and split-pane tabs. It preserves
  the pointer's grab offset and commits the app-owned order only on a valid drop.
  Logical slot measurements account for scrolling, direction, and zoom; cancellation
  and reduced motion clear or settle the temporary presentation.
- `docs/frontend.md` and `packages/ui/README.md` document the implementation.
  No dependencies, daemon/protocol changes, or CSP relaxation were needed.
- Validation passed: UI type checking; app type checking and production renderer
  build; 303 app tests; Chromium and Firefox tab/layout fixtures; all 66 theme Axe
  checks; production session-tab and split-workspace workflows. Coverage includes
  pointer tracking, gaps, cancellation, auto-scroll, RTL, zoom, strict CSP, and
  retained content DOM, drafts, focus, and scroll.
- The staged Electron smoke check passed with an isolated local runtime and URL
  fixture: one moving tab preview, unchanged order before release, committed order
  after release, preserved composer draft, and no renderer errors. The result was
  visually inspected. This is staged host acceptance, not a new installed release.
- Visual evidence is saved in the task's `connected-tabs` artifact directory,
  including theme screenshots and an Electron drag recording. The separate inline
  window-header work was present in the final desktop staging; its changes remain
  independent of this shared tab implementation.

## Outcome

Use the supplied browser-tab reference for geometry: the selected tab joins the
conversation surface, while inactive tabs recede into the strip. During dragging,
the tab follows the pointer from the exact point where it was grabbed, and other
tabs slide aside to show the destination. Apply the same behavior to web and
Electron through the shared renderer.

Retain WHIP's typography, theme palettes, session/host identity, close and menu
controls, constrained tab widths, and existing split-pane workflows. This is an
update to the shared tab component; native window tear-off and mobile touch
dragging are outside this change.

## Baseline before implementation

- `packages/ui/src/workspace-tabs.stylex.ts` centers 36px tabs in a 48px strip.
  Active tabs have a full border and 6px corners, leaving a visible gap above the
  content. The strip draws a continuous bottom border.
- `packages/ui/src/workspace-tabs.tsx` keeps only the `AutoScroller` feedback
  plugin. The standalone strip uses `OptimisticSortingPlugin`, but the shared
  workspace disables it. Neither path renders a tab that follows the pointer.
- `packages/ui/src/workspace-layout.tsx` calculates insertion/split destinations
  and displays a marker. Actual pane/order changes happen on drop.
- `SessionTabs` in app owns persisted order and pane membership. Selected content
  lives in stable sibling DOM nodes so transfers preserve drafts, caret, and scroll.
- The installed dnd-kit 0.5.0 `DragOverlay` relies on `Feedback`; that plugin
  registers runtime CSS with `StyleInjector`. Simply adding the default overlay
  would bypass the deliberate CSP-compatible setup.

## Phase 1 — Connect the active tab to the content

Change `workspace-tabs.stylex.ts` and the minimal state attributes in
`workspace-tabs.tsx` needed to distinguish selected and neighboring tabs.

- Keep the 48px strip and 144–224px width range. Bottom-align the tab shell with
  roughly 6px top clearance; preserve at least 44px targets for coarse pointers.
- Fill the selected shell with `colors.background`, exactly matching the content
  pane. Remove its surrounding pill outline and the visible separator beneath it.
- Use 12px upper corners and small concave lower shoulders, taking their shape
  from the reference. Compile these decorative shapes through StyleX; they have
  no pointer events and do not overlap neighboring controls.
- Keep `surface.navigation` behind inactive tabs. Use restrained hover fills and
  short separators between inactive tabs, suppressing separators beside selection.
- Draw any strip baseline behind the selected shape, so it remains continuous
  with the body without a one-pixel seam. Handle first/last tabs and clipped
  scroller edges explicitly.
- Keep text/status/host metadata and action slots stable on hover and selection.
  Preserve a visible keyboard focus outline independent of selection.

Acceptance: the active tab reads as an extension of its pane in Light, Dark, and
Claude Code themes, including 125%, 150%, and 200% zoom and a split workspace.

## Phase 2 — Make the tab follow the pointer

Add a small shared UI drag-feedback component/helper used by both providers in
`workspace-tabs.tsx` and `workspace-layout.tsx`. Reuse dnd-kit's current sensors,
drag lifecycle, collision handling, and auto-scroll; add no dependency.

- Keep the 6px activation threshold. Clicking a tab still navigates; dragging a
  background tab does not activate it. Menus and close buttons do not start a drag.
- Capture the source rectangle, pointer offset, and visible tab presentation at
  activation. Render one inert, `aria-hidden` presentation in a document portal;
  reuse the tab face without cloning interactive controls, IDs, tooltips, or
  sortable registrations. Preserve the real tab as the focus/drag source.
- Hide the source's visible face while reserving its place. The preview has the
  same dimensions and stays under the grabbed point, without scaling, rotation,
  or spring motion. A faint shadow is appropriate only while it is lifted.
- Within a strip, keep the preview vertically aligned to the row. Once the
  pointer leaves the strip's vertical bounds, allow it to follow both axes for
  existing cross-pane and split drops. Keep the transition continuous.
- Drive preview geometry through a ref and animation-frame updates. Use compiled
  StyleX for appearance and measured inline geometry/native Web Animations for
  movement; no stylesheet injection or CSP changes.

Acceptance: a mid-drag screenshot shows the tab at the pointer, including outside
the source scroller. Moving the pointer within one target slot moves the preview
without waiting for a reorder threshold.

## Phase 3 — Show the insertion gap and preserve workspace state

Give standalone and shared-pane dragging one transient visual placement model.
Replace the standalone optimistic DOM-order mutation with the same visual slot
displacements used in the shared workspace; keep actual tab order owned by app.
This avoids having DOM sorting and workspace insertion logic disagree.

- Measure logical slots independently of animated rectangles. Derive sibling
  offsets and an insertion gap from the proposed destination, excluding the
  dragged face. Animate neighboring tabs over the existing 100ms motion interval.
- Use the measured tab widths, strip direction, scroll offset, and zoom factor.
  Keep auto-scroll working at both strip edges; refresh targets when scrolling,
  resizing, or pane geometry changes, including scrolling while the pointer rests.
- Cross-pane hover shows a gap in the target strip. Content-edge hover continues
  to use the existing split preview and `canDrop` constraints.
- Commit one reorder/transfer on a valid drop. Convert the provisional insertion
  point to `WorkspaceDrop.index` using its existing pre-removal contract, so
  moving right within the same pane cannot produce an off-by-one result.
- Animate the preview into the committed destination over 100–160ms. Cancellation
  returns it to the source. Disable settling/sibling animation for reduced motion;
  direct pointer tracking remains immediate.
- Escape, invalid drops, lost pointer capture, window blur, source removal, and
  unmount must clear every preview/transform without persisting a partial move.
  Suppress the click generated after dragging.
- No pointer-move writes to storage, SDK state, or the persisted pane tree. Keep
  session content mounted in its existing stable layer; do not mount inactive
  conversations to render the preview. An accepted cross-pane drop retains the
  current destination-selection behavior.

## Phase 4 — Verify integration and document the result

Extend the existing tests, rather than adding another drag test harness:

- `packages/ui/tests/workspace-tabs.mjs`: selected-tab/body continuity; pointer
  offset before any reorder; sibling gap movement; both reorder directions;
  Escape/invalid-drop rollback; overflow auto-scroll; RTL; zoom; keyboard and
  modified-link behavior; focus and close/menu controls; reduced motion.
- `packages/ui/tests/workspace-layout.mjs`: moving between panes and splitting;
  one preview across the workspace; repeated drags after pane pruning; no stale
  transforms; cancellation; unchanged content DOM, draft, caret, and scroll.
- `apps/web/scripts/session-tabs.mjs` and `workspace-layout.mjs`: actual route,
  persistence, and selected-view behavior. Add assertions for motion during drag,
  not only final tab order.
- Reuse the theme/Axe checks and strict-CSP checks in Chromium and Firefox.
  Record short drag clips and screenshots in the user's Claude Code theme plus
  light/dark examples; manually inspect the Electron result as well.
- Coordinate with the separate inline-window-header plan if it lands: tab bodies,
  close/menu controls, and previews must remain outside native window-drag regions.
  Do not treat that historical/proposed document as current implementation policy.

Run UI type checks, affected UI/browser suites, app checks, and affected production
browser suites. Update `docs/frontend.md` and the workspace-tabs section of
`packages/ui/README.md` when implementation lands. No daemon/protocol changes or
desktop-specific tab implementation are needed.

## References

- Current architecture: `docs/frontend.md` (canonical).
- [dnd-kit DragOverlay](https://dndkit.com/react/components/drag-overlay/): one
  preview per provider and configurable drop animation.
- [dnd-kit feedback guide](https://dndkit.com/react/guides/feedback/): feedback
  modes, plugin replacement, and double-animation/state-reconciliation pitfalls.
- Installed source was checked as well as documentation: the local
  `@dnd-kit/react/index.js` overlay uses `Feedback`, and
  `@dnd-kit/dom/index.js` registers its runtime CSS through `StyleInjector`.
