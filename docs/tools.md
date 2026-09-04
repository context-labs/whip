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
| `mcp` | `list_servers`, `list_tools`, `call` |
| `state` | private/blackboard get, set, append, CAS, list/history, subscriptions |
| `artifacts` | `put`, `inspect`, `read` |
| `schedules` | `create`, `list`, `cancel` |
| `permissions` | `request`, `status`; a kernel cannot approve |

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
mcp.call(server="docs", tool="search", arguments={"query": "leases"})
```

Calls are addressed by server and original tool name. The manager preserves
normal connection, timeout, reconnect, and per-server serialization behavior.
Large results become handles through the same bounded-output path as built-in
operations.

`whip mcp serve` is a protocol bridge for external MCP clients. It hosts
daemon-owned tool services directly and does not create a model agent.

## Authorization and output

- File and shell operations use the capability dispatcher with the calling
  agent’s identity and grants.
- Omitted child capabilities inherit the parent set; an explicit list may
  only narrow it.
- Permission approval is human/protocol-side and revalidates the exact
  operation before it resumes.
- Inline output is bounded. Larger content is stored immutably and returned
  with a handle, source, size, and readable spans.
