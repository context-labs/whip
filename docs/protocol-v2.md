# WHIP protocol v2

The Go daemon owns execution, admission, provider credentials, model context,
configuration and SQLite persistence. Unix sockets and WebSockets use the same
JSON-RPC 2.0 methods, typed payloads, validation and handlers. WHIP's protocol
major is `2`; the JSON-RPC envelope version remains `"2.0"`. Compatible builds
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

Protocol minor **1** adds structured `command_not_found` (`-32011`) for a missing
command in the initialized client namespace. A generic lookup failure is never
proof that a command was not accepted. Responses contain exactly one of `result`
and `error`, including `result: null` for a successful null result.

## Local and trusted-network setup

Network serving is disabled by default. Unix clients continue using the daemon
socket. Enable a loopback listener on an ephemeral port with:

```sh
WHIP_NETWORK=1 WHIP_ALLOWED_ORIGINS=http://localhost:3000 whip daemon start
whip daemon status --json
```

The reported `network_endpoint` is an HTTP base URL. Connect a browser
WebSocket to `/api/v2/ws` on that endpoint, replacing `http:` with `ws:`.
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

This milestone adds no connection authentication or hosted relay. Client IDs
are command namespaces, not authenticated user identities. Existing signed
human approvals, automation restrictions, content grants and delegated MCP
checks still apply. A reverse proxy may terminate TLS when desired; configure
its externally visible Host and browser Origin explicitly.

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
{"jsonrpc":"2.0","id":"request-1","method":"initialize","params":{"protocol_major":2,"build_id":"browser-build","client_kind":"human","client_id":"browser-installation"}}
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

## Content transfers

Upload a body with `POST /api/v2/content/upload?root_id=ROOT`. Supply the body
length, `X-Content-SHA256` with a lowercase hexadecimal SHA-256 digest, and an
optional Content-Type. The response is a content handle. The upload must fit
the existing input limit (64 MiB); interrupted or mismatched transfers remove
temporary state. Browsers supply Content-Length automatically.

Download with `GET /api/v2/content/REFERENCE?root_id=ROOT&agent_id=AGENT`.
The reference/root/agent association must satisfy the existing content grant.
The agent may be omitted for a root grant. Reads are bounded and recheck the
grant between chunks. Content is served as an attachment with an inert media
type so uploaded HTML cannot execute on the daemon's origin. No tickets are
issued. Transfers have a separate limit of 16 concurrent HTTP requests.

## Existing human approval signatures

Human approval uses the existing enrollment and challenge flow. Sign the
SHA-256 digest produced by `protocol.ApprovalMessage` with Ed25519. Its input is
UTF-8 `"whip privileged request v2\0"`, the method, the decimal daemon generation,
a zero byte, the raw connection nonce, and the exact transmitted JSON payload
bytes, in that order. The payload is the `decision` or `command` field, without
its surrounding envelope. Verification precedes decoding that payload.

Never parse and reserialize a received payload before verification. JSON
whitespace, escaping and property order affect the signature. The generated
`signing-fixture.json` includes non-ASCII text, HTML characters and intentional
whitespace; the interoperability test verifies matching Go/WebCrypto digests
and Ed25519 signatures. Its fixed seed is test-only, not a runtime credential.

## Persistence and validation

SQLite remains in WAL mode with `synchronous=NORMAL`. Accepted commands survive
a daemon crash; this is not a guarantee against power loss. Incompatible
development databases fail with an archive/reset message. Startup never
silently removes them or changes unrelated configuration, credentials or
workspace files.

`TestV2CrossTransport*` exercises concurrent Unix/WebSocket clients, committed
admission, cross-transport retries and conflicts, reconnect snapshots, replay
boundaries, subscription exhaustion, and existing signed human identity. The
transport tests additionally cover fragmented-message limits, invalid frames,
concurrent ping/data writes, close during fragmentation, exact host/origin
checks, content grants and interrupted upload cleanup. The generated package
checks TypeScript declarations, Go wire fixtures, signature interoperability
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
subscriptions, exact-byte Go/JavaScript Ed25519 fixtures, signed human mode
changes, scoped uploads/downloads and cleanup. JSON results are written to
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
complete root metadata remains available through root views. Both page APIs
require `limit` and `max_bytes`.

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
