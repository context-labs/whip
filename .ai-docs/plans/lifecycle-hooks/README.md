# Lifecycle hooks: project policy and automation

Branch: `main` (working tree)

## What this does / Goal

Add trusted command hooks at five lifecycle boundaries: `UserPromptSubmit`,
`PreToolUse`, `PostToolUse`, `PostToolUseFailure`, and `Stop`.

Whip loads project-owned `.whip/hooks.json` files and plugin hook files under
`.agents/plugins/*/hooks/hooks.json`. This gives projects a portable way to
enforce policy, add context, audit tool use, and run quality gates without
adding a runtime or dependency.

Example: block a dangerous tool call before execution, then require tests to
pass before the agent can stop.

## Non-goals

- Embedded JavaScript, Python, WASM, or a general plugin ABI.
- Mutating tool schemas, model requests, or tool arguments in v1.
- Async, session, file-change, shell, or installer hooks.
- Replacing Whip's permission system.
- Persisting hook state in SQLite or introducing another config dialect.

## Design

Primary surfaces:

- `internal/hooks/{types,config,runner}.go`: discovery, normalization, matching,
  execution, and decisions.
- `internal/tools/bashrun/bashrun.go`: bounded subprocess I/O and exact exits.
- `internal/agent/{agent,subagent,background}.go`: lifecycle integration.
- `internal/tui/{tui,registry,shell}.go`: trust, `/hooks`, and `/cd` reloads.
- `cmd/whip/{run,acp}.go`: headless and ACP setup.

### Hook package and protocol

- Discover user plugins, project plugins, then the project hook file; preserve
  deterministic file and declaration order.
- Normalize both formats into one immutable, read-safe `hooks.Manager`.
- Send versioned JSON on stdin and expose stable `WHIP_*` environment values.
- Match selectors against native Whip tool names; invalid entries emit visible
  warnings while valid entries still load.
- Accept allow/block/deny decisions from JSON stdout and exit code `2`; other
  non-zero exits fail open unless a `PreToolUse` hook sets
  `on_failure: block`.
- `Stop` failures always fail open, and stop rejection is capped at three
  retries to prevent loops.
- Apply output, time, command-count, and environment-size limits. Skip async
  tools rather than reporting misleading post-execution results.

### Safe subprocess execution

Extend `bashrun.Options` with stdin, environment, working directory, separate
stdout/stderr capture, and byte limits. Extend `bashrun.Result` with stderr and
the exact exit code. Reuse its process-group cancellation and `KillAll`
behavior; add no dependency and keep existing callers compatible.

### Agent loop

The agent consumes a narrow hook-runner interface so tests can use fakes and
the loop does not depend on configuration details.

- `UserPromptSubmit` may append context or reject the turn before the model.
- `PreToolUse` may block before tool execution.
- `PostToolUse` and `PostToolUseFailure` may append context after completion.
- `Stop` may reject completion and return feedback to the model.
- Emit hook lifecycle events for observability; store durable decisions as
  ordinary conversation messages, requiring no database migration.

### Concurrency, frontends, and trust

- Run hooks serially within one event; independent tool calls remain parallel.
- Run pre-hooks before path/global mutation locks and never hold those locks
  across subprocess I/O.
- Copy/swap the active manager safely so `/cd` stays busy-safe and race-free.
- Subagents inherit the parent runner while each worktree event carries its
  explicit working directory.
- Interactive project hooks load only after `checkTrust`; `/cd` and ACP load
  them only when `config.Trusted(dir)` is true. User hooks are already trusted,
  and `whip run` retains its existing trusted-directory contract.
- `WHIP_DISABLE_HOOKS=1` disables all hooks for recovery and CI isolation.

## Test plan

- Loader tests for both formats, precedence, ordering, selectors, and partial
  validation failures.
- Runner tests for JSON/env input, decision parsing, exit semantics, limits,
  timeout, cancellation, and process cleanup.
- Compatibility tests for existing `bashrun` callers.
- Agent-loop tests with a fake provider and hook runner for all five events.
- Concurrency tests for parallel tools, same-path mutations, `/cd`, manager
  replacement, subagents, and worktree directories.
- Frontend tests for trust gating, `/hooks`, headless JSON output, and ACP.
- Run `go test -race ./...` and `task check`.

## Docs plan

- Document configuration, payloads, trust, limits, and troubleshooting in
  `docs/lifecycle-hooks.md`.
- Update `docs/README.md`, `docs/features.md`, `docs/roadmap.md`, and
  `docs/concurrency.md`.
- Record protocol and safety decisions in the lifecycle-hook documentation.

## Tasks

- [x] Add normalized hook types, loaders, matching, validation, and tests.
- [x] Extend `bashrun` with bounded structured I/O and exact exit reporting.
- [x] Implement the hook runner, decisions, limits, and cancellation.
- [x] Integrate the five events into the agent loop and event stream.
- [x] Wire subagents, worktrees, safe manager replacement, and explicit cwd.
- [x] Add TUI, headless, and ACP trust/loading behavior plus `/hooks`.
- [x] Write user and concurrency documentation.
- [x] Run race, integration, and adversarial subprocess tests, then `task check`.

## Completion notes

- The adversarial pass made manager + working-directory publication one atomic
  `SetHookScope` operation and synchronized process-wide child-marker snapshots.
- Hook-annotated and hook-blocked tool results re-enter the normal bounded tool
  output path; the original failure status survives annotation and truncation.
- Interactive bash timeout, cancellation, and inactivity markers route through
  `PostToolUseFailure`, matching non-interactive command failures.
