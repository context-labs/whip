# WHIP SDK v4

This SDK talks directly to the new Go runtime. The retained product applications
currently use `@whip/legacy-sdk`; their later cutover is tracked in
[the redesign plan](../../docs/backend-redesign-plan.md).

```sh
npm ci
npm run build -w @whip/sdk
go run ./cmd/whip-runtime -directory /tmp/whip-example/state -scripted
# In another terminal, using the socket printed above:
node packages/sdk/examples/session.mjs /tmp/whip-example/state/runtime.sock
```

The directory must be private to its owner (mode 0700); the runtime creates it
when absent. It never discovers or attaches to an installed daemon. The scripted
fixture provider requires the model selection `scripted/scripted`.

`Client.connect(transport, {clientID, expectedRuntimeID?, signal?})` pins the
runtime identity. `unixSocket(path)` is available from `@whip/sdk/node`; the main
entry point imports no Node APIs. Each call has a bounded connection with a fresh
handshake, and `client.call(method, params)` uses generated v4 types and validators.

`submit(sessionID, parts, requestID)` returns a durable admission. Persist the
runtime ID, client ID, request ID, session ID and exact payload before submitting
when the application needs crash recovery. Reuse that identity and payload on an
uncertain retry. A `DeliveryError` cannot establish whether work was accepted;
`recover(requestID)` reads its receipt. Reusing an identity with different input
returns `RemoteError` with kind `CONFLICT`.

`wait(requestID, {signal})` polls durable state until the input is cancelled,
deleted, or its turn finishes. It returns failed/interrupted outcomes as data.
Aborting the wait only stops observation; `inputs.cancel` or `turns.cancel`
explicitly cancels execution. History uses bounded pages and exact decimal-string
cursors. The client keeps no transcript cache or second execution state machine.

`sessions.history` returns `items` and an atomic `snapshot` containing
`revision`, `through_sequence` and `message_count`. Keep that revision with the
sequence cursor and pass `expected_revision` on later history/metadata/search
pages. A rewind returns `CONFLICT` for earlier revisions; appends keep the revision
and advance sequences. `context.snapshot` returns the same boundary fields.
Message `group_id` and `opening_input` describe whole conversation exchanges;
nullable execution IDs and `source` distinguish copied history from local work.

To rewind, stop the session explicitly, wait for active cancellation to settle,
and cancel any unclaimed inputs. Obtain a fresh snapshot and select zero or the
last sequence of a whole terminal group. Keep the edit ID and exact request before
sending:

```ts
const snapshot = await client.call('context.snapshot', { session_id: sessionID });
const edit = await client.rewind({
  session_id: sessionID,
  expected_revision: snapshot.revision,
  observed_through: snapshot.through_sequence,
  keep_through: selectedGroupEnd,
}, editID);
```

An uncertain delivery retries this same ID and payload. The response is the
original immutable edit, even after later execution; it is not a current history
snapshot. Changed payload reuse conflicts. Every new edit resets the REPL,
including keeping all messages. An exact retry never resets it again. Rewind
retains exact old execution evidence, independent mail/goals/state/spend and
external files. It does not restore a workspace. Active history hides the suffix,
and later messages never reuse its sequences.

Fork a terminal whole-group prefix using a stable fork ID and exact source
snapshot. The source may have later active work outside the selected prefix;
history/configuration revisions and the full observed tail must still match.

```ts
const source = await client.call('sessions.get', { session_id: sessionID });
const snapshot = await client.call('context.snapshot', { session_id: sessionID });
const fork = await client.fork({
  session_id: sessionID,
  expected_history_revision: snapshot.revision,
  expected_config_revision: source.config_revision,
  observed_through: snapshot.through_sequence,
  keep_through: selectedGroupEnd,
  title: null,
}, forkID);
```

An uncertain retry must reuse the ID and exact request. `fork.fork` is the immutable
receipt; `fork.tree` and `fork.root` are current projections. Deleting the destination
leaves `{fork: receipt, tree: null, root: null, deleted: true}` permanently. Source
deletion does not invalidate an already accepted fork. There is no automatic retry
or generic admission receipt for this mutation.

Forks preserve bounded history, compatible selected compaction/pins, effective
configuration/definition/engine and owner-scoped content handles without inventing
local execution or spending. They start with empty REPLs, fresh limits and no
copied authority, children, schedules, goals, mail or state. Working-directory reuse
does not create or restore a Git worktree. Ordinary content reads remain scoped to
the new root, including after source deletion.

Workspace actions are separate from history edits. `captureWorkspace`,
`restoreWorkspace` and `releaseWorkspace` accept `{session_id, snapshot_id}` and
an explicit `actionID`. Save both IDs and the exact request before sending;
`getWorkspaceAction(sessionID, actionID)` observes a lost acknowledgement, and an
exact mutation retry returns its durable outcome without replaying Git. Read
metadata with `getWorkspaceSnapshot` or bounded `listWorkspaceSnapshots`.
A response contains immutable action evidence plus the current snapshot projection.
`claimed` means the accepted action has not settled; `uncertain` means effects may
be partial and the same action will never execute again automatically.

The owner must have no active turn, queued input or ready mail when claimed.
Capture privately pins a Git object from the repository's tracked state; restore
only overlays tracked paths under the captured session working directory.
Untracked and later files may remain, staging is not preserved, and other
sessions/tools/editors are not frozen. Paths and Git object IDs stay private;
worktree/directory identity changes fail closed. Neither action rewinds history
or changes the REPL. Pins survive restore/restart and must be explicitly released
before deleting their owner or containing subtree. Released action receipts and
snapshot metadata remain readable after deletion. A new explicit release can
clean up after an uncertain release; the old action itself is never rerun.

`turns.attempts` reads bounded provider accounting with exact decimal counters.
Retries have separate attempt IDs, a shared logical-call ID, and a link to the
completed response. Unknown usage/cost is `null`, independently of known zero.

Stateless guest `models.call` and `models.batch` use that same ledger. Helper
attempts have purpose `model_helper`, an `operation_id`, and a zero-based
`batch_index`; retries retain both links and have distinct attempt IDs. Their
`message_id` is null. Read the ordered aggregate through the ordinary operation
result; no helper transcript or second accounting API exists.

A single call returns one `{attempt_id, text, failure, content_ref, truncated,
bytes}` item; a batch returns an array in prompt order. IDs, failures and content
references are nullable, and byte counts are exact decimal strings. Large output
has a bounded preview and a reference readable through the owner's `content.read`
or authorized guest `artifacts.read`. A reference never grants access to another
session. Publication failure reports unavailable output while preserving billing.
On a crash before the aggregate commits, some attempts can be settled while the
operation becomes uncertain and their response text is unavailable. Do not replay
the helper call to fill that gap.

Goal controls use the same durable input and turn lifecycle. Keep the goal ID
and exact creation payload before sending:

```ts
const admitted = await client.createGoal({
  session_id: sessionID,
  expected_current: null, // Or the current goal's { id, revision } when replacing.
  spec: { text: 'Complete the migration', max_continuations: '10' },
  start: true,
}, goalID);
if (admitted.initial) {
  const status = await client.call('receipts.get', admitted.initial.receipt.identity);
}
const { goal } = await client.currentGoal(sessionID);
```

Initial work belongs to the reserved `goal` receipt namespace. Observe its full
returned identity with `receipts.get`; `client.wait(requestID)` and `recover`
use your own client identity. Explicit `resumeGoal(sessionID, {id, revision},
requestID)` returns a caller-owned admission, so ordinary `wait`/`recover` apply.
Resume never resets consumed continuations. Omitted `max_continuations` means
100 additional runs; `'0'` allows the initial run without automatic continuation.

`getGoal` reads a stable ID, and `currentGoal` reads the latest created record,
including terminal states. Retrying an old creation does not select it again:
check `current`, nullable `goal`, and `deleted_at` in the result. `cancelGoal`
uses the stable ID and requests cancellation only of its exact active goal-owned
turn. An independently submitted human turn can continue after goal cancellation.
`goals_enabled` is captured configuration, not an authority grant. Guest
`goals.complete` requires ordinary scoped permission and accepts only a completion
intent; the unchanged goal completes when that turn successfully settles valid
output. Clients must not infer goal completion from the operation result alone.

`formulateGoal({session_id, request: {goal_id, expected_current, start,
max_continuations?, tail_messages?}}, requestID)` proposes a goal from a captured
raw-history tail through ordinary durable admission. Omitted or zero
`tail_messages` uses eight; explicit windows accept 2–100 messages. Use ordinary
`wait`/`recover` for the caller-owned maintenance receipt. No conversation message
is appended. Read its recorded model attempts, then
`getGoalFormulation(sessionID, attemptID)` for a valid candidate and immutable
`accepted`/`rejection` evidence. A failed activation retains provider billing;
a crash after activation can leave an accepted goal and interrupted helper turn.
The nullable goal `origin_formulation_attempt_id` links that decision. Retrying
admission does not run the provider again, and later cancellation or child deletion
does not rewrite candidate acceptance. Product UI adoption remains pending.

Use `content.put` with `{session_id, reference_id, media_type, data_base64}` to
upload up to 4 MiB. Generate and retain a unique reference ID before sending;
retrying it with the same owner, bytes and media type returns the same reference.
Submit `{type: 'content', reference_id}` parts alongside text. `content.read`
takes the owning session and reference IDs and returns verified `data_base64`.
References are session-scoped: the same handle can identify different bytes in
different sessions. Always keep the owner with a reference; a digest is not an
access token. The runtime
hydrates authorized bytes for the provider while history keeps the reference.
Each session is limited to 1,024 references and 64 MiB of referenced bytes.

For configured HTTP providers, omit `-scripted` and use the host configuration
described in [the development guide](../../docs/backend-redesign-development.md#openai-compatible-dispatch-increment).

Host ChatGPT account controls are direct SDK calls:
`beginOpenAILogin`, `getOpenAILogin(flowID)`, `listOpenAILogins`,
`cancelOpenAILogin(flowID)`, `openAIAccountStatus`, `setupOpenAIAccount` and
`logoutOpenAIAccount`. Begin accepts host-owned work; disconnection does not
cancel it. Recover a lost acknowledgement with list/get or the active flow,
not ordinary session receipts. Earlier-process flow IDs report interrupted.
The SDK neither opens a browser nor polls automatically. Clients explicitly
observe the bounded flow and cancel only on user intent.

Status separates saved authentication from configured routing. It does not
refresh tokens or verify network/model access; an expired saved credential is
still local `stored` evidence. Preserve the exact nullable expiry string. Login
saves credentials before route setup; `setup_required` can retry setup without
another device approval. Explicit setup can also confirm pending local credential
publication without token refresh. Storage failure is `unavailable`, distinct
from provider sign-in-required; failed logout remains unavailable until explicit
logout retry. Status reads never rewrite credentials. Setup/logout leave model
defaults and custom routes unchanged. Errors `ACCOUNT_CREDENTIALS`, `ACCOUNT_SETUP`, `ACCOUNT_CONFIGURATION`
and `ACCOUNT_LOGOUT` distinguish the required recovery. Public values contain
no access/refresh/device secret; terminal flows clear the approval URL/code.
Catalogs and product-client adoption remain pending.

Inference.net has parallel host methods: `beginInferenceLogin`,
`getInferenceLogin`, `listInferenceLogins`, `cancelInferenceLogin`,
`selectInferenceTeam(flowID, teamID)`, `selectInferenceProject(flowID, projectID)`,
`createInferenceProject(flowID, name)`, `retryInferenceLogin`, `rotateInferenceKey`,
`inferenceAccountStatus`, `setupInferenceAccount`, `logoutInferenceAccount`,
`listInferenceCleanup` and `retryInferenceCleanup`. Choices use the IDs exposed by
that flow. Only singleton choices advance automatically; project creation and key
rotation always need an explicit caller action. Recover lost begin/creation/rotation
acknowledgements by reading flows, never by automatically repeating the mutation.
Uncertain remote creation requires account inspection before another creation.

Status separates `management_state`, `inference_state` and `route_state`. Expired
management authorization does not invalidate a stored machine key. Local status
is not a network/readiness test. Setup installs only the canonical managed gateway
and leaves model defaults unchanged. Known persistence/setup failures can retry
their saved step without minting another key. A newly approved account cannot
inherit another account's key; a successful rotation saves the new key before
attempting to archive the old key.

Logout revokes local authority first and returns `local_failure`,
`cleanup_failure` and retained cleanup outcomes separately. A successful local
logout is not proof of remote key archival or session sign-out. Explicit cleanup
retries use the old saved authorization, never a newly signed-in account. Flows
and cleanup are bounded to 64 entries each, retained for up to 15 minutes in the
current process. Restart/expiry loses that evidence; an empty cleanup list does
not prove remote cleanup succeeded. All projections exclude private tokens/keys,
and terminal flows clear the approval code and URL.

Model selection accepts nullable `temperature` (0–2) and `top_p` (0–1), alongside
`provider`, `name` and `effort`. Both values must be finite. Null or omission uses
the provider default; explicit zero is preserved. A `sessions.configure` model
patch replaces the entire selection, so omitted sampling values clear previous
choices. Active turns and their retries retain their captured settings; later
turns and newly inherited children use the updated selection. Compaction inherits
these settings when its model is null, or uses its own complete model selection.
Chat routes support both preferences. API Responses and subscription routes
reject explicit sampling before credential lookup or HTTP dispatch.

See [the runnable example](examples/session.mjs), [Go client](../../internal/client/client.go),
and [real process acceptance](../../scripts/redesign/v4-fixture.test.mjs). Product-client adoption remains in progress.


The v4 transcript now includes assistant `tool_call` parts with a stable call ID,
name and JSON arguments, followed by `tool_result` parts in tool messages. A
result includes its call ID, output string and error flag. Prompt inputs accept only
text/content parts; compact inputs have empty parts. Completed calls and results survive interrupted turns; an
unfinished provider request is recorded as uncertain. The runtime executes both
Starlark and QuickJS through the same durable code loop and restores committed
REPL checkpoints across restart. Model changes apply to the next turn and retain
that session's REPL state.


`permissions.list` returns durable decisions for a session. For each pending
operation, inspect `operations.get` for its exact capability, resource and
arguments, then call `permissions.resolve` with `{operation_id, approved}`.
Approval authorizes only that invocation. Explicit standing authority uses
`grants.create` with a caller-generated ID, session ID, capability and resource;
`grants.revoke` prevents future dispatches under it. A file grant's resource is
the session's working directory. The local socket is a trusted-client boundary.

Use `turns.operations` for effects and `turns.cells` for interpreter boundaries.
A completed write may coexist with an uncertain cell and no usable checkpoint.
Restart cancels pending permissions and never repeats completed/uncertain effects.
Do not interpret an interrupted turn or a failed connection as proof that a write
did not happen. Permission approval after cancellation returns `CONFLICT`.


`client.observe(sessionID, {after?: '0', expectedRevision?, signal?})` yields bounded committed pages
and a disposable preview. The underlying `sessions.observe` result contains
`messages`, nullable `preview`, and the current process `epoch`. Each preview has
an attempt ID, eventual `message_id`, revision, text, reasoning, incomplete call
fragments, and a truncation flag. Text, reasoning and calls share a 128 KiB bound.
Reasoning is provisional display only; it is absent from durable history and
later provider context. Never execute preview arguments. Upsert committed messages
by ID; a matching committed ID replaces the preview. Clear provisional display
when the preview is null or the epoch changes. Aborting this iterator stops
observation only. It retains a cursor and revision, not a transcript cache.
Resuming with a nonzero cursor requires `expectedRevision`. On a revision
conflict the iterator restarts at zero and emits the replacement snapshot,
including empty history. Replace all displayed history and previews when its
`snapshot.revision` changes; upserting alone cannot remove retired rows.


`client.spawn({parent_id, parts, overrides: {}, grant_ids: null}, requestID)`
atomically creates a child and accepts its initial input. It returns
`{session, admission}`; use `client.wait(requestID)` and `client.recover(requestID)`
for the same recovery behavior as submissions. Reusing the exact spawn identity
and payload returns the same child; changed parameters conflict. After deletion,
the receipt remains and `session` is null. Content parts must reference content
owned by the parent; admission creates new scoped child references to those bytes.

`grant_ids: null` inherits live standing parent grants; an explicit list selects
a subset and `[]` grants none. `grants.create` for a child additionally requires
`issuer_id` identifying a standing direct-parent grant with the exact same scope.
Ancestor revocation invalidates descendant dispatch. Child permission decisions
cannot widen delegated authority.

In a REPL, `agents.spawn` accepts a prompt and returns `session_id`/`input_id`.
`agents.wait_after_cell` accepts descendant input IDs and immediately returns a
registration. Finish the cell to begin the wait; the runtime then releases worker
and kernel capacity until those inputs finish. Same-cell blocking `agents.wait`
is unavailable in the new runtime. These operations use tree-ID grant resources.
`agents.submit` queues another input to a direct child. Use `agents.inspect` with
that exact input ID for outcome and paged text; `next_offset`, `total_bytes` and
`offset` are decimal strings. Verify `message_id` when paging unfinished output.
`agents.list` exposes bounded relative metadata. `agents.stop` retains queued
work while stopping a descendant subtree; `agents.delete` requires it to be idle.
Each control requires its own capability grant.


Reusable capacity lives in session resource scopes, separately from model budgets
and tree metadata. `trees.create` and `client.spawn` accept an optional `resources`
array, for example `[{kind: 'descendants', limit: '10'}]`. Fresh host configuration
version 4 supplies root defaults; existing trees retain their persisted limits.
Root limits must be finite. Omitted child limits and `limit: null` inherit the
ancestor bounds. Duplicate kinds are invalid.

```ts
const resources = await client.call('resources.list', { session_id: sessionID });
const current = resources.items.find(item => item.session_id === sessionID && item.kind === 'queued_inputs');
await client.call('resources.set', {
  session_id: sessionID,
  expected_revision: current.revision,
  resource: { kind: 'queued_inputs', limit: '20' },
});
```

The listing includes all applicable scopes, nearest first. Each scope exposes
`session_id`, `kind`, `revision`, nullable `limit`, and `used`, with exact decimal
strings for numbers. Revision `'0'` means no local record; clearing a child cap
still advances its revision. Kinds are `depth`, `descendants`, `queued_inputs`,
`active_operations`, `subscriptions`, and `runnable_descendants`. Usage is derived from live owning rows
across each subtree. Claiming or cancelling queued input, settling operations,
unsubscribing, and deleting children release the corresponding capacity.
Runnable capacity counts execution permissions of proper descendants, excluding
the owner. Waiting parents release permission at a settled cell boundary and
reacquire it before resuming. A zero cap keeps children queued while allowing the
owner itself to run; raising it wakes scheduling. Host worker slots remain a
separate physical limit. A running turn can be waiting without consuming either.
Depth counts edges and descendants excludes the owner. Stale updates conflict;
a finite limit below current usage is rejected. `LIMIT` admission failures create
no work, so a caller can explicitly retry after capacity becomes available.

`budgets.list({session_id})` returns local model-call, token, nano-USD, elapsed
millisecond and cumulative write scopes with exact decimal-string counters.
`limit: null` removes a local cap; ancestors still apply. `budgets.set` takes
`{session_id, expected_revision, budget: {kind, limit}}`; revision `'0'` creates an
initial cap. Spawning may include a `budgets` array of narrower child caps.
The additional kinds `logical_writes` and `logical_write_bytes` count committed
logical actions and submitted payload bytes. Root defaults are 100,000 and 1 GiB,
and these root limits must remain finite. Child spawn/submit, explicit mail,
content registration, state writes and subscription creation consume allowance.
State append charges the submitted suffix. Retries do not charge twice; deletion
does not refund ancestors. Ordinary human input, automatic notifications and
recording completed execution are exempt. A rejected write changes no SQL state;
these allowances are distinct from current disk usage and reusable capacity.

Read `used`, `reserved`, `uncertain` and `incomplete` separately. Unknown accounting
is not zero. Finite limits cannot be set below allocated exposure, and requests
without enough bounded allowance fail before dispatch. Deleting a child retains
its accounting against ancestors. Original turn/message IDs in accounting may
therefore refer to deleted history. A root budget denial fails that turn without
stopping unrelated sessions.

Mail has its own stable identity and does not create an input receipt:

```ts
const params = {
  sender_id: childID,
  recipient_id: parentID,
  delivery: 'next_turn' as const,
  subject: 'Investigation complete',
  body: 'The result is ready for the next turn.',
};
const mailID = crypto.randomUUID(); // Retain this ID with params before sending.
const sent = await client.sendMail(params, mailID);
const inbox = await client.call('mail.list', {
  session_id: parentID, state: 'pending', limit: 100,
});
const inspected = await client.call('mail.read', {
  session_id: parentID, mail_id: sent.mail_id,
});
```

Keep the mail ID and exact payload for retries after an uncertain send; IDs are
unique across the runtime. Retrying after recipient deletion returns a tombstone.
Listings omit bodies. Reading a body is read-only and never acknowledges delivery.
Only successful turn presentation or explicit authorized agent handling changes
delivery state. `queued` and `steer` mail can start idle execution; `next_turn`
waits for another trigger. Transcript messages identify mail provenance with
`mail.id`, exact decimal-string `mail.revision`, and `mail.presentation`; their
`input_id` is null. Other messages have `mail: null`.

An optional `evidence_ref` attaches content already owned by the sender. The body
may be empty when evidence is present. Admission creates a recipient-owned
reference to the same immutable bytes, returned in `sent.mail.evidence_ref` and
mail list/read metadata. Read it with `content.read` using the recipient session
ID; a sender's reference or a content digest does not authorize recipient access.
Guest agents use scoped `artifacts.read` for bounded pages. Mail presentation
contains only the reference, so evidence does not automatically fill model context.

Retry with the original sender reference and payload. Successful retries retain
the same recipient reference even after sender deletion or runtime restart.
Recipient content limits can reject the entire send; no partial mail or shared
reference remains. Sharing references does not upload or charge the body again.

Explicit state uses `scope: 'session'` for private values or `scope: 'tree'` for
shared values. Every write supplies an expected revision and stable version ID:

```ts
const versionID = crypto.randomUUID(); // Retain with this exact payload.
const params = {
  session_id: sessionID,
  scope: 'session' as const,
  key: 'results',
  expected_revision: '0', // Create. Use the current revision to replace.
  data_base64: Buffer.from('[9007199254740993]', 'utf8').toString('base64'),
};
const version = await client.writeState(params, versionID);
const chunk = await client.call('state.read', {
  session_id: sessionID, version_id: version.id, offset: '0', length: 65536,
});
const exactJSON = Buffer.from(chunk.data_base64, 'base64').toString('utf8');
```

The Node encoding above is an example; the SDK core has no Node dependency.
Encoded JSON preserves numeric lexemes; `JSON.parse` may lose integer precision,
so choose a decoder appropriate to the value. Reads return at most 64 KiB of bytes;
assemble chunks before decoding UTF-8/JSON. Each write/append payload is at most
4 MiB, with a final value limit of 64 MiB. `client.appendState` concatenates strings
or arrays against the same explicit revision. `state.get`, `state.list` and
`state.history` return metadata; list cursors are keys and history cursors are
exact revision strings. Reads never admit execution. Shared writes can create notifications for explicit
subscribers; private writes do not.

Keep IDs and exact payloads when recovering uncertain writes. Identical retries
return the original immutable version, not the latest head. Stale revisions and
changed payloads conflict. Deleting a private owner makes retries not-found;
shared versions survive their author's deletion until the tree is deleted.
State handles never authorize ordinary content reads or access to another tree.


Mail metadata uses `source: { kind: 'session' | 'state' | 'completion', id }`. For ordinary mail,
`id` is the sender session; for a state notification it is the subscription ID.
The send request still specifies `sender_id`. Its API cannot impersonate a state
notification. A notification body identifies an immutable `version_id`, `key`,
`revision` and `author_id`; read the value through `state.read` with that handle.

```ts
const subscription = await client.subscribeState({
  session_id: sessionID, key: 'results', after: observedRevision,
  delivery: 'queued',
}, crypto.randomUUID()); // Retain this ID and payload before sending.
const subscriptions = await client.call('state.subscriptions', {
  session_id: sessionID, limit: 100,
});
await client.call('state.unsubscribe', {
  session_id: sessionID, subscription_id: subscription.id,
});
```

Creation catches up atomically from `after`; subsequent writes coalesce pending
notifications. The cursor tracks enqueued/own revisions, not agent processing.
Own writes advance it without self-notification. Cancellation stops future mail
and retains existing notifications. A cancelled creation retry stays cancelled.
If a notification cannot fit the bounded mailbox, the whole state write fails
with a resource-limit error; no state change commits without its notifications.


Child completion policy is `overrides.report_mode`: `notice` (default), `inline`,
or `message`. It follows configuration inheritance and is captured per turn.
Message mode suppresses successful automatic reports; failures still notify the
parent. Automatic reports use queued mail and may wake an idle parent without
creating an input. Reading them does not acknowledge them.

Completion mail has source kind `completion` and a child ID. Parse its body for
`turn_id`, `input_id`, `mode`, truncation flags and bounded previews. Read its
attachment from `mail.evidence_ref`, the same metadata field as authored mail.
It belongs to the parent and contains the full JSON outcome and last assistant
text; use `content.read` with the parent's session ID. The digest keeps this
reference even when the body is truncated. Evidence survives child deletion.

Mailbox or content limits can delay publication while the child is already
finished. Inspect these pending outcomes with bounded pages:

```ts
const pending = await client.call('completions.list', {
  parent_id: parentID, limit: 20,
});
const item = pending.items?.[0];
if (item) {
  const chunk = await client.call('completions.read', {
    parent_id: parentID, child_id: item.child_id, turn_id: item.turn_id,
    offset: '0', length: 65536,
  });
  // Decode data_base64 as JSON bytes; follow next_offset until null.
}
```

A conflict means the exact snapshot was published or superseded; re-list instead
of joining bytes from different turns. Pending reads do not cause publication or
acknowledge mail. Runtime restart retries delivery without replaying completed
child work. A full retained mailbox can keep a report pending; clients should show
that delivery state separately from the child's terminal outcome.


A session's `overrides.output.schema` declares its final output contract. The
runner states the schema to the model, retains every raw assistant reply, and
allows one corrective model round if the final reply is invalid. A second mismatch
fails the turn with `output_invalid`. Each model round has ordinary accounting;
correction never replays the entire turn or an already completed host operation.

```ts
const result = await client.call('turns.output', { turn_id: completed.turn.id });
if (result.output) {
  const json = Buffer.from(result.output.data_base64, 'base64').toString('utf8');
  // json contains the validated value; message_id identifies its raw transcript reply.
}
```

This read derives output from the turn's captured schema and final assistant
message. It keeps no second output store. A live turn returns `BUSY`; a failed turn
or a successful turn without a contract returns `output: null`. A validated JSON
`null` has a non-null output record whose encoded bytes are `null`. JSON bytes
preserve exact numbers; consumers choose how to parse them. Updating or clearing
the session's contract does not change prior turns. Clear with
`patch: { output: { schema: null } }` in `sessions.configure`.


`client.compact(sessionID, requestID)` admits a maintenance input. Preserve the
identity and use `recover`/`wait` and ordinary cancellation exactly as for
`submit`. Its input and turn have `kind: 'compact'`; ordinary work has
`kind: 'prompt'`. A completed compact turn has no authored conversation reply,
structured output, cell, mail acknowledgement or child completion report.

```ts
const requestID = crypto.randomUUID(); // Persist before admission if recovery is needed.
await client.compact(sessionID, requestID);
const compacted = await client.wait(requestID); // Inspect compacted.turn.state.
const head = await client.call('context.head', { session_id: sessionID });
const summaries = await client.call('context.compactions', {
  session_id: sessionID, limit: 20,
});
// While idle, undo the selection without deleting its immutable evidence:
await client.call('context.select', {
  session_id: sessionID, expected_revision: head.revision, compaction_id: null,
});
```

A summary is derived context, not an assistant message. Read a selected summary
with `context.compaction` and its owner/compaction IDs. Undo accepts only a current
ancestor or null and uses a revision check. A conflict requires rereading the
head; busy requires waiting for the active turn. Undo does not restore files or
REPL state. Compaction is bounded and can fail; inspect the turn outcome.

Use `context.snapshot`, then `context.list` or `context.search` with its exact
`through_sequence` and `after: '0'`. Continue from `next_after` until null, even
if a search page has no matches. Search is literal and case-sensitive, with a
256-byte query limit. `context.read` accepts `message_id`, `offset` and a
`length` of at most 65536; assemble its base64 byte pages before decoding UTF-8
or JSON. These APIs inspect retained raw history regardless of the current
summary selection. They do not admit execution or acknowledge mail. Guest
`context.inspect/read/search` uses the same bounded own-history semantics.


Long-turn compaction preserves the exact opening input and the newest complete
assistant/tool exchange. `pinned_message_ids` names raw input-backed messages;
mail entries with user role are not opening inputs. Manual compaction shares the
oversized-context fallback. Raw message sequence numbers remain unchanged.
A confirmed structured provider context-limit rejection can cause one helper
and a new ordinary model round after its failed attempt is recorded. This does
not replay cells or the whole turn; uncertain transport failures still stop.

Set compaction policy through the ordinary revisioned configuration API:

```ts
await client.call('sessions.configure', {
  session_id: session.id,
  expected_revision: session.config_revision,
  patch: {
    compaction: {
      model: { provider: 'my-provider', name: 'summary-model', effort: '' },
      threshold_percent: 50,
    },
  },
});
```

The whole policy is replaced, and active turns retain their prior snapshot.
`{ model: null, threshold_percent: 0 }` resets it to the conversation model and
the 50% default. Effective configuration always reports 1–100. All helpers use
the selected route and ordinary accounting; invalid explicit routes fail.
Proactive compaction requires a configured provider context window and uses
this turn's reported usage or a current-request estimate. It does not reuse a
previous turn's occupancy or count helper usage as conversation occupancy.
Unknown windows still permit manual, local-bound and confirmed-rejection folds.
A failed helper after a final answer fails the turn without removing that answer
from raw history; inspect both the turn outcome and attempt evidence.

Inspect the instructions captured by a turn without rereading mutable files:

```ts
const { manifest } = await client.call('turns.instructions', { turn_id: turnID });
// manifest is null if that turn never captured instructions.
// Otherwise inspect its bytes, sha256 and ordered sources (paths/sizes/digests).
```

An ordinary turn captures authorized workspace project files and project skill
metadata once. A standing `files.read` grant is required for automatic project
reads; one-use file approvals do not enable discovery. File/configuration edits
affect the next turn. Revocation prevents later captures but cannot retract bytes
already captured. A malformed applicable source or failed audit write fails the
turn before provider dispatch. Maintenance compaction does not refresh sources.
Manifests survive restart, retain no file bodies, and cannot reconstruct changed
files or authorize later reads. An empty source list differs from a null manifest.
The original submitted input and raw conversation history remain unchanged.


List current skill metadata for completion or source inspection:

```ts
const page = await client.call('skills.list', {
  session_id: sessionID, prefix: 'review', limit: 50,
});
// page.items: name, description, disabled, source (relative path/size/digest).
// Fetch another fresh page with after: page.next_after when non-null.
await client.submit(sessionID, [{ type: 'text', text: 'Use $review for this change.' }], requestID);
```

The catalog includes disabled skills, which remain explicitly invocable but are
excluded from automatic model discovery. Duplicate names share one deterministic
winner across listing and invocation. Inspection requires standing workspace read
authority, works for idle/stopped sessions, and neither submits work nor reads
bodies. Pages are live metadata, not immutable history. Automatic discovery can be
disabled without disabling this API or explicit invocation.

An explicit reference reads the selected complete file after another authority
check. The body is frozen for that turn and audited as `invoked_skill`; it never
replaces the literal submitted input. Future turns do not re-expand references in
old history. Bodies are at most 256 KiB and share the 1 MiB composed instruction
and 1,152-source manifest bounds. Invalid, changed-identity or unauthorized
selected sources fail capture before provider dispatch.


Host configuration can map logical `skill_roots` IDs to directories; session
instruction policy selects an ordered list of those IDs. This creates no
filesystem authority. Grant standing `skills.read` for each selected ID, just as
workspace capture needs `files.read` for its exact workspace. Unknown IDs fail;
ungranted registered roots are not opened. Workspace names override host names.
Catalog and audit sources contain a relative path plus `scope` and nullable
`root_id`, never the absolute registry path.

The REPL reads a catalog skill with `skills.read({scope, root_id, name, offset, length,
sha256})` (keyword arguments in Starlark). Use `scope: "workspace"` and null
`root_id` for cwd skills, `scope: "host"` for a selected named skill root, or
`scope: "project"` for the selected project instruction boundary.
Use decimal-string offsets and at most 65,536 bytes; pass the returned full-file
SHA-256 for subsequent pages. Join decoded `data_base64` bytes before decoding
UTF-8. Changing the file between pages fails its digest check. Reads use the
current cell's captured policy and ordinary operation permissions; one-use
approval does not turn into standing discovery authority. A host root grants
no permission to neighboring files or executable scripts.


Standing user instructions are opt-in through the captured instruction policy's
`standing_instructions` flag. The host explicitly sets
`standing_instructions_file`; the session needs a standing `instructions.read`
grant with resource `standing`. No home-directory lookup or template creation
occurs during execution. Disabled or ungranted files are not opened.

The complete file must be at most 64 KiB, regular UTF-8 without NUL, and cannot be
a final symlink. Trimmed blank lines and lines beginning with `#` are omitted
from the captured rules. The manifest's `standing_instructions` source hashes
the full raw file, including comments; it exposes only host scope, logical root
ID `standing` and the basename. Active turns keep their captured rules while new
turns refresh authorized sources, including after restart. Missing or malformed
authorized files fail before provider dispatch. Catalog inspection and maintenance
compaction do not read standing instructions.

Ancestor project sources require explicit host `project_roots` and copied session
`instructions.project_root` (a nullable root ID). Grant standing `instructions.read`
with resource `project:<id>` to permit instruction files and `.agents/skills`
through the verified boundary-to-cwd chain. No workspace `files.read` grant is
needed for these source reads; the project grant does not authorize arbitrary
files or scripts. Missing authority leaves only independently authorized cwd
sources. Canonical cwd aliases retain applicable ancestors.

For example, select `project_root: "repo"` in the whole instruction policy, grant
`instructions.read` on `project:repo`, and read catalog metadata using the existing
`skills.list` call. Project metadata uses `scope: "project"`, `root_id: "repo"`,
and boundary-relative paths. A guest can read a selected skill with
`skills.read({scope:"project", root_id:"repo", name:"review", offset:"0", length:65536})`.
Later pages require the returned full-file `sha256`. Selection and source paths
are never grants. Roots/children, live inspection and turn capture share the same
catalog winners; old audit stays immutable while later turns refresh file bytes.


## Durable schedules

```ts
const scheduled = await client.createSchedule({
  session_id: sessionID,
  expression: '@every 10m',
  parts: [{ type: 'text', text: 'Check the build.' }],
}, 'my-stable-schedule-id');
const upcoming = await client.call('schedules.list', {
  session_id: sessionID, upcoming: true, limit: 20,
});
```

Retain the schedule ID and template for uncertain retries; the host captures an
interval anchor exactly once. Slots use UTC strings with nine fractional digits,
so do not round them through JavaScript `Date` when retaining a cursor. Upcoming
pages return `next_cursor`; full templates are available through `schedules.get`.
`latest.identity` addresses the ordinary `receipts.get` outcome, and the accepted
input carries `schedule_id` and `scheduled_for`. At most one input per schedule
is outstanding. Stopped owners retain due slots; cancellation stops future slots
and leaves accepted input/turn cancellation explicit. A deleted owner leaves a
creation retry tombstone. See the [domain contract](../../docs/backend-domain.md#durable-schedules).

The client identities `schedule` and `operation` are reserved for internal
admission. Public submissions and child creation reject them; inspecting their
returned receipt identities with `receipts.get` remains supported.


## Automatic titles

`configuration.automatic_title` controls the helper for future initialization;
an explicit `false` patch is preserved. First authored root text still supplies
a bounded immediate fallback. Explicit titles and manual clears take precedence.
The selected value remains `trees.get` metadata; ordinary metadata revision
changes, including pin/archive, prevent a late helper from replacing it.

`getAutomaticTitleDecision(treeID)` reads immutable source/policy/model evidence.
For eligible decisions, `receipt_identity` addresses the ordinary maintenance
receipt once admitted; it may not exist yet. `getAutomaticTitleResult(treeID,
attemptID)` reads candidate text and historical `applied` evidence. Neither method
starts work, automatically retries inference or caches a second title. Inspect
attempt usage through existing turn methods. Generated naming creates no
conversation message, and interrupted dispatched work is never replayed.


## Provider setup and catalog evidence

Use `providerPresets` / `bundledProviderModels` for setup templates and bundled
metadata, and `listProviders` for the current host revision, safe routes and
explicit defaults. `createProvider`, `updateProvider`, `removeProvider`,
`setProviderDefaults` and `setProviderCompactionModel` require that exact revision.
Reread after an uncertain delivery before deciding whether another mutation is
needed. For a pasted key retain its stable publication ID and original bytes;
reuse with different bytes conflicts. Key bytes and command arguments are input-
only and never returned. Publication may be visible but not yet durably confirmed;
`PROVIDER_KEY_PENDING` distinguishes that state from pre-publication failure.

`providerCatalog` reads local evidence; `refreshProviderCatalog` explicitly runs
discovery. A returned failure may accompany retained same-scope models; a successful
empty result clears them. Credential changes invalidate old scopes. Neither
cached nor bundled metadata proves inference availability. `providerReadiness`
keeps configured/credential/catalog/model evidence separate and reports inference
as `not_tested`. Prices and token limits use exact decimal strings, with null for
unknown values and `"0"` for explicit zero.

Explicit uncatalogued selections remain valid after cache loss. New roots capture
current host defaults; existing sessions retain their selections. These methods
create no SDK cache, automatic refresh, fallback model selection or mutation retry.


### Human questions

`getQuestion(sessionID, operationID)` and `listQuestions({session_id, after?,
limit, pending_only?})` read durable question evidence. Questions project their
ordinary operation's captured request and result; pending history is not a live
waiter. State distinguishes `pending`, `answered`, `dismissed` and `closed`, with
a fixed deadline and explicit nullable closure evidence and an empty answer array before a
successful answer.

Use `answerQuestion(sessionID, operationID, answers)` for a human's exact choice,
free text or dismissal. On uncertain delivery, preserve the original answer and
inspect `getQuestion`; a caller may explicitly resend that same answer. The SDK
never substitutes a new answer or retries on its own. A later different answer
conflicts. After runtime interruption, unanswered questions close and do not
resume; an already committed answer survives. Product rendering and recovery
still belong to the later supported-client adoption work.


### Language servers

`languageServerStatus(sessionID)` reads current safe connection metadata for that
session. It never starts a process or grants access, and omits custom command,
environment and raw startup errors. Runtime restart resets live status. Workspace
listing/search and explicit/automatic diagnostics are ordinary agent operations
whose durable evidence is available through operation reads; diagnostics remain
observations of captured content and do not change the success of a file write.


### Saved permission modes

`getPermissionPolicy(sessionID)` reads the tree’s saved `prompt`/`automatic`
policy and exact decimal revision. `setPermissionMode(params, editID)` edits a
root with its expected revision. Persist the caller-chosen ID and exact payload
before sending; use `getPermissionModeEdit(sessionID, editID)` after a lost
acknowledgement or explicitly resend that same request. The receipt describes
the original edit, including after deletion. Same-value edits preserve the
revision; reusing an ID with changed payload conflicts.

`getDefaultPermissionMode` and `setDefaultPermissionMode` read/edit the host
default through file-revision CAS. They affect future roots/forks only. Recover
uncertain host publication with a fresh read; the SDK does not replay a CAS
against a newer revision. Automatic root policy preserves workspace bounds and
input checks; children continue to need exact delegated grants. Product controls
must distinguish saved policy from individual permission decisions.
