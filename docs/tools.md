# Tools and host modules

The model-facing catalog contains one `execute` tool for both roots and children.
It evaluates a bounded Starlark or JavaScript cell. Captured module bindings
select the available syntax; the host separately authorizes every operation.
MCP discovery does not add direct tools to the model catalog.

[The runtime guide](rlm-runtime.md) explains execution and checkpoints. The
[domain contract](backend-domain.md) owns precise admission, authority, resource
and failure semantics. Host vocabulary lives in
[`internal/hostmodule/modules.go`](../internal/hostmodule/modules.go); declarations
and schemas are generated from the current captured definition, not this table.

## Modules

Starlark host functions accept keyword arguments. JavaScript host functions take
an options object and return a promise. The local `json` helpers do not contact
the host. A definition can expose only a subset of the following modules.

| Module | Purpose and principal operations |
| --- | --- |
| `context` | Bounded `inspect`, `search`, `read` and `history` for the current owner's evidence. |
| `files` | Workspace `list`, `search`, `read`, `write`, `patch` and `diagnostics`. |
| `skills` | Read an explicitly discovered skill. Text never grants additional authority. |
| `shell` | Foreground `run`, scoped output `read`, and background `start`, `poll`, `tail`, `wait`, `kill`, `list`. |
| `browser` | Offered-page `list_tabs`, `open`, `attach`, `run`, `detach`, `allow_preview_port`. |
| `computer` | Permission-gated `run` against allowed host applications. |
| `models` | Accounted stateless `call` and ordered `batch`. |
| `agents` | `spawn`, `submit`, `inspect`, `list`, `stop`, `delete`; exact-input waits and pending completion reports. |
| `mail` / `messages` | `send`, `list`, `read`, `complete`, `defer` with observed revisions and explicit delivery. |
| `mcp` | Catalog `list_servers`, `list_tools`, `search`, `describe`, `instructions`; authorized `refresh`, `reconnect`, `call`. |
| `state` | Revisioned `get`, `read`, `write`, `append`, `list`, `history` and subscriptions in session or tree scope. |
| `artifacts` | Immutable owner-scoped `put`, `inspect`, `read`. |
| `goals` | Explicit `complete` for the current captured goal. |
| `schedules` | `create`, `list`, `cancel` with durable due-input admission. |
| `permissions` | `request`, `status`; a guest cannot approve itself. |
| `user` | Root-only `ask` with durable pending and terminal outcomes. |

Starlark is not Python: it has no `import`, ambient `open` or `try/except`.
Neither engine has ambient filesystem, network or process authority. Supported
checkpoint state can survive eviction and restart without evaluating old cells.
A saved alias never bypasses the current turn's captured bindings or grants.

## Desktop Browser helper mode

Desktop Browser tabs remain experimental. Explicitly offered pages and an exact
provider connection supply the browser target; availability, tab metadata and
an attachment ID do not grant control. Open, attach and preview-port expansion
use captured resource identity and the native permission dispatcher. Missing or
ambiguous selection fails without silently choosing another page.

The helper batch runs against the same page shown to the person. Rod and
ChromeDP are interpreter choices, not alternate authority or browser-selection
paths. Preview routes stay bound to the selected execution host. Disconnect or
release ends control without closing human pages, and reconnect does not restore
an old attachment. See [Browser and computer use](browser-computer-use.md),
[desktop behavior](desktop.md#browser-tabs-experimental) and
[the SDK](../packages/sdk/README.md).

## Structured state

Session state is private to its owner. Tree state is shared within the current
tree. Every write names the observed revision; `"0"` creates an absent key.
Read the new revision before choosing a subsequent write or append:

```python
saved = state.write(scope="session", key="progress", expected_revision="0",
                    value={"done": False})
entry = state.get(scope="session", key="progress")
print(entry["value"])
```

Values are bounded JSON-compatible data. Non-finite numbers, cycles and live
resources are rejected. Large values retain immutable bodies and exact byte
metadata; read them through the corresponding scoped state/content operations.
Private/shared visibility, CAS conflicts, history and subscription delivery are
specified in [explicit state](backend-domain.md#explicit-state-and-immutable-values).
Inspection does not acknowledge mail or start an agent turn.

## user.ask

```python
answer = user.ask(question="Choose", options=[
    {"label": "A", "recommended": True}, {"label": "B"}
])
print(answer["answer"])
```

A question has 2–6 unique options, optional descriptions and at most one
recommendation. The batch form accepts 1–8 questions. Clients can answer or
dismiss; cancellation, expiry and restart produce explicit terminal states.
Only a root may ask. A child must communicate with its parent.

The native host records the question as an ordinary operation. Clients inspect
`questions.list` / `questions.get` and answer with `questions.answer`; connecting
late does not require replaying a notification to discover pending work. ACP presents the
question through editor permission choices; its single-choice surface does not
represent arbitrary multiple selection. See the native
[question contract](../internal/session/question.go) and
[client protocol](protocol-v2.md).

## Choosing between models and agents

Use `models.call` or `models.batch` for independent stateless analysis. They
capture their route, instructions, sampling and output limits, record every
attempt and preserve input order. Large output becomes scoped evidence. A
failed item does not erase already incurred accounting.

Use `agents.spawn` when work needs an identity, independent history, subsequent
turns, mail or further delegation. Spawn returns an admission with an exact
`input_id`. It commits the child, captured configuration, scoped original input
and delegated grants together. `grant_ids: null` inherits valid standing grants;
`[]` delegates none. A child cannot widen its ancestor's authority or module
ceiling, and one-use approval is not a delegatable grant.

```python
child = agents.spawn(prompt="Review the persistence changes.")
agents.wait_after_cell(input_ids=[child["input_id"]])
```

The registration returns immediately. Finish the cell to commit its outcome
and release the execution/kernel permits before waiting. This permits recursive
progress with one worker; it replaces same-cell blocking joins. Failed,
interrupted or cancelled child outcomes are data, not permission to retry them.
Parents receive captured completion reports and can inspect pending evidence
without rerunning a child. See [recursion](rlm-runtime.md#sessions-and-recursion).

## MCP

The native host owns bounded MCP connections and cached catalogs. Guest reads
use exact configured server and original tool names:

```python
mcp.search(query="lease search", limit=5)
mcp.describe(server="docs", tool="search")
mcp.call(server="docs", tool="search", arguments={"query": "leases"})
```

Catalog inspection does not itself connect a server. Explicit refresh or
reconnect has its own authority and lifetime. Catalog results are bounded;
large schemas/instructions use owner-scoped metadata evidence. A catalog entry
or server instruction is never permission to execute.

Native declarations live in `runtime-v4/host.json`. Project `.mcp.json`, Codex,
Claude and OpenCode declarations are import sources with recorded provenance.
Discovery does not make them trusted. Explicit import/configuration materializes
the chosen declaration into the native host configuration. Attached client
servers remain untrusted and cannot replace a native declaration of the same
name. The captured session selection still limits which servers are available.

Calls freeze exact endpoint/tool/schema identity and recheck permission after
waiting for capacity. Text, structured content and typed attachments retain
canonical operation evidence. Authorized images can enter model context through
scoped content hydration; foreign references cannot be copied as authority.
Changed declarations, lost connections or uncertain transmitted effects are
never automatically replayed. See the [MCP runtime](../internal/runtime/mcp_operations.go)
and [checked connection manager](../internal/mcp/manager_call.go).

`whipcode mcp serve` exposes a restricted native endpoint with read/write/edit/
bash and Browser aliases. It uses an isolated model-free session, preauthorizes
workspace reads and denies interactive permission acquisition. A declared tool
is not a grant: writes, shell effects and browser control still require native
authority. Closing the MCP stream joins its own work; it does not shut down the
shared native host. The endpoint does not expose interpreter execution.

## Authorization and output

Operations record admission, captured authority, dispatch and outcome separately.
Approval revalidates the exact current resource before dispatch. Saved standing
grants have explicit capability/resource scope and issuer chains; ancestor
revocation and explicit denial still apply. The client cannot infer permission
from an exposed schema or a previous successful operation.

Inline output and live previews are bounded. Larger evidence has explicit
owner, digest, size, offsets and continuation. Truncation or unavailable content
must remain visible. Retrying a local observation or database settlement cannot
repeat a provider request, shell command or external mutation whose outcome is
uncertain.
