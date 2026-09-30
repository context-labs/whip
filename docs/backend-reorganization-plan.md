# Backend reorganization with a frozen client contract

Status: implementation authorized; preparing an unmerged PR stack for review.

Execution instruction, September 29: the user authorized end-to-end overnight
execution with all work ready for morning review. For this execution, prepare
successive PRs without waiting for human review between steps. Keep changes
sequential and independently verified, preserve the compatibility constraints,
and leave every PR unmerged. The checkpoints below are evidence and eventual
merge checkpoints; human review occurs after the stack is prepared.

Research date: September 29, 2026 (America/Los_Angeles).
Starting development revision: [`271c0f8d2a35648d1b45056d57432590b783483c`](https://github.com/context-labs/whip/commit/271c0f8d2a35648d1b45056d57432590b783483c),
verified against GitHub and the clean local checkout. Some linked CI timestamps
are September 30 in UTC.

## 1. Purpose and definition of success

Make the existing backend easier to understand, change, and test without
changing the product it implements. A developer should be able to identify
which component owns a responsibility, what it depends on, who creates and
closes its resources, and where to make a change without following unrelated
code through the daemon.

The problem is accumulated coupling and unclear responsibility. The previous
redesign combined that problem with new protocols, storage, execution semantics,
and client adoption. Its scope made regressions hard to detect and attribution
harder. This effort deliberately separates structural improvement from those
product and protocol decisions.

The expected improvements are all four of the following:

| Improvement | What this effort can accomplish | Boundary it must respect |
| --- | --- | --- |
| Package boundaries | Move coherent implementations behind small package APIs; separate native client code from server implementation. | Existing protocol and storage types can remain shared. Package purity does not justify conversion layers. |
| Resource ownership | Give live resources explicit constructors, borrowers, and shutdown owners. | Creation, cancellation, sharing, and teardown must happen at the same points in execution. |
| Dependency direction | Keep domain services independent of daemon orchestration; keep clients from importing the runtime implementation. | Existing representation dependencies remain when removing them would require a new model or schema. |
| Smaller responsibilities | Separate provider setup, MCP host configuration, startup wiring, persistence operations, and selected actor helpers. | Extract existing work without changing scheduling, transaction boundaries, or event publication. |

Success is a useful improvement after each merged PR. Completing an idealized
directory tree, reaching a package-count target, or finishing every possible
extraction is not required. A smaller daemon with understandable remaining
coupling is a valid outcome.

## 2. Decisions and hard constraints

These apply to every PR, including an apparently mechanical move.

1. **Freeze the SDK and all observable client behavior.** Preserve SDK source,
   exports, request and response shapes, event shapes, omitted/null/empty values,
   validation, error codes/kinds/messages, capability negotiation, identifiers,
   sequence numbers, ordering, command acceptance, deduplication, cancellation,
   reconnect, replay, snapshot fallback, and restart recovery. Preserve the
   current protocol version, **6.9**. No frontend migration is part of this work.
2. **Use one implementation and the existing representations.** No new protocol,
   replacement runtime, alternate execution path, dual writes, compatibility
   mode, or old-to-new translation layer. Services may accept and return the
   existing `protocol` and `session` types directly. Existing provider encoders
   and client projections remain; this does not mean deleting legitimate
   conversions that already exist.
3. **Preserve database schema and persisted-data compatibility.** No schema
   revision, table redesign, migration rewrite, reset, import, changed config
   format, runtime-home relocation, content-handle format, checkpoint format,
   or changed acceptance/rejection of older stores. Preserve transaction and
   recovery behavior, including opening data written by the previous binary
   and rolling back to that binary after the candidate has written data.
   The recorded development schema is version **21**, with identity
   `whip-recursive-runtime-v21`; preserve its existing upgrade paths too.
4. **Preserve runtime features and semantics.** REPL APIs and guidance, both
   execution engines, scratch/checkpoint behavior, retained children,
   inheritance, mail, reporting, budgets, permissions, tools, MCP, browser and
   terminal ownership, model routing, compaction, provider retries and request
   encoding, tracing, usage/cost accounting, and exports remain intact.
   Prompts and defaults are behavior too.
5. **Keep behavior changes separate.** Discovered bugs, dependency upgrades,
   performance optimizations, feature removals, and new policies get separately
   proposed PRs. A cleanup PR cannot quietly fix a bug or update an expected
   result to fit a changed implementation.
6. **Start from current development.** Do not continue or merge the abandoned
   redesign stack. Recheck development before implementation and record any
   intervening commits. Review those deltas explicitly before updating the
   baseline; never silently redefine compatibility as whatever now passes.
7. **Work sequentially in reviewed stacks.** One PR under active implementation
   or review at a time; human approval before starting the next. Reviewed PRs
   remain stacked until the checkpoint for their group, then merge in dependency
   order. No parallel stream of speculative follow-up PRs. Each step must work
   independently and preserve all existing gates.
8. **Allow mechanical Go client moves.** Moving the existing Go client into a
   dedicated package and updating CLI/TUI/ACP imports is in scope. Preserve its
   APIs and behavior apart from internal Go import paths. The TypeScript SDK
   remains untouched.

These constraints and the checkpoint merge cadence are confirmed by the user.
There are no remaining planning preference questions. The overnight execution
instruction above supersedes waiting for human approval between implementation
steps; it does not authorize merging the resulting PRs.

Type aliases can preserve Go type identity during a package move; they do not
translate values. Use them only where they simplify the move, identify which
callers still need them, and remove unnecessary internal aliases as callers
move. Do not create a permanent forwarding service API to simulate two
architectures. Go's [type-alias guidance](https://go.dev/blog/alias-names)
supports incremental package moves without introducing distinct types.

## 3. What the repository research established

### Starting point and previous work

Development already includes [PR #196](https://github.com/context-labs/whip/pull/196),
which improved session-state ownership and retained protocol 6.9. Preserve that
work. There is no reason to undo it to pursue this plan.

The new domain/schema/protocol work in [PR #199](https://github.com/context-labs/whip/pull/199),
recursive semantics in [PR #202](https://github.com/context-labs/whip/pull/202),
renderer adoption in [PR #265](https://github.com/context-labs/whip/pull/265), and
REPL repair in [PR #296](https://github.com/context-labs/whip/pull/296) remain
unmerged. They demonstrate why that stack is unsuitable as this effort's base.
Its ownership observations and regression scenarios may be useful references;
its replacement types, lifecycle policies, and assertions are not the contract.
Do not cherry-pick its repairs without independently showing relevance to
development.

This plan supersedes [backend-redesign-plan.md](backend-redesign-plan.md) **as
the proposal for this reorganization**. The earlier plan remains historical
context, not an additional set of requirements. Current implementation guides
remain authoritative about implemented behavior; a stale reference in a guide
must be checked against code. For example, some architecture prose still calls
the protocol v4, while `internal/protocol/types.go` declares 6.9.

### Current ownership and coupling

The package inventory found 44 Go packages. On the active macOS build,
`internal/daemon` has 82 production Go files and 123 test files;
`internal/session` has 55 and 67 respectively. These are orientation figures,
not targets. Go language-server analysis found 376 references to `Store`
across 91 files and 73 to `ProviderService` across 21 files, including tests.
Refresh references for the exact revision and build tags before each move.

| Area | Current evidence | Consequence for the plan |
| --- | --- | --- |
| Daemon | `daemon.go`, `session.go`, `server.go`, provider services, clients, host handlers, recursive runtime, and transport-facing code share a package. | Separate coherent outer responsibilities before touching actor scheduling. |
| Provider setup | `ProviderService` owns login flows, credentials/catalog operations, and configuration, but also MCP import methods and lazy brand-icon state. | Separate the unrelated MCP host responsibility before moving provider implementation. |
| Shared configuration | `UpdateConfiguration` changes provider defaults and host settings in one versioned update, sometimes under the provider provisioning lock. Command handling and model admission call private provider helpers. | Preserve atomic updates and lock order; prepare narrow operation boundaries before extracting the package. |
| Persistence | `session.Store` opens SQLite and also constructs `capability.Workspaces` and `ProcessManager`; `Store.Close` closes processes before SQLite. | Resource ownership can improve without changing SQL. Process and workspace moves must be separate. |
| Workspace authority | `session/permission.go` opens/canonicalizes a workspace inside delegation's transaction; root authority setup has a different ordering. | Preserve both orderings. Do not move validation across a transaction to make a package appear pure. |
| Root and child execution | `AgentSession.RunTurn` already serves both. `RecursiveRuntime` owns the live tree; root scheduling and child scheduling/commit paths differ. | Extract shared implementation only where it is already shared. No new universal scheduler or session record. |
| Native clients | TUI and ACP import `daemon` for `Client`, `RootClient`, aliases, and connection helpers. | A dedicated Go client package offers a real dependency improvement without changing the wire contract. |
| Protocol types | `internal/protocol` imports `session`, `config`, `llm`, `capability`, `mcp`, and other existing packages. | It is not a dependency-free leaf. Moving protocol-consuming code into those packages can create cycles. |
| Transport | `internal/protocoltransport` already owns Unix/WebSocket framing. `serverConn` owns delivery queues and subscriptions. | Reuse framing. Keep queue, reconnect, and publication mechanics in place during initial service extractions. |
| Tracing | `daemon/spans.go` records causal relationships at runtime; `session/span.go` and OTLP code persist/query/export them. | Keep recording at the same execution points. Moving tracing into a delayed observer would change behavior. |
| Startup | `cmd/whip/daemon.go` combines locks, configuration, provider discovery, factory construction, kernels, server, gateway, and shutdown. | Small construction helpers and explicit ownership improve readability without a new framework. |

### Baseline is not yet green

The [CI run for the starting revision](https://github.com/context-labs/whip/actions/runs/36674593257)
failed. The inspected [SDK job](https://github.com/context-labs/whip/actions/runs/36674593257/job/109756705168)
stopped at `npm audit --omit=dev --audit-level=high`, reporting a high-severity
`brace-expansion` advisory. The later SDK/product acceptance steps in that job
did not run. This is not evidence of an SDK behavior regression, and successful
Go jobs do not establish a green product baseline.

Resolve baseline failures in separate prerequisite PRs, then run the complete
required gate. Do not lower the audit threshold, drop tests, waive the aggregate
gate, or call skipped checks successful. Any additional failures exposed after
the audit is repaired require the same treatment. This proposal records a
research result, not a completed local acceptance run.

## 4. Intended boundaries

Keep one Go module and ordinary `internal` packages. Use constructors and direct
calls. No DI container, service locator, generic repository layer, event bus,
new workflow framework, or package per database table. This fits Go's
[server layout guidance](https://go.dev/doc/modules/layout).

| Owner | Responsibilities | Dependencies and limits |
| --- | --- | --- |
| `cmd/whip` startup | Configuration, process locks, construction, handoff of ownership, shutdown wiring. | May compose all backend packages. Keep explicit startup order. Add a separate application package only if a real second caller needs it. |
| `internal/daemon` | Server entry points, root registry, root actor, command coordination, existing runtime composition. | Calls provider/host services and storage. It remains the integration point; shrinking it does not require emptying it. |
| Proposed `internal/provider` | Existing provider setup, authentication flows, discovery/catalog selection, host configuration operations used by that service. | Existing config/auth/LLM packages and unchanged protocol types. Must not import `daemon` or own turns. |
| Host MCP component | Import offers, applying host MCP configuration, and current icon lookup used by that surface. | Initially a small component inside `daemon`; separate package only when dependencies justify it. No session MCP lifecycle ownership. |
| Host directory component | Current list/create/pick behavior and OS-specific implementation. | Initially grouped separately inside `daemon`. A later package must preserve existing protocol errors directly, without inventing an error translation model. |
| `internal/session` | SQLite/content persistence, existing durable models, atomic transitions and queries. | No actor or provider construction. Keep existing transaction logic and representations. It may borrow the existing workspace resolver for authority checks. |
| Daemon resource owner | One shared process manager and workspace lock coordinator, passed to tools/MCP/runtime as today. | Ownership is explicit; scope and lifetime stay unchanged. Resource implementations remain in `capability`. |
| Existing `agent`, `rlm`, `tools`, `capability`, `mcp`, `llm` | Existing model loop primitives, kernels, tools, authority enforcement, integration machinery, provider encoders. | Keep their established responsibilities. No broad modernization sweep. |
| Proposed `internal/client` | Existing Go `Client`/`RootClient` and client service methods. | Existing protocol and framing; no dependency on the server/agent runtime implementation. Mechanical Go caller import changes are allowed; client APIs and behavior remain unchanged. |
| Existing `protocol` and `protocoltransport` | Current public representations, schemas, negotiation, and framing. | Preserve contract and representation ownership. Do not force storage to import protocol and create a cycle. |

The desired direction is startup → daemon orchestration → services/storage,
with clients → protocol/framing. This is a responsibility map, not a claim that
the existing type graph has already become a strict layered architecture.
Check actual imports after each step and document remaining justified edges.

The provider package initially retains the current atomic host-configuration
update even though some fields concern other domains. Splitting that operation
into independent provider, MCP, and permission-setting writes would violate the
contract. Clear ownership includes making this intentional coupling visible.

For a cycle, first consider a small interface owned by the caller, with the
same arguments/results and one existing implementation. An interface that
merely exposes the whole daemon or store does not establish a useful boundary.
If breaking a cycle requires a new representation, leave that boundary in the
same package for this effort.

## 5. Compatibility evidence before structural changes

### One fixed reference, plus comparison with each PR's base

Record the development SHA, Go/Node versions, dependency lockfiles, binary
build information, generated-contract digest, SDK artifact digest, and test
commands/results. Preserve a fixed SDK built from the accepted starting
revision. Also compare each PR with its immediate base so regressions can be
attributed to one change.

Prerequisite bug/dependency repairs must be separately reviewed. Record their
SHAs and the reason for any baseline adjustment. An SDK or product behavior
change requires its own decision; it cannot become an implicit consequence of
getting the reorganization baseline green.

Use isolated temporary homes, configuration, databases, content directories,
and deterministic providers. Never exercise restart/deletion tests against a
developer's real daemon or data. Live credentials and nondeterministic provider
responses are not a suitable regression oracle.

### Three complementary checks

1. **Static contract freeze.** Reject changes to SDK implementation/public
   exports, protocol definitions/generator behavior, or contract artifacts.
   Schema and generated TypeScript directories are ignored by Git today, so a
   clean `git diff` proves nothing about them. Generate both base and candidate
   with pinned tooling into isolated directories and compare their contents.
   Keep the source/API checks and generation determinism checks as well.
2. **Fixed-client behavioral acceptance.** Run the unchanged baseline SDK and
   representative existing client builds against the candidate backend.
   Reuse current fixtures and scenarios where possible. The new test runner
   must select the candidate daemon binary explicitly: the existing SDK test
   fixture builds Go from its own repository path, so importing a baseline
   fixture accidentally can test the old backend against itself. Keep any new
   orchestration outside frozen SDK source. Record the binary and SDK digests
   actually exercised.
3. **Stateful differential scenarios.** Feed the same scripted provider replies
   and controlled actions to base and candidate. Compare responses, errors,
   ordered observations, provider requests, durable outcomes, trace graphs,
   and recovery. Compare raw wire values where stable. Normalize only named
   nondeterministic values such as timestamps, random IDs, and temporary paths;
   preserve identity relationships, cursor values, event multiplicity, causal
   order, and required timing relationships. Never sort event streams or drop
   unmatched events to make a comparison pass.

Do not try to snapshot every concurrent interleaving. Use controlled barriers
to exercise important races, then assert the existing invariants and permitted
outcomes. Avoid sleeps as the main synchronization mechanism. Keep a small
cross-cutting smoke suite and add targeted characterization before each risky
move; do not build another backend or a general simulation framework to test
this refactor.

### Required preservation matrix

The named tests below are starting evidence, not an exhaustive allowlist or
proof of sufficient coverage. Review their assertions and fill gaps before the
associated move. Run the full existing gate in addition to targeted tests.

| Surface | Behavior to demonstrate | Existing starting evidence |
| --- | --- | --- |
| Contract/validation | All registered operations, both transports, negotiation, bad input, unknown fields, error payloads, bounds and empty values. | `runtime_parity_test.go`, `v2_acceptance_test.go`, protocol tests, generated contract checks. |
| Acceptance/identity | Acceptance is durable before execution or provider construction; retries attach to the same work; lost replies do not duplicate side effects. | `TestClientAcceptancePrecedesProviderConstructionAndSurvivesDisconnect`, `TestProtocolCommandRetryAttachesToOneRootExecution`. |
| Cancellation | Before admission, queued, running, completed, disconnected, and shutdown cases preserve their distinct effects; cancel the exact command/turn. | `actor_control_test.go`, `cancel_command_test.go`, `client_control_coverage_test.go`. |
| Reconnect/order | Replay cursor boundaries, snapshot fallback, command identity, no duplicate events, stale-stream rejection, slow clients and overflow. | `root_client_test.go`, `server_test.go`, `subscription_runtime_test.go`, SDK acceptance. |
| Root/child/REPL | Both engines, inherited/overridden model configuration and authority, retained children, mail/reporting, queued prompts, kernel pressure, scratch restoration. | `recursive_runtime*_test.go`, `execution_engine_test.go`, `report_restore_test.go`, `mailbox_delivery_test.go`, RLM tests and deterministic eval. |
| Providers | Default and explicit selection, aliases, request encoding, effort/sampling, auth secrecy, discovery, retries, conflicts, disconnect, compaction and child inheritance. | `provider_*_test.go`, `compaction_fallback_test.go`, `internal/llm` tests. |
| Permissions/resources | Shared manager identity, workspace/symlink checks, delegation and revocation, partial construction failure, process survival/termination, MCP reload. | `workspace_test.go`, `session_test.go`, `filesystem_access_test.go`, capability/session permission tests, MCP tests. |
| Tracing/accounting | Same root/child causal links, model/tool/permission spans, prompt and compaction references, failures, usage/cost, pagination and OTLP content. | `trace_rpc_test.go`, `spans_prompt_test.go`, `command_trace_test.go`, `session/span_test.go`, `otlp_export_test.go`, accounting tests. |
| Persistence/recovery | Existing accepted stores still open; rejected stores remain rejected; committed results and pending work recover identically. Base → candidate → base remains usable. | Migration/checkpoint tests, `v2_crash_test.go`, `child_commit_test.go`, plus a new round-trip compatibility fixture where missing. |
| Product clients | Sessions/tabs/sidebar, history and streaming, tools/permissions/questions, provider/model settings, traces, terminals/browser, attachments, stop/reconnect. | Existing SDK, web/UI browser suites, TUI/ACP integration, desktop/mobile checks; short human smoke at each major boundary. |

For data rollback, create representative state with the base binary, stop it
cleanly, copy the complete isolated home including content/checkpoints, run the
candidate and write more state, then reopen with the base. Also cover current
crash-recovery fixtures. Compare schema metadata and logical records rather
than SQLite file bytes. No two binaries may own the same home concurrently.

## 6. Delivery sequence

Each numbered item describes a reviewable change with its own evidence. If it
needs multiple coherent moves, split it **before implementation**, preserving
the same review-before-next cadence. Aim for roughly 100–500 substantive changed
lines per PR. A large pure move needs a clear move-aware diff and explicit
review scope; it is not permission to include cleanup or behavior changes.

### Prerequisites: repair and establish the baseline

- Handle the current dependency-audit failure and any subsequently exposed
  failures as independent maintenance/bug PRs. Avoid broad automated upgrades.
- Re-run the full existing CI and acceptance gates. Record a green execution
  base and its relationship to the starting revision.
- Enable CI for the stacked PR bases before creating dependent PRs. Currently
  `ci.yml` triggers PRs only against `main` or `development`. Extend PR
  triggering to this effort's `codex/backend-reorg-*` base branches using the
  **same full gate**, and verify it runs on a PR targeting a parent branch.
  Do not broaden release/publish triggers or add a reduced substitute workflow.

These are prerequisites to production-code refactoring, not excuses to bundle
fixes into the first extraction.

### PR 1 — Establish the frozen-contract regression gate

**Change:** add the baseline manifest, generated-output comparison, explicit
fixed-SDK/candidate-backend runner, and missing high-value lifecycle/data
characterizations. Build on existing tests. A harness PR and a focused
characterization PR may be needed; review them separately.

**Risk:** test setup can accidentally compare the wrong binaries or normalize
away the regression it is supposed to detect.

**Acceptance:** the runner passes on base versus base and on the repaired
execution base, identifies the exact artifacts, and demonstrably rejects a
deliberate test-only response/event-order mismatch. Restore that deliberate
mutation; no product mutation is committed. Existing assertions and gates stay
in force. Keep future baseline updates explicit and reviewable.

### PR 2 — Separate host MCP configuration from provider ownership

**Change:** move the existing MCP import/brand-icon responsibility out of
`ProviderService` into a small host component, initially in `daemon`. Update
`mcp_import_service.go` and its direct server/control callers. Preserve existing
protocol types and the icon resolver's lazy initialization, cache, context,
timeout, and shutdown relationship. Do not change session MCP attachment.

**Risk:** provider shutdown currently supplies cancellation to this work;
splitting the struct must not extend its lifetime or change concurrent access.

**Acceptance:** import-offer/apply/skip/error/secret tests and host RPC tests
remain unchanged in meaning. Provider operations no longer own MCP import
implementation. One authoritative component still owns icon state.

### PRs 3a–3b2 — Prepare and move the provider boundary

**PR 3a:** consolidate provider operations currently implemented from
`client_control.go`, `query.go`, and `budget.go` into a small callable service
surface while still in the same package. In particular, catalog refresh/read
uses private status and refresh helpers; compaction validation and model
admission also call private methods. Move existing operation bodies rather
than exporting all private helpers. Keep serialization at the existing client
boundary, use unchanged result types, and retain the exact refresh timeouts,
configuration reloads, admission order, and error handling. Locate shared
effort/default helpers through references rather than copying them. Preserve
the single versioned configuration update and its provisioning-lock scope.

**PR 3b1:** prepare integration-test boundaries using existing HTTP/authentication
seams and public service operations. Retain every scenario and assertion; keep
service-private tests with the implementation they exercise. Do not add
production test hooks or interfaces. Review this test-only preparation before
the package move so fixture changes cannot hide an extraction regression.

**PR 3b2:** move `ProviderService` and its provider implementation files to
`internal/provider`. Keep provider RPC decoding/dispatch in the server and
client RPC methods with the client. Pass the same protocol/configuration
values directly. Preserve the existing separation from `llm` request encoders.
Move service-private tests with the service; keep cross-transport and runtime
integration tests at their existing boundary. Update constructor callers in
daemon and `cmd/whip` mechanically.

**Risk:** default selection, configuration revisions, validation/secret
redaction, auth cancellation, and background login behavior are easy to alter
while moving helper functions.

**Acceptance:** the new package has no import of `daemon`; there is one provider
service implementation; provider configuration/auth/catalog tests, fixed-SDK
settings flows, and captured provider request comparisons pass. No encoder,
retry, default, or prompt changes. Review each preparation PR before starting its successor, then review this
stopping point before proceeding.

### PR 4 — Make startup and construction responsibilities readable

**Change:** extract small construction functions from `cmd/whip/daemon.go`
for the existing runtime factory and its tool/model/MCP assembly. Keep ordinary
constructor injection. Document ownership transfer and failure cleanup next
to the constructors that perform it. Group host directory operations separately
from the mixed host dispatch file in a separate PR if that move is worthwhile.

**Risk:** moving a `defer`, context creation, discovery call, or early return
can change initialization and cleanup order even when signatures look the same.

**Acceptance:** startup/owner locks still precede database access; admission
still precedes expensive provider construction where it does today; partial
factory results close correctly; config refresh and live reload timing remain
unchanged. Runtime acceptance on Linux and macOS passes. No new application
framework or generic container.

### PR 5 — Move process-manager ownership out of storage

**Change:** construct the existing shared process manager in the daemon's
composition path and pass it to the current tool, MCP, and runtime consumers.
Remove process construction/ownership from `session.Store` after all callers
are migrated. Internal constructor signatures and test fixtures may change;
public client behavior may not.

Inventory every store opener, including non-daemon tools and tests. Give each
an explicit cleanup owner where it needs live processes; do not hide a missing
production owner by having a test fixture close leaked resources afterward.

**Risk:** root-specific `StopRoot`, accepted background work, MCP process
ownership, partial startup, and global shutdown have different lifetimes.

**Acceptance:** every root/child consumer that shares a manager today still
shares one; root cleanup and process-group behavior are unchanged; manager
closure still occurs at the corresponding shutdown point before database
closure, including failure paths and joined errors. No new process registry,
goroutine, or process policy. Storage no longer owns process lifecycle.

### PR 6 — Make workspace ownership explicit without changing authority checks

**Change:** construct the shared workspace lock coordinator at the same host
composition boundary and inject the existing resolver where needed. The store
may retain a borrowed resolver for authority validation. Prefer that limited
dependency over moving security-sensitive checks out of their transactions.

**Risk:** path canonicalization timing, symlinks, shared mutation locks, and
delegation atomicity. `Workspaces` currently has no close method; do not invent
resource cleanup behavior solely to make lifecycles symmetrical.

**Acceptance:** preserve manager sharing across roots, exact validation and
transaction order, canonical-path-change rejection, and permitted concurrency.
Storage no longer constructs the live coordinator. Do not claim storage is
independent of capability types; that is neither required nor currently true.

After PR 6, assess whether the work has delivered sufficient clarity. Provider
separation, startup readability, and explicit runtime resource ownership are
already a useful completed increment. Further steps must justify their risk.

### PRs 7–8 — Separate native Go clients from server implementation

The user has confirmed that these mechanical Go client moves and caller import
updates are in scope. Client APIs and behavior remain frozen.

**PR 7:** isolate only the connection/path/constant plumbing shared by client
and server. `RuntimePaths`, local dialing, initialization constants, and
autostart are mixed with server code today. Reuse `protocoltransport` for
framing. Identify a small neutral home for shared path/connection primitives
without moving daemon ownership/locking policy into the client. Preserve
`Paths` versus side-effect-free `ResolvePaths` semantics.

**PR 8:** move `Client`, `RootClient`, WebSocket client, and client service
methods to `internal/client`, then update Go consumers in CLI/TUI/ACP and tests
mechanically. Keep reconnect logic, identities, timers, queues and retry policy
intact. Do not add a new SDK or modify the TypeScript SDK.

**Acceptance:** no import from the Go client package into `daemon`; existing
native-client API behavior and fixed-SDK behavior remain unchanged; TUI/ACP
acceptance, transport tests, replay/cancellation tests, and package builds pass.
Do not retain a forwarding layer in `daemon` as a substitute for migrating the
internal Go callers.

### PR 9 and later — Smaller actor and persistence responsibilities, selectively

Do not begin with a wholesale split of `Session`, `RecursiveRuntime`, or
`Store`. Choose one concrete responsibility at a time after refreshing the
reference map and identifying a future change it would make easier.

Candidate individual PRs, in increasing risk:

- Group persistence operations by existing responsibility within `session`
  without moving durable types or changing SQL/transactions. Do not introduce
  repository interfaces per table or a generic unit-of-work abstraction.
- Extract a small tracing bookkeeping helper from the existing span/context
  fields, retaining all span call sites, context relationships, and persistence
  timing. Keep OTLP export behavior and prompt references intact.
- Extract a focused root-actor helper for configuration activation or MCP
  lifecycle bookkeeping. Keep invocation on the same actor/goroutine, under
  the same locks and transaction/event boundaries. Do not create another actor,
  queue, mutable projection, or scheduling state machine.

These are candidate follow-ups, not permission for an open-ended cleanup.
Before each, specify exact files/callers, ownership moved, tests, and measurable
benefit in a new inventory row. If the proposed helper needs a broad backpointer
to the whole daemon or a second copy of its state, reconsider the extraction.
Leave the code together when the resulting boundary would be artificial.

### Closeout — Prove the result and remove only demonstrated redundancy

Each move deletes its replaced implementation in that same PR. Final cleanup
removes only remaining aliases/helpers with no callers, including build-tag,
reflection, generated-code, CLI dispatch, and test-fixture references. Do not
delete a feature or fallback merely because a static reference search misses
it. Separate any substantial deletion into its own reviewed PR.

Update `docs/architecture.md` to describe the resulting implementation and
ownership map, including intentional remaining coupling. Follow
[frontend.md](frontend.md) if a Go client move affects documented client
boundaries. Keep a concise PR/evidence ledger in this plan; avoid a second
competing architecture guide. Run full final acceptance and the fixed-client
and data rollback scenarios on the integrated development revision.

## 7. Lifecycle rules for every extraction

Before changing ownership, write a small table for the touched resource:
constructor, owner, borrowers, cancellation source, normal close, partial
construction cleanup, restart cleanup, and scope of sharing. Verify actual
behavior, including idempotent or repeated close calls, rather than simplifying
it from memory.

Current shutdown spans `Server.Close`, `Daemon.Close`, root supervisors,
provider shutdown, process cleanup, SQLite, kernel-manager defers, and the
gateway/startup code. Extracting these into a single `CloseAll` loop without
preserving ordering would be a behavior change.

Preserve specifically:

- Connection cancellation versus caller cancellation versus daemon-owned work.
- Root/child cancellation scope and authority revocation.
- Worker/REPL lifetime versus retained session lifetime.
- Shared host resources versus per-root/per-turn resources.
- Lock acquisition order, transaction scope, mailbox ordering, queue bounds,
  goroutine launch/join points, and publication-before/after-commit rules.
- Partial construction cleanup, panic recovery, error propagation, and
  persistence/accounting work that must settle during shutdown.

A structural PR can change who holds a pointer. It cannot change when the
pointed-to resource becomes available or ceases to be usable.

## 8. PR, CI, and review policy

Use `codex/backend-reorg-<step>-<topic>` branches from the accepted development
base. Preserve each useful change as a separate PR and commit history. Review
the first PR before starting its successor.

The first PR of each group targets development. Each successor targets its
immediate parent's branch, and work begins only after that parent is approved
and green. Reviewed parents remain open until the group's checkpoint. There is
only one PR under active implementation or review; the other open PRs are
already reviewed dependencies.

| Merge checkpoint | Group | Evidence required before merging the group |
| --- | --- | --- |
| Baseline and guardrails | Separate prerequisite repairs/CI setup, then PR 1 (split into sequential PRs if needed). | Full baseline is green; stacked-base CI and frozen-client checks work. Land prerequisite repairs before establishing their dependent baseline. |
| Provider boundary | PRs 2, 3a, 3b1, and 3b2. | Host MCP and provider responsibilities are separated; configuration atomicity, provider semantics, and unchanged clients are verified. |
| Resource ownership | PRs 4, 5, and 6. | Startup, process ownership, and workspace ownership are explicit; lifecycle, authority, and data compatibility evidence passes. |
| Native Go client | PRs 7 and 8. | Client/server dependency is separated; Go clients, unchanged TypeScript SDK, reconnect, and cancellation acceptance passes. |
| Selected follow-ups | Each separately agreed actor, tracing, persistence, or cleanup change, or a small group defined before work starts. | The specific benefit is demonstrated and the same compatibility gates pass. No open-ended final stack. |

At each checkpoint, review the combined result and decide whether the next
group still warrants its risk. Merge approved PRs bottom-up into development,
retargeting and rebasing descendants as needed. After each merge or rebase,
run the required checks on the resulting revision; verify the integrated
development revision before starting the next group. Preserve separate PR
review records. Do not auto-merge or use one final integration-only PR to
repair broken intermediate states.

Every PR description should contain:

1. The concrete responsibility being moved and resulting owner.
2. The scope of moves versus substantive edits, including all changed callers.
3. Why behavior is unchanged, identifying sensitive ordering/lifetime points.
4. Tests run on the exact submitted SHA, fixed SDK/backend identities, generated
   contract comparison, and applicable data/restart evidence.
5. Any uncovered scenario or remaining coupling, plus the next safe stopping
   point. An untested requirement is not marked complete.

Keep normal CI intact: formatting, vet, `whipvet`, lint, dependency hygiene,
the existing portable race/shuffle suite and **90% coverage floor**, cross
builds, Linux/macOS runtime acceptance, SDK/package/browser/product tests,
docs, desktop, mobile, and distribution checks. Preserve the aggregate `go`
job and all its dependencies. Add frozen-client checks to the required gate.

`Taskfile.yaml`'s `acceptance` task already covers recursive-agent, capability,
mailbox, reconnect, kernel, TUI/ACP, accounting, and SDK scenarios. Use it with
the existing CI workflow rather than inventing a narrower active test list.
Local full-module build/vet/test checks complement hosted coverage; browser
and TUI tests require their appropriate browser/TTY environment. Use targeted
tests during edits and complete the required gate once the PR is ready.

Move existing tests with implementation only when necessary. Update
architecture tests to follow the new owners while preserving their assertions.
For example, provider-call checks currently scan the daemon directory and
name `agent_session.go`; after a relevant move, scanning only the old directory
could silently stop enforcing the boundary. Update coverage/package selection
to include new packages, not exclude moved code.

Add focused import checks for boundaries actually introduced: provider must
not import daemon; client must not import the server implementation; storage
must not construct process managers after that step. Avoid blanket rules that
contradict the existing shared-type graph.

## 9. Stop, rollback, and scope control

Stop the current extraction when a frozen contract differs, a behavior test
fails, lifecycle/trace evidence is missing, a gate is skipped, or the move
requires a new representation. Find the structural mistake or revert to the
last green step. Do not repair the failure by modifying clients, weakening
assertions, accepting data loss, or redesigning the protocol.

If evidence reveals a pre-existing bug, reproduce it on the base, record it,
and propose a separate fix. If that fix must precede the refactor, pause the
dependent PR and resume only after the fix is reviewed and the reference
evidence is updated deliberately.

Each merged PR must be reversible independently. Schema compatibility removes
the need for a migration rollback, but still test reverting the binary after
candidate writes. If a later PR depends on an earlier one, revert dependent
changes first or restore the last verified development checkpoint. Never
repair test data by deleting a real runtime home.

Keep the following out of scope unless a future proposal explicitly reopens
them: uniform root/child durable records, a new scheduler, protocol redesign,
SDK state redesign, new permission/budget/retry policies, schema normalization,
new provider defaults, trace redesign, performance tuning, platform removal,
and feature retirement.

## 10. How to judge whether the work was worth doing

At the provider, resource-ownership, and native-client checkpoints, record:

- Which unrelated responsibilities left a broad owner, and the APIs left behind.
- Which dependency edges were removed and which remain intentionally.
- Whether a concrete change—provider setup, MCP import, runtime construction,
  or client connection handling—now has an obvious home and focused tests.
- Whether each moved resource has one documented owner and unchanged borrowers
  and lifetime, without duplicate mutable state or forwarding layers.
- Added versus removed non-test code, exported symbols, and abstractions;
  explain any growth needed for a real boundary.
- The unchanged contract digest and complete behavioral acceptance evidence.

Reduced file size alone does not count as success. More packages and interfaces
are not inherently improvements. Continue only when the next extraction makes
the backend easier to work on enough to justify its review and regression risk.

## Research source map

Primary implementation references at the recorded revision:

- Startup/lifecycle: `cmd/whip/daemon.go`, `internal/daemon/daemon.go`,
  `server.go`, `session.go`, `socket_unix.go`.
- Services: `internal/daemon/provider_*.go`, `mcp_import_service.go`, `host.go`.
- Runtime: `internal/daemon/agent_session.go`, `recursive_runtime.go`,
  `spans.go`; `internal/agent`, `internal/rlm`.
- Storage/authority: `internal/session/session.go`, `capability.go`,
  `permission.go`, `migrations.go`, `checkpoint.go`, span/OTLP files;
  `internal/capability/workspace.go`.
- Clients/contracts: `internal/daemon/client.go`, `root_client.go`,
  `websocket_client.go`, `internal/protocol`, `internal/protocoltransport`,
  `packages/protocol/scripts/check.mjs`, `packages/sdk/src/testing-node.ts`.
- Gates and guides: `.github/workflows/ci.yml`, `Taskfile.yaml`,
  `internal/daemon/architecture_test.go`, `docs/architecture.md`,
  `docs/frontend.md`, `packages/sdk/README.md`.

Research included local source/dependency inspection, language-server reference
mapping, the previous redesign plan and selected GitHub PRs, and current
development CI logs. At research time, full baseline acceptance remained a
prerequisite. The execution ledger below records subsequent repairs and results.

## Execution ledger

All PRs remain unmerged. Rebased dependency heads are recorded below; successful
checks on an earlier head are not a claim that a current-head rerun has finished.

| Step | Revision and PR | Evidence and status |
| --- | --- | --- |
| Dependency-audit prerequisite | `db3a1cea247eee7cbaa0424fe3ee89eb0d36bf9d`, [PR #299](https://github.com/context-labs/whip/pull/299) | Only 11 `brace-expansion` lockfile entries changed. Local install, unchanged production-audit threshold, protocol freshness, 470 SDK unit tests, example build, and package smoke passed. [Full CI](https://github.com/context-labs/whip/actions/runs/36678816413) and security passed. This is the green maintenance base; the original development SDK remains the fixed oracle. |
| Stacked-base CI prerequisite | `dba610243623b1a7f6f13f910ddec4d6df9bb711`, [PR #300](https://github.com/context-labs/whip/pull/300) | Only CI/security PR-base filters and their distribution-test expectation changed, plus this plan. All 36 distribution-policy tests and workflow validation passed. GitHub verified both workflows start against stacked bases. [Full current CI](https://github.com/context-labs/whip/actions/runs/36681079742) and current security passed. |
| Isolated provider test environment | `a57b5261b6225b64451b0d5b06374f721cb83153`, [PR #301](https://github.com/context-labs/whip/pull/301) | Separate test-only repair prevents inherited provider credentials from changing fixtures. [Full current CI](https://github.com/context-labs/whip/actions/runs/36681286958) and current security passed. |
| Frozen-contract gate | `a7be9a28a0f37598985733e2fe50e06af1aa06f4`, [PR #302](https://github.com/context-labs/whip/pull/302) | Local fixed-reference and immediate-base comparisons passed, including mutation rejection and both-engine data rollback. [Current full CI](https://github.com/context-labs/whip/actions/runs/36681287828) completed: both Linux/macOS compatibility jobs, Go tests with the 90% coverage floor, lint and runtime checks passed. The aggregate failed the independently reproduced UI scroll-readiness race repaired downstream in PR #303; current security passed. |
| Dialog assertion readiness | `1882979ad414c256cada7b2fb71e11a02f46bd4e`, [PR #303](https://github.com/context-labs/whip/pull/303) | Separate test-only repair waits for sheet focus before scroll assertions. The combined 36-test workflow-policy suite passed. [Full current CI](https://github.com/context-labs/whip/actions/runs/36681287194) passed, including SDK/UI/browser/product, both compatibility jobs, Go tests with the 90% coverage floor, lint, runtime, mobile, builds, docs, desktop and distribution. Current security also passed; this is the combined green prerequisite baseline. |
| Host MCP ownership (plan PR 2) | `d8175212d3db3371b14862dc07ad24fc62393108`, [PR #304](https://github.com/context-labs/whip/pull/304). | Three operation bodies move unchanged from `ProviderService` to a private `hostMCPService`, owned by `Server`. The real HTTP cancellation characterization passed against the original implementation before extraction. Focused MCP/icon race tests, full-module build/vet/whipvet, and the full daemon race/shuffle suite (250.722s) passed. Fresh fixed-SDK comparisons passed against the original reference and immediate base `1882979ad414c256cada7b2fb71e11a02f46bd4e`, including both transports, mutation rejection, lifecycle and both-engine data rollback. [Full current CI](https://github.com/context-labs/whip/actions/runs/36682214774) and current security passed. |
| Provider operation boundary (plan PR 3a) | `a211fb0e7629f9692eb5133aa00244aea17e38a9`, [PR #305](https://github.com/context-labs/whip/pull/305). | Catalog operation/reload characterization passed three times under the race detector on unchanged production code before extraction. Typed catalog results, compaction validation, model admission and provider-context borrowing now form the narrow operation surface. Shared effort and permission-default helpers move once to their existing configuration/session owners. Validation results are recorded below. [Current CI](https://github.com/context-labs/whip/actions/runs/36683418233) is running; current security passed. |
| Provider integration test boundary (plan PR 3b1) | This change, based on `a211fb0e7629f9692eb5133aa00244aea17e38a9`. | The five existing affected integration tests now use HTTP/auth persistence rather than private provider callbacks. They passed three times under the race detector (23.256s). Full-module build, vet and whipvet and fresh fixed-SDK compatibility passed. The full daemon race/shuffle suite passed (261.125s). |

### Host MCP ownership and lifetime

| Concern | Preserved behavior after extraction |
| --- | --- |
| Construction and owner | `NewServer` creates one `hostMCPService` after selecting its provider; `Server` owns the component. The production composition has one server per daemon/provider. |
| Borrowers | The three existing host MCP RPC cases call the component directly with the same protocol values. No forwarding methods remain on `ProviderService`. |
| Cancellation | The component borrows the exact selected provider context. Supplied providers retain their independent lifetime; server or connection cancellation does not replace it. `ProviderService.Close` still cancels icon requests at the existing shutdown point. |
| Lazy resources and sharing | The first enabled icon lookup creates one resolver through the existing `sync.Once`. Cache location, timeout, in-flight sharing and disk cache behavior remain unchanged. Imports do not construct or launch MCP servers. |
| Close and failure cleanup | The component has no independent close action or goroutine. Server shutdown order is unchanged, including provider close before worker join. Construction allocates no resolver, so there is no new partial-construction cleanup. |
| Persistent and session state | Host imports retain the existing single versioned config update, validation and idempotence. Session MCP lifecycle, provider configuration updates, protocol and schema remain unchanged. |

This step removes host import and icon implementation ownership from provider
onboarding. Provider context borrowing is intentional until a later separately
reviewed composition change can preserve the same cancellation boundary.

### Provider operation boundary

`ProviderService.ListCatalogs` owns the existing catalog read/refresh operation
and returns `protocol.ProviderCatalogsResult` directly. The daemon retains JSON
serialization, query decoding and the query-only 30-second deadline; per-provider
five-second fetches and the final configuration reload are unchanged. The new
blocked-fetch characterization proves that a concurrent configuration change is
reflected in the complete returned result and cannot republish a disabled route.

`ConfigureCompaction` and `CheckModelProvider` are the existing operations with
exported names. Their call sites keep the versioned configuration callback,
provisioning-lock scope, admission check, accounting reservation and tracing in
exactly their previous order. `Context` exposes the existing provider lifetime
for the host MCP borrower without creating or cancelling another context.

The catalog-to-storage helper stays private with provider implementation.
`config.ValidateConfiguredEffort` now owns the one shared configuration/catalog
validation body; `session.DefaultPermissionMode` owns the one legacy fallback
body beside its existing constants. Callers retain the same error strings,
explicit-off handling and unrecognized-mode fallback. No new type, configuration
write, package, translation layer or runtime construction is introduced.

Local validation passed: full-module build, vet and whipvet; focused provider and
registry race tests (18.710s); config and session race suites (1.504s and 147.339s);
and the full daemon race/shuffle suite (251.788s). Fresh compatibility comparisons
passed against the fixed SDK reference and immediate base `d8175212d3db3371b14862dc07ad24fc62393108`,
including both transports, mutation rejection, lifecycle and both-engine data
rollback. The compatibility evidence records the tested working diff and binary
and contract hashes; the submitted PR records the commit and hosted CI results.
Existing CI and frozen-contract gates remain required.


### Provider integration test boundary

The existing onboarding, custom-connection, host RPC, Unix/WebSocket login and
recursive subscription-runtime tests stay in daemon. Local HTTP fixtures now
exercise model discovery, device authorization, workspace/project selection,
key provisioning and credential persistence through their existing public APIs.
Assertions retain the same token, team and project identities, configuration
conflicts, cancellation/recovery, unsupported rotation, and secret isolation.
A persisted per-project machine key also connects the provisioning request to
its saved identity. Runtime credentials are seeded by a separate public
`openaiauth.Manager` before the provider service reads them.

The unsigned-account rotation assertion runs before login because successful
login now actually persists credentials. The failed-rotation assertion remains;
no production behavior changes. HTTP overrides are installed before daemon
construction and restored after its workers close. Unexpected external hosts
fail the test, and all fixture data lives in temporary homes. The service-private
unit tests remain in place for the separate package-move PR 3b2.

Fresh compatibility evidence at `/private/tmp/whip-backend-compat-pr3b1-fresh/evidence.json`
records the immutable SDK reference `271c0f8d2a35648d1b45056d57432590b783483c`,
immediate base `a211fb0e7629f9692eb5133aa00244aea17e38a9`, and the complete staged
test diff, including the new fixture. Node v24.14.1 and Go 1.27.0 on darwin/arm64
built all three selected integration binaries freshly. SDK and generated
contract artifacts matched; both transport comparisons, deliberate mutation
rejection, lifecycle checks, and both-engine fixed/base → candidate → base data
rollback passed. Production source, SDK, schema and CI requirements are unchanged.
