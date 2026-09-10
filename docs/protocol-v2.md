# WHIP protocol v6

The Go daemon owns execution, admission, provider credentials, model context,
configuration and SQLite persistence. Unix sockets and WebSockets use the same
JSON-RPC 2.0 methods, typed payloads, validation and handlers. WHIP's protocol
major is `6` (minor `0`); the JSON-RPC envelope version remains `"2.0"`. Compatible builds
attach regardless of build ID. Replacement of a running daemon is explicit.

The executable contract is `internal/protocol`: wire DTOs, operation registry,
permission metadata and schemas. `packages/protocol/schema/manifest.json`
lists RPCs and runtime operations with their parameter/result types. Generated
TypeScript declarations and Ajv validators are exported by `@whip/protocol`.
Run `npm ci` and `npm run check` from the repository root. Regenerate after
editing Go types with `npm run generate`; drift checks compare without rewriting
files. Standalone validators require no runtime code generation or Ajv dependency.
Typed RPC/runtime maps classify query, durable and ephemeral operations. The
handwritten `@whip/sdk` consumes this contract; see [SDK usage](../packages/sdk/README.md).

Protocol **6.0** adds immutable session execution languages and cell result format 2.
Older clients are rejected during `initialize`, before they can subscribe to JavaScript
cells whose completion no longer depends on Starlark `steps`. `execution_engines`
advertises bundled descriptors (`id`, `language`, `label`) and initialization includes
`default_execution_engine`. `session.create.execution_engine` selects `starlark` or
`quickjs`; an omitted value resolves on the execution host. Clients freeze the
resolved language with the create command before its first submission. Session
metadata and agent views expose `execution_engine`; descendants always derive it
from the root. Child selection overrides are rejected, and forks preserve the
source language without copying guest checkpoints. `configuration.get` and
`configuration.update` expose `default_execution_engine`, backed by `rlm.defaultEngine`.

Protocol **6.1** adds `session.create.definition`, which names the agent
definition a new session runs (`coding` by default, or `junior-developer`), and
`definition` on session metadata and snapshots so clients can show and assert it.
An unknown id fails creation with the available ids. Both fields are additive;
6.0 clients continue to create coding sessions.

Fresh stores use schema 15. Versions 10–14 migrate transactionally through each
required upgrade, preserving identity, history, command receipts and legacy Starlark
scratch. Version 15 adds a root engine column guarded against updates and an internal
BLOB checkpoint table keyed by root/agent. Checkpoint publication preserves the last
good image on failure, validates ownership, engine and SHA-256, and enforces 40 MiB
per image, 256 MiB per session tree and 1 GiB daemon-wide. One generation per agent
is retained; deleting an agent subtree or session removes its guest images.
Engine build/ABI/profile incompatibility fails restore explicitly and retains the image.

Protocol **5.0** adds required `archived` metadata to sessions, snapshots, and
navigation summaries, and requires a normalized status in catalog cursors.
Catalog queries default to active sessions and may request archived or all
sessions. These wire changes require clients and daemons to update together;
older majors fail during `initialize`, before session reads or commands. There
is no compatibility fallback. The existing `/api/v3/ws` transport path remains
stable. Fresh runtime stores use schema 12. Schema 10 upgrades through the
transactional archive migration to schema 11, then through the persisted
last-turn migration to schema 12. Schema 11 stores run only the latter step.
Both steps preserve existing IDs, history, configuration, and command state.
Other older stores are rejected without mutation; upgraded stores cannot be
opened by older binaries.

Protocol **4.0** added explicit unlimited model budgets: `limit` and `remaining`
are decimal strings or `null`, with separate `uncertain` and `incomplete` fields.
This breaks the old finite-only contract, so clients and daemon update together.
The initialize handshake owns protocol compatibility.

Protocol **3.0** removes client enrollment, signing keys and connection nonces.
All connected clients may answer permission requests and change permission modes.
This is an incompatible trust-model change: protocol 2 clients must update and
explicitly restart the daemon; older majors are rejected, with no fallback.
The JSON-RPC envelope remains 2.0 and the runtime data directory remains
`runtime-v2`; wire version and storage location are separate.

Protocol **3.1** adds the negotiated `session_summaries` capability and read-only
`sessions.summaries({root_ids})`. Clients send at most 32 distinct root IDs, each
at most 256 UTF-8 bytes. The response returns one item per requested ID in request
order, with `missing`, bounded `title`/`cwd`, optional stable `workspace_id`, decimal
string `running_agents`, `queued_agents`, `pending_permissions` and
`pending_questions`, plus `truncated`. The complete result is bounded to 64 KiB;
identity and counts are preserved when presentation strings need shortening.
These are advisory observations across descendants, not an atomic execution
snapshot or completion guarantee. Lookup failures return errors rather than
inventing missing roots. This query opens no actor/transcript, writes no journal,
and originally required no schema change. Current clients require protocol 5.
Coverage: daemon/session `TestSessionSummaries*`, SDK summary
and generated interoperability tests.

`command_not_found` (`-32011`) identifies a missing command in the initialized
client namespace. A generic lookup failure is never proof that a command was
not accepted. Responses contain exactly one of `result` and `error`, including
`result: null` for a successful null result.

## Local and trusted-network setup

Network serving is disabled by default. Unix clients continue using the daemon
socket. Enable a loopback listener on an ephemeral port with:

```sh
WHIP_NETWORK=1 WHIP_ALLOWED_ORIGINS=http://localhost:3000 whip daemon start
whip daemon status --json
```

The reported `network_endpoint` is an HTTP base URL. Connect a browser
WebSocket to `/api/v3/ws` on that endpoint, replacing `http:` with `ws:`.
An existing daemon must be explicitly stopped/restarted for a changed network
configuration to take effect. Environment options are inherited by automatic
starts and daemon binary replacement.

For an explicitly trusted network, configure a bind address and exact hosts:

```sh
WHIP_LISTEN=192.168.1.10:8080 \
WHIP_ALLOWED_HOSTS=192.168.1.10:8080,whip.local:8080 \
WHIP_ALLOWED_ORIGINS=http://localhost:3000,http://whip.local:3000 \
whip daemon start
```

`WHIP_NETWORK=false` explicitly disables serving. `WHIP_LISTEN` alone enables
it; when network serving is enabled without a bind address, the default is
`127.0.0.1:0`. With no explicit accepted hosts, only the listener's actual
address is accepted. Hosts include the port; origins include the scheme and
port and have no trailing slash. Wildcards and origin suffix matches are not
supported. Requests without an Origin header are permitted for native clients.
A supplied browser Origin must match the configured list, including for HTTP
content transfers. The `null` browser origin is not accepted.

Clients run locally or on an explicitly trusted network. There is no connection
authentication, device pairing or hosted relay. Client IDs are command namespaces,
not authenticated user identities; client kind does not restrict permission
decisions. Every connected client is trusted to approve or deny requests.
Permission prompts, remembered rules, content grants, budgets and internal agent
and delegated MCP authority checks still apply. A reverse proxy may terminate
TLS when desired; configure its externally visible Host and browser Origin
explicitly.

## Envelopes and initialization

Unix sockets use one newline-delimited JSON envelope per message. WebSockets
use one envelope per text message. Binary WebSocket messages are rejected.
Fragmented messages have a cumulative 1 MiB bound; control and data frames are
written under one lock. Large bodies travel through HTTP content transfers.

`initialize` is the first request. A request ID identifies only its response;
it is unrelated to a durable command ID, connection ID, root ID, agent ID or
turn ID. Initialization reports protocol/build information, the persistent
runtime ID, daemon generation, connection ID, execution host platform and
capabilities. Build equality is not required for compatibility. Older protocol
majors are rejected without restarting the runtime.

```json
{"jsonrpc":"2.0","id":"request-1","method":"initialize","params":{"protocol_major":5,"build_id":"browser-build","client_kind":"human","client_id":"browser-installation"}}
```

Fields use snake_case. Signed 64-bit counters use decimal strings; JavaScript
must not convert them to numbers. Binary data uses base64. Event payloads and
operation results are JSON, without a second string-encoded JSON document.
Unknown methods and invalid parameters fail before execution admission.

## Commands and queries

Submit a mutation with `command.submit`. Its payload names a runtime operation
and contains that operation's typed parameters:

```json
{"jsonrpc":"2.0","id":"request-2","method":"command.submit","params":{"command_id":"compose-7","scope":"root","root_id":"root-id","operation":"submit","payload":{"text":"Implement the requested change"}}}
```

The receipt acknowledges committed admission, not execution completion. Reuse
the same `(client_id, command_id)` when retrying an uncertain delivery. An
identical retry observes existing work; a changed payload under that identity
fails with a conflict. `command.status` reads durable state and structured
outcomes. Go clients can use `SubmitAndWait` when they need completion.
Disconnecting cancels connection queries and subscriptions, not accepted work.
Cancellation targets a specific turn so a delayed cancellation cannot affect a
later turn. Uncertain external effects are interrupted rather than replayed.

Read operations travel through `query` with `root_id`, `operation` and typed
`payload`, and do not create command journal entries. `operation.invoke` handles
explicitly ephemeral operations such as terminal input and MCP attachment.
Credentials and terminal input must never enter durable retry queues. Provider
login operations expose progress and choices while keeping tokens on the host.
Configuration writes require a revision and preserve host-side atomic writes.

## Reconnect and bounded views

Use `root.snapshot` to obtain recent history and active presentation state with
one consistent event cursor. Subscribe with `events.subscribe` after that
cursor, assigning a fresh subscription ID. `events.replay` reads durable events;
expired/ahead cursors require explicit resynchronization. Replay failures are
reported rather than silently stopping a stream.

Each event identifies its subscription. Ignore queued events from a replaced
subscription, even if their root ID matches the current view. Use
`events.unsubscribe` to release it; at most 16 subscriptions may be active on
one connection. Live delivery currently polls durable events every 50 ms.

Snapshots bound both item counts and bytes and explicitly mark omitted or
truncated state. `history.page` reads the existing raw transcript; older pages
carry the captured history revision so rewinds cannot mix old and new views.
Large message bodies use content references. Human inspection of child history
does not admit it to an agent's model context. Pending questions and permissions
are included in reconnect state; answering resolves the shared pending item.

Large stream payloads use `ContentEventPayload`: `content` references the full
JSON event and `truncated: true` marks omitted fields. New emissions retain the
stream's agent, turn, call and invocation identity plus complete small fields
inline, so cumulative updates and completions remain attributable. Clients must
also accept older content-only variants: advance the cursor and mark evidence
unavailable, without attributing an unknown owner to the root. Missing results
do not imply success. Accounting and usage have their own projections and do not
consume the reconnect snapshot's presentation suffix.

`history.rewind` and `session.fork` require `expected_revision`. `history.clear`
also accepts this decimal-string precondition; applications should send their
displayed history revision so a delayed clear cannot erase a different history.
The root checks it before changing history or releasing workspace snapshots.
Omitting it retains the explicit unconditional-clear behavior used by existing
Go callers.

Transcript `message.content` is a string for plain text or an array of typed
content parts for multimodal input. Text parts carry `text`; image parts carry
`image_url.url` and optional dimensions `w`/`h`. These are the existing persisted
message forms, not a second presentation history. Snapshot and history byte
limits still apply; oversized messages are omitted explicitly or returned as
scoped content references. Clients must handle both content forms.

## Content transfers

Upload a body with `POST /api/v3/content/upload?root_id=ROOT`. Supply the body
length, `X-Content-SHA256` with a lowercase hexadecimal SHA-256 digest, and an
optional Content-Type. The response is a content handle. The upload must fit
the existing input limit (64 MiB); interrupted or mismatched transfers remove
temporary state. Browsers supply Content-Length automatically.

Pass optional `agent_id=CHILD` when uploading for a specific child. Unix clients
use the same `agent_id` on `upload.begin`. Child uploads create an exact recipient
grant; parent, sibling, and unrelated-root reads do not inherit it. Omitting the
agent, or selecting the root agent, retains the existing root grant. Uploads do
not submit work or admit content into any agent's model context.

Download with `GET /api/v3/content/REFERENCE?root_id=ROOT&agent_id=AGENT`.
The reference/root/agent association must satisfy the existing content grant.
The agent may be omitted for a root grant. Reads are bounded and recheck the
grant between chunks. Content is served as an attachment with an inert media
type so uploaded HTML cannot execute on the daemon's origin. No tickets are
issued. Transfers have a separate limit of 16 concurrent HTTP requests.

The `input_attachments` capability accepts optional `attachments` on `submit`,
`steer`, and `agent.submit`: each entry contains `kind` (`text` or `image`), an
unchanged `content` handle, and an optional display `name`. Requests and their
durable input journals retain references, not copied attachment bodies. Admission
validates identity, metadata, and the root/recipient grant. The turn worker reads
bounded chunks, rechecks grants and metadata, and verifies the full SHA-256 digest
before constructing model input. Matching command retries observe their original
state even if the content grant has since been revoked.

An input accepts at most 16 attachments totaling 20 MiB; each UTF-8 text excerpt
is limited to 256 KiB and names to 256 bytes. Images require matching supported
image media types, valid dimensions (at most 32768 per side and 64 megapixels),
and a model with vision support. Unsupported or corrupted input fails explicitly.
Database/I/O failures and cancellation retain the existing execution recovery
semantics. Uploaded text remains a separate content part; its `@file` and `$skill`
text is never interpreted as an authored-input expansion. Existing text and
`parts` inputs remain supported. A daemon restart recovers the persisted input
references without creating another upload or duplicating a command.

## Permission decisions

Any connected client can approve or deny a pending request. No enrollment,
signer, signature, keychain entry or connection nonce is required. Permission
requests remain durable and appear in root snapshots and events. The daemon
resolves competing answers once and revalidates the operation, capability,
budget, path and policy before resuming execution. Client approval cannot grant
an agent a capability it does not have.

```json
{"jsonrpc":"2.0","id":"decision-request","method":"permission.decide","params":{"decision":{"command_id":"decision-7","root_id":"root-id","permission_id":"permission-id","allow":true}}}
```

The result contains `operation_id` and `lease_id`. A successful reply accepts one
decision handoff to the dispatcher. Revalidation and the tool operation settle
asynchronously; observe runtime state and events for their authoritative outcome.
The reply does not establish tool completion, and a crash during the handoff can
interrupt the underlying operation. Optional `reason` explains a decision;
`remember` accepts `tree` or `global` for an allowed request. An uncertain
acknowledgement requires reconciling current permission state; the SDK
does not automatically repeat decisions.

Permission mode changes use the ordinary durable runtime `permission.mode`
operation through `command.submit`, with typed `external_permissions` parameters.
The root session stores this choice durably (`true` → `prompt`, `false` →
`automatic`). Its update event and stored value commit together. Full and bounded
root snapshots expose the saved value as `permission_mode`, and daemon restart
restores it before root or child work resumes. New and migrated sessions default
to `prompt`; attachment does not overwrite an existing choice. Headless and deny
execution restrictions are separate from this consent preference.
There is no separate `permission.mode` RPC. Mode changes and rules still obey
runtime admission checks. `identity.enroll` and `identity.status` are removed.
This trusted-client assumption does not expose an approval tool to agents or
remove delegated MCP authority validation.

## Saved execution hosts

`config.get` advertises saved-host support with a present `remote_hosts` array,
including when empty. Each profile has `id`, `name`, `url`, `runtime_id` and
`connect_on_launch`. `config.update` accepts an optional replacement array along
with the existing required configuration `revision`; an empty array removes saved
profiles, while omission preserves them. Validation and atomic revision-checked
persistence use the existing configuration path and preserve provider credentials.

The locally launched web app reads and writes this registry only through Local.
It verifies each remote's handshake identity and opens independent SDK connections;
there is no new aggregate session API or forwarded production traffic. Older
local daemons without this optional field remain usable for local sessions but
must be updated before profiles can be saved. Remote daemons need only the
capabilities used by their own views. See [web setup](web-app.md) and
[frontend ownership](frontend.md).

## Persistence and validation

SQLite remains in WAL mode with `synchronous=NORMAL`. Accepted commands survive
a daemon crash; this is not a guarantee against power loss. Incompatible
development databases fail with an archive/reset message. Startup never
silently removes them or changes unrelated configuration, credentials or
workspace files.

`TestV2CrossTransport*` exercises concurrent Unix/WebSocket clients, committed
admission, cross-transport retries and conflicts, reconnect snapshots, replay
boundaries, subscription exhaustion, and trusted-client permission decisions. The
transport tests additionally cover fragmented-message limits, invalid frames,
concurrent ping/data writes, close during fragmentation, exact host/origin
checks, content grants and interrupted upload cleanup. The generated package
checks TypeScript declarations, Go wire fixtures, typed permission decisions
and generation drift. Full runtime acceptance and race suites remain required.

## Real-browser smoke harness

The SDK acceptance harness starts a temporary daemon with a fake runner, an
allowed frontend origin and an isolated database. It never connects to the user's
active daemon. It runs the **built SDK**, not a second handwritten RPC client:

```sh
npm ci
npx playwright install chromium firefox
npm run test:browser
WHIP_SDK_RACE=1 npm run acceptance
npm run test:package
```

Chromium and Firefox run headlessly. On macOS the browser runner also opens an
isolated page in actual Safari and receives the result through the temporary
frontend. No remote-automation setting changes are needed. Set
`WHIP_SDK_BROWSERS=chromium,firefox` for Linux CI; an omitted Safari run is not
reported as a Safari pass. `WHIP_SAFARIDRIVER_URL` can optionally select an already
enabled WebDriver. The test page can be closed after completion.

The external ES module runs under strict CSP without unsafe-inline or unsafe-eval.
Coverage includes command acceptance/recovery, snapshots/views, React StrictMode
subscriptions, unsigned permission decisions and mode changes, scoped
uploads/downloads and cleanup. JSON results are written to
`/tmp/whip-sdk-browser-results.json`; acceptance measurements to
`/tmp/whip-sdk-measurements.json` (or the OS temporary directory on other hosts).

### Bounded collections and session catalog

`root.collection` pages agents, inbox, blackboard, budgets, capabilities,
schedules, and pending permissions with a count limit of 128 and a byte budget
of 4–512 KiB. Each entry has one typed item field. An item too large for the
page uses `body`, a root-granted content reference to that same JSON entry.
Cursors bind the root, collection, offset, and collection revision; intervening
collection changes require restarting pagination. Revisions are updated with
SQLite mutations, independently of event publication.

`sessions.list` pages lightweight session metadata directly from SQLite without
opening roots. `sessions.revision` supports inexpensive polling invalidation.
Catalog cursors are invalidated when catalog metadata changes. Summary fields
are limited to 128 Unicode characters, with `truncated: true` when applicable;
exact actionable metadata remains available through `sessions.get`. Both page APIs
require `limit` and `max_bytes`.

`sessions.list` accepts a bounded `search` string matched against the complete
title and working directory. Its optional `status` is `active` (the default),
`archived`, or `all`; filtering happens before pagination. Its cursor binds the
normalized status, search, and catalog revision. Refresh from the first page
when that revision changes. `workspace_id`
is a digest of the complete stored working directory, independent of its
shortened display text. Clients namespace it by host runtime ID; they must not
group workspaces by the shortened `cwd`. Paths beyond the defensive 4096-character
read bound omit the workspace identity and remain distinguishable by session ID.

`sessions.get({root_id})` returns exact title and working-directory strings,
`history_revision` as a decimal string, and `archived` without opening an actor,
transcript, or subscription. Its response is limited to 64 KiB; oversized
metadata fails explicitly instead of returning truncated actionable values.
`session.archive({archived})` is a durable command that changes catalog visibility.
Explicit root snapshots, history, summaries, and Attention still include archived
roots. Archive and restore preserve running work, pending questions, and recency.

### Host bootstrap, attention, and themes

`provider.catalogs` is a genuinely rootless runtime query: no root actor is
constructed and no model session is required for provider onboarding.
Its optional `refresh: true` parameter forces upstream discovery; ordinary reads
reuse fresh cache entries. Provider descriptors carry optional `available`
metadata so clients can exclude unavailable model routes without deleting aliases.

`provider.list` is the cheaper host-owned connection inventory: it returns safe
source/readiness metadata, the resolved default provider and a configuration
revision, without upstream requests or secret-command execution. `config.update`
accepts `disabled_providers` under the same revision check as other settings.
Provider entries optionally include `category`, `family`, and `key_url` for
known-provider presentation. `key_source: "env_file"` or `"key_file"` identifies
an explicitly configured named-key file on the execution host. Optional
`environment_variable` and `credential_path` contain the name/path only; the key
itself never enters inventory.
`provider.discover` accepts the same optional `{ model, provider }` selection as
`provider.list`, rereads host sources, and persists missing provider references.
It returns inventory and optional `discovery_error`. It preserves overrides,
disabled IDs, defaults and no-op revisions. It is an ephemeral host operation,
never a durable session command or an implicit part of inventory polling.
Setup-open and explicit Refresh use it; older hosts can retain read-only inventory
behavior when they do not advertise the operation.
`provider.key.set` can include a transient `discovery` result in its returned
configuration to distinguish loaded model catalogs from unverified public or
bundled lists. Ordinary configuration reads omit it; it is not persisted state
or proof of a successful inference call.
`provider.disconnect` requires `{ provider, revision }`, removes the selected
WHIP-owned credential and disables the route. External/environment credentials
can only be disabled. Legacy `provider.logout` retains account-only behavior.
Disabling rejects subsequent model-request admissions, including helpers,
subagents and compaction; already admitted calls keep their route snapshot.

`host.directories.list` browses directories on the execution machine before a
session exists. Its optional `path` is absolute or begins with `~/`; empty starts
at the execution host's home. `prefix`, `show_hidden`, and `after` select entries,
and `limit` is 1–128. Results contain directory names and full paths, the parent,
`next_after`/`has_more`, and an explicit `truncated` flag when the 20,000-entry
scan ceiling is reached. It reads no file bodies. Symlinks to directories are
navigable. Filesystem changes can alter later pages; refresh the directory to
obtain a new listing.

`host.attention` reads a lightweight live index without opening every root or
spending root subscriptions. `limit` is 1–128 and `max_bytes` is 4–512 KiB. Items
contain root identity/title, decimal-string active-agent and pending-permission
counts, and short live question metadata. Full choices and permission details
remain in the selected root snapshot. Page using `next_after_id` as `after_id`;
refresh from the first page to see newly active roots earlier in the ordering.
This is an advisory live view, not a replay cursor or an atomic root snapshot.
Questions disappear with their live turn, including restart. If the in-memory
root inspection cap of 10,000 is reached, `truncated` is explicit.

`host.themes.list` returns the shared shipped catalog and bounded custom themes
from `WHIP_HOME/themes`, including individual malformed-file errors. Custom
discovery is capped at 128 directory entries and files at 64 KiB.
`host.themes.resolve` accepts exactly one `name` or `json` string and returns
normalized semantic colors, syntax/Markdown roles, and Chroma token styling.
Names are catalog identities, never arbitrary paths. Resolution is pure;
neither importing nor selecting a theme writes daemon configuration. Browser
theme selection and validated startup caches belong to the client.

### Human inspection of inter-agent mail

`mailbox.list` is distinct from the command input `inbox` collection. It accepts
root and recipient agent IDs, optional status (`all`, `pending`, `delivered`, or
`done`), a count limit of 1–128 and a 4–512 KiB byte budget. Metadata includes
sender/recipient, delivery class, excerpt, timestamps, decimal-string message
revision, and body size/reference. Its cursor binds root, agent, status, offset,
and collection revision; changes explicitly require starting pagination again.

`mailbox.read` reads one message associated with that root and recipient. Inline
bodies stay within the existing 8 KiB storage bound and are encoded as text;
larger bodies retain their existing content reference and root/agent grants.
Inspection does not acknowledge, deliver, defer, or complete mail, admit it into
model context, or start agent work.

Runtime query results larger than 512 KiB return a root-scoped content handle
instead of an oversized envelope. The Go `Query` convenience method resolves
that content using existing grant checks. Rootless queries cannot create an
unscoped content grant; clients should use the paginated catalog API.

The generated TypeScript package includes `EventPayloadTypes` and
`TypedRootEvent`, linking ordered event kinds to their payload DTOs. Provider
catalog wire fields use snake_case while the host's provider-cache format
remains unchanged.

Inline runtime values use `inline` for JSON, `text` for UTF-8 textual bodies,
and `binary` for base64-encoded binary bodies. Exactly one representation may
be present. Content references omit all three. Existing stored content bytes
and hashes retain their original representation.

## Model accounting

Protocol v4.1 adds optional `accounting` fields. Root snapshots expose them for
the whole agent tree; agent transcript
results expose the selected agent's own totals. `stream.accounting` publishes
updated tree summaries. Each summary carries an event `revision`; clients
must ignore summaries older than the one already applied, including summaries
queued before a newer snapshot.

Provider-reported cost, estimated cost, unknown-cost calls, estimated-usage
calls, and pending calls are separate fields. Monetary values are microunits
of USD and all int64 fields use decimal strings. `model.call.started` commits
with admission, so changes in pending counts advance the summary revision.
`model.call.settled` commits with the budget changes; interrupted calls also
emit `model.call.interrupted`.
A late result replacing an interrupted estimate emits `model.call.corrected`.
These lifecycle events contain bounded call metadata, never prompts or keys.

Budget limits and remaining amounts remain nullable: `null` means unlimited.
Missing usage stays in `uncertain`, not `used`; an unknown numeric cost can have
zero `uncertain` and `incomplete: true`. Reported-cost and token-usage completeness
are independent. A late correction removes only the matching uncertainty.
Catalog wire models retain scalar display prices and add optional raw `pricing`
decimals so clients can round-trip exact rates without repricing old calls.
