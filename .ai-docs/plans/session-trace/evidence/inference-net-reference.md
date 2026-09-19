# How inference.net does it (reference, 2026-09-14)

Sources: docs.inference.net (`integrations/traces/*`, `cli/traces`, `cli/spans`, `guides/halo-desktop-app`, fetched as `.md`), and the monorepo at `/Users/samheutmaker/Desktop/context-labs/src/monorepo/inference` (commit f12a0dbc, 2026-09-02). File references below are relative to that repo.

## Pi adapter (what the screenshot's data came from)

- Package `@inference/tracing`, TypeScript only. It patches Pi's `Models` collection (`stream`, `streamSimple`, `complete`, `completeSimple`) so every model turn emits one OpenInference **LLM** span named `pi-agent.<provider>.turn`. Nothing in Pi is hooked for agent or tool layers; users wrap `agent.prompt()` in `agentSpan(...)` and each tool body in `manualSpan({ spanKind: TOOL, toolName, toolCallId, input })`.
- Parent/child is plain OTel context propagation; agent identity (`agent.id`, `agent.name`, `agent.role`) set once on the outer AGENT span is copied onto LLM children by the patcher.
- Spans are batched and exported on an interval; nothing is sent at span start. `shutdown()` force-flushes.

## Ingest

- `POST /v1/traces` on `telemetry.inference.net` (`apps/llm-ops-otel-server/src/routes/traces.ts`). **OTLP/HTTP JSON only**: anything but `application/json` → 415. Body ≤ 4 MiB (pre- and post-gunzip), `Content-Encoding: identity|gzip`. Bearer `INFERENCE_API_KEY`. Response `{}`; failures → 503 so exporters retry. Also `POST /api/public/otel/v1/traces` with Langfuse Basic auth (`pk-inference:<key>`).
- Edge writes the raw body to R2 then publishes a pointer to RabbitMQ; `apps/llm-ops-consumer` decodes, routes attributes, prices spans, and inserts ClickHouse `spans` (`packages/llm-ops-db-clickhouse/migrations/033_create_spans.sql`) plus `spans_by_trace`, `spans_by_session`, and the `traces_by_project` roll-up (migration 067).
- HALO desktop app "live mode" is the same OTLP push to `http://127.0.0.1:8799/v1/traces`. Batch alternative: `inf trace upload <file.jsonl>` (one span per line in their export line format, `packages/services/llm-ops-otel-service/src/exports/otlp-line-select.ts`).

## Attribute conventions the pipeline reads (`packages/services/llm-ops-otel-service/src/router/extractors.ts`)

Precedence low → high: codex < langsmith < langfuse < elevenlabs < cursor < vercel < pydantic-ai < **OTel GenAI** < **OpenInference**; `llm.usage` JSON wins for tokens.

| Promoted column | OpenInference key | OTel GenAI key |
| --- | --- | --- |
| `observation_kind` | `openinference.span.kind` (LLM, TOOL, CHAIN, AGENT, RETRIEVER, EMBEDDING) | `gen_ai.operation.name` (`chat`→LLM, `execute_tool`→TOOL, `invoke_agent`→AGENT, `embeddings`) |
| `llm_provider` | `llm.provider` | `gen_ai.provider.name` ?? `gen_ai.system` |
| `llm_model_name` | `llm.model_name` | `gen_ai.request.model` |
| `llm_response_model` | | `gen_ai.response.model` |
| `input_tokens` (excl. cache read) | `llm.token_count.prompt` | `gen_ai.usage.input_tokens` ?? `gen_ai.usage.prompt_tokens` |
| `output_tokens` (excl. reasoning) | `llm.token_count.completion` | `gen_ai.usage.output_tokens` ?? `gen_ai.usage.completion_tokens` |
| `total_tokens` | `llm.token_count.total` | `gen_ai.usage.total_tokens` |
| `cache_read_tokens` | `llm.token_count.prompt_details.cache_read` | `gen_ai.usage.cache_read.input_tokens` |
| `reasoning_tokens` | `llm.token_count.completion_details.reasoning` | |
| `cost_total` (USD) | `llm.cost.total` (+ `llm.cost.prompt_details.input`, `llm.cost.completion_details.output`, ...) | |
| `session_id` | `session.id` (> `conversation.id` > `chat.id`) | `traceloop.association.properties.session_id` |
| `user_id` | `user.id` | |
| `agent_name` / `agent_id` | `agent.name` / `agent.id` | `gen_ai.agent.name` / `gen_ai.agent.id` |
| `input_messages` / `output_messages` | `llm.input_messages` / `llm.output_messages` as a JSON string **or** indexed-flat `llm.input_messages.0.message.role|content|tool_calls.0.tool_call.id|function.name|function.arguments` | `gen_ai.input.messages` / `gen_ai.output.messages` or flat `gen_ai.prompt.N.*` / `gen_ai.completion.N.*` |
| `input` / `output` | `input.value` / `output.value` (+ `*.mime_type`) | |
| tool identity (TOOL classification) | `tool.name`, `tool_call.id` | `gen_ai.tool.name`, `gen_ai.tool.call.id` |

Resource: `service.name`, `service.version`, `deployment.environment` promoted; everything else kept losslessly in type-split maps.

Cost: computed in the consumer from `llm_model_name` × token columns against Postgres `model_provider_routes` pricing (`apps/llm-ops-consumer/src/entrypoints/observability/otel-cost-enrichment/`). A **non-zero** producer `llm.cost.total` freezes the row; a **zero** total next to real tokens means "producer could not price this" and is filled in. Omit the attribute when cost is unknown.

Root selection: `argMin(tuple(parent_span_id != '', start_time, span_id))` — a real root wins, else the earliest span. A trace is visible as soon as any of its spans lands; late roots flip the roll-up automatically.

## Trace viewer UI (`apps/web/src/observability/components/trace-viewer/`)

- Header (`detail/TraceDetailHeader.tsx`): DURATION, SPANS, TOKENS, COST from the server roll-up; while running, DURATION is a local clock. Tabs Tree / Conversation / Timeline / Details are pane toggles.
- Left EXECUTION panel (`detail/SpanTreeSidebar.tsx`) with a USAGE toggle; label rule (`detail/spanDisplayName.ts`): LLM → `llmModelName`; TOOL → `spanName`. Server mints tool names as `name: summary` (`packages/services/inference-service/src/gateway-conversation-trace.ts:918`): summary = basename when the first string argument looks like a path, else first line truncated to 60 chars; arrays → `N items`. Gateway LLM span names are `provider/model` (`:908`), which is the `inference/kimi-k3` chip.
- Timeline (`detail/WaterfallCanvas.tsx`, `detail/timelineMath.ts`): 28 px rows, virtualized; domain = min start .. max end over visible rows; `x = (t - view.t0) / (view.t1 - view.t0) * width`, 3 px minimum bar; zoom clamp 0.2%–180% of domain, pan ±25%; ⌘/Ctrl+wheel zoom, drag pan, double-click zoom-to-span; `niceTicks` 1/2/2.5/5×10ᵏ ms. **In-flight span = `durationNs == 0`**; it is drawn to `nowMs` (a 500 ms local clock, `detail/useLiveNow.ts`) at 0.8 opacity, label "running".
- Right panel (`detail/SpanDetailPanel.tsx`): Overview / Raw. `COST (ROLLED UP)` and rolled tokens are **client-side** sums over the node and all descendants, only for AGENT/CHAIN spans (`detail/rollups.ts`); `CHILDREN` is the descendant count.
- Conversation (`detail/conversation.ts`): top-level LLM spans (no AGENT ancestor other than root) sorted by start; messages parsed from `inputMessages`/`outputMessages`, deduplicated across spans on `[role, content, toolCall ids+names]` because agent loops resend history; tool spans grouped under the assistant message whose end precedes them; sub-agents attach by `tool_call.id`.
- Data: one `traces.getDetail` tRPC call `{trace, spans[], truncated}` with a 5,000-span cap; **no live refetch** (no `refetchInterval`, no subscription) — only the local clock moves; new spans appear on manual refresh.

Reusable pure logic for a Whip port (in-house code): `detail/timelineMath.ts`, `spanTree.ts`, `detail/rollups.ts`, `detail/spanDisplayName.ts`, `detail/spanUtils.ts`.

## HALO desktop app (`/Users/samheutmaker/Desktop/context-labs/src/halo`, app v0.1.17)

Electrobun + React + tRPC + Hono + Drizzle over `bun:sqlite`. Local trace viewer with a live mode; the web viewer above ported its live affordances from here.

- **Ingest**: `POST /v1/traces` (also `/v1/otel/v1/traces`, `/otel/v1/traces`) on `127.0.0.1:8799` (`app/src/server/app.ts:81-83`). **OTLP/JSON only**, 415 otherwise (`app.ts:57-64`); `Content-Encoding` identity or gzip; 4 MiB cap pre- and post-gunzip; no auth; response `200 {}`. Ingest is synchronous into SQLite, upsert on `(project_id, trace_id, span_id)` (`telemetry/storage.ts:270-287`), so re-posting a span overwrites it. Also imports JSONL (its own `JsonlSpanRecord` line shape, `fileimport/types.ts:46-70`, drag-and-drop or `POST /v1/import/upload`), Langfuse and Phoenix.
- **Attributes read** (`telemetry/otlp.ts:349-470`): the same OpenInference and GenAI key set as inference.net; OpenInference wins. Session identity `session.id` → `cursor.run_id` → `elevenlabs.conversation_id`. Kind from `openinference.span.kind` / `gen_ai.operation.name` / `tool.name`.
- **Cost: no pricing table.** `cost_total` comes verbatim from `llm.cost.total` (`otlp.ts:387-392`); a producer that omits it shows `—`. Whip must emit cost.
- **Long sessions: both views.** `trace_summaries` has one row per `trace_id`; the sessions table has one row per `session.id` aggregated over traces (`GROUP BY project_id, session_id`, `storage.ts:1600-1667`) with a **"Turns" column = trace count**. Opening a session builds **one tree** with a synthetic `Turn N` node per trace, sorted by start, one waterfall and one conversation spanning `min(start)` → `max(end)` (`spanTree.ts:68-140`: `"halo.synthetic": "session_trace"`, `spanId: "session:<traceId>"`, `spanName: "Turn N"`). Turn nodes are not inspectable; cost roll-up skips them to avoid double counting.
- **`session_id` for a trace is taken from the root span only** (`storage.ts:463`); a trace whose root lacks `session.id` disappears from the sessions list.
- **Caps**: trace detail clamps at 250 spans (`storage.ts:871`), session detail at 1,000 spans by `start_time ASC` (`storage.ts:1250`); no paging in the detail UI.
- **Live**: spans render when the OTLP batch arrives. A span posted **without `endTimeUnixNano`** is stored with `end_time = 0` and drawn to now as "running" (`otlp.ts:606-626`, `detail/timelineMath.ts:28-32`, `WaterfallCanvas.tsx:367`); re-posting it with an end time completes it via the upsert. A child arriving before its parent gets a synthetic `pending_parent` node that the real parent replaces (`spanTree.ts:142-219`). UI receives changes over a tRPC WebSocket on 8800 (`live_events` table, 10,000 retained).
- **Root**: parentless span with earliest start, else earliest span (`storage.ts:495-523`). Trace token/cost roll-ups are flat sums over all spans (`storage.ts:384-394`), so a producer must not put usage on both a parent and its children.
- **Conversation** (`conversation.ts:160-202`): top-level LLM spans in start order; messages from `input_messages` then `output_messages`; repeated history deduplicated on `role:content[:120]:toolCallNames`, first occurrence wins; **assistant text must be in the span's output messages** to become an agent bubble. Session mode does **not** dedupe across turns, so full-history producers show turn 1's messages again under turn 2. Delta-only input messages work and avoid both problems.
