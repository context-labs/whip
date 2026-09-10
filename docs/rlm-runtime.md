# Recursive runtime

RLM is whip’s execution model, not an optional mode. Every root and child
model sees `rlm_exec`; its Starlark or JavaScript cells call daemon-hosted modules. There is
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
- one bounded kernel using its root session's immutable execution engine;
- a durable transcript and private state;
- an agent ID, parent ID, capabilities, and effective budgets.

Capabilities omitted at spawn inherit the parent’s set. An explicit list only
narrows it. The default maximum depth is two edges. Child names must be unique
under one parent, and child admission fails before persistence if no kernel
worker is available.

Spawn receipts contain only `id`, `name`, `parent_id`, `status`, and `report`.
`agents.inspect(id=...)` returns current state, capabilities, and budgets.
Exact MCP selectors are loaded only with `include_grants=True`, in a
`mcp_grants` inline JSON result or a caller-owned content handle. The JSON
includes `all` and `selectors`, distinguishing a root's all-tools grant from
an agent with no MCP access. This keeps large permission snapshots out of
ordinary responses without changing delegation or authorization. See
[agent inspection](tools.md#choosing-between-models-and-agents) for retrieval
and response-shape migration examples.

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
showed that exact revision commits successfully (a failed turn shows it
again). `messages.read` records a delivery receipt, `messages.complete`
finishes the observed revision, and `messages.defer` returns it to `pending`
at a later time with a new revision. Listing metadata records an observation
for explicit controls without automatically acknowledging delivery. If a
message changes before an explicit completion or deferral, reread it before
retrying. Batch completion is atomic: one stale revision rejects the batch.

Bodies are never pushed whole into another model's prompt. A turn that starts
with ready mail receives a bounded digest: one line per pending message with
sender, kind, subject, size, and a 2 KiB excerpt. Child turn results reach the
parent the same way as runtime-authored `agent.completed`, `agent.failed`, or
`agent.cancelled` messages with a short preview and an evidence handle for the
full text; blackboard subscriptions post `state.changed` messages upserted per
subscription. Readiness is derived from durable state (`queued` inbox rows and
`pending` mail), so an in-memory wake is only an optimization.

Every normal node's system prompt includes an identity block (id, name, parent,
depth, report mode) and a child's first input is `[task from parent <name>
(<id>)]` plus the prompt, so `messages.send(recipient="parent")` always works;
a direct relative's name or id is accepted too. Messages travel one hop
(parent, child, sibling), so there is no `root` alias. Parents steer children with
`agents.submit(id, text, delivery="steer"|"queued")` and can block briefly on
`agents.wait(ids, timeout_ms)` (default 10 s, capped at 25 s so a blocked cell
releases its kernel pool slot promptly; host-call time is not charged to the
cell clock; the result carries per-child status plus `settled` and
`timed_out`, never the reply itself). The
system prompt tells every node that mail wakes it, so the expected pattern
after a spawn or submit is to end the turn and let the reply arrive as a
mailbox-triggered turn. `agents.spawn(report=...)` picks how a child's
turn end reaches the parent: `notice` (default, 160-byte preview plus evidence
handle), `inline` (4 KiB preview), or `message` (only failures; the child must
report explicitly). The selected mode is persisted and restored before the
child's identity prompt and next turn are built. Sender caps: 16 KiB body (use an evidence handle above
that), 20 pending messages per sender→recipient pair, 30 sends per 10 seconds.

## Input and fork continuity

Root input, child tasks, and human steers use the same recipient-authorized
payload decoder. Referenced payloads are read completely in chunks of at most
64 KiB. The total input ceiling is 64 MiB, matching uploads and including
serialized multipart content and the child task prefix. The transport frame
remains 1 MiB; large input uses the upload/content path. RLM focusing still
controls what enters the model prompt after complete input decoding.

Malformed, inaccessible, missing, and oversized payloads fail explicitly.
They do not terminate the root or leave a child turn running. Invalid boundary
steers are settled individually so later valid work remains usable.

A fork copies the selected raw transcript prefix, root compactions fully covered
by that prefix, and a snapshot of active content
grants readable by the source root. Root grants retain their scope; grants to
the source root agent/subtree are remapped to the destination root identity.
Child-private and revoked grants, live agents, queues, subscriptions,
schedules, and scratch are excluded. Content references remain immutable and
shared, while the fork's access survives source deletion or later revocation.
Deleting a root removes its dependent mailbox and descendant transcript rows
and leaves shared content references/objects intact.

## Context and handles

Restoration focuses the model view to at most four recent user/assistant
exchanges and one bounded summary. Root and child transcripts remain raw,
append-only message logs; focusing, decay, and compaction affect the model view.
Raw deltas, per-agent compactions, input settlement, and delivery receipts commit
in the same turn transaction. Summaries carry raw sequence coverage rather than
positions in the shortened model view. Tool arguments/results and multipart
content remain retrievable after compaction and restart.

`context.inspect()` describes the caller's retained history. `context.history`
lists at most 20 message previews using `after_seq`, `limit`, and `through_seq`;
read an individual message with `seq`, optional `field`, `offset`, and `length`
(up to 8192 bytes). `field="message"` returns the serialized message; `content`,
`parts.N.text`, and `tool_calls.N.arguments` address decoded text. Follow the
complete returned continuation for partial fields, including `message_revision`
when supplied. For list pages use `after_seq=next_seq`, `through_seq`, and
`turn_id` when present. Changed provisional message metadata requires a fresh
read from offset 0. Sequence IDs are agent-local; this
API grants no parent/sibling transcript access. Clear and rewind invalidate old
history cursors; inspect again after either operation.

`context.search(query="...")` searches decoded text and tool arguments in the
caller's raw history, including current-turn journal entries already removed
from the model view. Those entries are explicitly provisional and identify
their turn. Provisional cursors expire when their originating turn journal is
replaced; inspect again. Committed pagination freezes an upper sequence so later
appends do not shift pages. Storage errors are errors, never empty results.

Explicit content-handle inspect/read/search forms remain available. Search is
case-sensitive, literal, and non-overlapping, including across 64 KiB read
boundaries. Queries are at most 64 KiB; each call scans at most 8 MiB and returns
at most 20 matches (history also caps messages examined at 128). A partial search
reports its stop reason and continuation, never an unqualified absence of
matches. Match spans identify exact source bytes; valid UTF-8 excerpts have
separate bounded text spans. Large inputs and host outputs still use immutable
content handles. No constructor snapshot represents the full conversation.

## Environment prompts

`AgentSession.RunTurn` composes one environment prompt at the shared root/child
turn boundary from the node's agent definition (`internal/agentdef`). Focusing
never replaces it. The definition supplies the persona, its own operating
rules, and the discovery it wants; the runtime supplies the `rlm_exec` guide,
assembled from per-module fragments for the modules the definition selects
(`rlm.RuntimeGuide`). In order, a normal prompt holds the persona and runtime
guide, identity/report mode, the definition's rules plus the instruction-scope
block when project files are discovered, cwd/platform/time/user, scoped project
instructions, the applicable skill catalog, and standing `me.md` instructions.
The coding definition selects every module and every discovery source. A child
composes from its own definition, which is its parent's narrowed by the spawn
arguments. `/me`, cwd changes, reload/model replacement, and restored children
use the updated sources on their next turn. A running turn keeps its applied
prompt. The root `-system` override remains exact, does not propagate to
children, and survives a model change or reload because the session re-applies
its run configuration to the replacement runtime.

Project instructions load only along the applicable workspace-root-to-cwd
ancestor chain, broad to specific. At each directory CLAUDE.md precedes AGENTS.md;
AGENTS.md wins conflicts at that directory. Narrower applicable rules override
broader ones, and explicit user instructions remain authoritative. Children
retain applicable ancestor rules. Deeper subtree rules are discovered on demand
through existing file operations. Instruction text does not expand authority.

Each project or standing-instruction source is limited to 64 KiB; skill metadata
reads and the total assembled prompt (1 MiB) are bounded. Missing optional files
are normal; unreadable, malformed, oversized, or escaping project-rule sources
fail the turn explicitly instead of applying partial constraints. Existing
`me.md` comment syntax and explicit `$skill` expansion remain supported. Context
inspection reports the actual applied sources and application time, with file
and cwd changes identified as taking effect next turn.

## Execution language and checkpoints

New roots select `starlark` or `quickjs` through creation metadata or
`--rlm-engine`. The default is Starlark; `rlm.defaultEngine` changes future
negotiated creations. Legacy sessions and unnegotiated creations stay Starlark.
Descendants derive the root's persisted selection. Forks inherit it, retries
retain it, and a conflicting resume selector fails. Running sessions cannot
switch language.

Starlark globals persist in a live worker. After each cell the kernel saves a
structured checkpoint in `agent_checkpoints`. Legacy `agent_scratch` rows are
retained as a read fallback and migrate on the next successful save. A fresh worker reconstructs
supported data directly and compiles validated helper definitions before use.
Restoration never replays data assignments or host effects.

The supported data subset is `None`, booleans, arbitrary integers, finite
floats, strings, bytes, lists, tuples, and dictionaries. Types, dictionary order,
and shared nested list/dictionary references survive. Cycles, unsupported
runtime objects, and containers containing functions are skipped without losing
unrelated bindings. Checkpoints are bounded to 256 KiB per binding, 768 KiB
aggregate, and the complete encoded protocol frame, including escaped strings
and manifest metadata.

Top-level `def`s and assigned lambdas can survive when they have immutable
literal defaults, no captured closure state, and supported dependencies.
Helper-to-helper references and recursion are supported. Source belongs to the
actual function object, including definitions executed before a later ordinary
cell error. Helpers with mutable/nonliteral defaults or changed, missing, or
unsupported global dependencies are reported as skipped. Helpers see globals
as bound when defined; pass arguments or mutate shared containers rather than
rebinding a name a helper reads. Restoration must not silently change that
binding. Tool-calling helper bodies run only when subsequently invoked.

Changed omissions and persistence failures appear in the cell's scratch
report. A failed checkpoint does not undo a completed cell or justify repeating
its external effects; the previous durable checkpoint remains available and
the next cell retries saving. Failed loads stop the replacement worker and
release its reservation; the next acquisition retries. Corrupt checkpoints fail
explicitly instead of being overwritten with an empty environment. A cell lost
with its running worker is outside the last completed checkpoint.

Every restore produces a bounded runtime notice and a `scratch.restored` actor
event naming restored and omitted bindings. The daemon owns audit delivery;
there is no detached notification goroutine. Important durable information
belongs in `state`, `artifacts`, messages, files, or child transcripts. The
schema migration preserves legacy Starlark records without replaying host effects.

QuickJS runs a pinned bundled WASM build inside wazero in its own worker.
Its settled checkpoint captures the entire guest heap: lexical bindings,
closures, object identity, cycles, classes, and BigInts survive eviction and
daemon restart. Ordinary language errors preserve preceding mutations once
owned work settles. A failed `const` initializer and lexical redeclaration
follow JavaScript rules; cells are not wrapped in a new local scope.

Host functions accept an options object and return Promises, for example
`await files.read({path: "README.md"})`. Top-level await is supported. The
worker drains owned requests even after early rejection or a forgotten await;
an unresolved promise, unhandled rejection, cancellation, or job limit is
reported explicitly. Images never claim to preserve an active provider call,
host operation, or pending stack. Restoring an image performs no host effects.

Host payloads have a narrower contract than the guest heap: passive objects,
arrays, strings, booleans, null, finite Numbers (integers must be safe), and BigInts. Exact host
decimal/exponent/negative-zero tokens use frozen wrappers; explicit conversion
to Number can lose precision. Unsupported values, cycles, accessors, and unsafe
integer Numbers are rejected at the host boundary. The runtime disables Proxy
to keep validation passive. Result previews are separately tagged and bounded;
undefined has no value, while null is an explicit result.

Checkpoint envelopes bind root/agent ownership, engine build/ABI/profile,
sequence, settled boundary, fidelity, byte count, and SHA-256. Storage publishes
one latest image atomically, with 40 MiB/image, 256 MiB/root, and 1 GiB/store
ceilings. A failure preserves the previous image; a corrupt or incompatible
image fails visibly and remains retained for explicit recovery. There is no
automatic cross-build or cross-language migration. SHA-256 checks integrity;
these are trusted internal artifacts, never guest-controlled handles.

Result version 2 carries `execution_engine`, `language`, `has_value`, and
`metrics`. Starlark reports steps; QuickJS reports jobs and host/compute timing.
Public protocol major 6 deliberately requires updated clients. Current views
read legacy Starlark results and result-v2 across live updates, history, and
reconnect, and derive unfinished descendant cells' language from the root.

Model-facing results use one JSON object containing `value`, `output`, `steps`,
and any `scratch` or `restored` notices. Output and value previews are bounded
before serialization so the JSON stays valid. Oversized values use `value: null`,
`value_preview`, and `truncated: true`; the preview is not the full return value.
Failed cells retain this result after the tool error prefix, preserving printed
output, step counts, and checkpoint notices in client replay.

Cells are observable while they run. The worker publishes its print output
so far as `stream.tool.output` (throttled to 100 ms; the result carries a
bounded output preview), and every host call inside a cell emits `stream.cell.host`
with `module.operation`, a bounded argument summary (identifying keys such as
`path` and `command` truncated to 80 bytes; payload keys such as `content`,
`body`, and `code` shown only as their size), the duration, and any error.
Both are per-agent presentation events, so clients can render a live REPL
for any node without touching the model's context.

## Background shell jobs

`shell.run` blocks its cell and is capped at 120 seconds. `shell.start`
launches the command as a background job under the same permission prompt and
capability checks as `shell.run` and returns immediately with a job id;
`shell.poll`, `shell.tail`, `shell.wait` (capped at 25 s, not charged to the
cell clock), `shell.kill`, and `shell.list` operate on jobs the calling agent
started. A job keeps the last 1 MiB of output in memory; once it ends,
`shell.poll` returns the output inline or as a handle above the inline limit.
Jobs outlive cells and turns, are killed when their agent stops or is deleted
and when the root shuts down, and do not survive a daemon restart. At most 8
jobs run per agent at once.

## Permission rules

Permission requests can be approved or denied by any connected client. Clients
are trusted on the local machine or configured trusted network; pairing and
signing keys are not required. Agent capabilities and budgets remain enforced,
and internal MCP calls still require delegated authority.

A permission prompt names its rules: for shell operations (`bash`,
`workspace_process`, `shell_start`) every command on the line is collapsed to
its arity prefix (`go test ./...` -> `go test`, `ls -la` -> `ls`), so
`go build ./... && go test ./...` has two rules and is skipped only when both
are covered; an `ls` rule never approves `ls && rm -rf ~`. Lines with command
substitution, backticks, or a redirect other than a stderr merge or
`/dev/null` have no rule and always prompt. For `write` and `edit` the rule
is the canonical path. Approving with "always" installs the prompt's rules for
the session tree: each is stored in SQLite as a `permission_rules` row keyed
by root, operation, and rule, and deleted with the session. The global allowlist lives in the config
key `permissions.allow` as `operation:rule` entries (for example
`"bash:go test"`); the daemon reloads it on each new session, so hand edits
take effect on the next session.

When an admission matches a rule the daemon skips the prompt, stores the
admission with `require_permission=false`, and emits
`permission.auto_approved` with the operation, command, rule, and
`rule_source` (`tree` or `global`). Installing rules also resolves every
other pending prompt in the root that they now cover. Rules never
widen a capability: the operation is still validated against the agent's
grants. `/permissions` (or `/permissions list`) prints the tree rules and the
global allowlist; `/permissions forget <id>` deletes a tree rule.

Permission mode belongs to the root session and persists across client and
daemon restarts. `whip --yolo` saves Full Access for the initially selected
session: ordinary file grants allow host paths outside the project and permission
prompts are approved automatically. Explicit child path and operation ceilings
still apply. `--cautious` saves approval prompts and the original project file
boundary for that session;
new sessions default to prompting. Switching sessions or reconnecting restores
the selected session's saved mode. ACP also preserves the mode when loading a
session and reflects changes made by other clients. Capabilities and budgets
still apply; the mode cannot change while an agent is running. The terminal
mode label shows `full access` while automatic approval is active.
Changing the current directory never changes authority. Downgrading to Ask keeps
the cwd but denies subsequent outside file/process operations until navigation
returns to an allowed directory. Pending approvals are invalidated atomically;
already running background processes are not retroactively sandboxed or killed.
Project-instruction and skill discovery remains bounded by project context and
the agent's current file authority. A denied cwd contributes no project context;
global user instructions and configured user skills remain available. Explicitly
invoked project skills are reauthorized before reading even when their catalog
was cached before a downgrade. Shell execution has ordinary OS-user authority
in either mode, subject to its existing consent gate.

## MCP

MCP servers are daemon-owned integrations available from every authorized
node through `mcp.list_servers`, `mcp.list_tools`, `mcp.instructions`, and `mcp.call`. Their tools
are not appended to the provider’s tool catalog. Root and child therefore keep
the same stable interface even as MCP servers connect, fail, or reconnect.

The root session owns the only live manager; attachment, model reload, status,
root calls, and descendant calls use that synchronized owner. Tool invocations
use the existing operation ledger and an MCP grant separate from file/shell
authority. Native WHIP configuration confers trust; imported and attached
definitions require consent or a saved rule. Approval binds the exact server,
raw tool name, and definition, and cannot override revoked capabilities.

Omitted child capabilities inherit a snapshot of the parent's currently
advertised MCP definitions. Explicit `capabilities=["read"]` excludes MCP;
`"mcp"` and optional `mcp_tools=[{"server": "...", "tool": "..."}]` allow
bounded delegation. Issuer references and selectors live in the existing
capability scopes and survive restart. Newly discovered tools never expand a
retained child's grant. See [MCP tools and consent](tools.md#mcp) for examples.

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

The worker memory budget limits resident RAM. On Linux, the separate virtual
address-space ceiling includes the Go runtime's measured startup reservations,
the configured budget, and 4 GiB of growth allowance. This permits normal runtime
allocations even when startup reservations exceed 4 GiB; the parent still enforces
the configured RAM budget. The address-space ceiling remains finite.

These are execution bounds. Durable budgets separately account for token,
cost, elapsed, content, record, operation, child, schedule, and depth limits.
Root model cost, tokens, and cumulative request time default to unlimited;
children inherit unless explicitly constrained. Storage and live-capacity
limits retain their existing defaults.

Model turns, helper calls, batch members, compaction, final answers, titles,
and every wire retry share durable ancestor budgets. Admission estimates the
input and clamps the output allowance to fit. Actual usage is always recorded,
even above the estimate or limit; an overdrawn budget has zero remaining and
stops further work. Completed responses and requested-but-unexecuted tools
remain in the transcript when accounting stops a turn.

Each attempt retains compact metadata, its exact ancestor reservations, and
its settlement identity. These internal records do not consume the agent's
record or payload allowances; they follow the session's existing retention and
physical cleanup lifecycle. Record storage grows with the number of attempts.
There is no automatic pruning or accounting-history cap in this phase.

Missing or interrupted usage keeps the reservation estimate in `uncertain`,
separate from known `used` amounts. A late outcome corrects only that attempt's
contribution. Unpriced calls require unlimited monetary budgets; a finite cap
cannot be imposed on an existing budget with unresolved, unpriced history.
A newly introduced child cap applies to subsequent calls. Known free rates can
establish zero cost even when token usage remains unknown.

Elapsed budget means cumulative model request time: concurrent requests each
consume time, while idle time, ordinary tools, and retry backoff do not. Each
request gets a deadline bounded by its remaining ancestor allowance and the
provider timeout. The overall model-call deadline also covers retry backoff.

A dispatched attempt without complete usage uses its reserved allowance as an
explicit estimate; a pre-dispatch rejection releases the reservation without
a token or monetary charge. Settlement is durable and idempotent by attempt
ID. Persistence failures retain the result in the live owner and pause further
calls until that exact settlement succeeds. Restart settles unresolved calls
once as interrupted estimates before releasing residual reservations. A late
result from an interrupted live call corrects its estimate without releasing
another call's reservation. There is no provider-call replay or billing poller.


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
migrated automatically; this is an intentional clean break. The current
development schema is version 14 (`whip-recursive-runtime-v14`). Opening a
version 10 database performs a transactional, one-way upgrade that adds session
archive state and updates catalog revision tracking in schema 11. The subsequent
schema 11-to-12 transaction adds nullable, bounded last-turn outcomes per agent,
backfills retained lifecycle evidence, and reconciles legacy stopped turns.
Schema 12-to-13 adds the session's durable permission mode, defaulting to Ask
for existing sessions. Historical mode events do not resurrect past live choices.
The migration emits the new mode so clients reconnecting through event replay
also learn the default.
Schema 13-to-14 preserves saved modes and tags valid bootstrap root file grants
as session scope, retaining their original canonical path as the Ask boundary.
Legacy child grants keep explicit paths because their creation records do not
prove inheritance. Newly created default children record their issuer and inherit
its live file scope. Revoked/expired grants are never revived by the migration.
Runtime identity, agent IDs, history, configuration, command outcomes, and
existing sessions are preserved; existing sessions start unarchived. Backfill
leaves outcomes unknown when evidence is missing or pruned, externally stored
events exceed 8 MiB, or edited legacy root history has no safe reset boundary.
Child history is unaffected by that root-history restriction. Each failed
upgrade step rolls back and can be retried.
Older binaries cannot open the upgraded database. Other incompatible schemas
are rejected without modification; WHIP never automatically deletes them.

## Recovery

On restart, retained non-root agents are reconstructed from metadata,
capabilities, provider settings, report mode, and transcripts. Unclaimed
queued input and its correlated queued root command survive. Readiness is
re-derived from durable rows, so a restored node with ready mail or queued
input wakes without an in-memory signal.

Claimed input, including human steers already claimed at a loop boundary,
and running turns are interrupted. Retained children return to idle. Restart
does not replay uncertain external effects or actor controls merely because
a command still says queued. Explicit terminal root stop/failure also
interrupts queued input. Ordinary child execution failures can retry claimed
input up to three times; invalid input and interrupted attempts do not retry.

Mailbox delivery is at least once until a successful turn commits its receipt.
An observed pending message can therefore be shown again after failure or
restart. Deferral and pending-message replacement increment its revision, so
an older receipt cannot consume its newer content or future wake. These rules
do not provide exactly-once tool effects.

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
