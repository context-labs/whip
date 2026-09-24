# Restore the sub-agent composer dock

Branch: `compaction-loop-and-ui-cleanup`
Date: 2026-09-19
Status: Implemented and validated (2026-09-19).

## Goal

Restore a persistent, quiet overview of delegated work immediately above the
chat composer, without undoing the improved chronological transcript. Opening a
child should preserve the main conversation and composer and show the child in
a right-hand split.

## Confirmed user decisions

- Keep a **compact launch record** inline, not a second rich live agent card.
- Clicking a child opens its chat **split right**, not in the main chat's place.
- **Active first, finished collapsed**. Selected/open and failed/waiting children
  must not disappear unexpectedly.

## Research: exact former implementation

The former component was `ChatActivity`, captioned **Session agents**.

| Change | Commit | Date |
| --- | --- | --- |
| Introduced current activity + agent dock | `d570a1610728ba148259539473bb768c3debd061` | Sep 9, 2026 |
| Moved current-agent status to information bar; kept child dock | `f6f0311dc748d4e79242f465771a51f82f6e70ff` | Sep 11 |
| Added call/compaction counters | `cac044b3bff1e4ac902fd7895cd56f1a5f2b527e` | Sep 13 |
| Removed dock and introduced inline cards | `cf181d3d95bb59fd313398002583ef2fe228cb3d` (`ui clean up and such`) | Sep 17 |

Best recovery baseline: `7148bb95ef4f4f41d75cdd977daf3d06be1fb455`,
the removal commit's first parent. Inspect with:

```sh
git show 7148bb95ef4f4f41d75cdd977daf3d06be1fb455:packages/app/src/chat-activity.tsx
git show cf181d3d95bb59fd313398002583ef2fe228cb3d -- packages/app/src/chat-activity.tsx packages/app/src/conversation.tsx packages/app/src/transcript-activity.tsx
```

Historical source spans at that baseline:

- `packages/app/src/chat-activity.tsx:76–106`: final child-only dock.
- Same file `37–54`: lifecycle and turn counters; `179–189`: dock styles.
- `packages/app/src/conversation.tsx:351–381`: dock → human requests → composer.
- `packages/app/test/chat-activity.test.tsx:149–158,172–181`: bounded rows,
  navigation, admission order and focus retention.
- `docs/frontend.md:1234–1246`: old architectural description.

The dock was a vertical list, not a carousel. It showed up to three direct
children of the selected agent: status dot, name, model/effort, status and arrow.
Running/queued/blocked/failed and keyboard-focused rows were eligible; normally
completed/idle rows disappeared, and stopped/deleted children were excluded.
Attention was prioritized while preserving admitted order and keyboard focus.
Overflow/omitted agents exposed the existing Agents inspector. Styles bounded
height to 30dvh with 28px desktop/44px phone row targets. It did not offer stop
controls, live transcript previews or result expansion. Click changed the agent
in the **same** view—this is explicitly NOT the behavior to restore.

Do not revert the removal commit or copy the whole old file: both also contain
unrelated transcript, status, history and layout work that must remain intact.

## Current architecture to retain

- `chat-activity.tsx` still owns `agentStatus` and call/compaction summaries.
  Reuse these rather than creating another definition of working/waiting/failed.
- `chat-activity-rows.ts` gives typed `agents.spawn` operations stable chronological
  rows. `transcript-activity.tsx:InlineAgent` is the current rich card;
  `timeline.tsx` supplies snapshot agent state to it.
- `conversation.tsx` owns selected-agent history, navigation, root-wide requests
  and the composer. Only selected children acquire history; a roster must not
  subscribe to every child transcript.
- `composer.tsx` scopes drafts/attachments by runtime/root/agent and selection by
  view/agent. Opening a split must not retarget or remount the source composer.
- `PendingRequests` already owns approvals/questions, above the composer. Keep
  those controls authoritative; the dock must not introduce duplicate forms.
- `details/observation.tsx` is the existing paged full Agents directory.
- Current frontend guide explicitly documents the dock's removal. Update it
  when this change ships, not by treating the old plan as current requirements.

## Proposed interaction and visual design

### Quiet, bounded overview

```text
[transcript and compact chronological launch records]

Agents                                      All agents
  ● researcher       Working · 3 calls        [split →]
  ◇ reviewer         Waiting for approval     [split →]
  ! test-runner      Failed                    [split →]
  ▸ Finished (2)

[pending approval/question, when present]
[queued inputs, when present]
[main chat composer — unchanged recipient and draft]
```

Use existing typography, semantic status tokens and quiet borders. Names and
status carry the hierarchy; model/effort and detailed counters belong in a
secondary tooltip or existing inspector rather than widening every row. No
random agent colors, progress bars, elapsed-time polling or card carousels.
The whole row opens the child; the split icon clarifies the destination.

- Start from the old three-row compact bound, with explicit overflow/All agents.
  An expanded Finished section has a capped, scrollable region. Do not let the
  combined dock/requests/queue consume the entire chat pane.
- Scope to **direct children of the agent in this pane**, as before. Root chat
  shows its children; a child's own chat can show its children. All agents opens
  the full recursive directory. This is a proposed default, not a new backend filter.
- Prioritize waiting/failed work, then active/queued children in stable admission
  order. Preserve the focused row; avoid rearranging a click target underneath
  the pointer or removing keyboard focus during status updates.
- Finished is a grouping of settled latest turns, not a claim that retained
  agents can never run again or that their reports have been read/delivered.
  Show the actual current status within it; resumed work returns to active.
- Failed/waiting children stay prominent. A settled child currently open in the
  relevant child pane remains readily reachable rather than vanishing mid-use.
- No children: no empty dock. Partial metadata: an explicit incomplete/All agents
  affordance; never present a bounded snapshot count as the full session total.
- Offline: keep the roster, label updates paused; do not infer completion from
  a dropped connection. Deleted agents lose unavailable actions with sane focus
  fallback. Stopped agents can remain in Finished with truthful status.
- Single live announcer remains `CurrentActivity`; no per-row aria-live stream.
  Use accessible row names, keyboard focus and reduced-motion support. At narrow
  widths, use a compact disclosure/stack, not horizontal document overflow.

### Right-hand child chat

Use the existing split workspace and session routing, not a custom side drawer.

- Open the child with the same runtime/root, `kind: chat`, child agent location,
  a separate view identity, and a split to the right of the source chat pane.
- Source transcript position, draft, attachments, queue and recipient remain
  unchanged. The child pane has its own composer, clearly named for that child.
- Proposed reuse policy: if the exact child chat is already visible in another
  pane, focus it. Otherwise prefer the right-hand agent pane created for this
  source chat; reuse that companion view for A → B → A rather than accumulating
  tabs or panes. Agent-scoped drafts and view/agent reading positions remain
  isolated. Never overwrite unrelated tabs or commandeer an unrelated pane
  merely because it is focused. Validate companion ownership/location on every
  click; a moved, closed or repurposed view is not blindly reusable.
- Do not silently fall back to replacing the main chat when the workspace is
  narrow or a split/tab limit is reached. Share the existing split geometry guard
  and show actionable feedback: widen the window, close a pane, or explicitly
  choose Open in tab. Do not create a hidden split on a phone. A previously
  created split still follows the existing collapse/restore behavior on resize.
- Reuse the same agent-opening intent for the compact launch record so inline
  and dock navigation do not disagree. Keep inspector navigation changes out of
  scope unless necessary for consistency.

### Verified routing constraints

`openSessionView()` calls `openRelated()` and opens an adjacent **tab**, not a
split. It also retains the source agent; calling it unchanged will not satisfy
this design. `tabs.split()` duplicates a view into a right pane, while
`openChatView()` can create a root-chat split but currently cannot target a child.

Add a focused routing helper backed by a small **atomic tab-state operation**
that creates the child chat directly, using the existing descriptor/layout tree.
Avoid publishing an intermediate duplicate root composer through split-then-
retarget. Navigate via `tabDestination(target)` with the target view ID, child
agent and no inherited inspector panel; reuse existing workspace focus handling.
A view-local companion ID suffices initially; no new persisted ownership or SDK
state is necessary. Revalidate it against the current layout before each reuse.

Current constraints: **4 panes / 32 tabs**; right splitting requires source frame
width at least **641px** and is unavailable in compact layout. The geometry guard
currently lives in `SessionTabStrip.canSplit`, not in the state API, so extract/
share it rather than bypass it. Reusing a valid child view must work even when
new-pane/tab capacity is exhausted. Check all creation constraints before writes.

Source map: `session-tab-routing.ts:40–44,73–84`;
`session-tabs.ts:455–469,553–570,597–606,673–683`;
`session-tab-strip.tsx:177–181`; `workspace-views.ts:12–37`;
`docs/frontend.md:813–821`. These are current working-tree references and may
shift with concurrent edits. Root/child views share one SDK root-view lease;
only the selected child needs its own history acquisition.

## Implementation steps

1. **Projection and dock:** add a small `AgentDock` in `packages/app`, reusing
   current status helpers and snapshot data. Extract/test only the selection,
   grouping and stable-order logic that needs independent tests; no new store.
2. **Navigation:** extend existing session-tab routing/model only as needed for
   explicit child-chat split intent and safe pane reuse. No daemon session creation,
   no duplication of root leases, no persistent independent recipient selector.
3. **Composition:** mount dock outside the transcript scroller in
   `conversation.tsx`, before existing requests/composer. Name the child composer
   recipient clearly and keep root composer untouched when a split opens.
4. **Inline simplification:** replace rich `InlineAgent` presentation with a
   compact launch record using the existing row identity and typed child ID.
   Preserve launch failure/cancellation, missing IDs, execution details and Open
   in REPL. Do not filter away failed spawn evidence. Keeping the row identity
   avoids gratuitous changes to activity grouping and reading anchors.
5. **Validation and docs:** update canonical frontend guide, feature map and
   affected fixtures; review both code size and correctness before shipping.

## Test and acceptance plan

- Dock: 0/1/many children, direct-child scope, bounded/omitted collections,
  waiting/failure priority, stable focus/order, collapse state, finished → running,
  offline/reconnect and deleted/stopped agents.
- Navigation: first right split; repeat-click idempotency; another child reuses
  the child pane; exact visible child focus; no unrelated-pane replacement;
  source pane stays root; source draft/attachment/queue/reading state preserved;
  closed/moved/repurposed companion handled; capacity reuse succeeds and creation
  failure leaves the snapshot unchanged; same child in another mode; tab/pane limits and compact layout fallback.
- Inline record: typed/missing child ID, failed spawn, durable live/history row
  reconciliation, launch-detail disclosure and navigation intent preserved.
- Subscription assertions: mounting roster adds no child history observers,
  transcript fetches, polling, event reducers or SDK collections.
- Browser: long/short histories, end-following versus reading older text while
  dock grows/shrinks, focused/selected content, 320px/split panes, large text,
  light/dark themes, keyboard and reduced motion; no viewport overflow or jumps.
- Extend `chat-activity.test.tsx`, agent/dock tests, routing/session-tabs/workspace
  tests and composer/queue/reading-position tests. Update the inline-agent checks
  in `apps/web/scripts/chat-activity.mjs` and add the right-split browser scenario.

## Scope and safety

No new dependencies, protocol fields, daemon work, eager child transcript
previews, new stop/delete controls, result-delivery semantics or saved settings.
Widths, transcript display preferences and existing split-view behavior stay
owned by their current layers. No changes to REPL/trace layout in this feature.

The checkout contains substantial in-progress changes, including chat history
prefetch and session-tab routing. Re-read those files at implementation time and
merge targeted edits; do not restore old versions, reset unrelated work or claim
this research validated unrelated uncommitted code. Implementation validation is
recorded below.

## Implementation record

User approved execution after reviewing this plan. Implemented:

- [x] Snapshot-only `AgentDock`, direct-child scope, active/attention priority,
  three-row compact bound, collapsed Finished, focus/pointer retention, partial
  and offline labeling; aligns with composer and capped at 30dvh.
- [x] Atomic `SessionTabs.openChildChat`, shared split geometry guard and routing
  helper. Reuses visible exact matches or validated view-local companion receipts;
  protects unrelated/moved/repurposed views and source composer identity.
- [x] Conversation wiring and explicit Open in tab fallback (not offered when
  32-tab capacity is exhausted); source recipient/draft remains untouched.
- [x] Compact typed inline launch records retain launch failure/cancellation,
  deleted/missing-ID safety and expandable execution evidence.
- [x] Canonical frontend guide, feature source/test map and roadmap updated.
- [x] TypeScript and production renderer build: `npm run check:web` passed.
- [x] Combined focused run: **288 tests passed in 13 files**, covering dock,
  conversation integration, launch evidence, streaming/reading positions,
  composer/queue, requests, tab routing/state, workspace leases and desktop close.
- [x] Production **Chromium + Firefox** acceptance passed via `node scripts/pack-web.mjs`
  then `WHIP_CHAT_AGENTS_ONLY=1 WHIP_WEB_BROWSERS=chromium,firefox WHIP_CHAT_ACTIVITY_RESULTS=/tmp/whip-agent-dock-results node apps/web/scripts/chat-activity.mjs`.
  Verified no eager child-history reads; right split and repeat/inline reuse;
  root/child draft isolation and root PNG/preview preservation; 1440/1100/1280px
  resizing; 390/320px explicit tab fallback with no hidden split; keyboard,
  reduced motion and no CSP/page errors. Finished disclosure and first child split
  preserved end-following/reading anchors: **0px drift in both browsers**.
  Evidence: `/tmp/whip-agent-dock-results/results.json`, 18 screenshots and videos.
  Renderer digest: `457aee0d7ae41dbead5af945df22c56da0a3759c693fe0b55318590981a3f1c7`.
- [x] Syntax checks for both browser scripts and final `git diff --check` passed.

Validation setup corrections: re-packed stale embedded assets before browser runs;
compact chrome has no desktop tab DOM, so narrow assertions use visible panels
and a subsequent wide layout to prove no hidden split. No application workaround
was needed for either fixture correction.

Deliberate scope: no persisted companion ownership, no protocol changes, no
new dependencies or child-history observers for the roster. Full recursive
navigation remains in the existing inspector. Browser fixture children are
stopped; recipient isolation is verified through labeled composers, independent
drafts and selected child history requests, without invoking a live provider.
