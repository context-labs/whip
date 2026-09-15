# Live session trace view and OTEL export

Status: implemented 2026-09-14 on branch `session-trace` (worktree `.claude/worktrees/session-trace`); Items 1–3 landed together. See Verification results at the end.
Evidence: [evidence/whip-telemetry-inventory.md](evidence/whip-telemetry-inventory.md) (what Whip records today, with numbers from kuzco-4090) and [evidence/inference-net-reference.md](evidence/inference-net-reference.md) (inference.net ingest conventions and trace viewer internals).

## Decisions

1. **One trace per root turn.** A trace is one submission and everything it caused, including child turns started by that work (Decision 3). Sessions group traces by `session.id = root_id`. Why: bounded span counts per waterfall (dozens to hundreds, not ~20K), matches inference.net's "one message, one trace" and its Sessions tab, and avoids a session-long root span that never ends. HALO renders exactly this shape: one sessions row per `session.id` with a "Turns" column equal to the trace count, and one combined tree with a synthetic `Turn N` node per trace when opened.
2. **Flat tree**: under a turn span, LLM spans and tool spans are siblings in time order; host calls nest under their cell; child turns nest under the host call that triggered them. The emitting model call is kept as an attribute (`whip.emitting_call_id`), not as the parent. Why: that is the shape in the screenshot and in inference.net's own Pi tree, parents always enclose children in time, and cost roll-ups stay meaningful.
3. **Pass the parent span id down now.** The kernel tags the host-call context with its identity; `agents.spawn`, `agents.submit` and `messages.send` write `parent_span_id` and `trace_id` onto the inbox row or mail row they create; a child turn's span takes its parent and trace from the row that started it. Only the immediate parent is stored; the tree is rebuilt by following pointers, which is how OTEL works and how grandchildren fall out for free.
4. **Full session content, exported once: delta prompts.** Every LLM span carries `llm.input_messages` = the messages added to that agent's transcript since its previous model call (first call of a turn: the submission and any mailbox digest; later calls: tool results and injected steers), and `llm.output_messages` = the assistant message it produced. Every tool and host span carries full arguments and output. The whole conversation therefore appears exactly once across the export, never repeated per call. `whip.input.delta = true` and `whip.input.through_seq` (transcript position) let a consumer rebuild the exact model view; `whip.compaction.summary` rides on compaction spans. Why: repeating history per call would make the largest observed session a 1–2.5 GB export, and HALO's session conversation does not deduplicate across turns, so full-history producers show every earlier turn again under each later one. Its trace conversation only needs assistant text in the span's output messages, which this satisfies. Exact per-call request capture stays a later opt-in.
5. **Both attribute dialects**: OTel GenAI semantic conventions and the OpenInference keys inference.net promotes.
6. **Open spans in export**: included, with `end_time = start_time` and `whip.span.in_progress = true`. Both HALO and inference.net's viewer draw a zero-duration span as running to now, and both upsert on `(trace_id, span_id)`, so a later export of the finished span replaces it. HALO also accepts a missing end time; the zero-length form is chosen because it is valid OTLP for every consumer.
7. **`--push` in v1**: the same OTLP/JSON body that `-o` writes is sent as `POST` requests (`Content-Type: application/json`, gzip, ≤ 4 MiB each, `Authorization: Bearer` when a token is configured) to an OTLP/HTTP endpoint instead of a file. Plain HTTP, not a multipart upload. HALO (`127.0.0.1:8799/v1/traces`, no auth) and inference.net (`telemetry.inference.net/v1/traces`) both accept it. It is the verification oracle for the exporter, and the same code path a future daemon-side live push would use: post a span without an end at `span.started`, post it again at `span.ended`.
8. **Wait spans in v1**: permission prompts and `user.ask` questions are `wait` spans under the turn.
9. **Nanoseconds everywhere.** One fixed-width layout for every TEXT stamp (`2006-01-02T15:04:05.000000000Z07:00`, sorts lexicographically, parses with `time.RFC3339`), and `INTEGER` unix nanoseconds on the new tables. Existing rows are not backfilled; no backward compatibility is required.
10. **A third session view kind**, `trace` (`?view=trace`), beside chat and REPL.
11. **No redaction.** Export includes whole arguments, outputs, prompts and completions.
12. **Delivery order**: spans and export first (Items 1 and 2), verified through inference.net's own viewer and HALO desktop live mode, then the native UI (Item 3).
13. **Cost in OpenInference keys only, with input / cache-read / output splits stored at settlement** (see the cost encoding paragraph under Export). GenAI semconv has no cost attribute; both consumers read the six `llm.cost.*` keys.

## Why this matters

A Whip session already produces everything a trace viewer needs, but in a shape nobody can render as a timeline: every durable timestamp is whole-second, model calls do not know which turn they belong to, tool cells exist only in the transcript until the turn ends, and the event journal is a 10,000-row sliding window that the largest real session has already scrolled through 92 times (918K rows evicted). So today the only way to answer "where did six minutes and $25 go" is to read the chat. Inference.net's viewer answers it at a glance, but only after the run, only for spans exported at their end, and only in their app.

The plan adds one durable, precisely timed span record written at boundaries Whip already crosses, exposes it live through the same journal and SDK view machinery the chat and REPL use, and maps it to standard OTLP for export. It does not embed an OpenTelemetry SDK, does not duplicate transcripts, and does not change the meaning of any existing event or table.

## What exists and what is missing

Whip already has span-shaped durable rows (`turns`, `model_calls`, `operations`, `agents`), start/end events for every boundary (`turn.*`, `model.call.*`, `stream.tool.*`, `stream.cell.host*`), stable identities (turn id, logical call id, tool call id, invocation id), token and cost accounting per attempt, and a bounded live view pattern (`packages/sdk/src/executions.ts`). Details in the inventory.

| # | Gap | Where | Why it blocks the view or the export |
| --- | --- | --- | --- |
| G1 | Timestamps are whole seconds | `internal/session/session.go:173` `now()`, `mailbox.go:125` `timestampArg` | A 41 ms `files.read` and a 26 s model call land on the same tick |
| G2 | Stamps are taken at insert, not at observation | `internal/daemon/session.go:772` | Stream events cross the supervisor mailbox before insertion; under load the skew shows on a timeline |
| G3 | The journal is a sliding window | `EventRetention = 10_000` | A trace assembled from events loses its beginning; the export must come from durable rows |
| G4 | No turn id on `model_calls` / `operations`; cells are transcript-only until turn end | schema | Cannot parent a model call under its turn from durable data; a running turn's cells are invisible durably |
| G5 | Model-call events omit model name and the prompt/completion split | `ModelCallEvent` | Needed for `llm.model_name` / `gen_ai.usage.*` and the tree label; they live only in blobs |
| G6 | No link from a model call to the assistant message it produced | `llm.Message` | The LLM span's OUTPUT would be positional |
| G7 | Host calls carry a summary and error, not the operation id | `rlm.HostCall` | `files.read: README.md` cannot open its full `operations` row |
| G8 | Child work carries no causal identity | `InboxEnqueue`, `MailboxSend`, `inbox`, `agent_messages` | A child turn cannot be parented under the call that started it |
| G9 | No span concept in protocol, SDK, or app | | The view, the reducer, and the export all need one |

## Design

### One record, written at existing boundaries

```
spans(
  root_id TEXT NOT NULL REFERENCES sessions(id),
  id TEXT PRIMARY KEY,          -- 16 hex chars: first 8 bytes of sha256(kind + whip identity); deterministic, so start and end are idempotent
  trace_id TEXT NOT NULL,       -- 32 hex chars: inherited from the parent span, else first 16 bytes of sha256(turn id)
  parent_id TEXT NOT NULL DEFAULT '',
  agent_id TEXT NOT NULL, turn_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('agent','llm','tool','host','wait')),
  name TEXT NOT NULL,
  start_ns INTEGER NOT NULL, end_ns INTEGER NOT NULL DEFAULT 0,   -- 0 = open
  status TEXT NOT NULL,         -- running | ok | error | cancelled | interrupted
  attrs BLOB NOT NULL DEFAULT '{}',  -- bounded (≤ 4 KiB): identities, model, tokens, cost_micros, tool name, summary, error excerpt, pointers to bodies
  links BLOB NOT NULL DEFAULT '[]',  -- other causes (extra mailbox messages in a digest), exported as OTEL links
  updated_seq INTEGER NOT NULL  -- event seq of the last write, for cursor paging
)
CREATE INDEX spans_root_trace ON spans(root_id, trace_id, start_ns);
CREATE INDEX spans_root_updated ON spans(root_id, updated_seq);
```

Bodies are never copied. `attrs` holds pointers: `model_call_id`, `operation_id`, transcript `seq`, or a content reference already minted by the stream path. That keeps invariant 6 (large values are referenced) and keeps the table an index over rows Whip already stores; the export resolves pointers into full bodies (Decision 4, 11).

Why a new table rather than columns on `turns`/`model_calls`/`operations` plus a new `cells` table: that route changes four tables, still needs a union query with four shapes for both view and export, and has no place for wait spans. One table, one store helper (`RecordSpanStart` / `RecordSpanEnd`), one event pair, one page query.

Why not the OTel Go SDK: it adds the `go.opentelemetry.io` tree and protobuf, exports on span end only (live start would need a custom processor anyway), and creates a second source of truth beside SQLite. OTLP/JSON is a small schema; a hand-written encoder over `spans` is a few hundred lines with no dependency.

### Boundaries and hook points

| Kind | Start | End | Name | Parent | Attrs |
| --- | --- | --- | --- | --- | --- |
| `agent` (a turn) | `StartRootTurn`, `StartRootMailboxTurn`, `StartAgentTurn` (same transaction as `turn.started`) | `CommitRootTurn` / `FinishAgentTurn` | agent name (root: definition id) | root turn: none. Child turn: the `parent_span_id` on the inbox row it claimed, or on the earliest pending message of its mailbox digest (`ORDER BY created_at, rowid`, already the digest order); other digest messages become links. Human `agent.submit` and schedule turns: none (own trace) | `whip.turn_id`, `agent.id`, `agent.name`, `whip.agent.parent_id`, model, provider, trigger, `input.value` (the submission), `output.value` (final text) at end |
| `llm` | `beginAgentModelAttempt` (`internal/daemon/budget.go`): model, provider, purpose, input estimate, attempt number, logical id | the permit's `Settle` closure: prompt/completion/cached/reasoning tokens, cost, source, status | `<provider>/<model>`; purpose `compact` → `compaction` | the agent's current turn span | `gen_ai.request.model`, `gen_ai.provider.name`, `llm.model_name`, `llm.provider`, tokens in both dialects, `llm.cost.total` when `cost_source != unknown`, `whip.model_call.{id,logical_id,attempt,purpose,cost_source}`, transcript `seq` of the produced message |
| `tool` (rlm_exec, bash, custom) | `OnToolStart` wiring in `AgentSession.RunTurn` | `OnToolEnd` (status from exit code, `DurationMs`) | `rlm_exec` → `cell`; `bash: <first line>`; custom tool name | the turn span | `tool.name`, `tool_call.id`, `whip.emitting_call_id`, `whip.execution_engine`, `input.value` = args, `output.value` = result (content ref when large) |
| `host` | `emitHostStart` (`recursive_runtime.go:182`) | `emitHostCall` (status, error, duration) | `<module>.<operation>: <summary>` using inference.net's summary rule (basename for paths, first 60 chars otherwise) | the enclosing `tool` span (`call.CallID`) | `tool.name`, `tool_call.id`, `whip.invocation_id`, `whip.operation_id` (Item 1 step 6), `input.value` = arguments, `output.value` = result via the `operations` row |
| `wait` | `permission.pending`, `question.pending` | decision / answer | `permission: <operation>` / `question` | the turn span | operation, rule, principal, question text |

Start and end times are `time.Now()` on the goroutine that observes the boundary and travel inside the record; the actor inserts later without restamping. Transactional boundaries (turn start/end, model admission/settlement) join the existing transaction. Stream-style boundaries (tool, host, wait) ride the supervisor mailbox like a stream event (`workerSpan` envelope) so event ordering stays actor-owned.

### Causal identity for child work (Decision 3)

1. `internal/rlm/kernel.go` puts the `HostCall` identity into the context immediately before `kernel.host.Call(ctx, ...)` (one `context.WithValue`).
2. The recursive host derives the host span id from it. `agents.spawn` (`AdmitAgent` → `enqueueInboxTx`), `agents.submit` (`SubmitAgentInput` → `EnqueueInbox`) and `messages.send` (`SendMailboxMessage`) copy `parent_span_id` and `trace_id` into `InboxEnqueue` / `MailboxSend`.
3. New columns: `inbox.parent_span_id`, `inbox.trace_id`, `agent_messages.sender_span_id`, `agent_messages.trace_id` (all `TEXT NOT NULL DEFAULT ''`).
4. `StartAgentTurn` already claims the inbox row (inbox trigger) or detects ready mail (mailbox trigger); it reads the parent and trace from the row, or from the earliest pending message, and writes the turn span with them. Extra digest messages go to `links`.
5. Steers that join a running turn at a loop boundary are not new spans; they become span events on the running turn span with a link to the sender's span (Item 1 step 8, small).

Grandchildren need nothing extra: a grandchild's turn parents to the child's `agents.spawn` host span, whose parent is the child's cell, whose parent is the child's turn, whose parent is the root's host span. Each row stores one parent pointer; `trace_id` is copied down so one indexed query returns the whole trace.

Trade-off accepted: a child that keeps working after the root turn ends still belongs to that trace, so a trace's extent can exceed the root span. The header shows the root span's duration; the waterfall domain covers all spans.

### Live path

`span.started` carries the full record; `span.ended` carries id, `end_ns`, status, and the attrs that changed. Both are registered in `protocol.EventPayloads`. Cost: two small events per span; on the largest real session 97% of retained events are tool-argument deltas, so this is noise.

Because the journal is a window, the SDK view never trusts events alone: it loads `trace.page` (durable, cursor `updated_seq`, filter by trace or turn) and applies later `span.*` events, requesting a page when it sees an end without a start. Bound: 4,096 spans / 2 MiB per root with the existing visible-truncation rule. Default scope is the selected turn's trace; "whole session" pages.

Clock: `trace.page` responses carry `server_time_ns`; the SDK keeps one offset per response and draws open spans to `now + offset`. That retires the REPL view's "client-observed only" caveat, because starts are now server-measured.

### Export (Decisions 4, 5, 6, 7, 11)

`trace.export {root_id, trace_id?}` renders standard OTLP/JSON (`ExportTraceServiceRequest`: `resourceSpans[].scopeSpans[].spans[]`, hex ids, `startTimeUnixNano`/`endTimeUnixNano` as strings, typed attribute values, status enum names) into the content store and returns a `ContentHandle`; the desktop uses the existing download adapter. CLI: `whip sessions export <root> --otel [--trace id] [-o file] [--push URL]`. `--push` posts the same body in ≤ 4 MiB gzip batches with `Authorization: Bearer` from the configured Inference credential (`internal/inferencenet` already provisions one) or `--token`; targets are `https://telemetry.inference.net/v1/traces` and HALO desktop's `http://127.0.0.1:8799/v1/traces`.

Mapping: `resource.attributes = {service.name: whipcode, service.version, whip.runtime_id, whip.session.id}`; `scope = {name: whip, version}`; span kind INTERNAL for agent/tool/host/wait, CLIENT for llm; `status` ERROR with message on failure; attributes as in the table plus `openinference.span.kind` (AGENT / LLM / TOOL / CHAIN) and `gen_ai.operation.name` (`invoke_agent` / `chat` / `execute_tool`); `session.id`, `agent.id`, `agent.name` on every span; `llm.input_messages` / `llm.output_messages` (indexed-flat OpenInference form) and `gen_ai.input.messages` / `gen_ai.output.messages` (JSON) on LLM spans; full `input.value` / `output.value` on tool and host spans; open spans as in Decision 6; `links` as OTLP links.

Input messages per LLM span are the transcript delta since the agent's previous model call (Decision 4), read from `messages` / `transcript_messages` between the previous call's `through_seq` and this call's; compaction spans carry their summary. Consumers that want the exact request rebuild it as the daemon does on reload (system prompt, latest summary, raw rows after the cutoff). Ephemeral notices and the decay pass are not persisted, so byte-exact capture stays a later opt-in (`trace.capturePrompts`, storing the request body as a content object per call).

Cost encoding (Decision 13). OTel GenAI semantic conventions define token counts only (`gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.cache_read.input_tokens`, `gen_ai.usage.cache_creation.input_tokens`) and no cost attribute, so cost is emitted in the OpenInference dialect only, which is the one both HALO and inference.net promote to columns: `llm.cost.total`, `llm.cost.prompt_details.input`, `llm.cost.prompt_details.cache_read`, `llm.cost.completion_details.output`, and, when a rate exists, `llm.cost.prompt_details.cache_write` and `llm.cost.completion_details.reasoning`. Values are USD doubles (Whip's micro-USD integers divided by 1e6). Whip's `Pricing.ActualCost` already computes exactly three terms, non-cached prompt tokens × prompt rate, cached tokens × cache-read rate, completion tokens × completion rate, so Item 1 exposes that as a breakdown and `SettleModelCall` stores the three components beside `cost_micros`. When the provider reports a total (inference.net and OpenRouter return `usage.cost`), the total is the reported figure and the three splits are the rate-based estimate, labelled `whip.cost.source = reported` and `whip.cost.split_source = estimated`; when only rates are known, all four are estimates and `whip.cost.source = estimated`; when neither is known, no cost key is emitted at all, because inference.net treats a zero total as "producer could not price" and HALO shows what it receives. Reasoning tokens are billed inside completion tokens by OpenAI-compatible providers, so the reasoning token count is emitted but no separate reasoning cost; cache-write cost is omitted until a provider Whip supports charges for it.

Interop facts that constrain the encoding (see the evidence file): `session.id` must be on the root span of every trace, because HALO takes a trace's session from its root only; `llm.cost.total` must be emitted whenever Whip knows the cost, because HALO has no pricing table and inference.net treats a non-zero producer total as authoritative; usage attributes go on LLM spans only, because both viewers roll tokens and cost up with flat sums; HALO clamps a trace detail to 250 spans and a session to 1,000, so very long turns will truncate there (our own view pages). Optional later: `--format jsonl` in HALO's `JsonlSpanRecord` line shape for drag-and-drop and `inf trace upload`.

### Frontend (Decision 10)

- `packages/sdk/src/trace.ts`: `TraceView` + `traceRows(state, traceId)` following `executions.ts`; pure helpers ported from inference.net's in-house viewer (`timelineMath.ts`, `spanTree.ts`, `rollups.ts`, `spanDisplayName.ts`) rather than rewritten.
- `packages/app`: `trace-view.tsx` mounted in `conversation.tsx` beside `ReplView`; tab kind `trace` in `session-tabs.ts`; three panes on `react-resizable-panels` (execution tree with `@tanstack/react-virtual`, waterfall as absolutely positioned bars at 28 px rows, details with Overview/Raw and existing `ContentRead` for bodies); header duration/spans/tokens/cost; a trace picker (one per root turn) with a whole-session mode. Whole-session totals come from `stream.accounting`; per-trace totals are client sums over loaded spans, flagged when truncated. "Open chat" jumps to the conversation for the same agent (existing `openSessionView`).
- Desktop: nothing specific; the packaged renderer picks the view up. TUI: not in scope.

## Items (in delivery order, Decision 12)

### 1. Durable spans, live span events, causal identity, precision (daemon)

**Why.** G1–G9 except the UI. Everything else stands on this.

**Steps.**
1. One stamp layout: replace `now()` and `timestampArg` with a shared fixed-width nanosecond constant; audit the other `Format(time.RFC3339)` sites listed in the inventory that write stamps compared against `now()` (schedules `anchor`/`last_fire`, mailbox `available_at`, slot text). Display-only formatting can stay.
2. Schema 19: `spans` table + indexes; `inbox.parent_span_id`, `inbox.trace_id`; `agent_messages.sender_span_id`, `agent_messages.trace_id`; `upgradeV18` in the existing chain, additive, no backfill.
3. `internal/session/span.go`: `SpanRecord`, deterministic ids, `RecordSpanStart` / `RecordSpanEnd` (upsert row + journal event in one transaction, usable inside an existing transaction), `PageSpans(ctx, rootID, traceID, afterSeq, limit)`.
4. Hook the transactional boundaries: root and child turn start/finish (reading parent/trace from the claimed row or digest); `AdmitModelCall` / `SettleModelCall` (they already write `model.call.*` in a transaction; model name and token split are already in hand). Add `Pricing.CostBreakdown(usage)` next to `ActualCost` and store `cost_input_micros`, `cost_cache_read_micros`, `cost_output_micros` on `model_calls` at settlement so the LLM span carries the split without re-pricing at export.
5. Hook the stream boundaries via a `workerSpan` envelope handled next to `recordStreamEvent`: tool start/end in `RunTurn`, host start/end in `recursive_runtime.go`, permission and question waits.
6. Thread identity: kernel puts `HostCall` in the host-call context; `recursiveHost` reads it for spawn/submit/send; carry `OperationID` on `rlm.HostCall` (and on `stream.cell.host`) when the call went through the dispatcher; add internal `CallID` to `llm.Message` (stripped by `stripAuthored`) so the produced assistant row and the LLM span agree.
7. Register `span.started` / `span.ended` in `protocol.EventPayloads`; add `trace.page` to the registry; `npm run generate`.
8. Steer-at-boundary span events with links (small; may slip to Item 3 without blocking anything).

**Tests.** New: a root turn with one model call, one cell, two host calls, one spawned child that runs one turn yields the expected parent chain and trace id, with nanosecond ordering; ids stable across a repeated end; an interrupted call leaves `end_ns = 0` with status `interrupted` until corrected; spans survive event eviction (insert 10,001 events, page still complete); a mailbox-triggered child turn parents to the earliest message's sender span and links the rest; migration 18→19 on a copied fixture; `available_at <= now()` comparisons still hold with the new layout. Update the seven test files that assert `RFC3339` stamps (`schedule_test.go`, `scheduler_test.go`, `mailbox_delivery_test.go`, `server_test.go`, `root_client_test.go`, `mcp_capability_test.go`, `runtime_test.go`). Keep green: `v2_event_schema_test.go`, turn lifecycle acceptance tests, `model_call_storage_test.go`, `agent_turn_storage_test.go`, `swarm_storage_test.go`, protocol drift check.

**Trade-offs.** Two more rows per boundary in the hot path and two more journal rows. SQLite writes already serialize; the model-call path adds a row to a transaction that already exists. Tool/host spans add one mailbox envelope each, the same cost as today's `stream.cell.host.started`.

### 2. Export

**Why.** The second requirement, and the cheapest way to verify Item 1 before the UI exists.

**Steps.**
1. `internal/session/otlp.go`: encode spans into `ExportTraceServiceRequest` JSON, resolving body pointers (transcript rows, `operations` payload/result, content references) into full attribute values; LLM input messages are the transcript delta between consecutive model calls of the same agent, output messages the produced assistant row.
2. `trace.export` RPC storing the result in the content store; `whip sessions export --otel`; `--push` with gzip and ≤ 4 MiB batches.
3. Golden test on a fixture session (checked-in JSON) plus invariant assertions both consumers depend on: hex ids of the right length, every span has a start, roots have empty parents and carry `session.id`, `llm.model_name` and `llm.output_messages` on every LLM span, usage only on LLM spans, cost present when known and omitted when unknown, open spans zero-length and flagged, every message of the transcript appears exactly once across the export.
4. Manual oracle: push a kuzco-4090 session to HALO desktop and to inference.net; the sessions row must show one turn per root turn, the tree, labels, tokens and cost must match Whip's accounting, and the conversation must read as one transcript without repeats.

### 3. SDK trace view and desktop view

**Steps.**
1. `TraceView` in the SDK with page + event merge, bounds, server-clock offset, derived tree and roll-ups; unit tests mirroring the executions tests (start→end, end-without-start triggers a page, truncation flag, offset arithmetic, parent chain across agents).
2. App view kind `trace`, `?view=trace` serialization, tab strip and context-menu entries next to REPL, the three panes, zoom/pan parity with the reference (⌘/Ctrl+wheel zoom, drag pan, double-click to span, fit), trace picker.
3. Theme tokens and density from `@whip/ui`; kind colors as tokens.
4. Vitest render tests (empty, running, truncated, failed span, child agent nested) and one browser fixture in `apps/web/scripts`.
5. Docs: `docs/frontend.md` (view kind, retention row), `docs/features.md` row, `docs/protocol-v2.md` (kinds, RPCs, stamp layout).

**Trade-offs.** Custom DOM waterfall, no library. Long sessions are a list of traces, not one picture.

## Preserved, changed, not built

Preserved: every existing event kind and payload shape; the `events` window and replay; `turns`, `model_calls`, `operations`, transcripts; chat and REPL views; TUI; `whip run --format json` (it will also print `span.*` lines).

Changed: all new TEXT stamps carry nanoseconds (old rows untouched); schema 18→19 (one table, four columns); `llm.Message` and `rlm.HostCall` gain one internal field each; `InboxEnqueue` / `MailboxSend` gain parent identity; `stream.cell.host` gains `operation_id`; protocol minor bump with two event kinds and two RPCs.

Not built: an OpenTelemetry SDK dependency; protobuf or gRPC; exact per-call prompt capture (opt-in later); HALO JSONL line format (later, if drag-and-drop matters); daemon-side continuous live push (same code path, later); a cross-session trace list; TUI trace panel; span retention caps (rows are ~300 B; the largest session would hold ~20K).

## Verification

Go: `go test ./...` with the new tests; race detector on the daemon package. Protocol: `npm run generate` then the drift check. Web: `npm run check:web`, `npm run test:web`, browser fixture. Desktop: `npm run check:desktop`. Manual: run a session on kuzco-4090 through the new build, watch the root span appear before the first model call returns, export the session, push it, and compare against Whip's own view.

## Verification results (2026-09-14)

- Go: `go build ./...`, `go vet ./...`, `go test ./...` green after the schema 19 fixtures, the idempotent settlement details, the root-failure event ordering, and the session delete list were updated for spans.
- Protocol: `npm run generate` produced `SpanRecord`, `SpanPage`, `TracePageParams`, `TraceExportParams`, `TraceExportResult`; `npm run check` green; minor bumped to 6.7.
- SDK: 328 node tests green including `trace.test.ts` (upsert ordering, page merge and clock offset, whole-trace eviction).
- App: `tsc` clean; `trace-math.test.ts` (15) and `trace-view.test.tsx` (3) green; tab, routing and REPL suites unchanged.
- Oracle: a generated export was written with `WHIP_OTLP_DUMP`; the local OTLP endpoint on 8799 turned out to be the monorepo's dev server, which requires an API key, so the HALO/inference.net push remains a manual step (`whip sessions export <root> -push http://127.0.0.1:8799/v1/traces -token …`).

Deviations from the plan text: span writes call the store directly from the observing goroutine instead of riding a `workerSpan` supervisor envelope (SQLite serializes writers and settlement already writes off-actor; one code path instead of two); the inbox and mail columns are `span_trace_id` rather than `trace_id` because `InboxEnqueue.TraceID` already names the dispatcher's trace; the desktop view uses a CSS-grid layout and one virtualized row list rather than resizable panels (`ponytail:` add `react-resizable-panels` from `@whip/ui` when pane sizing matters).
