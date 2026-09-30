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
| `internal/client` | Existing Go `Client`/`RootClient` and client service methods. | Existing protocol and framing; no dependency on the server/agent runtime implementation. Mechanical Go caller import changes are allowed; client APIs and behavior remain unchanged. |
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

All active review PRs remain unmerged. Rebased dependency heads are recorded below; successful
checks on an earlier head are not a claim that a current-head rerun has finished.
The current order is 299 → 300 → 301 → 307 → 302 → 304 → 305 → 306 → 308 → 309 → 310 → 311 → 312 → 313 → 314 → 315 → 316 → native client extraction.
The separate UI-readiness repair now precedes the compatibility gate so that
PR #302 inherits that repair in its own required CI. The combined tree at the
new PR #302 head is identical to the previous PR #303 tree; PRs #304–#306 and
the 3b2 code commit likewise retain their previous complete trees. The reorder's
ledger update changed only documentation. Earlier SHA-specific evidence below remains
historical evidence; the reordered heads require fresh hosted results.
GitHub automatically marked historical PR #303 merged into its former stack
parent when the reordered parent contained its commit. Replacement PR #307 keeps
that separate repair reviewable against PR #301. No merge command ran, and
development remains at `271c0f8d2a35648d1b45056d57432590b783483c`.

| Step | Revision and PR | Evidence and status |
| --- | --- | --- |
| Dependency-audit prerequisite | `db3a1cea247eee7cbaa0424fe3ee89eb0d36bf9d`, [PR #299](https://github.com/context-labs/whip/pull/299) | Only 11 `brace-expansion` lockfile entries changed. Local install, unchanged production-audit threshold, protocol freshness, 470 SDK unit tests, example build, and package smoke passed. [Full CI](https://github.com/context-labs/whip/actions/runs/36678816413) and security passed. This is the green maintenance base; the original development SDK remains the fixed oracle. |
| Stacked-base CI prerequisite | `dba610243623b1a7f6f13f910ddec4d6df9bb711`, [PR #300](https://github.com/context-labs/whip/pull/300) | Only CI/security PR-base filters and their distribution-test expectation changed, plus this plan. All 36 distribution-policy tests and workflow validation passed. GitHub verified both workflows start against stacked bases. [Full current CI](https://github.com/context-labs/whip/actions/runs/36681079742) and current security passed. |
| Isolated provider test environment | `a57b5261b6225b64451b0d5b06374f721cb83153`, [PR #301](https://github.com/context-labs/whip/pull/301) | Separate test-only repair prevents inherited provider credentials from changing fixtures. [Full current CI](https://github.com/context-labs/whip/actions/runs/36681286958) and current security passed. |
| Dialog assertion readiness | `722b49321f3cb2a70c8d829911279ffd79caf0a0`, [PR #307](https://github.com/context-labs/whip/pull/307), based on PR #301. | Separate test-only repair waits for sheet focus before scroll assertions. The prior combined head `1882979ad414c256cada7b2fb71e11a02f46bd4e` passed the 36-test workflow-policy suite, [full CI](https://github.com/context-labs/whip/actions/runs/36681287194) and security, including both compatibility jobs and the 90% coverage floor. This repair now precedes the guard; [reordered-head CI](https://github.com/context-labs/whip/actions/runs/36685631006) and [security](https://github.com/context-labs/whip/actions/runs/36685630150) passed. |
| Frozen-contract gate | `8f0230444eb447e87c619d7c689eca811ac5d1f3`, [PR #302](https://github.com/context-labs/whip/pull/302), based on PR #307. | Local fixed-reference and immediate-base comparisons passed, including mutation rejection and both-engine rollback. At historical head `a7be9a28a0f37598985733e2fe50e06af1aa06f4`, [CI](https://github.com/context-labs/whip/actions/runs/36681287828) passed both compatibility jobs, Go coverage, lint and runtime but failed the independently reproduced UI race. The new head includes the separate repair and has the exact previously green combined prerequisite tree. [Fresh CI](https://github.com/context-labs/whip/actions/runs/36686140019) remains pending. |
| Host MCP ownership (plan PR 2) | `bfd030fb7bee920982c8d0ab9f6a4820be962166`, [PR #304](https://github.com/context-labs/whip/pull/304). | Three operation bodies move unchanged from `ProviderService` to a private `hostMCPService`, owned by `Server`. The original-implementation cancellation characterization, focused MCP/icon race tests, build/vet/whipvet, full daemon race/shuffle (250.722s), and fixed/immediate-base compatibility passed. Historical head `d8175212d3db3371b14862dc07ad24fc62393108` passed [full CI](https://github.com/context-labs/whip/actions/runs/36682214774) and security. Its tree is unchanged by reordering; [fresh CI](https://github.com/context-labs/whip/actions/runs/36686146852) is pending. |
| Provider operation boundary (plan PR 3a) | `fec68225b2e6e888f138d2721c30d827987add73`, [PR #305](https://github.com/context-labs/whip/pull/305). | Catalog/reload characterization passed three times before extraction; typed catalog results, compaction validation, model admission and context borrowing retain their operation order. Local validation below was recorded at historical head `a211fb0e7629f9692eb5133aa00244aea17e38a9`. The complete tree is unchanged by reordering; [fresh CI](https://github.com/context-labs/whip/actions/runs/36686153135) is pending. |
| Provider integration test boundary (plan PR 3b1) | `cb959b12336d403ece55abd923afd09a4f98cc94`, [PR #306](https://github.com/context-labs/whip/pull/306). | Five integration tests use HTTP/auth persistence rather than private provider callbacks. At historical head `d81f814b139d3a2320db6942e02c22a3e54d9b80`, targeted race tests passed three times (23.256s), full daemon race/shuffle passed (261.125s), and build/vet/whipvet plus fresh fixed-SDK compatibility passed. The complete tree is unchanged; [fresh CI](https://github.com/context-labs/whip/actions/runs/36686160171) is pending.  The current head includes the inherited header-spelling correction; earlier run results refer to the prior head. |
| Provider package extraction (plan PR 3b2) | `4b5c0e5790f698371abed1ed4c8bcf3dd9cd3885`, [PR #308](https://github.com/context-labs/whip/pull/308). | Eleven implementation files move to `internal/provider` with existing service/constructor names. The declaration proof preserves all 1,188 existing production declarations after type qualification; all 758 existing tests remain. Private tests move with the service; transport/runtime/default-pair/catalog-reload assertions remain in daemon. This code tree equals historical tested commit `f45cecb74dfe0c02826c044ace30112e53e0bccf`. Fresh publication-provenance compatibility passed against the new immediate parent. [Current CI](https://github.com/context-labs/whip/actions/runs/36685870687) and [security](https://github.com/context-labs/whip/actions/runs/36685870367) passed.  The current head includes the inherited header-spelling correction; earlier run results refer to the prior head. |
| Startup factory extraction (plan PR 4a) | `b251786c8d868cba722f103d0a77ce72d07377f5`, [PR #309](https://github.com/context-labs/whip/pull/309). | The existing runtime factory and five helpers move to `cmd/whip/daemon_runtime.go`. Exact body comparison, full CLI race/shuffle (71.763s), module build/vet/whipvet, and fresh fixed/immediate-base compatibility passed. [CI](https://github.com/context-labs/whip/actions/runs/36686776874) and [security](https://github.com/context-labs/whip/actions/runs/36686776543) started; results were pending when this entry was recorded.  The current head includes the inherited header-spelling correction; earlier run results refer to the prior head. |
| Host directory grouping (plan PR 4b) | `774623f12ef182856862209d186bc70150d17cf1`, [PR #310](https://github.com/context-labs/whip/pull/310). | Four existing directory listing, creation and native-picker functions move to `internal/daemon/host_directory.go`. AST-directed extraction preserves all six original function declarations and comments, leaving host dispatch and attention in `host.go`. Existing host/directory/RPC race/shuffle tests (4.644s), module build/vet/whipvet and exact source comparison passed. Fresh fixed/immediate-base compatibility passed. [CI](https://github.com/context-labs/whip/actions/runs/36687285608) and [security](https://github.com/context-labs/whip/actions/runs/36687285375) were pending at publication.  The current head includes the inherited header-spelling correction; earlier run results refer to the prior head. |
| Process ownership (plan PR 5) | `42285977df445ac79334ac96e465d44f057f0350`, [PR #311](https://github.com/context-labs/whip/pull/311), based on PR #310. | Baseline lifecycle characterizations were committed and passed before production edits. Startup now constructs the shared manager explicitly and transfers ownership to the daemon only after successful construction. Validation and the detailed ownership map are recorded below. |
| Workspace ownership (plan PR 6) | `codex/backend-reorg-06-workspace-ownership`, based on PR #311 (`42285977df445ac79334ac96e465d44f057f0350`). | A green, mutation-calibrated composition test precedes the ownership change. Host startup supplies one coordinator to storage; existing root/child/tool borrowers retain the same accessor and implementation. Validation and the deliberate remaining dependency are recorded below. |
| Gateway fixture synchronization | `067fe82cd90b99aed42efa80c24e3917ba96e820`, [PR #313](https://github.com/context-labs/whip/pull/313), following workspace ownership. | Eight test-only lines synchronize the two pipelined browser writes before backend rejection. Existing assertions remain exact; the detailed baseline diagnosis and checks are below. |
| Browser performance probe repair | `codex/backend-reorg-06b-performance-probe`, based on PR #313. | A separate instrumentation repair measures commit markers received in snapshots as well as notifications, preserving exact event sequences and first receipt. Focused negative-control regressions, full ordinary/controlled browser runs, and source conservation are recorded below. |
| Shared native plumbing (plan PR 7) | `3127d8ef67c3d59cd70f55ad697102e275c2a6fc`, [PR #315](https://github.com/context-labs/whip/pull/315), based on PR #314. | Shared connection primitives and command presentation move without changing client/server policy. The declaration, characterization, full affected-suite and fresh fixed/immediate-base evidence is recorded below. The PR remains unmerged. |
| Tagged TUI title fixture repair | `e289b4ea3344b6fdd45a8782d17c5c66084e90e4`, [PR #316](https://github.com/context-labs/whip/pull/316), based on PR #315. | Separate test-only prerequisite fixes a title-generation fixture that already failed on the starting revision's policy. Both transports retain the original final assertions; the existing tagged acceptance test is added to Linux/macOS runtime CI. Diagnosis and evidence are recorded below. |
| Native client extraction (plan PR 8) | `codex/backend-reorg-08-native-client`, based on PR #316 (`e289b4ea3344b6fdd45a8782d17c5c66084e90e4`). | Exact implementation/private-test moves and canonical Go imports preserve all client APIs, bodies and assertions. Full affected race/PTY suites, named native acceptance and static checks passed. After the separate inherited title-fixture repair, the tagged TUI gate and refreshed fixed/immediate-parent guard passed at clean code commit `ecf96616acf48233658620c81286a08fd9bada80`; final documentation records the complete evidence below. |

Fresh post-reorder evidence is recorded at
`/private/tmp/whip-reorg-03b2-published-compat-evidence/evidence.json`: fixed
reference `271c0f8d2a35648d1b45056d57432590b783483c`, immediate parent
`675717575868c5b3b9bf2da1a9b2863238b12a28`, and candidate code commit
`98a2401cbf8a75cc9fe79c224b28427bde2b3b77`. Fresh fixed/parent/candidate binaries
passed both transport comparisons, response/order mutation rejection, lifecycle
checks, and both-engine retained-root/child/content/schema/trace rollback.
The following commit updates only this ledger; its product and test source
remains identical to that recorded candidate.

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

### Provider package ownership

`internal/provider` owns the existing `ProviderService` implementation and its
private configuration, discovery, catalog and account helpers. The service still
uses the same configuration, authentication, model-client and permission-default
dependencies. Daemon constructs or receives it at the same call sites and closes
it at the same lifetime boundaries. Host MCP borrows the exact provider context
through `Context`; no resource or shutdown order changes.

Daemon retains RPC decoding, native client methods, protocol type aliases, query
deadlines and serialization. The provider package uses existing protocol values
directly and has no daemon import or forwarding service implementation. A narrow
architecture test enforces that import direction. Model-call checks cover both
packages and exempt only `internal/daemon/agent_session.go`.

Private service tests move with their implementation. Both packages retain
equivalent temporary-home and provider-environment isolation, including a
child-process regression. All existing test bodies are preserved after type
qualification except the expanded architecture scan and the approved Cerebras
catalog read helper, which now reads the typed service result with unchanged
membership, request-count, error, restart and cache assertions. Daemon client
tests and the frozen guard retain serialization coverage. The named model
acceptance command adds the provider package so the moved snapshot test remains
selected; its original packages, regex and all CI requirements are unchanged.

Local build, vet, whipvet and integration-fixture compilation passed. The full
provider and daemon race/shuffle suites passed (3.078s and 252.881s), as did the
relevant CLI startup/routing/compaction/authentication tests (5.804s). Focused
architecture checks passed after adding the import guard (1.717s); JSON test
events confirm the moved model-snapshot acceptance test ran and passed. The
declaration comparison accounts for 80 declarations in the new package and
preserves every existing production declaration. The test-name comparison
preserves all 758 existing tests and adds only provider-environment isolation
and the provider-to-daemon import guard.

Fresh compatibility evidence at `/private/tmp/whip-reorg-03b2-compat-evidence/evidence.json`
passed against the fixed SDK revision `271c0f8d2a35648d1b45056d57432590b783483c`
and immediate base `d81f814b139d3a2320db6942e02c22a3e54d9b80`. Freshly built
fixed/base/candidate binaries passed both transport comparisons, actual response
and ordering mutation rejection, lifecycle checks, and both-engine retained
root/child/content/schema/trace rollback. All new files were staged before the
run captured its candidate diff. Later ledger additions record these results;
production and test source match the captured candidate.


### Startup factory extraction (plan PR 4a)

`cmd/whip/daemon.go` now contains the startup and shutdown sequence in 186 lines;
`daemon_runtime.go` contains runtime, tool, model, MCP and compaction assembly.
The only new function, `daemonRuntimeFactory`, returns the existing
`daemon.Factory` and borrows the same store, provider service, kernel manager and
limits. The daemon owns the returned session components. Startup still owns
its locks and deferred provider/kernel cleanup; no new lifetime or interface is
introduced.

The Go AST-directed move preserved the complete factory literal and all five
helper declarations byte for byte. The `runDaemon` token stream is unchanged
apart from replacing its factory assignment. Config reloads, MCP discovery,
model/provider/effort options and partial failure cleanup remain in their
original order. `daemonKernelCommand` is still read inside each factory
invocation. The existing CLI test files are unchanged, and the compaction guide
now points to the helper's new file.

Validation passed: `go build ./...`, `go vet ./...`,
`go run ./cmd/whipvet ./...`, and the full
`go test -race -shuffle=on -count=1 ./cmd/whip` suite (71.763s). The CLI suite
exercises the moved construction code; the independent compatibility guard
continues to exercise the daemon integration fixture. No new tests were needed
for the unchanged bodies.

Fresh guard evidence is at
`/private/tmp/whip-backend-compat-pr4a-fresh/evidence.json`: fixed SDK/reference
`271c0f8d2a35648d1b45056d57432590b783483c` and immediate parent
`355b792bc99567068e65fdd0fd9f992a5b50ea0c`, with Go 1.27.0 on darwin/arm64 and
Node 24.14.1. The staged source move is recorded in `candidateDiff`; the
precommit `candidateRevision` is the parent. Later documentation updates record
the results without changing the tested source. The fresh candidate daemon
binary SHA-256 is
`64623692653b4046145dae98e7ea4fcf94820bd6c19383a8f1ff531a157b322c`.
Both transport comparisons, deliberate response/order mutation rejection,
cancellation/reconnect checks and fixed/immediate-base rollback passed. Rollback
covers both engines, retained root/child state, content, schema and trace/export.


### Host directory grouping (plan PR 4b)

`internal/daemon/host_directory.go` groups directory listing, creation and native
folder selection in the existing daemon package. `host.go` now contains 176
lines of host RPC dispatch and attention aggregation. This makes filesystem and
platform picker behavior independently findable without a new service or
interface. Callers and tests stay unchanged.

The Go AST-directed extraction moved exactly `hostDirectories`,
`hostDirectoryCreate`, `directoryPickCommand`, and `hostDirectoryPick`. All six
original function declarations and their comments are byte-identical; the
remaining source is unchanged apart from imports and whitespace. Directory
filtering, bounds, errors, permissions, platform commands and cancellation
behavior therefore retain their original implementation. Three current directory
source references in `docs/features.md` follow the move; theme references still
point to host dispatch.

Existing host, directory and related RPC race/shuffle tests passed (4.644s), as
did module build, vet and whipvet. The same-package move adds no test or runtime
interface; existing tests remain in place. The full daemon suite was not repeated
locally for this byte-identical same-package move; all required hosted jobs
remain enabled.

Fresh compatibility evidence at
`/private/tmp/whip-reorg-04b-compat-evidence/evidence.json` passed against fixed
SDK/reference `271c0f8d2a35648d1b45056d57432590b783483c` and immediate parent
`13493cf20dd84c7c97a18d33b1a6202d849eae94`, using Go 1.27.0 darwin/arm64 and
Node 24.14.1. All four changed files, including the new Go file, were staged
before the guard captured `candidateDiff`; its precommit `candidateRevision`
is the parent. Subsequent changes only correct a documentation source reference
and record these results; production source matches the tested candidate.

Fresh fixed, parent and candidate binaries passed both transport transcript
comparisons, deliberate response/event-order mutation rejection, lifecycle
checks and fixed/immediate-base rollback covering both engines, retained
root/child state, content, schema and trace/export. The candidate binary SHA-256
is `7f2bff23200cb61ecdb73b4939ca643cc8064a2212ae89421c67b3a66a48e85f`.


### Process ownership (plan PR 5)

The process manager moves from `session.Store` to daemon composition. The
production change touches eight files: startup construction/failure cleanup,
the daemon and root fields/constructors/teardown, existing agent/tool/recursive
borrowers, and removal of the Store field/constructor/accessor. The process
implementation, SQL, schema, provider lifecycle, and client protocol are
unchanged. Workspace ownership remains in storage for the separate next step.

| Boundary | Owner and lifetime after this change | Preserved behavior |
| --- | --- | --- |
| Construction | `runDaemon` constructs one manager immediately after successful `session.Open`. | Environment snapshot precedes permission rules, daemon generation, provider discovery, kernels, and root factories. |
| Failed startup | Caller closes manager, then Store, after generation or `daemon.New` failure. | Original failure remains the return value; provider/kernel defers retain their existing order. |
| `daemon.New` | Successful construction adopts the supplied Store and manager. Failure adopts neither. | Existing nil Store/factory error has priority; duplicate ownership and recovery failure preserve caller resources; recovery failure releases the Store guard. Nil manager is rejected explicitly after the existing input checks. |
| Borrowers | Every root borrows the same daemon pointer; root/child tool services and MCP retain that pointer and root scope. | No per-root manager, lazy allocation, new registry, or process policy. Standalone MCP probing keeps its independent owner. |
| Root/partial bind teardown | Existing root teardown and failed-open defer call `StopRoot(rootID)`. | Defer placement, component close order, worker joins, settlement, and root isolation are unchanged. Root cleanup does not close the shared manager. |
| Global shutdown | At the old final Store-close point, daemon joins `processes.Close()` then `store.Close()`. | Callbacks can still use the database; sentinel errors remain joined; `sync.Once` caches the same repeated Close result and guard release stays deferred. Root errors are not newly aggregated. |
| Fixtures | Native/tagged fixtures name their manager and pass it explicitly; successful owners close it before fallback test cleanup. | Retry/duplicate constructors reuse a handle; reopened owners receive a fresh one. Direct borrowers close before their manager and database. `openStore` gains no hidden cleanup or runtime resource. |

Before production edits, four characterizations locked down environment snapshot
timing, live-resource recovery failure and retry, shutdown database usability
with joined/cached errors, and partial-bind/root StopRoot isolation. Existing MCP,
agent, fresh recursive child/grandchild, restored child, and duplicate-owner tests
gained pointer/liveness assertions. The eight focused race cases passed in
4.481s at test-only commit `05eef77e9ff1b8ca6ac2dac4a17994dc594bf59e`.
The same cases plus explicit nil-manager/error-priority and storage-construction
checks passed after the move in 4.493s. No test assertion was weakened to accept
changed runtime behavior; only the obsolete Store-owned process accessor
assertion was removed from storage's accessor test.

The AST-directed fixture migration and an exact source conservation check cover
all eight changed production files, permitting only the enumerated ownership
edits. All 380 pre-existing test function names in changed files remain. Evidence:
`/private/tmp/whip-pr5-fixtures-transform.log` and
`/private/tmp/whip-pr5-production-proof.log`.

Local validation passed: full daemon race/shuffle (275.485s), session (148.173s),
capability (30.041s), tools (14.327s), MCP (22.298s), and agent (9.192s) race
suites; full CLI race/shuffle (68.494s); integration-tagged daemon/TUI/CLI fixture
compilation; and module build, vet, and whipvet. Final test-only cleanup-order
adjustments passed focused daemon/tools race runs (2.381s/3.911s). Independent
review caught a restart fixture reusing its old closed manager after direct
`session.Open`; its new lifetime now receives a fresh named manager and an
explicit registration/liveness assertion, which passed with race detection
(2.083s). Production behavior was unchanged by that fixture correction.

The initial frozen compatibility run against the fixed reference and full PR4b
parent `0099a665221ba54ac23ca6656a8a1a017af6f48d` passed:
`/private/tmp/whip-pr5-compat-evidence/evidence.json`. It covers Unix/WebSocket
responses and ordered events, acceptance/cancellation lifecycle, and both-engine
root/child persisted-data rollback with content, schema, trace pages, and OTLP
export. The final published-parent run will refresh provenance after the
inherited test-header lint correction is rebased through the stack; the current
result records the pre-rebase parent and the initial captured working diff.

A preflight with CI's golangci-lint 2.13.1 found only the inherited lowercase
provider-header fixture after the new tests adopted explicit SQL contexts and a
narrow explained exception for the required cached-error identity comparison.
Those two fixture cases passed again with race detection (1.916s). The inherited
header correction belongs to PR3b1 and will arrive through the parent rebase;
PR5 does not mix that repair into its ownership change.

The inherited header fixture correction was committed in PR3b1, then rebased
through PR3b2/PR4a/PR4b/PR5. The final PR5 code head
`057edb919da1a652fb284e2d54d4cf020a7867c6` differs from the locally race-validated
`c339eba56b79ceb9dae78d95c2cb9892ab072bf9` only by that canonical spelling of the
same case-insensitive `Header.Get` key. The parent mapping is recorded at
`/private/tmp/whip-reorg-header-correction-rebase.json`.

Final full golangci-lint 2.13.1 passed with zero issues. Fresh frozen compatibility
passed at the clean committed code head against immediate parent
`774623f12ef182856862209d186bc70150d17cf1`, with the fixed reference unchanged:
`/private/tmp/whip-pr5-published-compat-evidence/evidence.json`. The evidence has
an empty candidate diff and passes all calibrated transport/event, lifecycle,
mutation-rejection, and both-engine fixed/immediate-base rollback checks. This
recording update changes documentation only; hosted checks run on the published
PR head. The PR remains unmerged for review.


### Workspace ownership (plan PR 6)

Host startup now creates one `capability.Workspaces` immediately before
`session.Open(path, workspaces)`. Storage borrows the supplied coordinator and
rejects a nil argument explicitly. `Store.Workspaces()` remains the intentional
borrowed-reference boundary: authority validation and the existing runtime
borrowers already need the store, so they use its same coordinator. There are
no additional daemon/root pointer fields or constructor arguments, per-request
allocations, fallback construction, new interfaces, or cleanup methods.
`Workspaces.Open`, canonicalization and lock acquisition remain at their existing
call sites; the moved constructor only creates the empty coordination map.

The baseline characterization was committed at
`1dcbacbb72c3ffda13938e21397e75e6e5bb7479` before production edits. It uses real
root, second-root, spawned-child, restored-child and tool-host services. Writes
through canonical and symlink aliases succeed before locking, wait until their
bounded contexts expire while the shared target is held, and succeed after
release. Target content stays unchanged while blocked, and unrelated-path writes
succeed concurrently with the held lock. Three race/shuffle runs passed on the
original implementation (17.193s). In a disposable snapshot, replacing only the
restored child's coordinator with a fresh instance caused the restored-child
lock assertion to fail, as required. After injection, the test also asserts
identity with the externally supplied coordinator and passed three race/shuffle
runs with the architecture guard (18.391s).

The AST migration accounts for all 177 existing storage-open calls across 57
files, including aliased imports and integration-tagged CLI/TUI/daemon fixtures.
Each storage lifetime receives one coordinator; existing fixture assertions and
cleanup remain unchanged. The source-conservation proof at
`/private/tmp/whip-workspace-conservation-proof.txt` preserves 578 existing
function declarations after normalizing only the added argument and the two
production constructor changes. The only other existing-test differences are
the new characterization's supplied-pointer checks and the architecture guard's
additional forbidden storage constructor. Every authority method is byte-identical;
entire delegation/admission, tools, dispatcher, workspace, runtime binding,
child cloning and skill-completion files are byte-identical. The frozen
content-only migration literal is untouched.

Module build, vet and whipvet passed. CI's pinned golangci-lint 2.13.1 reported
zero issues, and daemon/TUI/CLI integration-tagged fixtures compiled. Full
race/shuffle suites passed: session (159.052s), capability (29.182s), tools
(18.324s), MCP (22.606s), daemon (274.455s), agent (10.175s), and CLI (80.987s).

Fresh compatibility evidence is at
`/private/tmp/whip-workspace-compat-fresh/evidence.json`, using the fixed SDK
`271c0f8d2a35648d1b45056d57432590b783483c`, immediate parent
`42285977df445ac79334ac96e465d44f057f0350`, Go 1.27.0 darwin/arm64 and Node
24.14.1. The recorded candidate is the characterization commit plus its captured
staged source diff; later documentation updates do not change the tested source.
Fresh binaries passed Unix/WebSocket responses and ordered events, deliberate
response/order mutation rejection, cancellation/reconnect lifecycle and
fixed/immediate-base rollback for both engines with retained root/child state,
content, schema and trace/export. Candidate integration binary SHA-256:
`390f550a80c4017e6306710625cb1ad14b617a9637f6808bdfd46c6b2758a4b8`.

Corrected inherited CI provenance is recorded at
`/private/tmp/whip-corrected-ci-audit.wDmPXD/provenance.json`. For immediate
parent PR #311, the downloaded Linux compatibility artifact identifies parent
`774623f12ef182856862209d186bc70150d17cf1` and the synthetic merge candidate for
published head `42285977df445ac79334ac96e465d44f057f0350`. Its
[CI run](https://github.com/context-labs/whip/actions/runs/36689454272) has passed
lint and Linux/macOS compatibility; its
[security run](https://github.com/context-labs/whip/actions/runs/36689453922)
passed. At this pre-publication snapshot, its SDK job had subsequently failed
and classification was pending; the full workflow was not claimed green. The
following entry records publication and the later diagnosis. These corrected
artifacts supersede the earlier parent-provenance runs for this head; they do
not claim completion of other inherited workflows.

### Separate gateway fixture synchronization repair

Workspace ownership was published as [PR #312](https://github.com/context-labs/whip/pull/312)
at `26a6fec9b505fd6c00cd10d64c753c779509a2a4`, based on PR #311. Its
[CI](https://github.com/context-labs/whip/actions/runs/36692034286) and
[security](https://github.com/context-labs/whip/actions/runs/36692033890) workflows
started; full hosted acceptance remains pending. This separate test-only repair
starts from that exact head and changes no product or client implementation.

PR #308's original [CI run, attempt 1](https://github.com/context-labs/whip/actions/runs/36689144507/attempts/1)
failed in `TestHandshakeFailClosedBeforePipelinedTraffic/different-runtime`:
the second browser write returned `broken pipe` before the assertions ran.
The gateway and its tests were unchanged from frozen development. The fake
backend replied immediately to initialization while the browser sent its two
frames separately, so correct handshake rejection could close the connection
before the second write. A diagnostic probe against unchanged PR #311 observed
the expected error response, upstream closure without forwarding, and browser
EOF before the second write in all three rejection modes. The Linux write error
was not reproduced locally on macOS, where that first post-close write was
accepted by the kernel. Diagnosis and external probes are preserved under
`/private/tmp/whip-corrected-ci-audit.wDmPXD/`.

The fixture now holds the incompatible initialization response until both
browser writes have succeeded. Its per-subtest channel also observes test-context
cancellation so failed setup cannot strand the backend worker. Every existing
network-marker, protocol-error, and no-forwarded-request assertion remains
byte-identical, as does the rest of the test file after removing the eight added
lines. No sleeps, retries, ignored write errors, or product changes were added.
The preservation proof is `/private/tmp/whip-reorg-gateway-fixture-preservation.txt`.

Local full gateway race/shuffle passed (1.499s), and the affected test passed
100 race/shuffle repetitions with the original CI shuffle seed (1.755s). Module
build, vet, whipvet, and full CI-pinned golangci-lint 2.13.1 passed with zero lint
issues. The separate pre-change diagnostic barrier probe
also passed 100 repetitions with an added scheduling delay; that delay exists
only in the external probe, not in this repair.

One failed-job rerun of PR #308 was authorized after source attribution and
diagnosis. Its open head was verified unchanged at
`4b5c0e5790f698371abed1ed4c8bcf3dd9cd3885` immediately before the request;
attempt 2 is pending. The original failure remains recorded above. Corrected
PRs #306, #309, and #310 now have fully successful CI; all five corrected
#306/#308/#309/#310/#311 security workflows and both compatibility matrix jobs
passed, with the expected Linux parent/tree provenance verified.

PR #311's original SDK job passed all 470 SDK and 1,380 app tests, then timed out
in the browser performance probe while recording committed markers. Its artifact
contained all 40 markers in the DOM but only 39 measurements, with no page errors.
A controlled snapshot/subscription handoff reproduced the same measurement gap
on both PR #311 and its unchanged immediate parent: the probe observed event
notifications but omitted a marker delivered in a snapshot. The source-level
diagnosis is `/private/tmp/whip-pr311-performance-diagnosis.md`. A separate
instrumentation repair is planned; it is not included here. One failed-job rerun
was authorized and requested on unchanged PR #311 head
`42285977df445ac79334ac96e465d44f057f0350`; attempt 2 remains pending. Neither
workflow is claimed fully green before its checks finish.


### Separate browser performance instrumentation repair

The original PR #311 SDK failure remains recorded at [run
36689454272](https://github.com/context-labs/whip/actions/runs/36689454272), job
109802928468. The unchanged 30-second wait required 40 commit measurements;
its artifact recorded 39 unique observations, missing only `commit-probe-001`,
while all 40 markers appeared in the page. No browser errors were reported.
The controlled handoff reproduced this exact signature on PR #311 and its
immediate parent `774623f12ef182856862209d186bc70150d17cf1`: the marker arrived
in a snapshot while the SDK replaced its subscription. The prior probe only
observed event notifications. This is a preexisting measurement gap, not a
change to process ownership or SDK delivery. The original CI artifact lacks
raw frames, so the controlled trace establishes the matching causal mechanism
rather than claiming to recover that run's exact transport trace.

The repair is based on gateway prerequisite PR #313,
`067fe82cd90b99aed42efa80c24e3917ba96e820`. The existing self-contained browser
initializer moves into `apps/web/scripts/performance-probes.mjs`; the performance
script passes that function directly to Playwright. Root-matching snapshot
presentation entries retain their actual decimal-string event sequences, not
the snapshot cursor. Bounded commit deduplication keeps the first WebSocket or
desktop-frame receipt across either route, and the existing MutationObserver
still records mounted live-row observation. Ordinary delta latency samples
remain notification-only. No product, SDK, protocol, dependency, timeout,
40-marker assertion, clock check, or existing capacity limit changes.

The focused Node test imports the same installer and exercises its serialized
function with controlled receipt, DOM, and clock inputs. The exact original
initializer fails the snapshot and deduplication regressions; the repaired
installer passes all nine cases, including subtests. Coverage includes both
host bridges, 40 mixed-route markers with sequences beyond JavaScript's safe
integer range, repeated receipts before and after DOM observation, unrelated
roots and child presentation, snapshot delta exclusion, and overflow. CI runs
this regression beside the existing full performance command; every previous
workflow step remains unchanged.

On this parent, protocol/browser generation and production asset packaging,
JavaScript syntax checks, workflow lint, and all 36 workflow/publication policy
tests passed. The complete ordinary browser performance run passed all 40
marker and keyboard checks with a 0.384ms clock-correlation error bound. The
controlled handoff also passed all checks with a 0.357ms bound: first marker
sequence `910` arrived once in a snapshot and zero times in event notifications,
yet its actual receipt and DOM observation were recorded. Both runs retain the
original timeout and all performance checks. These local runs used Node
24.14.1, Go 1.27.0 darwin/arm64, and pinned Playwright 1.63.0; hosted CI remains
separate evidence.

Exact conservation evidence is
`/private/tmp/whip-performance-probes-conservation.json`: after excluding only
the initializer import/extraction, the main script is byte-identical; original
globals, transport bridge, and DOM observer are exact, and the workflow gains
only the focused test command. Browser artifacts are under
`/private/tmp/whip-performance-probes-validation/{ordinary,controlled}/`.
The original-installer negative control and focused/workflow logs are
`/private/tmp/whip-performance-probes-{original-negative,focused,workflow-tests}.log`.
The causal investigation is preserved in
`/private/tmp/whip-pr311-performance-diagnosis.md`. No additional hosted rerun
was requested by this repair; the previously authorized PR #311 failed-job
rerun is independent of these results. This PR remains unmerged for review.

### PR 7 — Shared native connection plumbing

This step starts from browser instrumentation prerequisite [PR #314](https://github.com/context-labs/whip/pull/314),
`0081b66e5445b90d10e4d5ada7786335943a6edc`. Its
[CI](https://github.com/context-labs/whip/actions/runs/36693408346) and
[security](https://github.com/context-labs/whip/actions/runs/36693408075) runs
started; this local evidence does not claim that those full hosted runs passed.
All active PRs remain unmerged.

`internal/daemonconn` now owns the existing runtime path calculation, validated
local dialing, native launch/exec primitives, shared limits and event envelope.
Daemon still owns runtime/startup/maintenance locks, inherited descriptor
validation, readiness retry policy and lifecycle decisions. The single mutable
`ErrDaemonOwned` binding is used directly at all eight call sites. Launch and
restart still share one executable lookup seam; CLI callbacks keep their
existing owners. `Paths` retains its filesystem effects, `ResolvePaths` remains
read-only, and the dial/initialize deadline asymmetry is preserved.

The exact native `CreateSession` value moves to session with a daemon type alias,
including `DefinitionRevision`, field order and JSON tags. The existing command
presentation decoder moves once to `internal/commandpresentation`; native client
implementation, outcome encoding and `fillCommandPresentation` remain in daemon
for now. These shared homes allow the following client package move without
making clients depend on server implementation or adding translation/forwarding
functions. Existing exported daemon chunk/buffer constants are immutable aliases
of the shared constants. Protocol, framing, provider policy, SQL and schema
sources are unchanged.

Characterization commit `1ed6409596e002b4043c9bb06b0eef42a27608b0` preceded every
production edit. Its named table covers all 21 recognized operations, exact
failure/status/JSON quirks, unknown-operation raw bytes, empty model `@`, and
explicit failure precedence. Those tests plus existing launch/path/maintenance
checks passed three race repetitions on unchanged production (8.452s). A
throwaway Go overlay removing `@` failed the exact empty-model assertion; the
mutation never touched repository source. After extraction the same three-run
checks passed: daemonconn 1.481s, commandpresentation 1.795s, daemon 8.314s.

Four existing primitive tests move with their implementation. Mixed ownership
and socket integration tests remain in daemon, including the fixed 100-byte
boundary assertion. Existing Taskfile/CI selectors and assertions remain; one
focused daemonconn/commandpresentation command is added to acceptance and the
existing Linux/macOS runtime matrix. The frozen SDK bridge remains in daemon
with its original integration/Unix tags. A narrow architecture check prohibits
the shared helper packages from importing daemon. The current architecture map
and desktop fixture's source-location comment follow the new ownership.

The type-aware extraction records 156 resolved identifier edits and verifies
original bytes before replacing them. The resulting proof covers 684 declarations
across 48 files: 459 function bodies, including 105 named tests, match the exact
expected reference/extraction changes. A separate comparison preserves all 29
selected moved/retained declaration bodies, including owner locks, autostart
policy and encoders. Imports and comments are outside token comparisons. The
only descriptive correction changes the decoder's misleading inherited
plain-text prohibition to an accurate stored-outcome description; its raw-byte
fallback remains unchanged. Evidence is
`/private/tmp/whip-pr7-{transform-journal.json,caller-conservation.log,declaration-conservation.log}`.

Local module build, vet, whipvet, integration-tag compilation and pinned
CI-equivalent golangci-lint 2.13.1 passed with zero lint findings. The complete
TUI race/shuffle suite passed in a real PTY (18.844s), and all 36 workflow policy
tests passed. Full affected-package race/shuffle passed: daemonconn 1.773s,
commandpresentation 1.419s, daemon 271.359s, session 156.806s, ACP 2.033s and
CLI 80.477s. Logs are `/private/tmp/whip-pr7-full-race.log`,
`/private/tmp/whip-pr7-tui.log`, and the matching build/vet/whipvet/lint/tagged
compile/workflow logs under `/private/tmp/whip-pr7-*`.

Fresh fixed-SDK and immediate-parent compatibility passed, including immutable
sources and freshly generated artifacts, both transports' calibrated response/
error/ordered-event comparisons, deliberate response/order mutation rejection,
lifecycle behavior and both-engine fixed-base/immediate-base → candidate → base
rollback with root/child state, content, schema and trace/export checks.
Evidence: `/private/tmp/whip-pr7-compat-fresh/evidence.json`, with Go 1.27.0
(darwin/arm64), Node 24.14.1 and the original fixed SDK revision
`271c0f8d2a35648d1b45056d57432590b783483c`. The candidate binary SHA-256 is
`3ba071e27e6e0e082871d5c0e0a828549bf02889d0f98fa779f4449962b6bf19`.
This pre-commit local run records the characterization SHA plus its complete
staged extraction diff. Only the reviewed decoder comment and this ledger were
updated afterward; no executable behavior changed. Hosted checks will record
the final commit and actual PR parent separately.

### Tagged TUI title fixture repair before native client extraction

The existing `TestInteractiveSessionOverTrustedProtocol` failed on both Unix
and WebSocket on clean PR #315, with correct model/provider and two transcript
messages but the prompt-derived title instead of `Worker Investigation`.
The same fixture and policy were present at the fixed development revision
`271c0f8d2a35648d1b45056d57432590b783483c`: the fake's obsolete
`GenerateTitle(context.Context)` signature did not implement the current
prompt-taking optional interface, its 19-character input was below the existing
20-rune generation threshold, and it treated the first, provisional title event
as a generated title. This is baseline fixture drift, not a client extraction
regression. Evidence: `/private/tmp/whip-tui-title-fixture-diagnosis.md` and
`/private/tmp/whip-pr8-parent-native-title.log`.

The test now supplies an explicit longer prompt and a matching fake that records
the exact generation input. A cancellable test-only channel holds generation
until the client receives the provisional title; the test then requires the
generated title event and the original final model/provider/title/two-message
snapshot assertion. The original 10-second context remains. There are no
sleeps, timeout increases, production changes or altered title-policy defaults.
The existing tagged test command is added to the Linux/macOS runtime matrix;
Taskfile acceptance already runs it, and every prior selector remains.

Both transports passed all three race repetitions without a PTY (3.634s),
matching the hosted command's environment. The full TUI race/shuffle suite,
including integration-tag tests, passed in a real PTY (10.164s). Existing
`TestTitleLifecycle*`, `TestGenerateTitle*` and `TestProvisionalTitle*` policy
checks passed three race repetitions (daemon 13.417s, session 5.268s).
Four throwaway Go overlays independently restored the old signature or short
prompt, changed the captured input, or returned the wrong generated title;
each failed its intended assertion. The repository was never mutated for
these negative controls. Results are
`/private/tmp/whip-title-fixture-negative-results.json`.

Module build, vet, whipvet, all-package integration-tag compilation, full
CI-equivalent default-tag golangci-lint 2.13.1, and all 36 workflow-policy tests
passed. Evidence logs use
`/private/tmp/whip-title-fixture-{focused,tui-full,policy,build-vet,lint}.log`.
An additional integration-tag-only TUI lint run found the inherited `noctx`
finding on this fixture's unchanged `net.Listen("unix", paths.Socket)` call.
It did not pass. That call remains unchanged to keep this repair focused;
there is no suppression. Exact output and parent-source comparison are
`/private/tmp/whip-title-fixture-tagged-lint.log` and
`/private/tmp/whip-title-fixture-listener-proof.txt`. This supplemental result
is distinct from the passing required lint gate and tagged compile/tests.
Local runners used disposable homes and removed ambient provider credentials;
no installed daemon or live provider was used. Product code, SDK/protocol,
schema/migrations and dependency files are unchanged from PR #315. Hosted
compatibility and workflow results will record this separate prerequisite's
published head and immediate parent; local checks do not stand in for those
future results.


### PR 8 — Native client implementation ownership

The eight existing client implementation files and eleven client-only declarations
from mixed daemon files now live in `internal/client`. All 53 exported Client
methods, 43 RootClient methods, five constructors/autostart entry points, state
values, interface methods and option/response fields are preserved. Wire and
storage aliases resolve to the same canonical protocol/session declarations;
framing calls use the existing frozen `protocoltransport` implementation directly.
No runtime forwarding client implementation remains in daemon.

The client borrows the neutral connection primitives introduced in PR 7. Its
command IDs, event ordering, reconnect/snapshot behavior, cancellation targets,
provider mutation no-replay policy, buffers and timeouts are unchanged. Server
handlers and `client_control.go` remain daemon-owned. CLI composes the server
where needed; TUI and ACP consume the client and existing protocol types without
importing daemon implementation. A new production-import guard enforces these
four directions, and the existing provider-call guard also scans client while
retaining its exact AgentSession exception.

All 23 selected existing private tests and PR 7's fill characterization move with
their implementation. The real expired-cursor snapshot integration test, scoped
service/provider tests and SDK fixture remain in daemon. A test-only frame writer
retains the exact marshal/write sequence. Three fake RPC failures become exact
canonical error literals, preserving their codes, messages and ErrorData.Kind.
The existing runtime/Taskfile shared-package command gains client, and the named
reconnect acceptance command gains its new package without changing its regex.

Fresh typed analysis at the reviewed PR 7 head records 234 declaration groups,
24 selected tests and exact source hashes. The transform uses AST spans and 794
resolved identifier-use locations. The API comparison expands aliases and
normalizes only approved declaration ownership: 123 public entries plus the
private callResponse shape match. Source conservation covers 1,305 declarations
across 109 files, including 1,012 function bodies and 344 named tests, after the
recorded extraction and reference edits. It rejects unaccounted added declarations
and changed/untracked Go files; external extra-declaration and extra-file
mutations both fail. The new architecture guard is compared with its exact
reviewed body. Imports and comments are outside that token comparison; the
baseline snapshot retains original full source, comment spans and hashes.
Evidence lives under `/private/tmp/whip-pr8-*`, including the typed baseline,
transform journal, API/declaration conservation logs and proof mutation log.
An external test overlay also compared all three replacement error frames
byte-for-byte with the parent server constructors, including ErrorData.Kind;
all matched (`/private/tmp/whip-pr8-rpc-fixture-bytes.log`).

On the original PR 7 parent, full race/shuffle passed for client (1.472s), daemon
(273.502s), daemonconn (2.508s), commandpresentation (1.950s), session (154.258s),
ACP (3.612s) and CLI (71.013s). Full TUI race/shuffle passed in a real PTY
(16.150s). Both named reconnect/session and CLI/TUI/ACP Taskfile acceptance
commands passed with their existing assertions. Module build, vet, whipvet,
integration-tag fixture compilation and pinned golangci-lint 2.13.1 passed; lint
reported zero issues. All 36 workflow policy tests passed (56.580s).

The additional tagged `TestInteractiveSessionOverTrustedProtocol` gate failed on
both transports: the snapshot title was the prompt-derived `Investigate workers`,
while the fixture expected `Worker Investigation`. The same unchanged test failed
on clean PR 7 in a real PTY. Its fake implements GenerateTitle(context.Context),
but the existing runtime uses GenerateTitle(context.Context, string); its
19-character prompt also falls below the existing 20-rune generation threshold.
The provisional title event precedes generated-title completion. These inherited
fixture issues required a separate test-only prerequisite, without changing title
policy, increasing timeouts or weakening the final title assertion. The original
and parent failures are retained in `/private/tmp/whip-pr8-native-acceptance.log`
and `/private/tmp/whip-pr8-parent-native-title.log`; PR 8 does not repair them.

The initial fresh fixed-SDK/immediate-parent guard passed at
`/private/tmp/whip-pr8-compat-fresh/evidence.json`, with candidate HEAD and immediate
parent both recorded as PR 7's commit and the complete staged extraction diff.
It compared freshly generated artifacts, both transports' response/error/ordered
event transcripts, mutation rejection, lifecycle behavior and both-engine
root/child/content/schema/trace rollback. Toolchains were Go 1.27.0 darwin/arm64
and Node 24.14.1; fixed SDK/backend stays
`271c0f8d2a35648d1b45056d57432590b783483c`. Candidate test binary SHA-256 was
`8cfe4b022e942473a7f7a1b45c7f2b687a271346586375ed5ce80785f3d4ae6e`.
Publication was held at that checkpoint for the separate fixture repair. The
initial failure and initial-parent evidence above remain historical results;
the repaired-parent validation below supersedes that publication hold.


After independent review of the separate test-only prerequisite [PR #316](https://github.com/context-labs/whip/pull/316),
the extraction was rebased onto its exact head
`e289b4ea3344b6fdd45a8782d17c5c66084e90e4`. Clean code commit
`ecf96616acf48233658620c81286a08fd9bada80` retains both additive CI commands and
all repaired fixture assertions. The rebase changes only that inherited fixture,
CI and the ordered ledger relative to local checkpoint
`c87ecb8e6021480e0a1c83d874d39a982c23fe90`. All 435 production Go files, plus 11
tracked skill examples, are byte-identical; the prior full affected race results
remain applicable without repeating unchanged suites. Exact tree evidence is
`/private/tmp/whip-pr8-rebase-production-proof.json`.

Fresh typed reference analysis and expected-source regeneration use the repaired
parent, rather than the earlier snapshot. All 124 API entries still match;
source conservation still covers 1,305 declarations across 109 files, including
1,012 function bodies and 344 named tests. All 534 authored daemon test names
remain exactly once across daemon/client, with precisely the approved 24 moved
tests and one additional architecture test. All 1,257 original Go comments are
preserved verbatim; splitting the autostart test intentionally copies its existing
Unix build constraint into both files. Both extra-declaration/file negative
controls still fail, and all three RPC error frames match the repaired parent's
server constructors byte-for-byte. Refreshed artifacts use the
`/private/tmp/whip-pr8-rebased-` prefix, with the test-name proof at
`whip-pr8-rebased-test-conservation.log`.

On the rebased code, repaired tagged TUI startup/title acceptance passed both
transports in a real PTY (7.226s). Module build, vet, whipvet, integration-tag
fixture compilation and pinned golangci-lint passed with zero lint issues. All
36 workflow policy tests passed (52.097s), retaining the title fixture command
alongside the client package's existing runtime command.

The final fresh compatibility run passed at
`/private/tmp/whip-pr8-rebased-compat-fresh/evidence.json`, recording clean candidate
`ecf96616acf48233658620c81286a08fd9bada80`, exact immediate parent
`e289b4ea3344b6fdd45a8782d17c5c66084e90e4`, and an empty candidate diff. Both
transport comparisons, response/order mutation rejection, lifecycle checks and
both-engine fixed/immediate-parent persisted root/child/content/schema/trace
rollback passed. The fixed SDK/backend pin, toolchains and candidate daemon test
binary hash are unchanged from the initial evidence above. The final follow-up
commit changes only documentation; hosted checks will record its own SHA and PR
base. The extraction and its prerequisite remain separate, unmerged review PRs.
