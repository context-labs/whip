# Split views and movable workspace tabs

Branch: `whip-rlm` (implemented in the existing checkout)

Status: implemented and verified, 2026-09-07. The user selected a WHIP-owned
layout using existing tabs/dnd-kit and a small resizing primitive. The existing
sidebar/search work remains the baseline.

## Goal

Let people arrange several conversations in nested horizontal and vertical panes,
with a tab strip in each pane and tabs that can move between panes. Match the
interaction in the supplied editor screenshot: two stacked panes beside a third
full-height pane must be a normal layout. Preserve WHIP's drafts, navigation,
themes, and bounded observation. Leave a small, explicit extension point for
future terminal or REPL tabs.

Use **pane** for a rectangular frame containing tabs, **tab/view** for one UI
instance, and **session** for the host-owned chat resource. Splitting a chat view
must not fork its session or start another execution.

## Approved scope and preferences

Confirmed during research:

- Allow the same chat in multiple panes, with independent scroll and agent
  selection.
- Keep all panes docked within one WHIP window for the first release.

The plan also gives each view independent inspector selection. Draft text,
files, and submission locks remain shared for the same runtime/root/recipient;
this state-ownership detail was included in the approved implementation plan.

Implementation defaults:

- Nested splits in either direction, initially up to **four panes** and **32
  total tab instances** per host workspace. Four is a resource limit, not a fixed
  2-by-2 layout. It fits the existing four-root observation budget.
- Chat is the first production tab type. Actual terminal/REPL functionality,
  detached windows, floating panels, cross-window dragging, layout undo/redo,
  multi-host simultaneous attachment, and arbitrary plugins are separate work.
- New-session and Settings remain existing full-canvas routes. Visiting them
  retains workspace metadata and releases chat observation; returning restores
  the layout. This change does not redesign those forms.

## Library research and recommendation

Checked official documentation and the current npm metadata on 2026-09-07.
Inspected the published FlexLayout JavaScript, declarations, CSS, and package
manifest in a temporary directory. The final implementation installs only the
small resizing primitive; the docking frameworks below were not adopted.

| Option | What it supplies | Assessment for WHIP |
| --- | --- | --- |
| **FlexLayout / `flexlayout-react` 0.10.8** | React docking, nested tabsets, dragging, a serializable model, custom tab rendering, visibility events, keyboard tab navigation and resizing | Initial candidate, subsequently declined. MIT; React 18/19 peers; no additional runtime dependency declared. The published implementation preserves keyed panel content during moves. |
| **Dockview / `dockview-react` 8.2.0** | Docking, groups, layouts, custom renderers and React 19 support | Strong alternative. Core is MIT, but current built-in spatial keyboard navigation and keyboard docking are Enterprise features. Core focus navigation and ARIA support remain free. |
| **`react-resizable-panels` 4.12.4 + existing dnd-kit** | Accessible resizing primitives plus WHIP's existing drag toolkit | Selected for the confirmed in-window scope. WHIP owns drop targets, tab transfer, tree edits, empty-pane removal and restoration; the dependency supplies resizing only. |

Sources: [FlexLayout](https://github.com/caplin/FlexLayout),
[Dockview licensing matrix](https://dockview.dev/docs/overview/licence/),
[Dockview repository](https://github.com/dockview/dockview), and
[react-resizable-panels](https://github.com/bvaughn/react-resizable-panels).
Version metadata came from each package's public npm registry entry.

### Approved approach

Use a WHIP-owned immutable binary tree of splits and panes. Reuse existing
Base UI/StyleX tab chrome and dnd-kit with one workspace drag provider.
`react-resizable-panels` 4.12.4 supplies only resizing and keyboard separators;
no docking framework or vendor stylesheet is used. Library compatibility must
pass the existing production CSP. This supersedes the initial FlexLayout
recommendation above: our confirmed in-window scope and existing tab components
make owning the small layout model a better fit.

App owns atomic split/move/close/resize operations and bounded persistence. UI
owns geometry, drop targets and feedback. Keep selected view DOM in a flat keyed
layer over measured pane slots so moving between recursive layout branches does
not remount the conversation. Hidden tabs retain metadata and work state, not
mounted transcripts.

Implementation checks: three-pane fixture, cross-pane drag/cancel, native links,
keyboard resizing and move menus, stateful content preservation, themes/CSP,
subscription gating and measured production bundle delta.

## Architecture findings

The canonical [frontend guide](../../../docs/frontend.md) remains authoritative.
The roadmap has shipped single-strip tabs but no existing docking implementation.

| Starting implementation | Required change |
| --- | --- |
| `packages/app/src/session-tabs.ts`: identity and deduplication use `rootId`; one ordered list per host | Add stable view-instance IDs and pane ownership. A chat resource may have multiple views. |
| `session-tab-routing.ts`: URL drives the one active tab | The URL should describe the focused view; other panes retain their own locations. |
| `shell.tsx`: one tab strip, one `whip-session-panel`, a document-wide composer selector | Render one strip per pane; scope IDs, focus, shortcuts, and commands to the focused view. |
| `conversation.tsx`: route navigation and async completion checks refer to global router location | Separate the route adapter from pane content; use the originating view's identity and location revision for late results. |
| `runtime.ts: acquireView`: shares roots and refuses a fifth retained root when all four are leased | Reconcile visible roots as a set, releasing obsolete leases before admitting replacements. |
| `conversation.tsx` calls `openAgent/closeAgent`; SDK tracks opened children in a Set | Reference-count app consumers. One duplicate view must not close another's history. |
| `runtime.ts: setDraft` emits app updates only when draft presence changes | Add recipient-scoped draft notifications so duplicate composers do not show stale text. |
| `timeline.tsx` bookmarks and composer selection use recipient identity | Add view identity to reading/caret state while retaining shared draft/resource ownership. |

### State and package ownership

- Extend the existing runtime-owned tab workspace into a `WorkspaceLayout` store;
  retire the old flat-list authority in the same migration. Keep one immutable
  layout tree per retained host workspace. Its panes own tab ordering and
  selection; split nodes own direction and ratio; the workspace owns focus.
- Keep app-facing snapshots immutable and stable between changes. They are
  projections of that tree for React, routing, sidebar, and summaries. No
  parallel layout authority is retained.
- Use a typed chat descriptor in each pane, conceptually
  `{ id: viewId, kind: 'chat', runtimeId, rootId, location: { agent?, panel? } }`.
  Tab IDs identify views independently from root IDs; the migrated primary view
  retains its root ID as its initial ID for compatibility, and duplicates use UUIDs. A later tab kind adds its own descriptor
  and renderer; do not invent terminal protocol shapes now.
- `packages/ui` owns a thin `workspace-layout` subpath: split geometry,
  resize controls, drop targets, and shared dragging for existing tabs. The
  tree type contains only layout data; app supplies rendered content and headers.
- `packages/app` owns descriptors, navigation, limits, persistence, rendering
  chat content, visibility, SDK leases, and user commands. SDK state remains in
  SDK views; no new Query client, copied transcript store, or daemon connection.
- Preserve SDK/protocol/platform interfaces. Child consumer accounting can live
  alongside app root leases; no backend change is needed for this phase.

## Interaction contract

- Each pane has its own horizontal tabs, actions menu, and quiet active-pane
  indicator. Global connection, sidebar toggle, search, and application controls
  appear once in the shell, outside the pane tab strips. One global picker lists
  every open view with its pane and supports searching long/overflowed tab lists.
- **Split right** and **Split down** create another view of the current chat and
  select it in the new pane. Left/up placement is available through edge drops; movement menus offer
  right/down splits and existing panes. All directions use the same nested layout model.
- Dragging within a strip reorders. Dropping on another strip/center moves the
  same view. An edge drop creates a pane. Preview the destination; Escape cancels
  without changing the layout. Moving a view never copies it implicitly.
- Menus provide **Move to pane**, **Move to new split**, and **Split view** so
  dragging is optional. Compute the resulting pane count: moving the last tab
  out of one pane can replace that pane without exceeding the cap.
- Removing the final tab collapses that pane and redistributes space. Closing a
  tab preserves the session and its work. Reopen restores the view to its prior
  pane if it survives, otherwise to the focused pane. Keep one empty workspace
  placeholder when no tabs remain.
- Ordinary sidebar/search opening first reuses a matching view in the focused
  pane, then a matching existing view elsewhere, otherwise opens in the focused
  pane. Explicit split/open-in-another-pane commands create duplicates. Prefer the selected matching view in the focused pane, then model order as fallback.
- Open in background tab adds an inactive tab in the focused pane. It does not
  mount a conversation, change pane focus, or acquire a root subscription.
- Tab labels/attention are derived from existing batched summaries, deduplicated
  by root across all panes. Duplicate views share session title/status and have
  accessible labels identifying their pane and selected agent.
- Session deletion updates all views of that resource consistently. A failed or
  uncertain deletion does not silently close them. Clear/rewind invalidates all
  affected bookmarks while each pane renders the same authoritative revision.

## Routing and focused-pane behavior

Keep `/h/$runtimeId/s/$rootId` and validated `agent`/`panel` search parameters as
the shareable conversation location. Layout geometry is window-local metadata.

- Record the target view ID as a validated, window-local browser-history state
  hint. It is not necessary in shared URLs. Back/Forward restores that view if
  it exists; otherwise use the normal open/reuse policy for the URL.
- Treat selecting a tab, activating another pane, or changing agents as explicit
  navigation. Produce one history entry per completed user action, even when
  focus and selection events both fire. Retain existing replace semantics for
  inspector changes. Resizing, dragging the same focused tab, title updates,
  polling, and model hydration do not push navigation entries.
- A matching view ID must also belong to the current host and requested root.
  A copied deep link without local layout works in a single pane. Foreign-host
  URLs retain the current host-mismatch behavior and send no requests there.
- Refactor `ConversationRoute` into a small URL adapter plus `ConversationView`
  with explicit identity, location, view-scoped navigation, and focus handles.
  Mount one content surface per visible selected tab, not multiple Router Outlets.
- Async fork/create/history actions capture originating view ID, client epoch,
  and location revision. They can update that still-matching view after success
  without taking focus from another pane; closing or navigating away invalidates
  the target. Preserve command notices and uncertain-delivery handling.
- Existing New-session directory prefills keep priority over startup restoration.
  Successful explicit creation adds/selects a tab in the remembered focused pane.

## Lifetimes, drafts, and observation

- A workspace-level reconciler owns the visible root leases, deduplicated by
  runtime/root. Release roots absent from the new visible set before acquiring
  new ones; handle a switch with four occupied panes without a transient fifth
  lease or a render-time exception. Retain the existing four-root/30-second cache
  policy and never evict a root another visible pane still needs.
- Reference-count visible child-history consumers per SessionView/agent, share
  initial loading, and call `closeAgent` only after the last consumer releases.
  Include inspector/history-open call sites in this audit. Preserve retry and
  host-epoch guards so a stale completion cannot remove a newer consumer.
- Keep view wrappers in a flat keyed layer across pane moves. Mount heavy
  conversation content only while selected and actually visible; a hidden or
  maximized-away tab must not retain a lease merely because it is mounted.
- Preserve drafts, attachments, upload state, acceptance locks, and pending
  commands outside pane components. For duplicate recipients, add a narrow
  subscription over the existing draft owner and read current text at submission.
  Acceptance clears only matching submitted text/files. Shared locks prevent two
  panes from concurrently admitting the same composer submission.
- Store scroll/follow bookmarks and composer caret selections by view plus
  recipient, retaining current bounds. Keep text selection stable while moving
  or resizing. Remounting hidden content restores bookmarks without fetching
  unlimited history. A duplicate initially starts at the source reading position
  and subsequently moves independently.
- Session summaries remain one batched, bounded query across unique open roots.
  Sidebar/search still use lightweight catalogs. One host and one Query client
  remain attached; pane closure never cancels host execution.

## Persistence and responsive layout

- Migrate `whip.web.tabs.v1` to a versioned workspace entry. Each old host list
  becomes one pane, preserving order, selected root, locations, and closed tabs.
  Validate the migrated result before writing; preserve the old entry if migration
  or storage fails, and use memory with an honest notice.
- Persist only a validated WHIP layout JSON: pane/tree IDs, weights,
  selection, known tab descriptors and title hints. Restore only recognized node
  types; reject unknown executable component/config data, invalid weights,
  duplicate IDs, foreign-host descriptors, and oversized/deep trees.
- Preserve bounds of 32 open views, 20 closed entries, four host layouts, and
  64 KiB total layout metadata. Bound/normalize tree depth to the four-pane model.
  Drop closed metadata and oldest inactive host layouts first under byte pressure;
  never silently discard open views from the current workspace.
- Save committed layout changes, not every pointer movement. Keep snapshots and
  resize feedback live while persistence waits for the gesture to finish.
- Start with a 320 px minimum pane width and 240 px minimum height. Disable split
  destinations that cannot fit. Assess available **workspace** size after the
  resizable sidebar, rather than relying only on window width.
- If the saved layout cannot fit, temporarily show the focused pane with the global open-tab picker; mobile uses
  the existing selector/Sheet. Preserve its full desktop tree and weights; do not
  flatten the saved layout. Restore when space permits. Below 768 px always use
  this single-pane presentation and retain 44 px touch controls.

## Styling and accessibility

Follow the current restrained tabs, Inter typography, Lucide icons, and all 65
semantic themes. All authored visuals use StyleX and shared tokens. The resizing
primitive supplies geometry without introducing vendor theme CSS or another
component styling system.

Retain real anchors for session navigation and separate close/menu controls.
Scope DOM IDs and focus refs to each view. Application shortcuts operate on the
focused pane; browser shortcuts and textarea editing remain native. Preserve
Base UI menus/dialogs and focus return to the originating pane, with a nearest
surviving-pane fallback after closure.

Verify keyboard tab activation, splitter arrow resizing, focus cycling, move/split
menu alternatives, drag cancellation, and reduced motion. Audit separator names,
controlled pane relationships, ranges and focus against the
[WAI-ARIA splitter guidance](https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/).
The resize primitive supplies separator interaction; WHIP supplies meaningful
labels and controlled pane relationships and validates the complete result. Do not claim a
library's accessibility feature list as screen-reader acceptance.

## Ordered implementation plan

1. **Integration proof.** Pin the resizing primitive in the UI boundary and prove nested
   docking, lifecycle, link semantics, styling/CSP, and keyboard interaction in
   isolated fixtures. Record results before broad adoption.
2. **Workspace model and migration.** Evolve `session-tabs.ts` into the one
   workspace owner, implement descriptors/limits/tree operations, immutable
   projections, versioned validation, and one-pane migration. Preserve the old
   behavior while rendering one pane.
3. **Conversation independence.** Refactor `conversation.tsx`, root/child lease
   coordination in `runtime.ts`, draft subscriptions in `composer.tsx`, and
   view-specific selection/bookmarks in `compositions.ts`/`reading-positions.ts`/
   `timeline.tsx`. Remove global-focus and global-route assumptions.
4. **Pane UI and routing.** Add the generic UI `workspace-layout` entry and app
   workspace host, replace the global strip in `shell.tsx`, and adapt
   `session-tab-strip.tsx`/`session-tab-routing.ts`, native session links, sidebar,
   search, creation, and command actions. Extract/reuse tab labels and menus;
   remove obsolete app reorder wiring once docking owns it.
5. **Responsive and accessible behavior.** Add available-size presentation,
   focus/move menus, separator validation, bounds and empty/loading/error states,
   all-theme styles, and mobile selector integration.
6. **Acceptance and documentation.** Add focused model/lease/router tests and a
   production `apps/web/scripts/workspace-layout.mjs` fixture. Update
   `docs/frontend.md`, `docs/features.md`, `docs/roadmap.md`, and UI/app README
   references. Record actual browser/manual coverage and any remaining limits.

No Go, backend protocol, SDK public interface, or platform additions are expected.
Follow source evidence if the integration proof shows otherwise; do not hide a
cross-package change inside a UI refactor.

## Validation and acceptance

- Model: nested layouts, all four edge drops, transfer/reorder, empty-pane
  collapse, close/reopen, duplicate chat identity, 32-view/four-pane admission,
  migration, malformed/oversized storage, storage denial, and host eviction.
- Navigation: focused-pane URL, same-URL duplicate views, Back/Forward history
  hints, deep links without saved state, child/inspector locations, foreign hosts,
  modified links, background tabs, directory creation, and late async results.
- Lifetimes: two panes on one child; close either without losing the other;
  four distinct roots then switch one; moving a visible view preserves its lease;
  32 tabs open with only visible roots observed; hidden tabs and host changes
  clean up; summaries deduplicate roots; disconnect does not stop accepted work.
- Work preservation: independent reading positions and caret selections; shared
  draft edits and attachments; duplicate submit locking; edits after acceptance;
  uncertain delivery; scroll anchoring through drag, resize, streaming and paging.
- UI: screenshot's three-pane arrangement, two/four panes, long titles, overflow,
  all themes and custom/automatic appearance, sidebar at 320/420 px, narrow panes,
  mobile transitions, keyboard-only operation, focus restoration, Axe and CSP.
- Run `npm run check:web`, `npm run test:web`, affected UI type/component/tab/CSP
  checks, `npm run pack:web`, then existing production `browser.mjs`,
  `session-tabs.mjs`, `sidebar.mjs`, `session-search.mjs`, the new workspace
  fixture, and relevant performance scenarios with isolated fake providers.
  Record Chromium/Firefox and actual Safari results separately; include manual
  VoiceOver and physical touch checks for the new drag/keyboard interactions.
- Confirm future extensibility with a second **fixture-only** tab renderer that
  moves/resizes/persists without any chat-specific assumptions. Do not ship a
  fake terminal or REPL to satisfy this test.

Acceptance: a person can create the supplied nested layout, drag tabs between its
panes, navigate and compose independently, then reload and recover the layout,
without losing work, targeting the wrong session, or opening background root
subscriptions. The layout must preserve WHIP's theme and CSP conventions.

## Delivery checklist

- [x] Research, preferences and custom-layout approval.
- [x] Immutable tree, bounded migration and model tests.
- [x] Shared drag/resize UI and stable content identity.
- [x] Conversation leases, draft subscriptions and view-scoped navigation.
- [x] Integrated workspace and responsive menus.
- [x] Browser/CSP/theme regression checks and adversarial review.
- [x] Canonical documentation and validation evidence.


## Implementation findings

- The app tree is the only layout authority. `WorkspaceLayout` receives generic
  content and places stable, sorted siblings over measured pane slots. Stable
  keys alone were insufficient: changing DOM order also reset scroll during a
  transfer, so view siblings retain sorted order across moves.
- The route placeholder no longer acquires a root. Route resolution happens after
  a React commit; a competing route lease could exceed the four-root cap before
  workspace cleanup. Browser acceptance now navigates to a session absent from
  the tab catalog while four distinct roots are visible.
- Draft notifications are recipient-scoped, including explicit discard. Child
  history consumers are reference-counted with same-commit release/reacquire
  coalescing. View bookmarks are discarded only when their view leaves both open
  tabs and bounded closed history.
- One resize dependency was added: `react-resizable-panels` 4.12.4. With cursor
  injection disabled it still allocates an empty adopted stylesheet. It inserts
  no rules, and the strict CSP remains unchanged. Tests assert both facts.
- Narrow composer controls wrap. Very short pane content can scroll, keeping
  notices and Send reachable. Generic UI minimums remain 320 × 240 px.
- Review fixed lost duplicate-view identity in inspector commands, selected-view
  reuse in sidebar/search, misleading self-only edge previews, and a competing
  route lease. Split checks use unscaled client dimensions for CSS zoom.

## Validation evidence — 2026-09-07

[acceptance.json](acceptance.json) records the completed checks. App types,
production compilation/packing, all **141 app tests**, UI types/unit tests,
Storybook build and isolated packed-package production/development rendering pass.
The final `task check` also passes (Go checks, SDK gates, frontend checks, UI
tests and asset packaging tests).

- The production workspace suite passes **seven workflows in Chromium and
  Firefox**: duplicates/shared drafts/independent scrolling; same-URL history and
  separate agents; nested moves and source-pane pruning; drag/cancel/re-drag;
  keyboard resize/pane focus/composer shortcut; mobile/420 px sidebar/restoration;
  minimum-size active composers; and new-root admission at the four-root limit.
  The fixture groups related assertions into seven reported workflow entries.
- Existing production browser, session-tab, sidebar and search suites pass in
  both engines. The large-history performance suite passes in Chromium.
- UI fixtures pass strict CSP, Axe, nested pointer/keyboard resizing, mixed
  renderers, selected DOM/focus/scroll retention, and drag after menu transfer.
  The existing tab suite also passes across all 65 themes.
- Inspected actual light/dark nested layouts, mobile presentation, 420 px sidebar
  and minimum-size active panes. Screenshots are retained under
  `/tmp/whip-workspace-layout-results`.
- A repeated-drag test initially raced dnd-kit's asynchronous Escape cleanup.
  Tracing confirmed the handler was registered and cancellation was still active.
  The test now waits for drag-source removal before starting the next gesture.
  No registry workaround or fixed sleep was added.

Actual Safari split interactions, VoiceOver and physical-device touch were not
exercised in this run; these remain release acceptance work. The feature does not
claim detached windows, production terminal/REPL tabs or cross-window dragging.

