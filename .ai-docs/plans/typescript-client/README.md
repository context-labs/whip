# WHIP TypeScript client

Branch: whip-rlm

> Superseded approval design: protocol 3.0 removes client enrollment, signing
> keys, signatures and connection nonces. Every connected client is trusted to
> approve or deny permissions. Permission prompts/rules and internal agent/MCP
> authority remain enforced. The signed-approval descriptions and validation
> results below record the original SDK milestone, not the current requirement.

## Trusted-client permission follow-up

Protocol 3 removes approval enrollment, signing keys, nonces and the signed
permission-mode RPC. Any connected client may answer permission requests;
mode changes use ordinary durable commands. TUI/ACP startup no longer accesses
the approval keychain or asks to pair a first human. The React example exposes
Allow once and Deny without identity injection. Tool permissions, remembered
rules, content grants, budgets and delegated agent/MCP authority remain.

The shared external permission resolver retains one decision claim until its
invocation settles, preventing competing clients from both receiving successful
handoff receipts. One resolver map replaces the waiter/early-answer maps;
generic invocation cleanup covers built-ins and MCP. Receipts retain their
existing asynchronous handoff semantics, documented in the protocol reference.

Validation passed: `task check` (149 SDK tests, protocol types/fixtures/drift and
full Go checks), `task acceptance`, full tools race tests, focused client and
schema-7 reopen race regressions, 20 SDK daemon scenarios on Unix/WebSocket with
race detection, packed-package installation, Chromium/Firefox/actual Safari,
and the example's unsigned Allow/Deny/reconnect UI smoke with zero console errors.
Older protocol majors are rejected without launching/replacing a daemon.
Schema 7 and the runtime-v2 data directory remain usable without a reset.

## Goal

Implement the approved six-phase SDK plan: attach-only browser/Node WebSocket
and Node Unix clients, durable command handles and application-supplied recovery
storage, optional bounded synchronized views, content and signed approvals,
host-service parity, and a minimal React adapter and example.

The daemon owns execution, credentials, persistence and model context. No new
auth, custom-agent framework, hosting, Electron packaging, or unrelated MCP fixes.
Client IDs are namespaces, not authenticated identities. Never replay terminal
input/secrets or automatically submit offline drafts.

## Design and ownership

- Generated `packages/protocol` remains the wire source of truth. Named maps
  classify RPC/runtime methods, with standalone CSP-safe validators.
- `packages/sdk` exposes core, node, state and react entry points. Native
  transports, one connection owner, explicit disposal, stable session handles.
- Durable commands retain identity; disconnect/aborting waits never cancels work.
  Metadata-only recovery binds runtime/client/command IDs. Explicit retry only.
- Views reconstruct snapshots plus ordered events, preserve history revisions,
  paginate large collections and retain stale state during recovery. No new DB.
- Content is scoped and bounded; signed operations bind exact bytes and nonce.
- One npm workspace lockfile. Ajv/codegen, TypeScript, esbuild, browser and React
  test dependencies are build/test tools; React is optional for SDK consumers.

## Ordered delivery

- [x] 1. Typed operation maps, standalone validators, structured missing-command error.
- [x] 2. Native transport adapters and bounded connection lifecycle.
- [x] 3. Command/session primitives and metadata-only recovery.
- [x] 4. Subscription recovery, session views and session-list view.
- [x] 5. Content, approvals and host-service parity.
- [x] 6. React example, actual SDK acceptance, docs and simplification review.

## Validation

Type/registry fixtures, strict outgoing/additive incoming validation, Unix/WS
equivalence, lost acknowledgements/crash recovery/identity conflicts, ordered
snapshot subscriptions, bounds/slow consumers, approvals/content/config secrets,
Node24 and strict-CSP Chromium/Firefox/actual Safari, packed-package install,
React lifecycle, task check, affected race suites and release acceptance.
Measure admission/event delivery/recovery and request volume without changing WAL
NORMAL durability.

### Completed validation

- `npm ci` and `npm run check`: 7 protocol interoperability tests, generated
  contract drift/type checks, 154 SDK tests and React example type/build checks.
  The registry-driven suite invokes all 104 registered operations through the SDK
  using Go-produced fixtures; behavior tests separately cover lifecycle,
  permissions, content, configuration and provider-service behavior.
- `task check`: formatting, Go vet, repository checks, full Go tests and TS checks.
- `task acceptance`: existing runtime/recursive-agent/client/RLM release acceptance
  with Go race detection, 20 SDK daemon acceptance scenarios and package installs.
- `npm run acceptance`: 20 scenarios against isolated fake-provider daemons over
  Unix and WebSockets, including real lost acknowledgements and process crashes.
- `npm run test:browser`: built SDK under strict CSP in Chromium 153, Firefox 155,
  and actual macOS Safari 26.3.1. React StrictMode, Ed25519 signing fixtures and
  scoped content transfers pass. Safari is tested as a real browser, not emulated
  by Playwright WebKit. Linux CI runs Chromium and Firefox; Safari stays a macOS
  release check.
- `npm run test:example`: React application connect/create/submit, daemon crash
  and reconnect, disabled disconnected submissions, and preserved drafts. Live
  interleaved cumulative tool updates keep one row per call/output stream and
  reconstruct the same rows after reconnect; root/child SDK tests also cover
  text/reasoning/terminal deltas and stable rendering identities.
- `npm run test:package`: packed protocol and SDK archives install into a clean
  consumer, with runtime imports and TypeScript checks outside the workspace.
- Final adversarial review added regressions for initialization/close races,
  aborted waits, cancellation ordering, nonce rotation, prototype event names,
  background-tab memory bounds and exact Unix framing. No unused SDK imports or
  dependencies remain; `git diff --check` passes.

### Local example follow-up

- A real Browser reproduction found a root mailbox hot loop: failed turns left
  mail pending and every actor wake immediately retried it. Root mailbox work
  now waits for explicit inbox input after failure/cancellation/interruption,
  including across restart. Successful explicit input restores normal delivery.
- Terminal root events and interrupted-turn events include the exact turn ID.
- The example groups consecutive identical internal mailbox deliveries with a
  visible count and expandable body; canonical history is unchanged.
- Budget reservation denials identify the budget, required reservation and
  remaining capacity. The reported session had spent about $1.13, but a provider
  catalog output cap of 1,048,576 tokens required roughly $23.90 per call against
  the remaining $23.87 of its $25 limit. Limits and provider configuration were
  not changed. Per-call output budgeting remains a separate runtime follow-up.
- Browser checks covered reconnect, grouped-history expansion, an explicit
  failed retry without looping, and a fresh successful prompt with one assistant
  message, plus history pagination and another daemon restart. Focused root
  mailbox/budget/lifecycle race tests, `task check`, and `task acceptance` pass.

### Measurements

Local Node 24 fake-provider run, 32 commands across four concurrent roots with
mixed Unix/WebSocket clients. These are fixture baselines, not production model
latency claims; the process ran without race instrumentation for these numbers.

| Measurement | Median | 95th percentile |
|---|---:|---:|
| Committed acceptance | 1.92 ms | 4.13 ms |
| Command completion | 27.52 ms | 50.74 ms |
| Submission to first stream event | 27.47 ms | 50.41 ms |
| Received event to published view | 16.59 ms | 17.58 ms |

Crash to recovered view was 277 ms in the recovery scenario. The concurrency run
made 57 command-status requests and retained at most 5,623 serialized payload
bytes per view. Bounds tests separately exercise oversized histories, streaming
output and delayed notification timers. SQLite WAL / `synchronous=NORMAL` is
unchanged; no power-loss durability promise was added.

### Implementation notes

The planned protocol 2.1 `command_not_found` correction is included. Actual SDK
validation also exposed a shared daemon JSON-RPC envelope bug: some errors carried
both a partial result and an error, while nil successes omitted the result. The
common encoder now emits exactly one outcome on both transports, with focused
cross-transport coverage. No schema change or unrelated MCP work was needed.

Runtime Ajv compilation, the redundant generated schema bundle, the nested npm
lockfile and the old handwritten browser protocol harness were removed. Native
networking, fetch, AbortController and WebCrypto cover transport/content/signing;
React uses the same external store rather than a separate reducer or cache.
The example delegates first-human pairing to the existing terminal workflow and
accepts an already-paired signer from its host. No connection authentication,
daemon launching, custom-agent framework, or product UI was introduced.

## Prior art

- https://opencode.ai/docs/sdk/ — generated contract with handwritten conveniences.
- https://ajv.js.org/standalone.html — validator generation without runtime eval.
- https://react.dev/reference/react/useSyncExternalStore — immutable external views.

Update feature map, roadmap, protocol reference, concurrency/ownership docs and
SDK usage examples as part of delivery. No automatic commit or publication.
