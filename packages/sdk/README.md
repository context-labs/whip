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

`turns.attempts` reads bounded provider accounting with exact decimal counters.
Retries have separate attempt IDs, a shared logical-call ID, and a link to the
completed response. Unknown usage/cost is `null`, independently of known zero.

Use `content.put` with `{session_id, reference_id, media_type, data_base64}` to
upload up to 4 MiB. Generate and retain a unique reference ID before sending;
retrying it with the same owner, bytes and media type returns the same reference.
Submit `{type: 'content', reference_id}` parts alongside text. `content.read`
takes the owning session and reference IDs and returns verified `data_base64`.
References are session-scoped; a digest is not an access token. The runtime
hydrates authorized bytes for the provider while history keeps the reference.
Each session is limited to 1,024 references and 64 MiB of referenced bytes.

For configured HTTP providers, omit `-scripted` and use the host configuration
described in [the development guide](../../docs/backend-redesign-development.md#openai-compatible-dispatch-increment).

See [the runnable example](examples/session.mjs), [Go client](../../internal/client/client.go),
and [real process acceptance](../../scripts/redesign/v4-fixture.test.mjs). Product-client adoption remains in progress.


The v4 transcript now includes assistant `tool_call` parts with a stable call ID,
name and JSON arguments, followed by `tool_result` parts in tool messages. A
result includes its call ID, output string and error flag. Inputs accept only
text/content parts. Completed calls and results survive interrupted turns; an
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


`client.observe(sessionID, {after?: '0', signal?})` yields bounded committed pages
and a disposable preview. The underlying `sessions.observe` result contains
`messages`, nullable `preview`, and the current process `epoch`. Each preview has
an attempt ID, eventual `message_id`, revision, text, incomplete call fragments,
and a truncation flag. Never execute preview arguments. Upsert committed messages
by ID; a matching committed ID replaces the preview. Clear provisional display
when the preview is null or the epoch changes. Aborting this iterator stops
observation only. It retains a cursor, not a transcript cache.


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
version 2 supplies root defaults; existing trees retain their persisted limits.
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
`turn_id`, `input_id`, `mode`, truncation flags and `evidence_ref`. The reference
belongs to the parent and contains the full JSON outcome and last assistant text;
use `content.read` with the parent's session ID. Evidence survives child deletion.

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
