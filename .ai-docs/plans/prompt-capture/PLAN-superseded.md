# Prompt capture: what the model actually saw, per call

Status: plan, reconciled with the local implementation on 2026-09-15. Scope is
exactly two issues from the HALO oracle run: the system prompt is missing from
exported LLM spans, and compaction is invisible to the export. Follows
[session-trace](../session-trace/PLAN.md); decisions are at the end.

## Why

The HALO run on 2026-09-14 made the gap concrete: the first LLM span of a real
session reported 9,466 input tokens and showed one user message. The export
rebuilds prompts from the durable transcript, and both transcript writers skip
role `system` (`internal/session/agent_turn.go:252`,
`internal/session/runtime.go:691`). So the system prompt, the per-request
ephemeral notices and every compaction summary never reach the export, and
after a fold the export keeps showing raw rows the model no longer sees.

inference.net's own SDKs do not solve this for an event-driven integration:
their provider wrappers repeat the whole request on every span, and their
OpenCode plugin captures the system prompt once per turn and drops history.
Compaction is modelled nowhere. We keep the delta export (session-trace
decision 4) and add exact per-call capture of only the parts the transcript
cannot supply, using storage that already exists.

## What one request contains today

Assembled in `internal/agent/agent.go:513-521` per provider attempt:

| Position | Content | Source | In transcript? |
| --- | --- | --- | --- |
| 0 | primary system prompt | `refreshPrompt` (`internal/daemon/prompt.go:15`) runs first in `RunTurn`, composes a `PromptSnapshot`, installs it with `SetSystemPrompt`; constant within a turn | no |
| 1 | ephemeral system message | `withEphemeralSystem`: scratch notice, budget notice, `turn_start` hook contribution, plus `EphemeralNotices()` re-evaluated on every request (`internal/daemon/agent_session.go:120-137`) | no |
| 2 | compaction summary, after a fold | `compact()` installs `summaryPrefix + summary` as role `system` with `RawSequence` = raw cutoff (`agent.go:1059`); a later fold merges the prior summary into a new one | no; the fold lands in `compactions` at turn commit |
| … | history and this turn's rows | `appendTurnMessages` records each through `OnMessage`, so every non-system message carries a `RawSequence` transcript identity | yes |
| last | tool-limit notice, rare | `agent.go:1234`, role `system` | no |

Helper calls differ from the turn's prompt, which is why capture must read the
request rather than the snapshot: the compaction call is
`[system prompt, user: summaryPrompt]` with purpose `compaction`
(`agent.go:1043-1049`) and its output comes back as a plain string from
`Complete`; the title call has its own one-line system message
(`agent_session.go:450`).

Bug found while tracing this: `internal/daemon/spans.go:248` names a span
`compaction` when `attempt.Purpose == "compact"`, but the agent passes
`"compaction"`, so compaction spans are currently named `provider/model`.

## What already exists and is reused

- **`model_calls.attempt`** stores the whole `llm.ModelAttempt` as JSON at
  admission (`internal/session/model_call.go:167`) and decodes it on load
  (`:362`, `:698`). A field added to `ModelAttempt` persists with no schema
  change. Schema stays at 19.
- **Content store**: sha256-addressed blobs (`internal/content/store.go:38`)
  behind root-scoped `content_references` + `content_grants`, written through
  `Store.StoreContent` and read through `readRuntimeValue` and the `content.read`
  RPC. Identical bodies dedupe to one blob.
- **`compactions`** table per `(session_id, agent_id)`: generation `seq`,
  `cutoff` = raw transcript seq of the last folded row, `summary`. Written at
  turn commit through `RootTurnCommit.Compactions` → `appendCompactionsTx`
  (`internal/session/transcript.go`), or immediately for a manual compaction
  (`RecordRawCompaction`). Exposed by `history.compact.log` and used by
  `history.compact.retry`; untouched by this work.
- **`Events.OnCompaction(summary, cutoff, before)`** fires in the daemon right
  after a fold (`agent_session.go:180`); the raw cutoff is derivable there.
- **Exporter delta assignment** (`assignDeltas`, `internal/session/otlp_export.go:385`)
  walks an agent's transcript in raw order and gives each LLM call the rows
  between consecutive produced assistant rows. It is extended, not replaced.
- **Trace details pane** already renders every span attribute in its Raw span
  block, so digests and sizes show up with no UI work.

## Design

### 1. Capture rule and point

Capture per provider attempt every request message the transcript will not
have: every message with role `system`, with its request index and its
`RawSequence` (the summary's is the raw cutoff), and any non-system message
with `RawSequence == 0` (this is how the compaction call's fold prompt is
captured with no special case). Nothing else is in scope: not tool
declarations, not sampling parameters.

Where: `runAttempt` in `internal/llm/accounting.go:393` holds the final `req`
after the ephemeral insert and before `stripAuthored`, and already builds the
`ModelAttempt` it hands to the budget. `ModelAttempt` gains
`Prompt []CapturedMessage{Index, Role, RawSequence, Content}`.

### 2. Storage

In `beginAgentModelAttempt` (`internal/daemon/budget.go`), before
`AdmitModelCall`, each captured body is stored through the content store with
a root grant, and the field is rewritten to carry `Digest`, `Bytes` and
`ReferenceID` instead of `Content`. Before minting a reference, an existing
root-scoped reference with the same digest is reused, so a distinct body costs
one `content_references` row per session. The rewritten capture then persists
inside `model_calls.attempt` as today. Admission is the right moment: a failed
or interrupted attempt still sent that prompt.

A content-store failure logs through the existing span error path and drops
the capture for that call; it never fails the model call.

### 3. Span attributes

`modelCallSpanStart` adds, per captured slot, `prompt.<slot>_digest`,
`prompt.<slot>_bytes` and `prompt.<slot>_ref`, where the slot is `system`,
`ephemeral`, `summary` (plus `prompt.summary_raw_cutoff`) or `message_<index>`
for any other captured row. Identities and sizes only, so the journal event
stays small and the trace view can fetch a body through `content.read` later.

### 4. Export

The delta rule extends to captured slots. A slot is emitted in
`llm.input_messages` when its digest differs from the same slot on the agent's
previous call, placed by request index among the rows it is emitted with:

- an agent's first call emits `system`, `ephemeral`, then its first rows;
  later calls emit `ephemeral` only when the notices changed and `system` only
  when the composed prompt changed;
- the first call after a fold emits the summary as a new `system` message and
  carries `whip.compaction.raw_cutoff` and `whip.compaction.folded_rows`, so a
  consumer can tell that rows at or before the cutoff are no longer in the
  model's context; `folded_rows` counts transcript rows between the previous
  cutoff and this one;
- a compaction call exports its real input (captured system prompt and fold
  prompt) and the summary as its output. The output comes from the first later
  call whose `summary` slot is new, or, when the fold ended the turn, from the
  `compactions` row with that raw cutoff once the turn commits. No new write
  is added for this; the only blind spot is a live export in the seconds
  between a turn-ending fold and its commit;
- `gen_ai.system_instructions` is not emitted; neither inference.net nor HALO
  reads it, and both render the system message from `llm.input_messages.0`.

Exact reconstruction of any call's prompt is then: captured system messages at
their indices, plus transcript rows from the summary's raw cutoff (or the
start) through the call's last row. Sessions recorded before this change have
no capture and export exactly as today.

### 5. Naming fix

`modelCallSpanStart` compares against `"compaction"`, matching what the agent
passes; `CompactAccounting("title")` keeps its own name through the existing
`provider/model` fallback.

## What stays the same

- Transcript writers keep skipping system rows; history, `context.audit`,
  `context.history` and compaction retry are untouched.
- Span ids, kinds, parenting, the delta rule for transcript rows, the
  `compactions` table and its turn-commit path stay as shipped.
- Schema version, protocol version and the SDK/app types do not change; the
  new attributes are ordinary span attrs.

## Out of scope, noted for later

- A View action for prompt bodies in the trace details pane.
- Tool declarations and invocation parameters on LLM spans.
- A `-full-prompts` export mode that repeats the full prompt per span.
- Manual `/compact` while idle has no open turn, so it gets no span today; its
  capture still lands on the model call row.

## Work items, in order

1. `llm.CapturedMessage` on `ModelAttempt`; `runAttempt` fills it from the
   final request. Unit test: system, ephemeral, summary with raw cutoff and the
   compaction call's fold prompt are captured with indices; transcript rows
   are not.
2. Daemon: store bodies with reference reuse by `(root, digest)`, rewrite the
   capture to references, add the span attrs, fix the purpose name.
   Integration test: a turn with a mid-turn fold yields the expected attrs on
   the turn call before the fold, the compaction call and the first call after.
3. Export: slot deltas, compaction attrs, compaction call input and output.
   Fixture test with two turns, a changed ephemeral notice and one fold, with
   count and content assertions per span; the existing "every message once"
   test keeps passing.
4. Docs: `docs/protocol-v2.md` span attrs; this plan's decisions.
5. Oracle: rerun a real session with a forced fold against headless HALO and
   confirm the system message on the first LLM span, the summary on the first
   post-fold call, and the compaction span with input and output.

## Decisions

1. Capture is always on, matching session-trace decision 11 (no redaction).
   Bodies are local and deduplicated.
2. No schema change: the capture rides in `model_calls.attempt`; bodies live in
   the content store; spans carry identities and sizes only.
3. Capture reads the request in `runAttempt`, not the per-turn
   `PromptSnapshot`, because helper calls send different system messages and
   the ephemeral message changes per request.
4. The export keeps delta semantics for captured slots: emitted on first
   appearance and on change, never repeated.
5. Compaction output is derived from existing writes (next call's summary slot,
   else the committed `compactions` row), accepting the brief blind spot for a
   live export between a turn-ending fold and its commit.
6. `gen_ai.system_instructions` is not emitted.

## Open questions

None blocking. If the blind spot in decision 5 matters for live HALO viewing,
the fix is a small `output_ref` write in `OnCompaction`; say so and it goes
into item 2.
