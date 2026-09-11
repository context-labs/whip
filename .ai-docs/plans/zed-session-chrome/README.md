# Zed-inspired session tabs and information bar

Branch: `feature/agent-definition` (existing checkout).

Status: implemented and validated, 2026-09-11. See the delivery log below.

## Follow-up: restore original tab shape

The user requested the original tab contours and radii exactly, with everything
else unchanged. Restored the original SVG paths, rounded corners, shoulders,
spacing, tab heights, separators and matching drag preview from the pre-change
implementation. The session information bar, REPL adjacency, metadata and other
behavior remain unchanged. This supersedes the flat-tab visual direction below.
Validation: original geometry matched against the pre-change source; type checking,
Chromium/Firefox tab interaction checks and Axe across all 66 themes passed.

## Goal

Give Whip the compact, continuous workspace chrome shown in the user's Zed screenshot: a row of flat tabs above a quiet information and action bar. Make the current project, execution host, selected agent, and activity easy to identify without interrupting conversation reading.

The accepted scope is **desktop and web**, with **host/project, selected agent, and status** as the primary information. Model and permission controls stay beside the composer. Clicking **Open REPL** opens and selects a new REPL tab immediately to the right of the source tab in the same pane, preserving the chat tab. Terminal UI changes are outside this implementation.

## Reference and current implementation

The reference is the user-supplied `Screenshot 2026-09-11 at 12.09.34 AM.png`. The relevant qualities are rectangular adjacent tabs, thin vertical separators, muted inactive titles, an active tab continuous with the content surface, and a second row with contextual information on the left and small actions on the right. The screenshot's editor contents are not requirements and are not copied into this plan.

Whip already ships session tabs, split panes, restoration, close/reopen, overflow selection, and drag reordering. This change refines their visual hierarchy and changes explicit REPL opening from an in-place mode switch to a new adjacent view.

| Existing source | What it establishes |
| --- | --- |
| [Frontend guide](../../../docs/frontend.md#product-intent-and-visual-philosophy) | Reading-first design, theme-owned colors, visible identity, shared desktop/web renderer |
| [Workspace ownership](../../../docs/frontend.md#split-workspace) | Per-pane selected views, shared root observations, independent child/reading locations |
| [Roadmap](../../../docs/roadmap.md#whip-client-foundation) and [feature map](../../../docs/features.md#react-web-application) | Tabs, splits, New Chat, REPL and activity already ship |
| [Tab styles](../../../packages/ui/src/workspace-tabs.stylex.ts), lines 6–25 | Current 48px strip, 42px tabs, spacing, title and metadata layout |
| [Tab rendering](../../../packages/ui/src/workspace-tabs.tsx), lines 155–185 | Curved SVG shoulders shared by live tabs and the drag preview |
| [Session tab controller](../../../packages/app/src/session-tab-strip.tsx) | Runtime identity, summaries, navigation, view-mode actions, close/reopen and splits |
| [Tab state](../../../packages/app/src/session-tabs.ts) and [routing](../../../packages/app/src/session-tab-routing.ts) | Existing `chat`/`repl` view descriptors, unique view IDs and URL reconciliation; ordinary `open()` reuses a root view and cannot implement the new-tab action unchanged |
| [Session content](../../../packages/app/src/conversation.tsx) | Chat/REPL composition, child notice, inspector, requests and composer |
| [Chat activity](../../../packages/app/src/chat-activity.tsx) | Existing current-activity projection and named child rows |
| [REPL viewer](../../../packages/app/src/repl-view.tsx) | Existing agent selector, language, loaded-cell count and history help |
| [Session controls](../../../packages/app/src/details/session-controls.tsx) | Workspace/context detail reads and qualified usage accounting |

The frontend guide is the implementation authority, subject to the user's accepted changes. Older plans and harness research provide context, not additional requirements. Update the guide's rounded-tab treatment and in-place Open REPL descriptions when implementing this plan.

## Design direction

The person using this workspace is directing several coding sessions and recursive agents. They need to switch work, know where it is executing, and recognize when intervention is needed. The chrome should feel like a compact editor: steady, restrained, and precise.

- **Domain:** sessions, projects, execution hosts, root/child agents, conversations, REPL evidence, pending decisions.
- **Color world:** editor canvas, recessed navigation gray, primary text, secondary text, quiet divider, activity and attention colors. Resolve all through Whip's existing theme roles; the reference's dark palette is not a new hardcoded theme.
- **Distinctive element:** a per-view identity trail from host/project to the selected recursive agent, paired with Chat/REPL mode and the session's real activity.
- **Design choices:** flat adjacent tabs instead of curved shoulders; a single contextual row instead of a large session heading; short operational status instead of a dashboard of cost and token metrics.

### Layout

```text
┌─────────────────────┬──────────────────────┬─────────────────┬──────────┐
│ ● Debug questions × │   Invoice report      │   New Chat      │ +  ▾     │
├─────────────────────┴──────────────────────┴─────────────────┴──────────┤
│ Local / whip / Root ▾         Working…        Open REPL  Details  ⋯    │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│                         Conversation                                   │
│                                                                        │
│                  Child activity / pending questions                    │
│                  Composer + model / permission controls                │
└────────────────────────────────────────────────────────────────────────┘
```

The sketch shows hierarchy, not final spacing. Each split pane gets its own tabs and information bar, scoped to its selected view. A child selection reads, for example, `Remote / whip / harness-research`; selecting a child never renames the root's session tab.

### 1. Flat tab strip

- Remove curved shoulders and the inset gaps between tabs. Use square corners, quiet full-height separators and a baseline beneath inactive tabs. The active tab shares the information bar's background and opens into it.
- Use one line for session title and existing activity/draft indicators. Keep widths bounded and horizontally scrollable; long titles ellipsize without pushing out controls.
- Put the selected host prominently in the information bar. Retain concise host disambiguation for tabs when several hosts are present, plus full identity in accessible labels and tooltips.
- Keep active close controls visible, with existing hover/focus/touch access on other tabs. Keep close/menu controls separate from the tab link.
- Preserve current overflow picker, new-tab action, context menus, drag/reorder, split actions, native link behavior and keyboard shortcuts. Use the same flat geometry in the drag preview.
- Retain the current 48px outer strip for the first pass to preserve native traffic-light positioning and window dragging. Remove internal top spacing so tabs occupy the row. Tune density only after checking the real macOS inset window; do not change native window chrome as an incidental CSS adjustment.
- Zed's back/forward arrows are not required for the first pass. Existing browser/navigation behavior remains available; visible history buttons can be a separate deliberate addition.

### 2. Session information bar

Add a fixed, non-scrolling row immediately below each pane's tabs, approximately 36px at default desktop text size, growing for accessibility and touch targets.

| Position | Content and behavior |
| --- | --- |
| Left | Host / project folder / selected agent. Show the folder basename, with the full host path in a focus-accessible tooltip. Agent control opens the existing agent details section; child views have an explicit path back to Root. |
| Flexible middle | Current activity from the existing projection: working, queued, awaiting answer/approval, reconnecting, or the actual last-turn outcome. Long operation names truncate. Blank/idle must not be presented as successful completion. |
| Right | Open REPL action in chat, a REPL mode label in REPL views, Session details, and the existing session-actions menu. Use tooltips and accessible names for icon-only controls. |

Use the existing inspector routing and the adjacent-tab behavior below. A toolbar action targets its originating view and pane explicitly; another pane must never receive it because the URL or focus changed meanwhile.

Move the existing **current status row** from `ChatActivity` into the information bar so there is one live announcement. Keep named child activity and actionable questions/permissions near the composer. Preserve elapsed-time and reduced-motion controls when moving that status. Detailed error messages remain in their canonical notices; the bar carries only concise state and navigation to its owner.

Replace the standalone child identity notice with this trail. For REPL, consolidate its agent identity into the shared bar while keeping REPL-specific language, loaded-cell count, paging and history help in a compact local toolbar. Preserve access to agents omitted from the initial snapshot; do not turn a bounded agent list into a silently complete selector.

### 3. Open REPL in an adjacent tab

```text
Before: [ Debug questions · Chat ] [ Invoice report ]
Click:    Open REPL
After:  [ Debug questions · Chat ] [ Debug questions · REPL* ] [ Invoice report ]
                                                         * selected
```

- Create a new view ID with `kind: 'repl'`, the source runtime/root identity and selected agent. Insert it immediately after the source tab in that source's pane; do not append it at the end, open a split, or convert the chat tab.
- Select the new tab, scroll it into view and focus its content panel. Keep the original chat's title, agent, draft, attachments, caret and reading position intact. The REPL has its own reading bookmark and no composer.
- Label the new tab with the session title plus a visible **REPL** marker. It observes the same daemon session through the existing shared root view; opening it creates no new agent/session and executes no code.
- Use this behavior for the information-bar action, tab-menu **Open REPL**, and conversation **Open in REPL** actions. All call one shared app helper. Selecting an existing REPL tab, restoring a saved tab or handling its view-ID-bearing history entry must not create another tab.
- Each explicit Open REPL action creates a fresh adjacent view, including when another REPL view is already open. Do not silently move or reuse another pane's REPL. Ordinary tab selection is how users return to an existing view.
- Return to chat through its preserved tab. The existing REPL-menu **Open chat** action should select a matching chat in the same pane (same runtime/root/agent, nearest to the REPL, preferring the left on a tie), or create a chat view immediately to its right if none remains. It must not convert the REPL tab.
- Close, reopen, drag, split, reload and Back/Forward use existing view IDs and layout persistence. The REPL and chat remain independently movable/closable; neither closes the other. No persisted source-tab relationship is needed.
- Enforce the existing 32-view limit for a **new view**, even when its root is already open. At capacity, leave the source and selection unchanged and show the existing workspace capacity error. Never fall back to replacing the chat.
- Validate the source at action time. If it moved, insert next to its current position; if it closed or is not a session-backed view, report the action failure without opening an unrelated tab. Initial REPL inspector state is closed. Navigation failures use existing workspace error handling and cannot destroy the source view.

### 4. Responsive and exceptional states

- **Narrow split panes:** preserve selected-agent identity, an attention/status indicator and one actions menu; shorten project text and move secondary controls into that menu. Full host/path and status remain accessible by focus/click, not only hover.
- **Phone layout:** keep the existing session picker instead of forcing a horizontal desktop strip. The information row simplifies and has at least 44px touch targets.
- **New Chat:** show selected host/folder or `Choose a project`, and `Not started`. Reuse Welcome's setup controls. Do not create a session merely to populate the bar. Hide session-only actions until promotion to a real session.
- **Loading/offline:** retain trustworthy saved tab identity, mark unavailable live data, and disable commands requiring the host. Never show the last running status as live after disconnect.
- **Long names, missing/deleted agent, restored tabs:** preserve root/view identity and a clear fallback. No unbounded metadata fetches or assumptions that names are unique.
- **Zoom, themes, motion:** support UI font scaling, light/dark and custom themes, increased contrast, reduced motion and keyboard focus. Use a minimum height, not clipped fixed text geometry.

## Data and ownership

Use the selected `SessionView`, `useSelectedAgent`, existing runtime host names, and `SessionTabs`. The bar does not create a second session subscription, polling loop, execution reducer, persistent store, or dependency.

- Project identity starts with the existing snapshot cwd. Inspect selected-agent cwd semantics before labeling it as the child's actual working directory; never substitute the root path without making its scope clear.
- Reuse `activityStatus` and existing execution projections. Preserve the distinction between selected-agent activity and session-wide requests or working descendants.
- Keep status announcements in one live region. Do not announce every streaming token or replay a status merely because a pane receives focus.
- Keep model and permissions beside the composer. Any future top-bar placement must use the existing model/provider/effort data and picker behavior; avoid two independent mutation controls.
- Context inspection currently uses explicit detail queries. Do not infer a context-window percentage from cumulative token spend or issue recurring context audits to fill the bar. A requested live context gauge requires a separate check of authoritative, scoped data support.
- Cost can be sourced from existing accounting if requested, but must distinguish reported, estimated, unknown and incomplete amounts, and label session-tree versus own-agent scope. Missing cost is not $0.
- Git branch is not established as a live field by the inspected header sources. If requested, verify the host contract first; a live branch display may need a bounded host capability and refresh policy. Never inspect the viewing machine's Git checkout for a remote session.

The adjacent REPL tab uses the existing `SessionTabs` descriptors and v3 persistence. Extend that owner with a bounded adjacent-view insertion operation and route using the new `whipViewId`. Do not use ordinary root-reusing `open()` or navigate to `view=repl` with the source chat's ID: the current `visit()` path would convert that chat in place. No backend or storage-schema changes are expected.

## Implementation sequence

1. **Validate composition.** Prepare the flat strip and information bar in a component fixture using realistic long titles, adjacent Chat/REPL views, multiple hosts, child selection, activity, and an offline split. Review light/dark and narrow layouts against the accepted scope.
2. **Restyle the existing tabs.** Update `packages/ui/src/workspace-tabs.stylex.ts` and the shared shape/preview in `workspace-tabs.tsx`. Remove obsolete curve markup/styles. Adjust existing tab fixtures and assertions that intentionally verify the old contour; preserve interaction assertions.
3. **Implement adjacent REPL opening.** Extend `packages/app/src/session-tabs.ts` with atomic adjacent-view insertion and add a shared action helper in `session-tab-routing.ts`. Update tab-menu and conversation REPL entry points in `session-tab-strip.tsx` and `conversation.tsx`. Add model/routing tests for exact insertion, fresh identity, same-agent scope, repeated explicit opens, capacity and source changes.
4. **Add the app information bar.** Introduce `packages/app/src/session-info-bar.tsx` as an app component with explicit scoped props and callbacks. Compose it in `conversation.tsx`; use the shared adjacent-REPL action and existing session actions. Add the simple draft counterpart through `welcome.tsx` if needed. Promote a generic primitive to UI only if it has a concrete second use.
5. **Consolidate existing chrome.** Move current activity presentation out of `chat-activity.tsx` while keeping its child rows; remove the duplicate child notice. Reconcile REPL identity controls in `repl-view.tsx`. Preserve request handling, error ownership, focus return and independent reading positions.
6. **Verify and document.** Add focused bar tests, extend tab/split/REPL browser fixtures, inspect native inset geometry, and update the frontend guide and feature map, including the former in-place REPL behavior. Record scope changes and measured visual acceptance here.

## Validation and acceptance

- Selecting a tab updates its own bar; duplicated root views with different children/modes remain independent.
- Every Open REPL entry point creates and selects a fresh view immediately to the right of its source in the same pane, with the same runtime/root/agent and a distinct view ID. The original chat remains a chat with its draft and reading position preserved.
- Test opening from the middle/last tab, a background tab's menu, a child conversation, two split panes showing the same root, repeated explicit opens, an existing REPL elsewhere, and the 32-view limit. A failed open never converts or closes the source.
- Selecting existing views, Back/Forward, close/reopen and reload do not allocate extra views. Moving tabs and resizing splits preserve drafts, caret, independent reading positions and restored layout. Closing either view never cancels host work or closes the other.
- Status follows existing source truth through pending question/approval, working, idle, failed/cancelled/interrupted and disconnect/reconnect. No duplicate current-status announcement is rendered above and below the conversation.
- Every visible toolbar control works through the existing action path. Full error and request interfaces stay reachable. New Chat cannot expose actions for an uncreated session.
- Long labels fit at 320px pane width, the existing phone breakpoint, increased text size and browser zoom. Theme contrast and reduced-motion behavior hold.
- Native traffic lights, empty-strip window dragging, tab dragging, context menus, close buttons and split targets remain separate usable hit regions.
- The bar adds no background root leases or polling. Existing four-pane/32-view bounds and CSP protections remain intact.

Checks during implementation: `npm run check:web`, `npm run test:web`, affected `@whip/ui` checks and `test:tabs`/`test:layout`; then packed-app session-tabs/workspace-layout/REPL browser workflows. Extend `session-tabs.test.ts`, `session-tab-routing.test.ts`, and the existing chat-activity and REPL tests for adjacent opening and moved controls. Review visual diffs before accepting baselines. Perform a real desktop smoke test for native window chrome, not just browser screenshots.

The canonical frontend guide, app/UI references, feature map and roadmap are updated with the implementation.

## Scope boundary

This plan covers desktop/web tabs, the session information bar, and adjacent REPL opening. It does not add editor/file tabs, terminal tabs, Git controls, always-visible cost/context gauges, new split persistence, or a replacement navigation system. There are no outstanding preference questions for this scope.


## Delivery log — 2026-09-11

- [x] Reuse the existing tab geometry and interaction owners; remove SVG curves,
  add square adjacent separators and preserve the 48px native strip.
- [x] Add the shared app information bar for chat, REPL, New Chat and unavailable
  views; retain one current activity announcement, motion control and elapsed time.
- [x] Consolidate agent selection into the existing paginated inspector. Keep
  questions, approvals, child activity and composer controls in their current owners.
- [x] Add atomic adjacent opening, fresh REPL IDs, nearest same-agent chat lookup,
  explicit history restoration, capacity/source checks and shared route/focus handling.
- [x] Review adversarially. Fixed expired-history entries converting another chat
  and REPL tabs losing host metadata. Added regression coverage. The final review
  also caught offline child views displaying the root directory; they now say
  the directory is unavailable until the actual child directory is known.
- [x] Verify actual layouts and repair status/control overlap at 320px pane width.
- [x] Update canonical docs and existing browser fixtures for the new controls.

Validation completed:

- `task check`: passed, including Go vet/tests, contract/SDK checks, production
  web build, 53 app test files / 546 tests, distribution and onboarding script tests.
- `npx tsc -p packages/app/tsconfig.json --noEmit`: passed after the final UI edits.
- `node packages/ui/tests/workspace-tabs.mjs`: Chromium/Firefox keyboard, focus,
  close, pointer reorder, RTL, zoom, overflow, touch and strict CSP; all 66 themes
  pass Axe. Final metadata-width run also verifies the same geometry.
- `node apps/web/scripts/repl-viewer.mjs`: 11 workflows each in Chromium and
  Firefox, including toolbar/menu fresh adjacency, source drafts and reading
  anchors, Back/Forward/reload, independent agents, splits/moves, bounded shared
  history, live evidence, narrow panes and WCAG Axe in three themes. No daemon
  commands and at most one root subscription for duplicate views.
- `node apps/web/scripts/chat-activity.mjs`: Chromium/Firefox and staged Electron
  verify accurate status, live phases without polling, REPL entry from evidence,
  motion/typography, 320/390px bounds and selection through completion. Electron
  additionally checks the exact embedded renderer, 48px native drag/no-drag regions
  and 400% native zoom using an isolated profile.
- Staged desktop build passed. This is a development build, not a signed release
  packaging or notarization test. No real sessions or provider requests were used.

Visual evidence was inspected from `/tmp/whip-repl-viewer-results` (light, dark,
320px splits) and `/tmp/whip-chat-activity-results` (staged Electron and narrow
active headers). The UI now has one shared identity bar and a compact REPL
language/history-help row, with no duplicate REPL heading.

Implementation kept the existing v3 workspace schema, shared root leases, bounded
reading stores and dependencies. The implementation uses the production fixtures
for visual acceptance rather than introducing a parallel permanent mock screen.

- `node apps/web/scripts/workspace-layout.mjs`: all seven split workspace workflows
  pass in both Chromium and Firefox.
- `node apps/web/scripts/session-tabs.mjs`: all 13 Chromium workflows and 12
  Firefox workflows passed (the extra Chromium workflow measures retained heap).
  Updated the streaming assertion to inspect grouped execution cells and accounted
  for distinct tab/sidebar polling owners when their requested root lists match.
  Tests retain the existing two-second polling bound per owner.

Final renderer build: `ae521e6e6e8e52194cdff990962e03474b745b0661ec0de70d7f21fc8e3c53c4`.
Final app test rerun: 53 files / 546 tests passed. Type checking and diff whitespace
checks passed. The Settings/performance/chat-polish fixture controls were updated
for the new UI and syntax-checked; their full unrelated scenario suites were not
rerun. No signing, installation, commit or publication was performed.
