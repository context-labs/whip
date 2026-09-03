# Handoff: whip context/token cost reduction

**Audience:** another AI (or engineer) picking this up cold. Everything you
need — the evidence, the design, what shipped, what's left, and how to verify.

**Repo:** `github.com/context-labs/whip` · **Branch:** `perf/context-cost-management`
· **PR:** #113 (open, CI green) · **Plan doc:** `docs/context-cost-plan.md`

---

## 1. Why this exists — the incident

Session `7ec5ba63` (whip's own SQLite session store, `~/.whip/sessions.db`)
ran kimi-k3 for 48h building a dashboard. Provider-truth spend:

- **2,096 API calls** (731 main-loop + 1,365 subagent)
- **283M input tokens served** (264M cached, 19M fresh), **~1.5M output**
- **≈ $210** at inference-net kimi-k3 rates ($3.95/M in, $0.40/M cache-read,
  $19.8/M out). The whip UI **reported ~$413** — inflated by double-counting.

The user had `compactPct: 30` set (compact at 30% of the 1M context window),
yet **only two compactions fired in 48 hours** while per-request prompt tokens
climbed to 392k.

### Root causes (each verified against the session DB, not guessed)

| # | Root cause | Evidence |
|---|-----------|----------|
| 1 | Compaction trigger ran on `EstimateTokens` (chars/4 + flat 1200/image), which peaked at **296k/1M (28%)** while the provider billed **392k (37%)** | Replayed session through whip's own estimator: never crossed the 30% threshold (314k), so `maybeCompact` correctly no-opped on broken inputs |
| 2 | Images counted at flat 1200 tokens regardless of size | 4 real screenshots cost **4.1k–11.4k** tokens each (28px-patch vision model); whip counted 1.2k — **~7× undercount** |
| 3 | No image normalization at ingest | A **3410×2646** clipboard PNG went in verbatim; ~7.1M input tokens was re-sending old screenshots every request |
| 4 | Compaction tail = `compactKeepBack = 6` **messages** (not tokens); summary capped at **1024** output tokens with 500-char tool-result truncation | Six messages can be six 50KB tool dumps; 1024 clips a 2-day fold into near-uselessness |
| 5 | No doom-loop guard | One stuck subagent (`rewrite-flight-parser-structurally-4`) made **789 calls**, including **516× identical `sleep 280; git status`** (a **235-in-a-row** streak) — **85M tokens (~$35)** polling git |
| 6 | Usage double-counted | Subagent spend fanned into parent counters via `AddUsage` **and** persisted on task sessions → session row read **550M vs 283M** provider-truth (~2×); arithmetic fits `main + 3×subs ≈ 561M` |

Reference implementation consulted: **opencode** (`/home/abe/code/coding-harnesses/opencode`).
Key contrast: opencode triggers compaction off **provider-reported `usage`**
(`session/overflow.ts`, `session/processor.ts:435-484`), never the estimate;
caps images at 2000×2000/JPEG q80→40/≤5MiB (`image/image.ts`); token-budgets
its compaction tail (`compaction.ts:115-120`); and blocks the 3rd identical
tool call (`processor.ts:356-380`).

---

## 2. What shipped (all 7 items, on the branch)

Commit stack (oldest→newest):

```
ebd7516 docs: plan for context/token cost reduction
bfab024 feat(llm): pixel-true image token estimates + ingest normalization
a85be57 feat(agent): real-usage compaction trigger, budgeted tail, doom-loop guard, single-count usage
e57ad57 feat(tui): normalize pasted and @-mentioned images at ingest
f3ba3a8 lint: goimports grouping in images.go, drop predeclared 'real', remove unused encodePNG
fb61707 docs: handoff doc for the context-cost work
2d593bb fix(agent): lastPrompt only from own requests + reset on fold; refused calls fire tool events; cleanup
(HEAD)  feat(usage): per-model subagent ledger — accurate totals, persisted on session/task/compaction rows
```

### Item 1 — real-usage compaction trigger
`internal/agent/agent.go`
- `Agent.lastPrompt` (new field) records the provider-reported `prompt_tokens`
  of the agent's most recent **own** conversation request, set via
  `notePrompt` in the turn loop (not in `AddUsage`, which also receives
  foreground-subagent and summary-call usage). `compact` resets it to 0 so
  the estimate fallback drives the trigger until the next real request.
- `maybeCompact` fires when `lastPrompt ≥ threshold × ContextLimit`; the
  chars/4 estimate is the fallback only when the provider returns no usage.
- **Also closed the single-round-turn gap:** a final response whose usage
  crosses the threshold now compacts *before* the turn returns (skipped if the
  turn already compacted, so it never re-folds a fresh fold). Proactive
  compaction sets `a.compacted = true` to suppress the double-fold.

### Item 2 — pixel-based image estimates
`internal/llm/images.go`, `internal/llm/openai.go`
- `ContentPart` gains `W`/`H` (persisted in session JSON; stripped from the
  provider wire by a **fixed deep-copying `stripAuthored`** — see "bug found").
- `ImageTokens(w,h)` = `⌈w/28⌉·⌈h/28⌉`, floor 85, fallback 1200 for
  undecodable parts. `DecodeImageSize` reads headers via `image.DecodeConfig`
  (blank imports: png/jpeg/gif/webp/bmp). `PartTokens` routes image parts to
  the pixel estimate, lazy-decoding pre-dims session rows from the data URL.
- `EstimateTokens` (agent.go) now uses `llm.PartTokens` per part.

### Item 3 — image normalization at ingest
`internal/llm/normalize.go`; wired in `internal/tui/paste.go`
(`saveClipboardImage`) and `internal/tui/mentions.go` (`imageParts`).
- `NormalizeImage`: pass-through if already ≤2000×2000 and ≤5MiB; else decode
  → `x/image/draw` scale to fit → JPEG q80→40 ladder until ≤5MiB → shrink
  ×0.75 per round (≤32 rounds). Corrupt input passes through unchanged.
- New dependency: `golang.org/x/image v0.45.0` (promoted to direct via
  `go mod tidy`).

### Item 4 — token-budgeted compaction tail + incremental summaries
`internal/agent/agent.go`
- `compactKeepBack = 6` (messages) → `compactTailBudget()` = `min(15k,
  max(2k, usable×0.25))` where `usable = ContextLimit × (1 − threshold)`.
- `compactTailStart` walks **whole user turns** newest→oldest accumulating
  tokens until the next turn would bust the budget; the existing orphan
  tool-pair walk-back is preserved.
- Summary `MaxTokens` 1024 → **4096**. Summaries are **incremental**: a prior
  fold (marked by `summaryPrefix`) is passed to `buildSummaryPrompt(history,
  prior)` and merged forward instead of re-derived from truncated transcripts.

### Item 5 — image stripping in decay
`internal/agent/decay.go`
- New **Pass 3** in `decay()`: image parts past the hot window are swapped for
  a text placeholder naming dims + the spilled file path (`spillImage` writes
  the base64 payload to `$TMPDIR/whip-img-<pid>/`, mirroring bash's spill).
  The model can re-attach via `@<path>` if it genuinely needs the pixels.
  Text parts of the message stay inline.

### Item 6 — doom-loop guard
`internal/agent/agent.go` (`markDoomLoops`, `doomLoopRefusal`)
- Tracks the last (tool name + `\x00` + args) key and a consecutive-run
  counter. The **3rd consecutive identical** call (`doomLoopMaxRun = 3`)
  returns a refusal tool-result without executing. **Any different call
  resets the run** (so legit test→fix→test alternation is safe), and `wait`
  is exempt (repetition is its designed use).
- Marked in **issue order before the worker goroutines spawn**, so the
  parallel `runTools` batch can't scramble ordering.
- A refused call still fires `OnToolStart`/`OnToolEnd` so the TUI's queued
  row (opened on `OnToolCall`) closes instead of sticking at "⋯".

### Item 7 — usage accounting: accurate totals, counted once
`internal/agent/agent.go`, `internal/agent/subagent.go`, `internal/session/session.go`, `internal/tui/tui.go`
- `Agent.usage` = this agent's **own** requests (turns + its compaction
  summaries). `Agent.subUsage` = a per-model ledger (`"model @ provider"` →
  `llm.Usage`) of every subagent under it. `TotalUsage()` = own + subs.
- Every sub gets `usageSink = parent.AddSubUsage` in `newSub`. `AddUsage`
  and `AddSubUsage` forward through the sink, so foreground, background,
  follow-up, **nested** subs and their compaction calls all reach the root
  ledger exactly once. The old `OnUsage: parent.AddUsage` fan-ins are gone
  (they mixed sub spend into the parent's own counter — the ~2× inflation).
- **Persisted:** `sessions.usage_*` = own; new `sessions.sub_usage` JSON =
  the ledger. Task rows (`task-<parent>-<id>`) now get their own `usage_*`
  + `sub_usage` stamped via `BackgroundTask.SubUsage/SubSubUsage`.
  `compactions` gains `model` + `usage` columns (the summary request's bill).
  Resume and fork restore the ledger (`SetSubUsage`).
- **Displayed:** header/status/opencode sidebar/context doctor show
  `TotalUsage()`. `sessionCost` prices own spend at the session model's
  rates plus each ledger entry at **its** model's rates (falls back to any
  catalog that knows the model when the sub has no provider); if any
  component is unpriceable the cost segment hides rather than under-report.
- Old rows have an empty ledger; historical sub spend before this change is
  only recoverable from the task sessions' per-message `usage` blobs.

---

## 3. Bugs found *by the new tests* (not in the original plan)

1. **`stripAuthored` shallow-copied `Parts`.** Zeroing W/H on the wire copy
   would have mutated the caller's persisted history. Fixed with a proper
   slice copy. Covered by `TestContentPartDimsPersistedAndStripped`.
2. **End-of-turn compaction gap (item 1).** A single-round turn returned
   before round-2 `maybeCompact`, so the just-landed usage never triggered a
   fold. Closed (see item 1), with the `a.compacted` guard preventing a
   re-fold of a fresh fold.

3. **`lastPrompt` poisoning (review pass).** Setting it inside `AddUsage`
   let a foreground subagent's or the compaction summary call's
   `prompt_tokens` overwrite the parent's real context size, and the
   pre-fold value lingered after a fold so the next round could re-fold the
   fresh summary. Fixed: `notePrompt` from the turn loop only, reset in
   `compact`. Covered by `TestLastPromptIgnoresFannedInUsage` and an
   assertion in `TestMaybeCompactUsesRealUsage`.
4. **Refused doom-loop calls left a stuck TUI row.** The refusal short-cut
   skipped `OnToolStart`/`OnToolEnd`. Fixed; asserted in
   `TestDoomLoopRefusalSkipsExecution`.

## Pre-existing failures (NOT this branch)

`internal/tui` `TestStartupReportUnknownBgNotice` and
`TestStartupReportSkillsAndWarnings` fail on clean `main` (verified via a
`git worktree` checkout). They don't fail CI's `test` job. **Candidate
follow-up:** fix these two in a separate small PR.

---

## 4. Verification (how to reproduce)

```bash
cd /home/abe/code/whip
git checkout perf/context-cost-management
go build ./...          # clean
go vet ./...            # clean
golangci-lint run       # clean
go test ./...           # all pass except the 2 pre-existing internal/tui
                        # TestStartupReport* failures (fail on main too)
go test ./internal/agent/ ./internal/llm/ -count=1   # fresh, not cached
```

**CI:** PR #113 → 12/12 checks pass (lint, test, codeql ×2, driver,
govulncheck, `go`, build ×4 platforms).

**New tests by item:**
- 1: `TestMaybeCompactUsesRealUsage`, `TestMaybeCompactEstimateFallback`,
  `TestLastPromptIgnoresFannedInUsage`
- 2: `TestImageTokensPixelFormula`, `TestImagePartRecordsDimensions`,
  `TestContentPartDimsPersistedAndStripped`, `TestPartTokensLazyDecode`,
  `TestDecodeImageSize`
- 3: `TestNormalizeImage{OversizedShrinksToBudget,SmallPassthrough,CorruptPassthrough,NoiseFitsBudget}`
- 5: `TestDecayStripsColdImageParts`
- 6: `TestDoomLoopGuard`, `TestDoomLoopRefusalText`,
  `TestDoomLoopRefusalSkipsExecution`
- 7: `TestBackgroundTaskUsageNotDoubleCounted`, `TestSubUsageForwardsThroughNestedSubs`,
  `TestSubCompactionUsageReachesParentLedger`, foreground split in `TestTaskToolSpawnsSubagent`;
  session `TestRecordCompactionCarriesModelAndUsage` + sub_usage round-trip; TUI
  `TestSessionCostUsesFetchedPricing` (per-model sub pricing, unpriceable hides),
  `TestResumeRestoresUsage` (ledger persist/resume), `TestTaskRowCarriesSubUsage`
- Updated for the budgeted tail: `TestCompactKeepsToolCallPair`,
  `TestCompactionEventsCarryModelAndUsage`

**Non-goals held:** no tokenizer vendored; the 50KB tool-output truncation +
spill-to-disk is untouched.

---

## 5. What's left / possible follow-ups

1. **Merge PR #113** once reviewed. Watch loupe review / any human comments.
2. **Fix the 2 pre-existing TUI test failures** (separate PR; they fail on
   `main`, so landing them first de-noises this branch's CI signal).
3. **Optional quality pass on compaction:** opencode preserves the overflowed
   user message (media stripped) or injects a synthetic "continue" after a
   fold — whip currently re-derives from the tail. Evaluate whether that
   improves post-compaction continuation.
4. **Cache-hit observability:** the session had a 90.3% cache-hit rate; the
   TUI could surface a warning when the hit rate drops (a leading indicator
   of prefix churn / doom loops) — not implemented, just an idea from the
   analysis.
5. **Empirical $ validation:** after merge, rerun a comparable long session
   and diff `usage_in`/cost against the ~$210 baseline. The items target
   roughly a **$210 → ~$60–80** reduction on that workload profile.

---

## 6. Pointers for a fresh session

- Start here: `docs/context-cost-plan.md` (the plan, with per-item rationale)
  then this file (what shipped + how to verify).
- The cost forensics live in `~/.whip/sessions.db` (session `7ec5ba63`); the
  reconstruction used Python + `sqlite3` against the `messages` table (each
  assistant row carries a `usage` JSON blob).
- Branch is clean and rebased-safe: `git log --oneline origin/main..HEAD`
  shows the commit stack; nothing else diverges from `main` except these.
