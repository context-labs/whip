# A trace for `/compact`

Status: implemented 2026-09-16 (see Verification at the end). Follows
[prompt-capture](../prompt-capture/PLAN.md), which left idle compaction out of
scope (its decision 1). Decisions are at the end.

## Why

`/compact` is a user action: the TUI command, the app's **Compact history**,
and the SDK's `session.history.compact()` all send the `history.compact` client
command, which runs one model call outside any turn. Today the trace of that
call depends on what happened before it:

- **Right after a turn**, the agent node still holds the ended turn's trace
  identity (`session.turn` is reset only when the next turn starts in
  `RunTurn`, or when history is replaced). The summary call's LLM span lands as
  a late child of a turn that already ended, and nothing attaches the summary
  to it, because the `OnCompaction` hook that does so only runs for mid-turn
  folds.
- **After a daemon restart with no turn yet**, the same command leaves no span
  at all; only the `model_calls` accounting row and the `compactions` row
  exist.

Either way a user who just ran `/compact` and opens the trace view cannot find
what it did, what it cost, or what summary it wrote.

## What happens today

| Step | Where | Trace today |
| --- | --- | --- |
| Command admitted; refused while a root operation runs | `client_control.go:605` | none |
| `runner.CompactNow` in a worker goroutine | `client_control.go:617` → `client_control.go:286` | none |
| Summary model call, purpose `compaction` | `agent.CompactNow` → `compact()` | LLM span only if a stale turn identity exists, else none |
| Summary persisted | completion handler `RecordRawCompaction` (`client_control.go:800`) | `compactions` row; no `output_ref` anywhere |

Two other model calls run outside a turn and share the gap: `goal.from-context`
(user-initiated) and the automatic session title (system-initiated, launched
right after a turn ends, so it parents under that turn through the same stale
identity). This plan covers `/compact`; open question 2 asks about the goal
call. The title call is a consequence of its turn and reads well as its late
child, so it is left alone.

## Design

One wrapper around the existing work in `AgentSession.CompactNow`
(`internal/daemon/client_control.go:286`), on the node, so the actor handler
and the completion path do not change:

1. **Open a root span** with its own trace: kind `agent` (the `spans` table's
   CHECK lists five kinds; a new one would need a table rebuild for no gain),
   name `compact`, attrs `trigger: command`, `command: history.compact`,
   `input: /compact`. Ids derive from a fresh random command id the way turn
   spans derive from a turn id, so start and end agree without coordination.
2. **Give the node that identity** for the duration: set `turn.TurnID`,
   `SpanID`, `TraceID` to the command's. `modelCallSpanStart` then parents the
   summary call under the new root and names it `compaction`, through the code
   that already exists. This is safe because the handler refuses `/compact`
   while a root operation runs and `RunTurn` resets the journal when the next
   turn starts. Clear the identity afterwards so nothing later inherits it.
3. **Intern the system prompt** (`internPrompt`) so the compaction span carries
   `system_prompt_ref`, as it does for mid-turn folds. Reuse by digest makes
   this one lookup.
4. **Run `agent.CompactNow`** unchanged.
5. **Attach the summary** with the existing `recordCompactionOutput`, which
   patches the compaction span with `output_ref`, `output_bytes` and
   `raw_cutoff` using `turn.LastModelCallID`.
6. **Close the root span**: status `ok` with the summary excerpt as `output`,
   or `error` with the error excerpt.

Export: `agentSpan` renames every root to the agent's name and exports it as
`AGENT` / `invoke_agent`. A root with `trigger: command` keeps its own name,
exports as `openinference.span.kind = CHAIN` with `gen_ai.operation.name` set
to the command, and takes `input.value` and `output.value` from its attrs. The
child LLM span already exports the full summary as its output message.

Trace view: root spans are listed by `traceRoots` (no parent, kind `agent`), so
`compact` appears in the picker with its time, and the tree shows one
`compaction` child with Read buttons for the prompt and the summary. No app
change. HALO shows the command as one more turn of the session; that is how
HALO models any root span.

## Preserved

- The `compactions` table and the completion handler that writes it.
- Model-call accounting, admission and settlement.
- `RunTurn`, mid-turn folds, and the four kinds of span they write.
- The automatic title call keeps parenting under the turn that triggered it.
- No schema change, no protocol change, no new RPC.

## Items

1. The wrapper in `CompactNow` and the export naming rule. Tests: after a
   manual `/compact` the store holds a root `agent` span named `compact` with
   `trigger: command` and one child `compaction` LLM span carrying
   `system_prompt_ref`, `output_ref` and `raw_cutoff`; a `/compact` issued right
   after a turn does not attach to that turn; a second `/compact` opens a
   second trace; a failed summary call closes the root as `error`. Export
   fixture: a command root exports as `CHAIN` named `compact` with the child's
   summary as output.
2. Docs: one paragraph in `docs/protocol-v2.md` (command roots), one sentence
   in `docs/frontend.md` (the picker lists `/compact` runs).
3. Oracle: dump the manual-compaction test's export with `WHIP_OTLP_DUMP` and
   push it to the local HALO backend; the session gains a `compact` turn whose
   only span is the fold with its summary.

About 90 minutes.

## Decisions (2026-09-16)

1. `/compact` is its own trace, not a late child of the previous turn. It is a
   user action with its own start and end, the previous turn has ended, and
   after a restart there is no previous turn to attach to.
2. `goal.from-context` gets the same wrapper, root named `goal`. The automatic
   title call stays a late child of the turn that triggered it.

## Verification (2026-09-16)

- `internal/daemon` `TestUserCommandsOutsideATurnGetTheirOwnTrace`: a `/compact`
  before any turn yields a root `compact` (trigger `command`, input `/compact`,
  output = summary) with one `compaction` child carrying `system_prompt_ref`,
  `output_ref` and `raw_cutoff`; the node's identity is cleared afterwards; a
  `/compact` after a turn opens a second trace beside the turn's and closes as
  `error` when nothing can be folded; `goal.from-context` opens its own trace
  with one `helper` call under it. The whole daemon package, session, agent
  packages and golangci-lint are green.
- `internal/session` `TestExportOTLPNamesCommandRootsAsChains`: a command root
  exports as `CHAIN` named `compact` with `gen_ai.operation.name
  history.compact`, `input.value` `/compact`, the summary excerpt as
  `output.value`, and the fold's summary as the child's output message.
- Oracle: the test's export (`WHIP_OTLP_DUMP`) pushed to the local HALO
  backend. HALO lists `compact` (`/compact` → summary, $0.00013), the failed
  `compact`, and `goal` (`/goal-from-context 2` → goal text) as traces beside
  the turns.
- Finding on the way: the system prompt is now interned from the agent's own
  first message rather than the composed snapshot, because a `/compact` before
  any turn sends whatever the agent already holds and the snapshot is empty
  until the first turn composes it.
- Finding on the way: a successful `goal.from-context` enqueues a goal turn, so
  a `/compact` issued immediately after it is refused as "root operation is
  running"; that is existing behaviour, not a trace concern.
