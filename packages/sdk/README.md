# WHIP TypeScript SDK

For product frontend work, read the canonical
[frontend architecture and design guide](../../docs/frontend.md), including how
the app combines SDK views with TanStack Query and local state. This README owns
the SDK's public usage contract.

Private, ESM client package for Node 24, browsers and future Electron clients.
The SDK attaches to an existing WHIP v4 daemon. Execution, credentials, SQLite,
model context, permissions and schedules remain on the execution host.

## Install and check in this repository

```sh
npm ci
npm run build
npm run check
npm run acceptance
npm run test:package
```

`@whip/protocol` contains generated Go-derived wire types and standalone
validators. `@whip/sdk` is browser-safe; `/node` adds Unix sockets, `/state` adds
optional synchronized views, and `/react` adds optional React subscriptions.
Core and state do not import React or Node built-ins. React consumers supply
React 19. Both packages remain private; package-archive installation is tested.

## Attach and submit

```ts
import { createWhipClient } from '@whip/sdk';

const client = createWhipClient({
  endpoint: 'http://127.0.0.1:8080', // accepts a WS URL or daemon HTTP base URL
  clientId: 'my-application',       // persist this namespace to recover commands
  clientKind: 'automation',        // default; interactive apps explicitly use human
});
try {
  await client.connect();
  const creation = client.sessions.create({ cwd: '/path/on/execution/host' });
  const created = await creation.result();
  if (created.status !== 'succeeded' || !created.result) throw new Error(created.failure?.message);
  const session = client.session(created.result.root_id);
  const command = session.submit({ text: 'Explain the current changes.' });
  console.log(command.record); // durable command identity; never the prompt body
  await command.accepted();
  const outcome = await command.result();
  console.log(outcome.status, outcome.result, outcome.failure, outcome.content);
} finally {
  client.close(); // detaches; accepted execution survives
}
```

Node scripts can attach with networking disabled:

```ts
import { createWhipClient, unixSocket } from '@whip/sdk/node';
const client = createWhipClient({ endpoint: unixSocket('/path/to/daemon.sock'), clientId: 'my-script' });
```

Run the included script with either transport:

```sh
node examples/client/node.mjs /path/to/daemon.sock /host/project 'Review the changes'
node examples/client/node.mjs http://127.0.0.1:8080 /host/project 'Review the changes'
```

## Three operation lifetimes

`client.call(method, params)` exposes every generated RPC. Runtime conveniences
use `client.query`, `client.submit`, and `client.invoke` with generated operation
name/parameter/result maps. `session.query`, `session.command`, and
`session.invoke` bind the root ID without a mutable current-session pointer.

Queries return `{result?, content?, root_id?}`. Commands return handles;
ephemeral operations are sent once and are never journaled/replayed by the SDK.
Wrong execution classifications fail at compile time and runtime. Parameters
use generated snake_case data; SDK options use camelCase. Int64 counters remain
decimal strings; compare with `BigInt`, not `Number`.

`result()` resolves the daemon's typed terminal envelope. Failed, cancelled and
interrupted execution are outcomes, not transport errors. Local/protocol errors
reject with a structured `WhipError.kind`; server errors use `RpcError` and keep
the numeric code. A command finishing does not mean all descendants, mailboxes
or schedules have finished. There is no implied whole-tree completion promise.

```ts
await command.result({ signal }); // abort stops only this local wait
await command.cancel().result();  // explicit command-targeted root cancellation
session.agents.cancelTurn(agentId, turnId); // explicit child turn target
```

`cancel()` waits for the original command's acceptance before sending its target.
Only root submit/steer handles support command-targeted cancellation. Other
operations must use their existing explicit turn/control operations.

Upload an image or bounded text excerpt explicitly before submitting it:

```ts
const uploaded = await client.upload(bytes, { rootId, mediaType: 'text/plain' });
const input = { text: 'Use this context.', attachments: [uploaded.asAttachment('text', 'notes.txt')] };
await client.session(rootId).submit(input).accepted();
// A child-only upload uses { rootId, agentId: childId, mediaType: 'text/plain' }.
// Submit its attachment with session.agents.submit(childId, input, 'queued').
```

`asAttachment` never reads the content or embeds it in the request. The host
checks grants, size, digest, and model image support on the execution path.
An input supports 16 attachments totaling 20 MiB; text excerpts are at most
256 KiB each. Text excerpts do not expand `@file` or `$skill` references. An
interrupted upload never attaches itself to future work. Inspecting an existing
content reference is separate from explicitly submitting it as model input.

## Reconnect and recovery

Observe `client.getSnapshot()` with `client.subscribe(listener)`. Initialization
reports host capabilities, limits, persistent runtime ID and generation. Build
equality is irrelevant. New work is rejected while disconnected; the application
keeps drafts. Queries fail on disconnect; callers decide whether to query again.
Accepted work remains daemon-owned.

Lost acknowledgements produce `DeliveryUncertainError`, never a guessed failure.
`command.result()` reconciles by status after reconnect. If status definitively
reports `command_not_found`, call `command.retry()` explicitly to resend the
same original bytes and identity. A failed status lookup is not proof of absence.
Definitive conflicts cannot be erased by retrying a changed request.

Supply `recoveryStorage: {list, put, delete}` to preserve versioned identity-only
records across application restarts. `put` is awaited before transmission;
failure prevents sending. Applications own storage, retention and forgetting.
`client.recoveryRecords()`, `client.recover(record)` and `client.forget(record)`
operate on those records. A recovered accepted command needs no original body.
Retrying a missing command after reload requires
`client.recover(record, originalPayload).retry()`; the application is responsible
for supplying the original request. No prompt body, token, key, MCP secret or
terminal input is persisted by this interface.

A new generation with the same runtime ID recovers normally. A different runtime
ID stops recovery and requires an explicit new client. Do not move a recovery
record between client namespaces or execution hosts.

## Optional synchronized state and React

```ts
import { createSessionView, createSessionListView } from '@whip/sdk/state';
import { useSessionView } from '@whip/sdk/react';

const view = createSessionView(client.session(rootId));
await view.start();
// Any number of React components can call useSessionView(view).
// Other frameworks use view.subscribe and view.getSnapshot directly.
await view.openAgent(childId);
await view.loadOlder(childId);
await view.loadCollection('agents');
await view.dispose();
```

The application owns start/dispose and shares a view between components.
Hooks only subscribe; they never start another cache or connection. Create
resources inside an owning effect or outside React rendering, and dispose that
owner's resources on cleanup. Reusing a disposed resource is an error.

Views combine consistent snapshots, ordered subscriptions, history revisions
and paginated collections. Stale state remains visible during recovery. Root and
child presentation remains separate from committed transcript entries. Inspecting
a child never adds its transcript to another agent's model context.

Pass the displayed revision to `session.history.clear(revision)` just as with
rewind, so another client's destructive edit causes a conflict instead of an
unconditional clear. Transcript message `content` is a string or a content-part
array; text attachments and images use the latter. Large message bodies remain
explicit scoped references under the normal page and view byte limits.

Presentation rows append text, reasoning and terminal deltas. Tool-call arguments
and tool output are cumulative values, so updates replace the matching row by
call ID, including interleaved calls. Snapshots use the same grouping as live
events. A row keeps its first sequence as a stable rendering key; use the root
cursor or raw subscription cursor for stream progress.

The default view retains at most 8 MiB of serialized payload and 512 messages per
opened agent; JavaScript heap overhead is additional. Large/missing output is
explicitly marked. Call `closeAgent` when no longer inspecting a child. The
catalog view polls revisions only while observed and never opens all roots.
There are at most 16 active root subscriptions per connection.

`executionRows(view.getSnapshot(), agentId)` from `@whip/sdk/state` projects
read-only Starlark cells and restart markers for one agent. It merges loaded
history with `snapshot.executions`, which the existing session subscription
maintains. Calls retain stable keys across commit; cumulative arguments/output
replace earlier values; repeated host calls keep separate event identities.
`ExecutionCell` exposes code, output, result/error, status, steps, scoped body
references and optional client-observed times. Recorded outcomes without enough
evidence are marked unknown, and historical durations are not invented.

Supplemental execution evidence is capped at 256 entries per root, 128 host calls
per cell and 1 MiB **inside** the view's payload budget. Truncation is explicit.
History revisions and root changes invalidate incompatible observations. When
a child was observed before its history was loaded, ambiguous reused call IDs
remain separate observed evidence instead of borrowing an older result. Durable
transcripts recover code/results; host traces and restart details observed before
the client attached may be unavailable. This API does not add a subscription,
fetch all history or load every child's transcript. Use `loadOlder(agentId)` and
scoped content reads explicitly when needed.

For scripts, `await client.events.subscribe(rootId, cursor)` gives a single
bounded async iterator. Install a view or read a snapshot to obtain a cursor.
Expired cursors, sequence gaps, and slow consumers fail explicitly; reacquire a
snapshot or replay instead of continuing an incomplete stream. Unknown future
event kinds are marked `unknown: true`.

## Model accounting

Protocol v4.1 adds optional `RootSnapshot.accounting` for the entire session tree.
`SessionView` updates it from scoped, revision-ordered `stream.accounting` events,
without adding accounting rows to the conversation or execution evidence. Model
attempt lifecycle events refresh budgets through the existing coalesced snapshot
path. Reconnect restores the host snapshot; no separate polling is required.

Cost counters are decimal strings in microdollars. `reported_cost_micros` is the
provider's charge (including zero); `estimated_cost_micros` uses saved catalog
rates. `unknown_cost_calls` and `estimated_calls` respectively count unknown
cost and incomplete token usage, and can overlap. `pending_calls` counts active
requests. Never treat absent accounting as a known zero or add subtree totals
to their descendants. `session.agents.inspect(id)` returns optional own-agent
accounting under `result.accounting`, distinguished by `scope: 'agent'`.

## Content and human approvals

```ts
const reference = await client.upload(bytes, { rootId, mediaType: 'text/plain' });
const text = await reference.readText({ maxBytes: 1 << 20, signal });
const stored = client.content(handle, { rootId, agentId });
const value = await stored.readJSON({ maxBytes: 512 << 10 }); // unknown; validate before use
```

Content reads verify association, exact size and SHA-256. WebSocket clients use
HTTP; Unix clients use existing bounded RPC chunks. Uploads grant content to the
root; the daemon must explicitly grant it to a child. Large command outcomes
stay content references rather than causing implicit downloads. Upload failure
never submits a prompt or attaches a partial file. Browser WebCrypto requires
a secure context such as localhost or HTTPS.

Connected clients can approve or deny requests directly:

```ts
await client.permissions.decide({ root_id: rootId, permission_id: permissionId, allow: true });
await client.permissions.setMode(rootId, false).result();
```

Permission decisions require no signer, enrollment, or authentication. Both human
and automation client kinds can answer requests. The daemon validates the
permission's root and enforces tool permissions and delegated authority. Decisions are sent once; after an uncertain acknowledgement, inspect
pending state before explicitly retrying with the original `command_id`.
Permission-mode changes use ordinary durable command handles. Provider secrets
stay outside recovery storage and logging; the SDK does not log payloads.

Provider/configuration helpers (`client.providers`, `client.configuration`) call
host services. Login status/list allow reconnect; restart interrupts incomplete
flows. Configuration updates require the last read revision and surface conflicts.
`client.providers.validate({ name, base_url, key })` checks a candidate key without
saving it; `setKey` already validates before saving. `rotateKey('inference')`
rotates the execution host's machine key. Both are ephemeral and sent once;
inspect provider status after an uncertain acknowledgement before retrying.
`session.terminalInput` is ephemeral and is never automatically retried.

## React example and validation

Host bootstrap reads do not create sessions:

```ts
const directories = await client.host.directories({ path: '~/projects' });
const providers = await client.providers.catalogs();
const themes = await client.host.themes.list();
const colors = await client.host.themes.resolve('opencode');
const attention = await client.host.attention();
```

Directory and attention results have explicit pagination and truncation. Attention
is a live index: refresh from its first page for newly active roots. Session
catalog searches use `client.sessions.list({ search })`; group displayed paths
using `workspace_id` scoped to the host runtime, never the shortened `cwd` label.
Custom theme JSON can be validated/resolved with `host.themes.resolveJSON(json)`;
selected themes remain client preferences.

`session.mailbox.list({ agent_id, status })` and `session.mailbox.read(id, agentId)`
inspect inter-agent mail without changing delivery state or model context.
Bodies are bounded text or scoped content references. This is separate from the
execution command inbox, and message revisions remain decimal strings.

Start a daemon with `WHIP_NETWORK=1` and
`WHIP_ALLOWED_ORIGINS=http://localhost:3000`, then read its endpoint from
`whip daemon status --json`. Start the example with:

```sh
npm run build
npm start -w @whip/client-example
```

Open http://localhost:3000 and enter the reported endpoint. The example supplies
its own metadata-only localStorage recovery adapter and sessionStorage drafts.
Pending permissions expose Allow once and Deny whenever the client is connected.
This remains a minimal example of the SDK primitives.

```sh
WHIP_SDK_RACE=1 npm run acceptance
npx playwright install chromium firefox
npm run test:browser
npm run test:example
npm run test:package
task check
task acceptance
```

Tests start isolated fake-provider daemons, never the user's active daemon.
The browser run loads the actual built SDK under strict CSP and checks React
StrictMode; on macOS it also opens actual Safari. Set
`WHIP_SDK_BROWSERS=chromium,firefox` for a Linux CI run. No Safari setting is changed.

The registry tests cover all registered methods using Go-produced fixtures;
daemon acceptance tests cover execution and recovery behavior over both
transports. Measurements and completed release checks are recorded in
[`typescript-client/README.md`](../../.ai-docs/plans/typescript-client/README.md).
