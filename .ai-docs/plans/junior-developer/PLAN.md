# JuniorDeveloper: the second agent definition

Status: implemented September 10, 2026 on `feature/agent-definition`, one
commit per step. See the implementation record at the end.

Written against `feature/agent-definition` at `72ddc0b3f`, which landed the
[canonical agent definition](../agent-definition/PLAN.md). Line numbers are
from that tree.

## Request

Add a second agent definition, JuniorDeveloper, with limited access to host
functions. Its purpose is to prove the definition abstraction: a second value
must be selectable, persistable, restorable, and actually constraining, without
touching the coding agent. The profile below is a recommendation the user asked
for; the enforcement gaps it exposes are the real deliverable.

## What JuniorDeveloper can do

| Field | Value | Why |
| --- | --- | --- |
| ID | `junior-developer` | Kebab case like `coding`; the CLI and protocol use it verbatim |
| Persona | "You are a junior developer working under review." | Distinct voice proves the persona is definition-owned |
| Rules | Small explained changes; run tests after every change; never rewrite history, force-push, or delete files it did not create; no new dependencies or build/CI changes; ask with `user.ask` when unsure | A junior's operating rules differ from a senior's and mention only modules it has |
| Project files | `CLAUDE.md`, `AGENTS.md` | Needs repository rules |
| Skill discovery | off | Exercises the least-used toggle; skills are power-user playbooks |
| Standing instructions | on | User rules still apply |
| Modules | `context`, `files`, `shell`, `state`, `artifacts`, `permissions`, `user` | Read, edit, run tests, keep notes, ask. No `browser`, `computer`, `models`, `agents`, `messages`, `mcp`, `schedules` |
| Capabilities | `read`, `write`, `shell` | Can edit and run tests; no browser, computer, or MCP authority |
| Model, compaction | host defaults | No model can be assumed present in a user's configuration |
| Surface | auto title on, goal loop off | A junior does not run autonomous goal loops |
| Children | none | It cannot spawn; it has no `agents` module |

Seven of fourteen modules and three of six capabilities. It cannot delegate,
cannot reach MCP, and cannot drive a browser or the desktop.

## Why this matters

The first definition proved composition. A second one exposes three things
the coding-only pass could not:

1. **Selection and persistence do not exist.** Nothing lets a client, the CLI,
   or the database say which definition a session runs. `DefinitionFor`
   (`internal/daemon/definition.go`) maps a session kind to `Coding()`, and the
   `sessions` table has no definition column. A second definition needs a
   durable identity so restart and fork rebuild the right agent.
2. **Module selection is advisory.** The prompt catalog derives from
   `Definition.Modules`, but the kernel installs every module
   (`internal/rlm/worker.go:123`) and `recursiveHost.Call`
   (`internal/daemon/recursive_runtime.go:742`) dispatches any module the model
   names. A JuniorDeveloper that calls `mcp.call` would succeed.
3. **Root capabilities are advisory.** `Bind` copies `Definition.Capabilities`
   into the node's name list, which only governs child narrowing. Root grants
   come from `ensureAuthority` (`internal/session/capability.go:279`), which
   always issues every file, shell, and MCP operation. A JuniorDeveloper root
   is authorized for `browser_exec` and `mcp.call` regardless of its definition.

Without 2 and 3 the abstraction is a prompt change with a different name. With
them, a definition's module and capability lists are enforced at the same
host boundary as everything else.

## Target state

- `agentdef` holds a registry: `Lookup(id)` returns `Coding()` or
  `JuniorDeveloper()`. `Operations(capabilities)` maps capability names to the
  file, shell, and MCP operations they grant, replacing the switch in
  `capabilityDelegations`.
- A session row carries `definition`, defaulting to `coding`. Creation accepts
  a definition id, validates it against the registry, and persists it. Fork
  copies it. `DefinitionFor` takes the session metadata and fails clearly on an
  unknown id.
- `whip run -agent junior-developer` and `whip --agent junior-developer`
  create such a session; resuming with a different `-agent` fails, as with
  `-rlm-engine`. The SDK passes `definition` through `sessions.create` once the
  contract is regenerated. No picker UI.
- The kernel installs only the definition's modules and the host rejects a
  call to any other module. Root grants at first bootstrap cover only the
  operations the definition's capabilities map to. Existing roots keep their
  grants; the store already refuses to reissue authority to an existing root.
- `goal.set` and `goal.run` fail for a definition whose goal loop is off.

## Decisions taken here

1. **Identity only, no revision.** The column stores the id. A revision has no
   consumer until definitions can change at an idle boundary.
2. **No immutability trigger.** The engine column has one because checkpoints
   depend on the engine. Nothing depends on the definition column yet, and a
   later "switch definition when idle" feature would need to update it.
3. **Root grants narrow only at first bootstrap.** `ensureAuthority` already
   skips grant creation for an existing root, so a coding root created before
   this change keeps its full grants and a JuniorDeveloper root created after
   it never had more. A definition that widens later does not widen old roots.
4. **Module enforcement lives in both the kernel and the host.** The kernel
   environment omits modules the definition did not select, so the bindings
   the model can see match the catalog it was given and there is nothing to
   discover and try. The host still rejects an unselected module before
   dispatch, because the worker is a disposable environment and not an
   authority boundary. Decided September 10 after the user asked that
   unavailable modules not appear in the agent's environment at all.
5. **Protocol minor bump to 6.1.** Two additive fields: `definition` on session
   creation and on session metadata.
6. **Goal commands are rejected, not silently stored,** when the surface flag
   is off. Storing a goal that never continues would be a trap.

## Implementation steps

Each step leaves `go test ./...`, `go vet ./...`, `task contract`, and the
analyzer green. Steps 1 and 2 are independent of each other.

### Step 1. Registry, operations mapping, and the JuniorDeveloper value

- `agentdef.JuniorDeveloper()` with the profile above.
- `agentdef.Lookup(id) (Definition, bool)` over a fixed slice of constructors;
  `agentdef.IDs()` for error messages and CLI help.
- `agentdef.Operations(capabilities []string) (files, shell []string, mcp bool)`
  containing the mapping currently in `capabilityDelegations`
  (`internal/daemon/recursive_runtime.go:1252`). `read` grants `read`; `write`
  grants `read`, `write`, `edit`, `workspace.write`; `shell` grants `bash`,
  `shell_start`, `workspace_process`; `browser` grants `browser_exec`;
  `computer` grants `computer_exec`; `mcp` grants `mcp.call`.
- `capabilityDelegations` calls `Operations`.
- Generalize the golden test to every registered definition:
  `TestDefinitionPromptGolden/<id>/<engine>/<case>`. Move the coding files with
  `git mv`; a diff of the moved files must be empty. JuniorDeveloper goldens are
  new files: root guide with and without a handle, identity, composed root.

Tests: registry lookup; JuniorDeveloper validates and is a strict subset of
coding in modules and capabilities; `Operations` output for each name and for
the empty list; the moved coding goldens are byte-identical; new goldens.

### Step 2. Persist the definition id

- Schema 15 to 16: `ALTER TABLE sessions ADD COLUMN definition TEXT NOT NULL
  DEFAULT 'coding'`, following `upgradeV14` in
  `internal/session/execution_engine_migration.go`. Add the column to
  `cleanSchema`, every `SELECT` that feeds `scanMetas`
  (`internal/session/session.go:250`, `:419`, `:981`), the `Meta` struct with
  `json:"definition"`, and the fork `INSERT ... SELECT`
  (`internal/session/session.go:922`).
- `CreateSessionForCommandWithEngine` gains a `definition` argument and the
  existing variants keep passing `"coding"`. The store validates only that the
  value is non-empty; the daemon validates it against the registry before
  calling the store.
- `EnsureAuthority(ctx, rootID, grants RootGrants)` where `RootGrants` carries
  the file and shell operation lists and the MCP flag. Callers compute it from
  `agentdef.Operations`. Existing tests call the old form; keep a thin
  `EnsureAuthority(ctx, rootID)` that passes full grants so fixtures stay
  untouched.

Tests: upgrade from a version 15 database keeps existing rows at `coding` and
rejects reopening with an older binary; a new session stores its definition;
fork copies it; `Load` and the catalog scan return it; a root created with
narrowed grants is denied `browser_exec` and `mcp.call` by the ledger while a
root with full grants is not.

### Step 3. Select and enforce in the daemon

- `CreateSession` and `CreateSessionParams` gain `Definition string`
  (`internal/daemon/control.go:78`, `internal/protocol/runtime_contract.go:12`).
  `sessionDefaults` resolves an empty id to `coding` and rejects an unknown id
  with `agentdef.IDs()` in the message.
- `DefinitionFor(meta session.Meta)` replaces the kind-based lookup: tool hosts
  have none; agents resolve `meta.Definition`, empty meaning `coding`; unknown
  ids fail so a downgraded binary refuses the session instead of running it as
  the wrong agent.
- `Daemon.open` computes root grants from the definition before calling
  `EnsureAuthority`; `installReplacement` keeps the same definition.
- `recursiveHost.Call` returns `module %q is not available to this agent` for
  a module outside `node.definition.Modules` before dispatching.
- `rlm.KernelOptions.Modules` carries the definition's list to the kernel,
  which passes it to every worker it spawns or replaces through a `-modules`
  flag next to the existing limit flags (`internal/rlm/worker.go:60`). The
  Starlark worker's `installModules` installs only those modules; the QuickJS
  worker builds its allowlist from them instead of `Modules()`
  (`internal/rlm/quickjs_worker.go:25`). Nil means every module, so existing
  kernel tests are unchanged. Modules are not part of a checkpoint, so restore
  and eviction keep the filtered set without migration.
- `goal.set` and `goal.run` fail with `this agent does not run goals` when
  `Surface.GoalLoop` is false.
- Bump `protocol.Minor` to 1, regenerate `packages/protocol`, and run
  `task contract`. The SDK's `sessions.create` already spreads extra
  parameters, so `definition` passes through with no SDK code change.

Tests: a Starlark kernel limited to `context` fails `browser.run(...)` with an
undefined-name error and a QuickJS kernel with a `ReferenceError`, while
`context.inspect()` still works after a worker replacement; creating with
`junior-developer` yields a root whose node capabilities are `read write
shell`, whose prompt lacks the browser, MCP, and messaging lines, and whose
`mcp.list_servers` call through the host boundary fails with the module error;
`tool.call` of `browser_exec` on that root is denied by the ledger; an unknown
definition is rejected at creation; reopening the daemon restores the same
definition; `goal.run` fails for JuniorDeveloper and succeeds for coding; a
coding session still resolves every module and capability exactly as before.

### Step 4. CLI selection

- `whip run -agent <id>` and `whip --agent <id>` (decided September 10),
  defaulting to `coding`, validated against `agentdef.IDs()`, threaded like
  `-rlm-engine` through `cmd/whip/run.go:144`, `cmd/whip/main.go:66`, and
  `tui.Run` (`internal/tui/client.go:76`). Resume with a different `-agent`
  fails with the session's actual definition in the message.
- ACP and the web application keep creating coding sessions.

Tests: `cmd/whip/run_test.go` mirrors the engine flag cases: create with the
flag, resume mismatch, unknown id.

### Step 5. Documentation

- `docs/features.md`: the definition registry, how a session selects one, and
  that modules and root capabilities are enforced.
- `docs/rlm-runtime.md`: the host rejects unselected modules; root grants
  follow the definition at bootstrap; schema 16 note in the migration section.
- `docs/tools.md`: the module table describes the coding agent; other
  definitions select a subset.
- `docs/protocol-v2.md`: 6.1 additive fields.
- `README.md` or the `whip run` usage line for `-agent`.

## Preserved, changed, not built

**Preserved**

- Every coding prompt golden byte, now under the per-definition path.
- Coding behavior: all modules, all capabilities, full root grants, goal loop.
- Existing sessions: the migration defaults them to `coding`; their grants are
  untouched.
- Protocol major 6; the SDK client code.

**Changed**

- Schema 16 adds `sessions.definition`.
- Protocol 6.1 adds `definition` to session creation and metadata.
- Kernels install only the definition's modules; the host refuses calls to
  any other module.
- New roots receive grants for their definition's capabilities only.
- Goal commands fail for definitions without the goal loop.
- `EnsureAuthority` takes explicit grants; the old signature remains for tests.
- One shared capability-to-operations mapping in `agentdef`.

**Not built**

- A definition picker in the TUI, web, or mobile clients, or definitions in
  the `initialize` result.
- Spawn-time selection of a named child definition.
- Changing a session's definition after creation.
- Definition revisions or stored bodies.
- A friendlier error for an unselected module inside the kernel. An undefined
  name reads like any other typo; the prompt catalog is the model's map.
- Per-definition model or compaction routes for JuniorDeveloper.

## Risks and edge cases

- **Legacy roots stay wide.** A coding root created before this change keeps
  full grants. That is the existing policy for lost authority and is the safe
  direction.
- **Module gate versus internal host use.** `focusInput` and prompt
  composition call Go directly, not through `Call`, so gating `Call` cannot
  starve the runtime's own needs. Mailbox digests still render for a root
  without the `messages` module; nothing can send it mail, so this is inert.
- **Worker flag drift.** The kernel and worker agree on flags through one
  command builder; the `-modules` flag follows the limit flags, and an
  unknown module name fails worker startup, which the kernel reports as it
  does other startup failures.
- **Local library modules stay.** `json`, `math`, and `time` are pure
  Starlark helpers, not host modules; they are always installed.
- **Grant rows must exist.** `ensureAuthority` reads a generation for each of
  the three grant ids after insertion. Narrowed grants insert rows with empty
  operation lists rather than skipping rows.
- **Contract regeneration.** Generated TypeScript and Ajv validators are
  checked in; `task contract` catches drift. The web client rejects unknown
  fields only through the daemon's validation, which the regenerated schema
  updates.
- **Golden move.** `git mv` keeps history; the step asserts the moved files
  are unchanged before adding new ones.

## Resolved items

1. Profile: keep read, write, and shell with the seven modules above (decided
   September 10). The enforcement tests use ad hoc definitions for the empty
   shell grant and the read-only cases.
2. The TUI `--agent` flag is in scope (decided September 10).
3. Unselected modules are absent from the kernel environment as well as
   rejected by the host (decided September 10; decision 4).

## Implementation record (September 10, 2026)

Commits on `feature/agent-definition`, in step order: registry and
JuniorDeveloper, persistence and explicit root grants, selection and
enforcement, CLI flags, documentation. `go test ./...`, `go vet ./...`, the
repository analyzer, `task contract`, and `task sdk` were green at every step.

Deviations from the plan as written:

- **The snapshot query needed the column too.** `RootSnapshot` builds its
  metadata from its own `SELECT` in `internal/session/event.go`, not from
  `scanMetas`, so the resume check first saw an empty definition. Fixed in the
  CLI step; the daemon test now asserts the snapshot's definition.
- **Legacy-schema fixtures drop the new column.** Two migration tests rewind a
  fresh database by dropping newer columns; they now drop `definition` as they
  already drop `execution_engine` and `permission_mode`.
- **`upgradeV14` early return.** Its "already current" check compared against
  the moving current version; it now returns early only for versions at or
  past 15 so it stays a pure 14-to-15 step.
- **Grant rows keep one shape.** A narrowed root still has all three grant
  rows; an empty operation list denies everything. `MCPSelectors` reports a
  root without `mcp.call` as denied rather than as an empty selector set,
  which no reachable path relies on.
- **Delegation operation order is canonical, not alphabetical.** Child grant
  operation lists follow the order root grants have always used. Authorization
  is membership-based, so nothing observable changed.
- **`DefinitionFor` returns `(definition, ok, error)`** so tool hosts,
  unknown ids, and agents are distinguishable at one call site.
- **The prompt test factory mirrors production.** `openPromptRuntime` resolves
  the definition from metadata, so the same fixtures cover both definitions.
- **SDK fixture.** The SDK's metadata fixture gained `definition: 'coding'`;
  no SDK source changed.
