# RLM context, MCP inheritance, and limits audit

Inspected 2026-09-08 UTC. Research and recommendations; no runtime changes made.

Evidence: the live session `b4732d98979dcac406676723ba344ef8` on runtime
`b200e8142f5e28dbda106b4664d57ec5`, read-only SQLite queries, live `context.audit`
and `mcp.status` queries, source inspection, and isolated authority-free kernel
experiments with mocked host responses. The live daemon is the development
binary `v0.5.13-101-g04b4c062-permission-fix`, using `~/.whip-web-v4`.
Source was reviewed through checkout `a7e1a2e61` plus concurrent working changes;
this does not establish that every current source change is in the live binary.

## Findings

### MCP grants are not automatically inserted into the child's model context

Every recursive agent installs one exclusive model-facing tool, `rlm_exec`.
`AgentSession.refreshPrompt` composes runtime help, identity, operating rules,
environment, applicable project instructions, a skill catalog, and standing
instructions. The prompt builder takes no MCP grant/catalog input.

The model receives generic instructions about MCP discovery and permissions.
An ephemeral budget explanation is also added at turn start, including when
all model budgets are unlimited. Neither is a per-tool allowlist.

MCP descriptions, argument schemas, and server instructions become model-visible
when the agent requests them and prints or returns the result of the Starlark
call. Keeping a value in a Starlark global alone does not place it in the model
prompt.

Live root context audit:

| Item | Observed size | Model-visible? |
| --- | ---: | --- |
| System prompt | 37,920 bytes | Yes |
| Runtime help component | 10,911 bytes | Yes; included in system prompt |
| 50 skill catalog entries | 19,733 bytes of names/descriptions/paths, before catalog framing | Yes; included in system prompt |
| Model-facing tool schema | 712 bytes, one tool | Yes |
| MCP host schemas | 639,798 bytes, 780 tools | No automatic insertion |

These rows overlap and must not be summed as independent prompt costs.
The child's first provider response reported 10,295 input tokens, including
its task and environment. Its recorded host calls, at inspection, included
files, shell, state, and context operations, with no MCP discovery or calls.

Sources: `internal/daemon/recursive_runtime.go:newNode`,
`internal/daemon/prompt.go:refreshPrompt`, `internal/rlm/environment.go:ComposePrompt`,
`internal/agent/agent.go:turn`, `internal/daemon/agent_session.go:ContextAudit`.

### Why this child inherited 780 tools

The parent omitted `capabilities` and `mcp_tools` in `agents.spawn`. Omitted
capabilities inherit the parent's capabilities, including MCP. The runtime then
enumerates every currently advertised tool that the parent is authorized to
delegate. It does not filter that list by the child's coding task.

The live root has a compact `mcp_all: true` grant. Children receive a finite
snapshot of exact tool definitions, so newly added or changed tools do not
silently expand their authority. Each selector contains a server name, tool
name, and definition fingerprint. Call-time consent and revocation checks still
apply; a selector is not a credential or an unconditional approval to call.

The 780 selectors came from 12 ready MCP servers:

| Server | Tools |
| --- | ---: |
| inference | 426 |
| ahrefs | 135 |
| getleads | 47 |
| paper | 34 |
| chrome-devtools | 29 |
| firecrawl | 26 |
| playwright | 24 |
| playwright_live | 24 |
| x-twitter | 15 |
| analytics-mcp | 9 |
| gsc | 8 |
| exa | 3 |

The live manager reports these servers imported from `~/.codex/config.toml`,
except `exa` and `firecrawl`, which came from `~/.claude.json`. Disabled,
connecting, and failed servers did not contribute to this snapshot.

Sources: `internal/daemon/mcp.go:delegatedMCPTools`,
`internal/session/capability.go:MCPSelectors`, `internal/capability/mcp.go`,
`internal/tools/mcp.go:mcpRegistration`.

### Why the parent receives the list

`spawn` persists the child and delegated authority, publishes the child, wakes
it, then returns a map containing `effective_mcp_tools: mcpTools` alongside its
ID, status, capabilities, and budgets. `agents.inspect` also returns the full
selector list. This is informational return data; scheduling and authorization
already have their own authoritative state.

The full return field was introduced with MCP delegation hardening in commit
`632949ecf`. Exact persisted grants support authorization and recovery. Their
presence in every spawn receipt is not needed for those properties.

The observed failure sequence was:

1. The child was admitted and started successfully.
2. Printing the returned allowlist exceeded the cell's 65,536-byte output cap.
3. The cell returned an error after the spawn effect had committed.
4. The parent retried, encountered two Starlark syntax errors, then the truthful
   duplicate-name error, before recovering through `agents.list()`.

The checkpoint omission was a separate warning. It was not the cause of the
output-limit error. The parent incorrectly described it as the only problem.

Isolated worker experiments using the persisted selector data reproduced:

- Default output cap: printing the full response fails; `child["id"]` remains
  accessible in the live worker.
- Output cap raised to 256 KiB: printing succeeds (111,112 bytes in the reduced
  test response), but `child` still exceeds the 256 KiB scratch binding cap.
  Scratch uses a different encoded graph representation and size accounting.
- A compact receipt prints successfully and checkpoints successfully (485-byte
  snapshot in the earlier control experiment).

No real agents or MCP effects were created by these experiments.

Another exposed path: `mcp.list_tools(server=...)` returns every tool's full
description and schema, including unauthorized entries marked `authorized=false`,
without pagination or the `boundedText` handling used for normal MCP outputs.
This can recreate oversized results during discovery.

Sources: `internal/daemon/recursive_runtime.go:spawn`, `inspect`, `mcp`,
`internal/rlm/worker.go:evaluate`, `internal/rlm/scratch.go`,
`internal/rlm/scratch_codec.go`.

## Limits with the largest implications for agent quality

| Current behavior | Configuration / rationale | Assessment |
| --- | --- | --- |
| Cell output: 64 KiB, overflow fails the cell | `rlm.outputBytes`; memory/output protection | Keep bounded capture, but spill or summarize oversized output and preserve truthful effect outcomes. The current failure behavior is confirmed harmful. |
| Input above 8 KiB becomes a 4 KiB head + 2 KiB tail and content handle | Reuses the database `InlineValueLimit` constant | Storage layout is deciding what the model sees. Long user instructions and child tasks can lose their middle from immediate context despite ample model capacity. |
| Many host outputs above 8 KiB become handles with 4 KiB previews; context reads cap at 8 KiB | Same shared inline constant | Full data survives, but unrelated storage and model-presentation limits are coupled. Small reads can create unnecessary retrieval work. |
| Restoration keeps at most eight qualifying user/assistant messages, each at most 8 KiB, plus an at-most-8-KiB summary | Fixed `FocusedHistory` policy; used for restored roots and children | Drops tool exchanges from immediate context regardless of available model capacity. Raw transcripts remain retrievable, but restoration differs substantially from uninterrupted execution. |
| Proactive compaction defaults to 50% of model context | Configurable `compactPct`; uses provider-reported token usage when available | A policy choice, not a provider requirement. Evaluate quality and cost before choosing a default. |
| Compaction tail target clamps to 2,000–15,000 tokens; summary output caps at 4,096 tokens | Mostly fixed constants; whole turns may exceed the tail target | Scaling largely stops at 15,000 tokens even on large-window models. |
| Compaction input clips user/assistant text to 2,000 characters and tool arguments/results to 500 | Fixed `writeTranscript` limits | The summarizer can miss key constraints or evidence before it has a chance to summarize them. Raw references survive, but require deliberate recovery. |
| Old tool-output decay uses a 24,000-token hot window and 8,000-byte minimum | Fixed constants, applied at turn boundaries | Another independent context-retention policy to evaluate alongside compaction/restoration. |
| Scratch: 256 KiB per binding, 768 KiB aggregate; frame: 1 MiB | Scratch caps fixed; `rlm.frameBytes` configurable | Resource boundaries are useful, but interacting representations/caps create surprises. Omitted scratch is not equivalent to a failed host action. |
| Workers: 4; active children: 8; concurrent child turns: 4; recursion depth: 2 | Worker count configurable; durable caps separate | Real capacity controls. Defaults and scheduling behavior should reflect host resources and intended parallelism. |
| Tokens, cost, cumulative model-request time | Unlimited by default and verified unlimited for this session | These are already opt-in restrictions, not the cause of this incident. A model request still has a default 10-minute deadline and a finite output ceiling. |

Additional resource bounds include 1,000,000 Starlark steps, 1,024 host calls,
30 seconds of Starlark compute per cell, 256 MiB worker memory, and per-root
durable storage/record limits. Those protect execution and retention; they
should be evaluated separately from how much useful context reaches the model.

The reviewed paths contain explanations and boundary tests, but I did not find
quality evaluations supporting the particular fixed preview, restoration, and
compaction values. Their quality impact beyond the reproduced spawn incident
is a risk to test, not a measured regression claim. This session had no recorded
compaction when inspected.

## Recommended sequence

1. Return compact spawn and inspect summaries. Keep complete permission state
   in the daemon and expose exact details on demand. Preserve intended access
   inheritance; shrinking access is not required to reduce prompt overhead.
2. Give MCP discovery targeted search and single-tool schema retrieval, with
   bounded/paginated catalog results. Ordinary calls and instructions already
   have handle-backed output handling; apply the same principle consistently.
3. Make output overflow produce a usable preview/content reference and explicit
   truncation information. Distinguish execution failure, output presentation,
   checkpoint omission, and already-committed host effects.
4. Separate storage-inline thresholds, transport sizes, scratch limits, and
   model-context policies. Preserve full user instructions when they fit the
   chosen model, and retain a coherent recent history across restart.
5. Evaluate context policies against the model's actual window, reserved output,
   and task needs. Measure instruction retention, task completion, repeated reads,
   duplicate actions, latency, and cost. Include large tool catalogs, long tasks
   with critical middle instructions, restart continuity, and compaction cases.
6. Review the 50-skill catalog and long static runtime help as prompt overhead.
   Measure a compact catalog with explicit discovery; preserve user/project
   instructions and required skill behavior.

Raising every ceiling would leave the large-response design, inconsistent
truncation, and misleading outcome reporting unresolved. The objective is useful
model context with predictable resource controls and recoverable large data.
