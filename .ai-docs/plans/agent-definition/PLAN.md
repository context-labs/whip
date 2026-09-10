# Canonical agent definition: the coding agent as the first definition

Status: approved September 10, 2026; ready for implementation. No code has
been written yet.

Written against the working tree on `codex/provider-onboarding` at `f1093a0d7`
plus uncommitted changes. Line numbers below are from that tree. This plan
supersedes the definition portion of the
[Quant migration architecture](../quant-whip-migration/architecture.md); every
other part of that document is out of scope here.

## Request and recorded decisions

Narrow the "Whip as a generic agent runtime" effort to one item: a canonical
agent definition, with the current coding agent as the first and only
definition. Decisions recorded from the September 10 conversation:

1. Go only. No TypeScript, protocol, or database representation yet.
2. Keep the current paradigm: every turn recomposes the prompt from the running
   binary. Nothing new is persisted. The existing `sessions.kind` column
   selects the coding definition. An identity column such as
   `definition = coding@3` is added only when a second definition exists. The
   resolved body is never stored.
3. The definition supplies model, provider, effort, and compaction defaults.
   The existing per-session overrides stay authoritative.
4. Project instruction discovery, skill discovery, standing instructions, auto
   title, and the goal loop are definition toggles. `user.ask` is the `user`
   module and is covered by module selection. Git workspace snapshots stay a
   runner feature. Diff rendering and LSP output in tool results are out of
   scope.
5. Named child definitions exist in the struct and are empty for coding.
   Children keep resolving as "parent definition, narrowed by spawn arguments".
6. Module selection is real: the prompt catalog derives from the selected
   module list. Coding selects every module.
7. The `run.configure` system override becomes a session override on the
   effective definition. It keeps its verbatim semantics, stays in memory, and
   must survive a runtime replacement. Whether it should later replace only the
   persona and rules while keeping the runtime guide is a recorded follow-up,
   not part of this pass.
8. New package `internal/agentdef`. Alternatives considered: `definition`
   stutters at call sites; `agents` collides with the host module name.
9. The auto-title prompt drops the word "coding" and becomes
   "Name this session." No definition field for it.

## Why this matters

The coding agent has no home. Its behavior is assembled in seven places:

| Piece | Where | Layer |
| --- | --- | --- |
| Persona, module catalog, rules | `internal/rlm/prompt.go:53`, `internal/rlm/environment.go:68` | Go constants, recomposed every turn |
| JavaScript prompt variant | `internal/rlm/javascript_prompt.go:12` | Line-by-line rewrite of the Starlark text |
| Project instructions, skills, `me.md` | `internal/rlm/environment.go:83` | Filesystem discovery, always on |
| Model route, sampling, effort, compaction | `cmd/whip/daemon.go:118` | Factory reading host config |
| Browser, computer, LSP wiring | `cmd/whip/daemon.go:282` | Factory, always on |
| Root capability set | `internal/daemon/recursive_runtime.go:261` | Literal at bind |
| Callable host modules | `internal/rlm/modules.go:22` | Compile-time map, identical for every agent |

Concrete costs today:

- **A second agent requires forking the daemon.** There is no value to swap.
- **Per-session behavior has no single owner, so it leaks.** `run.configure`
  stores the system override, turn cap, and cache key on the live
  `AgentSession` (`internal/daemon/client_control.go:224`). A model change
  rebuilds the runtime and `installReplacement`
  (`internal/daemon/client_control.go:1405`) copies only permission policy.
  The override is silently dropped and the root reverts to the composed prompt.
- **The catalog does not reflect the registry.** `messages.ack` is callable
  but undocumented; the catalog is a hand-maintained string, not derived from
  `moduleRegistry`.
- **Nothing tests the coding agent as a unit.** Five daemon test suites hand
  wire `agent.NewRuntime` fields; a regression in composition is invisible
  until a live run.
- **Capabilities are a literal.** `Bind` grants every root all six capability
  names regardless of configuration.

## Target state

One Go value describes the coding agent. The factory, prompt composer, bind,
spawn, restore, and root-only surface features read from it. Per-session deltas
apply as overrides to produce an **effective definition**, and the factory
always builds from that, so a runtime replacement cannot lose them. Children
resolve to the same struct: parent effective definition, narrowed.

### Package and shape

Package `internal/agentdef`. No interfaces. One constructor per agent.

```go
package agentdef

type Definition struct {
	ID           string            // "coding"
	Instructions Instructions
	Modules      []string          // host module names the model is told about and may call
	Capabilities []string          // root capability names: read write shell browser computer mcp
	Model        ModelDefaults     // empty fields fall back to host configuration
	Compaction   CompactionDefaults
	Children     map[string]Child  // named child definitions; none for coding
	Surface      Surface
}

type Instructions struct {
	Persona              string   // "You are an expert coding agent."
	Rules                string   // operating rules that are about the agent, not the runtime
	ProjectFiles         []string // {"CLAUDE.md", "AGENTS.md"}; empty disables discovery
	SkillDiscovery       bool
	StandingInstructions bool     // me.md
}

type ModelDefaults struct{ Model, Provider, Effort string }
type CompactionDefaults struct {
	Model, Provider string
	Threshold       float64 // 0 = host default
}

type Child struct {
	Instructions string
	Modules      []string
	Capabilities []string
	Model        ModelDefaults
	Budgets      map[string]int64
	Report       string
}

type Surface struct{ AutoTitle, GoalLoop bool }

func Coding() Definition
func (d Definition) Validate() error // Modules ⊆ rlm.Modules(), Capabilities ⊆ known names
func (d Definition) Child(name string, overrides ChildOverrides) (Definition, error) // narrows only
```

What stays outside the definition on purpose: provider endpoints and
credentials, RLM kernel limits, browser and computer host policy, the LSP
server registry, permission mode and rules, and the execution engine. Those
are host or session facts.

### Splitting the prompt

Today `BuildPrompt` is one string mixing three things. The split:

| Text | Owner | Where it goes |
| --- | --- | --- |
| "You are an expert coding agent." | definition | `Instructions.Persona` |
| "Your only tool is rlm_exec…", cell guidance, scratch and state rules, handle rules, budget rules | runtime | `rlm.RuntimeGuide(engine, modules, cwd, history)` |
| Per-module catalog lines and per-module rules, for example the `mcp` discovery rule, the `agents` budget and capability rules, the messaging section, the `shell.start` guidance | runtime, per module | fragments selected by `Modules` |
| Working directory line, available-context handle line | runtime environment | stays in `RuntimeGuide` |
| Git hygiene, the `@` file-tag rule | definition | `Instructions.Rules` |
| "Bias toward acting…", child collaboration and report-mode rule | runtime | `RuntimeGuide` core rules; the collaboration rule attaches to the `agents` fragment |
| Instruction scope and precedence | discovery | emitted only when `ProjectFiles` is non-empty |

The JavaScript variant keeps working unchanged: `BuildPromptForEngine` rewrites
six specific lines by prefix and then rewrites host-call examples generically.
Fragments preserve those line prefixes, so the same rewriter runs over the
assembled guide. The engine descriptor's `GuideSHA256`
(`internal/rlm/engines.go:54`) becomes the hash of `RuntimeGuide` over all
modules, keeping its meaning as "hash of the runtime guide text".

### Effective definition and overrides

`Session` gains one field holding its effective definition and one holding the
last `run.configure` payload. `installReplacement` re-applies that payload to
the new runner after bind. Model, provider, and effort continue to come from
`meta` as today. This is the whole fix for the lost-override defect.

### Children

`spawnAttempt` resolves the child definition through `Definition.Child`, which
applies the existing narrowing rules to capabilities, adds the same rule for
modules, and carries model, effort, budgets, and report as it does now. Nothing
new is persisted per child; `restoreChildren` rebuilds the child definition from
the parent's effective definition plus the agent row, exactly as it rebuilds the
route today. Named children are looked up in `Children` when a spawn names one;
coding has none, so this path is exercised only by a unit test.

### Where the definition is consumed

| Consumer | Today | After |
| --- | --- | --- |
| `cmd/whip/daemon.go` factory | inline assembly | `agentdef.Coding()` selected by `meta.Kind`; passed in `RecursiveRuntimeOptions.Definition` |
| `resolveSessionDefaults` (`internal/daemon/control.go`) | host `DefaultModel` | definition `Model` defaults, then host defaults |
| compaction route in the factory | host `CompactModel`, `CompactPct` | definition `Compaction`, then host |
| `daemonToolServices` | browser and computer always wired | wired only when named in `Capabilities` |
| `RecursiveRuntime.Bind` | capability literal | `definition.Capabilities` |
| `AgentSession.refreshPrompt` and `rlm.ComposePrompt` | constants and always-on discovery | `Instructions` and `Modules` via `PromptOptions` |
| `spawnAttempt`, `restoreChildren` | clone parent struct | `parent.definition.Child(...)` then clone |
| `maybeGenerateTitle`, goal continuation in `completeTurn` | unconditional | gated by `Surface` |
| `RecursiveRuntimeOptions` in five daemon test suites | no definition | zero value defaults to `Coding()`, so tests stay untouched |

## Implementation steps

Each step leaves `go test ./...` and `task acceptance` green. Steps 1 and 2 can
land before anything in the daemon changes.

### Step 0. Capture the golden

Before touching any prompt code, add a test that writes the current assembled
prompt for both engines, root and child identity, with and without a context
handle, into `internal/rlm/testdata/coding-prompt/*.golden`. This is the
acceptance guard for the whole plan: the coding agent's prompt must be
byte-identical before and after extraction. Also snapshot `Engines()` guide
hashes.

Tests: new golden test only.

### Step 1. Add `internal/agentdef`

`Definition`, `Coding()`, `Validate`, `Child`, and `ChildOverrides`. `Coding()`
lists all fourteen modules and all six capability names, empty model and
compaction defaults, empty children, both surface flags true. `Child` rejects
widening of capabilities or modules with the same error text used by
`requestedCapabilities` today.

Tests: `agentdef` unit tests for validation and narrowing.

### Step 2. Split the runtime guide in `internal/rlm`

- Introduce per-module fragments and `RuntimeGuide(engine, modules, cwd, history)`.
- `PromptOptions` gains `Persona`, `Rules`, `ProjectFiles`, `SkillDiscovery`,
  `StandingInstructions`, and `Modules`. `SkillDirs` already expresses
  "disabled" as a non-nil empty slice; `SkillDiscovery=false` maps to that.
- `ComposePrompt` emits sources in the same order and with the same source
  kinds as today so the context audit view does not change.
- Keep `BuildPromptForEngine(engine, cwd, history)` as a thin wrapper that
  builds the coding guide over all modules; `evals/rlm/eval_test.go` calls it
  at four sites and `engines.go` hashes it.
- Instruction scope and precedence text is emitted only when `ProjectFiles`
  is non-empty.

Tests: golden from step 0 must still pass byte for byte. `prompt_test.go`,
`environment_test.go`, `quickjs_test.go` may need their expected strings
updated only if the golden proves the bytes changed, which the plan does not
allow.

### Step 3. Thread the definition through the daemon

- `RecursiveRuntimeOptions.Definition`; zero value means `agentdef.Coding()`.
- `AgentSession.definition`; `newNode` takes it; `Bind` reads capabilities
  from the root definition.
- `refreshPrompt` fills `PromptOptions` from the definition. The root override
  path is unchanged.
- `spawnAttempt` and `restoreChildren` call `Definition.Child`.
- `Session` stores the last `run.configure` payload; `installReplacement`
  re-applies it after bind.
- `maybeGenerateTitle` and goal continuation check `Surface`.
- The title prompt in `internal/daemon/agent_session.go:412` becomes
  "Name this session. Reply with a plain 3-6 word title: no quotes and no
  trailing period."

Tests: existing daemon suites. New tests: a `session.model` command after
`run.configure` keeps the override; a child's effective definition equals the
parent's narrowed by spawn arguments; a definition with `Surface.AutoTitle`
false never generates a title. Update
`apps/desktop/scripts/onboarding-smoke.mjs:72`, whose fake provider recognizes
the title request by its opening words.

### Step 4. Factory and creation defaults

- `cmd/whip/daemon.go` selects `agentdef.Coding()` for `SessionKindAgent`,
  passes it to the runtime, gates browser and computer wiring on
  `Capabilities`, and applies compaction defaults with definition-then-host
  precedence.
- `resolveSessionDefaults` applies definition model defaults before host
  defaults.

Tests: `cmd/whip/daemon_test.go`, `run_test.go`, `internal/daemon/control`
creation tests.

### Step 5. Documentation

- `docs/rlm-runtime.md` "Environment prompts": describe persona, runtime
  guide, module fragments, and discovery toggles.
- `docs/architecture.md` package map: add `internal/agentdef`.
- `docs/features.md`: note that the coding agent is a definition and that the
  run override survives model changes.

## Preserved, changed, not built

**Preserved**

- Every system prompt byte for the coding agent, for both engines, root and
  child.
- The module set, the six capability names, child inheritance and narrowing.
- Database schema. No migration.
- Protocol and SDK. No generated contract changes.
- TUI and web behavior. Context audit source kinds and order.
- `whip run -system` verbatim override semantics.

**Changed**

- New package `internal/agentdef` owns the coding agent's composition.
- `run.configure` state survives `session.model` and `session.reload`.
- Browser and computer host wiring follows the definition's capabilities. No
  visible change for coding, which names both.
- The auto-title prompt reads "Name this session." This is a two-word,
  model-visible change outside the system prompt golden; the desktop smoke
  script that matches the old wording is updated in step 3.
- `GuideSHA256` is computed from the runtime guide over all modules. Its bytes
  do not change if step 0 holds.

**Not built**

- Definition identity or body in the database, and any migration.
- TypeScript or protocol representation of a definition.
- Hooks, TypeScript tools, callback hosts, executor clients.
- Named children for coding, and a `modules=` spawn argument.
- Direct named tools, or any second model-facing interface.
- Moving diff rendering or LSP diagnostics out of tool results.
- Persisting the system override.

## Risks and edge cases

- **Byte drift during the split.** The golden test is the only reliable guard.
  Do not merge step 2 with a regenerated golden; the golden is regenerated only
  by an explicit decision.
- **`GuideSHA256` is advertised in `initialize`.** Clients compare it only for
  display today. If the golden holds, the hash is unchanged.
- **The JavaScript rewriter matches line prefixes.** Fragment boundaries must
  keep the six rewritten lines intact as single lines.
- **Children of an overridden root.** Unchanged: the override is root-only and
  children keep composing from the definition.
- **Test fixtures without a definition.** The zero value defaults to coding so
  the five daemon suites and the evals do not change.
- **`user.ask` stays root-only** regardless of module selection; the check in
  `internal/daemon/question.go:204` remains.

## Recorded follow-up

`whip run -system` currently replaces the entire composed prompt, including the
runtime guide, so a headless run with a custom system prompt receives no module
documentation. Once the definition exists, the override could replace only
`Instructions` and keep the runtime guide. Deferred by decision 7; revisit after
this plan lands.
