# What Whip already records (inventory, 2026-09-14)

Read from the working tree on branch `compaction-loop-and-ui-cleanup` and from
the live runtime database on kuzco-4090 (`~/.whip/runtime-v2/sessions.db`,
read-only via python3 sqlite over ssh).

## Durable tables that are already span-shaped

| Table | What one row is | Timing columns | Identity it carries | Bodies |
| --- | --- | --- | --- | --- |
| `turns` | one agent turn (root or child) | `created_at`, `updated_at` (RFC3339, **seconds**) | `id` (root: `<agent>:<inboxSeq>` or `<agent>:mail:<nanos>`), `agent_id`, `trigger` | none |
| `model_calls` | one provider attempt | `created_at`, `updated_at` (seconds); `elapsed_millis` | `logical_id`, `attempt_number`, `agent_id`; **no turn id** | `attempt` blob (Model, Provider, Purpose, Pricing, InputTokens); `result` blob (prompt/completion/cached/reasoning tokens, cost) |
| `operations` | one admitted host operation (bash, files.*, mcp.call, ...) | `created_at`, `updated_at` (seconds) | `id = <cmd>:<round>:<toolCallID>:<rand>`, `agent_id`, `command_id`; **no turn id** | `payload` = `capability.Admission` (operation, full arguments); `result` (output, error) |
| `agents` | one agent for its whole life | `created_at`, `updated_at` | `id`, `parent_id`, `name`, `model`, `provider`, `last_turn` (TurnOutcome with model_calls, compactions, last_activity_at) | none |
| `messages` / `transcript_messages` | one raw transcript row (committed at **turn end**) | `sent_at` on human messages only | `seq`; assistant rows carry `usage`, `model`, `tool_calls[].duration_ms/exit_code`; **no link to `model_calls`** | full content |

`internal/session/session.go:173`: `func now() string { return time.Now().UTC().Format(time.RFC3339) }` — every `created_at`/`updated_at` above is whole-second.

## The event journal

`events(root_id, seq, kind, payload_inline|payload_ref, created_at)`; sliding window `EventRetention = 10_000` per root, replay limit 1,000, live delivery polls durable rows every 50 ms (`internal/daemon/subscription.go:80`), SDK batches notifications at ~16 ms.

Kinds that already mark span boundaries (payload type in `internal/protocol/events.go`):

| Boundary | start kind | end kind | payload fields available |
| --- | --- | --- | --- |
| root turn | `turn.started` | `turn.succeeded/failed/cancelled/interrupted` | `agent_id`, `turn_id`, `inbox_seq`, `error` |
| child turn | `agent.turn.started` | `agent.turn.*` | same |
| model attempt | `model.call.started` | `model.call.settled` (`.interrupted`, `.corrected`) | `model_call{id, logical_id, number, purpose, usage_source, cost_source, tokens (sum), cost_micros, elapsed_millis}`; **no model name, no prompt/completion split** (those are in the `model_calls.attempt/result` blobs) |
| model tool call (rlm_exec, bash, custom) | `stream.tool.started` | `stream.tool.completed` | `id`, `name`, `args`, `result`, `turn_id`, `agent_id`; `stream.tool.call` deltas stream the arguments |
| host call inside a cell | `stream.cell.host.started` | `stream.cell.host` | `id` (= tool call id), `invocation_id`, `name` = `module.operation`, `args` = summary, `host_status`, `result` = error, `text` = duration (ms, rounded) |
| permission wait | `permission.pending` | `permission.decide` outcome / `permission.auto_approved` | `operation_id`, `operation`, `canonical_path`, `command`, `rule` |
| human question | `question.pending` | `question.answered/closed` | question payload |

Emission path: worker goroutine → `session.emit` → `supervisor.post(workerEnvelope{stream})` → actor `recordStreamEvent` → `AppendRootEvent` (`internal/daemon/session.go:772`). The row's `created_at` is stamped at insert, not at the moment the callback fired.

Identity already threaded through tools: `tools.WithTurnIdentity` mints one random id per turn used as both `command_id` and `trace_id` on `capability.Request`; `WithOperationIdentity` prefixes operations with `<cmd>:<round>:<toolCallID>`. This "trace_id" is unrelated to the session `turn_id`.

## Live numbers (kuzco-4090, 2026-09-14)

```
sessions: 20
events per root (top): 4wh77yxmkhnqk72vecdq  10000 rows, seq 918026..928025   <- ~918K rows already evicted
retained event kinds: stream.tool.call 9726, stream.accounting 87, stream.cell.host.started 56, stream.cell.host 56,
                      model.call.settled 44, model.call.started 43, stream.reasoning 37, stream.usage 36, ...
model_calls: 4437 (4429 succeeded, 8 failed)      operations: 3605      turns: 876      agents: 29 (9 children)
stream.text share of retained events: 0.2%       <- RLM agents write code in tool-call argument deltas, not text
```

Sample rows:

```
model_calls.attempt: {"LogicalID":"ROY6COLLGGJOI5XRLYMS5OT6YR","Number":1,"Model":"gpt-6-astra","Provider":"openai-codex",
                      "Purpose":"turn","Pricing":{"prompt":"","completion":""},"InputTokens":9336,"MaxTokens":128000,...}
model_calls.result:  {"Usage":{"reported":true,"prompt_tokens":8806,"completion_tokens":13,"prompt_tokens_details":{"cached_tokens":0},
                      "completion_tokens_details":{"reasoning_tokens":0}},"Dispatched":true,"Elapsed":2888211301,"Failed":false}
model_calls.created_at/updated_at: 2026-09-13T19:44:12Z / 2026-09-13T19:44:15Z
stream.cell.host:    {"agent_id":"4wh7...","turn_id":"4wh7...:mail:1789186426608908055","invocation_id":"1245:1","host_status":"completed",
                      "id":"call_uwNP6ZzLOX8yfE5fmEBpDZPK","name":"agents.inspect","text":"41ms","args":"id=3e7osb37tlkrdzpbnb4q"}
operations.payload:  {"request":{"operation_id":"043f89e0d3ce07a2edbc4f39fb92284b:40:call_wVXkAbjh9yF5wuybg96166Hz:d4b4...",
                      "operation":"bash","arguments":{"command":"python3 -I -S -B - <<'PY'..."},...}}
```

## Frontend surfaces that exist

- `packages/sdk/src/executions.ts`: bounded "observed evidence" view (256 cells, 128 host calls per cell, 1 MiB) merged with loaded history; `ExecutionCell.observedStartedAt/EndedAt` are **client clocks** because events carry no precise time (`packages/app/src/execution-time.tsx` labels them "Observed").
- `packages/app/src/repl-view.tsx`: per-agent cell list (code, host calls with duration string, output, return value).
- Session view kinds: `chat` and `repl` (`packages/app/src/session-tabs.ts`, `?view=repl`); inspector sections in `packages/app/src/navigation.ts`.
- UI deps already present: `react-resizable-panels`, `@tanstack/react-virtual`, `@base-ui/react`, `lucide-react`, StyleX tokens. No charting or canvas library, and none is needed for a waterfall.
- Desktop is one renderer artifact of `packages/app`; nothing desktop-specific is required for a new view.

## No tracing dependencies today

`go.mod` has no `go.opentelemetry.io/*`; nothing in `evals/`, `docs/` or git history mentions OTLP.
