# Native REPL viewer probe

`repl-viewer.mjs` is the public entrypoint for the native production-asset probe.
It runs a disposable runtime, local streaming provider and explicitly registered
fixture executor. It never connects to an installed runtime or account.

## Retained guarantees and native evidence

| Retained scenario | Native evidence |
| --- | --- |
| 180 recorded root executions; completed/failed code, printed output, return values and execution counters | 180 actual Starlark cells, with deliberately failing arithmetic cells. Public canonical cell/result messages provide the exact expected counter and value. |
| New read-only REPL beside Chat; keyboard menu and focus; mode links; drafts and separate reading anchors | Actual production menu, URL, workspace identity and DOM assertions. REPL has no composer. |
| Back/Forward, reload and copied REPL URL | Actual browser navigation; copied document closed before returning input focus to the original document. |
| Split/move/drag, root/child/empty selection, child failures and narrow information bars | Independent immutable child definitions and actual child cells. A deliberately unreadable file has an explicit scoped delegated grant; its actual cell and operation failures are inspected only in that child. |
| 80-cell child history, exhausted pagination, sparse text-only tail, root/Chat shared pages | Explicit native `sessions.history_page` and `sessions.turns` reads with returned opaque cursors. No synthetic snapshot refresh or arithmetic on IDs. |
| Bounded retention and rendering | At most 100 records per wire page, bounded SDK transcript/execution windows, fewer than 40 mounted cell cards. Metadata captures are count/byte bounded. |
| Partial incoming code, cumulative live stdout, repeated host calls and completion | Actual split provider tool-call arguments precede a cell. Eight real prints and two real file reads precede an explicitly held executor. The committed cell replaces provisional stdout. |
| Restart restoration without replay | Crash/restart of the disposable runtime, old process identity rejection, unchanged effect count before an explicit new cell, then canonical saved-name restoration evidence. |
| Claude Code/light/dark, high contrast/print fade, narrow/mobile, accessibility | Actual viewport/media/theme changes, screenshots and WCAG A/AA Axe checks. |
| Inspection never submits or executes work | Fixed native read-method allowlist for page traffic and exact provider-effect baselines surrounding all deliberate fixture submissions. |

## Mechanisms deliberately replaced

The old harness generated cells, host calls, step counts (206/42), durations
(12/8 ms), restored-name counts and root event snapshots. This probe uses actual
accepted inputs and executions. It does not manufacture those timing/count
values or replay a retired event reducer.

The former accumulated 32→64→80-cell snapshot and 128-cell/512-message assertion
is replaced by independently bounded canonical transcript and execution windows.
An explicit older execution read replaces the turn window; it does not retain
all 80 cells at once. Exhaustion remains stable across background read refreshes.
A text-only tail does not silently scan older history.

The old one-root-subscription assertion is now at most one outstanding
`sessions.observe` per exact session owner across duplicated views. Root and
child observations have distinct owners. This does not claim one observer for
an entire recursive tree or count the SDK's retained warm leases.

## Validation

`WHIP_WEB_REPL_RESULTS=/tmp/whip-native-repl-final3 node apps/web/scripts/native-repl-viewer.mjs`
passed all ten workflow groups in Chromium 153.0.8010.12 and Firefox 155.0.
Both owned browser/runtime lifetimes joined; page errors and CSP violations were
empty. Each owner had at most one outstanding observation across duplicate views.
The 80-cell child used eight explicit older reads, across separate history and
turn windows. Root traversal exceeded 512 canonical messages without retaining
all execution bodies.

Metadata evidence retained 8,441 request records / 768,271 counted bytes in
Chromium and 7,144 / 659,986 in Firefox, below the explicit 20,000-record / 4 MiB
caps. The live-evidence screenshot was inspected: two distinct real file calls,
all eight committed stdout lines, actual counters and return value 42.

The dedicated provider fixture prerequisites (`945d6b44f`, `e0dd2fb82`) also passed
six real-process tests, including provisional-before-cell identity, joined
settlement/restart, and automatic compaction respecting a tools-free request.
Those tests are not synthetic execution-event acceptance.

Invoke the retained public entrypoint with `node apps/web/scripts/repl-viewer.mjs`
after `npm run pack:web`; `WHIP_WEB_BROWSERS=chromium` or `firefox` narrows the
matrix. No Safari, signed desktop, notarization or installed-runtime acceptance
is claimed by this probe.

## Hosted observation-lifetime diagnostics (2026-09-29)

PR #277 run 36584226063, conversation job 109460102619, failed the final Chromium
`Duplicate views must share the owner observation` assertion after the preceding
nine workflow groups passed. The assertion counts outstanding requests from
Playwright socket events across reload and runtime restart; it did not retain
the overlapping identities before throwing. The failure artifact glob also
missed this runner's previous default output directory.

The original at-most-one assertion, requests, geometry and deadlines remain
unchanged. The runner now records up to 32 raw overlap groups (at most 16 requests
each), retaining exact connection/request IDs, expected runtime/process epoch,
send and settlement times, and reply-versus-close settlement. It saves maxima
before asserting and on every failure. The default output directory now ends in
`-results`, matching the existing CI artifact glob.

A separate passive probe observes the real document's native WebSocket send,
message, error and close methods. It neither replaces frames/replies nor sends,
holds, retries or closes a connection. It records the local close call separately
from the later physical close event, and keys maxima by document, verified
runtime/process epoch and exact session owner. Its initial-connect ordinal is
diagnostic evidence of the SDK's observed initialization sequence, not a claimed
wire client ID. Same-epoch overlaps remain counted across those ordinals.
Each document retains at most 128 live connection records, 256 owner keys and 32
overlap groups; overflow is explicit. Node retains at most 32 document summaries
of 128 KiB each. Maxima are published before navigation because a pagehide binding
can be lost when its JavaScript context is destroyed. Current-document evidence
is also captured directly before the assertion and on failure.

This instrumentation distinguishes local observer retirement from socket-close
delivery and separates document/process lifetimes. It does not declare the
hosted failure a measurement error, change the shared SDK view owner, or weaken
the original assertion. A local pass alone cannot establish the hosted cause.

Validation used production base `1edc9d82343d45629cc4c85e6749d4b0976d777a`
and renderer digest
`3e18ccd8ca5376ea08892974771b1fff9faf6ab8cc0987483fc84656206f04f2`.
`npm ci --ignore-scripts` and `npm run pack:web` passed. The unchanged public
REPL entrypoint passed all ten workflow groups in both Chromium and Firefox;
all original raw owner maxima and native document/runtime/epoch/owner maxima
were one, with no page/CSP/probe faults or overflow. Evidence is
`/tmp/whip-277-repl-final-results/results.json` and
`/tmp/whip-277-repl-final.log`. Both browsers and disposable runtimes joined.

After that matrix, review tightened diagnostic-only edge handling: recording
happens only after native `send`/`close` succeeds, and the final document summary
uses the same bounded upsert as navigation publication. Seven focused lifecycle
tests pass, including unchanged native exceptions for rejected send/close,
genuine same-lifetime duplicates, different epochs, retirement and overflow.
Syntax and whitespace checks pass. Those final diagnostic edge changes were
not rerun through the whole browser matrix; they change no product code, native
request, assertion, deadline or workflow. The hosted cause remains open pending
an exact failing overlap capture.
