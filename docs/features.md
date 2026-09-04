# Feature map

whip is a local recursive coding-agent runtime. This page describes the
current architecture; package and test names are included where they make a
contract easier to locate.

## Recursive agent runtime

- One `AgentSession` implementation is used for roots and descendants
  (`internal/daemon/recursive_runtime.go`).
- Every provider request exposes exactly `rlm_exec`; MCP discovery cannot
  widen the model-facing catalog (`internal/agent/rlm_test.go`).
- `agents.spawn` creates a retained asynchronous child with the same interface
  as its parent. `inspect`, `list`, `stop`, and `delete` provide lifecycle
  control.
- Omitted capabilities inherit; explicit capabilities narrow. Budgets roll up
  through ancestry and enforce tokens, cost, elapsed time, depth, active
  children, and concurrent turns.
- Worker capacity is reserved before child persistence. Rejection leaves no
  child record (`TestRecursiveSpawnRejectsCapacityBeforeAdmission`).
- The default recursion limit is root → child → grandchild.
- Restart reconstructs retained nodes, transcripts, authority, route, and
  kernels (`TestRecursiveRuntimeRestoresRetainedAgentAndTranscript`).

## Starlark execution

`internal/rlm` owns the framed worker protocol and module registry.

- Each kernel serializes its cells so small globals persist within a worker.
- Cells are bounded by steps, host requests, wall time, memory, output bytes,
  and frame bytes.
- The worker has no ambient daemon/provider credentials or host I/O API.
- A crash discards globals, not durable host state.
- Large cell results and host outputs become content handles.

Available modules are summarized in [tools.md](tools.md).

## Focused context

- Full history and oversized inputs are stored behind content handles.
- A request carries at most four recent user/assistant exchanges and one
  bounded summary.
- `context.inspect/search/read` returns source metadata and byte spans.
- Proactive and reactive compaction protect the provider context window.
- Large values are immutable, content-addressed, and separately authorized.

The deterministic evaluation expands a corpus above 500 KB and proves the
answer can be found through bounded reads without copying the corpus into the
root prompt (`evals/rlm`).

## Messages and collaboration

- A child’s ordinary assistant response is local to its transcript.
- `messages.send(delivery="steer"|"queued"|"next_turn")` stores a durable
  body; readiness is derived from pending mail, not a separate wake row.
- `messages.list/read/complete/defer` make body admission and lifecycle
  (`pending → delivered → done`) explicit; delivery is committed with the turn.
- Bursts retain every message and derive one ready signal
  (`TestMailboxBurstDerivesOneReadySignal`); a turn receives one bounded digest.
- Evidence handles can be granted to a direct relative with the message.
- Child turn results arrive as `agent.completed|failed|cancelled` messages
  with a short preview and evidence handle; they do not inject child output.
- Private state is agent-scoped; blackboard state is shared and supports
  append, compare-and-swap, history, and subscriptions.

## MCP

- Stdio and streamable HTTP servers are discovered from project, Codex, and
  WHIP configuration (`internal/mcp`).
- Root and child kernels use `mcp.list_servers/list_tools/call`.
- Provider tool catalogs remain stable at one tool while MCP servers change.
- Connections have startup/call deadlines, per-server serialization,
  reconnect generation guards, and bounded structured/media flattening.
- `whip mcp serve` is a daemon protocol tool host, not a model agent.

## Built-in capabilities

- `files`: list/search/read/write/patch with canonical path authorization and
  same-path mutation ordering.
- `shell`: managed process groups, bounded output, interactive PTY support,
  and workspace-wide effect authority.
- `browser`: live/dedicated/headless/extension backends behind daemon policy.
- `computer`: macOS accessibility and screenshots with per-app policy.
- LSP diagnostics can be attached after file changes.
- Human permission requests are durable, signed at the client boundary, and
  revalidated before an exact operation resumes.

## Provider loop and models

- OpenAI-compatible streaming with retry events and usage accounting.
- Model-to-provider routing, live catalog discovery, context/output limits,
  reasoning effort, vision flags, sampling parameters, and pricing.
- `models.call` and `models.batch` provide stateless analysis without creating
  durable child identities; batch results retain input order.
- Prompt-cache keys are stable per retained session.

## Daemon and clients

- The daemon is the only runtime/store owner.
- TUI, `whip run`, sessions commands, ACP, and MCP stdio are protocol clients.
- Stable command IDs make retries idempotent.
- Ordered replay and behavioral snapshots restore client presentation state.
- Slow clients lose their bounded connection instead of blocking a root.
- Schedules and blackboard subscriptions create durable wakeups.
- Process shutdown is root-owned and waits for supervised workers.

## Terminal UI and adapters

- Streaming text, reasoning, tool, plan, permission, usage, and terminal
  events are rendered from daemon events.
- `/agents` inspects or controls the recursive tree; `/mcp` manages server
  lifecycle.
- `/model`, `/effort`, `/goal`, `/compact`, `/rewind`, `/fork`, `/schedule`,
  `/browser`, and `/computer-use` are daemon commands.
- ACP maps editor sessions and permission decisions onto the same root
  protocol. It does not own a second agent loop.
- In opencode mode, `ctrl+x r` (or `/repl`) turns the right sidebar into a
  live Starlark REPL for the visible agent: code as the model writes it,
  print output as it happens, each host call with its duration, results,
  errors, and worker restarts. Cells are cards on the panel shade, a hairline
  separates the REPL from the chat, and the mouse wheel over the REPL scrolls
  it independently of the chat (it follows the newest cell until you scroll
  up). In opencode mode the agent tree lives in the
  right panel (Context and REPL modes alike) instead of under the input;
  `ctrl+t` or ↓ on an empty input focuses it, ↑/↓ select, enter opens,
  `ctrl+x` stops, esc leaves. The root heads the tree: enter on it, or esc
  with an empty input, returns from a child to the main transcript. The REPL
  panel follows whichever agent is open and keeps every cell seen during the
  TUI session, even after snapshots drop idle children.

## Storage and recovery

Runtime-v2 stores command/event journals, agents, per-agent transcripts,
messages, state, capabilities, budgets, permissions, schedules, and content
references in SQLite WAL plus immutable content files.

On restart:

- committed command outcomes remain final;
- running descendant sessions become retained idle sessions;
- queued recursive notifications remain actionable;
- uncertain operations are interrupted and reservations are reconciled;
- external side effects are never guessed or replayed automatically.

See [architecture.md](architecture.md), [rlm-runtime.md](rlm-runtime.md), and
[concurrency.md](concurrency.md) for the contracts behind these features.
