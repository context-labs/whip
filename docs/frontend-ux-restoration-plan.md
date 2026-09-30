# Restore frontend UX over the native SDK

> **Status correction (2026-09-29):** restoration is not complete UX parity.
> The implementation and focused evidence described below remain useful, but
> the user has requested a complete new parity inventory and plan before code
> changes, with each proposed protocol-driven UI change discussed individually.
> The [desktop/web inventory](frontend-desktop-web-parity-inventory.md) now records
> the completed source inventory, approved onboarding corrections and remaining
> decisions. The [handoff](backend-redesign-handoff-2026-09-29.md) preserves the
> initial audit and authorization. Historical status below is retained.

Status: implementation completed, 2026-09-29. The planning checkpoint was
draft #287. See the [implementation record](frontend-ux-restoration-progress.md)
and [acceptance index](frontend-ux-restoration-acceptance.md) for tested slices,
exact comparison evidence and explicit limits. Findings below preserve the
planning-time state; frontend.md remains the current architecture authority.

The goal is the latest development frontend's experience over the current native
backend and SDK. Restore established markup, interactions, loading treatment and
presentation with the smallest necessary binding changes. Small SDK/backend
additions are in scope when they are necessary to preserve that experience.
Rare, truthful recovery states are an accepted exception; ordinary use should
feel like the previous application.

## Reference, stack and decisions

| Item | Exact reference |
| --- | --- |
| Implementation foundation | `446160bb49ce00334de4d9b70030949f5e6f9702`, branch `codex/backend-redesign-child-policy-inheritance`, draft [#283](https://github.com/context-labs/whip/pull/283) |
| Planning branch | `codex/frontend-ux-parity-plan`, stacked directly on that branch |
| Approved UX reference | Development `12f0ea0768b7d769765596c35c049fe80edfaeba` plus its captured 14-file working-tree overlay |
| Historical migration baseline | `e3fed9c91918d9c36766dd47d878c1b5466238d1`; useful for attribution, not the complete UX target |
| Overlay identity | SHA-256 `04255978030cbe6289a7eeb2d02b92d1f47c7d52361a0d3ef0f6f6a70140c7c6`; per-file hashes in [reference manifest](frontend-ux-reference.json) |

The user explicitly selected the latest development UX, authorized proposing
small SDK/backend additions, and accepted brief inline recovery when an outcome
really is unknown. The remaining permission-scope question is recorded below.

The original development checkout has not been reset, checked out, staged or
edited. A private snapshot contains the exact patch, copies of the 14 changed
files, status and manifest. Its local location is in the reference manifest.
This PR publishes hashes and paths, not the uncommitted source patch. Before
implementation, reconstruct a separate reference checkout from the approved
commit and overlay, verify its hashes, and record its build identity. A remote
implementer needs an approved copy of that private snapshot; hashes alone cannot
reconstruct it. Do not silently substitute a moving development checkout or the
older baseline. If the private capture is unavailable, reacquire and identify
the reference before claiming a comparison.

Keep the provider output-limit correction from [#282](https://github.com/context-labs/whip/pull/282)
and the child-policy inheritance correction from #283. Do not merge, deploy,
replace either installed application, change runtime configuration or run live
inference as part of this planning task. Later verification uses disposable
profiles and runtimes. There is no new performance optimization campaign.

## Findings that determine the approach

The migration changed the existing frontend in place; it did not replace it
with an unrelated application. All 140 original app source files remain, and
the original app StyleX files were unchanged in the audited migration. Much of
the visual foundation can therefore stay. The larger regressions are at the
data-to-view boundary: readiness gates, transcript projections, execution
identity, provider workflows and observation lifecycle.

The previous [migration plan](backend-redesign-plan.md) required retained
capabilities and manual acceptance. Passing tests for the new contract did not
establish that the old interaction expectations survived. Some fixture ports
changed expectations to match the new behavior. In particular, durable displayed
reasoning was real old behavior, not invented test data: the old
`internal/daemon/agent_session.go`, `internal/daemon/transcript_presentation.go`
and `internal/session/presentation_history_test.go` at the historical baseline
record and test it. The [activity audit](../apps/web/scripts/native-chat-activity-audit.md)
now corrects that earlier characterization. The native implementation currently
retains reasoning only in active, in-memory preview.

The approved reference also contains additional UX changes made on the development
branch after the migration baseline: completed-response footers and history
actions, an explicit Default reasoning choice, unified Projects navigation,
compact agent settings, conversation/summary model controls, remote folder
creation and refined dialogs. The captured overlay adds tab/menu/keyboard and
overlay refinements. Comparing only with `e3fed9c91` would lose those improvements.

## Minimal-change rules

1. Start each workflow from its approved old component and interaction test.
   Preserve layout, copy, focus, keyboard behavior, disclosure state and loading
   footprint. Change bindings where the SDK contract requires it. Review the
   resulting diff against both the old reference and current native branch.
2. Keep `SessionView`, `ExecutionView`, runtime leases, `DurableCommand`,
   `RecoveryJournal`, exact identities/revisions and native authority rules.
   Put protocol mechanics in the SDK; use small pure presentation functions and
   app hooks for product decisions. Do not recreate `WhipClient`, `RootSnapshot`,
   the retired event reducer, a general legacy facade or a second transcript store.
3. Reuse current modules before creating abstractions. Restore old JSX/handlers
   selectively, not whole mixed commits or broad reversions. Preserve existing
   StyleX, Markdown, motion, `ReadingList` and reading-position behavior unless
   a specific reference comparison proves that they need an edit.
4. Keep ordinary screens free of revisions, admission receipts, cell IDs and
   protocol lifecycle explanations. Put diagnostic evidence in details. Never
   turn missing data into a false idle/success/unavailable outcome to simplify UI.
5. Keep ownership and retention explicit. All display identities are scoped to
   runtime/session/attempt as appropriate; exact integers remain decimal strings.
   A projection does not acquire execution or cancellation authority.
6. Old product assertions remain expectations. Change fixture setup and transport
   as needed; any changed user-visible assertion needs a reason and a parity-gap
   entry. Hiding a feature or deleting its test does not complete restoration.

These are proposed implementation constraints. [frontend.md](frontend.md) remains
the current architecture guide and must be updated with each implemented change,
including any changed SDK retention contract.

## Restoration map

Paths below are current files; their versions at the approved reference define
the old presentation unless stated otherwise.

| Workflow | Restore or retain | Native binding and smallest necessary change | Acceptance |
| --- | --- | --- | --- |
| Startup and welcome | Existing `StartupScreen`, geometry, motion, escape behavior; no flash of provider setup for a configured host | [runtime.ts](../packages/app/src/runtime.ts), [welcome.tsx](../packages/app/src/welcome.tsx), [provider-readiness.ts](../packages/app/src/provider-readiness.ts): distinguish pending/usable/needs-setup/error; remove nonessential catalog/preset/MCP warming from the blocking path | A1 |
| Session opening and observation | Old content slots, cached conversation during refresh, independent root/child panes | [conversation.tsx](../packages/app/src/conversation.tsx), runtime paired leases: aggregate native selected/root/tree/queue status in a small app hook; verified usable transcript need not await unrelated metadata | A2 |
| Submission, queue and recovery | Old composer focus, draft lifecycle, recipient selection and familiar pending treatment | Keep [chat-submission.ts](../packages/app/src/chat-submission.ts), [recovery-storage.ts](../packages/app/src/recovery-storage.ts), native durable commands; move mechanics out of JSX without changing their semantics | A3 |
| Permissions | Readable requester/action/path, focused approval card, queue count and ordinary controls | [requests.tsx](../packages/app/src/requests.tsx), [input-presentation.ts](../packages/app/src/input-presentation.ts), [permission-mode.tsx](../packages/app/src/permission-mode.tsx): derive display from actual operation metadata; preserve exact native decision/grant scope | A4 |
| Provider onboarding | Previous recommendations, method choice, focused key form, Back/Cancel, account context and suggested model action | [provider-connections.tsx](../packages/app/src/settings/provider-connections.tsx), [provider-login.tsx](../packages/app/src/settings/provider-login.tsx), [provider-setup.tsx](../packages/app/src/provider-setup.tsx): existing presets, setup, login and account APIs; G2/G3/G10 below for missing features | A5 |
| Configuration | Old model/effort/permission controls and one Save per form; latest Summary model and Compact at controls, engine/limits/import preferences | [configuration.tsx](../packages/app/src/settings/configuration.tsx), [provider-defaults.tsx](../packages/app/src/settings/provider-defaults.tsx), [mcp-import.tsx](../packages/app/src/settings/mcp-import.tsx): existing selections/settings/source policy; G4/G11 for atomic saves and exact value semantics | A6 |
| Activity presentation | Compact Read/Search/Run/Browser/agent subjects, details on demand | [transcript-activity.tsx](../packages/app/src/transcript-activity.tsx), [timeline.tsx](../packages/app/src/timeline.tsx), [conversation-rows.ts](../packages/app/src/conversation-rows.ts): pure allowlisted projection of native arguments/results, friendly status labels | A7 |
| REPL and stream settlement | Old writing/running/completed card, code rather than JSON wrapper, live clock, stable selection and disclosure | [repl-view.tsx](../packages/app/src/repl-view.tsx), [execution-time.tsx](../packages/app/src/execution-time.tsx), [execution-state.ts](../packages/sdk/src/execution-state.ts): stable projected rows, exact body reads, G1 for durable ordered parts | A8/A9 |
| Reading and history | Old follow/Latest behavior, bookmarks, split-pane independence and response footers | Existing reading components; extend [conversation-history.ts](../packages/app/src/conversation-history.ts) with an exact group-end resolver for response actions, preserving native revision/tail checks without numeric sequence arithmetic | A9/A10 |
| Terminal | Existing Ghostty view, host shell configuration and responsive output | [terminal-view.tsx](../packages/app/src/terminal-view.tsx), [terminal-open.ts](../packages/app/src/terminal-open.ts), [terminal.go](../internal/terminal/terminal.go), [terminal protocol](../internal/protocol/terminal.go): human-shell environment policy and bounded output observation, G5 | A11 |
| Latest development polish | Latest tabs/menus, keyboard context menus, response controls, Projects, natural-height dialogs, compact agent options and folder picker | Selectively port the reference component hunks and tests; G6 for remote folder creation. Do not import old data plumbing or unrelated development changes | A10/A12 |

### Loading, submission and permission details

Current startup waits on inventory, then multiple metadata reads, then readiness.
Current conversation readiness gates connected content on selected/root/tree
observations; the queue read starts afterward and is independent. Startup and
session gates expose internal coordination as visible waiting. Reuse the existing
layout while each required piece resolves; keep queue loading independent;
retain valid content during refresh and place a failed metadata read at its own
control. A pending provider-readiness response is not evidence that setup is
required. A configured route is not evidence of tested inference either: keep
the ordinary flow simple without making a false validation claim.

Preserve the current submission behavior that already meets the requirement:
admission clears the relevant authoring surface, duplicate submission is locked,
and an unknown acknowledgement retains the exact original payload and draft.
Persist recovery evidence before sending; reconnect must not automatically
replay a mutation. Storage failure after acceptance must not re-enable sending.
Keep ordinary status quiet; show brief Check/Retry recovery only when needed.
Do not restore the old recovery store's silent eviction of unresolved records.

`permissions.resolve` is an operation-scoped idempotent decision, not a journaled
`DurableCommand`. Hold its exact operation and decision in the pending form across
an unknown acknowledgement; check or repeat only that decision. Background
updates must not silently rebase a pending approval onto another request.
Preserve #283's same-workspace default-child Full Access inheritance, explicit
restrictions, revision expiry, root-only questions and computer/MCP consent.
The old host-wide Remember rules are not equivalent to exact native standing
grants; the scope decision below must be explicit.

### Providers and configuration

Restore the previous Connected/Disabled/Needs attention groupings and everyday
forms. Native route/declaration construction belongs behind those forms. Keep
custom endpoints, credential references and raw declarations in advanced
configuration. Account APIs already expose useful email/team/project/plan
metadata; showing those does not require another backend endpoint.

For canonical Inference.net/OpenRouter key setup, reuse `setupProviderKey` and
the same publication ID through an uncertain retry. Reuse existing login flow
IDs, cancellation/expiry behavior and account operations. `setProviderDefaults`
already atomically saves provider/model/effort/model settings. Preserve that
operation; a combined save with permission defaults needs G4. The old Execution
Settings form also saved engine, summary model, compaction threshold, limits and
import preferences together. Include that form's atomic save in G4 rather than
quietly splitting its action. Restore the
latest explicit Default reasoning choice; unspecified effort must not be
silently presented or serialized as an explicit Off choice.

Read-only setup must not send inference, refresh a catalog, start login, publish
a route or execute a command credential. For G3, return bounded non-secret
evidence of approved environment/account candidates and publish only after the
person chooses Use. Check the meaning of `unchecked` command credentials and
refreshable subscriptions before declaring them unusable. Restore useful
source labels only where actual source evidence exists.

Keep Disable, Disconnect and removal distinct. The old
`internal/daemon/provider_disconnect.go` cleared WHIP-owned credentials and
interrupted pending login/catalog state while preserving externally owned
credentials. Current `removeProvider` deliberately preserves key files; it
cannot implement the old Disconnect button by relabeling alone. Reuse managed
account logout and add source-aware owned-key lifecycle handling for G10.

For G11, preserve explicit default intent rather than substituting a numeric
value: old goal/retry zero selected defaults, while native zero continuations
disables continuation and its public attempts field accepts only 1–5. The old
`MaxRetries` actually counted total attempts despite its label; do not blindly
add one. Extend the native default representation as necessary and characterize
boundary values before choosing a compatibility mapping. Values above the native
attempt ceiling need a contract change or a specifically accepted difference.
The Claude/Codex toggles already exist in native MCP source policy; restore their
reference placement/save behavior through that owner. Retain explicit native
import/connection/grant boundaries, and flag any old automatic-import behavior
that cannot map to those boundaries instead of disguising it as a presentation fix.

### Conversation, RLM and execution observation

Restore compact summaries from captured operation arguments/results. The old
`internal/rlm/host_display.go` contains reusable allowlisted display rules: paths,
search patterns, shell commands, browser URL/query and agent name. Port those
small rules as a pure display mapper with the old bounds; do not require another
server presentation service. Keep denied and uncertain states distinct even
when mapping ordinary dispatched/succeeded states to Running/Done.

Restore the old REPL card structure. Reuse the old bounded partial `executionCode`
parser at `packages/sdk/src/executions.ts` in the reference for actual streamed
code, and the current `ExecutionTime` for live duration. A provisional display
row must not pretend to be an executable persisted cell.

Use one stable display key through partial arguments, committed call, running
cell and completed result. Current `call.id || call.index` can change as a
provider streams the ID. G1 should assign a bounded per-attempt display slot at
first appearance and carry it through committed presentation. Join with exact
message/call/cell identity, never matching text or timing. Preserve disclosure,
DOM selection and reading anchors; avoid a separate raw-arguments footer that
disappears when the real card arrives.

Retained REPL bodies already exist in canonical messages. Reuse loaded
`SessionView` data first; otherwise promote/reuse the validated exact-message
read in `readLargeMessage` behind the SDK. `context.read` provides bounded chunks
and owner/message/turn/group/sequence/retirement metadata. Validate all of it and
decoded parts. Hydrate only retained/visible execution rows, count bytes and
items in an explicit budget, and release them with the lease. Update the current
ExecutionView "never copies transcript bodies" invariant if it now holds these
bounded projections; do not leave contradictory documentation.

Chat and execution paging must cooperate behind the interface. Loading older
executions should load their exact bodies without making the person page two
unrelated windows. Preserve the selected older window during background updates;
at the retention limit evict from the opposite edge with a stable anchor and
the ability to reload. Do not replace bounded retention with an unbounded cache.

Fix identity and handover before changing observation cadence. Then compare the
same held stream to the reference. If visible cadence still regresses, have
active execution reads follow source activity with one coalesced in-flight read
and bounded active-turn reads, retaining immutable settled evidence. Do not
merely lower all polling intervals or reopen the completed performance project.

Restore the latest completed-response timestamp/copy/fork/rewind footer. Native
history can have non-contiguous sequences: use the actual last canonical record,
`group_id`, captured revision and observed tail. Current `historyBoundary` resolves
the boundary before an exchange; extend it or add a small group-end resolver for
response keep-through actions. Never copy old `endpoint + 1` arithmetic. If the boundary is incomplete, defer
that mutation control while retaining copy and valid content.

## Parity gaps and proposed prerequisites

These are work items, not reasons to accept a degraded ordinary experience.
No ordinary workflow is known to be inherently impossible with the authorized
small contract additions. Some historical information cannot be recovered.

| ID | Gap and evidence | Proposed resolution / limit |
| --- | --- | --- |
| G1 | Display reasoning and original ordered stream parts are absent from durable native messages; preview is in memory. See [observation](../internal/runtime/observation.go), [runner](../internal/runner/runner.go), [model](../internal/model/model.go) | Add bounded versioned presentation metadata owned by canonical message/attempt settlement; details below. Previously discarded reasoning cannot be reconstructed |
| G2 | Provider route/declaration has no enabled state. See [host configuration](../internal/config/host.go) and [provider protocol](../internal/protocol/providers.go) | Add persisted enable/disable with revision-checked edit and readiness/dispatch enforcement. Removing a route or hiding its row is not Disable |
| G3 | Presets list environment variable names but do not report available unconfigured credentials | Add bounded read-only candidate evidence for approved sources, no secret values; explicit Use publishes configuration. Exact historical source labels require preserved origin metadata, otherwise show only accurate current labels |
| G4 | Provider defaults/permission defaults and execution defaults/summary model/MCP preferences are now separate mutations, unlike the old category forms | Extend native defaults editing with combined validation and one host revision/write per original form. Cover both Providers and Execution Settings. Sequential hidden writes cannot promise all-or-nothing success |
| G5 | Human terminals inherit a filtered process environment; output is read by pages without a wait/push contract | Separate human terminal environment from agent execution policy. Investigate existing output notification for bounded wait-on-read or an equally small observer, preserving epochs/cursors/input semantics |
| G6 | Latest reference has remote New folder; native API lacks `host.directory.create` | Add one bounded, explicit, authorized directory-create operation and SDK binding; reuse old picker flow. Preserve native macOS directory creation behavior separately |
| G7 | Old Remember host/session-descendant rules differ from exact native grants | Pending user decision: retain current authority scopes with familiar UI, or explicitly design broader native rules. Never widen scope behind familiar labels |
| G8 | Unknown mutation acknowledgement and bounded recovery capacity cannot truthfully behave like unconditional success or silent record eviction | Accepted exception: quiet inline recovery, preserved work, explicit Check/Retry. Full unresolved capacity must be visible and cannot silently forget work |
| G9 | Retired/missing historical cells, partial output or reasoning may never have been persisted | Preserve available history, show scoped absence/retry only when meaningful; no invented history. Import/reconstruction of unavailable legacy runtime data is not part of this restoration |
| G10 | Old Disconnect cleared WHIP-owned credentials and login/catalog state; native route removal preserves credential files | Reuse managed-account logout and add bounded source-aware owned-key cleanup with revision checks and truthful partial-failure recovery. Preserve external files/environment/command credentials and do not delete keys shared by another route |
| G11 | Old default intent and accepted execution limits do not map one-to-one to resolved native continuations/attempts; MCP source preferences moved and explicit import has its own workflow | Represent default versus explicit values without conflating zero. Test old ranges and import effects; extend contracts for parity or obtain an explicit exception where authority/limit semantics remain different. Do not silently rename or drop old controls |

### G1: durable presentation without another transcript

Prefer an optional presentation field on canonical messages (for example a
nullable `messages.presentation` column), persisted atomically with successful
assistant settlement. Use versioned, byte-bounded ordered parts: references to
canonical text/tool calls plus explicitly streamed display reasoning and stable
display slot IDs. Avoid duplicating full prose and tool results. Feed preview
and settlement from the same bounded per-attempt accumulator. Include the
metadata in immutability/idempotent-settlement checks and all relevant history
and exact-read conversions and page byte budgets.

Display reasoning must remain separate from model-facing `Part[]`, token/request
accounting and opaque or encrypted provider continuation. Do not expose private
continuation or helper internals. Fork copies presentation with correctly scoped
display identities, without copying execution authority; rewind retires it with
the owning history, and compaction preserves retained raw history.

The old app also retained partial presentation on failed/interrupted turns. Such
an attempt may have no canonical successful assistant message. Preserve bounded
display evidence with the existing failed attempt/turn settlement, explicitly
marked failed/interrupted, and keep retry attempts distinct. The exact storage
shape must be settled in the implementation PR after inspecting those records.
Deferring this part leaves G1 open; successful-message reasoning alone is not
full parity. A crash before settlement may still lose unpersisted live preview
(G9); this proposal does not require a per-fragment event journal. Use a forward
additive migration from the then-current schema, nullable old records and no
fabricated backfill. Test atomic rollback on migration failure and backup-based
recovery with the matching older binary; do not assume a down-migration or that
older binaries can open the new schema.

### G5: terminal fidelity and temporary-install isolation

Preserve the host's human shell configuration: normal HOME/ZDOTDIR/PATH,
SSH_AUTH_SOCK, relevant XDG variables and existing WHIP markers as appropriate
to local or remote host launch. Separate this policy from the restricted agent
process environment. Do not widen a shared allowlist for all execution merely
to repair terminal UX.

The review installation's deliberately empty HOME is also a source of missing
shell configuration. A normal-fidelity disposable test should isolate
WHIPCODE_HOME and desktop profile while preserving the human shell environment;
a separate clean-room-HOME test covers first-run onboarding. This proposal does
not change the running review installation.

The smallest likely output extension is optional bounded wait-on-read, waking on
PTY output, exit or closure; inspect the existing manager notification path
before choosing the API. Default zero wait preserves current callers. Fit the
wait inside SDK deadlines, allow one waiter per observer, cancel observation
without closing the terminal, and retain exact cursor/epoch validation. Do not
replay terminal input after an unknown acknowledgement or reopen a terminal
implicitly to recover an ambiguous open.

## Implementation increments

Each numbered increment is a coherent draft PR on its predecessor, with its own
reference comparison, focused tests and progress entry. Additive backend/SDK
prerequisites may be a preceding small PR when that makes the behavior easier
to review. Do not combine unrelated migrations into a single restoration commit.

| Step | Deliverable and ordering | Required evidence before calling it complete |
| --- | --- | --- |
| 0. Freeze and reproduce | Build isolated approved reference and native foundation. Inventory old assertions and classify fixture-only changes. Capture the same ordinary workflows and forced failure checkpoints | Reference commit/overlay/build hashes, candidate hashes, reproducible deterministic scenarios, baseline screenshots/traces. No writes to original checkout |
| 1. Opening, submission and approval | Restore startup/welcome/session slots and familiar composer/permission presentation using existing owners; preserve exact recovery and authority | A1–A4, including held independent reads, unknown acknowledgements, local persistence failure and owner switches. Resolve G7 before any scope change |
| 2. Providers and settings | Restore old forms and latest controls; implement G2–G4/G10/G11 underneath before exposing unsupported behavior | A5/A6, both form transactions under faults, default/range/import semantics, source-aware disconnect, disabled-provider dispatch tests, login cancellation/expiry, actual bundled suggested-model metadata |
| 3. Conversation and REPL | First restore simple labels/clock/code presentation, then G1 and stable stream handover, bounded exact-body hydration and history/footer bindings | A7–A10, restart/fork/rewind reasoning, failed attempts, interleaved parts, exact retry boundaries, bounded older execution browsing |
| 4. Terminal and remaining development polish | G5, G6 and approved navigation/dialog/picker refinements; use separate file-scoped PRs where independent | A11/A12, human-shell fixture plus unchanged agent restriction, terminal lifecycle, latest reference keyboard/menu and picker tests |
| 5. Combined acceptance | Compare exact final stack against approved reference in Chromium/Firefox and packaged desktop; review shared SDK/UI effects on mobile and other callers | All A rows linked to artifacts, known failures dispositioned, human walkthrough with side-by-side checkpoints. Update current architecture/workflow guides and progress record |

Steps 2 and 4 can be researched/implemented independently once shared contracts
are assigned, but land in an explicit stack. One integrator owns schema numbering,
protocol generation and shared SDK changes. Stop expanding a slice if restoring
it starts requiring a second state system: revisit its boundary instead.

## Acceptance matrix

Use the same deterministic provider/executor scenario through each backend's
real transport. Old product assertions are the oracle; wire setup differs.
Save interaction checkpoints, DOM/selection identity, geometry and screenshots,
not only final screenshots. Keep tests bounded and deterministic; real accounts
or billable inference are not needed for these fixtures.

| ID | Must demonstrate |
| --- | --- |
| A1 | Cold configured startup never flashes onboarding; held catalog/MCP reads do not block usable UI; pending readiness is neither unavailable nor ready. Warm start retains content. Existing splash footprint, reduced motion and escape behavior match |
| A2 | Hold/fail selected session, root, tree and queue reads independently. Valid transcript/draft/focus/scroll survive metadata refresh. Correct scoped retry. Switching owners never flashes another session's content or controls |
| A3 | Submit once, duplicate click, queued steer, lost acknowledgement, accepted response plus storage failure, reconnect, full recovery storage, reopen and attachment draft. No duplicate mutation or automatic replay; correct draft/caret/recipient retention |
| A4 | Named root approvals, clear child identity/denial/delegation presentation, keyboard focus and queue count, exact approve/deny/remember scope, unknown reply and changed operation. Native child operations use delegation, not direct approval cards. #283 default/explicit-empty child delegation, policy revision expiry, MCP/computer consent remain correct |
| A5 | Fresh profile, configured key/account, detected candidate, custom provider, disabled/re-enabled provider, expired login, cancellation, failed key and suggested model. Disconnect clears only owned credentials/login/catalog state, preserves external/shared credentials, handles partial cleanup honestly and cannot let stale login republish. Opening setup has no mutation/inference/credential-command side effects. Real bundled `kimi-k3-fast` 1,048,576 output ceiling regression remains covered |
| A6 | Model, explicit Default/Off effort, permission default and latest summary/compaction, engine/limits and import controls. Test zero/default versus explicit values and old/native range boundaries without silent coercion. Save/cancel/failure/concurrent revision change preserve values and all-or-nothing guarantees for each original form; no hidden partial success. Import preferences retain source ownership and explicit consent boundaries |
| A7 | Actual file path, search query, command, browser subject, child name and outcome; concise ordinary expansion with raw evidence in details. Native denied/uncertain effects never rendered Done from a parent turn's success |
| A8 | Hold empty/partial call ID, completed ID before commit, commit before cell, cell before observer, result before/after terminal metadata, provider retry, SQL settlement retry, cancel and reconnect. One stable card/disclosure/selection through in-place updates; no blank gap or scroll jump. Both Starlark and QuickJS: print output, null versus absent return, counters, escaped partial code, exact copy and checkpoint presentation. Persist settled reasoning/order across restart, fork, rewind and compaction; failed-attempt evidence stays truthful. No continuation leakage or model-input/accounting changes |
| A9 | More than one 16-turn execution window and more than 128 cells; exact bodies despite sparse/text-only chat tail; byte/count bounds and opposite-edge eviction; no older-window reset on background refresh. Validate owner change/rewind/retirement between body chunks and deduplicate reads across shared Chat/REPL panes. Root/child alternate 20 times, split panes, Back/Forward, reopen within/beyond 30-second lease, restart/reconnect. Wheel/Latest intent and reading bookmarks match the reference; selection survives in-place updates, with no unproven promise of restoring DOM selection after unmount/restart |
| A10 | Latest response timestamp, offline copy, full/visible response copy, tool-ending responses, fork/rewind keep-through boundary, imported history/gaps, active root work and stale revision refusal. Draft/caret retained across Chat/REPL/child navigation |
| A11 | Fixture shell startup via HOME/ZDOTDIR, prompt/aliases/PATH and terminal resize/copy/paste/tabs; real local and remote host scoping. Compare output wakeup behavior during typing. Ring truncation, process exit, epoch change, lost open/input acknowledgements and observer cancellation preserve lifecycle and no-replay semantics. Restricted agent environment remains restricted |
| A12 | Latest unified Projects, agent More options, dialog height/viewport scrolling, tab close/menu order, Details overflow, ContextMenu/Shift-F10, explicit Choose folder and local/remote New folder. Light/dark/high contrast/reduced motion, keyboard, 320/390/530px layouts and zoom without clipping |

Reuse relevant existing gates in [Taskfile.yaml](../Taskfile.yaml):

- Steps 1–3: focused app/SDK tests, `task check:product-conversation`,
  `task check:product-content`, `task check:product-activity`, and the relevant
  browser entrypoints rather than repeatedly running unrelated suites.
- Step 2: affected Go config/provider/store tests, generated contract/SDK checks,
  `task check:product-settings` and disposable onboarding checks.
- G1: store migration/failure rollback, runner/model projection and SDK observation tests;
  include failed/retried/cancelled attempts, UTF-8 truncation, oversize bounds,
  multiple interleaved tool calls and unchanged model-request parts/accounting.
- Step 4: terminal/protocol/SDK tests, terminal and picker browser probes,
  `task check:product-ui`, and affected desktop tests.
- Final stack: `task check:product-web`, `task check:product-browser`,
  `task check:product-conversation`, `task check:product-content`,
  `task check:product-activity`, `task check:product-settings`,
  `task check:product-ui`, `task check:product-desktop`; run
  `task check:product-mobile` for shared SDK/UI contract changes and affected
  CLI/TUI/ACP/SDK example checks for new protocol operations. Record unsupported
  environment gates honestly. Do not reopen broad performance optimization.

Keep NATIVE-01/03 reading failures and NATIVE-04 desktop navigation failure from
the [closeout findings](backend-redesign-closeout.md) open until
their exact criteria are rerun and demonstrated. NATIVE-02 is an observer-count
assertion whose product cause is unproven; it is not an established diagnosis of
REPL jank. NATIVE-04 also lacks its expected evidence archive; the failure and
missing evidence are distinct. The [candidate inventory](backend-native-candidate-validation.md)
retains the recorded job results. A later passing unrelated scenario does not
close any of these.

## Decisions and completion record

| Question | Decision |
| --- | --- |
| Which old frontend defines parity? | User: latest development including current refinements; exact reference above |
| May a small SDK/backend change preserve an established interaction? | User: yes, preserve UX across those boundaries |
| Must an unknown outcome imitate the old flow? | User: rare truthful inline recovery is acceptable; ordinary UX matches |
| Restore broader old Remember scopes or retain current authority? | Asked during planning; pending. Default proposal is familiar UI with current exact authority, not silent widening |

Before each implementation PR, identify its A/G rows and compare the actual
component diff with the approved reference. Its description records behavior
restored, necessary binding/contract changes, tests and remaining exceptions.
Do not claim parity merely because compilation and native fixture tests pass.

This planning PR validates document links, the reference manifest and diff
scope. It does not claim a new product test pass, restored UX, closed historical
acceptance failures or permission to merge/deploy. The plan is complete when
the reference, gaps, ordering and acceptance criteria are reviewable; restoration
is complete only after the implementation stack satisfies the matrix and the
remaining exceptions are explicitly accepted.
