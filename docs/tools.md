# Tools and host modules

The model-facing catalog has one entry:

| Tool | Purpose |
| --- | --- |
| `rlm_exec` | Evaluate one bounded Starlark cell against daemon-hosted modules |

This is true for roots and children. MCP discovery does not add tools to a
model request; configured MCP operations remain under the `mcp` module.

## Modules

All operations accept keyword arguments.

| Module | Operations |
| --- | --- |
| `context` | `inspect`, `search`, `read` supplied or history handles |
| `files` | `list`, `search`, `read`, `write`, `patch` |
| `shell` | `run` (blocking, 120 s cap), `read` handle-backed output, background jobs: `start`, `poll`, `tail`, `wait`, `kill`, `list` |
| `browser` | `run` |
| `computer` | `run` |
| `models` | `call`, `batch` for stateless model work |
| `agents` | `spawn`, `submit`, `wait`, `inspect`, `list`, `stop`, `delete` |
| `messages` | `send`, `list`, `read`, `complete`, `defer` |
| `mcp` | `list_servers`, `list_tools`, `instructions`, `call` |
| `state` | private/blackboard get, set, append, CAS, list/history, subscriptions |
| `artifacts` | `put`, `inspect`, `read` |
| `schedules` | `create`, `list`, `cancel` |
| `permissions` | `request`, `status`; a kernel cannot approve |
| `user` | `ask`; root agent only |

Example:

```python
matches = files.search(path=".", query="TODO")
reviewers = models.batch(prompts=[
    "Identify the risky change",
    "Identify missing tests",
], max_tokens=800)
child = agents.spawn(
    name="reviewer",
    prompt="Review the persistence changes and message the parent with findings",
    capabilities=["read", "shell"],
)
{"matches": matches, "reviewers": reviewers, "child": child}
```

Starlark is not Python: there is no `import`, `open`, or `try/except`.
Interpreter globals survive worker and daemon restarts except closures and
self-referential values (see the scratch snapshot in `rlm-runtime.md`). Use
`state`, `artifacts`, messages, and retained children for shared work.

## user.ask

`user.ask(question="...", options=[{"label": "...", "description": "..."}, ...], multiple=False)`
shows the user a floating dialog with 2 to 6 options (unique, non-empty
labels; descriptions optional) and blocks the cell until they pick, dismiss,
or the turn is cancelled. Host time is not charged to the cell clock. It
returns `{"answer": [labels...], "dismissed": bool}`; a dismissed question
has an empty answer. Only the root agent may ask; a descendant gets the error
`only the root agent can ask the user; send your parent a message instead`,
and one question per agent is open at a time. Clients hear about it through
the `question.pending`, `question.answered`, and `question.closed` events and
answer with the `question.answer` client op; a client that connects while a
question is open finds it in the session snapshot. The blocked cell keeps its
kernel pool slot while it waits, so a root waiting on the user holds one of
the pool's `MaxWorkers` slots. Headless `whip run` prints the
pending question and leaves it open; under ACP the question appears as a
permission prompt with one option per label plus Dismiss, so `multiple=True`
collapses to a single answer there.

## Choosing between models and agents

Use `models.call` or `models.batch` for independent stateless analysis. Use
`agents.spawn` when work needs an identity, capabilities, a transcript,
follow-up turns, messages, or further recursive delegation.

Spawn is asynchronous and returns admission metadata, not an answer. When a
child turn ends the parent receives an `agent.completed` message with a short
preview and an evidence handle; a child sends `messages.send` for anything
more. Mail reaches a turn as a bounded digest of excerpts; the parent loads
full bodies with `messages.read` and finishes them with `messages.complete`.

## MCP

The daemon owns MCP connections. Both root and child kernels call:

```python
mcp.list_servers()
mcp.list_tools(server="docs")
mcp.instructions(server="docs")
mcp.call(server="docs", tool="search", arguments={"query": "leases"})
```

Calls use the exact configured server and original tool name. `list_tools`
marks which definitions the caller is authorized to use. Each call runs through
the durable capability dispatcher, reserves operation capacity, and rechecks its
grant and current server definition after permission and the server's call queue.
Large results become handles through the same bounded-output path as built-in
operations.

Servers explicitly configured in native WHIP configuration are trusted. Imported
Claude/Codex definitions retain their provenance, including when saved by the
import command; ACP attachments also require consent or a saved allow rule.
Only the daemon's native configuration establishes native trust. A client cannot
claim it or replace a native definition by attaching a server of the same name.
Explicit permission denials and revoked grants still win. Headless execution
uses preauthorization or denies promptly; it never waits for a permission UI.

Children inherit the parent's currently available MCP tools when capabilities
are omitted. `capabilities=["read"]` has no MCP access. An explicit list can
include `"mcp"`, optionally narrowed by
`mcp_tools=[{"server": "docs", "tool": "search"}]`. These exact definitions are
persisted with the child's issuer grant. New tools or changed endpoints and
schemas do not expand an existing child's authority; ancestor revocation still
applies after restart.

`mcp.instructions` returns server usage guidance with its source and connection
generation. Large guidance returns a handle for bounded `context.read` calls.
Instructions and tool annotations do not authorize effects. Replacing or
reconnecting a manager invalidates pending calls; transmitted calls are never
automatically retried because their external outcome may be uncertain.

`whip mcp serve` is a protocol bridge for external MCP clients. It hosts
daemon-owned tool services directly and does not create a model agent.

## Authorization and output

- File, shell, and MCP operations use the capability dispatcher with the calling
  agent’s identity and grants.
- Omitted child capabilities inherit the parent set; an explicit list may
  only narrow it.
- Permission approval is human/protocol-side and revalidates the exact
  operation before it resumes. Approving "always" installs a rule (the
  arity-collapsed command prefix, the canonical path, or the exact MCP server,
  raw tool name, and definition digest) for the session
  tree; `permissions.allow` in the config holds the global `operation:rule`
  allowlist, and `/permissions` lists or forgets tree rules.
- Inline output is bounded. Larger content is stored immutably and returned
  with a handle, source, size, and readable spans.
