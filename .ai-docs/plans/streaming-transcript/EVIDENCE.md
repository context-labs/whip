# Zeron-style streaming transcript — implementation evidence

**September 17 update:** streaming scroll-following was corrected after user
feedback. See [the correction and validation](SCROLL-FOLLOW.md). The spring and
performance measurements below describe the original September 16 build.

Implemented and validated on 2026-09-16 (America/Denver). The canonical ownership,
interaction and budget specification is in [the frontend guide](../../../docs/frontend.md#conversation-and-navigation-patterns).
This report records the implementation and its verification, rather than a second architecture spec.

## Delivered

- The daemon captures versioned presentation metadata in its existing turn journal,
  including exposed reasoning, ordered prose references, tool identities, host
  operations, exact file targets and typed spawned-child IDs. QuickJS and Starlark
  use the same lifecycle capture. Normal, failed and cancelled returns finalize
  the metadata; orphaned partial prose/reasoning follows the last journal record.
- Metadata is limited to 64 KiB per record, 128 parts and 128 operations per
  execution, with explicit omission markers. Large-body pages retain a compact
  identity/status envelope and authorized content handles. Raw history retains
  metadata through restart, paging, fork, rewind and compaction. Provider request
  encoding, opaque continuation and token accounting stay independent.
- Additive protocol 6.8 fields and regenerated schemas feed the SDK's existing
  execution projection. Turn, part, call and invocation scope distinguish reused
  IDs. Historical completion updates retained live rows in place. Missing or
  incomplete evidence is labeled partial; old history retains generic executions.
- Desktop and web render chronological activity trees with reasoning and individual
  operations, plus separate inline spawned-agent cards. Agent cards use the existing
  collection and typed child identity. The duplicate agent dock is removed.
  Compact, Comfortable and Detailed densities remain; bounded manual choices,
  keyboard focus and selection take precedence over automatic folding.
- Operation details retain available code/output and explicit stored-body reads,
  plus **Open in REPL**. Labels and highlighting follow the execution language.
  Mobile retains its portable conversation projection and native renderer.
- TanStack Markdown parses coalesced live updates at up to 30 Hz, reuses unchanged
  blocks and renders top-level blocks as virtual rows. Original assistant prose
  supplies the single completed-response copy action. Selected blocks preserve
  their DOM while subsequent changes wait for selection release.
- Native animations implement adaptive text fades, activity reveals, connectors,
  shimmer and folding. Reveals reserve the measured row height while clipping
  content, so staggered arrivals remain virtualized. Shared preferences and
  visibility control motion. The chat-only spring yields to user interaction and
  preserves the existing reading/history ownership; REPL keeps its scroll behavior.
- No new dependency, state store, history database or desktop renderer was added.
  Zeron's MIT notice ships in web assets and desktop third-party notices.

## Automated verification

See [machine-readable validation](evidence/validation.json) and accompanying logs.

| Layer | Result |
| --- | --- |
| Go: `internal/llm`, `internal/rlm`, `internal/session`, `internal/daemon` | Normal and race suites passed |
| Real host capture | QuickJS and Starlark start/completion, part identity, display target and durable history assertions passed under race detection |
| Protocol and SDK | Contract generation/drift checks and 334 SDK tests passed |
| Shared web app | Typecheck, production build, 655 tests in 66 suites passed |
| Shared UI | Typecheck and 14 tests passed |
| Desktop | Typecheck, 90 core tests, 92 distribution tests and startup self-test passed |
| Mobile compatibility | Typecheck and 160 tests in 25 suites passed |
| Browser reading | Chromium and Firefox anchor/prepend/output/Latest checks passed |

Nine optional desktop SSH integration tests were skipped because their fixture
helper was not configured. The staged macOS app was built and exercised; signed
release packaging/notarization was not part of this run.

New regression coverage includes metadata/provider isolation, UTF-8 ranges,
retry discard, orphaned interrupted output, parallel completion order, evicted
starts, compact content envelopes and explicit content reads, history mutations,
scoped live/history reconciliation, group/count behavior, enclosing execution
failure after successful operations, disclosure aliases, selected Markdown,
unchanged block identity, coalescing, spring convergence and bookmark aliases.

## Production renderer checks

The fixture uses an isolated real daemon and production SDK with deterministic
streams. It verifies that Go's embedded assets and Electron's staged renderer
match digest `513a1c1b58bbec2c834f51ea06a4c99d7cf16064abe9a0437945fddf4de4ecde`.
It does not connect to a user's daemon or invoke a paid upstream provider.

[Electron](evidence/electron-streaming.webm),
[Chromium](evidence/chromium-streaming.webm) and
[Firefox](evidence/firefox-streaming.webm) recordings cover restored reasoning and
inline agents, live operations, keyboard disclosure, explicit execution details,
REPL access, snapshot recovery, selection through completion and deferred folding,
128-operation trees, streamed Markdown, Unicode and highlighted JavaScript.
[Results](evidence/results.json) record each surface's assertions.

The fixture also checks reduced motion, 390/320 px layouts, light/dark themes,
increased contrast, minimum/maximum system type sizes and absence of horizontal
overflow, console errors and CSP violations. Electron additionally passes 400%
native zoom and native title-bar hit-region checks. Representative screenshots:

- [Electron activity](evidence/electron-running.png)
- [Light theme, large type](evidence/chromium-light-large-type.png)
- [Dark theme, small type](evidence/firefox-dark-small-type.png)
- [320 px pane](evidence/chromium-narrow-320.png)
- [Electron at 400% zoom](evidence/electron-zoom-400.png)

Peak mounted rows throughout the long-tree entrance and fold were **67 in Chromium,
67 in Electron and 48 in Firefox**, below the fixture's 80-row bound. The measurement
caught and led to fixing an initial zero-height stagger problem.

Keyboard interaction and screen-reader-facing roles, names, headings and disclosure
states were checked with the actual browser accessibility trees; snapshots are
retained for [Electron](evidence/electron-accessibility.yml),
[Chromium](evidence/chromium-accessibility.yml) and
[Firefox](evidence/firefox-accessibility.yml). **An actual VoiceOver/NVDA speech and
navigation session was not performed.** That remains a manual accessibility check.

## Performance comparison

The baseline uses frontend/SDK commit `f02c6cf920a237574ae03df6a221a376e16a98a8`
in a temporary worktree, with the same current Go fixture/backend. Both runs use
10,000 root messages, 100 children with 100 messages each, a 1.4 MB tool body,
a retained 128-operation tree, streamed Markdown, 16 concurrent synthetic agents,
32 near-limit drafts and 40 trusted key presses.

| Measurement | Baseline | Final renderer |
| --- | ---: | ---: |
| Event receipt → DOM, median / p95 | 21.2 / 52.9 ms | 65.5 / 80.5 ms |
| Durable commit return → DOM, median / p95 | 63.8 / 71.0 ms | 112.1 / 137.3 ms |
| Trusted keydown → next paint, median / p95 | 40 / 48 ms | 48 / 72 ms |
| Input handler → next animation frame, p95 | 31.9 ms | 37.5 ms |
| Maximum visible rows after paging | 21 | 22 |
| Maximum retained SDK payload | 407,455 bytes | 407,455 bytes |
| Final retained root messages | 512 | 512 |
| Sampled used JS heap | 19.41 MB | 19.97 MB |

The richer renderer has a measured latency cost. Its 30 Hz display coalescing and
Markdown/animation work trade some immediacy for stable presentation; these
measurements do not isolate each contributor. A preceding repeat measured event
receipt → DOM at 55.4/80.3 ms and keydown p95 at 56 ms, illustrating run-to-run
variation. The result should not be described as a performance improvement.

The SDK remains below its 8 MiB session budget and the execution-evidence limits
are unchanged. History anchors survived four prepends and 20 root/child round trips;
selection survived an 8,000 px scroll. Subscription limits and draft retention
checks passed. The 612-message peak spans the root plus one inspected child;
the root itself remains bounded to 512 messages.

Raw data: [baseline](evidence/performance-baseline.json),
[final](evidence/performance-current.json), [preceding repeat](evidence/performance-repeat.json).
Event-to-DOM excludes physical paint. Keyboard timing uses browser Event Timing,
rounded to 8 ms; the JSON retains censoring and clock-calibration details. These
are local synthetic comparisons, not production latency guarantees.

## Reproduce

From the Whip repository with Node 24 and the installed browser runtimes:

```sh
npm run check
npm run test:web
npm run check:web
node scripts/pack-web.mjs
node apps/desktop/scripts/build.mjs --renderer-ready
WHIP_WEB_BROWSERS=chromium,firefox,electron WHIP_CHAT_ACTIVITY_RESULTS=/tmp/whip-chat-activity-results node apps/web/scripts/chat-activity.mjs
WHIP_WEB_BROWSER_RESULTS=/tmp/whip-web-browser-results node apps/web/scripts/performance.mjs
go test -race ./internal/llm ./internal/rlm ./internal/session ./internal/daemon
```

All implementation changes remain local and uncommitted. Existing unrelated MCP,
runtime-guide and browser-tab work was preserved. The installed app and the user's
active daemon were not replaced or restarted.
