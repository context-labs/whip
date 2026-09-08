# Gate the `workflow` tool behind an `experimental` config flag

`Branch:` dynamic-workflows

## What this does

The `workflow` tool (dynamic multi-agent orchestration) ships on by default
today. This gates it behind an opt-in config flag so it's honestly
experimental: absent the flag, the tool is never built into the agent's tool
set — the model never sees the schema, never tries to call it. Present the
flag, it's built as today.

## Goal

1. New config field `experimental []string` (JSON `experimental`). Listing
   a feature name opts into that feature. Today the only name is
   `"workflows"` (the `workflow` tool).
2. The agent carries the **whole experimental set** (mirroring the config
   field) and each gated feature checks its own name against it — one
   general mechanism, not a per-feature `WithWorkflow(bool)`. A new
   experimental feature = a `const FeatureX` + one `if` guard at its build
   site; no new option, no new config block.
3. The `workflow` tool is only appended to `a.Tools` when
   `"workflows"` is in the set — checked **before** the tool is built, not
   disabled post-hoc (unlike the `BrowserDisabled` field, which is set after
   `New()` and is effectively dead for the tool-set path; the browser tool
   is actually gated by a nil manager at runtime).
4. **Default OFF.** A stock config with no `experimental` key gets no
   `workflow` tool — the honest experimental posture.

## Non-goals

- No in-session `/workflows` toggle — it would need a tool-set rebuild (turn
  restart) to take effect, so config is the only honest UX. The TUI plan
  (`.ai-docs/plans/workflow-tui/`) owns in-session visibility.
- No change to the `workflow` tool's own description / opt-in nudge — that
  stays as the last line of guidance when the tool *is* enabled.
- No generic "experimental framework" beyond the `[]string` set + per-feature
  `const` + `experimentalEnabled(name)` check. That *is* the general
  pattern — deliberately not abstracted further (no registry, no enum)
  because one consumer doesn't justify it; the shape extends for free.
- No prompt-cache / system-prompt change (the tool isn't in the schemas, so
  nothing references it when off).

## Design

Surfaces: `internal/config` (field), `internal/agent` (constructor gate).

### 1. Config — `internal/config/config.go`

Add to `Config` (next to the other `omitempty` fields, ~line 184):

```go
// Experimental opts into not-yet-stable features by name. Today the only
// entry is "workflows" (the dynamic multi-agent workflow tool). Absent or
// empty = stable-only. whip's own agent reads this slice wholesale (see
// agent.WithExperimental) — no per-feature config block.
Experimental []string `json:"experimental,omitempty"`
```

No `normalize()` change, no `Has` helper on Config — the agent owns the
membership check (see below). `internal/config` is a leaf; no new import.

### 2. Agent constructor gate — `internal/agent/agent.go`

Problem: `New()` builds `a.Tools` (agent.go:292-303) and only returns the
pointer afterward, so a field set by the caller (the `BrowserDisabled`
pattern) can't influence tool construction. We want the gate known *before*
tools are built. We also want **one general mechanism**, not a per-feature
`WithWorkflow(bool)` (that would grow a new option per experimental feature).

Solution: the agent carries the **whole experimental set** (mirroring
`config.Config.Experimental`); each gated feature checks its own name
against it. One option, one field, one check pattern; a new experimental
feature = a constant + one `if` guard at its build site.

```go
// Experimental feature names the agent recognizes. Add a constant here as
// new experimental features ship; gate the build with experimentalEnabled.
const FeatureWorkflows = "workflows"

type Option func(*Agent)

// WithExperimental sets the opt-in experimental feature set (mirrors
// config.Config.Experimental). Tools gated on an experimental feature are
// only built when their name is present. Default empty = stable-only.
func WithExperimental(features []string) Option {
	return func(a *Agent) { a.experimental = features }
}

// Experimental returns the agent's opt-in experimental set, so fork/swap
// sites that rebuild the agent can inherit the gate (agent.Experimental()).
func (a *Agent) Experimental() []string { return a.experimental }

// experimentalEnabled reports whether name is in the agent's experimental
// opt-in set.
func (a *Agent) experimentalEnabled(name string) bool {
	for _, e := range a.experimental {
		if e == name {
			return true
		}
	}
	return false
}
```

New field on `Agent`:

```go
// experimental is the opt-in experimental feature set (mirrors
// config.Config.Experimental); set via WithExperimental before tools are
// built in New. Default empty = stable-only.
experimental []string
```

`New` gains a variadic options tail (applied before tool construction):

```go
func New(client *llm.Client, model string, maxTokens int, systemPrompt string, opts ...Option) *Agent {
	a := &Agent{ ... }
	for _, o := range opts { o(a) }
	a.Tools = tools.All()
	...
	a.Tools = append(a.Tools, taskTool(a), taskSteerTool(a))
	if a.experimentalEnabled(FeatureWorkflows) {
		a.Tools = append(a.Tools, workflowTool(a))
	}
	a.Tools = append(a.Tools, todoTool(a), waitTool(a), memoryTools(a)...)
	...
}
```

**Default is OFF.** This is the honest experimental posture the user asked
for: a stock config gets no `workflow` tool. Only `workflowtool_test.go`
exercises the tool, so only it opts in explicitly
(`agent.WithExperimental([]string{agent.FeatureWorkflows})`); the other ~15
test call sites are unchanged — they don't use the workflow tool, and no
test asserts a full-roster count (verified by grep), so dropping it from the
default set breaks nothing.

Why variadic options and not a positional `[]string`: 22 call sites (6
production + ~16 tests). A positional param rewrites every test; the
variadic tail lets tests omit it and production pass one option. This is the
first `Option` in the package — a small, standard Go idiom, not a new
dependency or framework. `internal/agent` stays config-agnostic: only a
`[]string` crosses the boundary, no import.

### 3. Call sites — pass the set

- `internal/tui/tui.go:1096` — `agent.New(client, apiID, maxOut, sysPrompt,
  agent.WithExperimental(cfg.Experimental))`
- `cmd/whip/run.go:114` — same
- `cmd/whip/acp.go:121` — same (ACP sessions respect the user's config)
- `cmd/whip/main.go:191` — benchmark path; leave bare (default-off — the
  benchmark doesn't use the workflow tool; verify, then leave it).
- `internal/tui/fork.go:201` and `tui.go:774` — these rebuild the agent
  mid-session (fork / model swap). They must inherit the *current* agent's
  set, not re-read config (the fork should match its parent). Thread via
  `agent.WithExperimental(m.agent.Experimental())`.

The existing `BrowserDisabled`/`ComputerDisabled` post-`New` assignments
stay as-is (they gate via nil manager at runtime; out of scope to refactor —
those are stable features, not experimental).

## Prior art

- `BrowserDisabled`/`ComputerDisabled` (agent.go:162-168) — the *anti*-prior:
  field set after `New` can't gate the tool set, only runtime. This plan
  avoids that by threading the gate into construction.
- `MCPImport` (config.go:194) — a config field gating which features load.
  Same shape (opt-in list), different surface.
- `ComputerConfig.Enabled *bool` (config.go:219) — the `*bool` enabled
  pattern. We use `[]string` instead because the gate is general (many
  features, one field) and the list is extensible without per-feature
  config blocks.

## Test plan

- `internal/config/config_test.go` — round-trip: `experimental` survives
  Save/Load (the existing round-trip test covers new fields automatically if
  it serializes the whole struct; check, else add a focused case).
- `internal/agent/workflowtool_test.go` — opt in explicitly:
  `agent.New(..., agent.WithExperimental([]string{agent.FeatureWorkflows}))`
  so the existing `TestWorkflowToolEndToEnd` stays green. **Add** a case:
  bare `agent.New(...)` (default-off) → the `workflow` tool is absent from
  `ag.Tools` (scan names, assert not present). **Add** a case: a set
  containing a *different* name → `workflow` still absent (the check is
  name-specific, not "any entry enables all").
- `internal/agent/agent_test.go` (or a new `options_test.go`) —
  `WithExperimental` sets the field; `Experimental()` returns it;
  `experimentalEnabled` matches its own name only. A few lines.
- No TUI test required (the gate is in the agent; TUI just passes the slice).
- Gate: `task check` (gofmt -s + vet + `go test ./...`) + `go test -race
  ./...` (the new field is read in `New`, no concurrent access — but stay
  consistent).

## Docs plan

- `docs/features.md` "Dynamic workflows" section — one-line note: gated
  behind `"experimental": ["workflows"]` in config; absent the flag the tool
  isn't built. (Behavior → code → tests.)
- `README.md` — only if it documents the tool set or config keys; add the
  `experimental` key to any config reference table.
- No roadmap checkbox (this is a gating refinement, not a roadmap feature).

## Tasks

- [ ] 1. config: `Experimental []string` field (+ verify round-trip test)
- [ ] 2. agent: `Option` / `WithExperimental` + `experimental []string` field
      + `Experimental()` getter + `experimentalEnabled(name)` + `FeatureWorkflows`
      const; gate the `workflowTool` append in `New`; default OFF
- [ ] 3. call sites: tui.go, run.go, acp.go pass
      `WithExperimental(cfg.Experimental)`; fork.go + tui.go swap sites inherit
      via `WithExperimental(m.agent.Experimental())`
- [ ] 4. `workflowtool_test.go`: opt in explicitly with `WithExperimental`;
      add default-off (tool absent) + wrong-name (still absent) cases
- [ ] 5. `task check` + `go test -race ./...` green
- [ ] 6. adversarial pass: fork inherits the set; resume (agent rebuilt) —
      verify a resumed session still honors the config flag
- [ ] 7. docs/features.md note

## Status

Awaiting sign-off.
