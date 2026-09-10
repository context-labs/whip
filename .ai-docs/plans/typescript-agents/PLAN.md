# TypeScript-authored agents

Status: proposed plan, September 10, 2026. No implementation yet.

Written against `feature/agent-definition` at `c27ce5075`, which landed the
[canonical definition](../agent-definition/PLAN.md) and
[JuniorDeveloper](../junior-developer/PLAN.md). Line numbers are from that tree.

## Request and recorded decisions

Let applications define agents in TypeScript through the SDK: system prompts,
tools, MCP servers, models, children. For custom agents this may become the
default authoring path. The implementation should be clean even where it
departs from today's runtime. Decisions recorded from the September 10
conversation:

1. **The environment is trusted.** Any connected client may register a
   definition or bind as an executor, over any transport. Authentication, when
   it arrives with hosted execution, applies to every operation uniformly.
2. **MCP servers a definition declares are native-trusted,** the same
   provenance class as the daemon's configuration file. Consent gating stays
   for imported project files and editor attachments.
3. **Keep the executor lease** and **record provenance** on registered
   definitions. Both are correctness and debugging aids, not security.
4. **RLM stays universal.** Custom tools are host operations a cell calls;
   there is no second model-facing interface.
5. **JSON Schema is the wire format for tool inputs.** A zod adapter can sit on
   top of the SDK later without becoming a dependency.
6. **Definitions persist with revisions** once they can arrive over the wire.
   The earlier "never store the body" decision applied while the binary was
   the only source; a registered definition's source is the store.
7. **The acceptance fixture is JuniorDeveloper in TypeScript.** Registered
   under its own id, it must compose byte-for-byte the same prompt as the Go
   value and run as a session with nothing but the SDK.

## Why this matters

- **A definition is already data.** `agentdef.Definition` has no functions;
  the daemon composes prompts, installs kernel modules, and issues root grants
  from it. A TypeScript representation is a serialization, not a new model.
- **The daemon cannot express tool bodies.** The one thing a definition cannot
  carry is code. Every custom agent that needs more than built-in modules and
  MCP servers needs somewhere for its handlers to run, and that place has to
  obey the same lifecycle rules as a native tool: one invocation record, one
  authority path, bounded results, explicit failure.
- **Selection and persistence exist but only for built-ins.** `DefinitionFor`
  (`internal/daemon/definition.go`) resolves ids through a fixed slice of Go
  constructors, and `sessions.definition` stores an id with no revision. A
  registered definition needs a stored body and a pinned revision so a running
  session never changes under a redeploy.

## Target state

**One document, two sources.** The Go struct is the schema; the protocol
generator emits its TypeScript type. Built-ins are constructed in Go; registered
definitions are stored rows. `Lookup(id, revision)` hides the difference from
every consumer: the factory, session creation, the CLI, and clients.

**Three kinds of tools, one surface.** Built-in host modules, selected by name.
MCP servers, named by the definition and resolved against host configuration.
Custom tools, each a name, description, JSON Schema, and a handler, exposed as
operations of one reserved module, `tools`, so the model calls
`tools.lookup_ticket(id="...")` exactly like `files.read`. Arguments are
validated in Go with `github.com/google/jsonschema-go`, already a dependency,
before dispatch. Every call is an operation in the capability ledger under a
new `tools` grant, so children narrow it and restart interrupts it like any
other uncertain effect.

**The executor is the permission pattern pointed the other way.** The SDK
process that calls `serve(agent)` holds a lease for a definition and revision.
The daemon records a durable pending invocation, notifies the executor, and the
executor answers with an idempotent RPC keyed by invocation id and lease
generation. No bidirectional JSON-RPC requests; reconnect, dedupe, and
uncertain-effect handling reuse what permission decisions already have.

**What an author writes.**

```ts
import { defineAgent, tool } from '@whip/sdk/agents';

const support = defineAgent({
  id: 'support-bot',
  instructions: { persona: 'You answer support tickets for Acme.', rules: '...' },
  model: { model: 'kimi-k3-fast', provider: 'inference-net' },
  modules: ['context', 'state', 'artifacts', 'user'],
  mcp: { servers: ['docs'] },
  tools: [
    tool('lookup_ticket', 'Fetch a ticket by id',
      { type: 'object', properties: { id: { type: 'string' } }, required: ['id'], additionalProperties: false },
      async ({ id }, ctx) => db.tickets.get(id)),
  ],
});

const runtime = await client.agents.serve(support); // register, bind executor, stay connected
const session = await runtime.sessions.create({ cwd: '/srv/acme' });
```

`defineAgent` returns data plus a handler map; nothing in the document is a
function. The `agents` entry point imports no Node-only modules, so browser
observers are unaffected; handler code runs wherever `serve` was called.

## Phase 1: definitions over the wire

Goal: register a TypeScript-authored definition, create sessions against it
from the SDK and the CLI, and prove equivalence with the Go JuniorDeveloper.
No custom tools yet. A custom agent with built-in modules and named MCP servers
is fully usable after this phase.

### Go

- **JSON codec for `Definition`.** Add snake_case `json` tags to
  `agentdef.Definition`, `Instructions`, `ModelDefaults`, `CompactionDefaults`,
  `Child`, and `Surface`. Add `MCP struct{ Servers []string }`: nil means every
  configured server, an explicit list narrows, and any list requires the `mcp`
  capability. Coding keeps nil. Add `Tools []Tool` as an empty placeholder so
  the document shape is stable across phases.
- **Registered definitions.** `Validate` gains registration rules: an id must
  match `^[a-z][a-z0-9-]{1,63}$`, must not be a built-in id, and children may
  not name tools that do not exist. `Revision(document) string` is the hex
  SHA-256 of the canonical JSON.
- **Schema 17.** `definitions(id, revision, body, registered_by, created_at,
  PRIMARY KEY(id, revision))` and `sessions.definition_revision TEXT NOT NULL
  DEFAULT ''`, empty meaning a built-in. Follow `upgradeV15` in
  `internal/session/definition_migration.go`.
- **Store.** `RegisterDefinition(ctx, body, clientID) (id, revision, created
  bool, error)` is idempotent on content. `LoadDefinition(ctx, id, revision)`,
  `LatestDefinition(ctx, id)`, `ListDefinitions(ctx)` returning id, latest
  revision, registered_by, created_at. Session creation stores the pinned
  revision; `Meta` and the snapshot query in `internal/session/event.go:226`
  carry `DefinitionRevision`.
- **Resolution.** `agentdef.Lookup` stays the built-in registry.
  `daemon.DefinitionFor(meta)` resolves built-ins by id and registered
  definitions by id and revision from the store, failing clearly on a missing
  row. `resolveSessionDefaults` accepts a registered id, pins the latest
  revision, and rejects unknown ids listing built-in ids plus registered ids.
  `installReplacement` rebuilds from the same pinned revision because it reads
  the session's metadata.
- **MCP narrowing.** The factory filters `discovery.Merged`
  (`cmd/whip/daemon.go:160`) to `definition.MCP.Servers` when set; the
  definition's servers keep native trust.
- **Protocol 6.2.** RPCs: `definitions.register` (ephemeral, params: the
  document; result: id, revision, created), `definitions.get` (query),
  `definitions.list` (query). `session.create` unchanged; the daemon pins the
  revision. `session.Meta.definition_revision` added.
- **CLI.** `--agent` accepts registered ids. Client-side validation against
  `agentdef.IDs()` moves to the daemon's creation error, which lists every
  available id.

### SDK

- New entry `@whip/sdk/agents`: `defineAgent(input)` maps camelCase authoring
  input to the generated snake_case `Definition` and validates locally against
  the generated schema; `tool(name, description, schema, handler)` returns a
  spec plus handler (handlers are unused until phase 2 but the shape lands
  now); `client.agents.register(agent)`, `client.agents.list()`,
  `client.agents.get(id)`.
- `examples/agents/junior-developer.ts`: the TypeScript JuniorDeveloper below.

### Tests

- `agentdef`: JSON round trip of every built-in is lossless; registration
  validation rejects built-in ids, bad ids, and children naming unknown tools;
  `Revision` is stable across key order and whitespace.
- `session`: schema 16 to 17 upgrade; register is idempotent by content and
  yields a new revision for a changed body; a session pins the revision and a
  later registration does not move it; fork copies both id and revision.
- `daemon`: `DefinitionFor` resolves a registered definition and refuses a
  missing revision; a session created for a registered id composes its
  persona; `definitions.list` includes built-ins and registered rows with
  provenance.
- **Equivalence fixture.** The SDK test runs `defineAgent` on the TypeScript
  JuniorDeveloper and asserts the document equals
  `packages/sdk/test/fixtures/junior-developer.json`. A Go test loads the same
  fixture, sets the id to `junior-developer`, and requires `reflect.DeepEqual`
  with `agentdef.JuniorDeveloper()`; the existing golden then proves the prompt
  bytes match. Two directions, one committed file.
- **Acceptance.** The SDK acceptance harness registers the document as
  `junior-developer-ts`, creates a session, and checks the snapshot's
  definition and revision; `whip run --agent junior-developer-ts` works against
  the same daemon.

## Phase 2: custom tools and the executor

Goal: a definition's `tools` run in the SDK process under the runtime's
lifecycle rules.

### Definition and prompt

- `Tool{Name, Description, InputSchema json.RawMessage, TimeoutMillis}` with a
  default timeout of 5 minutes and a ceiling of 15 minutes (decided September
  10). A running handler holds the cell's kernel slot, so long work should
  still return a handle the cell polls. `Child.Tools []string` narrows by name
  through `Definition.Child`, like modules and capabilities.
- `rlm.RuntimeGuide` takes the tool specs and renders the `tools` module: one
  catalog line per tool from its name, required properties, and description,
  bounded in length, plus one rule line describing keyword arguments and
  JSON or handle results.

### Kernel

- `KernelOptions.Tools []string` and a `-tools` worker flag beside `-modules`.
  The Starlark worker installs a `tools` module whose operations are the
  names; the QuickJS allowlist gains `tools.<name>`. `validateModuleOperation`
  accepts dynamic `tools` operations from the manifest and nothing else.

### Authority and dispatch

- `capability.Authority` gains `Tools Reference`; root bootstrap issues a
  `tools:<root>` grant whose operations are `tools.<name>` for the
  definition's tools; children receive narrowed delegations through
  `capabilityDelegations`. Restart reconstructs it like the other grants.
- `Services.BindDispatcher` registers `tools.<name>` operations from the
  definition with a handler that validates arguments against the schema and
  hands the call to the executor registry. `Services.run` maps the `tools.`
  prefix to `authority.Tools`. `recursiveHost.Call` routes the `tools` module
  through the dispatcher, so the ledger records every invocation.

### Executor protocol

- `executor.bind` (ephemeral): definition id, revision, handler names. The
  daemon checks the names cover the definition's tools and returns a lease
  generation; a later bind for the same definition and revision replaces the
  previous holder, whose late results are rejected.
- `tool.invoke` notification on the executor's connection: invocation id,
  root, agent, turn, tool name, validated input, deadline, lease generation.
  Notifications are nudges; the pending operation row is the truth.
- `executor.pending` (query): invocations awaiting this lease, for reconnect.
- `tool.progress` and `tool.result` (ephemeral): keyed by invocation id and
  lease; a result for a settled or foreign invocation is rejected; results
  above the inline limit go through the content store as MCP results do.
- `tool.cancel` notification when the turn is cancelled or the deadline
  passes; the daemon settles the operation as cancelled regardless of whether
  the handler stops.
- Failure semantics: no bound executor fails the call after a bounded wait
  with an error the model reads; a disconnect after dispatch marks the
  invocation interrupted and never replays it; the handler receives the
  invocation id for idempotency.

### SDK

- `client.agents.serve(agent)` registers, binds, and runs the executor loop:
  routes `tool.invoke` to the handler, reports progress, posts the result,
  honors cancellation through an `AbortSignal`, and re-binds after reconnect,
  draining `executor.pending` first. The client's inbound dispatch
  (`packages/sdk/src/client.ts:325`) routes the new notification methods.
- Handler context: invocation id, root and agent ids, turn id, deadline,
  signal, and a bounded `content` accessor for handles.

### Tests

- Kernel: a `tools` manifest installs only the named operations for both
  engines.
- Daemon: an invocation reaches a bound executor and its result enters the
  cell; duplicate and late results are rejected; disconnect mid-call leaves an
  interrupted operation and no replay; deadline cancels; no executor fails
  closed with the expected error; a child cannot call a tool its parent
  narrowed away; restart restores the `tools` grant.
- SDK: `serve` binds before creating sessions, re-binds after reconnect, and
  passes cancellation to handlers.
- Fixture: a second example agent with one tool and one blocking check; the
  JuniorDeveloper fixture stays tool-free and unchanged.

## Phase 3: named children

- `agents.spawn(definition="researcher", ...)` selects a named child from the
  parent's `Children`; `Child.Budgets` and `Child.Report` apply when set.
  Children of a registered definition resolve tools to the same executor.
- SDK `children` in `defineAgent` and a `definitions.get` result that lists
  them for clients.

Tests: spawn by name applies the child's instructions, modules, capabilities,
and tools; an unknown name fails; restore keeps the child's resolved
definition.

## Phase 4: hooks

On the same invocation channel: `before_tool` and `before_spawn` decisions
that deny or rewrite arguments, revalidated after rewriting, and a
`turn_start` context contribution appended as ephemeral system text. Decisions
are idempotent, so a timeout fails closed and a lost reply is re-asked. Scoped
in a later plan.

## Preserved, changed, not built

**Preserved**

- Every coding and JuniorDeveloper prompt golden.
- Built-in definitions constructed in Go; `agentdef.Lookup` semantics.
- One provider loop, one model-facing tool, one capability ledger.
- Observer connections and their backpressure rules.
- The permission-decision pattern, now generalized rather than replaced.

**Changed**

- Definitions become registrable, persisted, revisioned data; sessions pin a
  revision.
- A dynamic `tools` module and a `tools` grant alongside files, shell, and MCP.
- A new executor role with a lease and its own notifications.
- Protocol minors 6.2 and 6.3.
- CLI `--agent` validation moves to the daemon.

**Not built**

- Authentication or per-transport restrictions on registration.
- Consent gating for definition-declared MCP servers.
- A picker UI in the TUI, web, or mobile clients.
- Changing a session's definition or revision after creation.
- Kernel-side sandboxing of handler code; handlers are application code.
- Direct named tools or any second model-facing interface.
- Hooks beyond the outline in phase 4.

## Risks and edge cases

- **Kernel slot occupancy.** Handlers hold the worker slot; the per-tool
  timeout is mandatory and bounded at 15 minutes. Long work should return a handle the cell
  polls, as `shell.start` does; scope that pattern when a fixture needs it.
- **Uncertain effects.** The ledger marks a lost result interrupted and never
  replays; handlers must be idempotent on the invocation id.
- **Version skew.** An executor binds a specific revision; a session pinned to
  an older revision with no executor for it fails closed with a clear message.
- **Schema drift.** The generated TypeScript `Definition` type and the Go
  struct are one source; `task contract` catches drift.
- **Grant rows.** A root without custom tools still has a `tools` grant row
  with an empty operation list, matching how narrowed MCP grants work.
- **Executor stream isolation.** Invocation notifications travel on the
  executor's connection only; an observer's slow consumer cannot close it.

## The JuniorDeveloper fixture in TypeScript

This is the phase 1 acceptance artifact, `examples/agents/junior-developer.ts`.
It uses only the `agents` entry point and no custom tools.

```ts
import { defineAgent } from '@whip/sdk/agents';

export const juniorDeveloper = defineAgent({
  id: 'junior-developer-ts',
  instructions: {
    persona: 'You are a junior developer working under review.',
    rules: [
      'Operating rules:',
      '- Keep each change small, and explain what you changed and why in plain language.',
      "- Run the project's tests or build after every change; if you cannot run them, say so.",
      '- Never rewrite history, force-push, delete branches, or remove files you did not create.',
      '- Do not add dependencies or change build, CI, or deployment configuration; ask first.',
      '- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.',
    ].join('\n'),
    projectFiles: ['CLAUDE.md', 'AGENTS.md'],
    skillDiscovery: false,
    standingInstructions: true,
  },
  modules: ['context', 'files', 'shell', 'state', 'artifacts', 'permissions', 'user'],
  capabilities: ['read', 'write', 'shell'],
  surface: { autoTitle: true, goalLoop: false },
});
```

The document it produces, and the committed fixture the Go and SDK tests both
compare against:

```json
{
  "id": "junior-developer-ts",
  "instructions": {
    "persona": "You are a junior developer working under review.",
    "rules": "Operating rules:\n- Keep each change small, and explain what you changed and why in plain language.\n- Run the project's tests or build after every change; if you cannot run them, say so.\n- Never rewrite history, force-push, delete branches, or remove files you did not create.\n- Do not add dependencies or change build, CI, or deployment configuration; ask first.\n- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.",
    "project_files": ["CLAUDE.md", "AGENTS.md"],
    "skill_discovery": false,
    "standing_instructions": true
  },
  "modules": ["context", "files", "shell", "state", "artifacts", "permissions", "user"],
  "capabilities": ["read", "write", "shell"],
  "model": { "model": "", "provider": "", "effort": "" },
  "compaction": { "model": "", "provider": "", "threshold": 0 },
  "mcp": { "servers": null },
  "tools": [],
  "children": {},
  "surface": { "auto_title": true, "goal_loop": false }
}
```

Running it after phase 1:

```ts
await client.connect();
const { revision } = await client.agents.register(juniorDeveloper);
const created = await client.sessions.create({ cwd: '/path/to/repo', definition: 'junior-developer-ts' }).result();
```

and from the terminal, against the same daemon:

```bash
whip run --agent junior-developer-ts --permission-mode automatic "add a unit test for the parser"
```

The equivalence test is the whole point of choosing this fixture: with its id
set to `junior-developer`, the document must deep-equal `agentdef.JuniorDeveloper()`,
and its composed prompt must match the golden already on disk. If those hold,
the TypeScript path and the Go path describe the same agent.
