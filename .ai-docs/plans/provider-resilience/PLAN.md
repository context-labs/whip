# Provider call resilience: a single call or timeout must not fail whip

Branch: `compaction-loop-and-ui-cleanup`. Written 2026-09-14 after campaign `modal-full-20260914-b`.

## Why this matters

Two of ninety attempts in the first campaign on the simplified eval scaffold ended with `context deadline exceeded` after about 46 minutes of agent work each (t0004 `expr-try-catch-errors`, t0026 `meriyah-explicit-resource-declarations`, about $57 of known cost between them). Neither was a model failure. A single streamed request hit whip's ten-minute per-call ceiling, whip does not retry a deadline, the agent turn returned the error, the CLI exited, and the harness recorded `agent_error`. The same mechanics apply to interactive use: one stalled or dropped provider stream ends the turn, and the user has to resend.

Whip's own retry loop is good at what it covers (transport errors and 429/5xx before the first token, with backoff and Retry-After). It does not cover the three failures that actually happen in long agent runs: a stream that stalls after it started, a stream that drops after it started, and a healthy generation that simply takes longer than a fixed total ceiling. opencode and pi both solve these with stall-based deadlines instead of total ceilings, and by regenerating the assistant message when a stream fails midway.

## How whip behaves today

Client (`internal/llm/openai.go`, `internal/llm/accounting.go`):

- `Stream` retries a failed attempt only while nothing has been emitted (`emitted` flag set by the first text, reasoning, or tool-call delta). Retryable means: a transport error, or an `HTTPError` with 429 or 5xx (`retryable()` at `openai.go:563`). `context.Canceled` and `context.DeadlineExceeded` are never retried. Backoff is 1 s doubling to a 20 s cap plus 25% jitter; `Retry-After` on an `HTTPError` raises the delay. Budget is `DefaultMaxAttempts = 8` attempts, configurable per provider as `maxRetries`. Since `f99af76f`, an SSE error chunk or a stream that closes with no completion marker before any delta is repeated exactly once (`preTokenError`).
- After the first delta, every failure surfaces: SSE error chunk (`api error: ...`, which is how Inference.net reports its own 30 s no-token stall), invalid chunk, usage decode error, stream closed without `[DONE]`, transport drop. The comment says a retry "would regenerate a partially received response"; the agent then appends the partial with `[response interrupted]` and returns the error (`agent.go:692`).
- Two total ceilings of ten minutes per call: `http.Client{Timeout: 10 * time.Minute}` in `New()` (covers headers and the whole body read) and `defaultCallTimeout` applied by `callContext` around the entire `Stream` call, so retries and their backoff also count against it. The budget permit inherits the remaining time (`permit.Timeout = time.Until(deadline)`), and `AdmitModelCall` requires a positive timeout because terminal recovery uses it to settle a reservation that never reported back (`internal/session/model_call.go:597`).
- No stall detection. A stream that goes quiet is only ended by the ten-minute total; a stream that keeps producing tokens for eleven minutes is killed even though it is healthy.
- `Complete` (compaction summaries) has the same loop, with "any content" as the no-retry point. The OpenAI Responses (`responses.go`) and ChatGPT subscription (`subscription.go`) stream paths mark all their mid-stream failures `nonRetryable` and share the HTTP client, so they inherit the same ceilings.

Turn and session (`internal/agent/agent.go`, `internal/daemon`):

- When `Stream` returns an error that is not a context-limit rejection, the turn ends with that error. The daemon commits the turn as `failed` (`session.go:952`), the root session stays alive for interactive clients, and `whip run` prints `{"type":"error"}` and exits (`cmd/whip/run.go:353`). The UI hook `OnRetry` shows "request failed; retrying in Ns" only for the pre-token retries.
- Child agents already have a turn-level retry: a failed child turn re-queues its input and re-wakes after 2^n seconds (cap 64 s) unless the input was invalid or the error is a permanent 4xx (`recursive_runtime.go:634`, `RetryInput`). There is no cap on `failures`, so a persistent outage makes a child retry every 64 s until its budget ends. Root turns have no equivalent.

## How opencode and pi handle it

| | opencode (`packages/opencode/src/session/retry.ts`, `processor.ts`, `provider/provider.ts`) | pi (`packages/ai/src/utils/retry.ts`, `provider-retry.ts`, `coding-agent/src/core/agent-session.ts`, `http-dispatcher.ts`) |
| --- | --- | --- |
| Total per-call ceiling | none by default (`timeout` option off) | none by default (SDK `timeoutMs` unset) |
| Stall detection | `headerTimeout` 300 s to first response headers; `chunkTimeout` 300 s between SSE chunks (`wrapSSE`) | undici dispatcher `headersTimeout` and `bodyTimeout` = 300 s idle (configurable 30 s to 5 min or off) |
| Mid-stream failure | retried like any other failure: the whole streaming step is wrapped in `Effect.retry`, attempt state is reset (`currentText`, `reasoningMap`), the assistant message is regenerated | provider stream yields `stopReason: "error"`; the agent loop ends; the session removes the failed assistant message from agent state (kept in history), backs off, and re-runs the turn |
| Budget and backoff | 5 retries; 2 s doubling, 25% jitter, 30 s cap without headers; `retry-after` and `retry-after-ms` honoured | provider layer mirrors the OpenAI SDK policy (408/409/429/5xx, `x-should-retry`, `Retry-After` capped at 60 s else fail fast, 0.5 s doubling to 8 s); agent layer 3 retries, 2 s doubling, 60 s cap; counter resets on any successful assistant message |
| Classification | SDK `isRetryable`, any 5xx, or message patterns (429/5xx codes in text, rate limit, overloaded, network, timeouts, "try your request again", "provider returned error"); context overflow never retried | message patterns (same families plus "ended without", "stream ended before", websocket text); explicit non-retryable list for quota, billing, insufficient balance; context overflow never retried |
| User visibility | session status `retry` with attempt, message, next time | `auto_retry_start` / `auto_retry_end` events; retry can be aborted; setting toggles |
| On exhaustion | assistant message carries the error, session idle, user resends | same; the failed message stays in history |

Both agree on the things whip lacks: idle-based deadlines with no fixed total, regeneration after a mid-stream failure, and text-based classification for errors the provider wraps in a 200 stream.

## Gaps, in order of cost to us

1. A fixed ten-minute total kills healthy long generations and is the only defence against stalls. (Both campaign losses.)
2. Nothing after the first delta is ever retried, so a stall or drop mid-answer ends the turn even when a fresh request would succeed in seconds.
3. Errors delivered inside a 200 stream are classified by position (before or after the first delta), not by what they say; a gateway "overloaded" after the first token fails the turn, a permanent "invalid api key" before it is retried once.
4. `context.DeadlineExceeded` is excluded from retry wholesale, so whip cannot tell its own stall deadline from a caller's cancellation.
5. Root turns have no fallback once the client budget is spent; children retry forever.

## Decisions (Sam, 2026-09-14)

1. **Stall timeout 120 s** (time to first byte, and gap between chunks). opencode and pi use 300 s; Inference.net's gateway itself gives up after 30 s without a token, so whip's timer is a backstop for silent transport stalls, and two minutes keeps eval wall time bounded.
2. **Per-attempt ceiling stays 10 minutes.** It moves from "the whole `Stream` call including backoff" to "one attempt", and hitting it is now a retryable failure inside the regeneration budget instead of the end of the turn. A healthy generation that needs more than ten minutes still fails after the budget; the eval report will name `stalled` and `call ceiling` separately so we can see whether that case exists before widening it.
3. **Regenerate after a mid-stream failure, up to 2 regenerations per logical call**, counted separately from the 8-attempt pre-token budget. Each regeneration re-bills the prompt and discards the partial; the UI is told.
4. **No root-turn auto-retry.** Client-level retries span minutes of provider trouble; a persistent outage surfaces as a failed turn. Children keep their re-wake, now capped.

Not up for decision: caller cancellation is never retried; context-limit rejections keep going to compaction; permanent 4xx (auth, billing, invalid request) fail fast; every attempt is still settled independently in accounting; partial output that is finally abandoned is still preserved as `[response interrupted]`.

## Items

### 1. Stall-based deadlines replace the ten-minute totals

Why: gap 1 and 4. Files: `internal/llm/openai.go` (`New`, `callContext`, `streamOnce`, `completeOnce`), `internal/llm/accounting.go` (`runAttempt`), `responses.go` and `subscription.go` request sites.

- `New()` sets `http.Client{Timeout: 0}` and a transport with `ResponseHeaderTimeout` = stall timeout. Header timeout failures are transport errors and already retryable.
- A `stallReader` wraps the response body: every `Read` resets a timer; when the timer fires it cancels the attempt's request context with `context.WithCancelCause(ctx)` and a typed `stallError{Elapsed}`. The scanner's error is mapped through `context.Cause` so callers see `stallError`, not `context.Canceled`.
- `retryable()` returns true for `stallError` and `ceilingError`; it keeps returning false for a caller's `context.Canceled` and for a deadline the caller set.
- `callContext` applies the ten-minute ceiling (decision 2) per attempt, not per `Stream` call, so backoff and earlier attempts no longer eat generation time. The reservation gets that ceiling as `Timeout`, which keeps `AdmitModelCall` and terminal recovery unchanged. Our own ceiling is signalled as a typed `ceilingError`, retryable within the regeneration budget; a deadline set by the caller stays non-retryable.
- `Complete`, the Responses stream, and the subscription stream read through the same `stallReader`.

Size: about 80 lines. Tests: a fake server that sends two deltas, pauses past the stall timeout, and on the next request streams to completion (turn succeeds, two attempts settled, `OnRetry` fired with the stall message); a fake server that streams slowly for longer than the old ceiling and completes (no error); header timeout retried; a cancelled caller context never retried.

### 2. Regenerate after a mid-stream failure

Why: gap 2. Files: `openai.go` (`Stream` loop, `streamOnce`), `internal/agent/agent.go`, `internal/daemon/agent_session.go`, web and TUI stream handlers.

- The `emitted` guard in `Stream` becomes a budget check: after a delta, a retryable failure is allowed `regenerations` more attempts (decision 3), counted separately from pre-token attempts. `preTokenError` folds into the general classification of item 3.
- Each attempt starts from an empty accumulated message (already true: `streamOnce` builds `msg` fresh). The partial from a failed attempt is kept only in `RetryEvent` (`Discarded` text length) so the UI can say what was thrown away.
- `RetryEvent` gains `AfterOutput bool`. The daemon publishes the existing `stream.notice` ("response interrupted after N characters; regenerating in Ns") and a new `stream.discard` event; web and TUI clear the current assistant draft on `stream.discard`. Transcripts never see the discarded partial; only the final attempt's message is appended to history.
- When the regeneration budget is exhausted, behaviour is today's: `preserveModelResponse` appends the partial with `[response interrupted]` and the turn fails.

Size: about 60 lines in `llm` and `agent`, about 30 across the two UIs. Tests: mid-stream SSE error chunk then a good stream (final message is the regenerated one, history has no partial, `stream.discard` observed); budget exhausted preserves the partial and returns the error; a permanent error mid-stream is not regenerated; accounting shows one failed and one succeeded attempt.

### 3. Classify provider-wrapped errors by text

Why: gap 3. Files: `openai.go` (`streamOnce` error chunk, `completeOnce` error, `responseError`), new `internal/llm/classify.go`.

- One `classify(message string) class` with two pattern lists, taken from pi's and opencode's lists and trimmed to what OpenAI-compatible gateways send: retryable (rate limit, overloaded, too many requests, 429/5xx codes in text, timeout or timed out, no next token, connection reset or refused, upstream, temporarily unavailable, try again, provider returned error, stream ended before), permanent (invalid api key, unauthorized, insufficient quota or balance, billing, unsupported model, invalid request). Unknown text is treated as retryable before the first delta (one attempt, today's behaviour) and permanent after it.
- Applied to SSE error chunks in both stream paths, to `out.Error` in `completeOnce`, and to 200-status error bodies. HTTP status stays authoritative when present; the text list decides only when the provider answered 200 or when a 4xx body says "rate limit".
- `Retry-After` is read from 429 and 503 response headers into `HTTPError.RetryAfter` (today only some paths set it), capped at 60 s; a longer server-requested wait fails fast with the requested delay in the message, as pi does.

Size: about 50 lines. Tests: table of messages to classes; `Retry-After` honoured and capped.

### 4. Backoff and budget housekeeping

Why: keep one policy. Files: `openai.go`.

- Pre-token budget stays `maxRetries` (default 8); regeneration budget is decision 3; backoff stays 1 s doubling to 20 s with jitter (opencode's 2 s to 30 s is not materially different).
- `RetryEvent.Max` reports the budget that applies to the current phase so the UI text is honest.

Size: about 15 lines.

### 5. Turn-level fallback

Why: gap 5. Files: `internal/daemon/recursive_runtime.go`, possibly `cmd/whip/run.go`.

- Children: cap `failures` (proposed 6, about 10 minutes of re-wakes at the 64 s cap) so a dead provider does not keep a child alive forever; the parent gets today's completion notice with the last error.
- Root: per decision 4, no automatic re-run.

Size: 10 lines for the cap.

### 6. Eval harness follow-through

`report.termination` provider tokens gain the new wording (`stalled`, `call ceiling`, `regenerat`); attribution stays `provider_error`. The next campaign runs against a whip built after items 1 to 3 and shows whether the two deadline losses disappear. If `call ceiling` still appears on attempts that were producing tokens, revisit decision 2.

### 7. Docs

`docs/agent-loop.md` retry paragraph and `docs/features.md:234` describe stall deadlines, regeneration, and the visible retry events; `internal/config` comment on `maxRetries` mentions the regeneration budget.

## Verification

1. `go test ./internal/llm/ ./internal/agent/ ./internal/daemon/` green (the pre-existing `TestQueuedMailWakesIdleRootWithoutHumanInput` failure excepted).
2. Manual: `whip run` against a local fake provider that stalls once mid-answer and then completes; the run finishes, the notice appears, the transcript has one assistant message.
3. An eval smoke on Modal (8 tasks) built from the finished branch, then the next full campaign; compare `provider_error` counts and the split between `stalled` and `call ceiling` in whip errors.

## Preserved, changed, not built

- **Preserved:** independent settlement of every attempt in accounting; exactly-once tool execution (a regenerated message's tool calls run once, after the final attempt); context-limit handling through compaction; caller cancellation semantics; `maxRetries` configuration; child re-wake behaviour (now capped); partial output kept as `[response interrupted]` when all attempts fail.
- **Changed:** no fixed ten-minute total (stall timeout plus a long safety ceiling); mid-stream failures are regenerated within a small budget; provider-wrapped errors are classified by text; `Retry-After` honoured on headers; new `stream.discard` UI event; children stop retrying after a cap.
- **Not built:** turn-level retry for root turns (decision 4); a longer per-attempt ceiling (decision 2, revisit with data); per-provider timeout configuration (constants first, a knob only if a provider needs it); any change to task, model, or grading in the evals; retrying after the model's own `finish_reason: length`.

## Sequencing and effort

Item 1 first (it removes both campaign losses on its own), then 3, then 2 (which depends on 3's classification and adds the UI event), then 4 and 5, then 6 and 7. About one day of work plus tests; the UI event is the only change outside `internal/llm`, `internal/agent`, and `internal/daemon`.

## Execution log

- 2026-09-14, item 1 (`572776a7`): `internal/llm/stall.go` adds the stall watchdog (`Client.do`, `stallBody`), typed `stallError` and `ceilingError`, per-path defaults (120 s chat, 300 s Responses and subscription, `Client.StallTimeout` overrides both), and `Client.AttemptCeiling`. `New()` no longer sets an HTTP total timeout; `callContext` is gone and `runAttempt` applies the ceiling per attempt with `context.WithTimeoutCause`, or the caller's nearer deadline without a cause so it stays non-retryable. Two old-policy tests replaced (`TestCallerDeadlineBoundsBackoff`; the budget-shortened permit is retried). Test servers must drain the request body before blocking or Go's server never notices the client hang up.
- 2026-09-14, items 2 to 7 (one commit): `classify.go` classifies provider wording (permanent wins over transient; unknown before a delta keeps the one-repeat `preTokenError`, unknown after a delta is not regenerated); `responseError` reads `Retry-After` capped at one minute; `Stream` regenerates after a delta within `Client.Regenerations` (default `DefaultRegenerations = 2`) and reports `Regenerating`, `Discarded`, `Regeneration`, `Regenerations` on `RetryEvent`; the daemon emits `stream.discard` (payload text = discarded characters, with `turn_id`) followed by a `stream.notice`; the TUI flushes shown text and drops queued tool rows, the web app removes the turn's live text, reasoning and tool rows, the SDK resets its accumulated text and yields a `discard` turn event, `whip run` prints a `discard` JSON event or a notice. `internal/protocol` lists the kind and the TypeScript contract was regenerated. Child re-wakes are capped at `maxChildRetryWakes = 6`. The eval report attributes `provider stream stalled` and `per-attempt ceiling` as `provider_error`. Docs: `docs/agent-loop.md`, `docs/features.md`, the `maxRetries` comment. Old-policy tests replaced: partial output is regenerated within the budget and every attempt settled; the subscription stream regenerates within the same budget. `TestProtocolBoundsInitializationConnectionsAndInFlightWork` failed once under machine load during `npm ci` and passed three times in isolation.
- 2026-09-14 verification: box (kuzco-4090) at `0300fde6`: `go build ./...`, `internal/llm`, `internal/agent`, `internal/tui`, `internal/protocol` green, 74 eval tests green; `internal/daemon` green except the box-only `TestQueuedMailWakesIdleRootWithoutHumanInput`, which fails there before this work too. Locally: `npm run check -w @whip/protocol` (contract regenerated, drift clean), `npm run check -w @whip/sdk`, `npm run test:web` green. End-to-end: `whip run --format json` against a local fake provider that streamed `Hel` then an error chunk reading `No next token received` produced `text Hel`, `discard {chars: 3}`, `text hello`, `done hello`, exit 0; the provider saw two requests. Modal smoke `modal-smoke-20260914-s2` (8 tasks, jobs 8, `kimi-k3-fast`, whip ref `0300fde68`, 2026-09-15 00:03 to 00:20 UTC): status `complete`, 7/8 passed (`anko-typed-variable-bindings` failed on grading), 8/8 graded and evidence complete, 6/8 accounting complete, known cost $17.97, 0 provider errors, fetch 1,530 files then pruned. Two model attempts failed fast (36.5 s and 12.1 s) and were retried before any delta; no stall, ceiling, or regeneration event occurred, so this smoke exercises the ordinary retry path only. The next full campaign is the measurement for the two ceiling losses.
