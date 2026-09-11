# SDK ergonomics: typed tools, a runtime handle, and turns

Status: final plan, September 11, 2026. Ready to implement; no code yet.

Written against `feature/agent-definition` at `6fdd4b65d`, which landed
[TypeScript-authored agents](../typescript-agents/PLAN.md) and
[agent hooks](../agent-hooks/PLAN.md). The SDK can now author a definition,
serve its tools and hooks, and drive a session over the raw protocol. This plan
adds the layer an application developer actually writes against, built only
from those public primitives.

## Request and recorded decisions

Expose higher-level primitives over the existing SDK so that defining, running,
and observing an agent reads like the frameworks developers already know
(the Claude Agent SDK, Vercel AI SDK, OpenAI Agents JS, Mastra, effect-agent),
without changing what the daemon owns. Decisions recorded from the September
10 research conversation; each is a default the user accepted and can still
veto during review:

1. **Standard JSON Schema is the typed tool contract.** `tool()` accepts any
   schema implementing `~standard.jsonSchema` (Zod 4.2+, ArkType, Valibot via
   `@valibot/to-json-schema`) and infers the handler's input type from it.
   Raw JSON Schema stays accepted, typed `unknown`. The wire stays JSON Schema
   and the daemon stays the validator. The SDK takes no runtime dependency on
   any schema library; the interfaces are mirrored as types.
2. **No Effect.** Effect-agent's ideas transfer (a stable semantic event
   union, typed failure kinds, scope-owned cancellation, resource-free
   definitions); its runtime does not. Adopting it would put a release
   candidate peer dependency in front of browser, React, and React Native
   consumers.
3. **The new layer is composition only.** Everything it does, a caller can do
   with today's `client`, `session`, command handles, and subscriptions.
   Those remain public and unchanged; the state and React packages keep
   consuming raw events.
4. **`serve` returns the runtime you run with.** The executor and the
   sessions that depend on it live on one handle.
5. **A turn is the unit of observation.** `session.run(input)` returns one
   async iterable of semantic events scoped to that turn plus a typed result.
   Prompts (questions, permissions) arrive as events that carry their own
   reply methods.
6. **Structured output is daemon work and comes last.** An optional `output`
   schema on the definition, enforced by the daemon, makes the turn result
   typed. Everything before it is SDK-only.
7. **Testing ships with the SDK.** A `@whip/sdk/testing` entry exports the
   scripted transport fixture and the live daemon fixture.
8. **Sequence by leverage:** typed tools, then the runtime handle and turns,
   then prompt helpers, then the testing entry, then output contracts.
9. **`tool()` takes one options object** (`name`, `description`, `input`,
   `output`, `execute`, `timeoutMs`), replacing the positional form. This
   matches `defineAgent` and `hooks` in our own SDK and the `tool({...})` of
   Vercel, OpenAI, and Mastra; the package is private and pre-1.0, so the
   positional form is removed rather than kept as an overload. (September
   11.)
10. **Tools declare an output schema as well as an input schema.** In phase
    1 it types the handler's return value at compile time and validates the
    result locally before it is posted; in phase 6 it travels on the wire as
    `output_schema`, the runtime guide shows the model what a tool returns,
    and the daemon validates results from any executor. (September 11.)
11. **Field names are `input`, `output`, and `execute`.** They mirror
    `defineAgent({ output })` in the same SDK and read cleanly in the object
    form; `execute` is what Vercel, OpenAI, and Mastra call the handler. The
    longer `inputSchema`/`outputSchema` was considered and passed over as a
    rename with no other consequence. (September 11.)
12. **Derived schemas target JSON Schema draft 2020-12.** It is the Standard
    JSON Schema spec's first-class target, every implementer supports it, and
    the daemon's validator (`google/jsonschema-go`) accepts both 2020-12 and
    draft-07, so raw JSON Schema written by hand in either draft still
    works. The protocol contract's own draft-07 is a TypeScript-generation
    choice and is unaffected. (September 11.)

No open questions remain; every default above was accepted in review.

## Why this matters

The incident commander acceptance in `examples/agents` is the honest picture
of authoring today. Three costs show in it:

- **Tools are typed by assertion.** `tool('search_incidents', ..., { type:
  'object', properties: {...} }, async ({ query, status }: { query: string;
  status?: ... }) => ...)` writes the shape twice, once as JSON Schema and once
  as a TypeScript annotation, and nothing checks that they agree. Every
  framework surveyed infers the handler type from the schema.
- **Running one turn is five steps of plumbing.** Connect, `agents.serve`,
  `sessions.create(...).result()`, `client.session(rootId)`, `submit(...)
  .result()`, and a separate `snapshot()` plus `events.subscribe(rootId,
  cursor)` to watch it, with the consumer filtering `stream.hook.decision`
  events by hand. The typescript-agents plan's own target-state sketch
  wished for `runtime.sessions.create(...)`.
- **Events are wire-shaped.** A subscription yields every kind for every agent
  and turn: `stream.text`, `stream.cell.host`, `question.pending`,
  `permission.pending`, `agent.*`. Answering a question is a separate
  command the consumer must correlate by id. The result of a turn is the
  command envelope's `result.text`, never an author-declared shape.

None of these are runtime gaps. They are the absence of a layer that the
protocol already supports, which is why the first five phases change no Go.

## What the survey showed, and what we take

Across the Claude Agent SDK, Vercel AI SDK v7, OpenAI Agents JS, Mastra, and
effect-agent:

- **Schema-inferred tool inputs**, converging on Standard JSON Schema as the
  library-neutral contract. The MCP TypeScript SDK v2 switched to it and
  dropped its zod dependency. We do the same (decision 1).
- **A second handler argument carrying execution context**: abort signal, call
  id, identity. Our `ToolContext` already has this; unchanged.
- **An agent object defined once and run many times.** Ours is `defineAgent`
  plus `serve`; the missing piece is the handle that owns sessions (decision
  4).
- **One async iterable of discriminated events** beside a text-only
  convenience, and **one result object** with typed output and usage
  (decision 5).
- **Approval as a first-class run event with a resume call**, not a side
  channel. Our questions and permissions become events with reply methods.
- **Scripted models for tests** (Vercel `MockLanguageModelV4`, OpenAI
  `ScriptedModel`, effect-agent `ScriptedModel`). Ours is the daemon fixture's
  scripted model, already used by the acceptance run; the SDK entry exposes
  it (decision 7).

Claude's `annotations` (`readOnlyHint`, `destructiveHint`) are worth adopting
once a consumer exists: `before_tool` hooks and permission modes could read
them. `searchHint` and `alwaysLoad` solve tool-search deferral, which the
universal RLM interface does not have.

## Target state

What an author writes after this plan, end to end:

```ts
import { createWhipClient } from '@whip/sdk';
import { defineAgent, tool } from '@whip/sdk/agents';
import { z } from 'zod';

const lookupTicket = tool({
  name: 'lookup_ticket',
  description: 'Fetch a ticket by id',
  input: z.object({ id: z.string() }),
  output: z.object({ id: z.string(), title: z.string(), status: z.enum(['open', 'closed']) }),
  execute: async ({ id }, ctx) => tickets.get(id),   // id: string; the return type must match output
  timeoutMs: 30_000,
});

const support = defineAgent({
  id: 'support-triage',
  instructions: { persona: 'You triage support tickets.' },
  modules: ['context', 'files', 'user'],
  tools: [lookupTicket],
  output: z.object({ summary: z.string(), owner: z.string() }),   // phase 6
});

const client = createWhipClient({ endpoint, clientId: 'support' });
await client.connect();
const runtime = await client.agents.serve(support);        // register, bind, serve
const session = await runtime.sessions.create({ cwd });   // a Session, pinned to this revision

const turn = session.run('Triage ticket 42');
for await (const event of turn) {
  switch (event.type) {
    case 'text': process.stdout.write(event.delta); break;
    case 'question': await event.answer([event.options[0].label]); break;
    case 'permission': await event.allow(); break;
    case 'hook': console.log(event.decision, event.operation); break;
  }
}
const result = await turn.result();   // { status, text, output?, usage, failure? }
runtime.close();
```

**Layering rule.** `WhipClient`, `Session`, `CommandHandle`, and
`Subscription` stay the raw layer, the way `query()` sits beneath the Claude
SDK's conveniences. The new layer imports only their public methods. Anything
it computes (turn scoping, text accumulation, prompt correlation) a user could
compute from the same events.

## Phases

Each step is one commit and leaves `go test ./...`, `go vet ./...`,
`go run ./cmd/whipvet ./...`, `task contract`, and `task sdk` green. Phases 1
through 5 touch only `packages/sdk`, `examples`, and docs.

### Phase 1: typed tools

`packages/sdk/src/agents.ts`, new `packages/sdk/src/schema.ts`.

- Mirror the Standard Schema interfaces as types (`StandardSchemaV1`,
  `StandardJSONSchemaV1`, `StandardSchemaWithJSON`), as the MCP SDK does, so
  the package adds no dependency. A `Schema` is either one of those or a raw
  JSON Schema object; `InferInput<S>` and `InferOutput<S>` resolve to the
  library's types for the former and `unknown` for the latter.
- `tool()` becomes object-form:

  ```ts
  tool<I extends Schema, O extends Schema | undefined = undefined>(options: {
    name: string;
    description: string;
    input: I;                                   // object root; keyword arguments
    output?: O;                                 // any JSON Schema root
    execute: (input: InferInput<I>, context: ToolContext) =>
      MaybePromise<O extends Schema ? InferOutput<O> : unknown>;
    timeoutMs?: number;                         // default 5 minutes, ceiling 15
    validate?: boolean;                         // local validation, default true
  }): ToolDefinition<I, O>
  ```

  The positional form is removed. `ToolDefinition` keeps `spec` for the
  document and gains `execute` plus the two schemas for local validation;
  `AgentDefinition.handlers` keys stay tool names.
- The document's `input_schema` is `input['~standard'].jsonSchema.input({
  target: 'draft-2020-12' })`, compacted, or the raw object as given.
  `output` is kept SDK-side in this phase (it reaches the wire in phase 6),
  so the canonical document does not change and no revision moves. A
  library that throws for the 2020-12 target is a definition-time
  `TypeError` naming the tool; there is no silent fallback to another draft,
  because the document's bytes are its revision.
- `tool()` rejects at definition time, naming the tool: an `input` whose
  derived root is not an object (the daemon requires keyword arguments), a
  schema that cannot produce JSON Schema, and a bad `timeoutMs`.
- Local validation in `serve`, on by default: `input['~standard'].validate`
  runs before `execute` and `output['~standard'].validate` runs on its
  return value; either failure posts a `tool.result` error naming the tool
  and the issue, and the handler's side effects for an output failure have
  already happened, which the error text says. The daemon has validated the
  input against the same JSON Schema, so input validation only adds
  library refinements the schema cannot express; output validation is the
  only check until phase 6. Raw JSON Schema tools have no `~standard` and
  skip local validation.
- `examples/agents` gains `zod` as a dev dependency; the ticket-lookup and
  incident commander tools move to the object form with inferred inputs and
  declared outputs. JuniorDeveloper has no tools and is unaffected.

Tests: a hand-rolled Standard Schema object (no library) infers the handler
input and constrains its return type (compile-time assertions through
`satisfies` and `@ts-expect-error`); the derived document `input_schema` is
compacted 2020-12 JSON Schema; a non-object input root is rejected; raw JSON Schema
still works and types `unknown`; the incident commander's document is
byte-identical before and after the change (pin the derived `input_schema`);
input validation failure posts an error and never calls `execute`; output
validation failure posts an error after `execute` ran; `validate: false`
skips both.

### Phase 2: the runtime handle

`packages/sdk/src/agents.ts`.

- `serve` returns `AgentRuntime`, which extends today's `Executor` with
  `sessions.create(params)` and `sessions.open(rootId)`. `create` fills
  `definition` from the served agent, awaits the command's terminal outcome,
  throws a `WhipError('execution_failed', ...)` carrying the daemon failure
  when creation fails, and returns a `Session`. `open` verifies through
  `sessions.get` that the session pins this definition and revision and
  throws otherwise, so a runtime never drives a session whose tools it does
  not serve.
- `Executor` remains as a type alias for `AgentRuntime` for one release.
- `runtime.close()` keeps its meaning: stop serving. Sessions outlive it, as
  they outlive the client.

Tests: `create` sends `definition` and resolves a `Session` for the returned
root; a failed creation rejects with the failure message; `open` rejects a
session pinned to another revision; the fixture round trip from
`agents.test.ts` gains the runtime step.

### Phase 3: turns

New `packages/sdk/src/turn.ts`; `Session.run` in `session.ts`.

- `session.run(input: string | SubmitPayload, options?: { signal?,
  includeChildren?: boolean }): Turn`. `Turn` is `AsyncIterable<TurnEvent>`
  with `result(): Promise<TurnResult>`, `text(): Promise<string>`,
  `cancel(): Promise<void>`, and `readonly turnId: Promise<string>`.
- Ordering: `snapshot()` for the cursor, `events.subscribe(rootId, cursor)`,
  then `submit`. The subscription starts before the command so no event is
  missed. The turn id is the first `turn.started` for the root agent at or
  after the command's acceptance; every later event is kept when its
  `turn_id` matches, or when it has no turn id and its `agent_id` is the root
  (mailbox and lifecycle events that belong to the turn window).
  `includeChildren` also keeps events from agents spawned during the turn,
  tagged with `agentId`.
- `TurnEvent` is a discriminated union derived from existing kinds:

  | type | from |
  |---|---|
  | `text` (`delta`) | `stream.text` |
  | `reasoning` (`delta`) | `stream.reasoning` |
  | `cell` (`id`, `code`, status) | `stream.tool.call/started/completed` where `name === 'rlm_exec'` |
  | `host` (`operation` from `name`, `summary` from `args`, `status`: running, completed, failed, cancelled; `error` from `result`) | `stream.cell.host.started`, `stream.cell.host` |
  | `progress` (`operation`, `text`) | `stream.tool.progress` |
  | `hook` (`hook` from `name`, `operation` from `args`, `decision` from `text`: deny, rewrite, skipped; `reason` from `result`) | `stream.hook.decision` |
  | `question` (`id`, `question`, `options: {label, description?, recommended?}[]`, `multiple`, `questions?` for a batch, `answer()`, `dismiss()`) | `question.pending` |
  | `permission` (`id`, `operation`, `command`, `allow(remember?)`, `deny(reason?)`) | `permission.pending` |
  | `child` (`agentId`, `name`, status) | `agent.admitted`, `agent.turn.*`, `agent.subtree.*` |
  | `notice` (`text`) | `stream.notice` |
  | `usage` | `stream.usage` |
  | `end` (`status`) | `turn.succeeded/failed/cancelled/interrupted` for the root |
  | `raw` (`event`) | every other kind in the window, so nothing is hidden |

  Unknown future kinds surface as `raw`. Each mapped type is a plain object
  with `seq`, `agentId`, and `turnId`.
- `TurnResult<Output = unknown>` is a discriminated union on the command
  status, so a `succeeded` branch narrows `output` to present:

  ```ts
  type TurnResult<Output = unknown> =
    | { status: 'succeeded'; turnId: string; text: string; output: Output; usage?: Usage }
    | { status: 'failed' | 'cancelled' | 'interrupted'; turnId?: string; text?: string; failure: Failure; usage?: Usage };
  ```

  `text` comes from the submit command's terminal outcome (`result.text`),
  `usage` is the last `usage` event, `failure` is the command's failure
  envelope, and `output` is `unknown` until phase 6 types it (a session
  opened raw keeps `unknown` afterwards). `text()` resolves to the same
  `text` or rejects with the failure. `result()` resolves when both the command is terminal and
  the `end` event has been seen, or the command is terminal and the
  subscription has drained to the command's `ingress_seq`, whichever first
  completes; it never waits on events the command's failure made impossible.
- `cancel()` sends the existing command-targeted cancel and resolves on its
  acceptance. The `signal` option cancels the turn on abort and disposes the
  subscription.
- Backpressure and disconnects follow `Subscription`: a consumer that falls
  behind fails the iterable with `resynchronization_required`; `result()`
  still resolves from the command's terminal status.

Tests (fixture-driven, in `packages/sdk/test/turn.test.ts`): text deltas are
yielded in order and `text()` equals the outcome; the subscription is
requested before the submit; events from another turn and another root are
excluded; `includeChildren` includes a spawned child's events; `end` closes
the iterable and `result()` carries status, usage, and failure; `cancel()`
sends the targeted cancel; an aborted signal disposes the subscription; a
buffer overflow fails the iterable but not `result()`; unknown kinds arrive
as `raw`.

### Phase 4: prompt helpers

`turn.ts`, `session.ts`.

- `question.answer(answers)` posts `question.answer` with the event's id and
  `dismiss()` posts the dismissed form; the batch shape (`questions`) maps to
  `answers: [...]` when present. `permission.allow({ remember })` and
  `deny(reason)` post `permission.decide` with the event's `permission_id`
  and a fresh command id. Each method is idempotent per event: a second call
  returns the first promise.
- `session.prompts()` returns the open prompts from the snapshot (`questions`
  and `permissions`) as the same `question` and `permission` event objects,
  so a client that attached mid-turn can answer without a live event.
- A `PromptRequiredError` is not introduced: an unanswered prompt simply
  keeps the turn running, as today.

Tests: `answer` and `allow` post the exact params with the event's ids;
repeated calls post once; batch questions map to `answers`; `prompts()`
reads the snapshot and its methods post the same params.

### Phase 5: the testing entry

New `packages/sdk/src/testing.ts`, exported as `@whip/sdk/testing`.

- `scriptedDaemon(options)`: today's `transportFixture` from
  `packages/sdk/test/transport-fixture.ts`, moved into `src` and given a
  small script table: `reply(method, handler)`, `emit(rootId, kind,
  payload)` to push events into subscriptions, and `turn(rootId, script)` to
  play a whole turn (started, text deltas, optional question, end) so a
  consumer of `session.run` can be tested without a daemon.
- `liveDaemon(options)`: `startFixture` and `eventually` from
  `packages/sdk/scripts/fixture.mjs`, typed and exported for Node consumers
  who want the real recursive runtime behind the scripted model, as the
  incident commander acceptance does. Node-only; documented as such.
- The SDK's own tests switch to the exported fixture; `transport-fixture.ts`
  becomes a re-export.

Tests: the existing suites pass through the moved fixture; a `turn(...)`
script drives `session.run` end to end in a unit test.

### Phase 6: output contracts

Go and protocol; the one runtime change. Two contracts land together under
one protocol minor: what a tool returns, and what a turn returns.

- `agentdef.Tool.OutputSchema json.RawMessage` (`null` when absent; any JSON
  Schema root when present, compacted by `Normalize`). `RuntimeGuide`
  appends a bounded `returns {...}` fragment to the tool's catalog line,
  built from the schema's top-level `type` and property names. The daemon
  validates every `tool.result.output` against it with `jsonschema-go`
  before the value reaches the cell; a mismatch settles the call as an error
  naming the tool and the violation, the same shape as an invalid input.
  `defineAgent` puts the derived schema on the wire; a definition without
  outputs encodes `"output_schema": null` and its revision is unchanged
  from phase 1 because phase 1 never sent the field. (Registered documents
  from before this phase decode with the field absent, which `Decode`
  treats as null.)
- `agentdef.Definition.Output json.RawMessage` (`null` when absent; an
  object schema when present, validated like a tool schema). `Child` may
  override it (`Output *json.RawMessage`); `Definition.Child` applies it.
- Prompt: when `Output` is set, `RuntimeGuide` appends one bounded rule line
  telling the model its final assistant message must be exactly one JSON
  value matching the schema, followed by the compacted schema. Built-ins have
  no output schema, so every prompt golden is unchanged.
- Daemon: at the end of a root or child turn whose definition has `Output`,
  `AgentSession` parses the final assistant text as JSON and validates it
  with `jsonschema-go`. On failure it appends one ephemeral notice with the
  validation error and runs one more model round (the same `EphemeralNotices`
  channel hooks use); a second failure fails the turn with
  `output_invalid` and the error text. The validated value is stored on the
  turn outcome (`TurnOutcome.Output`) and returned in the submit command's
  result beside `text` (`TextResult.Output json.RawMessage`).
- Protocol minor 6.5; regenerate the contract.
- SDK: `defineAgent` accepts `output` (Standard JSON Schema or raw), infers
  `Output`, and `AgentDefinition<Output>` carries it; `Turn.result()` from a
  session created through `runtime.sessions.create` is typed
  `TurnResult<Output>` with `output: Output`. A session opened raw keeps
  `output?: unknown`.

Tests: agentdef validation and canonical encoding for both fields; the tool
catalog line renders `returns` with and without a schema (goldens unchanged
for built-ins); a `tool.result` that violates `output_schema` settles as an
error and the cell sees it; a definition with output accepts a valid final
message, retries once on an invalid one, and fails with `output_invalid` on
the second; the submit result carries `output`; the SDK types infer through
`defineAgent` and `run`; the incident commander's summary tool becomes an
`output` schema in a follow-up example.

### Phase 7: documentation and examples

- README: the "Agent definitions and custom tools" section becomes "Author,
  serve, run", leading with the target-state program, then the raw layer
  beneath it. A short "Testing" section for `@whip/sdk/testing`.
- `docs/features.md` SDK bullets; `docs/rlm-runtime.md` gains an "Output
  contract" paragraph beside the hooks section; `docs/protocol-v2.md` 6.5.
- `examples/agents/support-triage.ts` replaces `ticket-lookup.ts`. It is
  the README program verbatim (two typed tools with outputs, one named
  child, all three hooks, an agent `output`), with a unit test that pins its
  document and drives one `session.run` loop through the scripted daemon
  from `@whip/sdk/testing`, and a live acceptance that runs one real turn
  and reads `result.output`. See "The support-triage fixture" below.
- `examples/agents/incident-commander.ts` and its acceptance move to typed
  tools, `runtime.sessions.create`, and `session.run`; the acceptance's
  hand-rolled event filtering is replaced by turn events, which is the proof
  that the layer covers a real consumer.
- This plan's implementation record.

## The support-triage fixture

The typescript-agents plan proved its abstraction with JuniorDeveloper: a
document written two ways that had to compose the same prompt. This plan's
proof is a program written once that has to work at every layer. The
support-triage example is the target-state program from the top of this
document, and it is exercised three ways:

- **At compile time.** `tsc` on the example is the type test: `execute`
  receives inferred inputs, a return that does not match `output` is a
  compile error (asserted with `@ts-expect-error` in the test), and
  `turn.result()` narrows `output` on `status === 'succeeded'`.
- **Against the scripted daemon.** The unit test pins the canonical document
  (so a schema library upgrade that changes derived JSON Schema shows as a
  diff, not a surprise revision), then plays a `turn(...)` script through
  `session.run`: text deltas, one hook decision, one question answered from
  the event, and the `end`, asserting the loop the README shows.
- **Against a live daemon.** The acceptance serves the agent with the SDK
  fixture's scripted model, creates a session through the runtime, runs one
  turn that calls `tools.lookup_ticket`, and asserts the typed `output` and
  the hook decision events. From phase 6 it also asserts that a malformed
  tool result is rejected by the daemon.

If those three hold, the layer is complete for a real consumer, and the
incident commander migration is a second consumer rather than the only one.

## Preserved, changed, not built

**Preserved**

- Every public export of `@whip/sdk`, `/node`, `/state`, `/react`, and
  `/agents` except the positional `tool()`; `Executor` as an alias.
- The wire: JSON Schema for tool inputs, the existing event kinds, the
  command lifecycle. Phases 1 through 5 change no protocol.
- Every prompt golden.
- The state and React views, which keep consuming raw events.
- Zero runtime dependencies in `@whip/sdk` beyond `@whip/protocol`.

**Changed**

- `tool()` takes one options object with `input`, `output`, and `execute`
  and infers both types; the positional form is removed. `serve` returns a
  runtime handle; `Session` gains `run` and `prompts`; a `testing` entry
  appears.
- Tools gain `output_schema` and definitions gain `output`; the guide shows
  tool return shapes; the daemon validates tool results and final messages;
  the turn outcome and submit result carry the validated value; protocol
  minor 6.5.
- `examples/agents/ticket-lookup.ts` is replaced by `support-triage.ts`.

**Not built**

- Effect, or any framework-owned effect system.
- Tool `annotations`, `searchHint`, `alwaysLoad`: no consumer yet; hooks and
  permission modes are where annotations would first matter.
- Handoffs or agent-as-tool primitives: `agents.spawn` with named children
  already covers delegation, and the model drives it from the cell.
- Client-managed message arrays: sessions are the durable conversation; the
  SDK does not grow a stateless `messages` mode.
- A model middleware layer: model routing belongs to the daemon's provider
  configuration.
- Streaming partial structured output: the output contract is validated at
  turn end; partial JSON is never presented as an answer.

## Risks and edge cases

- **Two schema libraries in one process.** Standard JSON Schema output
  differs slightly between libraries (`additionalProperties`, `$schema`
  headers). The document pins the derived schema, so a library upgrade that
  changes output changes the definition's revision. That is correct and
  visible, but worth saying in the README.
- **Turn windows are heuristics.** Events without a turn id that belong to a
  concurrently running child could be misattributed; the `raw` fallback and
  the explicit `includeChildren` flag keep the default view narrow. The
  state view remains the complete picture.
- **`result()` after disconnect.** Command outcomes survive reconnects
  through `CommandHandle`; the event iterable does not. The contract is
  explicit: the result is authoritative, the stream is best effort.
- **Local validation drift.** A library refinement the JSON Schema cannot
  express fails locally after the daemon accepted the call; the model sees a
  tool error with the refinement message, which is the same outcome a
  handler `throw` produces today.
- **Output validation after side effects.** A handler that wrote to a
  database and then returned a malformed value has already had its effect
  when the result is rejected. The error text says so, and the invocation id
  in `ToolContext` is what makes a retry idempotent; this is the same
  uncertain-effect rule the ledger already applies.
- **Two validations of one result.** From phase 6 the SDK and the daemon
  both check tool output. They check the same JSON Schema, so they agree;
  the local check exists for refinements and for a faster error, and
  `validate: false` removes it when the daemon's check is enough.
- **Output retries cost a model round.** One retry is bounded and visible in
  the ledger and stream; a definition that wants none can be added later
  with an `output.retries` field if a consumer asks.

## Implementation record (September 11, 2026)

Commits on `codex/mobile-ui` (which carries `feature/agent-definition`), in
step order: typed tools over Standard JSON Schema; the `AgentRuntime` handle;
`session.run` turns; prompt reply methods and `session.prompts()`; the
`@whip/sdk/testing` entries; output contracts for tools and turns (protocol
6.6); documentation and the support-triage fixture. `go test ./...`,
`go vet ./...`, the repository analyzer, `task contract`, and `task sdk` were
green at every step, and `npm run acceptance` ran the live daemon fixture
through the ported testing entry after phases 5, 6, and 7. One unrelated
package, `internal/terminal`, showed a timing flake during the phase 6 run
and passed three of three on rerun.

Deviations from the plan as written:

- **Protocol minor is 6.6, not 6.5.** Workspace terminals took 6.5 on the
  branch while this plan was being written.
- **`sessions.open` and `create` verify the pin through `root.snapshot`.**
  `sessions.get` returns metadata without the definition fields, so the
  runtime reads the snapshot's `meta.definition` and `meta.definition_revision`
  instead.
- **A child's `output` override is a plain schema.** The plan's
  `*json.RawMessage` generated a `null`-only TypeScript type; `json.RawMessage`
  with null meaning inherit generates `unknown` and keeps the same semantics.
- **The turn id is matched by inbox sequence.** A submit command's
  `ingress_seq` is the inbox sequence its `turn.started` carries, so the turn
  is identified exactly rather than as the next root turn after acceptance;
  the next-turn rule remains the fallback when acceptance is uncertain.
- **`result()` tears down observation once the command settles.** The first
  implementation kept the subscription open when a command failed before its
  turn started, and the pump waited for the heartbeat to end it; settling now
  disposes the subscription and rejects `turnId`.
- **Child events are always surfaced; child streams are opt-in.** Admissions
  and child turn lifecycle events belong to the parent's story and are yielded
  as `child` events without `includeChildren`; the flag adds the child's own
  text, host, and prompt events.
- **The scripted daemon names turns by count, not ingress.** `question.answer`
  and other commands also take ingress sequences, so `root-turn-N` follows the
  number of turns played on that root.
- **`TurnOutcome.Output` was not added.** Turn outcomes are rebuilt from
  lifecycle events whose payload has no output field; the submit result
  carries the validated value, which is what the SDK reads.
- **`@whip/sdk/testing/node` is its own subpath.** The live fixture imports
  Node modules, so it is separated from the browser-safe scripted daemon
  rather than exported from the same entry; `scripts/fixture.mjs` re-exports
  it so every existing script keeps its import.
- **Derived schemas carry `$schema`.** Zod emits the 2020-12 `$schema`
  header, so the incident commander's document changed once when its tools
  moved to zod, and its test pins the derived schema rather than the earlier
  hand-written one.
- **The fixture model learned a `final` block.** A `` ```final `` fence in an
  acceptance prompt is streamed verbatim once the cell has run, so the
  support-triage acceptance exercises the output contract end to end; a
  malformed tool result is asserted in the daemon test rather than live.
- **A retry after a correction records both assistant messages.** The
  corrective round appends to history like any other round; the plan implied
  the invalid message would be replaced.
