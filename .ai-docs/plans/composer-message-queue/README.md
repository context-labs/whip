# Composer message queue

Status: Implemented, 2026-09-18. See [validation evidence](VALIDATION.md) for actual checks and remaining manual validation limits. The original phased scope is retained below as the implementation record; `docs/frontend.md` describes the maintained architecture.

This document records scope, implementation phases, and acceptance criteria. [docs/frontend.md](../../../docs/frontend.md) remains the maintained architecture guide.

## Outcome and working defaults

While an agent is working, sending a message places it in the existing durable queue and shows it in a compact strip above the composer. The person can let it run normally, choose **Steer** to deliver it to the current turn at its next safe boundary, or remove it before delivery.

- Apply the new interface to shared desktop/web chat, for root and selected child conversations. Preserve native mobile presentation and existing direct-steer APIs.
- Preserve automatic FIFO delivery of ordinary queued messages. Steering one message leaves the relative order of the others intact.
- Start with **Steer**, **Remove**, and read-only message/attachment preview. Editing, reordering, manual hold, and an otherwise empty overflow menu are outside this first implementation.
- Sending while idle remains normal submission. The daemon decides whether another client or an ending turn has changed the actual delivery timing.
- Keep Stop available during an active turn. With a populated draft, provide a visible queue-send button as well as Enter-to-queue; Shift+Enter and IME behavior stay intact.
- Accepted messages belong to the daemon, independently of the browser window. Disconnecting or closing a tab does not remove them.

FIFO delivery and the initial Steer/Remove controls are the recommended defaults from the research; neither requires a new queue preference.

## Research that determines the implementation

| Existing behavior | Consequence |
| --- | --- |
| [Composer](../../../packages/app/src/composer.tsx) selects Queue or Steer before submission; [chat-submission](../../../packages/app/src/chat-submission.ts) issues a new command for either path. | Replace the dropdown, but retain the shared submission and draft-acceptance path. |
| [StartAgentTurn](../../../internal/session/agent_turn.go) claims one waiting inbox item in sequence order. | Reuse this scheduler and durable inbox for automatic delivery. |
| [ClaimSteers](../../../internal/session/input.go) and [pullSteers](../../../internal/daemon/agent_session.go) consume steer input at a model-loop boundary. | Steer requests future delivery to that boundary; it is not a tool interrupt. |
| [cancelInputCommand](../../../internal/daemon/cancel_target.go) can cancel the active turn once an input is running. | Removing a queue entry needs a queued-only operation. Never implement promotion with cancel-and-resend. |
| Inbox snapshots lack durable human-origin and originating-command fields sufficient for every child submission. Parent follow-ups use the same input kinds. | Add explicit provenance and correlation where necessary; do not infer human intent from text, captions, or kind alone. |
| [conversationRows](../../../packages/app/src/conversation-rows.ts) already renders pending input as transcript bubbles. | Partition queue and transcript presentation so one submission is never displayed twice. |
| [SubmittedInputs](../../../packages/app/src/input-presentation.ts) bridges local admission to authoritative inbox/history. | Extend this bounded bridge; do not create a second durable queue or a React event reconciler. |

## Phase 1 — Durable queue identity and safe mutations

**Goal:** Establish the daemon behavior before changing the composer.

1. Extend existing inbox storage with the minimum additive metadata needed for human origin, original command correlation, and a pending steer's intended turn. Keep message bodies and attachment references unchanged. Maintain the original submission's frozen request and digest.
2. Capture provenance for root human submissions, human child submissions, and internal/parent inputs at their existing admission points. Backfill older entries only when a durable join proves the relationship; leave ambiguous entries unclassified and available through their existing presentation.
3. Implement a transactional promotion of one queued human input, scoped by root, recipient agent, and inbox sequence, and fenced to an expected active turn. The selected item becomes eligible for that turn's next permitted boundary. Existing permission waits and running tools are not bypassed.
4. Implement transactional queued-only removal. Once the item has been claimed, return an already-started outcome without canceling work. A promoted item remains removable only until its boundary claim.
5. Serialize promotion, removal, boundary claims, and ordinary claims through the existing store/actor ownership. Multiple promotions are consumed in inbox sequence order. Each selected input can be claimed only once.
6. If the target turn ends before consuming a promoted input, clear the expired steering intent and preserve ordinary FIFO eligibility. Apply this to completion, failure, cancellation, and restart recovery. Respect existing explicit session/subtree stop or deletion policies; never revive terminal queue entries.
7. Settle correlated root submission commands consistently when a queued input is removed. Preserve the different child contract: `agent.submit` can already have succeeded after enqueue, so later removal changes the inbox item, not that completed command's result.

**Likely files:** `internal/session/input.go`, `runtime.go`, `agent_turn.go`, `migrations.go`, `command_cancel.go`; daemon admission, actor dispatch, and boundary delivery files.

**Exit criteria:** Store and daemon tests prove no lost/duplicate input, no accidental turn cancellation, intact attachment references, unchanged ordinary queue order, and no steering of a replacement turn. Include cancellation/claim races, root/child sequence collisions, and restart recovery. Run affected Go race tests.

## Phase 2 — Protocol and SDK support

**Depends on:** Phase 1.

1. Add typed durable control commands, proposed as `inbox.steer` and `inbox.remove`, through the Go registry and generated TypeScript contract. Root scope comes from the existing command envelope; parameters identify the agent and inbox sequence, plus the expected turn for Steer.
2. Return distinguishable outcomes for a successful mutation and for an item already claimed, removed, or no longer eligible for the intended turn. Command admission alone is not proof that promotion/removal has completed.
3. Apply existing runtime authority checks to the exact target and reject internal inputs. Support the ordinary multi-client conversation workflow without introducing a separate permission system. Control-command retries retain their own original command identity.
4. Include necessary optional origin, correlation, delivery-intent, and bounded preview fields in inbox snapshots and pages. Use existing content references for full message/attachment access. Large referenced inputs must still have a useful text or attachment preview without fetching the entire payload.
5. Add typed SDK methods and reconcile mutations through the existing session view, events, revision checks, and coalesced refreshes. Avoid additional child subscriptions or per-row polling.
6. Preserve snapshot/page limits and the SDK's 8 MiB session budget. Reuse inbox collection paging; expose partial state and additional-page availability rather than claiming an incomplete list is the entire queue.
7. Advertise support through the existing capability/operation negotiation. An older daemon retains the legacy dropdown and pending transcript presentation. Enable the new interaction only when its safe controls are supported.

**Likely files:** `internal/protocol/runtime_registry.go`, `runtime_contract.go`; session snapshot and collection paging; `packages/protocol`; `packages/sdk/src/session.ts`, `state.ts`, and existing negotiation/recovery code.

**Exit criteria:** Contract generation and drift checks pass. SDK tests cover snapshot/replay convergence, missing acknowledgements, repeated controls, paging, reconnect, runtime/recipient isolation, and old-daemon compatibility. No command is automatically resent with a fresh identity.

## Phase 3 — One presentation of each queued message

**Depends on:** Phase 2.

1. Derive the selected recipient's human queue from authoritative SDK inbox data. Keep daemon reconciliation in the SDK and display projection in the app.
2. Bridge pre-admission submissions into that list using `SubmittedInputs`. Correlate by runtime, root, agent, command identity, and inbox sequence; never match by text. Maintain stable keys through Sending → Queued → Steering → transcript handoff.
3. Extend local previews with bounded attachment descriptors needed to display image-only and mixed messages. Reuse scoped content readers and existing thumbnail/dialog behavior. Do not retain a second copy of uploaded image bytes or persist prompt bodies in command recovery records.
4. Partition pending queue rows from transcript rows. A claimed input appears once in the transcript, and committed history later replaces its live presentation without a duplicate or a temporary disappearance. Boundary-consumed steers retain their recorded chronological position.
5. Handle transient absence in a bounded or stale snapshot correctly. An omitted row is not proof of removal or delivery. Use authoritative events/status and existing paging rather than guessing or hiding accepted work.
6. Retain the existing 32-entry / 1 MiB local-preview bound. Prefer evicting confirmed previews; keep unresolved delivery visible. Disable controls until the exact admitted inbox identity is known.

**Likely files:** `packages/app/src/input-presentation.ts`, `conversation-rows.ts`, `conversation.tsx`, `chat-submission.ts`, and the receipt-to-preview bridge in `runtime.ts`.

**Exit criteria:** Projection tests cover identical messages, multiple images, rapid turns, child submissions, reload after local previews are gone, partial pages, uncertain delivery, and authoritative history handoff. Each message has one visible location at every settled state.

## Phase 4 — Composer queue interface

**Depends on:** Phase 3.

1. Add a focused app component for the queue strip above the composer. Reuse shared buttons, icons, tooltips, dialogs, and StyleX theme tokens. Do not add a new state store or animation dependency.
2. Show compact rows with a queue icon, one-line text preview, attachment indicators, Steer, and Remove. Image-only rows use meaningful attachment labels and small previews. Open a read-only detail view for full text and multiple attachments.
3. Keep three compact rows visible before scrolling the strip; make its height responsive to narrow/short panes. Expose more loaded/paged items through existing bounded paging rather than mounting an unlimited list.
4. Remove the active-turn delivery dropdown on supported daemons. Route active-turn Send/Enter through ordinary queued submission. Retain Stop; render a separate queue-send action when the draft is populated. Idle and first-message attachment submission keep their current behavior.
5. Show truthful states: Sending, Checking delivery, Queued, and Steering. Disable conflicting controls while a mutation is unresolved. Keep a row visible until authoritative state confirms the outcome; show action failures beside it with the existing explicit recovery flow.
6. Make Steer unavailable when the selected agent has no current steerable turn. A stale-turn response refreshes the row and explains that the turn ended; it does not automatically redirect to another turn.
7. Removing or steering an entry must not overwrite the currently typed draft, remove new draft attachments, or revoke content still owned by accepted work. Closing/navigating a conversation leaves its queue intact.

**Exit criteria:** Component tests cover all controls, multiple queued messages, image-only input, unreadable previews, failed mutations, and unchanged in-progress drafts. The interface matches the supplied layout using Whip's palette in light and dark themes.

## Phase 5 — Reading stability and accessibility

**Depends on:** Phase 4.

1. Integrate strip height changes with the existing chat reading owner. Preserve the anchor when the person is reading older messages; when following the tail, account for the changing available viewport without competing scroll adjustments.
2. Keep queue projection independent of ordinary draft keystrokes. Avoid remounting transcript rows or remeasuring the whole conversation because text changed in the composer.
3. Make all actions available by keyboard with message-specific accessible names. After removal, retain focus on a sensible adjacent action or return it to the composer if the list is empty. Background delivery must not steal focus from the draft.
4. Announce delivery/action status changes politely without reading streamed content repeatedly. Use the existing preview-dialog focus behavior and reduced-motion preferences.
5. Exercise disconnected/stale views: accepted rows remain readable, mutation controls wait for a live authoritative connection, and reconnect reconciles the same entries.

**Exit criteria:** Browser interaction tests show no per-keystroke jump, no forced bottom scroll when reading above, no focus loss during refresh, and no duplicate queue copies across navigation. Keyboard and screen-reader checks pass in narrow panes and enlarged text.

## Phase 6 — End-to-end acceptance and rollout

**Depends on:** Phases 1–5.

Extend an isolated product fixture with a controllable active turn and explicit tool/boundary gates. Use deterministic synchronization, not timing sleeps, for queue races. Do not restart the developer's main daemon or mutate real conversations for testing.

Required workflows:

| Scenario | Required result |
| --- | --- |
| Queue A, B, and C; let the turn finish | A, B, and C start in ordinary FIFO order. |
| Queue A, B, and C; steer B | B is consumed by the targeted turn at its next boundary; A and C remain ordered. |
| Remove A just as its turn starts | Either A is removed before claim, or removal reports already started; its turn is never canceled. |
| Target turn ends before B is consumed | B returns to ordinary eligibility at its original position; it cannot steer a later turn. |
| Two clients steer/remove the same input | One authoritative outcome; no duplicate delivery or resurrected item. |
| Disconnect after sending or clicking Steer | Recover the existing commands and queue identity; do not create replacements. |
| Restart with queued messages and a pending steer intent | Accepted inputs remain accounted for; obsolete turn targets cannot take effect. |
| Send text, image-only input, and multiple mixed attachments | Full payload is preserved through queueing, steering, preview, and transcript display. |
| Switch root/child/host while typing | Queue, draft, and controls remain correctly scoped. |
| Open a long conversation and grow/shrink the queue | Reading position and typing remain stable. |
| Connect to an older daemon | Existing submission UX remains usable; unsafe queue mutations are not attempted. |

Run affected Go and race suites, generated-protocol checks, SDK tests/acceptance, app tests, web production checks, and desktop checks. Also check mobile compatibility for the additive shared contract/SDK changes, without changing native presentation.

Run the deterministic browser workflow in Chromium, Firefox, and Electron. Inspect screenshots for light/dark themes, increased contrast, reduced motion, narrow panes, and large text; exercise keyboard and screen-reader behavior explicitly. Recheck retained-data/subscription bounds and the existing long-conversation input-latency fixture.

Update `docs/frontend.md`, the relevant feature documentation, and SDK usage examples with implemented behavior and limitations. Record actual validation evidence alongside this plan; do not mark unrun checks as passed.

**Release gate:** All queue identity, delivery race, attachment, and recovery tests pass before the new composer path is enabled. Backend/protocol/SDK can land first; the UI is enabled through advertised support once the complete interaction is ready.

## Delivery boundaries

Each phase should be a reviewable change with its own targeted checks. Storage/daemon and protocol changes may land together where generated types require it. Phase 3 projection changes should retain the old presentation until the Phase 4 interface is ready, so intermediate builds do not hide pending messages.

The finished feature has one daemon-owned queue, one SDK reconciliation path, and one visible presentation per input. Existing working-tree changes must be preserved throughout implementation.
