# Dynamic Workflows

Branch: `dynamic-workflows` (worktree `.worktrees/dynamic-workflows`)

## What this does

Ports the pi `better-workflows` extension (https://github.com/anishthite/better-workflows,
itself faithful to Claude Code's `Workflow` tool) into whip as a first-class,
built-in **`workflow` tool**: the model writes a small deterministic JavaScript
orchestration script that fans work out to subagents (`agent()`), pipelines it
(`pipeline()`), barriers on it (`parallel()`), groups progress into `phase()`s,
and returns a structured result. Runs are **background by default**: the tool
returns a run ID immediately and the finished result is delivered back into the
conversation as a steered message — the same fan-in primitive background
subagents already use.

"Dynamic" = the orchestration graph is decided by the script at runtime (loops,
conditionals, data-dependent fan-out), not a static DAG declared up front.

## Goal

- One new tool `workflow` (inline `script` | `scriptPath`, plus `args`,
  `resumeFromRunId`) registered on the parent agent in `agent.New`.
- Script runtime: goja (pure-Go ES5.1+ JS engine) sandbox with the
  better-workflows globals: `agent`, `pipeline`, `parallel`, `phase`, `log`,
  `args`, `budget`, `cwd`, `console.log`. Determinism enforced:
  `Date.now`/`Math.random`/argless `new Date()` throw (blocklist at parse time
  + neutered inside the VM).
- Subagent execution reuses whip's existing machinery: `newSub` route
  resolution (`SubModel`, per-call model/effort overrides), `Turn`, session
  usage roll-up via `OnUsage: parent.AddUsage`.
- Resume: every run persists its script + journal under
  `~/.whip/workflows/{scripts,runs}/`; `resumeFromRunId` replays the longest
  unchanged prefix of `agent()` calls (djb2 hash of prompt+opts) and runs the
  rest live.
- Caps: `min(16, NumCPU-2)` concurrent agents (channel-semaphore limiter),
  1000 agents/run, 4096 items per pipeline/parallel call.
- Nested `workflow(scriptPath, args)` one level deep, sharing caps.
- Failed agents resolve to `null` (scripts `.filter(Boolean)`); failures are
  not cached on the journal so resume retries them.
- Observability: each workflow's `agent()` calls appear as rows in the parent's
  task registry (dock/`/tasks`), grouped under a workflow run row; completion
  fans back via `Agent.Steer`.

## Non-goals

- No TUI panel or `/workflows` command in this change (the task dock already
  renders the rows). Can follow.
- No named/saved-workflow registry (`name`-only invocation); scripts persist
  per-run and re-invoke via `scriptPath`.
- No session-store (SQLite) persistence of runs — journals are JSON files;
  runs do not survive process exit mid-flight (status stays "running" in the
  file, resume still works).
- No `agentType`, no token `budget.total` enforcement (the field exists in the
  sandbox with `total: null`, matching better-workflows).
- No worktree `isolation` option in v1 (subagent worktree isolation exists for
  the background `subagent` tool; adding it to workflow agents is a follow-up).
- Not a security sandbox — a determinism/footgun guard only.

## Design

New package `internal/workflow` (pure logic at the core, I/O at the edges),
plus one wiring file in `internal/agent`.

| File | Port of | Contents |
|---|---|---|
| `internal/workflow/parse.go` | `src/parse.ts` | `Parse(script) (Meta, body, error)`: blocklist check, `export const meta =` prefix, brace-match the literal (strings/comments aware), decode via goja into a fresh VM (pure-literal rule), validate name/description/phases/effort. |
| `internal/workflow/persistence.go` | `src/persistence.ts` | `Home()` → `~/.whip/workflows` via `config.Dir()`; `GenerateRunID`; `PersistScript`; `JournalEntry{Index,Hash,Result}`; `PersistedRun` JSON save/load; `HashString` (djb2 base36 — must produce IDENTICAL hashes to the TS so journals are cross-compatible). Best-effort writes (never kill a run). |
| `internal/workflow/runtime.go` | `src/runtime.ts` + `src/limiter.ts` | `Run(ctx, script, Options) (Result, error)`: goja VM with globals; `agent()` Go func — cap checks, callIndex/callHash, resume replay, limiter (buffered-channel semaphore), runs `Options.RunAgent`; errors → `null`; `pipeline`/`parallel` spawn goroutines per item with `sync.WaitGroup` + buffered results; `phase`, `log`, `args`, `budget`, `cwd`, `console.log`; nested `workflow()` (depth 1). Per-item recover → null (a panicking stage drops the item, never kills the run). |
| `internal/workflow/manager.go` | `src/manager.ts` | `Manager`: `Start(script, args, opts) → runID/scriptPath`, goroutine runs `Run`, persists journal incrementally, `Events{OnPhase,OnLog,OnAgentStart,OnAgentEnd,OnSettle}` callbacks, `Stop(runID)` (ctx cancel), `Snapshot(runID)`/`List()` for the TUI later. |
| `internal/agent/workflowtool.go` | `src/tool.ts` + `src/agent.ts` | `workflowTool(parent)`: resolves script (inline, fences stripped | scriptPath), `Manager.Start`, returns runId text. `RunAgent` impl: `newSub` + `Turn` (usage → parent), per-call model/effort override via `resolveSub`, schema via a `structured_output` tool appended to the sub's toolset (+ one repair re-prompt). Registers each workflow in the task registry for the dock; completion → `Steer`. |

### goja: new dependency

`github.com/dop251/goja` — pure Go, no cgo, the standard JS engine for Go
(`golang.org/x/tools`-ecosystem projects use it; k6 runs on it). No stdlib
alternative exists. `goja_nodejs` is NOT imported (no require/process — the
sandbox is a guard, keep it minimal).

### Script semantics notes

- Scripts are async: body wrapped in `(async () => { ... })()`; goja supports
  promises natively. `await` of a Go func result: we return immediate values
  for cache hits and goja `Promise`s for real agent work
  (`vm.NewPromise()` + resolve from goroutine).
- Determinism: TS neuters via a prelude string; we do it in Go
  (`vm.Set("Math", ...)` equivalent — actually: run the same prelude JS inside
  the VM, it's realm-local and cheap).
- Resume hash input: `JSON.stringify({prompt, model, effort, phase, schema,
  isolation})` — replicate key order exactly for cross-compat (Go: build the
  string manually or marshal a struct with fields in that order).

### Channels over locks

- Limiter: `chan struct{}` cap N (acquire = send, release = receive) — one
  channel is the whole limiter.
- Fan-out results: buffered `chan result` sized to the batch, laid back in
  order (same shape as `agent.runTools`).
- Run completion → manager → `OnSettle` → TUI/task-registry + `Agent.Steer`
  (existing fan-in path, no new plumbing).

## Prior art

- Reference: `/tmp/better-workflows` (clone of the GitHub repo) — cited per
  file above; semantics mirrored deliberately (null-on-failure, no-failure-
  caching, longest-prefix resume, caps 16/1000/4096).
- whip side: `internal/agent/subagent.go` (`newSub`, `resolveSub`, `capReport`),
  `internal/agent/background.go` (task registry, settle→Steer fan-in),
  `internal/agent/worktree.go` (isolation provisioning, v2).

## Test plan

- `parse_test.go`: meta extraction, pure-literal rejection, blocklist, phase
  validation, brace matching through strings/comments.
- `persistence_test.go` (WHIP_HOME=temp): script+run round-trip, djb2 vectors
  cross-checked against the TS values, journal map.
- `runtime_test.go`: fake `RunAgent` (no LLM): pipeline no-barrier ordering,
  parallel null-on-throw, per-item panic → null, agent cap, determinism throws,
  resume replay (prefix hit, mid miss, failure not cached), nested workflow
  depth limit, usage/budget accounting. All under `-race`.
- `workflowtool_test.go` in `internal/agent`: end-to-end against the existing
  httptest streaming fake provider (like `agent_test.go`): tool call starts a
  run, agent() calls hit the fake, completion steers back into the parent.
- `go test -race ./...` before done.

## Docs plan

- `docs/features.md`: new "Dynamic workflows" section (behavior → code →
  tests), named tests listed like existing entries.
- Roadmap: not listed there; no checkbox to tick.
- `docs/concurrency.md`: add only if the limiter/fan-out teaches a pattern not
  already there (likely: no — same shapes as runTools).

## Tasks

- [x] Recon reference + whip architecture
- [x] This plan
- [x] parse.go + tests
- [x] persistence.go + tests
- [x] runtime.go + tests
- [x] manager.go + tests
- [x] workflowtool.go wiring (+ structured_output) + loop test
- [x] `task check` + `-race` green, `task tidy`
- [x] features.md section
- [x] adversarial pass (panics, ctrl+c, parallel calls, resume) — found+fixed: non-deferred recover, callJS leak on cancel, effort substring check, UTF-16 hash units, runPaths hack, cap shadow, dropped fan-out logs
- [ ] commit + push `dynamic-workflows`
