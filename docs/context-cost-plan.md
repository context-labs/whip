# Plan: cut context/token cost without losing quality

Evidence: session `7ec5ba63` (kimi-k3, 2026-09-01 → 09-03) — 731 main-loop
requests + 1,365 subagent requests, 283M input tokens served (264M cached,
19M fresh), ≈ $210 at inference-net rates. Peak per-request prompt: 392k
tokens. Two compactions in 48h; user had `compactPct: 30` set and it never
fired after the second compaction because the estimator never saw the context
cross 30% of the 1M window while the provider was billing 391k.

## Diagnosis (confirmed against the session DB)

1. **Compaction trigger runs on `EstimateTokens` (chars/4 + flat 1200/image),
   not real usage.** In session 7ec5ba63 the estimate peaked at 296k (28% of
   window) while real prompt tokens hit 392k (37%). With `compactPct: 30` the
   proactive path stayed asleep for the last ~150 requests. opencode triggers
   off provider-reported usage on every step (`processor.ts:435-484`,
   `overflow.ts`), cutting the stream mid-turn when full.
2. **Image tokens are undercounted ~7×** (`agent.go:693`: `1200*len(m.Parts)`).
   Four screenshots in that session actually cost 4.1k–11.4k tokens each
   (28px-patch vision models); whip counted 1.2k. Images also never leave
   context: ~7.1M tokens of the session's input was re-sending old
   screenshots.
3. **No image normalization at ingest.** Clipboard PNGs go in verbatim
   (3410×2646 observed). opencode caps at 2000×2000 and re-encodes JPEG
   q80→40 until ≤5MiB (`image/image.ts`).
4. **Compaction tail is 6 *messages* (`compactKeepBack`), not a token
   budget** — six messages can be six 50KB tool dumps; and the summary is
   capped at 1024 output tokens with 500-char tool-result truncation in the
   transcript, so each fold is small-lossy instead of small-lossless.
   opencode keeps a token-budgeted tail (`min(15k, max(2k, usable×0.25))`)
   and summarizes incrementally (carry the prior summary forward).
5. **No doom-loop guard.** One subagent (`rewrite-flight-parser…-4`) made 789
   sequential calls at 110–137k tokens each (85M tokens, ~$35) rewriting one
   parser. opencode blocks the 3rd identical (tool, args) call behind a
   permission prompt (`processor.ts:356-380`).
6. **Usage accounting double-counts subagents** — fanned into the parent via
   `AddUsage` (subagent.go:147) *and* persisted on the task session — and the
   resume path (`tui.go:811-829`) can re-seed from per-message usage when the
   columns are zero, inflating reported spend ~2× in this session (550M
   reported vs 283M provider-truth).

> **Status: implemented on this branch.** Items 1–7 landed with tests;
> `go build ./...` and `go test ./...` pass (the two `internal/tui`
> `TestStartupReport*` failures pre-date this branch — verified failing on
> clean `main`). Implementation notes per item are marked "as built".

## Work items (ordered by $ saved per line changed)

### 1. Real-usage compaction trigger — `internal/agent/agent.go`
- After each `Stream` returns (we already have `usage` at agent.go:448),
  record `lastUsage` and fire the proactive check off
  `usage.PromptTokens + usage.CompletionTokens ≥ threshold × ContextLimit`
  instead of (strictly, in addition to as a pre-flight fallback)
  `EstimateTokens`.
- Keep the chars/4 estimate only when the provider returns no usage.
- Ref: opencode `session/overflow.ts`, `session/processor.ts:435-484`.

### 2. Honest image token estimate — `internal/llm` + `internal/agent`
- Decode PNG/JPEG dimensions from the first bytes of each `image_url` part;
  estimate `⌈w/28⌉·⌈h/28⌉ + 2` (moonshot/qwen patching), floor at 85,
  keep 1200 only when undecodable.
- Store decoded dims on the ContentPart at ingest so the estimator never
  re-decodes.

### 3. Paste/ingest image normalization — `internal/tui/paste.go`, `mentions.go`, browser screenshot sink
- Cap at 2000×2000 (Lanczos via `golang.org/x/image/draw`), re-encode JPEG
  q≈80 (fall through 70/55/40) until base64 ≤5MiB. Port of opencode
  `image/image.ts:10-14`. PNG kept only when JPEG can't beat the byte cap.
- No-op for images already under the caps (cheap fast path).

### 4. Token-budgeted compaction tail + incremental summary — `internal/agent/agent.go`
- Replace `compactKeepBack = 6` messages with a token budget:
  `min(15k, max(2k, usable×0.25))`, walking whole user turns newest-first
  (never split a tool_call/result pair — keep the existing orphan walk-back).
- Summary `MaxTokens` 1024 → 4096.
- Thread the previous summary into `buildSummaryPrompt` ("merge, don't
  re-summarize; anything you don't carry forward is lost") so fold N builds
  on fold N−1 instead of re-deriving from truncated transcripts.

### 5. Image strip in decay — `internal/agent/decay.go`
- Once an image-bearing message exits the hot window and the conversation
  has moved on (≥2 user turns later), replace the image part with a text
  placeholder pointing at the paste file on disk (`~/.whip/pastes/…` already
  persists it): `[screenshot 3410×2646 omitted; at PATH — re-paste if needed]`.
  Zero quality loss for UI-iteration workflows; the file is recoverable.
- Largest single win for long UI-polish sessions (would have removed ~5% of
  this session's total spend, and much more in screenshot-heavy runs).

### 6. Doom-loop guard — `internal/agent/agent.go` runTools
- Key tool calls by (name, sha256(args)) per turn; on the 3rd identical call
  return an error tool result: "refused: identical call repeated — change
  approach or ask the user". Breaks subagent retry spirals that burn 10s of
  millions of tokens (opencode `processor.ts:356-380`).

### 7. Usage accounting fix — `internal/agent/subagent.go`, `internal/tui/tui.go`
- Persist usage on exactly one row: the task session keeps its own usage;
  the parent displays `own + Σ tasks` at render time instead of fanning into
  its cumulative counters. On resume, never re-seed from per-message usage
  once the columns are non-zero (already true) — additionally stop the
  fan-in double-add for subagents whose task session persisted usage.

### 8. Tests
- Estimator: synthetic history with a 3410×2646 PNG part must estimate
  within 20% of the provider-reported prompt tokens.
- Trigger: fake client returning usage{prompt=320k} on a 1M-window agent
  with threshold 0.3 compacts before the next request.
- Doom loop: three identical `bash` calls in one turn → third returns the
  refusal, no exec.
- Image strip: paste a 2-image message, age it out of the hot window, run
  decay, assert placeholder text + paste path, assert Parts empty.
- Compaction tail: 50k-token tail budget keeps whole turns and never
  orphans a tool result.

## Non-goals
- No tokenizer vendoring (tiktoken etc.) — real-usage trigger removes the
  need for precision.
- No change to the 50KB tool-output cap / middle-elision / spill-to-disk
  (already matches opencode's 50KB truncation + disk recovery).
- Prune of text tool results outside the hot window already exists as
  `decay`; this plan extends the same idea to images rather than adding a
  second mechanism.
