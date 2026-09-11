# Agent hooks

Status: proposed plan, September 10, 2026. No implementation yet.

Written against `feature/agent-definition` at `26ef5509b`, which landed
[TypeScript-authored agents](../typescript-agents/PLAN.md): registrable
definitions, custom tools served by an executor over a lease, and named
children. This plan is that plan's phase 4, scoped on its own after the
September 10 conversation and a survey of how the Claude Agent SDK, Pi, and
OpenCode do the same thing.

## Request and recorded decisions

Let an authored agent observe and gate what its sessions do, from the same
process that serves its tools: a hook before every host operation that can
deny or rewrite it, and a hook at turn start that contributes context.
Decisions recorded from the conversation:

1. **`before_tool` sees every host operation**, not only custom tools: every
   `module.operation` a cell calls, on the root and on every child. A
   definition may narrow it to named operations; the default is all of them.
2. **Gates hold the cell like a tool call does.** Default timeout 30 seconds,
   ceiling 60, per hook.
3. **Rewrite is in scope.** A hook may replace the arguments; the rewritten
   arguments take exactly the path fresh arguments take, so they are validated
   the same way, and the model is told what changed.
4. **Hooks apply to children and named children.** A child is the same
   definition narrowed; its hooks are its parent's.
5. **Required by default, fail closed.** A hook the executor does not answer
   (no executor bound, disconnected, timed out, or wrong revision) denies the
   operation with an error the model reads. A definition may mark a hook
   `optional`; then the operation proceeds with a visible notice. `turn_start`
   contributes rather than gates, so it always behaves as optional.
6. **Decisions are visible.** Every deny, rewrite, and skipped hook is a stream
   event, so the TUI and web can show why a cell failed or what actually ran.
7. **Every result field is optional.** A reply with no fields means allow,
   unchanged. That makes observe-only hooks one line. A reply that never
   arrives is not an empty reply: it is the fail-closed case in decision 5.
8. **A thrown handler error is a deny** for a required hook, with the error
   text as the reason, matching Pi and OpenCode. For an optional hook it is a
   skip with a notice.
9. **Hooks only narrow.** A hook cannot grant authority the ledger denies and
   cannot bypass the user's permission mode. `allow` means "no objection", not
   "approved on the user's behalf".

## Why this matters

A definition today says what an agent is and what it may reach: persona,
modules, capabilities, tools, children. It cannot say what the agent should do
with that reach on a given call. Three things people want from an authored
agent are missing without hooks:

- **Guardrails written by the author, not the user.** The permission mode
  belongs to whoever runs the session. An author who wants "never run
  `rm -rf`", "never read `.env`", or "children may not spawn children" has
  nowhere to put it. Hooks are where every comparable runtime puts it.
- **Live context.** A triage agent wants the on-call roster and open incident
  count in each turn. The system prompt is composed once; a per-turn
  contribution is the only clean place for facts that change between turns.
- **Observability from the application.** The application already serves the
  tools; it wants to see the operations too, for logging and metrics, without
  subscribing to the whole event stream.

The executor lease from the previous plan makes all three cheap: the process
is already connected, already answers idempotent invocations by id and
generation, already re-binds after reconnect. Hooks add two notification
kinds on that channel and nothing new to the runtime's shape.

## How the others do it, and what we take

All three run hooks in the same process as the agent, so "hook unavailable"
is a bug in their world and a normal state in ours. Everything else transfers:

- **Before-tool fires for every tool** and the handler filters (Claude Code
  narrows with matchers; Pi and OpenCode filter inside the handler).
- **Deny reaches the model as an error result with the reason**, and the turn
  continues. None stop the turn.
- **Rewrite exists everywhere.** Claude's `updatedInput`, Pi's in-place
  `event.input`, OpenCode's `output.args`. Pi documents that it does not
  revalidate; we do, by construction.
- **Per-turn context is transient**: Claude's `additionalContext`, Pi's
  `before_agent_start` system prompt, OpenCode's `chat.system.transform`.
- **Failure differs.** Claude proceeds on a timeout or crash and blocks only on
  an explicit decision; Pi and OpenCode block on a handler exception and have
  no timeout at all. We block on exception (decision 8) and, because our hook
  process can be absent, block on absence too unless the author opts out
  (decision 5).
- **Nobody labels a block as a hook decision in the transcript.** Claude shows
  a `systemMessage`; Pi and OpenCode render an ordinary tool error. We emit a
  dedicated event (decision 6).

## Target state

**Two wire hooks, one seam each.**

- `before_tool` runs in `recursiveHost.Call`, the single entry from a kernel
  into the host, before the module switch. That is the one place every host
  operation passes with its module, operation, arguments, and node identity.
  A deny returns an error to the cell. A rewrite replaces the arguments map
  and falls through to the same handler fresh arguments reach, so path
  canonicalization, JSON Schema validation for custom tools, ledger checks,
  and permission prompts all run on what will actually execute. Nothing is
  gated twice and nothing is gated after the fact.
- `turn_start` runs in `AgentSession.RunTurn` after the prompt is composed and
  before the first provider request. Its `context` joins the turn's ephemeral
  system text, the mechanism the worker-restart and budget notices already
  use: included in every request of the turn, never in history.

**`before_spawn` is `before_tool` for `agents.spawn`.** The spawn arguments a
cell passes (prompt, name, definition, capabilities, tools, budgets, report,
model) are exactly what a spawn hook wants to see, and a rewrite of them flows
into `spawnAttempt`, where `Definition.Child` still refuses to widen. A
separate wire hook would carry the same payload under a second name. The SDK
offers `beforeSpawn` as a typed view over `before_tool` narrowed to
`agents.spawn`; the daemon sees one hook kind.

**Declared in the definition, served by the executor.**

```go
type Hooks struct {
    BeforeTool *Hook `json:"before_tool"`
    TurnStart  *Hook `json:"turn_start"`
}

type Hook struct {
    // Operations narrows before_tool to these module.operation names; nil
    // means every host operation. Ignored for turn_start.
    Operations []string `json:"operations"`
    // Optional hooks proceed with a notice when unanswered or failing.
    Optional bool `json:"optional"`
    // TimeoutMillis bounds one invocation; zero is DefaultHookTimeout (30 s),
    // MaxHookTimeout is 60 s.
    TimeoutMillis int64 `json:"timeout_millis"`
}
```

`Definition.Hooks` is `null` when the agent declares none, in the canonical
encoding. `executor.bind` gains `hooks []string` and must cover every declared
hook, as `tools` must cover every declared tool.

**What the model learns.** A deny is the error the cell raises, with the
reason. A rewrite and a skipped optional hook append one bounded line to the
turn's ephemeral system text before the next provider request, for example
`Hook before_tool rewrote shell.run: {"command": "rm -r build"} (reason: ...)`,
so the model knows what ran without any operation changing its result shape.
Notices are capped at eight per turn and 2 KiB in total; later ones collapse
into a count.

**What the user sees.** One event kind, `stream.hook.decision`, with the hook
name, operation, decision (`deny`, `rewrite`, `skipped`), reason, and the
rewritten arguments summary. The TUI's REPL panel shows it beside the host
call; the web executions view lists it under the cell. An `allow` with no
changes emits nothing.

**What an author writes.**

```ts
const support = defineAgent({
  id: 'support-triage',
  modules: ['context', 'files', 'shell', 'state', 'user', 'agents'],
  capabilities: ['read', 'shell'],
  tools: [lookupTicket],
  hooks: {
    beforeTool: async ({ operation, arguments: args, agentId }) => {
      audit.log(agentId, operation, args);                       // observe: return nothing
      if (operation === 'shell.run' && /rm -rf/.test(String(args.command)))
        return { decision: 'deny', reason: 'destructive shell commands are not allowed' };
      if (operation === 'files.read' && String(args.path).endsWith('.env'))
        return { arguments: { ...args, path: `${args.path}.example` }, reason: 'secrets are redacted' };
    },
    beforeSpawn: async ({ spawn }) => {
      if (spawn.definition !== 'researcher') return { decision: 'deny', reason: 'only researcher children are allowed' };
    },
    turnStart: async ({ agentId }) => ({ context: `On call: ${await roster.current()}. Open incidents: ${await incidents.open()}.` }),
  },
});
const executor = await client.agents.serve(support); // registers, binds tools and hooks, serves both
```

## Steps

Each step is one commit and leaves `go test ./...`, `go vet ./...`,
`go run ./cmd/whipvet ./...`, `task contract`, and `task sdk` green.

### 1. Definition and protocol

Go:

- `agentdef`: `Hooks`, `Hook`, `Definition.Hooks *Hooks`, `DefaultHookTimeout`
  and `MaxHookTimeout`, `Hook.Timeout()`. `Validate` checks operation names
  against `rlm.Modules()` plus `tools.<name>` for declared tools, rejects
  duplicates, and bounds the timeout. `Normalize` keeps `hooks` `null` when
  absent and empty `operations` `null`. `Child` copies hooks unchanged
  (decision 4). `HookNames()` lists the declared hooks in canonical order.
- `protocol`: `ExecutorBindParams.Hooks []string`, `ExecutorBindResult.Hooks`;
  `HookInvokeParams{InvocationID, Definition, Revision, Generation, RootID,
  AgentID, TurnID, Hook, Operation, Arguments json.RawMessage, Input string,
  PermissionMode string, DeadlineMillis}`; `HookResultParams{InvocationID,
  Generation, Decision, Reason string, Arguments json.RawMessage, Context
  string, Error string}`; `ExecutorPendingResult.Hooks []HookInvokeParams`.
  Notifications `hook.invoke` and `hook.cancel` (reusing `ToolCancelParams`);
  RPC `hook.result` (ephemeral, `executor-lease`). `stream.hook.decision` in
  `EventPayloads`. Minor 6.4. Regenerate the TypeScript contract.
- `daemon/executor_rpc.go`: `executorTools` becomes `executorCoverage`,
  checking hooks as it checks tools; a definition with neither tools nor hooks
  still cannot bind.

Tests: canonical encoding with and without hooks; validation of operations,
duplicates, timeout ceiling; bind coverage for hooks; contract drift.

### 2. Executor registry serves hooks

Go:

- `daemon/executor.go`: `toolInvocation` generalizes to an invocation with a
  kind (`tool` or `hook`) and a settle shape. `InvokeHook(ctx,
  HookInvocation) (HookDecision, error)` mirrors `Invoke`: await lease, record
  pending, notify `hook.invoke`, settle on `hook.result`, deadline, turn
  cancellation, or disconnect. `settleHook` validates the reply: `Decision` is
  empty, `allow`, or `deny`; `Arguments` must be a JSON object when present;
  `Context` is bounded (4 KiB). An `Error` in the reply becomes a deny with
  that reason (decision 8). `pendingFor` lists hook invocations for the lease.
- The absence and timeout errors name the hook, the definition, the revision,
  and the fix, in the same shape as the tools error.

Tests: reply with no fields settles as allow unchanged; deny and rewrite
replies; error reply; duplicate and foreign replies rejected; deadline sends
`hook.cancel`; disconnect fails the pending hook; pending lists hooks after a
re-bind.

### 3. `before_tool` at the host boundary

Go:

- `daemon/recursive_runtime.go`: `recursiveHost.Call` consults
  `node.effectiveDefinition().Hooks.BeforeTool` after the accounting checks
  and before the module gate. When declared and the operation matches
  `Operations`, it marshals the arguments, calls the registry through the
  root's executor with the node's identity, turn id, and the session's
  permission mode, and applies the decision: deny returns
  `fmt.Errorf("hook before_tool denied %s: %s", op, reason)`; rewrite
  replaces the arguments map (decoded with `UseNumber`) and appends a notice;
  an unanswered required hook returns the registry's error; an unanswered or
  failing optional hook appends a skip notice and proceeds. Every deny,
  rewrite, and skip emits `stream.hook.decision` through `node.emit`.
- `tools.<name>` calls, `agents.spawn`, and every other module pass the same
  gate; the rewritten map continues into the existing handler, which validates
  it as it validates fresh input.
- Turn notices: `AgentSession` keeps a bounded per-turn notice list.
  `agent.Events` is passed to the loop by value, so a fixed
  `EphemeralSystem` string cannot change mid-turn; add
  `Events.EphemeralNotices func() string`, which the loop appends to the
  ephemeral system on every provider request (one line in `agent.turn` beside
  the existing `withEphemeralSystem` call). `RunTurn` points it at the
  session's notice list.

Tests: allow proceeds untouched; deny raises the reason in the cell and emits
the event; rewrite runs the rewritten arguments (a `files.read` path rewrite
observed at the host handler) and the next model request carries the notice;
rewrite to invalid arguments fails at the normal validator; `agents.spawn`
rewrite cannot widen (`capability ... is not available to the parent`);
`Operations` narrowing skips unlisted operations with no round trip; a child
is gated with its own agent id; required hook with no executor denies with the
expected error; optional hook with no executor proceeds and emits `skipped`;
handler error denies (required) or skips (optional); hook timeout denies and
sends `hook.cancel`; a definition without hooks makes no registry call.

### 4. `turn_start`

Go:

- `daemon/agent_session.go`: after the prompt snapshot is applied and before
  `agent.Run`, when `Hooks.TurnStart` is declared, call the registry with the
  turn id and a bounded preview of the input (first 2 KiB). A `Context` reply
  is appended to `events.EphemeralSystem`. Any failure or absence appends a
  skip notice and emits `stream.hook.decision` with `skipped`; the turn never
  waits past the hook's timeout and never fails because of it.

Tests: context appears in the first provider request and not in history;
missing executor skips with a notice; timeout skips; children get their own
turn hooks.

### 5. SDK

TypeScript (`packages/sdk/src/agents.ts`, `client.ts`):

- `defineAgent` accepts `hooks: { beforeTool?, beforeSpawn?, turnStart? }`,
  each a handler plus optional `{ operations, optional, timeoutMs }`.
  `beforeSpawn` is folded into the document's `before_tool` with
  `operations: ['agents.spawn']` added to any `beforeTool` narrowing (or the
  union when both are given), and its handler receives a typed `spawn` view of
  the arguments. `AgentDefinition` gains `hookHandlers`.
- Types: `BeforeToolEvent{invocationId, rootId, agentId, turnId, operation,
  arguments, permissionMode, deadline, signal}`, `BeforeToolResult{decision?,
  reason?, arguments?}`, `SpawnEvent{..., spawn: SpawnRequest}`,
  `TurnStartEvent{..., input}`, `TurnStartResult{context?}`. All result
  fields optional; `undefined`/`void` is allow.
- `serve` binds `hooks` alongside `tools`, routes `hook.invoke` to the
  handler, posts `hook.result` with whatever the handler returned (or
  `error` when it threw), honors `hook.cancel`, and drains pending hooks after
  a re-bind. `client.onNotification` accepts the new methods.
- Example: `ticket-lookup.ts` gains the three hooks above with a test that
  exercises deny, rewrite, observe, and the turn context through the fixture.

Tests: bind includes hooks; observe handler posts an empty result; deny and
rewrite results; thrown handler posts `error`; `beforeSpawn` is called only
for `agents.spawn` with a typed request; cancel aborts the signal; reconnect
re-binds and drains hook invocations.

### 6. Documentation

`docs/features.md` (definitions bullet), `docs/rlm-runtime.md` (a "Hooks"
section after "Custom tools"), `docs/protocol-v2.md` (6.4), the SDK README
(hooks in the agents section), and this plan's implementation record.

## Preserved, changed, not built

**Preserved**

- Every prompt golden. Hooks add nothing to the composed prompt; `turn_start`
  text is ephemeral.
- The capability ledger as the sole authority. Hooks never grant.
- Permission modes. A hook `allow` does not skip a prompt.
- Result shapes of every host operation. Rewrites are reported through
  notices and events, not by wrapping results.
- Definitions without hooks: no registry call, no round trip, no new events.

**Changed**

- `Definition` gains `hooks`; canonical encoding gains one nullable field.
- The executor lease covers hooks as well as tools; `executor.bind` requires
  both to be covered.
- Protocol minor 6.4; one new stream event kind.

**Not built**

- A separate `before_spawn` wire hook (folded into `before_tool`).
- Post-operation hooks (`after_tool`, `turn_end`). The event stream already
  carries completions; add a hook only when a consumer needs to modify a
  result.
- Hooks on the user's prompt (Claude's `UserPromptSubmit` block). The
  application owns the input path already.
- A hook `allow` that auto-approves a permission prompt.
- Per-hook matchers richer than exact operation names.
- Durable ledger rows for denials. The event journal records them; a denied
  call never reached the ledger.

## Risks and edge cases

- **Latency on every operation.** With no `Operations` narrowing, each
  `context.read` costs a round trip to the executor process. Locally that is
  a millisecond; over a network it is not. The definition narrows when it
  matters, and a definition without hooks pays nothing.
- **Rewrite surprises.** A model that asked for one thing and got another can
  loop. The per-request notice tells it what ran and why; the reason field is
  therefore worth writing well.
- **Fail closed makes hooked agents dependent on their process.** A
  required hook with no executor stops every gated operation. The error says
  which process to start; `optional` exists for advisory hooks. This is the
  deliberate difference from Claude Code, whose hooks cannot be absent.
- **`turn_start` cost.** A slow contribution delays the first model request by
  up to its timeout, every turn. Authors should cache; the default 30 seconds
  is a ceiling, not a target.
- **Children and named children.** A child inherits its parent's hooks and
  the same executor. A hook that must treat children differently reads
  `agentId` and the operation; there is no per-child hook declaration in this
  plan.
- **Notice bounds.** Eight notices and 2 KiB per turn keep a chatty hook from
  crowding the prompt; the event stream keeps the full record.
