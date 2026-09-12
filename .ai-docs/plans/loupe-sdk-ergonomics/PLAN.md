# Loupe on the SDK's author, serve, run layer

Status: research and proposed plan, September 12, 2026. Open questions at the
end; no Loupe code written for this yet.

Follows [Loupe on the Whip TypeScript SDK](../loupe-whip-sdk/PLAN.md), whose
eight steps landed on Loupe's `feature/whip-sdk`, and consumes the
[SDK ergonomics](../sdk-ergonomics/PLAN.md) work, which landed on
`codex/mobile-ui` at `8e577056e` (protocol 6.6). Loupe's vendored SDK is
still `feature/agent-definition@e6afc468b`, from before both.

## Why

Loupe built its own copies of three things the SDK now provides, and each copy
is where the live runs showed the most friction:

1. **A hand-rolled turn.** `runAgent` in `packages/whip/src/client.ts` does
   register or serve, `sessions.create(...).result()`, `configure`,
   `snapshot`, `events.subscribe`, `submit(...).result()`, and a `RunProgress`
   class that pattern-matches wire event kinds. `session.run()` returns the
   same turn as typed events plus a typed result, and `runtime.sessions.create`
   refuses a session pinned to a revision this process does not serve.
2. **A tool contract typed by assertion.** The `pr_*` and `submit_review`
   tools write JSON Schema by hand and cast `input: unknown`. `tool({ input,
   output, execute })` infers the argument type from a Standard JSON Schema
   and, new for the model, puts each tool's return shape in the runtime
   guide. Two of the live runs wasted cells on exactly that: the model treated
   a `files.read` handle as a string because nothing told it the return shape.
3. **A review delivered by tool call because the reply could not carry it.**
   The daemon's `max_turns` ends a turn with a no-tools answer, so
   `submit_review` needed a follow-up nudge turn whenever the model spent its
   budget exploring. An output contract on the definition makes the final
   message the deliverable: the daemon validates it against the review
   schema, corrects it once, and returns the parsed value as `result.output`.
   The forced final answer is then the review, not a lost turn.

Refreshing `vendor/@whip` is not optional: the positional `tool()` Loupe calls
was removed, so the first step is a compile fix either way. The question the
rest of this plan answers is how far to go beyond that.

## What the new layer gives Loupe, and what it does not

| SDK primitive | Loupe today | After |
|---|---|---|
| `tool({...})` with Standard JSON Schema | five tools with hand-written schemas and `input: unknown` | zod 4 inputs and outputs; `execute` arguments typed; return shapes shown to the model |
| `serve → AgentRuntime.sessions.create` | `agents.serve` or `register`, then `client.sessions.create`, then a snapshot | one call that also verifies the pin; the `register` path stays for the hook-less chat agent, which `serve` refuses |
| `session.run → Turn` | `snapshot` + `subscribe` + `submit` + `RunProgress` over wire kinds | typed events (`text`, `cell`, `host`, `hook`, `child`, `usage`, `end`, `raw`) and `result()` with `text`, `output`, `usage`, `failure`; `includeChildren` surfaces confirmers' own events, including the `end` error that explained nothing in the six-cell panel run |
| `defineAgent({ output })` | `submit_review` / `submit_verdicts` tools, last submission wins, one nudge turn | the final message is the review; daemon validation with one correction; typed `result.output` |
| `@whip/sdk/testing` `scriptedDaemon` | no test covers `runAgent` | a unit test drives register, serve, create, configure, run, and the nudge path without a daemon |
| prompt events (`question`, `permission`) | not applicable | not applicable: Loupe runs in automatic mode and no agent uses `user.ask` |

Two things the layer does not change:

- **A `Turn` is one turn.** The panel flow spans several: the reviewer spawns
  confirmers and ends its turn, their replies wake it, and a later turn
  submits. Loupe's `settle()` wait on `active_turns` stays for as long as the
  panel is model-driven.
- **Sessions still resolve an id to its latest-created revision.** The
  runtime now throws `conflict` when the pin differs instead of silently
  running the wrong agent. Loupe's content-hashed ids keep that from ever
  firing and stay.

## The one design decision: where the review comes from

An output contract validates the final message of **every** root turn whose
definition has `output`. A reviewer that spawns confirmers and ends its turn
with "waiting for the panel" fails validation, gets one correction, and fails
the turn on the second. So the output contract and a model-driven,
multi-turn panel cannot share a definition. Three ways out:

- **A. Keep `submit_review` for reviewers.** Use output contracts only where a
  turn is the whole job: the verifier (`{ verdicts }`) and, later, the fixer.
  Reviewers keep the tool, the anchoring feedback, and the nudge. Smallest
  change; the tool-call-under-cap problem stays.
- **B. Output contract everywhere, panel driven by Loupe.** The reviewer's
  output gains `suspected: [{ path, line, claim }]`. When it is non-empty,
  Loupe runs the confirmers itself as one-turn sessions on a confirmer
  definition (its own output contract: `{ verdict, evidence }`), in parallel,
  then runs a second turn on the reviewer session with the verdicts and takes
  the final review from that turn's output. Every turn is single-shot, so
  `settle()` and the nudge go, confirmers run in parallel instead of at the
  model's pace, and their failures are ordinary turn failures Loupe logs.
  This is the same shape verification and ensemble already have.
- **C. Output contract with a synchronous panel.** The reviewer waits for
  confirmers inside cells with `agents.wait`. Each wait is a cell against the
  budget and is bounded to seconds, while confirmers ran for minutes in the
  live run. Not recommended.

Recommendation: **B**. It aligns with the earlier decision to keep
deterministic orchestration in Loupe, and it deletes the two mechanisms the
first migration had to invent (the settle wait and the nudge). The anchoring
feedback loop is replaced by the daemon's own correction round plus Loupe's
existing post-hoc snapping; if line accuracy drops in dry runs, a
`check_anchors(findings)` tool the model may call before finishing restores
it without changing the contract.

## Steps

Each step lands green on `task check` in Loupe and, where it changes a run,
is dry-run against `context-labs/loupe#15` through the installed daemon.

### 1. Refresh the vendored SDK and fix the compile

- `task vendor:whip WHIP=../whip` from a checkout at `8e577056e` or later;
  `vendor/@whip/VERSION` records it; protocol 6.6.
- zod `3.25.76` → `4.x` in the root and `@loupe/core`. Loupe's own schemas
  use `z.object/string/array/number/boolean/enum/coerce/unknown`, `.default`,
  `.transform`, `.refine`, `.parse`, `.safeParse`: all present in zod 4.
  Run the tests; fix any message-option renames.
- `submit.ts`, `prtools.ts`, `vendor.test.ts`: object-form `tool({...})`;
  `handler` → `execute` in the two test helpers. Inputs become zod objects
  so `execute` is typed; outputs declared for the `pr_*` tools so the guide
  shows the model what comes back.
- Pin the derived `input_schema`/`output_schema` of each Loupe tool in a test,
  the way `support-triage.test.ts` does, so a zod upgrade that changes JSON
  Schema output shows as a diff rather than a surprise revision.

### 2. `runAgent` on the runtime handle and `session.run`

- `client.agents.serve(agent)` → `runtime`; `runtime.sessions.create({ cwd,
  model, permission_mode: 'automatic' })`; `register` + `client.sessions
  .create` only for an agent with no tools or hooks (chat).
- `session.configure` unchanged. `session.run(prompt, { includeChildren:
  true })`; `RunProgress` becomes a `TurnEvent` → log mapping: `text` and
  `reasoning` deltas throttled as now, `cell` and `host` one line each with
  status and error, `hook` decisions, `child` lifecycle, children's own
  `end` errors, `raw` accounting for the cost line.
- `turn.result()` supplies `text`, `usage`, and `failure`; the `failed`
  branch's message replaces today's outcome-envelope parsing.
- `settle()` and the nudge stay in this step; step 4 removes them.
- Test: `scriptedDaemon().serveSessions()` drives `runAgent` end to end,
  including a scripted turn that ends without a submission and the nudge
  that follows. This is the first test Loupe has of its runner.

### 3. Output contract for the verifier

- `verifierAgent` declares `output: z.object({ verdicts: z.array(...) })`;
  `submit_verdicts` is deleted; `verifyInline` reads `result.output`.
  Fail-open behavior is unchanged: any failure keeps all findings.
- Dry run: a PR with findings, verification on.

### 4. Output contract for reviewers and the Loupe-driven panel (decision B)

- `reviewerAgent` declares `output` = the review schema plus `suspected`;
  `submit_review` is deleted; `OUTPUT_CONTRACT` in `prompt.ts` describes the
  final message instead of a tool call; `runReview` reads `result.output`,
  normalizes, snaps, filters by profile as now.
- New `confirmerAgent(model)`: persona from today's confirmer, `pr_hunks` and
  `pr_context`, no `agents` module, `output: { verdict: 'confirmed' |
  'refuted', evidence }`. Loupe runs one session per panel model per
  suspected issue, in parallel, bounded by the existing `MAX_PANEL`; a
  finding is kept when a majority confirms, mirroring `mergeEnsemble`'s rule.
- The reviewer session gets a second `run` with the verdicts and returns the
  final review as `output`; the first turn's `suspected` list is what makes
  the second turn necessary, and a reviewer with nothing suspected finishes
  in one.
- Delete `settle()`, the follow-up nudge, `reviewerBeforeSpawn`'s tool
  narrowing (children no longer exist on the reviewer), and the `children`
  block; keep `beforeTool` for the write-attempt log.
- Dry run with `--max-turns 6` on a PR that produces a suspected issue.

### 5. Docs

`README.md`, `docs/architecture.md`, `docs/configuration.md` describe the
output contract, the Loupe-driven panel, and the runner test; the
`vendor/@whip/VERSION` line in the README moves to the new commit.

## Preserved

Dry run; incremental review; profiles; ensemble; verification fail-open;
skills, conventions, path instructions; `--dir`; chat, fix, help; the
agentic → one-shot fallback; the throwaway `WHIP_HOME`; `maxTurns`; prompt
caching; the fixer's hook guardrails; content-hashed definition ids; every
existing test's behavior except the two submit tools' handler tests, which
become output-normalization tests.

## Risks

- **zod 4 in Loupe's own schemas.** Low: the API surface Loupe uses is
  unchanged in 4.x. The transforms in `types.ts` stay Loupe-side; the wire
  schemas are plain objects, so Standard JSON Schema derivation never sees a
  transform.
- **Output correction costs a model round.** One bounded round on an invalid
  final message, in the ledger and the stream. Under a two-cell budget that
  is still cheaper than the nudge turn it replaces.
- **Losing the in-turn anchoring feedback.** Post-hoc snapping stays; the
  `check_anchors` tool is the fallback if dry runs show more dropped
  findings than before.
- **The daemon must be at 6.6.** The installed desktop daemon is
  (`8e577056e`); CI still waits for a whip release that contains this branch.

## Decisions (September 12, 2026)

Settled with Sam:

- **zod 4.** Loupe upgrades from zod 3 to zod 4 so tool inputs and outputs are
  typed from the schema.
- **Vendor from `whip-rlm`.** The branches were consolidated; `whip-rlm`
  carries the SDK work and both Loupe plans. Loupe's `vendor/@whip` refreshes
  from its tip.
- **Fixer stays prose.** No output contract for `@loupe fix`.
- **Testing: the scripted-daemon unit test of the runner is enough.** No live
  acceptance through the Node fixture.

Still open, restated in plain terms below:

1. **How the reviewer hands Loupe the review.** Today it calls the
   `submit_review` tool mid-turn. The alternative is an output contract: the
   review is the model's final message, the daemon checks its shape and
   corrects it once, and Loupe reads `result.output`. The catch is that the
   daemon checks the final message of every turn, and the model-driven panel
   spans turns (spawn confirmers, end the turn, wake on their replies), so a
   waiting turn would fail the check. Choosing the output contract means Loupe
   runs the confirmers itself from a `suspected` list in the reviewer's
   output and asks the reviewer for the final review in a second turn.
   Recommended: the output contract with Loupe-driven confirmers.
2. **Whether to keep a `check_anchors` tool.** The `submit_review` tool tells
   the model which findings sit on lines outside the diff and names the
   nearest commentable lines, so it can fix them before finishing. An output
   contract checks only the shape, so that in-turn correction disappears and
   Loupe falls back to snapping within ten lines and demoting the rest to
   notes, as the original Loupe did. `check_anchors` would be an optional
   tool the model may call before answering to get the same feedback back.
   Recommended: leave it out and add it only if dry runs show more findings
   landing off the diff than before.
