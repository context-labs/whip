# Whip backend redesign and delivery plan

Status: phases 0 and 1 complete and validated in PRs #197 and #199.
Phases 2 and 3 are complete and validated in PRs #200 and #201.
Phase 4 is complete and validated through [PR #202](https://github.com/context-labs/whip/pull/202)
and the audited mail repair in [PR #215](https://github.com/context-labs/whip/pull/215).
The earlier completion claim missed ordinary-mail content-reference transfer;
the repair passes local gates and hosted Linux, macOS and analysis checks.
The implementation remains in an unmerged draft PR stack.
Phase 5 is in progress and phases 6–7 are pending. The authorized execution scope
is all phases, including client adoption and final removal of the retired core.
Written: 2026-09-27. Planning reference: `6f02507bf`.

Execution baseline: `e3fed9c91918d9c36766dd47d878c1b5466238d1`. Commands,
active targets and measured evidence are in
[backend-redesign-development.md](backend-redesign-development.md).

Build a simpler Go backend with explicit ownership, uniform root/child sessions,
and a matching SDK and client contract. Deliver it in working increments with a
small, continuously green regression suite.

This document records the intended replacement architecture and its delivery
gates. It does not describe current behavior. Existing engineering guides remain
the references for current implementation; update them as each change lands.
In particular, follow [frontend.md](frontend.md) when changing SDK,
protocol, or application boundaries. Historical plans are context, not additional
requirements for the replacement.

## Scope and constraints

- Keep the core backend in Go. TypeScript examples from the design discussion
  illustrated concepts; they did not propose moving execution into clients.
- Start with fresh tables, configuration, runtime identity, and a new protocol
  major. No old-data importer, old-config reader, dual writes, or old-protocol
  compatibility layer is required.
- Preserve wanted product capabilities. Changing their implementation does not
  implicitly remove them. Record deliberate feature retirements explicitly.
- Update the TypeScript SDK, shared app, web, desktop, mobile, Go client, CLI,
  TUI, ACP bridge, examples, and documentation as applicable.
- Reuse useful provider, engine, transport, UI, and platform implementations
  where they fit the new boundaries. Evaluate reuse at the responsibility level.
- Keep SQLite and the existing socket/JSON-RPC/gateway transport approach unless
  implementation evidence establishes a concrete reason to change them.
- Develop against disposable homes and databases. The old user data directory
  can remain unused; cutover does not require deleting it.

## Redesign philosophy

1. **Give every fact one authority.** Database records own durable facts. Live
   workers own execution handles. SDK views own reconstructed client state.
   Derived caches and projections must identify their source and invalidation.
2. **Use one session model at every depth.** Roots and children share records,
   execution, history, and client operations. Parent relationships and explicit
   policies explain their differences.
3. **Separate retained state from execution.** A session exists without a live
   worker. Reading history does not start work. Loading a worker does not invent
   configuration or authority missing from storage.
4. **Make transitions explicit.** Input admission, turn start, model dispatch,
   host effects, transcript writes, and completion each have defined durability
   and failure semantics.
5. **Keep abstractions small.** Use concrete structs, ordinary constructors,
   direct composition, and small consumer-owned interfaces where useful. Avoid
   a generic workflow framework, service locator, or repository per table.
6. **Preserve meaningful distinctions.** Inputs, mail, turns, messages, model
   attempts, and host operations have different lifetimes. Unifying their
   storage envelopes must not erase those semantics.
7. **Allow representations to serve their purpose.** Stored messages, provider
   requests, and UI projections can have different shapes. Explicit conversion
   is useful; independently mutable copies of the same fact are not.
8. **Bound retained and live resources.** Queues, worker counts, event replay,
   content reads, transcript pages, and client caches need explicit limits and
   observable exhaustion or truncation behavior.
9. **Make deletion part of delivery.** Remove superseded implementations and
   tests as their replacements land. Keep historical reference in Git.

## Domain ownership and persistence

The names below establish responsibilities. Exact Go fields and SQL columns are
settled in phase 1 before dependent implementations grow around them.
The implemented Phase 1 contract is [backend-domain.md](backend-domain.md).

| Concept | Owns | Authoritative storage |
| --- | --- | --- |
| AgentDefinition | Immutable revision of defaults, instruction policy, tool descriptions, child templates, hook/output declarations | SQL definition revisions, including built-ins |
| SessionTree | Conversation title, archive/pin metadata; common engine selection | SQL |
| ResourceLimit | Session-scoped reusable subtree capacity; every ancestor applies | SQL limits; usage derived from owning rows |
| Session | Identity, tree/parent relationship, source definition revision, resolved configuration and its revision, working directory, retained lifecycle | SQL |
| Input | Accepted work, recipient, source, queue/delivery state, link to execution | SQL |
| RequestReceipt | Stable request identity, payload identity, admission/outcome needed for client recovery | SQL; provider credentials and ephemeral operations excluded |
| Turn | Execution identity, captured configuration revision, accepted inputs, start/end, durable outcome | SQL |
| Message | Authored/model/tool transcript entry with stable identity, ordering, and content references | SQL; large/binary bodies in content storage |
| Compaction | Summary and exact transcript boundary summarized | SQL |
| ModelContext | Composed instructions, selected summary, recent messages and provisional request data | Derived memory |
| SessionWorker | Loaded REPL, current execution snapshot, cancellation and temporary buffers | Memory |
| Runtime | Scheduler, live worker registry, resource lifetime and coordination | Memory; reconstructs work from SQL |
| ModelAttempt | One provider request, logical-call identity, route/pricing snapshot, outcome, usage and settlement state | SQL |
| Operation | Host-action admission, authority, dispatch, outcome and uncertainty | SQL; live handles remain in memory |
| Budget / Grant | Enforced limits, reservations and scoped authority | SQL; derived totals have explicit owners |
| SessionMail | Inter-session communication and delivery/acknowledgement state | SQL, distinct from execution inputs |
| Completion | Parent-owned pending terminal snapshot; published full evidence | SQL slot until publication; immutable parent-owned content plus canonical mail afterward |
| SessionState / TreeState | Explicit private/shared application state and relevant subscriptions | SQL, distinct from VM globals |
| REPLCheckpoint | Opaque engine image, compatibility metadata, integrity and execution boundary | Durable storage; initially SQL binary payload is acceptable |
| Content | Immutable bytes, metadata and scoped access references | Bodies in blob files; metadata/references/grants in SQL |
| ProviderConfiguration | Host routes, endpoint configuration and credential references | Host config files and secret sources |
| Event / Span | Bounded observation/replay and diagnostic evidence | Durable bounded records where needed; streaming deltas may be transient |
| SDK view | Bounded tree/session snapshots, history windows and live presentation | Reconstructible client memory |
| Client draft / recovery record | Unsubmitted user work and request identity metadata | Explicit device storage; separate from authoritative transcripts |

### Required ownership rules

- Exactly one root exists per tree. Other sessions have a parent in the same
  tree; relationships cannot form cycles. Root lookup is derived from this
  relationship rather than an independently writable second identity.
- Every session uses the same transcript table and turn lifecycle. There is at
  most one active turn per session, enforced through runtime ownership and a
  database constraint or transactional claim.
- Creating a session resolves a pinned definition plus explicit overrides,
  validates the result, deeply copies maps/slices, and persists effective config.
  Editing a definition or host default does not silently modify that session.
- Configuration changes are explicit and revisioned. A running turn uses its
  captured configuration; changes take effect at a defined safe boundary.
- Model changes preserve history, children, REPL state, and unrelated integration
  resources. Model selection contains no provider client or credentials.
- Prompt policy belongs to configuration. Dynamic project files, skills, and
  other declared context sources are refreshed at documented boundaries. Keep
  audit evidence of the actual composed request where required; do not pretend
  a pinned definition freezes external files.
- Definitions advertise available operations. Grants, permission decisions and
  host policy determine actual authority. Child creation cannot widen authority
  or evade ancestor budget limits.
- A SessionWorker may cache immutable configuration and derived context. The
  database remains authoritative; there is one path for committed updates to
  invalidate or replace those execution snapshots.
- SessionTree owns shared metadata and engine selection. Capacity belongs to
  session-scoped resource limits. One runtime scheduler and per-session
  execution ownership are the default. Add another actor only for an invariant
  that transactions and the scheduler cannot express clearly.
- Client disconnect, view closure, and aborted local waits release observation.
  Cancellation of accepted execution is an explicit operation targeting a turn.
- Parent-turn completion does not implicitly delete a retained child. Stop,
  cancel, session deletion, subtree deletion and worker eviction have distinct
  semantics and resource cleanup.
- VM state, explicit session state, shared tree state, and conversation history
  remain separate. Live process, browser, terminal and executor handles cannot
  be reconstructed merely by restoring their IDs.
- A content digest identifies bytes; an authorized reference grants scoped
  access. Provider encoding can hydrate bytes without rewriting the stored
  transcript into base64 bodies.
- Store model accounting once per actual attempt, including retries and helper
  calls. Preserve unknown usage/cost separately from known zero. Budget counters
  are transactional projections, with used/reserved/uncertain distinguished.
- Events provide bounded recovery and observation. Canonical state and final
  outcomes cannot depend on replaying an indefinitely retained event log.

### Fresh schema direction

| Current organization | Replacement direction |
| --- | --- |
| Root `sessions` plus `agents` | `session_trees` plus uniform `sessions` |
| `messages` plus child `transcript_messages` | One `messages` table keyed by session |
| Separate root/child turn commit paths | One `turns` lifecycle and persistence API |
| Special built-in definition resolution | Revisioned definition documents for every template |
| Configuration reconstructed through several owners | Stored effective session config plus explicit revisions |
| Model-call ledger plus competing usage representations | Attempt ledger with explicit budget/read projections |
| Checkpoints plus legacy scratch restoration | One checkpoint contract with engine-specific payloads |

This is a responsibility mapping, not an ALTER TABLE migration. Start with a
fresh schema version. Retain normal schema versioning for future releases of the
new system; omit migrations from the retired implementation.

## Go boundaries and execution flow

Target core packages:

```text
cmd/whip/          Startup, explicit dependency construction, CLI entry points
internal/session/ Durable conversation values and validation
internal/store/   SQLite schema, queries, atomic transitions, recovery
internal/runtime/ Scheduling, workers, cancellation, coordination
internal/runner/  Model/code/tool loop and model-context assembly
internal/model/   Provider implementations and request/response conversion
internal/engine/  QuickJS/Starlark execution and checkpoint formats
internal/tool/    Dispatch and integration with scoped authority checks
internal/content/ Immutable content bodies and bounded access
internal/config/  Host configuration and credential resolution
internal/protocol/ Wire DTOs, operation/event declarations, generation metadata
internal/rpc/     Wire validation and application-operation mapping
internal/client/  Go client for CLI, TUI and ACP
```

Keep existing integration packages where useful. This is a boundary map, not a
requirement to create empty packages or move every leaf implementation.

Domain values must not import SQLite, runtime, provider clients, or engines.
Store owns transactions and no live process managers. Runner uses narrow injected
interfaces and cannot reach a global runtime or issue SQL. Protocol owns explicit
wire shapes rather than exporting persistence implementations. Both clients
depend on the wire contract without importing backend execution packages.

Keep transactions intact across related tables. Splitting persistence into many
small repositories must not force orchestration to simulate database atomicity.
Long provider calls and host effects run outside database transactions and
scheduler locks. Root and child execution follow the same path:

```text
client / schedule / authorized agent
    -> admit work and commit request receipt + input
    -> scheduler claims input and starts a turn
    -> worker captures config and builds model context
    -> runner dispatches recorded model attempts and authorized host operations
    -> completed messages / cells become durable
    -> turn outcome and related input transitions commit
    -> clients reconcile committed state with live presentation
```

## Durability and recovery contract

Define these transitions before adding more features to the runner:

| Boundary | Required guarantee |
| --- | --- |
| Input admission | Retry identity, payload identity and input admission commit atomically; same identity with different payload is rejected |
| Turn start | Claim eligible input, link it to the turn, capture config and enforce one active turn atomically |
| Model dispatch | Record attempt/reservation before dispatch; settle each actual attempt without treating unknown cost as zero |
| Host effect | Validate authority, persist admission and record dispatch/outcome; uncertain effects are not automatically repeated |
| Transcript append | Completed entries have stable IDs/order and survive restart; streaming fragments are explicitly provisional |
| Cell checkpoint | Checkpoint identifies engine compatibility and the completed cell/transcript boundary it covers |
| Turn finish | Final outcome and associated input/receipt transitions commit together where they form one invariant |
| Observation | Notify after committed state exists; snapshot/replay closes notification gaps |

Persist completed transcript entries during a running turn. On a crash, retain a
truthful partial conversation associated with an interrupted turn. At cell
boundaries, commit checkpoint metadata/image and transcript evidence together
when their consistency requires it. External effects cannot be rolled back by
that transaction; their independent operation records remain necessary.

Recovery preserves unclaimed queued work, identifies interrupted turns, and
retains unresolved dispatch/accounting outcomes. Retrying failed persistence
must not reissue the provider request or host effect. A retry policy must identify
which work can safely be retried and when explicit input is required.

Blob writes need an explicit ordering with reference commits and orphan cleanup.
Checkpoint incompatibility/corruption needs an explicit failure/reset policy;
never silently replay code to rebuild state containing external effects.

## SDK and client contract

Keep the Go-derived wire generation path. Generate TypeScript declarations,
schemas, operation metadata and validators from the protocol registry. Treat
generated drift and actual Go/TypeScript interchange as required checks.

The client-facing shape should make identity consistent:

| Concern | Intended service shape |
| --- | --- |
| New conversation tree | `trees.create(...)`, returning tree and root session identities |
| Shared metadata | `trees.get/update(...)` |
| Reusable subtree capacity | `resources.list/set(...)` |
| Any root or child | `sessions.get(id)` / `session(id)` |
| Child creation | `sessions.spawn({ parentId, ... })` |
| Work submission | `session.submit(...)`, returning accepted input identity |
| History | `session.history(...)` |
| Execution inspection/cancellation | `turns.get/cancel(...)` |
| Descendants | `sessions.list({ treeId, parentId })` |
| Templates | `definitions.register/get/list(...)` |

A convenience `run()` may follow an input into its associated turn. Admission,
execution outcome, and the lifetime of a local stream remain distinguishable.
Durable requests retain stable recovery identities. Queries and ephemeral
operations such as terminal input do not enter an automatic replay queue.

Use lightweight tree views and uniform session transcript views. The SDK owns
bounded snapshots, event reconciliation, reconnect and request recovery. The app
owns selection, drafts and presentation, with no second reducer for daemon truth.
Snapshot revisions/cursors must provide a gap-free handoff to updates; expired
replay cursors require a fresh snapshot. Large counters remain exact across Go
and JavaScript. Content references remain scoped and lazy.

Custom tool/hook schemas belong to definition revisions. Executable handlers and
their connection-bound availability belong to live executor bindings. Reconnect
must not recreate authority or replay arbitrary effects.

Porting includes shared React presentation, web/gateway transfers, desktop native
bridges and packaging, mobile suspension and local recovery, Go CLI/TUI, ACP,
examples, fixtures and user documentation. SDK work begins with the first working
slice; client adoption continues through the project rather than waiting for a
completed backend.

## Development, testing and CI process

### Working loop

1. State one behavior or invariant to deliver, including the important failure.
2. Add the smallest meaningful check that would catch it breaking.
3. Implement the path through the required layers.
4. Run affected tests while editing; run the active change gate when complete.
5. Review state ownership, transaction boundaries and cleanup; delete superseded
   code/tests and commit a coherent increment.
6. Update phase evidence and feature/test disposition only where they changed.

Use an isolated integration branch and disposable runtime storage. Preserve a
baseline commit and record its actual check results before replacement work.
Use small reviewable changes without requiring every intermediate increment to
support the retired application. Each active target must remain buildable and
green. Retained integration code must not conceal an import back into the retired
orchestration.

Do not create tests for trivial forwarding or field assignments solely to raise
coverage. Prioritize transitions, transactions, concurrency, authority, recovery,
and provider/wire conversion. Test an invariant at the lowest layer that proves
it, then add cross-layer coverage where crossing the boundary is itself risky.

### Feedback gates

The following commands are implemented in phase 0 using the existing Taskfile
and test tools. Targets below are budgets; measured results are recorded in the
[development guide](backend-redesign-development.md).

| Gate | When and scope | Warm feedback target |
| --- | --- | --- |
| Focused checks | During edits: affected package tests and typechecking | A few seconds |
| `check:fast` | Before commit: formatting and compact active core regression suite | Under one minute |
| `check:change` | Before integration merge: active backend tests/build/vet, targeted race checks, affected SDK/client tests, contract drift/interoperability | A few minutes |
| `check:phase` | Phase completion: full active acceptance, broader race/platform/client checks, packaging where introduced | Deliberate longer run |

Keep pre-commit hooks lightweight. Avoid automatically rebuilding every client
or running the full cross-platform suite for each local checkpoint. Measure gate
duration and fix costly setup before adding a test-selection framework.

Maintain a short explicit active-target list in the task configuration. Include
new packages as they land, along with affected retained dependencies. Shared
domain/protocol changes trigger all active consumers, not only changed files.
Not-yet-ported targets have an assigned phase; they do not create permanent
expected-red jobs. Every supported target must return to the gate by cutover.

Configure CI triggers and required status checks for the integration branch.
Required jobs must actually run and their failures must reach the aggregate gate.
Keep production branch policy intact while the replacement has a narrower active
scope. Remove the rewrite branch's global 90% coverage floor as a blocking gate;
keep coverage reports diagnostic and require explicit high-risk scenario coverage.

Run deterministic tests without live provider credentials. Broader OS/browser
matrices, installation, signing, fuzzing, performance measurements and live-model
evaluation run at relevant milestones. A failure in a supported capability remains
a regression even when detected by the broader gate. Live-model quality evaluation
is separate from deterministic runtime correctness.

### Existing test disposition

Classify by feature/test family as its subsystem is touched:

| Category | Action |
| --- | --- |
| Required behavior with retained implementation | Keep; adapt setup only as necessary |
| Required behavior with replacement implementation | Rewrite against its new public boundary; preserve valuable failure scenarios |
| Intentionally removed behavior | Delete with the old implementation |
| Capability scheduled for a later phase | Record the obligation and target phase; cover it before declaring the feature supported |

Historical bug tests are requirements evidence. Actor names, duplicate table
layouts, old wire fields and obsolete migration paths are not automatically new
requirements. Replacing an assertion requires identifying whether the behavior
changed, the implementation broke, or the test was coupled to obsolete structure.
Do not simply update expectations until a failure disappears.

Keep the old suite in baseline Git history. Avoid an archived test directory,
permanent skip lists, blanket retries, and compatibility shims built just to keep
old assertions alive. `go test -run` still compiles all tests in selected packages;
remove or port obsolete tests that no longer compile.

Maintain one compact table here as families are addressed:

| Feature/test family | Guarantee retained or retirement decision | Replacement evidence | Target phase / status |
| --- | --- | --- | --- |
| Admission and client recovery | Stable request identity; accepted work survives lost acknowledgement | `scripts/redesign/v4-fixture.test.mjs`: lost acknowledgement, identical retry, SIGKILL and queue recovery | 2 complete |
| Execution and crash recovery | Explicit interruption, durable completed evidence, no uncertain-effect replay | `store/cells_test.go`, `runtime/engine_test.go`, SDK process-kill fixture | 3 complete |
| Accounting | Every dispatched attempt recorded; settlement retry does not redispatch | `store/attempts_test.go`, `runtime/provider_test.go`, `runtime/observation_test.go`; ancestor accounting in `store/budgets_test.go` | 3 complete; model limits implemented in 4 |
| Recursion and authority | Uniform session behavior, scoped grants, shared limits | `runtime/recursion_test.go`, `store/delegation_test.go`, `store/budgets_test.go`, `store/resources_test.go`, child-control, turn-permit, root/child lifecycle, completion-report and detached-child cleanup suites | 4 complete after retained-capability audit and repair |
| Reusable capacity | Shared subtree admission and lifecycle release; old per-target queue semantics intentionally replaced with ancestor aggregation | `store/resources_test.go`, `runtime/resources_test.go`, `rpc/resources_test.go`, turn-permit race tests, both-engine recursion and SDK restart fixture; counters derived rather than repaired | 4 implemented |
| Cumulative write allowances | Explicit logical actions consume permanent ancestor allowance; initial child input now charged consistently with follow-up input | `store/logical_writes_test.go`, `runtime/state_allowances_test.go`, SDK cap/retry/restart/deletion fixture; accounting and derived notifications remain exempt | 4 implemented |
| Mail and explicit state | Revisioned delivery distinct from inspection; private/shared isolation; immutable history and CAS | `store/mail_test.go`, `runtime/mail_test.go`, `store/state*_test.go`, `runtime/state_test.go`, RPC/SDK fixtures; `store/state_subscriptions_test.go` covers atomic coalescing, cursor/notification rollback and recipient deferral; `store/mail_evidence_test.go`, `runtime/mail_evidence_test.go` and SDK cover the audited evidence-sharing obligation | 4 complete; repair passes local and hosted Linux/macOS gates |
| Context and checkpointing | Raw history retained; checkpoint boundary and fidelity explicit | Both engines pass `runtime/engine_test.go`; durable compaction, raw-history access, captured helper routing, proactive thresholds and bounded context-rejection recovery implemented; fork/rewind remains pending | 3 complete; 5 in progress |
| Provider execution | Preserve supported wire protocols and tool cycles; uncertain partial-stream regeneration is retired in favor of explicit no-replay accounting | Chat wire profiles and Responses/private continuation implemented; subscription generation guards, captured sampling and stateless helpers/batch remain | 5 in progress |
| Integrations and product features | Preserve capability outcomes; inspect existing regression scenarios | Pending | 5 |
| All client surfaces | Correct submission, observation, recovery and resource cleanup | New SDK/socket fixture passes; product clients remain on the retained implementation | 2 complete; 6 pending |
| Old schemas/protocol/scratch compatibility | Retired by fresh-start scope | Delete with corresponding implementation | 1 through 7 |

### Test fixture and diagnostics

Adapt existing fixture utilities where useful. Use real temporary SQLite, the
real runtime/protocol/SDK, a scripted provider, and controlled external tools.
Runtime acceptance must execute the real runner; mocking it bypasses the very
orchestration being tested. Keep a small real-provider/real-engine smoke path
separate from the deterministic daily gate.

Add controls only when a scenario needs them: barriers for dispatch/completion,
provider/tool failures, dropped acknowledgements, disconnection, storage failure,
and daemon restart. Prefer explicit signals and controlled clocks to arbitrary
sleeps. Use a few subprocess kills to establish crash recovery; graceful shutdown
alone is insufficient. Bound every wait and clean up processes and resources.

Retain useful failure artifacts: database, logs, event trace, scripted actions,
and the command/seed needed to reproduce the failure. A narrow failed commit
must prove rollback or recovery, not merely that a returned error is non-nil.

Use the same disposable fixture for manual development of permission waits,
children, interruptions and large output. It must not target a developer's normal
daemon or depend on spending model tokens.

Add a few package-boundary checks for forbidden domain/runtime, store/resource,
and client/execution imports. Keep them tied to intentional dependency rules,
not source-text snapshots or incidental function names.

## Delivery phases

Phases are dependency gates. SDK/client work begins in phase 2 and continues
through phases 3–5; phase 6 completes coverage rather than starting client work.
Within a phase, deliver one useful behavior at a time. Only mark a criterion
complete with a passing check or recorded manual evidence.

| Phase | Deliverable | Depends on | Status |
| --- | --- | --- | --- |
| 0 | Baseline, feedback gates and fixture foundation | None | Complete; [evidence](backend-redesign-development.md#baseline-and-phase-0-evidence) |
| 1 | Domain contract, ownership, fresh storage/config | 0 | Complete |
| 2 | Working database → runtime → protocol → SDK slice | 1 | Complete |
| 3 | One provider, one engine, execution and recovery | 2 | Complete |
| 4 | Recursion and shared coordination | 3 | Complete |
| 5 | Remaining engines, integrations and product behavior | 4 | In progress |
| 6 | Complete client adoption and product validation | Starts at 2; finishes after 5 | Pending |
| 7 | Cutover, deletion and release readiness | All prior gates | Pending |

### Phase 0 — Establish the feedback loop

Record baseline revision/results and set up the integration branch, isolated
storage, active-target gates and fixture lifecycle. Inventory required behavior
families using existing tests and the feature map. Prepare the first admission
and restart scenarios; do not add failing placeholders to the required suite.

Acceptance:

- [x] Baseline identity and actual check results are recorded, including any
      pre-existing failures. No unrun check is described as passing.
- [x] Active targets and the initial behavior/test inventory are explicit.
- [x] Fast/change/phase tasks run locally; CI runs on integration-branch PRs and
      fails visibly when a required check is deliberately broken.
- [x] Hook behavior is lightweight; broader validation is available explicitly.
- [x] Fixture startup/shutdown is repeatable in disposable storage, with bounded
      waits and failure artifacts. It does not touch the normal runtime.
- [x] Feedback times are measured once; targets are adjusted transparently if
      necessary without silently dropping important checks.

### Phase 1 — Define the domain and fresh persistence

Implement durable value types, validation, definition revisions, effective config
resolution, host config and fresh schema. Establish transaction APIs and the
state machines that later code will use. Write the initial wire shapes and
generation fixtures. Choose the first provider and engine for phase 3.

Implementation: internal/session, internal/store, internal/config and
internal/protocol, with the v4 generator in cmd/whip-contract and packages/protocol.
The old storage/config/protocol and its clients are explicitly isolated under
legacy paths until the Phase 2 runtime/SDK cutover. They are not imported by the
new core. Phase 3 starts with OpenAI-compatible chat completions and Starlark.

Acceptance:

- [x] Fresh storage/config initializes deterministically with a new identity and
      no dependency on legacy readers or migrations.
- [x] Root uniqueness, parent/tree consistency, cycle prevention and active-turn
      uniqueness have meaningful validation/constraint tests.
- [x] Root and child use the same records, transcript source and persistence API.
- [x] Built-ins and registered definitions have pinned revisions; resolving and
      changing one session cannot mutate another through aliased maps/slices.
- [x] Each durable field has one authority; configuration update/capture rules
      are explicit. Secrets and live resource handles stay outside session rows.
- [x] Transaction tests cover atomic admission/claims and rollback using real
      SQLite. Store construction does not create process/workspace managers.
- [x] Admission, retry, interruption, cancellation and deletion semantics are
      documented sufficiently to implement phases 2–4 without guessing.

Evidence for every criterion, measured feedback times and hosted validation are
recorded in [the development guide](backend-redesign-development.md#phase-1-behavior-ownership-and-evidence).
Phase 2 now connects the new runtime and SDK; its evidence is recorded in the development guide.

### Phase 2 — Deliver the first working slice

Connect tree/session creation, accepted input, a scripted model turn, transcript
persistence, outcome, history and observation through the actual runtime and
generated wire contract. Implement a minimal SDK example and initial Go client.
Use the real runner with a scripted provider as soon as the loop exists.

Acceptance:

- [x] An SDK example creates a tree/session, submits work, observes a turn and
      reads its durable messages and outcome.
- [x] Losing an acknowledgement and resubmitting the same identity returns the
      same admission; conflicting payload reuse is rejected.
- [x] Concurrent submissions cannot start two active turns for one session.
- [x] Reconnect reconstructs completed state without duplicate messages.
- [x] Unclaimed queued input survives restart; reading history starts no work.
- [x] Client disconnect/local wait cancellation does not cancel accepted work.
- [x] Generated declarations/validators and actual Go-to-TypeScript fixtures
      agree; the new SDK talks directly to the new contract.
- [x] This end-to-end slice is part of the required change gate.

### Phase 3 — Make execution and recovery trustworthy

Add one real provider and one engine, content references, explicit operation
authority, permission interaction, model-attempt accounting, cancellation and
checkpointing. Establish failure behavior before broadening integrations.

Acceptance:

- [x] The real runner executes model/code/tool work through injected boundaries.
      A scripted provider exercises the same loop as the real provider.
- [x] Every dispatched model request, including retries/helpers, has an attempt
      record and truthful usage/cost/uncertainty; settlement failure cannot cause
      an automatic second provider dispatch.
- [x] Tool effects require scoped authority and have durable operation evidence.
      Denial/revocation prevents the relevant effect; unresolved effects are not
      blindly replayed after restart.
- [x] Completed messages survive a crash mid-turn; provisional output reconciles
      without becoming a second committed message.
- [x] Checkpoint integrity, compatibility, execution boundary and failure policy
      are tested; restoration does not replay external effects.
- [x] Injected transaction failures and selected real process kills yield the
      documented queued/interrupted/uncertain outcomes.
- [x] Explicit cancellation, deadlines and resource cleanup pass targeted race
      and lifecycle tests. Cancellation remains serviceable during slow calls.
- [x] A model/config change takes effect at its documented boundary while
      preserving REPL, history and unrelated resource state.
- [x] Content is authorized and bounded; provider encoding leaves durable
      references intact. One real-provider/engine smoke has recorded evidence.

### Phase 4 — Add recursion through the same execution path

Implement child admission, scheduling, authority/budget narrowing, mail,
session/tree state, retry/report policies, and subtree lifecycle operations.

Acceptance:

- [x] Child creation atomically persists identity/config/authority and its
      initial input before scheduling; restart retains accepted child work.
- [x] Root and child pass the same applicable turn, history, cancel and recovery
      scenarios. No parallel child commit or transcript implementation exists.
- [x] Concurrent descendants cannot overspend shared reservations or widen
      authority. Unrelated sessions cannot alter each other's scoped state.
- [x] Reusable depth, descendant, input queue, host-operation, subscription and runnable descendant
      capacities share revisioned ancestor-enforced limits; usage derives from
      canonical rows. Cumulative logical-write limits use immutable charge evidence
      and captured ancestry, without blocking required execution settlement.
- [x] Saturated worker/kernel capacity still permits required child progress;
      parent waits do not deadlock children. Queued work remains durable.
- [x] Retry and report behavior is explicit policy; failed/uncertain work follows
      the selected retry semantics at every depth.
- [x] Mail delivery/acknowledgement is distinct from human inspection and input
      admission. Private and shared state isolation is tested.
- [x] Ordinary authored mail can carry authorized content references, including
      evidence-only mail. Sharing recipient access and admitting the mail commit
      atomically; retries, rejection, replacement, restart and sender deletion
      preserve the intended ownership. Automatic completion evidence alone does
      not replace this retained capability.
- [x] Shared-state subscriptions atomically coalesce notifications with writes;
      subscription cursors and notification evidence survive restart.
- [x] Parent-turn completion, child cancellation, subtree deletion and worker
      eviction release the intended resources without implicit data loss.

Completion audit correction: the original seven acceptance bullets and retained
feature guidance were rechecked after the completion claim. The missing ordinary
mail evidence path is demonstrated by the retained
`TestSiblingDigestPreservesUnicodeAndEvidenceAccess` and the feature map's
message-with-evidence guarantee. The original replacement `MailSend` contained only text and routing fields.
The repair in [PR #213](https://github.com/context-labs/whip/pull/213) adds atomic
recipient-owned references, evidence-only messages and
replacement coverage through the store, both guest engines and generated SDK.
Automatic reports now use the same mail attachment metadata through
[PR #215](https://github.com/context-labs/whip/pull/215). The integrated repair at
`1244d7cd2` passes local phase/analysis gates and hosted Linux, macOS, analysis and
the aggregate gate in [run 36467683567](https://github.com/context-labs/whip/actions/runs/36467683567).
Phase 4 is complete with that repair; the earlier completion claim was premature.
The SDK fixture timeout repair retains all scenarios with separate deadlines and
failure diagnostics. See the detailed
[audit correction](backend-redesign-development.md#phase-4-completion-audit-correction).

### Phase 5 — Port retained product capabilities

Port the remaining engine/provider implementations, compaction, goals, schedules,
fork/rewind/workspace snapshots, MCP, browser/computer, terminals, custom tools,
hooks and output validation. Consult the feature map and existing regression
tests for capabilities omitted from this initial list. Retain the native helper
and host/gateway trust boundaries while replacing their orchestration callers.

Start with final-output contracts: the configuration already stores a schema,
but the runner must enforce it before declaring success. Retain raw assistant
messages, allow one corrective model round through the ordinary recorded attempt
path, and fail explicitly on a second mismatch. Expose validated output as a
projection of the terminal message and captured schema, without another mutable
turn-output store. Clearing a contract follows existing configuration patch rules.

Then add context selection and compaction before broad integration work: the
current explicit 100-message/4 MiB context limit is a temporary safety boundary.
Compaction must retain raw history and exact covered sequence boundaries, and
its model work must use ordinary attempt accounting. Both engines already run
through the new core; extend their shared contract evidence as capabilities arrive.

Compaction implementation decisions:

- Admit manual compaction as a durable input with kind `compact`, using the
  existing queue, receipt, turn, cancellation, permit and accounting paths.
  Input kind owns the distinction; a turn exposes it as a projection. Compact
  turns create no synthetic conversation message, consume no mail, execute no
  cells, and apply neither output contracts nor child completion reporting.
- Store immutable summaries separately from transcript messages. A summary names
  its model attempt, base summary, exact covered sequence and any pinned raw
  message IDs. One revisioned context head selects the current summary. Undo
  changes that selection with an idle-session CAS; it does not erase evidence,
  restore a checkpoint, or change workspace files.
- Settle accounting and summary evidence together. If head selection has changed,
  retain the completed attempt and summary without selecting it. A retry of an
  already committed settlement cannot reselect a summary after undo.
- Run automatic compaction only at settled model/tool boundaries through the same
  helper and accounting path. Summaries are quoted conversation data, never new
  system instructions or assistant answers. Do not stream them as ordinary reply
  previews. Require forward coverage progress and bound helper calls and bytes.
- Keep recent complete exchanges and the exact opening input of a split turn.
  Never split an assistant call from any of its tool results. Fold oversized
  historical prefixes in bounded batches or fail explicitly; never silently
  skip them. Before introducing omissions, provide bounded own-history metadata,
  exact reads and search against a fixed raw-history boundary.

Goal implementation decisions (work remains open):

- A stable goal ID identifies immutable text and continuation allowance. Its row
  owns revision, state and consumed continuations. The current goal derives from
  the latest successfully created per-session ordinal, including a terminal
  goal; there is no second mutable current-goal pointer. At most one goal is
  armed or paused. Replacing text creates a new ID. Deletion retains an identity
  tombstone and clears text.
- Creation supplies an expected current ID/revision, or null when no goal has
  ever been selected. Resolve exact ID/digest retries before current state/CAS
  checks; retrying an old goal cannot select it again. Replacing an open goal,
  cancelling its unclaimed inputs, and optionally admitting the first ordinary
  prompt commit together. Queue/write-limit failure rolls back the creation.
  Do not widen ordinary input receipts into a generic command ledger.
- Initial, resumed and automatic goal work uses ordinary inputs with explicit
  goal provenance. Resume has an ordinary request identity and expected goal
  revision; it never resets consumed continuations. Omitted allowance defaults
  to 100 additional continuations; explicit zero allows an initial run with no
  automatic continuation. Exhaustion requires a new goal ID for a new allowance.
  Admission/replacement/resume retain the existing busy-session restriction.
- Each ordinary prompt turn, including human or mail work, captures the armed
  goal identity/revision. Claiming an old goal-owned input cannot bind it to a
  replacement. Successful Finish either applies an exact completion intent or
  atomically admits one continuation and advances its durable count. Exhaustion,
  interruption, failure and cancellation pause continuation explicitly; restart
  never resets the allowance or replays an uncertain active turn. Semantic queue
  rejection pauses the goal while preserving successful turn settlement.
- `GoalsEnabled` is captured session configuration, copied from the definition.
  It is eligibility, never a grant. Typed `goals.complete` uses normal operation
  admission and a tree-scoped grant, with additional captured-own-goal checks.
  Its successful operation is the completion intent; no duplicate intent table
  is needed. Finish requires success, valid final output and the unchanged goal.
  Root permission interaction and child delegation follow ordinary rules.
  The legacy `GOAL_MET` text heuristic is retired.
- Cancelling a goal prevents future continuation and cancels its unclaimed
  inputs. It requests cancellation only for its active goal-owned input; a human
  turn that merely captured that goal remains independently owned. Cancellation
  targets the stable goal ID, so it does not require a revision that may advance
  while the user is stopping it. Disabling goals prevents future admissions and
  must leave pending work visibly paused rather than repeatedly unclaimable.
- Formulation from context is a maintenance input with a captured raw-history
  window and main model selection. It creates no conversation messages, cells,
  mail delivery or ordinary preview. Use the same helper-attempt loop as
  compaction and discard private provider continuation. Settle billing and the
  immutable candidate together; successful CAS can create the goal and first
  ordinary input in that transaction. A local savepoint around activation keeps
  billed candidate evidence while rolling back semantic CAS/capacity rejection.
  Real SQL errors roll back settlement and permit SQL-only retries.
- Formulation acceptance and maintenance-turn outcome are distinct. A crash
  after acceptance but before Finish leaves one accepted goal/input and an
  interrupted helper turn; recovery preserves the accepted work. An immutable
  origin-attempt association proves acceptance even after goal replacement or
  deletion. Rejected candidates never enter the goals table. Exact settlement
  replay cannot activate a previously rejected candidate when capacity returns.

Deliver goal records/admission first, then turn capture/continuation and typed
completion, then formulation and client integration. Acceptance must cover real
transaction rollback, retries after later state changes, authority, output
failure, cancellation timing, durable continuation limits, restart, both engines
and SDK acceptance/outcome distinction. These decisions deliberately replace
legacy in-memory round resets and automatic rearming after failure; they are not
claims that goal support is already implemented.

The goal-record foundation now implements immutable specifications, revisioned
lifecycle, exact create/resume retries, current selection, atomic ordinary-input
admission and deletion tombstones. Turn capture, durable continuation, recovery
pause and authorized completion settlement are now implemented internally. Fresh
config is 8 and schema 28. Generated input and turn projections preserve goal
provenance and exact decimal revisions. Runtime/guest/RPC/SDK controls and
formulation remain open; the public goal API is not yet exposed.

Stateless model-helper implementation decisions (work remains open):

- Preserve `models.call(prompt, max_tokens)` and ordered
  `models.batch(prompts, max_tokens)` as scoped host operations. The retained
  guest implementation has no per-call model/provider/sampling/stream controls;
  adding those would be new scope. Use captured session model selection; using
  its captured effort consistently is an explicit correction to the retained
  helper's omitted effort. Requests contain one user prompt, with no session
  instructions/history, tools, output contract, compaction, continuation or
  ordinary reply preview.
- Reuse the runner's recorded-attempt loop via a small typed helper settlement
  target. Store immutable operation ID and batch index on each helper attempt;
  derive a domain-separated logical request ID from those values. Do not create
  child sessions, inputs, a helper registry or another provider retry loop.
  A requested output cap must affect encoding and reservation; a subscription
  route must reject a smaller cap it cannot enforce.
- Store ordered aggregate output in the ordinary operation result. Operation
  settlement waits for linked attempts; cell settlement already waits for
  operations. A crash after item billing but before aggregate publication keeps
  the charge and an uncertain operation; that response text may be unavailable.
  No recovery redispatches it. No separate per-item response table is needed for
  a guarantee the retained product did not provide.
- Bound batches to 32 items and four simultaneous requests, with encoded
  argument/result bounds. Join every started request before returning from the
  operation. Normal settled provider failures are positional results. Cancellation
  or unresolved accounting failure stops new scheduling and drains started work.
  Distinguish unstarted, cancelled-before-dispatch and dispatched-uncertain work.
- Accounting or operation-settlement failures must abort the cell through a
  typed host failure boundary that guest exception handling cannot swallow.
  A catchable error string is insufficient. Preserve already committed output
  evidence and charges; never declare a successful checkpoint for an aborted cell.
- Recheck current ancestor exposure at model dispatch, counting existing
  reservations once. A sibling can settle above its bound after another helper
  reserves; prevent that later dispatch when the scope is now over budget or
  unbounded. This cannot prevent overages from requests already dispatched.
- Keep large helper output readable as owner-scoped content with a bounded
  preview. Registering automatic provider evidence cannot require a new user
  logical-write allowance after billing, roll back its charge or cause replay.
  Strict arguments, bounded fan-out and existing safe retry limits deliberately
  replace legacy lax parsing, unbounded batches and uncertain-response replay.

Deliver helper accounting/join/fatal boundaries, then single-call execution,
then bounded batch orchestration and both-engine SDK acceptance. Retain tests
for caught host failures, exact attempt/HTTP counts, ancestor overages, output
preservation, reversed completion order, cancellation, restart and large content.


Fork, rewind and workspace implementation decisions (work remains open):

- Make content handles unique within their owner using `(owner_session_id,
  reference_id)`. Fork can copy existing handles into a new owner while storing
  body bytes once. Qualify registration retries, logical-write identities and
  mail-evidence foreign keys accordingly; a digest never grants access. This
  preserves handles embedded in opaque text without aliases or string rewriting.
- Give every transcript message an immutable history-group identity and explicit
  opening-input marker. Native groups use their turn identity; imported groups
  retain immutable source provenance without fabricated turns, inputs, attempts
  or spending. Shared group boundaries drive context tails and compaction pins.
- Keep one current history. A session owns a history revision; messages retain
  immutable bodies and write-once retirement metadata. Rewind records an
  immutable edit, advances the revision and hides the suffix from current
  history/context/search. Exact evidence reads remain possible. Message sequence
  numbers never repeat, including after retirement and new work.
- Rewind compares both expected history revision and observed through-sequence:
  revision alone cannot detect an unseen newly completed turn. Require a stopped
  owner with no active turn or queued input for the initial implementation.
  Query boundaries and revisions together. Exact edit retries resolve before
  later CAS/lifecycle checks. Do not rewind mail, goals, state, budgets or effects.
- Rewind explicitly resets the REPL. Capture history revision on turns and tag
  cached kernels/checkpoint selection with it, so a crash between the SQL edit
  and cache disposal cannot restore the retired state. Fork begins with an empty
  REPL. Neither operation replays code or claims VM time travel.
- Fork atomically creates a new tree/root from an explicitly selected terminal
  history-group boundary, history revision and configuration revision. Copy
  history, pinned configuration/definition/engine, authorized content and scoped
  private continuation, subject to destination bounds. Exact fork identity/digest
  retries and deletion tombstones prevent duplicates. Copy no children, grants,
  schedules, mail, state, operations, checkpoints or spending. Workspace directory
  reuse does not create a Git worktree. Do not clone an armed goal or authority;
  retaining goal text as a new paused goal or origin metadata needs an explicit
  product decision in the implementation slice.
- Preserve selected compaction summaries and pins wholly covered by the fork
  boundary with immutable source provenance, without fake model charges. Rewind
  clears incompatible selection while retaining old summary evidence and prevents
  later selection of summaries that cover retired history.
- Keep workspace restore separate from conversation rewind. The retained Git
  checkout is a tracked-path overlay, not an exact rollback: it does not restore
  untracked files, remove all later files or preserve staging. Expose those
  semantics truthfully until an explicit stronger restore is implemented. Use
  opaque snapshot identities bound to worktree/scope, retain pins until release,
  and claim restore durably before Git executes. Interrupted or partially failed
  restores are uncertain and never automatically replayed. A workspace writer
  lock does not freeze external editors. Retire the old compound restore-then-SQL
  rewind because external effects and database edits cannot commit atomically.

Deliver content ownership, then history groups/revisions and REPL invalidation,
then fork imports, then separate workspace operations. Acceptance includes
source deletion/double forks, opaque handles, exact retries, concurrent stale
history edits, compaction pins, non-reused sequences, restart/reset boundaries,
and tracked/untracked/deleted/staged file behavior and partial restore failures.

Acceptance:

- [ ] Each retained capability is implemented, or its explicit retirement is
      recorded. No feature disappears merely because its old tests were deleted.
- [ ] Both engines pass common contract tests and their documented checkpoint
      fidelity/compatibility tests; shared semantics do not depend on language.
- [ ] Compaction preserves raw transcript history and records exact boundaries.
      Fork/rewind defines conversation, checkpoint and external workspace effects
      and prevents stale client history from being silently applied.
- [ ] Goals/schedules admit ordinary inputs; due work is handled according to
      policy even when its session worker is not already loaded.
      Schedules now have exact ordinary-input provenance, atomic cursor/charge
      admission, bounded client-independent polling, stopped-owner/restart and
      both-engine ownership coverage. Goal work remains separate; this combined
      acceptance item stays open until its goal obligations are satisfied.
- [ ] Integration reloads/model changes preserve unrelated children, grants,
      REPL and resource ownership.
- [ ] Executor disconnect, required/optional hooks, tool schemas and output
      validation have explicit failure behavior and matching SDK coverage.
- [ ] Human terminal/browser resources and agent authority remain distinct;
      reconnect does not silently restore revoked attachments or replay effects.
- [ ] All retained feature families have replacement evidence and use the new
      core; no compatibility wrapper delegates execution to the retired runtime.

Implementation progress: final-output contracts and the first context increment
are implemented. Manual compaction, revisioned summary selection/undo, bounded
own-history access, ordinary request reconstruction and automatic whole-turn
folding have replacement evidence. Captured helper routes and proactive thresholds
now use per-turn ordinary usage or a bounded request estimate, with no mutable
cross-turn usage cache. Workspace rules and project skill metadata now refresh
once per ordinary turn with standing read authority, immutable source audit and
frozen instructions across retries/corrections. Explicit current-input skill
bodies now use the same catalog winners as read-only source inspection/completion;
bodies remain turn-local and never rewrite canonical input. Named host roots
now use explicit registry IDs, captured root selection and standing grants;
`skills.read` supplies bounded, digest-checked body pages through the operation
ledger. Standing user instructions now use an explicit file and exact standing
authority, with filtered turn-local text and immutable raw-source audit.
Authorized ancestor project rules and skills now use explicit named boundaries,
captured selection, exact instruction grants and verified descriptor-confined
membership. Both engines and SDK process acceptance cover aliases, inherited and
restricted child sources, guest reads, refresh and restart. Broader Phase 5
acceptance, including fork/rewind, remains open.
Long-turn splitting now retains exact opening inputs and complete tool exchanges;
one confirmed provider context rejection can trigger a recorded smaller request
after accounting settles. Indivisible oversized exchanges still fail explicitly. Chat now preserves the pinned provider wire profiles, including off-effort omission
and derived session cache keys. API Responses now uses the same recorded-attempt
path and stores bounded private continuation with immutable assistant messages.
Replay requires the same route, credential, model and visible message parts;
helpers and public history never receive opaque continuation. Subscription model
execution and host-owned credential lifecycle now use the same recorded attempts,
with fixed routing, generation checks and one settled 401 refresh. Public account
onboarding remains Phase 6 work. Stateless model helpers and captured sampling
remain required. Legacy uncertain
partial-stream regeneration is explicitly retired under the new no-replay
accounting policy; all dispatched attempts must still settle truthful evidence.
This progress does not narrow Phases 5–7.

### Phase 6 — Complete SDK and client adoption

Finish the uniform services and views, shared app, web gateway, desktop native
bridges, mobile, Go client, CLI/TUI and ACP. Update examples and canonical docs.
Preserve app-owned drafts, selection and reading behavior without duplicating
daemon state in a second client store.

Acceptance:

- [ ] Every supported client builds against the new generated contract and
      exercises submission, observation and explicit cancellation.
- [ ] Uniform session handles/history/views work for roots and children. SDK
      recovery distinguishes acceptance, outcome and local observation errors.
- [ ] Snapshot/subscription handoff, dropped events, expired replay, restart and
      lost acknowledgement recover without duplicate work or presentation.
- [ ] Large histories/content and slow consumers remain bounded and report
      truncation/unavailability truthfully; cross-session access is rejected.
- [ ] Desktop native bindings, mobile suspension/resume, web content transfer,
      Go client and ACP pass their relevant transport/lifecycle checks.
- [ ] Fresh namespaces for local recovery records/caches prevent old identities
      from targeting the new runtime. Unsupported peers fail initialization.
- [ ] Manual product checks cover interrupted work, permissions/questions,
      children, content, drafts and navigation on the affected surfaces.
- [ ] Examples, SDK docs and canonical frontend/protocol guides describe shipped
      behavior. All supported client targets are now in required CI.

### Phase 7 — Cut over and remove the retired core

Finish deletion, restore comprehensive product gates and prepare matching
backend/SDK/client artifacts with a fresh runtime/config namespace.

Acceptance:

- [ ] Old root/child orchestration, duplicate transcript paths, legacy migrations,
      scratch readers, protocol shapes and superseded reducers/tests are removed.
- [ ] No active target depends on retired execution code or compatibility
      aliases. Temporary exclusions and scaffolding have been removed.
- [ ] All retained guarantees have replacement coverage; the disposition table
      contains no unresolved obligations for supported features.
- [ ] Full applicable build, vet/lint, race, contract, SDK, client, platform and
      packaging gates pass on the final revision, with exact coverage recorded.
- [ ] Fresh installation/startup and restart are validated on shipped targets;
      incompatible clients fail clearly. Matching artifacts use one source
      revision and verified shared renderer where applicable.
- [ ] Representative real-provider/engine workflows and bounded-resource checks
      have evidence. Unverified platform/manual checks remain explicitly named.
- [ ] Release/setup docs explain fresh config/data and retain old directories
      without silently importing or deleting them.
- [ ] The normal supported-product gate replaces the temporary active-target
      scope; this plan records completion and links to canonical documentation.

## Decisions to settle before dependent work

These are concrete decisions, not permission gates for routine implementation.
Resolve them with the simplest behavior consistent with the ownership rules and
record the result here. A phase cannot pass while its required semantics remain
undecided.

| Decision | Latest point | Starting position |
| --- | --- | --- |
| Submit vs steer, input batching, input-to-turn mapping | Phase 1 | Queued admission is durable; steering targets an explicit active turn |
| Configuration updates while busy | Phase 1 | Revisioned changes affect the next turn; current execution snapshot stays fixed |
| Session stop, cancellation, deletion and subtree policy | Phase 1 | Separate operations with explicit descendant/resource effects |
| First provider and engine | Phase 1 | Choose retained implementations with useful deterministic fixtures |
| Retry policy after interruption or uncertain dispatch | Before phase 3 effects | Preserve uncertainty; no automatic replay of unknown external effects |
| Cell/checkpoint/transcript commit boundary and fidelity | Before phase 3 checkpointing | Completed entries persist during turns; checkpoint names its covered boundary |
| Question/permission lifetime across crash | Phase 3 | Persist decisions as appropriate; never claim a lost live waiter resumed |
| Provider config changes and request provenance | Phase 3 | Host credentials remain live; record the actual request route/pricing snapshot |
| Fork/rewind relationship to VM and workspace state | Before phase 5 implementation | Explicit history revision and documented external-state behavior |
| Exact service names, DTOs, cursors and protocol major | Initial subset in phase 1; final before phase 6 completion | One Go-owned wire registry, uniform session identities |
| Feature retirement discovered during porting | Before deleting its only coverage | Preserve by default; record any intentional scope change |

## Evidence and ongoing maintenance

For each phase completion, append a brief entry with revision, commands and
results, relevant manual checks, remaining limitations, and links to replacement
tests/artifacts. Keep implementation detail in code and canonical guides; this
file owns the migration sequence, unresolved decisions and acceptance status.

No implementation or runtime validation is claimed by this planning document.

Useful starting references:

- [Current architecture](architecture.md),
  [runtime](rlm-runtime.md),
  [concurrency](concurrency.md), and
  [feature map](features.md).
- [Frontend ownership and validation](frontend.md),
  [SDK API](../packages/sdk/README.md), and
  [Go contract generator](../cmd/whip-contract/main.go).
- [Current task gates](../Taskfile.yaml),
  [CI workflow](../.github/workflows/ci.yml), and
  [pre-commit hook](../scripts/git-hooks/pre-commit).
- [Retained root-bound SDK](../packages/legacy-sdk/src/session.ts),
  [Go client in daemon](../internal/daemon/root_client.go), and
  [SDK process-restart fixture](../internal/daemon/v2_sdk_test.go).
- [Accounting failure regressions](../internal/daemon/budget_test.go),
  [engine recovery tests](../internal/daemon/execution_engine_test.go), and
  [compaction regressions](../internal/daemon/manual_compaction_test.go).
