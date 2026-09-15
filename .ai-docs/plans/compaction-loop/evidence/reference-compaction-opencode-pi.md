# How opencode and pi handle compaction and exhaustion

Shallow clones at `src/rlm/reference/opencode` (branch dev, 631f67a, 2026-09-13) and `src/rlm/reference/pi-mono` (main, 71dca87, 2026-09-11).

| | whip today | opencode | pi |
|---|---|---|---|
| Trigger | before every model call once the last reported prompt ≥ 50% of the window | after every model call once total tokens ≥ context − min(20K, max output) | after the tool loop ends (`agent_end`) or before a prompt, once tokens > window − 16K; mid-loop only via an overflow error |
| Kept tail | 25% of headroom clamped 2K–15K; whole turns; newest turn always kept whole | same 2K–15K formula; `splitTurn` cuts inside the newest turn at the earliest message whose remainder fits; if nothing fits, keeps nothing | flat 20K; cut at user or assistant messages, never tool results; a split turn gets its own "Turn Context (split turn)" summary appended to the history summary |
| Opening user message of a split turn | n/a | folded into the summary | folded into the turn-prefix summary |
| Fold cannot shrink | re-fires every round (the bug) | designed out: a fold always yields summary + ≤15K tail | `prepareCompaction` returns undefined when nothing is summarizable: no model call, no-op |
| Summary request itself too large or failing | n/a | `ContextOverflowError` "Session too large to compact", loop stops | `compaction_end` with `errorMessage`, `session_compact_failed`; no retry |
| Provider overflow error | reactive fold + one retry, then the error surfaces | compaction with `overflow` flag, replays the last user message; still too large → stop with error | one compact-and-retry (`_overflowRecoveryAttempted`), then "Context overflow recovery failed after one compact-and-retry attempt" |
| Cheap first step | none | `prune`: erases completed tool outputs older than 40K protected tokens, skipping the newest turn, no model call | none |

Files: `opencode/packages/opencode/src/session/{compaction.ts,overflow.ts,prompt.ts}`; `pi-mono/packages/coding-agent/src/core/compaction/compaction.ts`, `pi-mono/packages/coding-agent/src/core/agent-session.ts` (`_checkCompaction`, `_runAutoCompaction`), `pi-mono/packages/ai/src/utils/overflow.ts`.

Takeaways for whip: both make the fold always shrink and treat "compaction itself cannot proceed" as an immediate, clearly worded failure. Neither keeps working after an exhausted fold, because exhaustion cannot occur by construction. Neither pins the split turn's user message.
