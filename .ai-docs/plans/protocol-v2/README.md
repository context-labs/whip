# WHIP v2 protocol foundation

> Protocol 3.0 supersedes the approval-authentication portions of this historical
> plan. All connected clients may approve/deny without enrollment or signatures.
> Permission prompts/rules, internal agent/MCP authority, content grants and
> Host/Origin validation remain. See `docs/protocol-v2.md` for the current contract.


Branch: whip-rlm

## Goal

Implement the user-approved plan against 632949e: one typed JSON-RPC contract
over Unix sockets and WebSockets, full existing runtime client parity, durable
acceptance and reconnect, generated TypeScript contracts, and removal of v1.

## Non-goals

No new connection authentication, hosted relay, React UI, Electron packaging,
database compatibility, editor features, or terminal redesign. Preserve human
approval signatures, delegated MCP authority, content grants, and WAL/NORMAL.

## Design and ownership

The daemon owns execution and runtime configuration. Transport adapters own
framing and connection cancellation. Admitted commands remain supervised by
the daemon/root after disconnection. SQLite remains the only durable store;
raw transcript pagination supplies bounded views. Protocol types and operation
metadata generate the browser contract. Network serving is opt-in and bounded.

Main surfaces: internal/daemon, internal/session, internal/config, internal/tui,
cmd/whip, ACP/MCP clients, a small contracts package, and protocol documentation.

## Prior art

JSON-RPC 2.0: https://www.jsonrpc.org/specification
WebSockets: https://datatracker.ietf.org/doc/html/rfc6455
OpenCode research: /tmp/whip-opencode-client-research (shared runtime client
contracts and host-owned execution). Reuse gobwas/ws normal upgrades and
google/jsonschema-go; generate TypeScript with json-schema-to-typescript/Ajv.

## Ordered work

- [x] Typed contract, operation parity inventory, generated TS and fixtures.
- [x] Command/query lifecycle and clean schema, persistent runtime identity.
- [x] Bounded views, history revisions, replay and dynamic subscriptions.
- [x] WebSocket/HTTP adapters and opt-in trusted-network configuration.
- [x] All Go clients and provider/config parity; delete v1 and build restarts.
- [x] Transport-equivalence, race, acceptance, TS/browser, drift, latency tests.
- [x] Feature map, architecture, concurrency, protocol and network docs.
- [x] Adversarial review and final simplification.

## Validation

Exercise duplicate/conflicting submissions and acceptance/crash boundaries;
snapshot races, expired cursors and stale subscriptions; concurrent answers;
fragmentation and slow consumers; grants/signatures/MCP authority; credential
redaction and config conflicts; old protocol/database rejection without data
mutation. Run task check, affected race suites, task acceptance, npm checks and
generation drift checks. Do not claim completion with missing parity.

## Progress

Initial working tree contains pre-existing untracked .worktrees/; preserve it.
User explicitly approved implementation, including incompatible schema changes.


## Implementation record

The operation-by-operation cutover inventory is [PARITY.md](PARITY.md). The Go
registry and generated manifest cover 104 RPC/runtime operations; runtime
fixtures run every registered action through the actual Go client and handler.
The network setup and consumer contract are in `docs/protocol-v2.md`.

Clean schema 7 adds persistent runtime identity, typed command operation/outcome
metadata, history revisions, collection revisions and catalog invalidation.
No old-database migrations, legacy outcome decoder or v1 transport remains.
Normal startup rejects incompatible databases without mutating them. This work
has not opened/reset the user's runtime database or changed real credentials.

The final review corrected subscription retirement races, question/cursor
consistency, queued cancellation targeting, detached deletion ownership, retry
receipt retention, browser-manager lock ownership, catalog lost updates and
client-side model/completion assumptions. It also restored full explicit export
when UI history is bounded. Secret MCP/terminal/provider operations are ephemeral;
existing human approval and delegated MCP authority remain enforced.

## Validation record (2026-09-05)

- `task check`: passed, including format/vet/whipvet, all Go tests, TypeScript,
  Ajv request/message fixtures, Ed25519 signing interoperability and drift.
- Runtime registry and cross-transport tests cover acceptance, duplicates,
  changed-payload conflict, detach/reconnect, snapshots, paging, permissions,
  large content and subscription limits.
- Actual subprocess crash test accepts on Unix or WebSocket, performs a fake
  external effect, exits without cleanup, reopens the retained WAL, and checks
  recovery through both transports. Runtime/command identities stay stable;
  uncertain work is interrupted and its effect is not repeated.
- Real Chromium 153, Firefox 155 and Safari 26.3.1 smoke tests passed, including
  WebCrypto signatures, WebSocket operations and CORS content transfer.
- Full session/config/TUI/protocol/ACP race suites and affected browser-manager,
  provider, completion, export, admission and cancellation races passed.
- Final combined daemon/CLI/ACP race run passed (daemon 66.6 s, CLI 28.7 s).
- Final `task acceptance` passed, including the actual crash test and existing
  recursive runtime, authority, kernel and deterministic evaluation suites.

`TestV2ConcurrentAdmissionAndEventLatency` measured 48 commands across four
concurrent root agents using mixed Unix and WebSocket connections on the local
Mac: acceptance p50 1.824 ms / p95 5.871 ms; submit-to-completion-event p50
49.976 ms / p95 51.657 ms. The latter includes fake provider execution and the
retained 50 ms poll interval. These are observations, not performance guarantees.
SQLite remained WAL with synchronous=NORMAL; no stronger power-loss guarantee
was introduced.
