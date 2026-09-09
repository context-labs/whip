# Compact agent IDs and visible turn failures

Implemented and verified, 2026-09-08. The research and approved plan below record
the reasoning; follow `docs/frontend.md` for current frontend architecture.
The updated daemon and shared renderer are staged locally. Installation and
restarting the live daemon remain separate from this implementation.

## Implementation and validation

- New root/session, fork and child IDs use the shared 96-bit generator with
  atomic identity checks and three collision attempts. Existing IDs are preserved.
- Streaming and non-streaming requests normalize oversized cache keys at their
  shared serialization boundary. Permanent provider request errors do not
  requeue the child's identical input.
- Schema 12 adds a bounded optional `last_turn` projection, including legacy
  backfill, child terminal turn IDs, stopped-turn reconciliation and history-edit
  invalidation. The concurrent archive migration remains the schema 10→11 step.
  Generated protocol 5 contracts include the projection alongside that task's
  breaking API changes. Clients tolerate an absent optional outcome on a
  compatible protocol; protocol-major mismatch still requires daemon upgrade.
- Existing SDK snapshot/collection refresh already carries agent metadata; no
  second reducer or connection was needed. The shared chat/REPL notice and
  inspector badge use that metadata, including a scoped detail read for a
  selected agent outside the bounded first page.

Backfill deliberately leaves unavailable evidence unknown: pruned/missing
payloads, payloads exceeding 8 MiB, and root outcomes after legacy history edits
without a reliable boundary. Child history can still be recovered independently.
Future errors have a 4 KiB UTF-8 preview and a scoped full-details reference.

Validation completed:

- Focused LLM, session and daemon tests; Go race tests for all three packages.
- Combined repository `task check`, including generated contracts, SDK/app
  checks and builds; the concurrent mobile checks also passed.
- A read-only SQLite backup of the installed schema-10 database migrated to
  schema 12 and recovered the recorded provider failure for **all six original
  children**, preserving their identifiers. The source remains schema 10; the
  temporary copy was removed after verification.
- Chromium and Firefox: three themes, 390/1280 px widths, zero/existing cells,
  bounded long errors, keyboard expansion, chat, reload, fixture crash/restart
  and subsequent success. Screenshots and results are in
  `/tmp/whip-turn-failure-results`.
- Isolated Electron smoke acceptance: saved failure, reload, successful follow-up,
  host reconnection, daemon survival after GUI exit and retained tab drafts.
  This uses staged production assets with stock Electron, not signed/Finder
  installed-application acceptance. No live model calls or editor launches.
- Web assets embedded in Go and copied into Electron passed the same renderer
  manifest check: `be7376fa02f71e74262ef2ffc167a7dde4a9b4d723d15bbc0f1fab065affa75d`.
  The staged bundled daemon reports protocol 5 / schema 12.

Reproduce browser checks with `node apps/web/scripts/turn-failures.mjs` after
packing the production renderer. Reproduce desktop outcome checks with
`WHIP_WEB_TURN_FAILURE_FIXTURE=1 node apps/desktop/scripts/smoke.mjs` after
`node apps/desktop/scripts/build.mjs --renderer-ready`.

## Recommendation

Generate every new root/session, fork, and child agent ID from **12 bytes of
`crypto/rand`, encoded as 20 lowercase, unpadded base32 characters**. Use the
lowercase RFC 4648 alphabet (`abcdefghijklmnopqrstuvwxyz234567`). This provides
96 random bits; the final encoded character carries one bit of randomness.
It needs only Go's standard library.

Make each child ID independent: no root or parent prefix. Relationships already
live in `agents.root_id` and `agents.parent_id`. Preserve the invariant that a
root agent's ID equals its session ID. Names remain the primary UI identity.

Keep existing IDs unchanged. New and old formats must coexist in URLs, saved
sessions, drafts, commands, transcripts, content grants, and remote connections.
Host/runtime IDs, turn IDs, command IDs, content references, and authentication
tokens are outside this change. An agent ID is an identifier, not authorization.

Fix oversized provider cache keys independently, so existing agents benefit
without an identity migration. Also persist the latest turn outcome and expose
it in the selected agent's UI, including failures before any model output.

## Research and tradeoffs

| Option | Length | Properties | Assessment |
| --- | --- | --- | --- |
| 96 random bits, lowercase base32 | 20 | Cryptographic randomness; URL/path safe; one case | Recommended |
| 96 random bits, base64url | 16 | Equally strong randomness; case-sensitive alphabet | Shorter, but complicates case-insensitive filenames |
| Nano ID default | 21 | 126 random bits; URL-safe alphabet; configurable size | Good library, but no dependency is needed here |
| ULID | 26 | 48-bit timestamp plus 80 random bits; sortable | Longer; WHIP already stores ordering separately |
| XID | 20 | Timestamp, machine, process, and counter fields | Not 96 independent random bits; more environmental assumptions |
| UUIDv4 | 36 formatted | 122 random bits | Strong, but longer than needed |

Go supplies [cryptographic randomness](https://pkg.go.dev/crypto/rand) and
[unpadded base32 encoding](https://pkg.go.dev/encoding/base32). `rand.Text()` is
also secure, but promises at least 128 bits and permits longer output in future
versions; it is not the fixed compact format proposed here. The alternative
formats are documented by [Nano ID](https://github.com/ai/nanoid),
[ULID](https://github.com/oklog/ulid), [XID](https://github.com/rs/xid), and
[RFC 9562](https://www.rfc-editor.org/rfc/rfc9562.html).

Lowercase is deliberate: `internal/memory/memory.go` derives session memory
filenames from IDs, and macOS supports both case-insensitive and explicitly
[case-sensitive APFS](https://support.apple.com/guide/disk-utility/file-system-formats-dsku19ed921c/mac).
An alphabet using both cases would require additional filename encoding or a
different collision analysis on case-insensitive storage.

Estimated probability of at least one collision among independent random IDs,
before database checks, using `1 - exp(-n(n-1)/(2 * 2^bits))`:

| Random bits | Example representation | At 1 million IDs | At 1 billion IDs |
| --- | --- | --- | --- |
| 64 | 16 hex characters | 0.00000271% | 2.67% |
| 80 | 16 base32 characters | 0.0000000000414% | 0.0000414% |
| 96 | 20 base32 characters | 0.000000000000000631% | 0.000000000631% |

At one billion IDs, 96 bits means approximately **1 chance in 158 billion**.
Database uniqueness checks must still prevent a collision from overwriting or
attaching to another agent. These estimates assume a working cryptographic RNG;
they are not a claim of mathematical impossibility.

## Confirmed causes and implementation map

The affected session's six children all failed with the provider error:
`Invalid 'prompt_cache_key': string too long`, reporting length 82 and maximum 64.
Their retained histories contain user inputs, but no assistant or tool output.
Failures are saved as lifecycle events and parent mailbox notices.

| Current source | Finding |
| --- | --- |
| `internal/session/command.go`, `runtime.go` | Command-created sessions use 32-character hex IDs from the generic `runtimeID()` helper |
| `internal/session/session.go` | `Store.Create` and `Store.Fork` separately generate 8-character hex IDs, only 32 random bits |
| `internal/daemon/recursive_runtime.go` | Children use `rootID + ':' + 16 hex characters`, making a typical child ID 49 characters |
| `internal/daemon/recursive_runtime.go`, `internal/agent/agent.go` | New and restored children set a cache key to `rootID + '/' + childID`: 32 + 1 + 49 = 82 |
| `internal/llm/openai.go` | `Stream` defaults the request cache key from the client without limiting its length |
| `internal/llm/accounting.go` | `runAttempt` is the shared request serialization boundary for streaming and non-streaming calls |
| `internal/session/agent_turn.go` | Completion saves the turn status and error event, then returns the retained agent to `idle`; the child completion event omits `turn_id` |
| `internal/session/snapshot_view.go`, `event.go` | Terminal turns clear live presentation; agent metadata has no saved latest-turn summary |
| `packages/sdk/src/executions.ts`, `state.ts` | Failure finalizes existing execution cells but cannot explain a zero-cell failure; missing terminal turn IDs also prevent matching active-turn cleanup |
| `packages/app/src/repl-view.tsx`, `details/observation.tsx` | The REPL shows an ordinary empty state and the agent inspector has no latest-turn failure to display |

The observed gateway response establishes the 64-character limit for this
incident. Do not cite an unrelated API metadata limit as its specification.
[Provider caching guidance](https://developers.openai.com/api/docs/guides/prompt-caching)
supports keeping cache keys stable; shortening stored identities alone is not a
sufficient request-boundary fix.

## Phase 1 — Prevent the provider rejection

1. Add a small cache-key normalizer at the shared outbound request boundary in
   `internal/llm`, before serialization. Preserve empty keys and keys at or below
   64 UTF-8 bytes. Replace longer keys with the lowercase hexadecimal SHA-256
   digest of the complete original key: exactly 64 ASCII characters. Do not
   truncate the original or add a prefix to the 64-character digest.
2. Preserve explicit request-key precedence and existing defaulting behavior.
   Cover both `Stream` and `Complete`; the latter currently does not default to
   `Client.CacheKey`, and this fix need not change that behavior. CLI-provided
   keys and restored legacy child identities pass through the same boundary.
3. Preserve the existing logical root/child cache-key input. With new IDs it is
   only 41 characters; with legacy IDs it is hashed deterministically. No
   new random key per request, provider retry, reconnect, or daemon restart.
4. Correct the related inbox retry classification: a deterministic provider 400
   must not cause repeated whole-agent turns with identical invalid input.
   Use the typed HTTP error after existing agent recovery paths have run. Keep
   the established handling of transient failures and partial execution; do not
   introduce automatic replay of tools or a second provider retry mechanism.

Acceptance: an HTTP test server enforcing the observed limit accepts requests
from restored 32/49-character root/child fixtures. Verify empty, 64-byte,
65-byte, 82-byte, Unicode, explicit override, repeated, and different keys.
Verify a permanent 400 settles one child turn without requeuing that same input;
existing transient-retry tests continue to pass. No live paid model calls needed.

## Phase 2 — Unify new agent identity generation

1. Add one small `NewAgentID` helper in `internal/session/id.go`, using 12 random
   bytes and lowercase unpadded base32. Reuse it from session creation, fork,
   and daemon child admission. No ID framework or third-party dependency.
2. Replace the generation sites in `CreateSessionForCommand`, `Store.Create`,
   `Store.Fork`, and child spawning in `recursive_runtime.go`. Remove the root
   prefix from newly generated children. Leave generic `runtimeID()` and other
   uses of `randomRuntimeSuffix()` alone; they serve unrelated identities/names.
3. Use database admission as the uniqueness boundary. Check both `sessions` and
   `agents` when reserving a new identity, because sessions can exist before
   their root agent row. Keep checks/inserts atomic and existing primary keys
   intact. On an identity collision, generate again with a small bounded budget
   of three attempts, then return a clear error. Never retry arbitrary SQL,
   duplicate-name, budget, or authorization errors as identity collisions.
4. A child retry must rebuild any ID-derived node, client, and content authority
   and clean up the failed attempt. Preserve command idempotency: replaying an
   accepted create command returns its original ID, not a new one.
5. Treat IDs as opaque strings throughout CLI/TUI, SDK, URL state, mailbox and
   content access. Parent/child authorization continues using stored relations.
   Existing identifiers remain byte-for-byte unchanged and case-sensitive.

Acceptance: cover every creation path, root/session identity equality, child and
grandchild relations, forced collisions, failed admission cleanup, concurrent
creation, and command replay. Test mixed old/new trees, restore, fork, bookmarks,
draft restoration, memory filenames, and remote routing. Test format/decoding
and deterministic collisions; a giant random uniqueness test is not evidence
for the statistical guarantees and is unnecessary.

## Phase 3 — Persist the latest turn outcome

1. Add an optional `last_turn` summary to `RuntimeAgent`, backed by a nullable
   JSON projection on the `agents` row. Keep one summary per retained agent,
   containing `turn_id`, `status`, `started_at`, `finished_at`, `event_seq`, and
   an optional error preview/details reference. `turn_id` may be unknown only
   for incomplete legacy evidence. Encode event sequences using the existing
   protocol string convention.
2. Bound the inline error preview to 4 KiB of valid UTF-8, with an explicit
   truncation indicator. Preserve full details through the existing scoped
   content-reference mechanism when needed. Respect snapshot/collection byte
   budgets; do not put an unbounded error string or turn log in each agent row.
3. Maintain the projection atomically with existing turn/lifecycle events for
   root and child start, success, failure, cancellation, and interruption,
   including restart recovery and subtree stop. Reuse one typed store helper
   for projection updates. Durable events and turns retain the historical
   evidence; the projection exists to make current reads bounded.
4. Include `TurnID: commit.TurnID` in child completion events. Verify all terminal
   paths identify the turn they settle, so late completion cannot clear or
   overwrite a newer turn. Order outcomes by durable event sequence, not
   second-resolution timestamps.
5. Backfill existing agents in the schema migration from ordered lifecycle
   events, including externally stored event payloads. Pair legacy terminal
   events missing turn IDs with the preceding verified start for that root and
   agent. If pairing is uncertain, preserve the recorded failure with an unknown
   turn ID. Do not invent an execution or claim a successful turn from absence
   of evidence. Use a streaming, bounded-memory pass, never a full event scan on
   every snapshot. The six affected children must gain their existing errors.
6. Return the summary consistently through root snapshots, agent inspection,
   and paged agent collections. Update collection revisions when it changes.
   Reset the current summary on history clear/rewind as appropriate; a fork
   starts without inheriting the source agent's failure. Cover these transitions
   explicitly rather than letting an old event reappear after a reset.
7. Generate protocol schemas/types/validators from Go and apply the repository's
   compatible version/capability rules. Coordinate with the protocol changes
   already in the working tree. Clients must tolerate an absent summary from
   an older compatible daemon, representing it as unknown.

Keep agent lifecycle and turn outcome separate: `idle` can correctly coexist
with `last_turn.status = failed`. A failed turn must not make a retained agent
permanently terminal or prevent a later follow-up.

Acceptance: store and migration fixtures cover a zero-output failure, output
followed by failure, successive turns within one second, restart, cancelled and
stopped subtrees, duplicated/late events, missing legacy turn IDs, missing legacy
evidence, large errors, and failures older than the bounded presentation window.
Verify outcomes survive parent activity and mailbox acknowledgment/coalescing.

## Phase 4 — Show truthful outcomes in the shared application

1. Extend SDK state reduction and synchronized agent views to carry `last_turn`.
   Use the existing root attachment, replay, snapshot refresh, and collection
   machinery. No app-owned event reducer, raw WebSocket, or second connection.
   Ensure a selected agent outside the initial bounded page can load its summary.
2. Add a compact selected-agent turn notice in `packages/app`, shared by the
   chat and REPL views through their existing conversation composition. For a
   zero-cell failed turn, the REPL should explain the failure instead of showing
   only “No executions in loaded history.” With existing cells, preserve them
   and display the latest failure above the reader.
3. Use the agent's name, a restrained “Last turn failed” heading, the actual
   error, and expandable/copyable details. Show “before any output” only when
   evidence confirms it. Reuse UI primitives, semantic error colors, spacing,
   radii, and content-width conventions. Long provider messages must wrap and
   remain bounded at narrow widths; avoid a new stack of alerts or a modal.
4. Show a distinct latest-turn failure indication in the agent inspector/list
   while preserving its idle/running lifecycle status. On the next start, show
   the new running state; on success, remove the previous current failure notice.
   Cancelled, interrupted, disconnected, and history-load failures remain distinct.
5. Do not manufacture a Starlark execution cell for a rejected model request,
   inject a fake assistant transcript message, or retry work on page load. This
   task does not add a “Retry last turn” command with new replay semantics.

Acceptance: SDK/app tests cover live delivery, reload, reconnect, replay gaps,
selection switching, older-daemon fallback, and the real incident's empty-history
shape. Browser fixtures cover no cells, existing cells, large errors, small
windows, light/dark themes, keyboard access, and no unexpected focus/scroll jump.
Assert the missing-turn-ID fix clears the matching active turn and finalizes
existing cells without touching a newer active turn.

## Phase 5 — Validate and package the fixes together

- Run focused Go tests for LLM, session, and daemon behavior; generated-protocol
  checks; SDK/app type checks and affected tests; the focused browser workflow.
  Run repository-required checks before delivery. Use fake providers and fixture
  databases; no additional performance benchmark project is needed.
- Build the shared web renderer once. Both Go's embedded web assets and Electron
  must consume that exact artifact through existing packaging and manifest checks.
  Bundle the updated daemon with Electron; a renderer-only release cannot fix
  provider rejection or provide saved outcomes.
- Verify a staged desktop build with an isolated copied fixture database and
  mock provider, including the schema backfill, one legacy failed child, a new
  compact-ID child, and successful subsequent work. Check browser and Electron
  display the same outcome. Verify remote daemons independently: upgrading the
  local shell does not upgrade an SSH destination's daemon.
- Preserve a database backup before a future installed-daemon migration and use
  the normal release/update path. Do not assume an older binary can safely use
  a migrated database. No installed application replacement or live-agent retry
  happens as part of this research task.
- When implemented, update `docs/frontend.md` and affected SDK/daemon references
  with the outcome ownership and opaque-ID contract. Mark this plan implemented
  only after validation and distinguish staged acceptance from installation.

Phase 1 can ship independently and should land first. Phase 2 does not require
rewriting any saved identity. Phases 3 and 4 together make both old and future
failures visible; a frontend-only empty-state patch would leave reloads broken.
