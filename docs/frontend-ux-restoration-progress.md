# Frontend UX restoration implementation record

> **Later audit correction (2026-09-29):** the increments below were implemented
> and tested within their recorded scopes, but the overall parity completion
> claim was too broad. The [desktop/web inventory](frontend-desktop-web-parity-inventory.md)
> records remaining differences and the user's inventory-first, case-by-case
> planning direction. Preserve the evidence; do not infer complete UX equivalence.

Status: restoration implementation complete; final local acceptance and explicit
limits are recorded in the [acceptance index](frontend-ux-restoration-acceptance.md).
The [approved plan](frontend-ux-restoration-plan.md) defines scope and A1–A12.
Entries below preserve each checkpoint’s state; later entries supersede earlier
remaining-work notes. This is not merge or release approval.

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

## Increment 8 — Projects, folders and reference polish (G6 / A12)

Integrated source leaves `d3b2bc4c6`, `0e5cf6215`, `c3192a9ce`. The remote
picker restores explicit New folder / Choose folder with native single-child
creation; Mac native pickers allow directory creation. Unified Projects groups
exact directories across hosts, preserves route reveal/collapse and pages actual
root recency using one bounded client-keyed Query window. Stable-ID Search is
unchanged; recency is explicitly advisory under concurrent activity/pinning.

Latest reference compact agent More options, natural-height dialogs, Details
overflow, close-last tab menus and ContextMenu/Shift-F10 are ported without
replacing native definition or tab ownership. Leaf Go folder/recency race checks,
SDK paging tests, app/UI/desktop types and focused UI sets pass. Chromium/Firefox
UI tab/CSP/RTL/touch scenarios and all 66 theme accessibility checks pass in the
leaf. Combined protocol18/drift, SDK build and app types pass. Combined Projects,
agent and recency tests pass (12); folder/workflow checks pass (35).

Providers/settings is draft [#294](https://github.com/context-labs/whip/pull/294).

## Increment 9 — ordered conversation and REPL handover (A7–A9)

SDK follow-up `8bde7417e` and reference-card restoration `624d18e7d` are
integrated. Writing→call→cell→result uses one stable REPL article, partial code,
the existing live clock and retained output expansion/selection. Imported
canonical results remain visible without inventing a local cell or clock.
Ordinary provisional/JSON/checkpoint banners are removed; actual unavailable or
uncertain evidence remains explicit.

Chat consumes retained presentation as a pure projection. Interleaved reasoning,
Unicode prose and calls retain slot identity after commit. Truncated/overlapping
display metadata cannot drop or duplicate canonical text/calls. Failed attempts
retain labeled partial display, outside canonical copy/history authority. The
normal older-execution window no longer produces a warning in every transcript.

Combined focused ordered-presentation, transcript, REPL, activity, footer and
history-confirmation checks pass (77 tests); app types pass. The REPL leaf tests
verify the same article and selected code through partial ID/commit/cell/result,
persisted expansion, failed reseed and imported output. Comparative browser runs
are in progress. Inspection also found an immediate source-eviction body-retention
hole; the bounded SDK fix is recorded in increment 10.

Projects/polish is draft [#295](https://github.com/context-labs/whip/pull/295).

## Increment 10 — integration and comparative acceptance

The isolated combined acceptance branch stacks on draft
[#297](https://github.com/context-labs/whip/pull/297). Browser comparison exposed
and corrected an actual Projects virtualizer render loop, same-host settings
draft loss during reconnect, narrow provider action clipping, a transient REPL
engine-label change, and conversation unmounting during host/client recovery.
ExecutionView now retains shared immutable bodies within its existing byte
budget when the transcript moves away. Custom provider lifecycle controls and
unavailable-provider filtering are restored.

Idle response Rewind exposed a backend/UI mismatch: the original native guard
required a stopped lifecycle even with no work. Rewind now checks active turns
and uncancelled input atomically with its existing revision/tail/boundary guards.
It leaves lifecycle alone. New concurrent-admission and active/queued refusal
regressions, both-engine kernel reset cases, and RPC/history race checks pass.
A later submission captures the new revision; exact retry cannot retire later
work or reset its kernel. No stop/restart sequence or mutation replay was added.

Current combined evidence:

- Renderer: final combined 1,480 tests across 117 files pass, with app types
  and the production build. Recovery/handover, exact decisions and explicit
  read-only history retry regressions are included.
- SDK: all 223 tests, source/test types and packed consumer/browser/native smoke
  pass. Protocol: 18 interop tests and generator drift pass.
- Native CLI/TUI/ACP: the complete `check:native-cli` gate passes, including real
  compiled clients and `go vet`. Agent example: 18 acceptance tests; browser/Node
  client example smoke and types pass.
- Mobile: types, all 226 UI cases across the main and sandbox-enabled storage
  rerun, six real-backend cases, two fixture cases, both platform bundles and
  all 21 Expo doctor checks pass.
- Native backend: full store race suite passes. Full runtime race suite found
  three provider-work package boundary violations; all other runtime cases pass.
  Boundary corrections now pass the unchanged architecture gate and affected
  account/provider/configuration/RPC race checks. Model/runner/providerhost/
  hostview/terminal/protocol/RPC race suites pass. Idle-rewind follow-up races
  pass; independent review found no correctness issue.
- A1/A2/A3/A11: reference and native Chromium/Firefox comparison passes. See
  `apps/web/scripts/frontend-migration-parity.md` for exact scenario coverage;
  focused fault tests supplement it.
- A5/A6: approved reference five comparison groups and combined native eleven
  fault/lifecycle/form groups pass in each browser. Both form drafts survive
  real pre-publication socket loss; accepted-but-lost replies never replay or
  falsely claim confirmation. Kimi's real 1,048,576 output setting is verified.
- A7: native activity, ordered Markdown, durable reasoning, child work and
  original 12px reading-intent assertions pass across Chromium/Firefox.
  One Firefox status-label timeout after theme reload was retained; a repeat
  with bounded response-identity evidence passes. No speculative product fix.
- Packaged desktop: package verification, native browser/daemon discovery,
  onboarding, ordinary and failed-turn workspace, terminal, and editor IPC
  all pass using an isolated unsigned bundle. These runs use renderer
  `0d021161ba140a68cdf755e7860a3d3efc90e849b1013d864af066b318afead8`;
  final refresh follows the remaining shared-renderer changes.

The original development HEAD and all 14 captured file hashes are unchanged.
No installed runtime, deployment, merge, signing or notarization occurred.

## Increment 11 — final reading and decision recovery

The exact cached-child/Back failure came from delayed execution evidence changing
an existing execute row into a differently identified activity row. Keeping its
canonical display ID fixes the original 20-switch/Forward criterion within 2px.
Projects captured a null anchor from the virtualizer's previous visible range;
using the existing complete bounded row offsets fixes actual scrolled recency
reorder, retaining the same row and the original 2px assertion.

Approval and question cards now retain exact attempted decisions across query
cache eviction and same-owner client replacement. Authored question drafts are
bounded to the four displayed cards. Lost replies require explicit read-check or
retry of the original decision. Actual pre/post-publication socket faults pass
in Chromium and Firefox. No broader permission authority was introduced.

A failed observation also disabled an otherwise valid explicit history read.
History paging now requires the current attached client and SDK-owned captured
revision; mutations still require live observation. The regression fails before
and passes after. Both browsers pass held/failed older-page recovery over 10,000
real messages with DOM/selection/draft/anchor retention and canonical Latest.

Final renderer is `a78d2ad301fe33eb545ff565b51b363fccb8aa68dbc9d6e28442c8ad3536c2ce`.
Final A4 and A5/A6 fault suites, all six retained settings suites, actual response
history actions, queue/turn-outcome/content checks and the refreshed unsigned
packaged-desktop lifecycle checks pass. The desktop bundle is retained under
`/private/tmp/whip-ux-desktop-final-acceptance`. The repeatable browser fault task
is `check:product-ux-restoration`; the acceptance index records checkpoint-specific
browser, source, native runtime and package identities.

Fixture ports preserve product assertions: tab actions use reference context
menus, Projects selectors include both host and session, stable display IDs
replace retired prefixes, and hover/wheel tests use actual reference interactions.
Observer checkpoint `fde8bd220` was reused, not recreated. Connection diagnostics
retain bounded active/recent samples and exact aggregate counts; the transport's
per-RPC sockets are not mistaken for multiple active session observers.

The final [acceptance index](frontend-ux-restoration-acceptance.md) records the
remaining parity limits and unattributed historical/intermittent test findings.
Broader old Remember scopes remain a product decision; missing old reasoning
cannot be reconstructed. The original development HEAD and all 14 overlay file
hashes remain unchanged. No installed runtime, merge, deployment, signing or
notarization was changed.

## Increment 12 — provider onboarding decisions

Implementation commit: `32157959b`.

User review approved automatic host-startup import of supported environment
providers while retaining explicit provider selection. Existing routes, including
disabled routes, win; import stores environment variable names and does not choose
a model default. The obsolete environment-candidate activation contract and its
SDK/UI paths are removed; saved account candidates remain explicit.

Selecting or completing connection to a canonical known provider applies its
preset model and effort and opens the composer. Unknown presets/custom endpoints
open the composer with a persistent provider-only draft; model selection stays
in the composer and sending waits for a model. The onboarding model-confirmation
panel and the Draft before connecting bypass/state are removed. Current
credential status copy and source badges are accepted for now.

Focused checks: 168 provider/onboarding/model/tab tests passed after the selection
change; the later bypass removal passed all 58 affected welcome/sidebar/skill
tests. App type checking and production builds passed. Provider/settings checks
passed 12 groups in each of Chromium and Firefox at renderer
`8c64f09ddbce2cd987fed41ded4653bc0e2dbcd30d993406bc03271673e4a98e`.
The bypass removal then passed 14 native slash/composer groups in Chromium at
renderer `228ea6fd74d294c624affd01a6e6dfa955216e7027f0e415359ce37be0dd71a7`.
Reports are `/private/tmp/whip-provider-onboarding-browser/report.json` and
`/private/tmp/whip-remove-draft-bypass-browser/report.json`. An initial browser
selector failed because its model was beyond the picker's initial visible list;
using the normal Search models control corrected the fixture. The passing
provider report is a rerun, not the failed attempt.

These checks cover the approved flows, not all desktop/web parity. No packaged
desktop gate was rerun for this increment. UX-17 is the next approved change:
preserve supported reasoning effort when changing the Settings default model,
resetting to Default only when unsupported.

## Increment 13 — preserve supported effort in Settings

Implementation commit: `fb8e7e716`. The user approved preserving the selected
reasoning effort when changing the Settings default model/provider, resetting
only when the new model does not support it. The form reuses the existing
catalog effort check and still saves model, effort and permissions atomically.
Explicit Off is preserved when supported; session model editing is unchanged.

The High and Off regression cases failed against the old unconditional reset.
After the one-handler change, all 25 provider-default and model-selection tests,
app type checking and the production renderer build/pack passed. Current renderer:
`02487f5bf1ac3432d708dc2d510196f50816b28abddd4c107430ef4b2c334994`.
The required `GOCACHE=/private/tmp/whip-env-import-go-cache task check:fast` gate
passed before committing the unchanged backend, including the runtime suite.
`npm run check -w @whip/protocol` passed all 18 interop tests, types and generated
drift. The focused Settings change does not rerun or extend the earlier browser
and packaged-desktop acceptance claims.
