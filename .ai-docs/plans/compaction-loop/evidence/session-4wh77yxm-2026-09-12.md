# Evidence: reviewer compaction loop, session 4wh77yxmkhnqk72vecdq (kuzco-4090)

Source: `~/.whip/runtime-v2/sessions.db` on kuzco-4090, read 2026-09-12 03:57Z to 04:19Z.
Build: `desktop-v0.2.0-beta.2-53-g01f2db6`. Model gpt-6-astra via openai-codex, `compactModel` configured as deepseek-v4-flash-0731 (inference-net).

## The loop, agent `modal-eval-review` (3e7osb37), turn started 03:34:41Z

`model_calls` for the turn, created_at / purpose / whip estimate / provider prompt tokens / wall ms:

```
03:41:06 turn        in=146545 pt=132870 ms=10893
03:41:18 compaction  in= 20525 pt= 22209 ms=164965   <- folds prior turns; works
03:44:03 turn        in= 83308 pt= 91121 ms=9497
...  22 turn calls, context grows ~2-4K per call ...
03:50:04 turn        in=124612 pt=130771 ms=9100
03:50:13 compaction  in= 13964 pt= 14281 ms=160552   <- nothing left to fold but the current turn
03:52:54 turn        in=125276 pt=130962 ms=14160    <- no reduction
03:53:09 compaction  in= 13845 pt= 14202 ms=160914
03:55:50 turn        in=127147 pt=132483 ms=15708
03:56:06 compaction  in= 13816 pt= 14192 ms=159676
03:58:45 turn        in=129854 pt=135354 ms=28379
03:59:14 compaction  in= 13811 pt= 14191 ms=160069
04:01:54 turn        in=131456 pt=136244 ms=13213
04:02:08 compaction  in= 13807 ...
```

Turn cancelled from a client at 04:13:46Z (`agent.turn.cancel`); 52 model calls, 8 compactions.
Every compaction attempt ran on gpt-6-astra/openai-codex (attempt blob `Model`/`Provider`), not the configured compact model. The daemon process environment has no `INFERENCE_API_KEY`.

## What parents could see

- Root's last turn ended 03:34:59Z, the lead's 03:34:49Z; root's self-schedule ended 23:16Z the day before. No agent had a turn between 03:35Z and the cancel.
- The lead's audit request to the reviewer (created 03:34:41Z) stayed `pending` for 40 minutes: mail is delivered at turn start.
- `agents.list()` for a running child exposes `status=running` and `last_turn.started_at`; nothing about call count, compactions, or last progress.
- Long turns are normal in this session: `eval-execution` had turns of 373 and 122 minutes.
- Compaction leaves no mark on the `agents` row. `stream.notice` "compacting context…" is UI-only; `compactions` rows are written at turn commit.

## Session totals

4,428 model calls, ~335M tokens by whip's tally, 22.1 h of model-call wall time, 60 compactions totalling 2.1 h.
