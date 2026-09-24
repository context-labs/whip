# Compaction and system prompt visibility in the trace

Status: implementing, 2026-09-15. Supersedes the 2026-09-15 prompt-capture
draft (`PLAN-superseded.md`), which described a capture path through the
accounting layer that the shipped telemetry does not need. Decisions are at the
end, above the one open question.

## Why

The HALO oracle run on 2026-09-14 showed the first LLM span of a real session
with 9,466 input tokens and a single user message. That is the shape of two
gaps, not one:

- **The system prompt is invisible.** `refreshPrompt` composes it once per turn
  and hands it to the agent, but both transcript writers skip role `system`
  (`internal/session/agent_turn.go`, `internal/session/runtime.go`), and
  `recordTranscriptMessage` returns 0 for it. Nothing durable holds the prompt a
  turn ran under, so neither the export nor the trace view can show it.
- **Compaction is invisible.** The compaction call is an LLM span, but
  `internal/daemon/spans.go:248` names it `compaction` only when the purpose is
  `"compact"`, and the agent passes `"compaction"`, so it renders as
  `provider/model` like every other call. The summary it produced reaches the
  `compactions` table at turn commit, but `internal/session/otlp_export.go`
  never reads that table, so the fold has no output anywhere in the export and
  nothing tells a consumer which rows left the model's context.

Prompt debugging is half the reason to open a trace. Today the trace cannot
answer "what did this call send" or "what did that fold keep".

## What the shipped telemetry already gives us

Session-trace built the right shape and it should stay: `spans` is an index
whose `attrs` hold pointers and short excerpts, bodies live where Whip already
stores them, the content store dedups bodies by sha256 (`content.Put`), a
`content_references` row plus a root grant makes a body readable through
`content.read`, `endSpanTx` merges attrs with `json_patch`, and the export
resolves pointers into full attribute values. The fix is to point the existing
spans at three bodies that are currently never stored, and to teach the export
and the view to follow those pointers. No new table, no schema bump, no change
to `llm` or to model-call accounting.

## What one request contains, and where each part lives after this plan

| Part | Composed | Stable for | Durable today | After |
| --- | --- | --- | --- | --- |
| system prompt | `refreshPrompt`, once per turn | the turn | no | content ref, `system_prompt_ref` on every LLM span of the turn |
| ephemeral system message | `Events.ephemeral()` per request: turn notice + hook notices | one request (retries reuse it) | no | content ref, `ephemeral_ref` on turn and final calls (decision 4) |
| compaction summary | `compact()` installs it as role `system` after the fold | until the next fold | `compactions` at turn commit | also a content ref, `output_ref` + `raw_cutoff` on the compaction span |
| history and this turn's rows | `appendTurnMessages` | n/a | yes | unchanged; the delta rule stays |
| tool-limit nudge (`finalAnswer`) | trailing role `system` | one request | no | not captured; skipped: rare, constant text |
| tool declarations, sampling parameters | `Request.Tools`, `Agent` fields | rarely change | no | skipped: not part of these two issues; add when someone asks |

Decay (`internal/agent/decay.go:59`) rewrites tool-row content in place before a
request, so transcript rows are not byte-exact for what the model saw. This plan
does not promise byte-exact reconstruction. It promises that the parts the
transcript cannot supply are visible per call. Byte-exact capture of the encoded
request body is a different feature (one undeduplicated blob per call) and stays
out of scope.

## Design

### 1. Store helpers (`internal/session`)

- `InternContent(ctx, rootID, source string, data []byte) (RuntimeValue, error)`:
  `content.Put`, then reuse an existing root-scoped reference by
  `(digest, source, root)` before minting one. This is the query that already
  lives inline in `collection_page.go:122`, lifted so both callers share it.
  A 35 KB system prompt repeated over a hundred calls costs one blob and one
  reference row per session.
- `PatchSpanAttrs(ctx, rootID, spanID string, attrs map[string]any)`:
  `UPDATE spans SET attrs=json_patch(attrs,?), updated_seq=? WHERE id=?`, then
  emit `span.ended` with the merged record. Today `startSpanTx` is
  `ON CONFLICT DO NOTHING` and `endSpanTx` only patches open spans; the
  compaction span has already ended when the daemon learns the summary. The SDK
  reducer upserts by `updated_seq`, so a second `span.ended` is safe and the live
  view picks the patch up without a protocol change.

### 2. Daemon (`internal/daemon`)

- **System prompt.** After `refreshPrompt` in `RunTurn`, intern
  `snapshot.Prompt` with source `prompt.system` and keep the reference and size
  on `node.turn`. `modelCallSpanStart` already resolves the agent node through
  the runtime; it copies `system_prompt_ref` and `system_prompt_bytes` into the
  LLM span's start attrs. One store write per turn, none per call.
- **Ephemeral.** Only `Events.ephemeral()` knows the final composed string, and
  it is the single composition point. Add `Events.OnEphemeral func(text string)`
  and call it there. The daemon's handler interns the text with source
  `prompt.ephemeral` and keeps the reference on `node.turn`; `modelCallSpanStart`
  copies `ephemeral_ref` and `ephemeral_bytes`. The hook runs on the turn
  goroutine before `client.Stream`, which is before admission opens the span, so
  the reference is always the one this request carried, including across
  retries, which reuse the request.
- **Compaction.** Fix the purpose check to `"compaction"`. In the existing
  `events.OnCompaction` handler (`agent_session.go:179`), intern the summary
  with source `prompt.summary` and patch the compaction span with `output_ref`,
  `output_bytes` and `raw_cutoff`. The span id is
  `ModelCallSpanID(root, node.turn.LastModelCallID)`: settlement runs inside
  `runAttempt` before `Complete` returns to `compact()`, so at that moment the
  last settled call is the summary call. The `compactions` journal entry is
  written exactly as today.
- Capture failures are logged and never fail the call, matching the span-write
  posture.

### 3. Export (`internal/session/otlp_export.go`)

`assignDeltas` already walks each agent's LLM spans in start order. Extend it:

- Prepend a `system` message from `system_prompt_ref` when the reference differs
  from the agent's previous LLM span (so the first call of a session emits it,
  later calls only when skills, roots or an override changed it). Same rule for
  `ephemeral_ref`. Attributes are named by the daemon, so there is no
  classification by request index.
- A compaction span exports the summary from `output_ref` as
  `llm.output_messages.0` and `whip.compaction.raw_cutoff`. Its system prompt
  follows the same rule as every other call: emitted only when the reference
  differs from the agent's previous call, which it normally does not.
- The first non-compaction LLM span of that agent after a compaction span gets
  the summary as a `system` message (with the agent's `summaryPrefix`) ahead of
  its transcript delta, plus `whip.compaction.raw_cutoff`, so a consumer can tell
  that rows at or below the cutoff are no longer in the model's context.
- Sessions recorded before this change have no references and export exactly as
  today.

### 4. Trace view (`packages/app/src/trace-view.tsx`)

`SpanDetails` shows excerpts and says "Export the session for full bodies."
Add one row per `*_ref` attr with its size and a View action through the
existing `ContentRead` in `details/shared.tsx`. `spanDisplayName` shows
`compaction` for the fixed purpose. No per-call diffing.

## Preserved

- Transcript writers keep skipping system rows; history, context inspection,
  and compaction recovery are untouched.
- `model_calls`, `ModelAttempt`, admission and settlement are untouched.
- Span ids, kinds, parenting, the delta rule for transcript rows, and the
  `compactions` table and its turn-commit path stay as shipped. The export
  invariant that every transcript row appears exactly once still holds; system
  and summary messages are not transcript rows.
- Bodies never enter span attrs or the journal; the excerpt bound is unchanged.

## Items, in order

1. Purpose fix and store helpers: `InternContent` with reuse, `PatchSpanAttrs`
   with the re-emitted `span.ended`. Tests: reuse by digest within a root, no
   reuse across roots, patch merges over settled attrs and bumps `updated_seq`.
2. Daemon wiring: prompt reference after `refreshPrompt`, `OnEphemeral`,
   compaction patch. Integration test: a turn with a mid-turn fold yields a
   compaction span named `compaction` with `output_ref` and `raw_cutoff`, and
   every LLM span of the turn carrying the same `system_prompt_ref`.
3. Export: the two delta rules, compaction output and cutoff, the summary on the
   first post-fold call. Fixture test with two turns, a changed prompt between
   them, and one fold; assert message counts and contents per span and that no
   transcript row repeats.
4. Oracle: rerun the real session against headless HALO (recipe in memory) and
   confirm the system message on the first LLM span, the fold rendered with its
   summary as output, and the summary on the next call.
5. Trace view rows and the compaction name; app test with a captured span.
6. Docs: `docs/protocol-v2.md` span attrs, `docs/frontend.md` details pane.

## Risks

- Idle `/compact` runs through `CompactNow` outside any turn, so the summary
  call has no turn span and `modelCallSpanStart` skips it. It still settles in
  `model_calls` and its summary still reaches `compactions`. Giving it a span of
  its own needs a synthetic root span (open question 2).
- `content_references` lookups by digest are not indexed today; one query per
  ephemeral per call is cheap at current call rates, and an index is a one-line
  follow-up if it shows up.
- A summary is stored twice: as text in `compactions` (recovery) and as one blob
  per fold in the content store (trace). Accepted; the blob is what the live
  view and the export read without order-pairing heuristics.

## Decisions (2026-09-15)

1. Idle `/compact` gets no trace in this batch. The mid-turn fold is where the
   oracle's confusion came from.
2. Trace view rows ship in this batch, after the export and the HALO oracle.
3. Byte-exact request capture is out of scope. If it is ever wanted it is a
   separate flag storing the encoded body per call, with no dedup.
4. Capture the ephemeral system message (2026-09-16). It is the second
   `system` message the agent sends on turn and final-answer calls, as the
   last message since 2026-09-16 (it sat at index 1 before, where any change
   invalidated the cached history behind it): the runtime notice when a worker
   was replaced, the model budget notice (finite or unlimited per budget, no
   amounts, so it is byte-stable across turns), the definition's `turn_start`
   hook contribution, and hook notices raised mid-turn. `Events.OnEphemeral`
   hands the composed text to the daemon, which interns it when it changes and
   stamps `ephemeral_ref` on the call's span; the compaction and helper calls
   do not carry it and do not claim it.
