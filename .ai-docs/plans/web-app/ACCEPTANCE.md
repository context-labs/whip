# Web application acceptance ledger

Branch: `whip-rlm`. Evidence recorded on 2026-09-06.
The work remains open until the required release
checks below have evidence; passing an implementation test is not a scope change.

## Delivered scope

- `@whip/ui`: Base UI controls, StyleX semantic tokens, documented stories,
  self-hosted fonts, bounded read-only Markdown/code presentation, all 65 named
  TUI themes, automatic appearance and custom JSON resolution/import.
- `@whip/app`: shared React application using TanStack Router, Query, Virtual,
  Form and Hotkeys. It owns routes, drafts, preferences and view leases. The SDK
  owns the connection, commands, replay and session projections.
- `apps/web`: browser storage/clipboard/download adapters, production CSP and Vite
  build. Packaged releases embed it; `whip web` discovers an existing enabled
  listener without replacing or starting a runtime.
- Conversations, root/child input, grouped streaming, bounded history, attachments,
  exact-turn cancellation, questions/permissions, recursive inspection, attention,
  goals/schedules, budgets/context and host configuration/provider/integration UI.
- Narrow host read services for directory/theme discovery, attention and mailbox
  inspection; scoped attachment admission; multimodal generated message types;
  revision-checked history clear. Shared Go theme extraction preserves TUI behavior.

The [registry inventory](WORKFLOW-INVENTORY.md) names every operation and its web,
internal or deferred path. It is checked against the generated manifest. It does
not claim that every integration has been exercised against a real third-party
provider or OS service in every browser.

Editing, code review, terminals, arbitrary manual execution consoles, process
management, hosting/authentication, Electron packaging and custom-agent APIs stay
outside this milestone. The separate MCP execution bug was not taken over.
Executed Starlark evidence is inspectable; raw VM scratch globals are unavailable
and the UI explicitly says so.

## Automated and desktop-browser evidence

All daemon fixtures use temporary homes and fake providers. No tests use the
developer's running daemon, credentials or private session content.

| Gate | Evidence and result |
| --- | --- |
| Whole repository checks | `task check` passed: Go format/vet/whipvet/full tests, 8 protocol interop tests and drift, 156 SDK tests, independent SDK example build, application type/build and 68 tests, UI type check and 9 tests, theme drift and 3 packaging tests. Local log: `/tmp/whip-web-final-check.log`. |
| Release acceptance and races | `task acceptance` passed, including affected Go races, deterministic RLM containment/lifecycle checks, 24 built-SDK Unix/WebSocket daemon cases under the race detector, and packed SDK installation. Local log: `/tmp/whip-web-acceptance.log`. |
| Actual application browsers | Production Chromium and Firefox pass same-origin/CSP attachment, grouped streams, theme/draft reload, host completion, text/image uploads and explicit PNG decoding, two-client permission resolution, concurrent recovery storage, independent drafts, restart without repeated effects and wrong-runtime isolation. `apps/web/scripts/browser.mjs`; `/tmp/whip-web-browser-results/report.json`. |
| Routes and paused rendering | Inspector selection survives a full reload; hovering session links acquires no subscription. Chromium's simulated renderer freeze/thaw converges to the authoritative input/response without repeated execution. This is not evidence of physical OS sleep or BFCache restoration. |
| Responsive operation | Chromium/Firefox at 320/390px with touch mode: visible header controls stay within the viewport; input, scrolling, permission denial and stopping a turn submitted by another client work. Mobile emulation is not physical-phone acceptance. |
| Application accessibility automation | No critical or serious Axe violations in empty conversation, pending permission, goals/schedules inspector and phone conversation states in Chromium/Firefox. The test probe loads as an external same-origin script without loosening the production CSP. |
| Actual Safari | Safari 26.3.1 on macOS passes production-app attachment, native form submission/response, live theme selection, draft/theme reload and CSP. `apps/web/scripts/safari.mjs`; `/tmp/whip-web-browser-results/safari-report.json`. WebKit is not substituted for Safari. |
| Complete theme inventory | Deterministic Go generation/drift checks plus all 65 rendered theme Axe fixtures and 10 keyboard/focus/theme/storage/touch/Storybook interaction scenarios. UI CSP fixtures pass Chromium, Firefox and actual Safari, including custom Chroma styles and retained code selection. `packages/ui/ui-test-results`. |
| Native Safari keyboard walkthrough | Native key presses verified immediate Command-K search, command selection, input submission, permission denial and composer focus restoration, Escape focus return, and readable conversation controls at confirmed 200% page zoom. The isolated fixture closed cleanly. `/tmp/whip-web-browser-results/native-keyboard-report.json`. This does not substitute for VoiceOver speech/rotor or physical-phone testing. |
| Visual regression | Ten reviewed light/dark component baselines cover content, forms, overlays, responsive layout, pending/resolved/interrupted/reconnecting states and agent activity. Playwright comparisons pass on the macOS 26.3.1 ARM64 / Chromium 153 reference. An intentional geometry mutation produced an actual image diff, preserved the baseline hash and was followed by a clean ten-case run. `npm run test:visual -w @whip/ui`; negative evidence: `packages/ui/ui-test-results/visual-diff-proof.json`. This local gate does not claim Linux or macos-14 rasterization equivalence. |
| Installable packages | Real protocol/SDK/UI/app archives install into a clean consumer. Production and Vite development render with extracted source styles and lazy highlighting. `npm run test:packed -w @whip/ui`. |
| Full-size state and scrolling | 10,000 root messages, 100 retained children × 100 messages, 1,400,000-byte tool body, 32 drafts near 1 MiB, 16 concurrent streams. Four prepends retain the exact 56px anchor offset, selected text survives an 8,000px scroll, 20 cached recipient switches do not mix history. `apps/web/scripts/performance.mjs`; `/tmp/whip-web-browser-results/performance.json`. |

Small protocol/browser and loaded-workload latency samples are documented in
`docs/web-app.md` with sample counts and interpretation. The loaded run retained
at most 346,436 SDK payload bytes and 612 messages while inspecting one child,
returning to 512 messages after release. It rendered at most 19 transcript rows.
These counters are not a claim about total browser memory or power-loss durability.

## Final audit results

- Forty post-COMMIT-return probes reach the real SDK/app DOM at median 54.09 /
  p95 85.38 ms, with ±0.376 ms clock uncertainty. Before/after monotonic-clock
  calibration does not assume symmetric request latency. Production polling and
  durability settings are unchanged. The isolated loaded scenario and concurrent
  probe-bound/commit-identity test also pass under the Go race detector.
- Native Chromium EventTiming reports all 40 trusted keydowns: median/p95 32 ms,
  conservative 28–36 ms quantization bounds, zero censored or dropped entries.
  The keyboard-to-next-render estimate is separate from the earlier rAF proxy
  and is not physical display timing. The native report is retained at
  `/tmp/whip-web-browser-results-event-timing/performance.json`.
- Visual review fixed concatenated radio, checkbox and switch descriptions using
  an existing block style. Portable tests assert the description layout and
  retained ARIA associations. The Storybook test runner uses the supported manual
  Axe mode to avoid competing with the addon's automatic run; normal developer
  Storybook still runs its own checks and that behavior is tested.
- Application Axe testing caught a newly enabled context button fading through
  insufficient contrast. Removed opacity from the shared control transition so
  enabled controls immediately regain readable text; background and border
  transitions remain. The browser runner now bounds local command/locator waits
  and records each phase and RPC failures instead of hanging after fixture expiry.
  Chromium and Firefox pass with unchanged Axe assertions. The phone stop test
  waits for the previous turn's control to disappear, then verifies the new
  cancellation's transmitted turn ID and authoritative cancelled outcome. Final
  browser log: `/tmp/whip-web-bounded-final-browser.log`.
- Native keyboard inspection found that Commands initially focused Close and
  local request resolution left focus on the page body. CommandPicker now uses
  Base UI's explicit input focus target. Successful local answers restore the
  captured composer only if it is still mounted and focus has not moved elsewhere.
  Tests cover disappearing requests, failed answers, competing focus and recipient
  changes. Default permission scope now reads “This request only.” The expanded
  Chromium/Firefox run passed in `/tmp/whip-web-focus-browser.log`, and native
  Safari confirmed the fixes. Generic dialog focus defaults remain unchanged.
- The final serial `task check` is green. A concurrent rerun earlier hit an ACP
  test's ten-second deadline and a local Storybook navigation timeout during
  unusually slow execution; no deadlines were weakened. An intermittent isolated
  fixture startup timeout also did not recur in four subsequent diagnostic,
  normal or race starts. Its cause remains unconfirmed. Those timeout artifacts
  are retained separately from passing evidence.

## Required manual release checks still open

1. Physical iOS Safari and Android Chrome: connect through the documented trusted
   HTTPS origin; create/reopen, read/scroll, inspect a child, type with the software
   keyboard/IME, upload, answer a question/permission and stop a turn. Check phone
   keyboard resize, safe areas, touch targets and orientation. Device availability
   has been asked of the user; no result has been supplied.
2. macOS VoiceOver application flow: navigate sessions, read a
   virtualized conversation, move through dialogs/comboboxes, answer requests,
   use the command picker, recover from an error and return focus. Check browser
   zoom and reduced motion in that flow. The native keyboard/200% zoom subset
   above passes; VoiceOver speech/rotor and the full combined flow remain unverified.
   Automated ARIA and focus checks do not establish assistive-technology usability.
   Timing for enabling VoiceOver on the user's active Mac has been asked; no
   answer has been supplied yet.

These checks have not been waived. No milestone-complete claim is made while
they remain open.

## Deliberate limits and simplification

- View retention is four roots and 30 seconds after the last lease, respecting
  the daemon's subscription cap. Unobserved Query pages are discarded immediately;
  the SDK keeps bounded history. There is no second transcript/event reducer.
- Draft text has per-recipient keys and a pending-write map only. Admission checks
  allow at most 32 drafts, 256 KiB each and 1 MiB combined in a tab's current view of
  storage. Simultaneous additions by different tabs can temporarily exceed the
  aggregate; subsequent non-empty writes refuse until explicit clear/discard.
  Command recovery updates have stronger origin-wide Web Locks serialization.
- Attachments remain in the open composer, with an explicit reselect-after-reopen
  notice. They are not persisted as unsent file payloads or automatically attached
  to a later command. Provider secrets and terminal input are not draft data.
- The timeline uses one TanStack Virtual path, keyed prepend anchoring and fixed
  offset following that yields to user scrolling. Removed the alternate small-list
  renderer, manual prepend cache and duplicate resize/follow machinery.
- Removed the unused StyleX ESLint dependency. No editor, terminal, extra app-store,
  transport framework or worker dependency was added to meet an unmeasured need.
