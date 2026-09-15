# Compaction loop and silent unhealthy agents

Status: implemented 2026-09-13 in the working tree (uncommitted); Go, protocol, and web suites green. See Verification results.
Evidence: [evidence/session-4wh77yxm-2026-09-12.md](evidence/session-4wh77yxm-2026-09-12.md) (the incident) and [evidence/reference-compaction-opencode-pi.md](evidence/reference-compaction-opencode-pi.md) (how opencode and pi behave; clones under `src/rlm/reference/`).

## Why this matters

A sub-agent spent 40 minutes and 52 model calls making about six tool calls of progress. Whip's compaction could not fold a single tool-heavy turn, so it re-ran a 160-second no-op summary after every model call, on the wrong model. No parent and no human learned anything until a client cancelled the turn. Two defects, fixed together here:

- Compaction cannot shrink an oversized turn and does not notice that a fold failed to shrink anything.
- A child's health is invisible to its parent while a turn is in flight, and the configured compaction model silently fell back to the expensive one.

## Decisions

1. **Pin the turn-opening user message** when a fold cuts inside a turn (item 1). A child must keep acting on its exact orders and authorization text, not a paraphrase. Cost ~1K tokens for a mailbox digest. Stricter than opencode and pi, which fold it; chosen deliberately.
2. **Keep the 15K tail ceiling** (`compactTailMaxTokens`) for mid-turn folds (item 1). One constant; dropped detail stays reachable through raw-history references, and live-turn messages are readable via `context.history` as provisional rows.
3. **Fail at the window, stall at the threshold** (item 2; refined during implementation). Whip's threshold is 50% of the window, unlike opencode and pi which fold near the edge, so "cannot get under the threshold" is not "cannot fit": a 40K pasted prompt on a 128K model would fail a turn that works today. A fold that ends over the threshold but inside the window stalls further proactive folds for the turn. The window itself is judged by the provider's rejection, not the chars/4 estimate; a rejection with nothing left to fold fails the turn with `ErrCompactionExhausted`. The reactive one-retry is decoupled from proactive folds so a stalled turn still gets it.
4. **No tool-output pruning** (opencode's `prune`) in this plan. Items 1 and 2 remove the loop; pruning is a second mechanism to maintain. Revisit if tool-heavy turns still fold too often.
5. **Compaction-model fallback is logged, not shown in UI** (item 3). One whip.log line per session with model, provider, and the resolver's error. The fallback stays: the default compact model needs a credential many installs lack.
6. **Per-turn counters are written through into `last_turn`** from `model.call.*` events (item 4). `projectTurnEvent` already owns `last_turn`; the UI already renders it; the per-call agents-row update is marginal next to the per-call events clients already receive.
7. **No threshold push to the parent** (item 4). It would wake a ~100K-token parent turn for healthy long work too. Loud failure plus an informed poll is what both references rely on.
8. **No child round cap** (item 5). Spawn budgets (`tokens`, `elapsed`, `cost`) already bound a child with a loud failure; a default `MaxTurns` would also cut healthy supervision turns. Guide text left unchanged.
9. **Verification includes a one-off replay** of the real reviewer transcript through the new tail selection (item 6). Throwaway, not committed.
10. **One change for everything.** Fewer moving parts to land; the diff spans `internal/agent`, `cmd/whip`, `internal/session`, the guide fragment, and the two frontends' `last_turn` rendering.

## Items

### 1. Fold inside an oversized turn

**Why.** `compactTailStart` ([internal/agent/agent.go](../../../internal/agent/agent.go), ~line 884) walks back whole user turns and always keeps the newest one. A turn of 40+ tool exchanges is ~120K tokens, so a fold can only touch the prior summary and a few earlier messages. Context never gets under the threshold.

**Target.** When the newest turn alone exceeds the tail budget, the boundary may fall inside it on an assistant/tool-pair boundary. The older part of the turn folds into the summary; the pinned opening message and the kept pairs follow it.

**Steps.**
1. In `compactTailStart`, while walking back through the newest turn, remember the most recent assistant-message index as a fallback boundary. If the budget is exhausted before reaching that turn's user message, return the fallback instead of the user index. Whole pairs only; a boundary never lands on a `tool` message (the orphan-safety walk in `compact` stays as the backstop).
2. In `compact`, when the boundary is inside a turn, locate that turn's user message and rebuild as: system prompt, summary, pinned user message, kept pairs. The pinned message is excluded from the summarized `history` and from the "nothing to fold" check in item 2.
3. After such a fold the pinned message is the newest turn's user message for the next walk, so repeated mid-turn folds keep working without special cases.
4. No prompt change: `buildSummaryPrompt` already asks for "Active (with the exact next step)" and keeps raw-history references.
5. Reload path: `applyCompaction` (internal/session/session.go) rebuilds the view as system prompt, latest summary, then raw rows from the fold point. A pinned opening message sits *before* the cutoff in raw order, so on reload it would vanish. Add the derived rule there: when the fold point is not a user message, re-include the nearest preceding user message right after the summary. No schema change; the `compactions` row stays (seq, cutoff, summary).

**Tests.** New: a single user turn of many tool pairs over the limit folds under the threshold, keeps the opening user message verbatim right after the summary, and leaves no orphaned tool result; a second fold on the same turn works. Reload: `LoadAgentTranscript` after a mid-turn fold yields the same view the live agent had (summary, pinned user message, kept pairs). Keep green: `TestCompactKeepsToolCallPair`, `TestProactiveCompactAtFiftyPercent`, `TestCompactThresholdExplicitOverride`, `TestNoProactiveCompactBelowThresholdOrWithoutLimit`, the `context_journal_test.go` compaction tests.

**Trade-offs.** The summary now describes an in-flight turn, so mid-turn fidelity depends on the summary model; raw references and the pinned message bound the damage. Reference behaviour: opencode `splitTurn` cuts at the earliest message whose remainder fits and keeps nothing if none fits; pi cuts to a flat 20K at user or assistant messages and gives the turn prefix its own summary section.

### 2. Stop the no-op re-fold; fail loudly at the window

**Why.** `maybeCompact` never consulted any flag on the proactive path; after a fold it reset the reported prompt size, the next response reported over the threshold again, and it folded again. Nothing asked whether the fold shrank anything. Each no-op cost ~160 s.

**Target (decision 3).** A fold with nothing new to summarize makes no model call. A fold that ends at or over the threshold stalls proactive compaction for the rest of the turn. A provider context-limit rejection still gets one fold-and-retry per turn regardless of earlier proactive folds; when that fold finds nothing left, the turn fails with `ErrCompactionExhausted` ("context cannot be compacted to fit the model window") and the existing failure path posts the notice to the parent and records `last_turn.error`. Reference behaviour: [evidence/reference-compaction-opencode-pi.md](evidence/reference-compaction-opencode-pi.md).

**Steps (as implemented).**
1. `compact` returns the `errNoHistory` sentinel before any model call when nothing foldable remains after peeling the prior summary and excluding the pinned message.
2. `maybeCompact` skips while `compactStalled`; after a fold, or on `errNoHistory`, the stall is set when the estimate is still at or over the threshold. No estimate-based failure.
3. `retriedOverflow` guards the reactive retry; `compacted` keeps only its final-round-skip meaning. Both reset with the stall at turn end and in `finalAnswer`.
4. In the reactive path, `errNoHistory` becomes `ErrCompactionExhausted` wrapping the provider error; the text passes through `postCompletionNotice` unchanged.

**Tests.** `TestMaybeCompactStallsInsteadOfRefolding`, `TestMaybeCompactNothingToFoldMakesNoModelCall`, `TestTurnFailsWithCompactionExhaustedWhenNothingFolds`, `TestStalledTurnStillGetsOneOverflowRetry`. Kept green: `TestCompactDoesNotLoopOnRepeatedContextLimit`, `TestCompactTooLittleHistory`, `TestMaybeCompactUsesRealUsage`, `TestMaybeCompactEstimateFallback`, `TestTurnAutoCompactsOnContextLimit`.

**Trade-offs.** A stalled turn keeps making large calls until the edge; that is the price of not trusting the estimate to fail a turn. Matches where opencode ("Session too large to compact") and pi ("Context overflow recovery failed after one compact-and-retry attempt") fail.

### 3. Log the compaction-model fallback; fix the credential on kuzco-4090

**Why.** [cmd/whip/daemon.go](../../../cmd/whip/daemon.go) (~line 160) swallows the compact-route resolution error, so every summary ran on the conversation model. The resolver already names the missing key; the message goes nowhere.

**Target.** One whip.log line per session: compact model, provider, and the resolver's error, whenever the fallback engages.

**Steps.**
1. Log `resolveErr` through the whip.log writer (export a small `config.Logf`, or reuse the daemon's logger). No behaviour change.
2. Operational, outside the code change: on kuzco-4090 provide the inference-net credential through a supported source (`whip auth inference-net login`, an env file, or the daemon environment) and confirm the log line stops appearing.

**Tests.** A unit test that the log line is emitted when resolution fails is optional; the logger is a side effect. Keep green: `cmd/whip` tests.

### 4. Make an unhealthy child visible to its parent

**Why.** A parent learns about a child from a turn-end notice or by polling `agents.list()`. For a running child `last_turn` shows only `started_at`; "working for 40 minutes" and "looping for 40 minutes" look identical. Long turns are normal in whip (373 minutes in this session), so duration alone tells nothing.

**Target.** `last_turn` carries `model_calls`, `compactions`, and `last_activity_at` for the turn in flight. `agents.list()` and `agents.inspect()` serialize it already; the desktop and TUI already render `last_turn`.

**Steps.**
1. Add the three fields to `TurnOutcome` ([internal/session/turn_outcome.go](../../../internal/session/turn_outcome.go)).
2. Extend `projectTurnEvent` to accept `model.call.started` and `model.call.settled`: on `started`, increment `model_calls` or `compactions` by `purpose`; on both, stamp `last_activity_at`. `turn.started`/`agent.turn.started` reset them. Ignore events whose turn is not the current one.
3. One sentence in the agents fragment of [internal/rlm/guide_fragments.go](../../../internal/rlm/guide_fragments.go): what the fields mean, and that `agents.stop` or a steer message is the response to a child that is compacting repeatedly or has gone quiet.
4. Show the counters where `last_turn` is already rendered in `packages/app` (chat-activity, conversation, repl-view) and the TUI agent view.

**Tests.** New in `internal/session`: model-call events increment the counters on the running turn and a new turn resets them. Keep green: `turn_outcome` and `snapshot_view` tests, `packages/app` tests that touch `last_turn`.

**Trade-offs.** One `UPDATE agents` per model call and a collection-revision bump per call (the `collection_agents_update` trigger). Clients already receive an event per model call. Compute-on-read was rejected: four query sites and a timestamp join.

### 5. Child round cap: skipped

`MaxTurns` is only set on the root client run path. Spawn budgets already bound a child by `tokens`, `elapsed`, or `cost`, and exhaustion fails the turn loudly. A default cap would duplicate that with a blunter tool. Nothing to do (decision 8).

### 6. Verification results (2026-09-13)

- `go test ./internal/... ./cmd/...`: green. `TestManualCompactionUsesRawSequencesAfterFocusAndRestart` was updated: its fixture's turns (~3000 tokens) exceed the 2000-token tail budget, so the fold now cuts inside the newest turn (cutoffs 25/29 instead of 24/28) and the pinned user message reappears on reload. `TestModelAccountingAcceptanceHelperFailureStopsCurrentCell` failed once under full-suite load with `database is locked` and passed on every isolated rerun; unrelated to this change. New tests: `TestCompactTailStartSplitsOversizedNewestTurn`, `TestCompactFoldsInsideOversizedTurnAndPinsOpeningMessage`, `TestMaybeCompactStallsInsteadOfRefolding`, `TestMaybeCompactNothingToFoldMakesNoModelCall`, `TestTurnFailsWithCompactionExhaustedWhenNothingFolds`, `TestStalledTurnStillGetsOneOverflowRetry` (agent); `TestApplyCompactionRePinsOpeningMessageOfSplitTurn`, `TestTurnOutcomeCountsModelCallsAndCompactions` (session). Three session tests with synthetic cutoffs at assistant messages now expect the re-pinned user message; guide goldens regenerated.
- Replay of the reviewer's real 03:14–03:29 turn (64 messages, 31 tool pairs, 136,638 estimated tokens, threshold 129,000, budget 15,000): boundary at raw seq 1584 (assistant), split, kept tail 8,946 tokens; after the fold 18,805 tokens, view = system prompt, summary, pinned user seq 1527, kept pairs; no orphaned tool result. Throwaway test removed.
- `npm run check` in packages/protocol (tsc, interop, drift), `tsc -p packages/app/tsconfig.json --noEmit`, and `npm run test:web` (602 tests): green.
- Not done here: the operational credential fix on kuzco-4090. The TUI renders no `last_turn` today, so only the web app shows the counters.

### 6a. Verification plan (original)

1. `go test ./internal/agent/... ./internal/session/... ./cmd/whip/...` and the `packages/app` test suite.
2. Replay: load the dumped reviewer transcript (1,594 `llm.Message` rows from the incident, in the session scratchpad) into a throwaway test or `go run`, run the new `compactTailStart` and `EstimateTokens`, and report where the boundary lands and the post-fold size. Expected: boundary inside the final turn on an assistant message, post-fold estimate under the threshold. Not committed.
3. Manual: spawn a child with a tool-heavy prompt, watch `agents.inspect()` show `model_calls` and `compactions` climbing, and confirm a fold appears in the transcript as summary, pinned message, kept pairs.

## Implementation order (single change)

1. Item 1 and item 2 in `internal/agent/agent.go` with their tests; they share the boundary logic.
2. Item 3 log line in `cmd/whip/daemon.go`.
3. Item 4 store change, guide sentence, and frontend rendering.
4. Verification steps, including the replay.
5. Operational credential fix on kuzco-4090, separately from the code change.

## Preserved / changed / not built

- **Preserved:** threshold semantics (`compactPct`, 50% default), incremental summaries and their prompt, orphan safety, reactive context-limit retry, report modes, `agents.list` shape (additive fields only), spawn budgets and their guide text.
- **Changed:** tail selection inside an oversized turn with a pinned opening message; no-op fold detection with an immediate named failure; compact-route fallback logging; `last_turn` gains three fields and the UI shows them.
- **Not built:** watchdog or heartbeat tables, threshold pushes to parents, default child round caps, tool-output pruning, a keep-working-past-exhaustion mode, UI surfacing of the compaction-model fallback.
