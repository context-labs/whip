# Transcript research — September 17, 2026

User requested retaining the history-gap diagnosis while investigating a second
report. Research only; neither underlying issue is resolved by these notes.
Canonical architecture remains in `docs/frontend.md`.

## Confirmed: missing history interval leaves old operations at the chat tail

Evidence: read-only queries against the user's local transcript database and an
in-memory replay through the SDK execution projection.

- Conversation `p6lpamxi6jbdxfgfwfiq`; screenshot at 18:34, reply in turn 23.
- Before turn 21, the recent loaded history ended at record 1588.
- Turn 21 committed through record 1658. Snapshot refresh is bounded to 64
  messages and a 192 KiB message budget; only records 1601–1658 fit.
- `SessionView.synchronize` merges the old and new windows without detecting
  the missing interval 1589–1600 (`packages/sdk/src/state.ts`).
- Five observed executions remain unplaced: turn 21 parts p2, p4, p7, p10, p13.
  Their counts and partial flags match all five screenshot groups exactly:
  3 reads (partial); 1 command + 4 searches; 1 command + 1 read + 1 other tool
  (partial); 1 command (partial); 1 command.
- Supplying the missing durable records to `reconcileExecutions` makes the
  unmatched count fall from five to zero. Their identities and bodies are saved
  correctly. This is a client history-loading problem, not lost stored data.
- Backward paging uses the oldest loaded record as its cursor; it can skip an
  interior hole indefinitely.
- The earlier local UI guard only stops unmatched old evidence from appearing
  under later responses. It does not recover the missing history.

Required future work: SDK gap detection and bounded recovery of missing ranges,
with correct paging/retention behavior. Do not merely increase limits, infer
positions from reused tool IDs, or delete observed evidence. Test turns exceeding
both count and byte limits, reconnect, and interior gaps with existing history.

## Investigating: final reply appeared, then disappeared

Evidence: screenshot at 23:17:19; conversation `msmjemaex3za4ztluocq`, website
request at record 31, completed turn 7.

- Final assistant response is durably saved at record 42, part
  `msmjemaex3za4ztluocq:turn:7:p15`.
- Its 303 characters / 307 UTF-8 bytes match all 60 streamed text deltas exactly.
- Streaming ended at 23:17:10.895936 MDT; turn success was committed at
  23:17:11.258094 MDT. No regeneration/discard event was present.
- All 42 records total 69,603 public JSON bytes and fit in the recent snapshot.
  The confirmed history-gap mechanism above does not explain this case.
- Read-only replay of the actual events through the SDK reducer and the app's
  transcript projection retained all 303 characters across completion events.
  The saved projection has the same text and row identity.
- Current Whip accessibility state includes all three final paragraphs and their
  copy action. No navigation, refresh, input edits or submission was performed
  while inspecting it. User's draft remains `e`.
- Exact disappearance has not been reproduced. Layout/scroll/virtualization or a
  transient live-to-history transition remain hypotheses, not a confirmed cause.
- Asked whether the reply stayed missing, returned on its own, or returned after
  switching/reopening. No answer received at the time of this note.

Next evidence needed: observe the transition when it happens, distinguishing a
missing data row from an unmounted/hidden DOM row or a scroll-position change.
Do not claim a fix or attribute it to fades without reproduction.
