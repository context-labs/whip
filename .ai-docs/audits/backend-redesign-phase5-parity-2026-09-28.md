# Phase 5 retained-capability audit

Working audit at `b81fc5ff9` (integrated executor, shell, MCP); this is evidence for the maintained redesign plan, not a second architecture specification. Existing tested checkpoints are retained. No item below is implicitly retired.

## Backend obligations still requiring completion or an explicit decision

1. **Direct human host actions and model-free MCP tool hosting.** `internal/daemon/client_control.go:560–623` handles `tool.call` and `shell.run` as durable accepted actions, rejects conflicting busy controls, and owns execution beyond observer disconnect. `internal/daemon/client_control_test.go:271–358` verifies responsiveness, exact retry, and ordinary prompts queuing behind a shell action. `internal/daemon/actor_control_test.go:192–284` distinguishes pre-admission cancellation, observer cancellation, and actual action cancellation. `internal/daemon/client_control_coverage_test.go:37–88` verifies failed worker admission settles the receipt without leaked busy state. `internal/daemon/client_control_test.go:77–100`, `internal/daemon/tool_runner.go:14–43`, and `cmd/whip/mcp.go:160–217` exercise a real model-free tool host serving schemas and calls. `internal/tools/tools.go:518–527,775–790` defines a fixed builtin catalog; this is not authorization to expose arbitrary coordination operations. Preserve permission enforcement, including the headless denial in `internal/daemon/client_control_test.go:1267–1277`. Approved replacement: typed `host_operation` input using the normal receipt, queue, actual turn, captured configuration, grants, hooks, operations, settlement, and recovery. No synthetic prompt, provider attempt, interpreter cell, or conversation history. Reserve schema 42 (schema 41 is the separately integrated guest-utilities checkpoint). This agent owns this increment.

2. **Guest artifacts.put/inspect and permissions.request/status.** `internal/hostmodule/modules.go` advertises these names, but `internal/runtime/completions.go:217–220` only accepts artifacts.read and the current coordination dispatcher has no permissions implementation. Retained `internal/daemon/recursive_runtime.go:1836–1852` stores bounded immutable text with optional source, while inspect/read delegate to scoped context operations; `internal/daemon/recursive_runtime_host_behavior_test.go:26` exercises artifacts. Retained `recursive_runtime.go:852–859` makes permissions.request informational (invoke the exact operation to create a permission request), while status inspects an existing request. These must not grant model-origin authority or create a second permission ledger. Parent owns this increment.

3. **Trace reads/export and supported Trace UI.** `internal/daemon/server.go:516–523` exposes trace.page/export. `internal/daemon/trace_rpc_test.go:19–65,146,242` covers durable live paging, cursors/server clock, unfinished spans, owner-scoped OTLP content, reads without actor construction, and reads after stop. `cmd/whip/sessions_export.go:72`, `packages/legacy-sdk/src/trace.ts`, and `docs/features.md:800` show supported CLI/SDK/Trace-view use. New durable turn/attempt/cell/operation evidence exists, but no active equivalent public trace projection/export yet. A projection must not fabricate causal parents or timestamps absent from durable evidence. Backend coverage is required before Phase 5 closes; supported UI adoption follows in Phase 6.

4. **Host services before a root is selected.** `internal/daemon/host.go:24–67` implements host.skills.complete, host.directories.list, host.directory.pick, host.attention, host.themes.list/resolve. `packages/legacy-sdk/src/services.ts:10–23` and app directory/theme/notification consumers retain them. Existing new completions.list/read are completion reports, not directory completion. Attention should derive from canonical pending questions/permissions/mail; host reads must use the isolated new namespace and bounded safe filesystem access. Backend coverage is required before Phase 5 closes.

5. **Workspace directory change.** `internal/tui/client.go:2064` routes /cd to workspace.set; `packages/app/src/components/session-controls.tsx` and retained details controls expose directory changes. The new domain currently describes a fixed working directory (`docs/backend-domain.md:702`). No explicit retirement decision was found. Preserve this obligation until a transactional revisioned directory-change path (with resource/captured-cwd invalidation) or an explicit product decision exists. Workspace snapshot/restore and workspace.inspect are already implemented and do not supply /cd parity.

6. **Headless run configuration and loop controls.** `cmd/whip/run.go:42,46,170` retains max-turns (0 means uncapped), caller-selected stable cache-key, and run.configure. `internal/daemon/client_control.go:230–243` applies system override, max turns, headless policy, and cache key. New runner loop is fixed at 32 rounds and cache identity is session based. Captured instruction configuration covers system text, but not the other exact controls. Preserve headless fail-closed behavior rather than leaving a noninteractive run awaiting approval. Port or explicitly decide/document the changed constraints before CLI adoption.

## Phase 6 client obligations, not duplicate backend work

7. **Authored-agent SDK helpers.** `packages/legacy-sdk/src/agents.ts` provides tool()/defineAgent(), Standard Schema conversion/validation, timeout bounds, typed AgentSession and Agents.serve(), required/optional hooks, progress serialization, and connection-bound handler lifetimes. New `packages/sdk/src/executors.ts` provides the thin connect/bind/pending/result/hookResult/progress/events/close client. The high-level authoring helpers, examples, and typed sessions remain Phase 6 work. Reconnect behavior must respect new executor generations and must not silently replay uncertain handlers.

8. **All supported clients.** The Phase 6 ledger in `docs/backend-redesign-plan.md` still requires CLI/TUI, React/app/web and playground, docs/examples, and SDK services/views to adopt the new protocol and source of truth. The new gateway/browser transports and human terminal path are separate assigned checkpoints. Browser/computer/native-helper support is assigned independently. Retained tests must be replaced by equivalent domain-oriented coverage before Phase 7 removes their old implementations.

## Completed families deliberately not reopened

Imported history and compaction, fork, workspace snapshot/restore, automatic titles, account/provider authority, definition/module bindings and executor core, modes and delegated grants, root recovery/catalog, questions, files.list/search/LSP, schedules/goals/mail/state, shell run/input/jobs, and MCP trust/discovery/root ownership have existing tested checkpoints. Integration or documentation lag is not evidence that they need recreation. Broad test gates, canonical records, and draft PRs remain parent-owned.

## Client-cutover reconciliation (2026-09-28, native foundations `22b7e5b0d`)

The original audit above remains historical evidence. Direct human host actions,
artifacts/permission helpers, native trace reads/export, bounded host views,
workspace directory changes, captured run controls, authored-agent SDK helpers,
execution defaults and native browser/computer authority have since landed in the
stack through draft #259. Their canonical behavior and exact validation are in
`docs/backend-domain.md` and `docs/backend-redesign-development.md`; they are not
new implementation assignments. Native coding and junior-developer immutable
builtins are now integrated with actual retained instructions and distinct module
policies. The existing assistant definition stays unchanged.

Phase 5 and Phase 6 remain in progress. Phase 7 has not begun deletion. Explicit
remaining obligations discovered by actual client adoption include:

- Atomic browser delegation to a spawned child, including public receipt linkage
  and crash/ACK-loss behavior. The isolated guest checkpoint `60968f48a` is tested
  but not yet part of this integration head; public linkage is in progress.
- Live REPL stdout through bounded ephemeral native observation. Final committed
  cell output alone does not satisfy the live-output workflow.
- Whole-tree usage with reported/estimated/unknown cost, call counts and missing
  token metadata. A bounded recent-turn window cannot stand in for whole-tree
  totals. Derive these from the existing attempt ledger.
- Actual shared conversation/composer/sidebar/search/action workflows, bounded
  native input paging, requests/scheduled wakes, and their root/child ownership.
  The isolated app branch has tested recovery, content, execution leases, browser
  design, queue and inspector increments; the whole app is not yet migrated.
- Retained file mentions/completion; native session skill and host skill reads
  exist, but they do not replace workspace filename discovery.
- Captured host-settings reload (`session.reload`), tool `deny_permissions`
  editing, and explicit Rod/ChromeDP driver selection. No silent retirement is
  approved. Native direct `tool.schemas` already supplies the public builtin and
  custom-call catalog; inspectors should reuse it rather than duplicate it.
- CLI commands, TUI, ACP, desktop startup/distribution and mobile lifecycle
  adoption, complete supported-target gates, fresh-install/restart/packaging,
  and final retired-core deletion. Native Go client and launcher foundations do
  not establish those product-client milestones by themselves.

All these items remain required work. No installed runtime, existing user data,
real account, production branch or deployed artifact was changed for validation.
### Hosted scheduling repairs (2026-09-28)

PR #259 at cbd39d5dc (run36526022483) passed the partitioned runtime suites but failed the macOS terminal slow-reader fixture. PR #260 at b9e9cfa19 (run36526214610) additionally observed an empty MCP catalog during tools/list_changed refresh. These runs are failures, not final gate evidence.

The terminal fixture now waits until its queue is actually full before detaching and emits enough bytes to fill that queue plus two chunks. Failure diagnostics retain only a bounded4KiB tail. The existing15-second deadline remains. Twenty focused shuffled race repetitions passed20.013s. The MCP fixture waits for the refreshed root catalog and exact delegated child subset, checking that extra tools never appear on every poll; forty focused shuffled races passed17.144s. Both changes repair synchronization in tests without changing production behavior or skipping assertions. Hosted reruns remain pending.
