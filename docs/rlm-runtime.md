# Recursive runtime

RLM is whip’s execution model, not an optional mode. Every root and child
model sees `rlm_exec`; its Starlark cells call daemon-hosted modules. There is
no direct-tool agent mode and no mode field on a session.

The design has four goals:

1. keep large context available without repeatedly placing it in the prompt;
2. make recursive delegation use one understandable session abstraction;
3. make coordination, authority, budgets, and recovery durable;
4. keep all side effects behind one daemon-owned policy boundary.

## Session identity and recursion

Root and child nodes use the same `AgentSession` type. Each has:

- a provider route and reasoning effort;
- exactly one model-facing tool;
- one bounded kernel and disposable Starlark global scope;
- a durable transcript and private state;
- an agent ID, parent ID, capabilities, and effective budgets.

Capabilities omitted at spawn inherit the parent’s set. An explicit list only
narrows it. The default maximum depth is two edges. Child names must be unique
under one parent, and child admission fails before persistence if no kernel
worker is available.

## Durable communication

Spawn returns immediately with the child’s admission metadata. The child’s
assistant response stays in its transcript. To communicate, either side uses:

```python
messages.send(recipient=parent_id, subject="review", body="Findings…", delivery="queued")
messages.list(status="pending", sender="", limit=50)
messages.read(id=message_id)
messages.complete(ids=[message_id])
messages.defer(id=message_id, seconds=600)
```

`agent_messages` is the only "notify" table. A message carries a delivery
class: `steer` is injected at the recipient's next loop boundary (or starts a
turn when idle), `queued` gets its own turn when the recipient is idle, and
`next_turn` rides along with whatever turn comes next. Messages move
`pending → delivered → done`: a message is `delivered` only when the turn that
showed it commits (a failed turn shows it again), `messages.read` delivers it
explicitly, `messages.complete` finishes it, and `messages.defer` returns it
to `pending` at a later time.

Bodies are never pushed whole into another model's prompt. A turn that starts
with ready mail receives a bounded digest: one line per pending message with
sender, kind, subject, size, and a 2 KiB excerpt. Child turn results reach the
parent the same way as runtime-authored `agent.completed`, `agent.failed`, or
`agent.cancelled` messages with a short preview and an evidence handle for the
full text; blackboard subscriptions post `state.changed` messages upserted per
subscription. Readiness is derived from durable state (`queued` inbox rows and
`pending` mail), so an in-memory wake is only an optimization.

Every node's system prompt ends with an identity block (id, name, parent,
depth, report mode) and a child's first input is `[task from parent <name>
(<id>)]` plus the prompt, so `messages.send(recipient="parent")` always works;
a direct relative's name or id is accepted too. Messages travel one hop
(parent, child, sibling), so there is no `root` alias. Parents steer children with
`agents.submit(id, text, delivery="steer"|"queued")` and can block briefly on
`agents.wait(ids, timeout_ms)` (default 10 s, capped at 25 s so it stays under
the 30 s cell wall clock; the result carries per-child status plus `settled`
and `timed_out`, never the reply itself). The
system prompt tells every node that mail wakes it, so the expected pattern
after a spawn or submit is to end the turn and let the reply arrive as a
mailbox-triggered turn. `agents.spawn(report=...)` picks how a child's
turn end reaches the parent: `notice` (default, 160-byte preview plus evidence
handle), `inline` (4 KiB preview), or `message` (only failures; the child must
report explicitly). Sender caps: 16 KiB body (use an evidence handle above
that), 20 pending messages per sender→recipient pair, 30 sends per 10 seconds.

## Context and handles

A turn starts with at most four recent user/assistant exchanges, one bounded
summary, and handles for full history or oversized input. Host results above
the inline limit also become handles. Reads return a source identifier and
exact byte span so an answer can cite what it inspected.

Starlark globals persist across cells and survive worker restarts. After
every cell the kernel snapshots the worker's globals as Starlark source
(`name = repr(value)` for data, the original text for top-level `def`s and
lambda assignments, `b = a` for aliases) into the `agent_scratch` table, and
a fresh worker executes that program before its first cell, whether the old
worker was evicted by the pool, killed by the cell deadline, or lost to a
daemon restart. Closures, self-referential values, non-finite floats, values
over 256 KiB, and anything past a 768 KiB aggregate are skipped by name, and
the restart notice lists what was not restored. Every restore also appends a
`scratch.restored` actor event carrying the restored and not-restored names,
so a worker restart is auditable from the event log rather than from the
model's account of its ephemeral notice; the TUI renders it as a dim line. Helpers see globals as bound
when they were defined, so agents mutate containers in place or pass values
rather than rebinding a name a helper reads. Shared or long-lived information
still belongs in `state`, `artifacts`, messages, files, or child transcripts.

## MCP

MCP servers are daemon-owned integrations available from every authorized
node through `mcp.list_servers`, `mcp.list_tools`, and `mcp.call`. Their tools
are not appended to the provider’s tool catalog. Root and child therefore keep
the same stable interface even as MCP servers connect, fail, or reconnect.

## Limits

Omitted or zero values use these defaults:

| Config field | Default | Scope |
| --- | ---: | --- |
| `rlm.steps` | 1,000,000 | Starlark steps per cell |
| `rlm.hostRequests` | 1,024 | host calls per cell |
| `rlm.wallMillis` | 30,000 | wall time per cell |
| `rlm.memoryMiB` | 256 | worker memory ceiling |
| `rlm.outputBytes` | 65,536 | captured cell output |
| `rlm.frameBytes` | 1,048,576 | worker protocol frame |
| `rlm.maxWorkers` | 4 | daemon-wide live kernels |

These are execution bounds. The durable ledger separately accounts token,
cost, elapsed, content, record, operation, child, schedule, and depth budgets.

## Files and migration boundary

The runtime uses:

| Path | Purpose |
| --- | --- |
| `~/.whip/runtime-v2/daemon.sock` | owner-only local protocol socket |
| `~/.whip/runtime-v2/daemon.lock` | single-daemon ownership lock |
| `~/.whip/runtime-v2/daemon.log` | detached daemon diagnostics |
| `~/.whip/runtime-v2/sessions.db*` | commands, agents, transcripts, messages, policy, events |
| `~/.whip/runtime-v2/artifacts/sha256/` | immutable large bodies |

The daemon can be inspected and managed without entering the TUI:

```sh
whip daemon status [--json]
whip daemon start
whip daemon stop [--timeout 10s] [--force]
whip daemon restart [--timeout 10s] [--force]
whip daemon logs [-f] [-n 200]
```

`status` does not auto-start the daemon. Normal stop and restart checkpoint
durable state and wait for the owner lock to be released. `--force` sends a
signal only to the PID currently holding that lock.

`WHIP_HOME` replaces `~/.whip`. The pre-runtime-v2 database is not opened or
migrated automatically; this is an intentional clean break.

## Recovery

On daemon restart, retained non-root agents are reconstructed from metadata,
capabilities, provider settings, and their transcripts. Running child turns
become idle and their human input returns to `queued` (three retries, then
`interrupted`); committed messages remain `pending` until a turn that showed
them commits. Restore re-derives readiness from those rows, so a restored child
with pending mail or a queued prompt wakes without any in-memory signal.

In-flight external effects are marked interrupted. Restart never infers that
an uncommitted write or remote call is safe to repeat.

## Verification and evaluation

```sh
go test ./...
task acceptance
```

The deterministic RLM evaluation expands a large corpus, requires bounded
handle search plus stateless reviewer fan-out, and records correctness, model
calls, fan-out, host calls, tokens, latency, and estimated cost:

```sh
go test ./evals/rlm -run '^TestDeterministicRLMEvaluationReport$' -v
```

The opt-in live run spends provider tokens:

```sh
WHIP_RLM_LIVE_EVAL=1 \
WHIP_RLM_EVAL_REPORT=/tmp/whip-rlm-eval.json \
go test ./evals/rlm -run '^TestLiveRLMEvaluation$' -v
```

## Troubleshooting

- **Worker capacity exhausted:** stop/delete an idle subtree or raise
  `rlm.maxWorkers`; no rejected child record was committed.
- **A child finished but the parent has no answer:** the parent received an
  `agent.completed` message with a preview and evidence handle; use
  `messages.list/read`. Ordinary child output is otherwise local.
- **MCP unavailable:** call `mcp.list_servers()` or use `/mcp`; reconnect or
  configuration errors stay isolated from the agent loop.
- **Interrupted command after restart:** inspect external state before issuing
  a new command. The runtime will not replay it automatically.
