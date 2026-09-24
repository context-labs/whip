# A clearer, livelier agent conversation

Research and implementation proposal · 2026-09-08 · inspected commit `40699639b`.
Implementation authorized by the user after review, including host-operation
start events. Implemented in the existing checkout; no installed daemon
or application replacement is included. `docs/frontend.md` remains the architectural guide.

## Implementation record · 2026-09-09

Implemented the shared web/desktop activity projection, compact execution groups,
named session-agent rows and one live status above the composer. Group details
retain manual disclosure choice, reading aliases, focused cells and selected
previews. Existing Appearance typography/density/motion preferences apply. The
old per-message “Writing…” decoration was removed so it cannot conflict with the
current phase. Chat and REPL share client-observed timing; hidden documents stop
the display timer and activity animation.

Protocol 5.1 adds a correlated host-start event before dispatch and preserves the
existing completion event. Both views consume SDK execution evidence. Repeated
invocations, caught failures, cancellation, replay and ambiguous missing prefixes
are handled without guessing an operation or adding another state store. No
database migration, model argument or scheduling change is included.

Validation evidence:

- `task check` passed on the combined checkout: Go format/vet/whipvet/tests,
  generated protocol drift and interop, SDK checks, the production web build,
  361 app tests, UI checks/tests and packaging tests. Final log:
  `/tmp/whip-chat-task-check.log`. Concurrent provider and Settings work was
  preserved and included in this repository-wide gate.
- [Production browser results](evidence/browser-results.json): Chromium, Firefox
  and staged Electron, real SDK subscription and isolated fixture daemon. Covers
  grouping, host phase changes, REPL agreement, reload, selection, keyboard
  disclosure, reduced motion, 320/390px widths, and light/dark themes with the
  minimum/maximum configured system font sizes.
- [Renderer identity](evidence/packaging.json): all three engines verified the
  fixture daemon's embedded asset hashes against renderer
  `74c3a0abbe888d0d920bd364c63703cc189d6cb04db5564f5620b4ee52cbc169`.
  Electron used the real staged main/preload and bundled runtime. This is a
  development staging check, not signed installed-app acceptance. Concurrent
  Settings/provider work may produce later artifacts in the same checkout.
- [Animation observation](evidence/animation-observation.json): visible opacity
  changed during a 400ms Chromium trace with zero Layout events. The fixture also
  records video; the raw trace/video stay in `/tmp/whip-chat-activity-results`.
- Focused Go race tests passed for kernel host start/completion/cancellation and
  daemon presentation delivery. SDK regression tests and app/component tests
  passed, along with protocol drift checks and native mobile typechecking.
- [Desktop zoom geometry](evidence/electron-zoom.json) verifies horizontal reflow
  and visible activity at native 400% zoom (320 CSS pixels wide). It is not a
  complete accessibility acceptance test for the entire composer.

Screenshots: [running](evidence/chromium-running.png),
[expanded](evidence/chromium-expanded.png),
[large system type/light theme](evidence/chromium-light-large-type.png),
[desktop](evidence/electron-running.png).

Manual VoiceOver and actual Safari acceptance have not been performed. The
installed daemon/app were not restarted or replaced. Completion-only remote
daemons retain generic Running until upgraded; precise host-operation labels need
the new daemon events. Canonical implementation guidance is in `docs/frontend.md`.

Revised after comparing the REPL's execution-state path: **existing events and
SDK state already support accurate Running status**. The user has also requested
host-operation start/completion visibility in this pass, to make that status more
informative. Include that targeted event extension in V1; model-authored labels
and a new historical activity store remain optional.

## Recommendation

Make the main chat a readable account of the work: authored messages interleaved
with compact, expandable activity groups, named agents, and one dependable live
status. Preserve the REPL as the detailed execution reader.

The supplied screenshots suggest quiet text, sparse surfaces, a small moving
indicator and work that condenses after completion. They cannot establish exact
animation timing; the motion below is a proposed interpretation. The accompanying
[interactive study](design.html) illustrates the direction using simulated data.

Five decisions define the first release:

1. Group adjacent `rlm_exec` calls between written messages. Keep every authored
   update and answer in its original position.
2. Keep current work visible even when the root has replied while children continue.
3. Show agents by name, with an honest current state and a direct inspection link.
4. Use subdued motion to indicate activity; keep prose steady and immediately readable.
5. Derive cell status from the same evidence used by the REPL, and extend it with
   correlated host-operation starts/completions for precise live operation labels.

## Design intent

The person is directing coding work, reading results, deciding whether to wait or
intervene, and often switching between several sessions. This should feel like a
calm conversation with a team whose work is visible.

- **Domain:** turns, execution cells, host operations, delegated agents, handoffs,
  waiting for results, human approvals, retained evidence.
- **Color world:** the selected theme's canvas; raised neutral input surfaces;
  foreground reading text; accessible secondary gray; amber for human attention;
  red for failure; restrained success color for an explicit completed result.
  These are existing semantic tokens, not a new palette.
- **Signature:** a compact work summary that opens into real execution evidence
  and named agents, while the written conversation stays readable around it.
- **Replace defaults:** repeated tool cards become grouped disclosures; generic
  spinners become a labeled phase and age; a sprawling agent tree becomes a few
  named activity rows with deeper inspection on demand.

## Research and its implications

| Source | Relevant finding | Decision for WHIP |
| --- | --- | --- |
| [NN/g: visibility of system status](https://www.nngroup.com/articles/visibility-system-status/) | Timely feedback lets people understand the state and choose what to do next. | Always distinguish accepted input, ongoing work, human intervention and unavailable updates. |
| [Microsoft Fluent: Wait UX](https://fluent2.microsoft.design/wait-ux) | Accurate concise labels, context preservation and limited competing indicators reduce confusion during waits. | One primary live status; longer waits show the operation and time since evidence, without invented percent-complete estimates. |
| [Carbon AI Chat: ReasoningSteps](https://chat.carbondesignsystem.com/tag/latest/docs/interfaces/Type_reference.ReasoningSteps.html) | Processing details can open during work and close for an answer; a person's explicit open/close choice takes precedence. | Start compact with a live preview. Condense ordinary completed work, but never close details the person opened. |
| [W3C: status messages](https://www.w3.org/WAI/WCAG22/Understanding/status-messages.html) | Status changes must be available to assistive technology without taking focus; overly chatty updates are a problem. | Announce meaningful phase changes in one polite status region, never timer ticks or every tool result. |
| [W3C: animation from interactions](https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html), [pause, stop, hide](https://www.w3.org/WAI/WCAG22/Understanding/pause-stop-hide.html) | Motion needs accessible alternatives and control where applicable. | Honor reduced motion; offer a local pause for continuous activity animation without stopping execution or essential status updates. |
| [web.dev: high-performance animations](https://web.dev/articles/animations-guide) | Transform and opacity usually avoid layout work; layer promotion should be measured rather than applied everywhere. | Animate a tiny indicator and short transitions, not transcript height, Markdown layout or every arriving token. |

These support the interaction principles, not the exact proposed timings or
grouping algorithm. Those are WHIP-specific design choices to validate.

## What exists today

| Current source | Implication |
| --- | --- |
| `packages/app/src/conversation-rows.ts` | Creates one row for every tool call, maps `rlm_exec` to “Starlark execution”, pairs tool results by call ID and deduplicates repeated mailbox digests. It currently drops the explicit tool name/call identity from the public row fields. |
| `packages/app/src/timeline.tsx` | Renders each tool/reasoning/mailbox row as a panel disclosure. `Prose` already uses memoized streaming Markdown, Inter, 14px text and 1.65 line-height. |
| `packages/app/src/reading-list.tsx` | Owns virtualization, selected-text pinning, measurement, reading bookmarks and follow/latest behavior. The shared reading container is 840px wide. |
| `packages/sdk/src/executions.ts` | Already joins recorded cells with bounded live evidence, provides call IDs/status/errors/host summaries and reconciles live-to-history identity. Reuse this instead of decoding RLM results again in app. |
| `internal/daemon/agent_session.go`, SDK `observeExecution`, REPL `statusLabels` | `stream.tool.call` means Writing; `stream.tool.started` means Running; completion and turn terminal events settle the cell. The REPL already renders these states accurately, qualified by connection state. |
| `packages/app/src/conversation.tsx`, `agent-turn-notice.tsx` | Own selected-agent composition and the saved failure display. Pending permissions/questions remain above the composer. |
| `internal/session/runtime.go`, `turn_outcome.go` | Agents have names, lifecycle/blocking state and optional latest-turn status/timing. An idle agent may have a failed latest turn. |
| `internal/rlm/kernel.go`, `internal/daemon/recursive_runtime.go` | `OnHostCall` currently fires **after** the host operation returns. Its event is useful completed evidence, but cannot truthfully identify a long operation while it is running. |
| `internal/rlm/tool.go` | The model-facing tool accepts only `code`, not a short purpose label. Parsing Starlark text would be an unreliable substitute. |
| `internal/session/transcript.go`, `transcript_page.go` | Transcript pages expose message sequences, not a complete turn timeline with start/end boundaries. Historic “Worked for 42s” cannot reliably be reconstructed for every group. |
| `packages/app/src/presentation.ts` | Exports DOM-free conversation presentation to the native mobile app. Changes to row contracts must preserve that consumer even though this visual work targets web/desktop. |

## Proposed interaction

### Activity groups

Example while working:

```text
I’ll trace the connection path and check how the daemon handles it.

› Checking the connection path                       8 executions
  · Reading daemon configuration
    3 file reads · 2 searches completed

  · Connection review                               Working
    Test coverage                                   Waiting for approval

I found the mismatch in the connection defaults…
```

Completed routine work reduces to one quiet disclosure:

```text
› 8 executions · 3 file reads · 2 searches completed
```

Opening it shows a short ordered list of cells and their observed host operations,
with code/output available through existing bounded controls and **Open in REPL**.
Counts refer to observed operations, not unique files unless uniqueness is proven.
For example, repeated reads should say “3 file reads”, not “3 files”.

Rules:

- Group contiguous execution rows for the same runtime/root/agent and known turn.
  User messages, assistant prose, unknown history gaps, turn changes, restart
  markers and notices requiring action are boundaries. Never infer grouping from
  matching display text or an elapsed-time threshold.
- Reasoning and mailbox evidence may share the compact activity region, but retain
  distinct labeled disclosures. Preserve actual visible reasoning; never invent it
  or generate a hidden-thought transcript from tools.
- Preserve authored messages byte-for-byte. Do not collapse prose into a generated
  work summary or reorder it to place all work at the top of the turn.
- Default routine details closed, with one current operation preview while active.
  Manual open/close wins. On completion, remove the live preview only if it does
  not contain focus/selection and the person has not opened the group.
- A failure count and named failed item remain visible when a group is collapsed.
  Recovered errors stay discoverable; a later successful call does not erase them.
- The existing saved turn failure is the authority for a failed turn. Integrate it
  into the visible work status without showing the same full error twice. Keep the
  standalone notice as fallback when no matching group is loaded and in REPL.
- Paginated/incomplete history says “12 loaded executions” or “Earlier activity
  unavailable”, not an apparently complete total. Missing timing means omit timing.

### One live status near the composer

A single unboxed line, aligned with the reading column, answers what is happening
now. It remains visible while reading older messages. It is also the only looping
activity animation; other agent rows use static state icons.

| Evidence | Status example |
| --- | --- |
| Local submission, no receipt yet | Sending… |
| Accepted inbox item, no active turn | Queued |
| Active turn, no more specific evidence | Working… |
| Actual reasoning stream | Thinking… |
| Actual response stream | Writing response… |
| SDK execution cell is `writing` | Preparing an execution… |
| SDK execution cell is `running` | Running an execution… |
| Verified host operation is in progress | Reading files… / Searching the project… / Running a command… |
| Verified `agents.wait` operation is in progress | Waiting for agents… |
| Recorded host-operation completion | Secondary detail: “Last completed: file read” / “2 searches completed” |
| Root finished, descendants still running | 2 agents still working |
| Permission or question pending | Test coverage needs your approval / Waiting for your answer |
| Disconnected or recovering | Reconnecting · activity may have changed |
| Latest turn failed | Connection review failed · View error |
| Turn completed and no outstanding work | Hide the live line; retain the completed group |

State precedence: unavailable/recovering, required human action, failure requiring
attention, then specific active operation/model phase, then generic active/queued.
Concurrent work remains visible as a secondary count instead of overwriting the
important state. A historical handled failure must not permanently dominate a new
running turn. “Waiting” requires evidence of an actual wait, not merely the
existence of children.

Specific current labels such as “Reading files” or “Waiting for agents” use the
new operation-start evidence. A running cell can contain several host operations
or a wait; cell status remains independently Running. If current-operation
evidence is missing, fall back to that cell/turn state without guessing.

Reuse the REPL's client-observed elapsed time when available, or latest-turn
timing for a matching turn. Do not start a fresh historical timer on reload.
Update age every second only for the visible active status; longer ages can use
coarse minute labels. If a later SDK refinement exposes time since meaningful
output, it can support “No new output for 15s”; do not substitute connection
heartbeats or infer a hang. Never invent reassuring stages or percent-complete.

### Named agents

- Show up to three relevant direct child rows: blocked/failed first, then active
  work in stable admission order. A row contains name, textual state and optional
  model/effort in secondary text. Do not use IDs or repeat the full task prompt.
- Keep rows stable as their state changes; do not continually sort by latest event.
  Pin newly attention-worthy rows visibly without shuffling a focused list.
- “Show all agents” opens the existing inspector. Selecting a name uses existing
  scoped child chat/REPL navigation and preserves root draft/reading state.
- Do not hydrate every child's transcript just to populate these rows. Use the
  existing bounded agent metadata and session summaries. Exact whole-tree counts
  come from authoritative summaries; a partial page must qualify its counts.
- Avoid implying every old retained child belongs to the latest prompt. Without a
  recorded turn association, label these as **Session agents**, not “agents for
  this turn”. Inactive unrelated children remain in the inspector.

### Text and motion specification

Keep the self-hosted Inter/JetBrains Mono families. Preserve the current 14px/1.65
prose baseline and 840px column; improve hierarchy through spacing and weight.
Status/group titles: 13px, regular to medium; agent name: 13px medium; metadata:
12px using `surface.secondaryText`. Use tabular numerals for ages and counts.
Code stays 12px in details. Preserve Markdown headings, tables, links and copying.

The concurrent Settings work is introducing `typography.size*`, `codeSize`,
`appearance.motionFast/motionNormal`, configurable fonts and a shared reduced-motion
preference. Use those roles and the central preference reader when implemented;
the pixel sizes here are default design targets, not overrides of user settings.
Check the shared tool-detail preference before choosing a disclosure default.

Groups sit directly on the canvas. Use a 14px chevron, 16px activity icon, 8px
inline gaps, 12px group spacing, 24px between major authored messages, and a quiet
indent for details. Keep existing 6px control/10px panel radii where a control or
panel is actually needed. No additional card around every step. Mobile labels
wrap, metadata moves below, and interactive targets reach 44px.

| Transition | Proposed behavior |
| --- | --- |
| Ongoing work | A small dot cluster fades gently over 2.4s; text retains readable contrast. Start motion only if activity lasts about 1s; show the label immediately. |
| Phase label changes | Existing 160ms motion token; short crossfade without sliding the transcript. Frequent routine updates coalesce; blocked/error/disconnect changes are immediate. |
| Group disclosure | 100ms chevron rotation and 160ms detail fade. One measured layout change, no continuously animated height. |
| Agent admission | 160ms opacity entrance, at most 2px translation; only on genuinely new live admission. |
| Completion | Settle to a static marker, preserve the group’s disclosure choice, then remove the live status when appropriate. |
| Streaming prose | Render incoming text immediately; no artificial typewriter, per-word blur, repeated entrance effects or delayed output queue. |
| History/reconnect/tab restoration | No replayed entrance animation; render restored state immediately. |
| Reduced motion / paused animation / hidden document | Static indicators and immediate state transitions. Execution and status text still update. |

Use a single polite live region for semantic changes. Decorative dots are hidden
from accessibility; timer ticks are not announcements. Maintain keyboard focus,
selection, 200% zoom and contrast across existing themes. A “Pause activity
animation” shortcut should reuse the shared Reduce motion preference introduced
by Settings, not store another conflicting motion setting. It affects decoration,
not execution or essential textual status updates.

## Implementation phases

### 1. Pure grouping and stable identity

Add `conversationActivityRows` in a DOM-free app module, consuming the existing
conversation rows and SDK execution projection. Preserve `conversationRows` for
the mobile consumer initially. The grouping output holds references/identifiers,
not additional copies of tool arguments, output or transcripts.

Carry optional `toolName`, `callId`, `agentId`, `turnId` and provenance through
presentation; never classify by “Starlark execution” display text. Reuse SDK
execution identity reconciliation for calls rather than maintaining a second
live-to-history match algorithm. Keep unmatched/partial evidence explicitly
unknown. If a stable cross-commit message alias is needed, add it at the existing
SDK reconciliation boundary and expose it to the pure projection.

Group keys derive from scope, history revision and stable source-call identity,
not array index or count. Extend reading targets with source-row membership so
an old tool anchor resolves to its containing group. Prepending older members
must preserve the mounted group's identity/anchor; retain a bounded alias when
the earliest source changes. Expanded details should flatten into the same
virtual list, or use a small bounded preview and REPL link, never thousands of
hidden nested nodes inside one virtual row.

Keep disclosure preferences in the existing view-lifetime ownership model, keyed
by scope/group, capped at 128 and pruned with retained data. No saved transcript
cache, new socket, automatic detail fetch, or global state library.

Acceptance: many adjacent cells form one group; prose boundaries remain exact;
duplicate/late events, commit handoff, repeated identical prompts, missing bodies,
pagination and revision changes do not duplicate or misattribute activity.

### 2. Reuse REPL state and add host-operation visibility

Consume `executionRows(state, agentId)` and its existing `writing`, `running`,
`completed`, `failed`, `interrupted`, `cancelled` and `unknown` states. Use
`active_turns`, agent lifecycle/last-turn metadata, pending requests, actual
text/reasoning streams and shared session summaries for the surrounding status.

The daemon emits `stream.tool.started` before execution. SDK `observeExecution`
sets the matching cell to Running, then settles it on tool completion or a turn
terminal event. Copy this ownership model, not the implementation: chat should
consume the same projection, not maintain another event reducer.

Expose completed host-operation names/counts as evidence beneath the live state.
Do not present the last completed host call as the currently running operation.
A Running cell is not a claim that a subprocess is consuming CPU: it includes
host calls and waits, with explicit pending human input taking precedence.

If a reconnect's bounded evidence cannot identify a particular active cell,
the known active turn still supports “Working”. Mark disconnected observations
as paused/stale, as REPL does. Preserve unknown outcomes rather than guessing.

Extend only the missing operation-level lifecycle:

1. In `internal/rlm/kernel.go`, emit a start immediately before dispatching
   `kernel.host.Call`, followed by its terminal result when the call returns.
   Reuse the existing completion callback/measurement. Give each invocation a
   unique occurrence ID within its cell, including repeated calls to the same
   function. Correlate by root, agent, turn, cell call ID and occurrence ID.
2. In `internal/daemon/recursive_runtime.go` and the protocol registry, add a
   distinct start event (proposed `stream.cell.host.started`). Keep the existing
   `stream.cell.host` completion meaning and add optional correlation metadata
   to it. Do not send starts disguised as the old completion event: older
   consumers would render/count them as completed host calls. Add typed fields
   for occurrence, turn, module/operation and timing as needed; preserve existing
   bounded/redacted argument summaries. No raw file contents, prompts or new
   command-parsing heuristic in the status payload.
3. In `packages/sdk/src/executions.ts`, upsert the same host row from start to
   terminal state, rather than appending two rows. Maintain completion-only
   compatibility for old events that lack occurrence IDs. Terminal cell/turn
   events also settle any observed unfinished host rows; distinguish failed,
   cancelled, interrupted and unavailable outcomes. Late or duplicate completion
   cannot finish a different invocation. Preserve the existing count/byte limits.
4. Both the REPL and grouped chat consume this same richer host-call projection.
   Map verified operations to short labels: file reads, project search, command
   execution, browser/computer operations and agent waits. Show a concise
   `module.operation` fallback for an unrecognized operation. Pending approvals
   and questions override an ordinary Running label. A child-count label must
   refer to verified wait targets, not an unrelated session-wide active count.
5. Reuse existing ordered delivery, replay and bounded snapshot presentation.
   Test reload, reconnect and missing-prefix cases. If a start is no longer
   available, show the known cell/turn state rather than inventing the operation
   or claiming completion. A new durable current-activity table is not required
   for this fallback; consider a compact snapshot projection only if continuous
   operation detail across truncated history becomes a product requirement.
6. Generate Go-derived schemas/validators and follow the current protocol's
   additive capability/version rules. Test a newer client against completion-only
   events and the actual older-client behavior for the new event kind. If gating
   is needed, preserve the global event cursor rather than silently dropping
   sequenced events from a subscriber. No execution, scheduling or tool-argument
   schema change is required.

Operation completion means the host call returned. A `shell.run` call can return
a background-process handle, and `agents.spawn` can return while the child keeps
working. Neither return means all underlying work has finished. Similarly, a
host-operation error may be caught by the cell; keep operation, cell and turn
outcomes separate. Existing agent/process evidence determines subsequent work.

This adds roughly one small start event per host invocation and reuses its
existing completion. There are no new per-operation polling loops, per-second
wire events or extra model calls. Keep the optional natural-purpose label change
separate; accurate operation labels work without it.

Acceptance: grouped cells and REPL agree on Writing/Running/terminal state;
slow calls, partial code, permission waits, completion, later turns, reconnect,
missing evidence and child activity cannot create duplicate or stale indicators.
Also cover repeated same-function calls, host errors caught by Starlark, background
process handles, child admission versus child completion, and cancellation while
a host operation is blocked. Observe a deliberately slow operation before it
returns to prove this is live visibility rather than retrospective labeling.

### 3. Shared chat components and lifecycle

Compose app-owned `ActivityGroup`, `AgentActivityRow` and `ConversationStatus`
using existing UI disclosure, icon button, tooltip and theme primitives. Promote
only a reusable activity indicator to UI, with static/reduced-motion states and
a Storybook example. Keep status selection as a pure function of SDK truth and
shared summaries; connection state always qualifies execution observations.

Integrate in `timeline.tsx` and `conversation.tsx`; reuse `AgentTurnNotice`,
requests, REPL routing and inspector actions. Do not add a second composer or
approval flow. Root reply completion cannot hide still-running child work.

Use latest-turn daemon timing only where it actually corresponds to the displayed
work. Old groups without reliable boundaries show execution counts. Durable
per-turn “Worked for…” on arbitrary old history would require transcript-to-turn
sequence ranges and bounded turn summaries; defer that schema expansion until
there is a demonstrated need. Never derive historical durations from reload time
or add parallel agent durations together.

Acceptance: root-only work, several children, root finished/child active, stopped
child, failed turn without cells, later success, older history, partial agent
pages and disconnected state remain clear with one main activity animation.

### 4. Typography, motion and reading behavior

Apply the measurements above with StyleX and the existing motion tokens. Preserve
stable Markdown component identity; timers must not rerender the whole transcript.
Keep age updates in the visible status component, pause them on hidden tabs and
avoid animation on offscreen groups. Respect strict production CSP; use extracted
keyframes or native Web Animations, with no injected style dependency.

Integrate source-to-group anchors with `ReadingList`, preserve selection/focus
and the person's manual disclosure state, and keep the existing Latest action.
When reading old messages, new activity cannot force scrolling or close details.

Acceptance: long and narrow labels, both light/dark theme families, keyboard,
reduced motion, 200% zoom, selection, prepend, tabs/split panes and live commit
transitions. Inspect an actual animation recording; screenshots alone are not
motion acceptance.

Repeat typography/anchor checks at the minimum and maximum configured UI/code
sizes and with the System font choices from the concurrent Settings work.

### 5. Fixtures, packaging and delivery

- Extend existing timeline/reading-position tests and SDK execution/state
  fixtures, plus kernel host-callback and daemon event tests for the new starts
  and correlated completions. Reuse the current fixture infrastructure.
- Add a deterministic activity browser fixture: delayed provider, many cells,
  slow operation, multiple children, blocked request, failure, restart and late
  events. No paid model calls, live agents, editor launches or daemon interruption.
- Run app/UI/SDK checks, applicable Go/race tests and repository-required checks.
  Check native mobile imports/typechecking after portable presentation changes.
- Exercise Chromium, Firefox and staged Electron; perform an explicit VoiceOver
  check and state actual Safari coverage. Validate themes, narrow geometry, CSP,
  absence of orphan timers/subscriptions and unchanged retained-data bounds.
- Use one focused performance trace to check transcript rerenders and animation
  layout work. Do not start another startup-benchmark project.
- Build the web renderer once and verify the same manifest in Go embedding and
  Electron packaging. Bundle the new daemon event support with the renderer;
  completion-only remote daemons retain generic Running status until upgraded.
  No new database schema is planned for operation visibility. Installation follows the existing
  release process as a separate action.
- Update `docs/frontend.md`, UI/SDK references and the feature inventory when
  implemented. This proposal does not itself change canonical architecture.

## Scope boundaries and decisions

Recommended default: compact groups with a live preview, raw details always
available, authored updates fully visible. The visual target is web and desktop;
native mobile stays compatible and can adopt the same pure grouping later.

This reorganizes the observation of work. It does not change how the agent
schedules/delegates, add a new task manager, fabricate progress, infer hidden
reasoning, add automatic retries, or rewrite stored conversation content.

## Conversation polish — 2026-09-09

- Fixed compounded spacing from invisible message actions, viewport-based touch
  sizing, and paragraph margins adjacent to action controls. Fine-pointer actions
  use a stable 28px footer; actual touch input keeps visible 44px targets. Prose
  owns its last-child margins. Expanded work uses an inset rule, compact rounded
  focus target, and a left-aligned REPL action without redundant status headings.
- Internal mailbox digests now join following work at a new delivery boundary.
  Isolated digests appear as Agent updates without a success claim. Raw messages
  remain separately expandable, even at Detailed density. Authored replies are
  unchanged, including redundant replies: no text heuristic hides them.
- Full app suite: 365 tests passed; app/UI typechecks passed. Chromium and Firefox
  passed the recorded-mailbox fixture at 530/390/320px, with keyboard disclosure,
  copy focus, touch target checks, and no runtime/CSP errors. Existing live
  activity and user-message browser workflows also passed. The older queued-send
  test was updated to use Enter, matching the current Pause-button composer.
- Production renderer:
  `15e6551c2369900576641e60d173a6dd056e3dcd4ac3461d56f423efa1560cba`.
  Screenshots and browser evidence: [polish](evidence/polish/).
- Signed package 0.1.3 / `local-chat-polish-15e6551c` passed package verification.
  Installation is deferred: a root turn remains active, so the installed app
  and canonical daemon still use `local-theme-picker-6d99e1b2`.

## Optional later enrichment

Host-operation visibility is included in phase 2. These remain optional:

- **Natural purpose labels:** optionally add a bounded single-line `summary`
  argument to `rlm_exec`, saved with existing arguments and rendered as plain
  text. A model-written purpose is intent, not an execution-status claim. No
  separate model call is needed. This is independent of grouping and Running.
- **Historical turn durations:** add reliable transcript-to-turn boundaries only
  if arbitrary old groups need durable elapsed-time summaries. Counts and actual
  observed timing already cover the first release without that schema expansion.
