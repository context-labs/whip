# Native core retirement

This increment removes the retired execution implementation after the CLI,
terminal, SDK and supported applications moved to protocol v4. The old suite
remains in Git history, including checkpoint `c9db05507`; it is not copied into
an archive or excluded from package discovery. This document records behavior
replacements, not equivalent line coverage or completion of outstanding platform
and performance acceptance.

The initial deletion covers 825 files in eleven Go roots (`agent`, `agentdef`,
`daemon`, `inferencenet`, `legacy`, `llm`, `memory`, `protocoltransport`, `rlm`,
`tools`, `webgateway`), `cmd/legacy-contract`, both legacy JavaScript packages and
the old process fixture. It includes 325 test files. The later cleanup also
removes the unused capability dispatcher, permission-rule cache, browser broker
DTOs (except the shared native error type) and ambient skill/MCP compatibility helpers. The native filesystem,
process/PTY, MCP normalization and browser driver leaves remain.

## Behavior and test disposition

The accepted redesign replaces orchestration and persistence ownership. Old
actor names, mutable transcript copies, table layouts, unscoped permission rules,
wire aliases and predecessor migrations are intentionally removed. Their useful
failure cases move to native public boundaries:

| Retired family | Retained guarantee and native evidence |
| --- | --- |
| Daemon admission, queue and lost acknowledgement | `store/input_identity_test.go`, `receipt_match_test.go`, `creation_test.go`; the actual `scripts/redesign/v4-fixture.test.mjs` process retains accepted work and exact receipts across disconnect, SIGKILL and restart. |
| Active cells and scratch checkpoints | `store/cells_test.go`, `runtime/engine_test.go`, `engine/process` contracts; committed evidence survives, uncertain effects are not replayed, both engines use the same authority-free worker. |
| Model attempts, billing and storage failure | `store/attempts_test.go`, `budgets_test.go`, `runtime/provider_test.go`, `output_test.go`; atomic accounting/settlement retry never repeats provider dispatch. |
| Root/child execution, capacity and delegation | `store/delegation_test.go`, `resources_test.go`, `budgets_test.go`, `runtime/recursion_test.go`, `capacity_test.go`; one execution path, captured grants, ancestor limits and reusable worker capacity. |
| Mail and explicit state | `store/mail_test.go`, `mail_evidence_test.go`, `state*_test.go`, `runtime/mail_evidence_test.go`; immutable revisions, exact recipient ownership and retained evidence after sender deletion. |
| Context, compaction and imported history | `runner/compaction*_test.go`, `context_pressure_test.go`, `context_recovery_test.go`, `store/compaction_test.go`; immutable raw history, exact whole-exchange coverage, captured helper routes and recorded accounting. `runner/compaction_content_test.go` and `runtime/compaction_content_test.go` cover bounded folding before image hydration, unchanged raw history and indivisible-input refusal. |
| Fork, rewind and workspace effects | `store/fork*_test.go`, `rewind_test.go`, `runtime/fork_test.go`, `rewind_test.go`, `workspace_test.go`; provenance, receipts/tombstones and separate explicitly authorized workspace effects. |
| Steering and direct human operations | `store/steering_test.go`, `runtime/steering_test.go`, `host_operation_test.go`; original input identity, captured target turn, safe boundary, model-free human actions and distinct observer cancellation. |
| Provider adapters and continuation | `model/openai_*`, `sampling_test.go`, `reasoning_test.go`, `subscription_test.go`; exact supported wire behavior, malformed-call refusal, usage evidence and private continuation scoped to its attempt. Automatic partial-stream regeneration is intentionally retired. |
| Instructions, skills and local notes | `instruction/skills_test.go`, `roots_test.go`, `project_test.go`, `runtime/skill_read_test.go`, native CLI skills tests and `clientnotes`; explicit named roots, separate metadata/publication/grants, bounded bodies and exact local-note ownership. |
| Definitions, custom tools and hooks | `store/bindings_test.go`, `runtime/executors_test.go`, `hooks_test.go`; immutable declarations, captured provenance, child narrowing, required/optional failure and no replay after uncertain connection loss. |
| Goals, schedules and titles | `store/goals_test.go`, `goal_*`, `schedules_test.go`, `title_test.go` and corresponding runtime/RPC/SDK tests; ordinary durable inputs, bounded continuations, captured helper results and CAS. The old `GOAL_MET` text heuristic is retired. |
| Permission policy and grants | `store/operations_test.go`, `grant_scope_test.go`, `permission_mode_test.go`, denial tests; SQL-owned authority, exact operation identity, revocation and child scope replace the old dispatcher/cache. |
| Files, LSP, shell and human terminals | `tool` workspace/list/search suites, `runtime/lsp_test.go`, `shell_test.go` and `terminal`; captured/revalidated paths, bounded outputs and joined lifetime. Human terminals retain separate ownership. |
| MCP discovery/import/calls | Native `Manager.ResolveTool`/`CallChecked`, `runtime/mcp*_test.go`, `rpc/mcp_test.go`, real disposable self-host fixtures; exact generations, import trust, delegated catalogs, bounded typed results and joined processes. |
| Browser/computer/native helper | `runtime/browser_test.go`, `external_browser_test.go`, transfer/driver tests, `computer_test.go`, `browserhost` and Desktop bridge tests; human resources remain distinct from agent authority. Native external Chrome configuration, root generations, dispatch checks and bounded uploads are integrated; explicit controls now cover shared Web/Desktop, mobile, CLI and TUI. The unused computer helper lifecycle is removed with [replacement evidence](backend-native-computer-retirement.md); ambient browser managers, discovery/launch fallback and retry wrappers are also removed with [native driver/lifetime replacement evidence](browser-computer-use.md#native-browser-test-ownership). Actual headed/extension/platform acceptance remains distinct. |
| Trace and OTLP | `store/trace_test.go`, `runtime/trace_test.go`, `trace`; exact counters, causal IDs, body ownership, bounded transport and restart/deletion evidence. |
| SDK transport, views and recovery | Current `packages/sdk/test` suites and native gateway/executor/shell/computer/browser process fixtures; bounded observation, exact admission recovery and no SDK execution authority. |
| Client lifecycle and distribution | [CLI disposition](backend-native-cli-disposition.md), [terminal disposition](native-terminal-retirement.md), native web/Desktop/mobile/ACP fixtures, `hostcmd` and `localruntime`; actual disposable binary startup, verified identities and no installed-runtime mutation. |
| Old configuration, SQL and wire compatibility | Fresh-start scope intentionally rejects predecessor namespaces/versions. Current config/store rejection tests and compiled lifecycle fixtures replace old migrations and compatibility translators. |

Four particularly important old regressions are explicit normal-gate invariants:
`TestReadingQueuedHistoryDoesNotExecuteAndRestartResumes`,
`TestObserverCancellationAndExplicitCancellationHaveDifferentLifetimes`,
`TestChildAdmissionSurvivesRestartWithoutLoadedWorker` and
`TestStructuredOutputSQLSettlementRetryDoesNotReplayProvider`. Store receipt,
atomic-attempt and concurrent-admission tests supplement them. Capacity-yield and
slow-provider cancellation tests retain the old guarantee that waiting for an
execution owner must not lock durable authority/accounting.

## Context cost semantics

The old agent really invoked age decay, including large `rlm_exec` results and
older images. Its duplicate/superseded `read`/`write`/`edit` branches inspected old
model-facing tools; the retained production surface exposed `rlm_exec` instead.
The native system uses immutable recorded compaction, exact pins and raw tails,
with captured pressure thresholds and hard request bounds. It does not promise
the old unconditional 24k-token hot window or identical provider token costs.
Unknown/low-pressure windows retain their selected evidence until bounded,
manual or reactive compaction. This is the accepted context ownership change,
not permission to fail before foldable history can be compacted; the audited
multi-image hydration failure is repaired in `c0999eef9`. Native regressions cover
multiple valid images exceeding the aggregate request bound, incremental folding,
accounting/SQL rollback and refusal of an indivisible current input without replay.

## Shared leaf disposition

The old `capability.Authority`/`Ledger`/`Dispatcher`, permission-rule cache and
`browser.DesktopProvider` broker had no native consumers. `ErrStaleAdmission`
remains with native workspace revalidation, and `DesktopError` stays with the
native Desktop backends. `capability.Workspaces`, process/PTY
helpers, MCP call/result leaves, Desktop native backends and browser drivers stay.
The unused computer wrapper is removed in `f567bb709`; browser wrapper retirement
in `0265ab26c` removes ambient ownership and automatic batch retry, retaining
explicit native/desktop transports and joined process ownership.

Skill import still uses explicit `ForeignDirs`/`Scan` and the pure bounded
`ParsePromptMetadata`. The unused ambient `DefaultDirs`/`DirsFor`, duplicate
`LoadPromptCatalog` reader and old `PromptBlock` XML renderer are removed with
their implementation tests. The canonical `instruction.Catalog`/`Load` suites
cover source ordering, escaped metadata, disabled winners, scoped reads, byte and
entry limits; the repository skill-metadata ratchet remains. Import/default-root
selection/body authorization are tested through the shipped CLI and native SDK.

MCP retains foreign-format parsers and explicit `NativeConfigs` plus filtered
import policy. The old default-path constructor and unrestricted merge wrapper
are removed. A rewritten native provenance regression first demonstrated that
filtered discovery replaced an explicit host file with the retired config path;
its fix preserves the supplied source. The optional real-server smoke now uses
its explicitly selected native host file and is not run without opt-in.

## Normal product gate

`ci.yml` owns the required implementation; the redesign workflow calls it.
Every package comes from `go list ./...`. Complementary partitions cover heavy
store/runtime/TUI/CLI suites without a rewrite allowlist. The required aggregate
fails on skipped, cancelled or failed dependencies. The pre-commit fast check is
branch-neutral. The pinned linter stays at v2.13.1 against frozen baseline
`e3fed9c91918d9c36766dd47d878c1b5466238d1`.

Normal gates include protocol generation drift, SDK and process fixtures,
Chromium/Firefox product checks, packed UI consumers, docs/Storybook, mobile
backend/exports, Desktop package/SSH/IPC, both worker engines and four supported
Go distribution targets. Coverage profiles are diagnostic: the accepted plan
removed the temporary rewrite-wide 90% floor. Actual Safari, signed platform and
physical-device/live-provider evidence retain their distinct requirements.

The [current gate audit](backend-native-gate-audit.md#current-acceptance-snapshot)
maps exact CLI/TUI/UI/Desktop passes and remaining acceptance. The chronological
development record preserves earlier checkpoint results and failures.
Passing selected leaves or compilation alone does not complete Phases 5–7.
