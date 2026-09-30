# Frontend restoration acceptance — 2026-09-29

> **Subsequent UX audit correction (2026-09-29):** the focused results below
> remain evidence, but they do not establish complete baseline UX parity.
> User review and source comparison found remaining ordinary-flow differences.
> The [desktop/web inventory](frontend-desktop-web-parity-inventory.md) records
> the completed source audit and subsequent user decisions. The provider/onboarding
> checkpoint below supersedes those screens' earlier interaction expectations.

## Provider follow-ups — user-approved corrections

Commits `32157959b` and `fb8e7e716` implement environment-provider import at host
startup, explicit provider selection with known model/effort presets, composer
selection for unknown presets, removal of the onboarding model-confirmation and
draft-bypass paths, and preservation of supported Settings reasoning effort.

| Checkpoint | Evidence and limit |
| --- | --- |
| Provider selection | 168 focused tests; app types/build; 12 provider/settings groups per Chromium and Firefox. `/private/tmp/whip-provider-onboarding-browser/report.json`, renderer `8c64f09ddbce2cd987fed41ded4653bc0e2dbcd30d993406bc03271673e4a98e` |
| Draft-bypass removal | 58 affected tests; app types/build; 14 native slash/composer groups in Chromium. `/private/tmp/whip-remove-draft-bypass-browser/report.json`, renderer `228ea6fd74d294c624affd01a6e6dfa955216e7027f0e415359ce37be0dd71a7` |
| Settings effort | 25 provider-default/model-selection tests; app types; renderer build/pack `02487f5bf1ac3432d708dc2d510196f50816b28abddd4c107430ef4b2c334994`. Supported High/Off fail before the change and pass afterward; unsupported effort resets without an early write |
| Commit gate | `task check:fast` passed. Protocol types, 18 interop tests and generated drift passed. Subsequent Settings-only edits do not change that backend/contract |

Counts overlap and must not be added. Each browser result applies to its recorded
renderer. No packaged-desktop acceptance was rerun for these follow-ups; remaining
desktop/web parity and hosted acceptance stay open. See increments 12–13 in the
[progress record](frontend-ux-restoration-progress.md) for commands and limits.

## Earlier restoration checkpoint — retained evidence

The restoration is implemented on the existing native backend/SDK. Draft
[#298](https://github.com/context-labs/whip/pull/298) integrates the tested slices
[#288](https://github.com/context-labs/whip/pull/288)–
[#295](https://github.com/context-labs/whip/pull/295) and
[#297](https://github.com/context-labs/whip/pull/297), above the approved planning
checkpoint [#287](https://github.com/context-labs/whip/pull/287).
Implementation and local acceptance are complete with the explicit limits below.
The draft stack is ready for review; this is not merge or release approval.

## Exact comparison

The reference is development `12f0ea0768b7d769765596c35c049fe80edfaeba` plus
all 14 captured refinements in [the reference manifest](frontend-ux-reference.json).
It was reconstructed and built in an isolated checkout. Reference renderer:
`16092ad369f4c920d46d3aa169bf93c670b3348bd9395dd432030d6439019e46`.
Final native renderer:
`a78d2ad301fe33eb545ff565b51b363fccb8aa68dbc9d6e28442c8ad3536c2ce`.

Existing components, styles, reading behavior and controls were retained or
selectively restored. Protocol changes live in the native owners and SDK;
there is no revived legacy reducer, compatibility backend or second transcript
store. The [current frontend guide](frontend.md) documents actual ownership.
The [increment record](frontend-ux-restoration-progress.md) preserves before/after
regressions and the validation done at each checkpoint.

## Workflow evidence

These are layered checks, not a claim that every fault was repeated in every
browser. Browser comparisons use the same visible expectations and disposable
real runtimes; lower-layer tests cover races, bounds and authority. Reports,
screenshots and failure attempts are retained locally at the listed paths.

| Plan rows | Result and reproducible coverage | Evidence |
| --- | --- | --- |
| A1–A3 | Reference/native Chromium and Firefox: configured startup, independent metadata loading, draft/submission/reload. Native independent tree/queue reads have no separate equivalent in the reference atomic snapshot. Unit fault cases additionally cover persistence failure, unknown admission and full recovery storage. | `apps/web/scripts/frontend-migration-parity.mjs`; `/private/tmp/whip-ux-parity-native-r2/report.json`; reference `whip-ux-parity-reference-r3` (A2/A3), `whip-ux-parity-reference-a1`, `whip-ux-parity-reference-a11` and `whip-ux-parity-reference-firefox` |
| A4 | Exact root approval/denial and question replies retain the original operation and choice across lost acknowledgements, query eviction and client replacement. Authored question drafts remain; explicit Check reconciles, without automatic replay. Native standing grants retain exact scope. | `permission-recovery-restoration.mjs`, `permission-requests.mjs`, `standing-grants.mjs`; 20 request regressions; `/private/tmp/whip-ux-pack6-permission-recovery/report.json` (final renderer, four fault groups per browser) |
| A5–A6 | Reference five comparison groups and native eleven lifecycle/form/fault groups in each browser. Detection has no side effects; owned/shared/external Disconnect, disable, custom routes, account cancel/expiry, atomic settings, Default/Off and real Kimi 1,048,576 setting pass. | `provider-settings-{reference,restoration}.mjs`; `/private/tmp/whip-provider-reference-comparison/report.json`; `/private/tmp/whip-ux-pack6-provider-restoration/report.json` (final renderer) |
| A7 | Readable operation subjects, truthful outcomes, ordered prose/reasoning, child isolation, completed-copy footer and unchanged reading thresholds pass in both browsers. | `/private/tmp/whip-ux-final-activity3`, `whip-ux-final-activity-firefox`, `whip-ux-final-chat-polish2`, `whip-ux-final-reading` |
| A8–A9 | Real Writing→Running→Completed preserves the article, expanded full code and selection; >16 turns and 131 actual cells preserve older windows. Exact 20 root/child Back switches and Forward restore within 2px. Native store/runtime and SDK tests cover both engines, retry/settlement/retirement, immutable reasoning and bounded body ownership. | `native-ux-executions.mjs`; `/private/tmp/whip-ux-repl-acceptance/final-executions-28b9/results.json`; `/private/tmp/whip-ux-final-main-pack6/executions/results.json` (Chromium); `executions-firefox-final/results.json` in the same directory (Firefox) |
| A9 recovery | A real 10,000-message history tests held/failed pages, exact keyboard retry, no replay, retained DOM/selection/draft/anchor and canonical Latest after eviction. Both browsers pass on the final artifact. | `/private/tmp/whip-ux-final-history-recovery2/results.json`; `native-history-recovery.mjs` |
| A10 | Timestamp, actual clipboard, draft/caret, offline copy, exact whole-group fork/import, stale confirmation refusal, active-work refusal and fresh idle rewind pass in both browsers on the final artifact. | `/private/tmp/whip-ux-existing-navigation-pack6/footer-chromium` and `footer-firefox`; `native-response-history.mjs` |
| A11 | Reference/native shell startup configuration, aliases/PATH/prompt; native terminal lifecycle, input/observer no-replay and isolated packaged-desktop clipboard/resize/tabs pass. Agent environment remains restricted. | Comparative A11 reports; `/private/tmp/whip-ux-desktop-final-acceptance/terminal`; terminal/protocol/SDK race and lifecycle tests |
| A12 | Real Projects recency and local/remote folder creation, existing tab/Details keyboard menus, compact agent settings, 320/390/530px forms, 20px fonts, themes and actual desktop 200% zoom pass. Real scrolled Projects reorder retains the same row within 2px. | `/private/tmp/whip-ux-a12-projects-final`, `whip-ux-a12-sidebar-anchor-final`, `whip-ux-a12-sidebar-anchor-firefox`, `whip-ux-a12-zoom-final-capture/report.json`; `native-projects-polish.md` |

Comparative A1–A3/A11 browser checks ran on renderer `0d021161`; A12 browser
checks ran on `3a0b77f2`. Later changes leave their checked controls intact;
final desktop/zoom, A4–A6, A10 and history recovery run on `a78d2ad3`. Exact
full digests are retained in the reports and incremental records.

`task check:product-ux-restoration` repeats the native browser fault workflows.
The retained sidebar/theme gate is `node apps/web/scripts/sidebar.mjs`; actual
macOS Electron zoom is `node apps/web/scripts/native-projects-zoom.mjs`.
Reference comparison explicitly requires the approved private overlay; it does
not silently compare against a moving development branch.

## Integration validation

These are local results. GitHub reported no hosted checks for draft #298 at
closeout. Later commits after the packaged source only update fixtures and docs;
the final renderer digest remains unchanged.


- Shared renderer: **1,480 tests / 117 files**, app TypeScript and production
  packaging pass. Existing large-chunk and jsdom scrollTo notices are recorded.
- SDK: **223 tests**, source/test types, packed consumer, browser bundle and
  real native package smoke pass. Protocol: **18 interop tests** and generator
  drift pass.
- Go: full store race suite passes. The full runtime race run passed behavioral
  cases and exposed three package-boundary imports. Those were corrected at
  their owners; the unchanged architecture gate and affected provider/account/
  configuration/RPC race checks now pass. Both-engine idle-rewind and concurrent
  admission races pass. There is no claim of a second full 600-second runtime run.
- CLI/TUI/ACP: complete `check:native-cli` passes, including compiled clients,
  SSH fixtures and vet. Agent example: **18 tests**; client example and packed
  consumer smoke/types pass.
- Mobile: types, **226 cases across the main run and the permitted storage
  rerun**, six real-backend cases, two fixture cases, both platform bundles and
  **21 Expo doctor checks** pass. No physical-device test claim.
- Desktop: final isolated unsigned package verifies; native manager/discovery,
  onboarding, ordinary and failed-turn workspace, terminal and editor IPC all
  pass. Exact command/environment/exit records and the retained application are
  in `/private/tmp/whip-ux-desktop-final-acceptance`. Clean package source
  `f91cdb610`; native runtime SHA-256
  `9133dd178f25d9697a0aaf1002e78c7843b4ea3768bd6ef6ffd9f630862bcacd`. Desktop types and 116 unit
  checks plus startup and fixture checks passed earlier in the same stack.
- Submission: both browsers pass eight queue/steering/layout groups and eight
  native failed-turn outcome groups on the final renderer. Reports:
  `/private/tmp/whip-ux-final-composer-queue/report.json` and
  `/private/tmp/whip-ux-final-turn-failures`.
- Existing navigation suites pass both browsers: snapshot refresh, session tabs
  (13 Chromium / 12 Firefox groups), New Chat (12 each), session actions (11),
  multiple hosts (9) and split workspaces (14). Evidence:
  `/private/tmp/whip-ux-existing-navigation-pack6` and its `-r2` directory.
  All six retained settings entrypoints also pass; their immutable report copies
  and hashes are in `/private/tmp/whip-ux-final-provider-settings-evidence`.
- Content: real attachment preview, scoped lazy image reads, transfer failure
  and recovery pass in Chromium/Firefox. Diagram source/copy/keyboard/worker/
  reading-anchor checks pass; the restored hover interaction and real wheel
  input are required by the fixture, without relaxing geometry assertions.

## Historical findings and explicit limits

- **NATIVE-01:** original reading-intent geometry passes in both browsers.
- **NATIVE-02:** reused tested checkpoint `fde8bd220` qualifies observers by
  document/runtime/epoch/session and preserves raw physical-socket counts.
  Its 11 ownership tests pass. The original product cause remains unproven;
  delayed socket close is not evidence of a second active observer.
- **NATIVE-03:** fixed the actual row-identity change when delayed execution
  metadata arrived. The original 20-switch cached-child/Back/Forward criterion
  passes without a history reload and without widening its 2px threshold.
- **NATIVE-04:** both isolated desktop checkpoints pass discovery/navigation.
  Logs are retained. This does not establish the cause of the earlier
  intermittent hosted `ERR_ABORTED` or reconstruct its missing archive.
- One earlier combined Firefox run passed its behavior gates but reported four
  raw connection/interrupted-navigation console messages. Its preserved report
  remains a failure. The timed replay exposed a separate test bug: a lifetime
  cap counted ordinary per-RPC sockets as active. Diagnostics now keep bounded
  active/recent samples and exact counts. Final strict Firefox replay passes all
  six gates with no console errors or evidence overflow; four document probes
  each have maximum one active session observer. The earlier four messages remain
  unattributed; the later pass does not establish their cause. A separate earlier
  Firefox activity status-label timeout after theme reload likewise remains in
  its failed report; its bounded diagnostic repeat passed without a product edit.
- Broad historical **Remember** rules are not silently mapped to native grants.
  The current exact scope remains; broader policy remains an explicit product
  decision. Missing historical reasoning cannot be reconstructed; new retained
  reasoning survives settlement/history operations within documented bounds.
- Rare truthful uncertain-outcome recovery remains, as authorized. Rewind changes
  conversation history, not workspace files. Partial-code text selection during
  append is not promised: the reference also loses that selection. Completed
  code selection/disclosure through settlement is proved separately.
- The approved reference has its own light-theme contrast and nested provider
  discard-focus defects; these are recorded comparison limitations, not reasons
  to redesign it. An obsolete reference permission fixture lacks `getSnapshot`;
  native permission authority and interaction tests are recorded separately.
  Exact reference REPL failure: `/private/tmp/whip-ux-repl-reference-results`;
  provider/permission comparison details are in the provider evidence index.
- Automated browser geometry and screenshot inspection are not a human sign-off,
  physical display/accessibility-device test, signed/Finder launch or release
  approval. No installed runtime, original development files, merge, deployment,
  release signing or notarization was changed.
