# Frontend UX restoration implementation record

Status: in progress. This is not a claim of complete UX parity or release acceptance.
The [approved plan](frontend-ux-restoration-plan.md) defines scope and A1–A12.

## Reference and authority

Reference: development `12f0ea0768b7d769765596c35c049fe80edfaeba` plus the
14-file overlay verified against every hash in
[frontend-ux-reference.json](frontend-ux-reference.json). Reconstructed in the
isolated `/private/tmp/whip-ux-reference` checkout. Protocol generation and
`npm run pack:web` pass; renderer artifact:
`16092ad369f4c920d46d3aa169bf93c670b3348bd9395dd432030d6439019e46`.
The original development checkout and installed runtimes are untouched.

The user authorized implementation end to end. Ordinary UX follows this exact
reference. Rare truthful recovery remains. Existing native permission scopes
remain exact; broader old Remember scopes are still a flagged parity gap, not
an implicit authority expansion. No performance campaign, merge or deployment.

## Increment 1 — opening and readiness

- Startup checks the selected provider immediately after inventory; optional
  presets, catalog, execution defaults and MCP reads warm independently.
- Pending provider readiness retains the composer and control footprint.
  Command credentials and refreshable accounts stay eligible for explicit use;
  opening the app does not run commands, refresh accounts or infer.
- A live owned session no longer waits for tree decoration to allow submission.
  Tree errors stay a scoped details error; draft, focus and live activity remain.
  Host/session identity, selected/root scope and live-observation checks remain.
- The session-opening indicator itself already matches the reference. It was
  retained; its gating was the regression.

Regression evidence: four added welcome/runtime cases failed before the change;
all 83 tests in those two files pass afterward. The held/failing tree regression
failed before the session change; all seven conversation composition tests pass
afterward. The combined opening, provider, conversation, submission and recovery selection
passes 138 tests across eight files. `npm run check:web` passes the production
renderer build and app TypeScript; the final narrow diff was rechecked with
those 138 tests and app TypeScript. The build warns about existing large chunks;
jsdom logs its existing unimplemented `scrollTo` warning. Neither is a failure.

## Increment 2 — human terminal fidelity (G5 / A11)

The human terminal manager restores the reference's full host environment and
WHIP markers. The agent manager remains filtered. Read-only output waits wake
on PTY output/exit/retirement (maximum five seconds), replacing the renderer's
fixed 250ms polling delay. Epoch checks, byte cursors, one observer, no input
replay and shell ownership remain unchanged. The actual host HOME is authoritative.

Source leaf `c19e36aff`, generated leaf `a8249937d`; integrated as `1bfb669f0`
and `373ef2d1a`. Focused Go tests and race tests pass across terminal, capability,
RPC and protocol, including real concurrent read/write and fixture zsh
profile/aliases/PATH tests. Protocol generation/drift and 18 interop tests pass.
Full SDK checks pass (206 tests and source/test types). Terminal renderer tests
pass (44); integration rebuild plus the same 44 tests pass on the combined stack.
App TypeScript passes in the leaf. Real desktop comparison remains in final A11.

Opening slice is draft [#288](https://github.com/context-labs/whip/pull/288).

## Increment 3 — approval and activity presentation (A4 / A7)

Approval and question replies keep ordinary pending controls in place. Recovery
controls appear after a failed/uncertain reply and retry only the original
choice. Root approvals show the requester and actual command/path; exact raw
evidence remains in details. Child operations use native delegation, as required
by A4. Two old native fixtures invented directly approvable child operations;
those were corrected to root operations, with separate child non-approval and
foreign-owner rejection coverage. No backend authority changed.

File/search/shell/browser/agent subjects now come from an allowlisted bounded
projection of actual arguments. Raw arguments/results move behind disclosure.
Denied, waiting and uncertain outcomes remain distinct from successful work.
Seven new product assertions failed before this change. Focused request,
activity and permission-mode tests pass (55 tests across three files), and app
TypeScript passes. Browser/keyboard comparison is still in final acceptance.

Terminal slice is draft [#289](https://github.com/context-labs/whip/pull/289).

## Increment 4 — durable presentation prerequisite (G1)

Source leaf `06ad558f3`, integrated as `844af809e`, adds schema57 nullable
presentation metadata. One bounded per-attempt accumulator supplies ordered
preview and successful settlement, retaining reasoning separately from model
parts/accounting/private continuation. Failed/cancelled/uncertain evidence lives
with the existing attempt, is paged with history, and copies into immutable
imported groups on fork. Rewind, compaction, nested fork and source deletion
preserve the relevant ownership rules. Old absent data is not reconstructed.

Full affected session/model/store/runner/runtime/protocol/RPC suites pass in the
leaf. Model/runtime presentation and observation race checks pass. Focused final
paging/migration/bounds checks and generated protocol checks pass. Tests cover
partial call identity, multiple interleaved calls, JSON/UTF-8 bounds, immutable
settlement/retry, failed evidence, restart/fork/rewind/compaction, bounded refusal
and rollback. Physical fixture recovery proves schema56 backup → schema57,
older-binary refusal, then backup restoration with identical runtime/message IDs.
Source and generated contract are separate commits. Combined-stack focused store/model/runtime/protocol presentation, observation and
history-page checks pass; generated drift/18 interop tests, SDK build and all
206 SDK tests/type checks pass. SDK projection and UI handover remain next;
this backend prerequisite alone does not close A8/A9.

Approval/activity slice is draft [#290](https://github.com/context-labs/whip/pull/290).

## Increment 5 — completed-response controls (A10)

The reference response footer again contains timestamp, copy and root response
history actions, including tool-ending responses. Offline copy remains available;
incomplete/oversize loaded responses say Copy visible response. Failed or missing
tail evidence does not authorize history changes. Rewind is disabled during root
work and rechecked if work starts while confirmation is open. Native request IDs,
captured revision/tail and exact owner remain unchanged.

Response actions resolve actual group-end messages through at most four bounded
forward pages. Imported history and sequence gaps, including values above 2^53,
are not numeric adjacency. Workspace restoration remains a separate native
effect; the confirmation does not claim that rewinding restores files.

Focused history/projection/activity tests pass (62); history-confirmation and
reading/footer DOM tests pass (69), including offline copy/time and newly active
root refusal. App TypeScript passes. Comparative browser acceptance remains open.
Durable presentation prerequisite is draft
[#291](https://github.com/context-labs/whip/pull/291).

## Increment 6 — bounded execution and presentation SDK (G1 / A8 / A9)

Chronological native cell pages and actual history timestamps support bounded
exact body hydration. ExecutionView owns retained bodies and cell/turn paging;
SessionView owns failed-attempt presentation for retained history groups. Pure
SDK display rows maintain session/attempt/slot identity through preview, canonical
call, cell and result without inventing execution authority. Imported or failed
evidence stays distinct. Missing presentation never suppresses canonical calls.

Source leaves `3361ebe9a` and `1e4250b96` are integrated with regenerated
contracts. Protocol interop (18) and generated drift pass; integrated SDK build,
source/test type checks and all 217 tests pass, including real Unix-socket tests.
Focused coverage includes more than 128 cells / 16 turns, partial-turn continuation,
late results while browsing older work, exact Unicode reads, body deduplication,
rewind/disposal and handover. Native ordinal/cursor validation tests pass in the
leaf. Renderer binding and comparative A8/A9 acceptance still follow.

Response controls are draft [#292](https://github.com/context-labs/whip/pull/292).

## Increment 7 — providers and atomic settings (G2–G4 / G10–G11 / A5–A6)

Restored focused key/account flows, Back/Cancel, detected/connected/disabled
groups, source evidence, suggested models and the reference model picker.
Detection reads do not publish a route, run credential commands or infer.
Explicit Use rereads bundled/catalog model settings, preserving the real
`kimi-k3-fast` 1,048,576 output ceiling. Disabled providers are excluded from
ordinary model choices without deleting their credentials.

Providers and Execution each have one atomic Save with their captured revision.
Default effort, goal zero/default intent, accepted attempt ranges, conversation
summary model, compaction threshold, engine and import preferences are retained.
Clean forms follow fresh host reads; edited/error forms keep draft and revision.
Disconnect only clears owned credentials; shared/external sources and partial
cleanup receive truthful outcomes. Account cancellation, guarded publication and
credential revision checks prevent stale login from republishing after removal.

Integrated backend source leaves `3b7e65b3b`, `55acbc7b5`, SDK `8fbed96e7`,
UI `69aee18f6`; duplicate startup hunks were resolved with their disabled/Default
extensions. Nine affected Go packages pass race tests in the leaf. Combined
protocol18/drift, all219 SDK tests/type checks, app types and79 provider/settings/
model/welcome tests pass. Three added startup fixtures needed the new required
disabled field; their product assertions are unchanged. Browser A5/A6 is running
with disposable credentials; no live provider inference is used.

Execution SDK is draft [#293](https://github.com/context-labs/whip/pull/293).

## Remaining work

Backend/provider/settings/terminal and durable presentation prerequisites are
being implemented in isolated branches. SDK bindings, restored ordinary forms,
activity/REPL presentation, response actions and latest reference polish follow.
A1–A12 comparative browser and desktop acceptance is still outstanding, including
the previously open NATIVE findings; unit tests alone do not close those gates.
