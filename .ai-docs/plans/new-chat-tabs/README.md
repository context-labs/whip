# New Chat as a workspace tab

Branch: proposed `feat/new-chat-tabs`; researched on `codex/provider-onboarding`.

Status: Implemented and independently reviewed, 2026-09-09. Feature validation passed; latest repository-wide provider-work blockers are recorded below.

## What this does / Goal

Every explicit New session action in the shared web/Electron app opens and selects a fresh **New Chat** tab in the focused workspace pane. It contains the existing New Chat screen, including host selection, directory, permission mode, provider setup and first-message composer. Multiple unfinished chats are independent. Sending promotes the same tab into a session chat; opening a tab alone creates no daemon session.

## Non-goals

- No new tab system, dependency, daemon/protocol API, or native mobile tab redesign.
- No Settings tab (that proposal was superseded by full-window Settings).
- No new keyboard shortcut/native menu item required; existing palette actions participate.
- No provider-settings duplication, composer visual redesign, or automatic first-message send after onboarding/reconnect.
- No duplicate views of one unfinished draft in different panes in this first change.

## Research findings

References below describe the current working tree, including uncommitted provider-onboarding work; line numbers can drift. Preserve that work rather than replacing it with an older committed implementation.

| Area | Current behavior / source |
| --- | --- |
| Workspace model | `packages/app/src/session-tabs.ts:6–45` requires session identity for chat/repl descriptors; existing pane tree, 32 open views, four panes and 64 KiB layout budget are reusable. Parsing at `:119–125` rejects other kinds. Closed history retains 20 descriptors. |
| Route ownership | `packages/app/src/session-tab-routing.ts:47–98`: URL controls focused selection, saved selection is a startup hint, later `/` calls `tabs.home()`. Runtime notifications are deliberately not treated as navigation. |
| Launcher | `packages/app/src/routes/index.tsx:1–4` renders Welcome outside the workspace. `welcome.tsx:23–67` derives host/setup from global and route state; this must become panel-local input. |
| Draft collision | `packages/app/src/welcome.tsx:71–85` keeps cwd/permission in component state; prompt uses `welcomeDraftKey(runtimeId)`. `welcome-submission.ts:5–25` keys editable/frozen text, journal and running operation by host, not tab. |
| Durable first send | `welcome-submission.ts:71–175` freezes params/text and command identities before requests, preserves create/submit recovery, distinguishes unknown/absent/terminal outcomes and clears only the captured revision. Keep these guarantees. Its `:76` capacity check assumes a new session tab will be allocated. |
| Rendering | `session-tab-strip.tsx:44–69,212–232` recognizes session URLs, groups session summaries and renders session content/leases. `workspace-views.ts:7–24` assumes session-backed descriptors. |
| Existing primitives | `packages/ui/src/workspace-tabs.tsx:13–35` already accepts generic tab items. Reuse workspace layout, drag/drop, close, overflow and compact picker. |
| Entry points | `session-sidebar.tsx:76,238–239`: global and directory-prefilled links; `session-tab-strip.tsx:196`: plus; `shell.tsx:141,154`: palette. All currently navigate `/`. |
| Non-new links | `session-sidebar.tsx:71` wordmark and `conversation.tsx:94,254` fallback links are home/navigation, not necessarily fresh-draft commands. Native session links remain existing-root links (`session-tab-routing.ts:16–42`). |
| Onboarding | `welcome.tsx:36–40,90–105,142–150` supports no host, unavailable host and provider setup. New tabs must exist before a runtime connects. Provider-setup focus at `:100` uses a global selector and needs panel scoping. |

### Prior art and constraints

- `docs/frontend.md:19–32,460–520`: one owner per state, preserve drafts and unresolved work, bound retention, window-local workspace, no session cancellation on view close.
- `docs/frontend.md:798–822` documents the current first-message recovery path; update its ownership rather than bypassing it.
- `.ai-docs/plans/session-tabs/README.md:64–77` contains relevant OpenCode stable-draft/promotion prior art. Its `:152–165` explicitly chose one reusable launcher and deferred multiple drafts. This proposal deliberately supersedes that old scope, not the shipped workspace implementation.
- `docs/learnings/other-harnesses/opencode/opencode-ux.md:174` records draft retention research. Existing app persistence is the primary implementation to reuse.
- `docs/roadmap.md:110–112` lists shipped session tabs, not this new behavior. Add a separate follow-up when implementation starts; do not relabel the existing feature unshipped.
- `.ai-docs/plans/settings-workspace-tab/README.md:5–8` is superseded. Do not implement its singleton Settings behavior while generalizing descriptors.

Research evidence: `c0a6c5e4dc505ea9a355aa93a0fa1f1a` bytes 0–5548 (workspace trace); `dbd618986aa54a993421d8988beccf95` bytes 0–8192 and 8192–14452 (entry points, submission trace, prior art and tests).

## Proposed product decisions

1. **Every explicit action creates a distinct draft**, even when the current tab is already an empty New Chat. Insert after the selected tab in the focused pane; select it and focus its composer (or first required setup control). At 32 open views, report the limit without changing focus or losing work.
2. Label unfinished tabs **New Chat**; distinguish them through existing host/directory metadata in tooltips/accessible names. Reuse current tab visuals, not a new design language.
3. Preserve each draft's prompt, selected host/profile, cwd and permission across switches, moves, refresh and close/reopen within existing retention bounds. Closing is not discard and never cancels accepted work.
4. No connected host is required to open a tab. Host/profile choice is local to the draft. Provider inventory/defaults stay host-owned; credentials stay in ephemeral provider forms.
5. On first-message acceptance, replace the descriptor **in place**: same view ID, pane and order, no extra capacity. If it is still focused, replace the draft URL with the session URL. Otherwise update it silently without stealing focus.
6. Drafts can close, reopen, reorder and move to another/new split. Do not offer duplicate-view split, chat↔REPL, archive, session rename or copy-session-link until session-backed. Existing session tab actions retain their behavior.
7. Closing the final tab shows an empty workspace with a New Chat action, not an endlessly recreated replacement. A cold launch with no saved workspace may initialize one empty draft once. Home/wordmark selects a saved focused view, or uses this empty-workspace resolver; it is not an always-allocate action.
8. Settings stays full-window; New session from Settings returns to the workspace with a fresh draft. Back-to-workspace logic must understand draft destinations.

These are recommended defaults for approval, especially last-close behavior and moving rather than duplicating unfinished drafts.

## Design

### 1. Extend the existing descriptor and store

Use a discriminated union in `session-tabs.ts`; keep existing names unless a small local rename clarifies them. Do not perform a repository-wide naming cleanup.

```ts
// Sketch, not final API.
type WorkspaceTab = SessionBackedTab | NewChatTab;
type NewChatTab = {
  id: string;             // stable local view/draft identity
  kind: 'new';
  hostProfileId?: string; // may exist before runtime identity is known
  runtimeId?: string;
  cwd: string;
  permissionMode: PermissionMode;
};
// Existing session fields remain required only on SessionBackedTab.
openNew(options): NewChatTab;
updateNew(id, patch): void;
promoteNew(id, runtimeId, rootId): void;
```

Allocate no fake root IDs. Persist bounded setup metadata with the descriptor; editable text stays in the existing runtime draft store. Freeze/validate both union branches, closed descriptors and host metadata. Preserve existing v3 session layouts with additive compatible parsing or explicit migration; never discard the workspace on schema change. Promotion must be idempotent and guarded against stale results targeting a different submission.

### 2. Distinguish creation intent from navigation

Recommended route: `/new/$draftId`, a local-only draft address. Keep `/` as startup/home resolver and preserve existing session URLs. The ID is not a daemon/share-link identity and contains no prompt, credentials or cwd.

Add one app-level open-New-Chat action that allocates exactly once, applies optional host/cwd prefill, then navigates. All existing New session actions call it. Selecting a draft routes to its existing ID without allocating. Route reconciliation, StrictMode, runtime notifications, back/forward and reload must be idempotent.

A missing draft URL resolves an existing closed record where available; otherwise show a recoverable missing-draft/empty state with an explicit fresh-New-Chat action, rather than silently making a different draft. Legacy `/` links with validated host/cwd prefill are creation intents once per navigation, not once per store notification.

Preserve modified-click/new-window behavior on sidebar links intentionally: use an explicit creation-intent URL for a destination window, which allocates its own local ID once. Do not allocate in the source window on Ctrl/Cmd-click. Update `newSessionSearch` validation as needed; currently runtime-only prefill is rejected without cwd.

Generalize destination selection used by close/reopen, startup, settings-back and desktop close. Preserve route-close guards so a stale URL does not immediately reopen a closed descriptor.

### 3. Make Welcome panel-local

Refactor `welcome.tsx` into route-independent panel inputs: descriptor ID, setup values, setup callbacks and first-send operation. Route components only resolve identity. Render Welcome from the existing WorkspaceLayout content switch beside SessionContent.

Do not mirror SDK session state. Filter new descriptors out of summary grouping, root leases, missing-session badges and session RPC menus. Support local setup and disconnected hosts in each panel. Scope provider-setup focus to the invoking panel; keep global host changes from resetting another draft. Avoid keeping every hidden composer mounted just to retain state.

### 4. Give submission/recovery a draft owner

Scope editable/frozen text and running operations by stable draft identity, with runtime/client identity embedded and frozen in each submitted operation. Editable text should follow its draft when the user changes host before submission; it must not disappear into a previous-host key.

Thread draft ID through `WelcomeSubmissions.get/start/resume/finish`, command draft references and revision-aware clearing. Preserve original create/submit command IDs, transactional admission, storage-before-dispatch, immutable submitted params, explicit retry after confirmed absence, and truthful errors. Freeze host changes while acceptance is unresolved; do not silently move recovery to another host.

Move acceptance-to-promotion out of a mounted-only navigation callback into an app-owned, identity-checked completion path. If moved, find the current pane by ID. If closed, update retained closed metadata or retain a discoverable recovery record, but never reopen/focus automatically. Retire an accepted journal only after its created-session identity is safely represented in workspace/closed recovery; storage failure must remain visible and recoverable. Opening a created root during partial failure must not lose the unsent first-message journal.

Double-submit on one draft shares one operation. Two drafts on one host submit independently. Admission at the 32-tab limit succeeds for an existing draft because promotion uses its reserved slot.

### 5. Migration and bounded retention

- Import/reveal existing per-host Welcome prompts and journals once using a deterministic legacy owner/migration marker. Never attach one legacy operation to every new draft or automatically resubmit it.
- Maintain 32 open views, four panes, 20 closed descriptors and 64 KiB layout metadata.
- Preserve runtime text bounds (32 nonempty entries, 256 KiB each, 1 MiB total). Editable and frozen text compete for that budget; 32 tabs is not a guarantee that all can submit at once. Admission must fail visibly before network effects if protected text cannot be retained.
- Add explicit aggregate journal limits; proposed 32 unresolved records and 256 KiB metadata total, retaining the existing 8 KiB per-record cap. Validate this against current storage APIs in the first implementation step.
- Unresolved recovery may outlive closed-history eviction. Keep it discoverable through a small recovery list/action on empty/New Chat state; block admission rather than evict uncertain work. Do not build a general draft-management product.
- Clean up unprotected orphan draft metadata/text through existing bounded retention behavior. Do not silently erase nonempty text on close or claim durability after storage failure.

## Ordered implementation tasks

- [x] 1. Pin expected behavior in store/routing/submission regression tests; confirm current onboarding baseline and retention APIs without altering unrelated dirty changes.
- [x] 2. Add draft descriptor/store operations, serialization, bounds and legacy migration in `session-tabs.ts` plus focused tests.
- [x] 3. Make draft-scoped text/recovery identities and app-owned in-place promotion in `welcome-submission.ts`, with minimal `runtime.ts` integration where necessary. Add concurrent-draft and late-completion tests before wiring UI.
- [x] 4. Add `routes/new.$draftId.tsx` (or equivalent generated file-route convention), shared creation/selection navigation in `session-tab-routing.ts`, root resolver and `sidebar-state.ts` validation. Regenerate route output through existing tooling.
- [x] 5. Refactor `welcome.tsx`; render it in `session-tab-strip.tsx`; audit `workspace-views.ts`, session menus, compact picker, focus and settings-back consumers for discriminated narrowing.
- [x] 6. Route sidebar/global/directory plus, tab plus and palette through the shared action (`session-sidebar.tsx`, `session-tab-strip.tsx`, `shell.tsx`). Verify desktop close and modified-link semantics.
- [x] 7. Browser/desktop validation and adversarial review of recovery, persistence, capacity and close races.
- [x] 8. Update `docs/frontend.md` ownership/retention/first-message sections and `docs/features.md` behavior→code→tests; add/complete the roadmap follow-up. Mark only the old launcher's decision superseded in historical tab plan.

## Validation / acceptance

Extend existing tests rather than introducing a framework:

- `session-tabs.test.ts`: fresh IDs; mixed descriptors; selection/order; promotion at capacity; split move (no draft duplication); close/reopen; v3 compatibility; malformed/oversized storage.
- `session-tab-routing.test.ts`: all draft route lifecycle cases, startup restoration, no duplicates on repeated observe, back/forward, unknown draft, last-close, legacy creation links, session link dedup unchanged.
- `sidebar-creation.test.tsx`, `sidebar-state.test.ts`: each explicit entry point, repeated activation, host/cwd prefill and modified clicks. Palette action from chat, REPL, Settings and an existing New Chat.
- `welcome-submission.test.ts`: same-host independent drafts; same-draft double submit; frozen params/revisions; lost create/submit replies; explicit absence retry; terminal failure; stale observers; storage refusal; cross-window original-ID protection; legacy migration.
- Focused Welcome/panel tests: independent text/cwd/permission/host; no-host onboarding; provider focus scoping; tab-switch/unmount persistence; moved/background/closed promotion without focus theft; refresh/reopen.
- `workspace-views.test.ts`, `desktop-close-tab.test.tsx`: no SDK session leases for drafts; mixed-pane replacement selection; desktop Cmd-W closes draft before hiding window.
- Extend `apps/web/scripts/session-tabs.mjs` for visible New Chat selection, multiple drafts, first-send promotion, close/reopen and compact viewport behavior. Validate ARIA tab/panel associations, keyboard traversal, overflow and composer focus.
- Run existing frontend typecheck/test/build commands after implementation and the relevant browser fixture. Inspect package scripts then; no need for daemon/Go changes solely for this feature.

### Implementation decisions and validation (2026-09-09)

Implementation was explicitly approved and is implemented in the existing shared
workspace. The extensive uncommitted provider-onboarding work was preserved; no
files were staged and no commit/reset/revert was performed.

Concrete implementation choices:
- Cold empty startup uses the explicit empty state, just like closing the last tab;
  the optional implicit initial draft was omitted to avoid phantom creation.
- `ensureNew(id, options)` materializes deterministic legacy/evicted recovery owners
  without selecting or reopening closed work. Promotion and new metadata mutations
  persist before changing in-memory descriptors.
- Journal metadata uses the planned 32 records / 8 KiB each / 256 KiB aggregate;
  editable and frozen prompt copies share existing runtime text bounds.
- A focused `apps/web/scripts/new-chat-tabs.mjs` fixture extends coverage using the
  existing harness instead of enlarging the existing session-tabs script. No new
  dependency, daemon API, mobile tab system or generic UI layout change was needed.
- Unusually long encoded legacy host identities exceeding the existing 256-character
  descriptor ID bound fail visibly and retain their original recovery data.

Completed checks:
- Independent read-only adversarial review: all four original findings and the
  accepted-journal cleanup follow-on fixed, verified with **141 focused tests**.
  Regressions include numeric validated creation intent; actual fallback storage
  quota failure before/after writes; changed-text recovery after promotion;
  capacity-blocked plain legacy prompt discovery; and offline accepted cleanup.
- Full `npm run test:web` passed **47 files / 476 tests** after the original fixes.
  Latest full rerun after final cleanup tests: **479 passed / 1 failed**, solely
  `provider-connections.test.tsx:63` expecting “From OpenCode” after concurrent
  provider-onboarding changes outside this feature lane. No feature test failed.
- `npx tsc -p packages/app/tsconfig.json --noEmit`, `npm run check:desktop`,
  `npm run build:web` and `npm run pack:web` passed during feature validation.
  Final SDK rebuild → app TypeScript check → web build also passed at 04:40Z,
  resolving the transient provider discovery type errors after artifact rebuild.
- Final feature integration command (11 affected test files): **174 tests passed**
  at 04:40Z; no feature tests or typecheck blockers remain.
- `node apps/web/scripts/new-chat-tabs.mjs`: **Chromium 153 and Firefox 155 each
  passed 10 workflows**: independent setup/text plus reload, zero pre-send draft
  effects, close/reopen, draft split, mixed panes, compact picker/no overflow,
  missing-ID no allocation, final-close empty across reload, real first-send
  identity-preserving promotion, and delayed background acceptance without route,
  focus or composer-text theft. Exactly two create + two submit commands across
  two independent sends. Real daemon/provider/config RPCs use a local no-auth
  catalog and the existing synthetic fixture runner, not mocked readiness or
  acceptance responses and not paid inference. Explicit light/dark CSS backgrounds
  differ. Results and six screenshots per engine (including promoted) are at
  `/tmp/whip-new-chat-tabs-results/`. All owned servers/browsers stopped.
- `task check` passed completely at 04:16Z (Go formatting/vet/whipvet/tests, SDK,
  frontend, UI and repository scripts). The final 04:24Z rerun stopped at gofmt
  on concurrently edited `internal/config/modelsdev/catalog.go`,
  `internal/config/provider_models.go`, and `internal/daemon/provider_list.go`.
  Those provider files were not edited by this implementation lane.
- `git diff --check`: passed. No staging, commits, resets or reverts performed.

Remaining manual limitations: signed Electron, real mobile devices, VoiceOver and
live paid-provider network recovery were not exercised. Browser promotion is real
protocol execution with the existing deterministic test runner, not a live model.
