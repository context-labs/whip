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

### Native desktop, gateway and model inspection integration — 2026-09-28

The coherent integration at `397cb9785` passes complete `task check:phase`, including formatting/build/vet, all native Go race/shuffle suites, contracts, SDK/examples, public CLI, actual compiled fixtures and retained regressions. Store race passed313.939s, runtime372.290s, RPC72.644s, native CLI34.962s. Separate `task check:analysis` passes with zero findings against unchanged `e3fed9c91918d9c36766dd47d878c1b5466238d1` and no reported vulnerabilities. Logs: `/tmp/whip-desktop-native-phase-repaired.log` and `/tmp/whip-desktop-native-analysis-repaired.log`.

This checkpoint integrates native daemon/status/log/updater routing, desktop local/SSH attachment and startup fixtures, bounded renderer framing, native public web/gateway owner lifetime, explicit bundled computer-helper selection, Browser preview/control ownership, scoped content metadata, pending-permission filtering and captured model/prompt/notices/compaction inspection (schema52). The renderer and mobile cutover remains a separate in-progress increment; signed matching-artifact startup/continuity and Linux packaged renderer validation are still outstanding. Nothing was installed, restarted in a user runtime, merged into a product branch or deployed.

Earlier combined runs remain recorded as failures: the first SDK trace-state fixture used a1ns clock delta that rounded at epoch-sized floating point; `c2594080f` uses a deterministic small clock. The later runtime restart fixture assumed its scripted input was always the final message, ignoring canonical interrupted-child completion mail; `3ffd94dcc` forces and verifies that ordering while retaining ownership/restart checks. The CLI gate exposed a real socket/context deadline publication race; `397cb9785` recognizes the caller's already elapsed deadline before `Context.Err` publication and cancels only the exact accepted input. An earlier unrelated transport timeout remains uncertain and gains no cancellation authority. The final full gate includes all repairs.

Previously pending hosted checkpoints are now verified successful: draft259 head`9cfa29e63a48c16587a6498ff02f85e0b29e1468`, run36527605325; draft260 head`a51afc4f8770bebb1ffa07b5677d96b316188cdf`, run36527639551; draft261 head`50bcd028631a96d421a4baf38f87fa77dc7c718c`, run36526837824. The new desktop checkpoint's hosted validation is pending. Phases5–7 remain incomplete; the active source-scope exclusions and retired core have not been removed.

### Native welcome and creation handover checkpoint

Shared welcome/setup now uses the actual native Client and exact immutable definitions, host defaults, provider readiness/catalogs, bounded host skills, directories and global MCP declaration import. Session creation uses the existing tab UUID as creation identity and the common durable journal; the accepted root is verified before staged content and the authored draft move. Lost-ACK recovery can finish handover without sending a first message. A persisted unresolved creation blocks another payload after reload. Full Settings restoration of that saved creation into its draft remains an explicit follow-up; current receipt inspection does not silently send or manufacture a replacement root.

The five focused welcome/defaults/skills/new-chat/runtime suites pass 93 tests (4.78s), including actual codec validation, multiple scoped uploads, post-creation failures, changed-draft recovery, provider confirmation, exact revision selection and unknown host defaults. Owned production TypeScript files are clean; unrelated remaining legacy app callers still prevent the aggregate app typecheck. Conversation, tabs, terminal, mobile and remaining parity work continue; Phase 7 deletion is not claimed.

### Native terminal ownership checkpoint

Terminal rendering now uses native bounded reads and exact process epochs, preserves large decimal cursors, verifies owner/page byte ranges, and cancels obsolete observations. Input bounds and discard rules prevent a paste or lost connection from accumulating or replaying keystrokes. Epochless retained tabs remain visible as ended until an explicit restart. Native session-link navigation uses attached host evidence. The five focused terminal output/renderer/tab/routing/navigation suites pass160 tests (5.70s). Terminal-close integration in the tab strip is staged with the wider conversation-pane adoption; this checkpoint alone does not finish the aggregate UI port. A lost ephemeral terminal-open acknowledgement still requires a user-visible inspection path using the existing native list API before a replacement shell is suggested.

### Native selected-pane checkpoint

Visible panes now lease and render their exact selected root or child session through the native Client, SessionView and ExecutionView. Trace observers remain separate per-pane leases and reject obsolete client ownership. Native terminal close preserves uncertain same-epoch outcomes for inspection; confirmed missing or previous-process terminals close locally. The three focused pane/title/desktop-close suites pass 39 tests (4.00s), including child selection and exact owner requests. Conversation rendering is being integrated from its independently tested leaf; aggregate app typechecking remains pending that cutover.

### Native navigation and shell checkpoint

Shared sidebar, search, attention, desktop notifications, first-run catalog detection and bootstrap readiness now use native catalog/session metadata and canonical host attachment state. Exact child attention links, revision-bound keyset pages, bounded cursor storage, replacement-client isolation, visible overflow and large decimal counts are covered with the actual SDK codec and catalog view. Ten focused suites pass 48 tests (4.30s); owned source TypeScript is clean. Legacy conversation callers still prevent the aggregate typecheck until the released conversation leaf is integrated. No installed runtime or user session was touched.

### Full shared-renderer integration audit

After integrating native prompt inspection and conversation leaf9185083, the entire shared app typecheck passes and app/web source has no legacy SDK/protocol imports. The first full renderer run passed1188/1271 tests in97/108 suites. The83 failures remain recorded; most fixtures still used retired snapshot/event shapes. Porting the retained opening test exposed a real integration regression: pending native metadata showed paused activity, and a failed metadata read showed new-session body copy. The app now preserves neutral loading and explicit unavailable states. Six affected startup/status/error/conversation suites pass38 tests(4.20s). Input/body/streaming, creation and catalog invalidation fixtures remain in progress; the full renderer gate is not yet green.

### Native navigation fixture completion — 2026-09-28

The retained title invalidation and eleven sidebar-creation scenarios now use actual native SDK validation, catalog revisions, immutable agent revisions and durable creation/input commands. They preserve stale read cancellation, per-client listener retirement, host isolation, independent drafts, explicit model confirmation, secret exclusion, background focus and late creation navigation. Unsupported old hosts fail initialization before catalog reads. Missing provider metadata now displays a retryable error while retaining the first-message draft. A candidate provider-confirmation production change was discarded after correcting the fixture to the actual nested readiness selection shape.

All five affected suites (welcome, sidebar creation, title invalidation, provider connections/defaults) pass63 tests4.84s; shared app type checking passes. The earlier full renderer gate remains failed until the independently assigned input/content fixture migrations are integrated and the whole suite reruns.

### Saved creation handover after app reload — 2026-09-28

General Settings can explicitly restore an accepted native tree creation into its original draft tab after a new app instance loads. Fresh receipt evidence and the SDK's saved acceptance are required; identity-only evidence cannot restore. The original immutable request is retained for an explicit retry when acknowledgement was lost. Restoration does not submit the first message, navigate, or reopen a closed tab, and conflicts/storage failures retain the draft and recovery record. Initial creation and restoration share the same validated handover.

Four affected suites pass69 tests4.86s, including new-instance/closed-tab restore, exact retry, deleted or mismatched host evidence, destination conflicts, storage failure and the actual Settings control. Shared app type checking passes.

### Native desktop bootstrap and first-chat warming — 2026-09-28

The second full renderer run at012096e47 passed1,258 tests and failed25 in two suites. Twenty-four were the retained desktop startup fixture still using the old protocol; the remaining image-upload test released its deferred gate before the asynchronous upload entered. These failures are preserved as failures. The fixture now runs the production native framed transport, identity checks, bootstrap, routing and first-launch probe. It retains all startup visibility/notice/font/timeout, closed-tab, opt-in and global-skill checks. Upload synchronization now waits for the real content.put request.

The native startup fixture exposed missing first-chat metadata warming. AppRuntime now preloads bounded provider presets/catalogs, host permission/execution defaults and MCP import status with the existing provider read; it retains only host metadata for five minutes and stops warming a retired client. The four cold/zero-tab reopen scenarios still hold subsequent host reads and require the ready layout. All three affected suites pass62 tests22.44s; shared app types pass. The full renderer gate is being rerun.
