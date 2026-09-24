# Session REPL viewer

Branch: `whip-rlm` (existing working tree)

Status: implemented and automated acceptance complete, 2026-09-07. User approved execution of this plan.

## Goal

Choose **Open REPL** from a session tab's three-dot menu to switch that same
view from chat to a read-only Starlark notebook. Use the TUI REPL's information
hierarchy with WHIP's web components, typography and theme tokens. **Open chat**
switches back, preserving the draft and conversation reading position.

The mode belongs to a view, not to the underlying session. A split can show
chat on one side and the same session's REPL on the other. Switching one view
must not switch its duplicates or start/cancel any daemon work.

## Non-goals

- Typing or executing Starlark, rerunning/editing cells, shell/PTY input, or
  inspecting arbitrary live VM globals.
- A second event subscription, an unbounded notebook cache, or fetching an
  entire session/event journal at startup.
- A new docking library, editor dependency, tab plugin registry, or redesign
  of the existing Executions inspector.
- Reconstructing missing host-call traces or exact historical timings from
  chat text. A durable, complete execution archive would be separate work.

## Research and existing contracts

| Source | Finding and implication |
| --- | --- |
| `internal/tui/repl_panel.go:24`, `:98`, `:405` | TUI cells show partial Starlark code, cumulative print output, individual host calls, result/error, step count and restart markers. Reuse this hierarchy, not terminal rendering code. |
| `internal/tui/repl_panel.go:194`, `:335` | TUI retains observed cells across snapshots and follows the newest cell until the user scrolls away. It does not provide a complete historical archive after restart. |
| `internal/tui/repl_panel_test.go:18`, `:48` | Existing fixtures exercise partial JSON, interleaved root/child execution, host calls, results, errors and restart rendering; port these cases into the SDK/web tests. |
| `internal/daemon/recursive_runtime.go:169`, `internal/rlm/kernel.go:333` | Host-call events carry the parent model tool-call ID, redacted argument summary, duration and error. They describe completed host calls, not an invented live progress percentage. |
| `internal/rlm/kernel.go:345`, `internal/protocol/events.go:10` | Results contain value/output/steps and sometimes a restore report. Stream events and `scratch.restored` already exist in the protocol. |
| `internal/session/snapshot_view.go:69`, `internal/session/event_test.go:88` | Snapshot presentation is a bounded, uncommitted tail. Completed-turn detail is cleared; durable transcript history is independently pageable. |
| `packages/sdk/src/state.ts:237`, `:301`, `:435` | One SDK session view owns snapshot/replay/stream consistency. Arguments and output are cumulative, notifications are batched, and turn boundaries reset presentation. Restart lifecycle payloads are not retained as UI evidence today. |
| `packages/app/src/details/observation.tsx:284`, `packages/app/src/timeline.tsx:131` | Executions already displays loaded tool history and current presentation, but its generic rows omit host-call/restart detail. It is not sufficient as the REPL data model. |
| `packages/app/src/session-tabs.ts:4`, `:89`, `packages/app/src/session-tab-strip.tsx:165` | Descriptors currently accept only `kind: 'chat'`; stable view IDs, menus, duplicate views, splitting, moving and restore already exist. Extend those paths. |
| `packages/app/src/workspace-views.ts:7`, `packages/app/src/runtime.ts:474` | Visible panes share one root lease; selected children use reference-counted agent leases. Chat and REPL must share those owners. |
| `packages/ui/src/code-block.tsx:9`, `packages/ui/README.md:90` | CodeBlock already supports Starlark, bounded highlighting, theme tokens, plain-text fallback and caller-owned content actions. No editor/highlighter dependency is needed. |
| `docs/frontend.md:175`, `:220`, `:259` | SDK owns reconstructed session state, Router owns the focused shareable location, app owns window layout/reading hints, and UI owns generic rendering. |

The roadmap and existing harness research were checked. The shipped TUI and web
execution inspector are the relevant prior art; the split-view design explicitly
left production REPL rendering to the app.

## Interaction and visual design

1. Add **Open REPL** near **Session details** in the existing tab menu, context
   menu, and mobile open-session picker. It selects the target tab if needed,
   closes the menu, and replaces that view's content in its existing pane.
2. In REPL mode the same item becomes **Open chat**. Keep the session title and
   activity state; add a compact `REPL` label to distinguish it from a chat
   duplicate. Include mode and agent in accessible tab labels.
3. A compact toolbar shows `REPL`, the selected agent, and a count explicitly
   labelled as loaded cells. An agent selector uses the existing bounded agent
   collection; opening it must not hydrate every child's history. Initially use
   the agent already selected in the tab, or the root.
4. Render cells chronologically with an `In […]` header, status text/icon, and
   timing/steps when known. Code is visible by default. Below it, show compact
   host-call rows, print output, return value, and errors. Use fine separators
   and a narrow semantic status gutter; avoid a stack of redundant nested cards.
5. Long output starts as a six-line preview, following the TUI, with an explicit
   expand/collapse control and omitted-line count. While running, show the tail;
   once finished, keep the user's expansion and reading position. Code and output
   remain selectable and have copy controls. Excerpts and full-content actions
   must be labelled honestly.
6. Use 13px interface text and the existing 12px JetBrains Mono CodeBlock style,
   with 16px pane padding, compact spacing, semantic surfaces and 1px quiet
   borders. Support all existing themes and the 320px minimum pane width.
   Horizontal code scrolling stays inside the code block.
7. Follow new cells only while at the bottom. Scrolling up exposes **Latest**;
   streaming changes, output expansion, older pages, pane resizing and reconnect
   preserve the visible cell anchor. Chat and REPL have separate bookmarks.
8. There is no chat composer in REPL mode. Existing pending questions,
   permissions, connection notices and exact-turn stop controls stay reachable
   through the shared session wrapper. Viewing REPL alone submits nothing.
9. Loading, no executions, no executions in the loaded page, disconnected/stale,
   errors, missing child, and unavailable/truncated content are distinct states.
   An idle agent with no recorded cells says that cells appear when it executes.

Cell numbers describe the loaded notebook, not a claimed lifetime execution
ordinal. Loading earlier history may renumber visible labels; cell identity and
scroll anchors do not depend on that number.

## State and routing

- Extend the existing descriptor to `kind: 'chat' | 'repl'`. Keep the same view
  ID, root ID, pane, tab index and selected agent when changing mode.
- Use validated `?view=repl` for focused REPL URLs; omit it for chat. The Router
  remains authoritative for the focused view. The tab descriptor persists mode;
  a single route-search serializer derives the URL from it rather than storing
  a second mode field inside `location`.
- Extend `visit`/navigation binding and the route's search validation together.
  Include mode in navigation equality and pending-navigation keys. Back/Forward,
  copied links and modified clicks must select the correct renderer.
- Route-generated changes to agent/inspector location preserve mode. Audit tab,
  sidebar, search, inspector and restored-session links through the same helper.
  Sidebar/search reuse an existing view including its mode; a newly created
  session view defaults to chat. An explicit URL without `view=repl` opens chat.
- Retain validated v2 layout storage and the existing v1 migration; expand its
  allowed kinds and test old chat layouts, malformed/unknown kinds, close/reopen,
  and storage denial. Layout storage contains metadata only, never cell bodies.
- Splitting copies the source mode; dragging and moving preserve it. Mode
  changes do not consume another tab/root slot. Keep current limits: four panes,
  32 views, four retained roots and four saved hosts.
- Keep one selected agent per view across mode switches. Bookmarks use the
  existing bounded ReadingPositions store, adding a mode suffix after the
  runtime/view prefix so closing a view forgets both modes. Drafts and attachment
  state retain their current runtime/root/recipient ownership.
- Start with a direct two-case renderer, not a registry. Extract only the shared
  session wrapper needed to keep agent leases, pending requests, details and
  connection states available in both renderers.

## SDK data projection

Add a small execution module under `packages/sdk/src`, integrated into the
existing `SessionView` lifecycle. The app consumes immutable execution snapshots;
it does not listen to raw client events or maintain its own stream reducer.

The projection reconciles two sources:

- **Recorded history:** pair structured `rlm_exec` tool calls with their tool
  results from the already-loaded `HistoryView`; decode code, output, result,
  errors, steps and embedded restore information where present. Retain content
  references instead of loading bodies automatically.
- **Observed execution:** fold existing `stream.tool.*`, `stream.cell.host`,
  `scratch.restored` and turn lifecycle events inside SessionView. Preserve
  observed metadata through ordinary snapshots/turn boundaries, and merge it
  with the recorded cell when history commits. Seed from the available snapshot
  presentation on initial attach/reconnect.

Use stable source identities scoped to root, agent and history revision. Match
live/history calls by tool-call ID with occurrence/turn context where necessary;
do not assume an ID is globally unique across sessions or repeated turns. Preserve
the live row key across commit, deduplicate replay by event sequence (BigInt-safe),
and retain repeated host operations as distinct calls when their event IDs differ.

Decode partial JSON string arguments using a tested bounded parser so unfinished
`{"code":"…` becomes readable Starlark. Match the existing cumulative argument/
output contract; never append a full-so-far output to itself. Missing or invalid
completion data is unavailable/unknown, not a successful empty result.

Store only supplied or explicitly observed timing. Stream envelopes contain no
timestamps, so a running timer can represent time observed in this client, labelled
accordingly. Do not invent replayed historical durations. Distinguish writing,
running, completed, failed, cancelled/interrupted when evidenced, and stale/unknown
after loss of observation; an unclosed cell must not spin forever after its turn
has ended.

Bound the supplemental observed evidence to **256 entries per root, 128 host
calls per cell, and 1 MiB total**, counted inside the existing **8 MiB SessionView
budget**. Include restart rows, nested fields, references and indexing overhead
in admission/eviction accounting. Prefer evicting oldest completed evidence;
oversized active output becomes a visibly truncated preview. The app's visible
cell list derives from this evidence plus the already bounded history rather
than storing another copy of all history cells. No notebook data goes to browser
storage. Clear incompatible evidence on history revision/host change and disposal.

Full reload can recover recorded code/results from transcript pages. Per-host
call traces and standalone restart events that are no longer in the snapshot or
retained SDK view may be unavailable. Show that limitation where relevant; do not
walk the event journal to fill it in. **Load older** uses existing bounded
`view.loadOlder(agentId)` and runs only on request. Empty tool-free pages still
offer the next page. Existing scoped content reads/download limits apply.

This requires an additive SDK state API, but **no daemon, wire protocol,
platform-interface or dependency changes** for the proposed scope.

## Implementation sequence and files

- [x] **Execution projection:** add `packages/sdk/src/executions.ts`; integrate
  raw event folding, snapshot/history reconciliation, revision resets and memory
  accounting in `packages/sdk/src/state.ts`. Export types through the existing
  state entry point. Add focused SDK tests.
- [x] **Mode-aware tabs/routes:** update `packages/app/src/session-tabs.ts`,
  `session-tab-routing.ts`, `session-tab-strip.tsx`, and
  `routes/h.$runtimeId.s.$rootId.tsx`; share mode-preserving search construction
  with `session-sidebar.tsx`, `session-search-dialog.tsx` and inspector links.
- [x] **Shared session rendering:** extract the necessary common wrapper from
  `conversation.tsx`; keep `useWorkspaceViews`/`runtime.acquireAgent` as lease
  owners. Add `repl-view.tsx` and `repl-view.stylex.ts` in app, composed from
  existing UI CodeBlock, Select, Badge, Button and copy controls.
- [x] **Reading behavior:** add independent REPL anchors through
  `reading-positions.ts`, virtualize cells with TanStack Virtual, and preserve
  per-cell disclosure state within the same bounded view-local lifetime.
- [x] **Acceptance:** implement isolated browser fixtures and run the checks
  below. Compare to TUI fixtures and inspect desktop split/narrow screenshots.
- [x] **Documentation:** update `docs/frontend.md` (mode, ownership, budgets),
  `docs/features.md` (behavior → code → tests), `docs/roadmap.md`, SDK README,
  and this plan with implementation/validation evidence.

## Validation

- SDK: partial/escaped JSON; cumulative versus delta updates; interleaved calls
  and agents; same IDs in different roots/turns; repeated host calls; result/error
  decoding; scratch restore; interrupted execution; live-to-recorded deduplication;
  snapshot refresh, reconnect gaps and rewind revisions; very large counters;
  retained byte/count limits and explicit truncation.
- App: switching the targeted tab only; no new tab; Open chat restores draft,
  attachment and both scroll positions; agent selection; old-layout restore;
  close/reopen; mode-aware links/history/modified clicks; host changes and late
  content-read cancellation; missing agent and empty pages with more history.
- Browser: exercise chat/REPL duplicates in split panes, drag/move/resize, mobile
  picker, stream while scrolled up, older pages and expanded output. Assert one
  root subscription for duplicate chat/REPL views, shared child lease behavior,
  no background-root hydration and no mutation from opening/changing views.
- Visual/accessibility: Claude Code, light/dark and the existing all-theme
  contrast harness; 320px pane, wide pane and mobile; long code, large values,
  errors, stale/loading/empty states; labelled code regions, keyboard menu/focus
  return, visible focus and restrained status announcements. Do not put the
  streaming notebook itself in an aria-live region.
- Run affected SDK/app tests, `npm run check:web`, isolated Chromium/Firefox
  REPL workflows and existing session-tab/workspace browser scenarios, then
  `task check` for implementation acceptance. Use fake-provider fixtures; document
  actual Safari/VoiceOver/device coverage separately from automated emulation.

## Acceptance

The tab menu switches a view between chat and a readable, live, TUI-inspired
REPL notebook. Duplicates can show different modes/agents without losing drafts
or reading position. Evidence and subscriptions remain bounded, historical gaps
are explicit, and every visible action uses the existing web design system.

## Implementation notes — 2026-09-07

- `SessionContent` keeps leases, requests, cancellation and inspectors shared;
  the selected mode mounts either the chat timeline/composer or `ReplView`.
  `ReadingList` extracts the existing virtual reader so both modes share selection,
  follow, pagination and anchor behavior. No rendering dependency was added.
- `sessionSearch` preserves mode through route/search/sidebar/attention/inspector
  links. Mode changes target the original view ID and restore focus; duplicating
  a split copies both chat and REPL bookmarks before they evolve independently.
- Supplemental execution evidence belongs to `SessionView` and shares its root
  subscription. A child first observed before loading its history lacks a durable
  event-to-history mapping. Ambiguous reused IDs remain separate observed evidence
  instead of attaching new host traces to an older result.
- Adversarial review found result-only page joins, reused IDs in newly opened
  children, and current-child ordering issues. Regression cases cover each; fixes
  preserve call identity when older pages supply the missing assistant call and
  establish a safe first-history boundary for newly opened children.
- Main app checks currently pass: 155 tests and `npm run check:web`. The isolated
  REPL Chromium/Firefox fixtures passed menu/keyboard switching, split dragging,
  modes/agents, retained drafts/scroll, pagination, streaming host/restart evidence,
  sidebar/search links and mobile presentation. The root subscription maximum was
  one for duplicate views and navigation sent no commands.
- Existing split workspace browser scenarios passed Chromium and Firefox. Visual
  inspection includes Claude Code, light/dark, 320px panes, split agents and live
  output. Automated Axe checks cover the REPL in three themes; actual mobile
  devices, Safari and VoiceOver are not claimed by browser emulation.
- Adversarial review signed off after fixes for result-only pages, first-history
  loading/error placeholders, restart ordering, repeated IDs and ambiguous closed
  observations outside bounded pages. Final SDK check: **188 tests passed**.
- Final `env -u INFERENCE_API_KEY GOFLAGS=-p=2 task check` passed. Provider credentials
  were excluded from the test process so optional integration tests did not contact
  a live model; package-level Go parallelism was bounded. This includes Go format,
  vet/custom vet/tests, protocol generation drift, SDK, app, UI/theme and packing
  checks. App: **155 tests passed**; `npm run check:web` passed.
- Existing session-tabs passed Chromium (13 workflow groups) and Firefox (12);
  split workspace passed seven groups in each. Chromium performance passed all
  five groups: 10,000 root messages/100 children, four real prepends, native
  selection across 8,000px, 20 cached recipient switches, and responsive streaming
  with 16 fake agents and 32 near-limit drafts. Its old Details/readiness selectors
  were updated to the current tab menu and child composer.
- Final packed assets include all review fixes. Chromium and Firefox each passed
  all eight REPL workflow groups, including explicit keyboard focus and split
  bookmark-copy regressions. Maximum root subscriptions: **1**. No viewer commands
  were submitted. History requests stayed within 128 messages / 256 KiB. Axe
  WCAG 2/2.1 A/AA checks passed in Claude Code, light and dark themes.
- Evidence: `/tmp/whip-repl-final-check.log`, `/tmp/whip-repl-sdk-check.log`,
  `/tmp/whip-repl-viewer-results/repl-viewer.json`,
  `/tmp/whip-workspace-layout-results/workspace-layout.json`,
  `/tmp/whip-session-tabs-results/session-tabs.json`, and
  `/tmp/whip-web-browser-results/performance.json`. Screenshots alongside REPL
  results show full-width, 320px split panes, child errors, streaming and mobile.
  The browser scripts reproduce these temporary artifacts.
