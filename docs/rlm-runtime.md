# Recursive runtime

Every root and child uses the native Go runtime and sees one model-facing
`execute` tool. Starlark or JavaScript cells call captured host modules; the
runtime admits and authorizes each effect separately. There is no direct-tool
agent mode or client-owned execution loop.

The [domain contract](backend-domain.md) defines exact durable transitions,
limits and service boundaries. [The agent loop](agent-loop.md) explains provider,
cell and checkpoint sequencing. This guide describes the current implementation;
remaining redesign and platform acceptance is tracked in the
[development record](backend-redesign-development.md).

## Sessions and recursion

Roots and children share [`session.Session`](../internal/session/session.go),
configuration, inputs, turns, transcript, accounting and lifecycle operations.
A tree owns its root, metadata and immutable execution language. A root has no
parent. An agent definition is an immutable ID/revision, not a live worker.

Creating a session does not allocate a kernel or start a model request. The
scheduler derives readiness from durable queued input and due mail. Capacity
limits bound running work; children can wait for capacity instead of being
rejected merely because every worker is occupied. Every ancestor's applicable
resource, model-budget and authority limits still apply.

Spawn commits the child, captured configuration, original input, scoped content
references, delegated grants and receipt together. An exact retry returns the
same admission; changed defaults or grants cannot silently change its payload.
`grant_ids: null` inherits currently valid standing grants; `[]` delegates none.
A child's standing grant names its direct parent's issuer with the same
capability/resource. Every issuer hop is checked at dispatch. One-use approvals
cannot be delegated, and a child without authority cannot widen it through a
permission prompt.

A completed child turn leaves its response in that child's transcript. Parents
can inspect the exact admitted input's outcome or consume completion mail.
`agents.wait_after_cell(input_ids=[...])` registers descendant input targets and
returns immediately. The cell must finish: only after result/checkpoint commit
and kernel release does the runtime wait before its next model step. This permits
recursive progress with one turn worker and one kernel slot. It replaces the old
same-cell blocking join. A failed/cancelled/interrupted child resolves as data;
it never automatically retries the child or fails the parent.

See [child admission and waits](backend-domain.md#child-admission-delegation-and-waits)
for exact targets, limits and deletion behavior.

## Communication and explicit state

Mail is separate from input and transcript. Session senders may address self,
parent, direct children or siblings in their tree. Stable mail identities preserve
exact original payloads across retries; clients cannot forge completion/state
notification provenance. Optional evidence becomes a recipient-owned reference to
the same immutable body, so sender deletion does not remove the recipient's access.

`queued` mail waits for an idle session; `steer` may also be presented at a model
boundary after preceding cells settle; `next_turn` waits for another reason to
start work. The model sees a bounded digest and explicitly reads full evidence.
Human inspection neither starts execution nor acknowledges agent delivery.

Only a successful turn marks its presented, still-current revisions delivered.
Explicit agent completion or deferral requires observed revisions. Failed turns
leave mail pending and block automatic mail-only execution until explicit input
succeeds. New mail and restart cannot bypass that barrier.

A child's captured report mode chooses a small notice, a larger inline preview,
or explicit reporting on success. Failures still report. Bounded parent-owned
completion slots retain exact terminal evidence if mailbox/content capacity blocks
publication. Publishing the evidence, mail revision and slot removal is atomic;
publication failure does not run the child again. See
[mail](backend-domain.md#mail-and-presentation) and
[completion reports](backend-domain.md#child-completion-reports).

Private/shared state, immutable values and subscriptions have their own scoped
identities and revision checks. They do not act as another transcript or execution
queue. See [state](backend-domain.md#explicit-state-and-immutable-values) and
[state subscriptions](backend-domain.md#state-subscriptions).

## History, context and content

Raw history and the model's selected context are different views. Compaction
records exact source coverage without deleting raw messages. Tool exchanges and
the opening input of a partially summarized turn retain their required boundaries.
Context inspection/search/read is bounded and owner-scoped; a reference or digest
alone never grants access to another session's content.

Committed history pagination pins revisions and upper boundaries. Rewind changes
that revision and invalidates the REPL boundary. Old cell evidence remains readable,
but no saved interpreter can masquerade as the newly selected history. Fork
creates a new root with its selected prefix and authorized content references;
it does not copy model charges, running workers, queues or native handles.

The native content payload ceiling is 4 MiB. Large evidence uses explicit scoped
references and bounded reads with exact byte offsets, truncation and continuation.
Missing content, invalid cursors and exhausted capacity are errors or explicit
unavailability, never fabricated empty results. See
[content ownership](backend-domain.md#content-boundary) and
[history/context](backend-domain.md#raw-history-and-selected-model-context).

## Instructions and definitions

A turn captures its configuration and applied instructions once. Project rules,
standing instructions and skill discovery use explicit host declarations and
session authority. Their text can constrain behavior but cannot grant filesystem,
MCP, browser or custom-tool authority. Later file/config edits take effect in a
subsequent turn. Context audit reports the actual applied manifest, including
source, scope, path, byte count and digest.

Project rules follow the applicable workspace-root-to-cwd chain, broad to narrow;
CLAUDE.md precedes AGENTS.md at one directory. Sources, total prompt size and skill
metadata reads are bounded. Invalid or inaccessible required sources fail
explicitly rather than silently applying partial constraints. See
[instruction capture](backend-domain.md#instruction-capture).

Captured module/tool/hook bindings define syntax separately from authority.
Configuration edits can narrow or restore names within the original binding
ceiling; they cannot replace exact executor ownership or remove required hooks.
Saved guest aliases are rechecked against the owning turn's captured policy.
Children cannot widen their parent's bindings. See
[captured definitions](backend-domain.md#captured-definition-bindings).

## Languages and checkpoints

A new root chooses Starlark or QuickJS; the host's `engine` default is Starlark.
Descendants and forks retain that tree language. A model change does not replace
the REPL. [`internal/engine/process`](../internal/engine/process/worker.go) owns
bounded worker processes; QuickJS runs its pinned WASM image through wazero.
Starlark uses keyword arguments; JavaScript host functions accept options objects
and return promises, with top-level await supported.

The runner admits at most 32 logical model calls and 64 dispatched cells per turn.
Each cell names its committed assistant message and provider call. One serialized
kernel belongs to a session, and process capacity stays pinned through settlement.
Host calls have separate admission, consent, dispatch and result records.

After evaluation, immutable checkpoint bytes are staged first. Cell outcome,
result message and checkpoint reference then commit together. Language errors can
retain changed globals and a valid checkpoint. Transport loss or cancellation is
an uncertain boundary. A completed cell without a usable checkpoint stays readable,
but further code fails instead of falling back to an older image. Text-only turns
and history inspection remain possible; a new session starts a fresh REPL.

Restore checks ownership, digest, size, engine build, ABI, profile and fidelity.
Starlark reports skipped unsupported globals in its partial checkpoint. QuickJS
preserves its settled heap within the engine contract. Neither engine restores
by replaying old cells or host effects. No scratch-table fallback or cross-build
migration exists. Detailed behavior and replacement tests are linked from
[checkpoint ownership](backend-domain.md#code-execution-and-checkpoint-boundary).

Live stdout, reasoning and progress are bounded provisional observations. They
cannot create durable transcript entries or claim a successful operation. Exact
committed messages/cells/operations reconcile presentation; disappearing progress
is not a cancellation request. See
[observation](backend-domain.md#provisional-output-and-observation).

## Effects, permissions and native resources

The operation ledger owns authorization and dispatch. Ask/Full Access, exact
standing grants and one-use decisions remain distinct. Permission changes and
ancestor revocation recheck ready operations; they cannot undo effects already
dispatched. Changing cwd does not itself widen authority. Shell processes still
have OS-user authority: consent is not a filesystem sandbox. See
[permissions](backend-domain.md#host-operations-and-permission-decisions) and
[saved policy](backend-domain.md#saved-permission-policy).

Foreground shell commands have a 120-second ceiling; background jobs belong to
the session and can survive turn completion/cancellation. Session stop/deletion
and host shutdown kill and join their owned process groups. Handles never restore
on restart. Output is a bounded tail with explicit counts and truncation.
Interactive input names an exact owner, operation and sequence; late bytes cannot
spill into a later command. Human terminal tabs are a separate epoch-bound PTY
service with independent cursor readers and no replay of uncertain writes. Network
terminal access requires explicit host opt-in. See
[shell execution](backend-domain.md#session-shell-execution-and-human-input) and
[human terminals](backend-domain.md#human-terminal-tabs-and-persistent-browser-executors).

MCP connections are runtime-owned. Metadata inspection does not connect servers;
configuration/imports use explicit revisioned controls. Catalog and call access
honor captured server selection and delegated authority. Native trusted declarations
and imported/client-attached variants remain distinguishable at admission and
dispatch. Large text/media results receive owner-scoped content references.
See [MCP ownership](backend-domain.md#mcp-ownership-imports-and-delegated-discovery)
and the current [typed result handling](../internal/runtime/mcp_test.go).

Desktop browser and computer bindings use exact selected native resources and
bounded owners. An attachment ID alone is not a grant. Restart/reconnect cannot
restore a native selection or replay an uncertain action. Browser execution,
MCP imports and provider credentials have separate authority; a connected client
must not infer one from another. See the
[browser/computer guide](browser-computer-use.md) and
[native SDK binding contract](../packages/sdk/README.md).

## Custom tools, hooks and output

Custom executors bind immutable definition revisions on persistent initialized
connections. The runtime bounds peers, leases, calls, waiters and queued bytes.
Input validation precedes consent; exact executor selection occurs before durable
dispatch. Known handler failures and invalid output schemas settle failure;
disconnect after dispatch leaves uncertainty. Restart, observation and rebinding
never resend an invocation.

Required hooks can deny or rewrite an effect, with ordinary validation and
permission applied to the resulting request. Optional hook failure records a
bounded skipped notice; cancellation never becomes optional success. Hooks cannot
grant authority. Turn-start context and live hook/progress previews are temporary;
canonical arguments and settled outcomes remain the durable evidence.

A final output schema validates one complete text-only JSON value. One mismatch
permits one corrective response through the same accounting path. A second mismatch
or corrective tool call fails explicitly; corrective calls never execute.
`turns.output` derives the successful result from its exact assistant message and
captured schema. JSON null has a result record; absence of an output contract does
not. See [output contracts](backend-domain.md#final-output-contracts).

## Accounting and recovery

Every ordinary, helper, batch, compaction, title and corrective provider attempt
uses the same durable accounting boundary. Known zero, unknown usage, uncertain
reservation estimates and exact monetary values remain distinct. Finite ancestor
limits gate admission; actual usage is recorded even when it exceeds an estimate.
Physical resource capacity and cumulative logical-write limits are separate.
See [accounting](backend-domain.md#model-budgets-and-retained-accounting),
[capacity](backend-domain.md#reusable-resource-capacity) and
[logical writes](backend-domain.md#cumulative-logical-write-allowances).

A local observation timeout does not prove rejection. Exact request identity and
payload evidence resolve uncertain admission. Restart retains unclaimed queued
inputs, interrupts claimed work, settles unresolved attempts and reconstructs
readiness from durable rows. It never retries a whole failed/uncertain turn or
replays a dispatched host effect. The narrowly recognized structured context-limit
recovery belongs to the active model loop; it is not transport-error replay.

The fresh host lives under `$WHIPCODE_HOME/runtime-v4` (default
`~/.whipcode/runtime-v4`), with `host.json`, its native store and owned diagnostics.
Long directories use a deterministic private short socket path. Old directories
remain untouched and are never imported or silently deleted. Use the current
[setup guide](setup.md), [web lifecycle](web-app.md) and
[protocol v4 reference](protocol-v2.md) for client/startup controls.

## Verification

The required commands and exact passing/failed checkpoints are in the
[development record](backend-redesign-development.md). Current native runtime,
engine, model, store and client tests exercise real disposable processes and
loopback fixtures. [Evaluation documentation](../evals/README.md) separates
repeatable no-external-model qualification from opt-in paid/live-provider work.
A passing local fixture does not imply signed-release, remote-SSH, physical-device
or live-provider acceptance; those outstanding checks remain named explicitly.
