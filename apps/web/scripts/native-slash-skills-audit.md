# Native slash-skill acceptance

The retained `slash-skills.mjs` entrypoint now runs `native-slash-skills.mjs`
against the actual protocol-v4 runtime and packaged production renderer.
It imports no retired SDK, reducer, daemon fixture or fabricated candidate list.

## Preserved scenarios and native contracts

- A provider-free, folderless draft is available through the explicit **Draft
  before connecting** action. Primary onboarding still opens first. The existing
  composer, project picker and Session options are reused; Send is disabled.
- The disposable fixture publishes two exact named roots with
  `host.skills.publish`, selects them with `host.skills.set_defaults`, and uses
  the exact shipped `coding` definition. Both calls use the actual host revision.
  There is no ambient legacy-home scan, direct configuration write or restart.
- Global completion returns exactly 100 names from both named roots. Project
  completion adds exactly 100 project names, excluding unrelated process cwd.
  These human metadata reads do not require a configured provider or create work.
- Actual metadata replies are delayed 1,500ms. Focus preloads, editing remains
  usable, and Enter during a pending lookup never submits. Filtering finds names
  past index64 before applying the32-row visual cap. Native input events retain
  next-animation-frame filtering and zero RPC/loading flashes for11seconds.
- Folder choice preserves text/caret and changes the exact native scope. A new
  folderless draft cannot reuse project candidates. Returning to the first draft
  restores its authored reference, directory and exact definition.
- Only the explicit first Send creates one root and submits the exact text once.
  An ungranted `$skill` remains literal: the first successful turn's immutable
  instruction manifest has no skill metadata or invoked body, and `skills.list`
  returns no candidates. Publication/default selection does not grant authority.
- The fixture separately creates exact standing `files.read` and named
  `skills.read` grants. A **fresh** invocation succeeds and its immutable manifest
  proves the selected skill body and named-root metadata. Accepted work is never
  repeated. Provider effects are frozen after these two deliberate turns.
- Existing-session discovery uses `skills.list`, exact selected session identity,
  and two100-record pages with the returned name cursor. This is one logical
  preload, replacing the old single `workspace.complete` response. Warm typing
  performs no reads, even after freshness expiry.
- Keyboard Up/Down/Enter, held Enter suppression, mouse selection, Escape,
  middle-caret replacement, independent menu scrolling, light/dark rendering and
 390px popup containment remain asserted. Picker interactions add no provider
  calls, submissions or page/CSP errors.

The old advertised-capability strings, `root.snapshot`, `command.submit`,
`workspace.complete`, synthetic turn runner and legacy cwd/permission arguments
are replaced by their canonical native contracts, not emulated. Host previews
are explicitly human metadata reads. Selected-session instruction authority
still comes from exact grants. Native CLI `skills publish/defaults/allow` provides
an actual setup path (prerequisite f1e5c4998); the earlier convenience gap is
closed by that independent leaf, not by this fixture.

## Bounds and lifetime

The proxy only delays genuine skill metadata replies. It has at most128 live
connections,64 held replies and64 pending identities per connection. Metadata
retention is bounded to20,000 records/8MiB; errors64/4KiB and proxy errors32/2KiB.
Replies are matched by connection plus wire identity. Closing retires both ends,
clears timers, and cannot forward a delayed reply or initiate any request.
Two deterministic lifecycle tests cover exact forwarding and retirement while
held. Fixture, browser and proxy setup/cleanup are nested under their owners.
The runtime uses disposable HOME, paths, no-auth local provider and explicit
native roots. No installed state or user browser is changed.

## Executed evidence

- `node --test apps/web/scripts/native-skills-transport.test.mjs`:2passed.
- `WHIP_SLASH_SKILLS_RESULTS=/tmp/whip-native-slash-final node apps/web/scripts/slash-skills.mjs`:
  Chromium153.0.8010.12 and Firefox155.0,14workflow groups eachPASS.
- Metadata evidence452,566/464,952bytes; zero page/CSP/proxy errors, zero overflow.
  Screenshots, exact scoped request/reply metadata and report are under the above
  disposable results directory. All owned processes joined.
- Product prerequisite b80c793cf:43focused welcome/completion tests and full
  shared-app TypeScript checkPASS. Production renderer digest
  `7a3cc9491d3fad594e5d5e30dc95c5da6731276da79262dc79421b14290c6553`.

This is actual Chromium/Firefox web acceptance. It makes no Safari, native
Desktop, signed-release, model-quality or quantitative performance claim.


## Hosted admission-readiness regression (2026-09-29)

Hosted PR276 conversation validation exposed a harness ordering error at the first
`receipts.get`: the proxy had observed the outgoing `sessions.submit` frame, but
that observation did not establish that the runtime had committed admission.
The immediate read could correctly return `NOT_FOUND`. Terminal turn inspection
and the immutable instruction-manifest assertions occur later and are retained.

The proxy now also records genuine submission acknowledgements without delaying
or retrying them. The runner waits for the exact connection plus wire request ID
and `sessions.submit` method, rejects an error acknowledgement, then uses the
existing SDK receipt wait with the original client/request identity. A queued
acknowledgement may have no turn yet; the wait observes claim and settlement
without sending. It checks the acknowledged input ID, resulting input/turn join,
exact root owner and any turn ID already present in the acknowledgement.
No `NOT_FOUND` error is swallowed and no mutation is retried. Existing count,
byte, lifetime and browser deadlines remain unchanged.

The focused regression holds the real reply boundary in the proxy double: an
outgoing request supplies no acceptance evidence, another connection's identical
wire ID cannot supply readiness, and the matching acknowledgement is forwarded
once without another submission. It fails against the preceding transport and
passes after the repair; all3 transport tests pass.

Actual repaired matrix: `WHIP_SLASH_SKILLS_RESULTS=/tmp/whip-native-slash-readiness-results node apps/web/scripts/slash-skills.mjs` passed all14workflow groups in both Chromium153.0.8010.12 and Firefox155.0. Production renderer digest remained `85b21e7e63c3226d8f683ca46cfe6e4c43ba527466a228730fb8c60f00d934d9`; no product source changed. Both browsers report zero page/CSP/proxy errors and retained metadata within the existing bound. All owned browser/runtime processes joined. This repairs the observed admission-readiness assumption; it does not claim unrelated hosted timing issues are fixed.

## Completion selection race (2026-09-29)

Hosted run36581778862, conversation job109451533933, passed the admission-readiness
boundary above, then failed the unchanged Escape assertion at native runner
line264: replacing the selected `$accept-alpine ` completion with `/accept-al`
produced `$accept-alpine /accept-al`. This is a separate product selection race.
The completion hook deferred caret restoration to an animation frame. A newer
select-all could occur while the text still matched that callback's value guard;
the callback then collapsed the newer range before replacement text arrived.

The hook now restores the insertion caret during the controlled value's layout
commit, with the existing exact scope, element, value and focus guards. It leaves
no later animation callback that can override a subsequent selection. The
pending restoration is discarded when blocked, disconnected or mismatched.
Catalog reads, keyboard filtering, submission rules and ownership are unchanged.
Two deterministic regressions fail against the previous implementation for mouse
and keyboard completion, then pass with the fix: a newer select-all survives and
replacement produces exactly `/accept-al`, without submitting. Two further
regressions prove immediate middle-of-text editing for session and global drafts.

Executed on base0265ab26cb71cf98754302f8187b5800c183832e plus this repair:

- Four focused completion suites:71testsPASS; full shared-app TypeScript check
  and `npm run pack:web`PASS. Red regression evidence is retained in
  `/tmp/whip-native-slash-selection-red.log`.
- `WHIP_SLASH_SKILLS_RESULTS=/tmp/whip-native-slash-selection-results node apps/web/scripts/slash-skills.mjs`:
  Chromium153.0.8010.12 and Firefox155.0 each passed all14 original workflow
  groups. No browser assertion, geometry check or deadline changed.
- Production renderer digest
  `3e18ccd8ca5376ea08892974771b1fff9faf6ab8cc0987483fc84656206f04f2`.
  Both reports contain zero page/CSP errors; metadata evidence is458,661 and
  464,341bytes within the existing bound. Proxy retirement checks passed and all
  owned browser/runtime processes joined.

This proves the repaired completion race locally; a fresh hosted run remains
separate evidence. It makes no Safari, signed Desktop or latency claim.
