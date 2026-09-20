# Workspace and overlay evidence

Date: 2026-09-17 UTC. Lane: browser-workspace. Worktree: `whip-browser-tabs`, baseline `e261d9b5139d85ade9837eedccb8e9bdd7aaa5f1`.

## Packaged-discovered restore boundary — 2026-09-17 16:10 UTC

Root's ordinary physical UI run in the signed, shipping-fuse, opt-in Beta found the first New Browser descriptor/chrome followed by `whip:browser:restore: Invalid browser request`, preventing guest presentation. Read-only tracing confirmed that `BrowserWorkspace.sync` forwarded the workspace-only `kind: 'browser'` discriminator. Native `restoreTabs` correctly accepts only `id`, `url`, `titleHint` and `environmentId`; TypeScript structural assignability had not removed the extra runtime key. The old permissive frontend native mock did not catch this boundary mismatch.

Root authorized a narrow thaw. The only production delta was explicit restore-field projection in `browser-workspace.ts`; native validation was not weakened. The workspace fixture now rejects extra restore keys. Two new regressions cover first creation → admission → restore → presentation and reload of persisted local/preview metadata, including exact wire keys and retained workspace discriminators.

- **RED before the fix:** `job-cfb3cd93`, 16:02:11–18 UTC, both new tests failed: the creation payload contained `kind`, and reload threw `Invalid browser request`.
- **GREEN after the fix:** `job-3e0ab8ac`, 16:02:36–48 UTC, app TypeScript, **46/46** affected tests (workspace24 + preview10 + provider12) and web production build passed. Existing large-chunk warning only; scoped whitespace check passed. App production was re-frozen.
- **Actual native boundary:** native lane reported `job-650daf81` passing the real BrowserWorkspace/SessionTabs renderer → production preload → IPC strict restore validator seam, including raw-discriminator rejection, restored metadata and first New Browser admission/sync/native action. Its later full-run focus assertion failed; the cause was not established, and the earlier foreground-collision attribution was retracted. Native subsequently reported the bounded instrumented rerun `job-0c5dc731` exiting zero with `NATIVE_MANAGER_OK`, including the projection seam and existing manager/focus/control regressions, without production changes. The first failure remains an unexplained flake; the rerun is development-native evidence, not packaged/compositor or manual acceptance.
- **Latest full frontend:** root reported **709/709 PASS** in `job-d6a3f073` after the fix, superseding the earlier 707-test run below. Root also reported the corrected signed package build completed at 16:06:44 UTC. Building that artifact does not itself establish that the packaged UI rerun or remaining Phase 7 gates passed.

Docs-only follow-up adds the experimental/default-OFF behavior map in `docs/features.md` and an explicitly unchecked Browser rollout milestone in `docs/roadmap.md`. Native focus, corrected packaged local/SSH behavior, security review, supported-device/accessibility and performance/rollback gates remain owned by their acceptance records, not inferred from these automated passes.

## Human-first SSH preview UI — 2026-09-17 15:28 UTC

Root approved the optional native `BrowserPlatform.createPreview` seam and delegated the human entry to this lane. `browser-preview-controls.tsx` is mounted in Browser provenance and Settings → Browser, separately from provider selection. A person chooses a connected saved SSH host, its verified runtime catalog project path, and an explicit HTTP(S) URL naming literal `127.0.0.1` or `[::1]`. Local/URL hosts, shorthand loopback aliases, hostnames, credentials, port zero and invalid ports are rejected. Projects are deduplicated from at most 64 catalog conversations per page, with visible paging. Choosing a project does **not** choose a root or grant a conversation authority.

The input dialog unmounts before native may display its trusted confirmation. Existing shared Dialog hide-ACK gating remains intact. Native owns exact live-connection verification, confirmation of the environment-wide port union, route transaction and agent invalidation. The app does not infer a route from the address or environment hint. The workspace preflights capacity, awaits inert native metadata, inserts its tab, and awaits native `admitted` before allowing its slot into presentation. Native cancellation leaves no tab; concurrent capacity exhaustion or failed route admission discards provisional workspace metadata and closes the target. Presentation suppression applies to every new admission, not only the preview callsite.

```sh
npx tsc --noEmit -p packages/app/tsconfig.json
npm run test:web -- packages/app/test/browser-preview.test.tsx packages/app/test/browser-workspace.test.tsx packages/app/test/browser-provider.test.tsx
npm run build -w @whip/web
# All PASS, job-6835d100, 15:27:08–24 UTC.
# 44 focused tests = human preview10 + provider12 + production workspace22.
# Build retains the existing large-chunk warning.
npm run test:web
# PASS 707/707 tests, 70/70 files; job-7c7e4d9a, 15:27:58–15:28:22 UTC.
# Includes root's canonical workflow inventory fix; supersedes earlier inventory failure.
```

The ten new tests include actual React controls under StrictMode, SSH-only and deduplicated project selection without an agent root, literal URL validation, dialog-unmount-before-native-call ordering, hide ACK and balanced release, disconnect/reconnect without creation, safe web absence, native cancel, capacity before/after confirmation, route-admission error cleanup and pending-admission presentation exclusion. SDK/native boundaries are mocked; these tests do not claim live SSH, real native confirmation, packaged geometry or VoiceOver acceptance. App source readiness was sent to root; native transaction readiness and combined packaged acceptance remain separately owned.

## Explicit provider UI integration — 2026-09-17 15:15 UTC

Root delegated the app provider coordinator, controls and wiring to this lane after publishing the SDK BrowserProviders API. This supersedes the earlier root-owned-controls integration gap below.

- `browser-provider.ts` holds bounded, window-local explicit root/tab/pane associations and SDK BrowserSelection handles. It does not persist grants or duplicate protocol command state. Selection offers **only the selected Browser tab**, a create profile and optional explicitly chosen SSH preview metadata. Host/root identities are never inferred from the most recent conversation. It captures the originating pane for background native admissions and rejects wrong/retired provider epochs. Repeated admission delivery cannot close an already admitted human page.
- `browser-provider-controls.tsx` uses the existing shared Dialog, Select, Checkbox, fields and theme tokens: no new visual system. Conversation access is mounted next to Browser provenance; Settings offers global association management even after the original tab is closed. The dialog starts unselected, observes connected hosts and their existing recent catalogs, and offers bounded search/paging rather than copying the session state owner. Opening it performs no bind.
- Explicit release leaves human Browser tabs open. Host disconnect cancels pending selection, releases the SDK handle, leaves a visible unavailable state and never reselects on reconnect. SDK revocation uses the SDK's onError observation. Historical-module failures expose fresh-conversation Browser v1 help; no authority migration exists.
- `HostConnections` advertises `browserProvider` only when the optional native agent bridge exists. `AppRuntime.browserAssociations` owns wiring and disposal independently of ordinary human Browser creation.
- Native/root-approved SSH project isolation metadata uses bounded absolute catalog cwd prefixed with `cwd:`. Native separately verifies the exact saved host, runtime and live SSH generation. Offering metadata never prepares network routes or grants agent authority, and URL hosts cannot substitute for SSH. Human-first SSH preview route bootstrap remains a separately acknowledged native API gap, not silently implemented using an environment hint.

Validation executed in the authorized worktree:

```sh
npm run test:web -- packages/app/test/browser-provider.test.tsx
# PASS 12/12, job-89781845, 15:14:19–23 UTC.
# Eight coordinator tests + four actual controls fixtures: explicit root, hide ACK,
# historical-root errors/help, safe disabled web, disconnect/reconnect, late binding,
# one-tab offer, captured-pane admission, dedupe, revocation, release and SSH metadata.
npx tsc --noEmit -p packages/app/tsconfig.json
# PASS, job-c9ffd7af TypeScript phase, started15:14:42 UTC.
npm run test:web
# 696 PASS / 1 FAIL, 68/69 files, job-c9ffd7af, finished15:15:17 UTC.
# Only failure is root-owned canonical workflow inventory missing four new Browser RPCs:
# browser.command.result / browser.provider.bind / browser.provider.event / browser.provider.unbind.
npm run build -w @whip/web
# PASS, job-8e4da53f, 15:15:57–15:16:01 UTC; existing large-chunk warning only.
```

These fixtures exercise real React controls with SDK/native seams mocked. They do not establish a real daemon→SDK→native integrated approval run, packaged UI geometry, VoiceOver or connected SSH production acceptance. Root owns that combined acceptance and canonical documentation updates.

## Production workspace milestone — 2026-09-17 06:26 UTC

**Status: production renderer integration implemented, typechecked and tested; not packaged/native end-to-end, VoiceOver, real-browser, or connected SSH acceptance.** Root-approved `contracts.md` supersedes historical proposals below. No commits were made by this lane, and no canonical docs or schema files were edited.

### Shipped surfaces

- Optional `AppPlatform.browser` and `BrowserWorkspace` coordinate native snapshots, target epochs/generations, idempotent lazy registration, renderer admission before native ACK, provisional rollback, close cancellation, balanced overlay holds, stale snapshot rejection, fractional CSS geometry, four presented slots, and same-generation focus return. Provider pages require explicit `runtime.browser.admit(page, target, paneId, options)`; unknown snapshot entries never silently enter the workspace. Its default background insertion does not change pane focus/selection.
- SessionTabs persists inert Browser address/title/environment-hint metadata in v3, enforces eight newly opened Browser tabs and existing 32-total/4-pane limits, retains valid restored Browser descriptors, reopens with a fresh identity, and keeps an immutable pre-browser snapshot. A separate bounded address-only recovery copy preserves downgrade orphans even if a new Browser is opened before Restore. Failed native admission does not leak to closed history or recovery. Forget explicitly clears closed addresses/recovery, not open tabs or cookies.
- Browser route, mixed-tab rendering, kind/provenance labels, New Browser controls/command, pane moves/splits, Duplicate, close/close-others cancellation, reorder, picker, reopen, unavailable web views, settings recovery, and daemon-free create/navigate are integrated with existing styling and ownership.
- Browser toolbar includes validated address, history, loading/stop/reload, find, copy, external open (never for SSH hints), zoom, page developer tools, profile-clear confirmation, explicit errors and unavailable/retry UI. Native focused-page events update pane focus; bounded guest shortcuts are handled without arbitrary command dispatch. Provider association UI remains a root-owned integration through `attachmentControls?: ReactNode`.
- Shared native-hide ACK gating covers Dialog/AlertDialog, menus/submenus/context menus, Popover/CommandDialog/Drawer, Select/Combobox, theme popover, Tooltip, Toast and drag/drop previews. Default-open and StrictMode acquire/release, close transitions, nested holds, fail-closed behavior and absent-capability synchronous behavior have executable coverage. The coordinator defers explicit page-focus requests until the final overlay hold is released.

### Executed validation

```sh
npx tsc --noEmit -p packages/app/tsconfig.json
# PASS, including latest source polish (job-b5a6e38d TypeScript phase, 06:25:22–26 UTC).
npx vitest run --config apps/web/vitest.config.ts packages/app/test/browser-workspace.test.tsx packages/app/test/session-tabs.test.ts
# PASS 71/71 (22 production Browser + 49 existing SessionTabs), job-08933429, 06:23:28–38 UTC.
npx vitest run --config apps/web/vitest.config.ts
# Earlier full run PASS 676 tests / 68 files before additional six Browser tests.
# Latest job-f97e8fe9: 681 PASS / 1 FAIL, 67/68 files, 06:25:56–06:26:18 UTC.
# Sole failure: workflow-inventory.test.ts canonical documentation inventory lacks newly registered
# rpc:browser.command.result, rpc:browser.provider.bind, rpc:browser.provider.event.
# Root notified; canonical inventory is not this lane's edit scope.
npm run build -w @whip/web
# PASS job-3fc46efd, 06:26:33–36 UTC, latest source; existing large-chunk warning only.
# Earlier PASS job-d9c56a45, 06:12:05–08 UTC.
```

The daemon-free shell fixture renders the actual application with a native-platform mock and no started execution host: create → route → navigate; native beforeunload cancellation preserves the tab; web keeps an unavailable address without offering Browser creation; settings controls cannot become interactive before native hide ACK. Other tests cover UTF-8 URL and codepoint title caps, unsafe schemes/credentials, snapshot/generation staleness, provider originating-pane rejection, ACK rollback, non-destructive downgrade restore, single page identity through splits, native geometry, nested focus return, actual default-open Dialog behavior and transition/portal lifetimes.

**Remaining acceptance / integration:** root must mount provider-association controls in both Browser route and split-pane BrowserView callsites and use explicit background admission; canonical workflow inventory needs the three protocol operations above. Actual packaged app overlays/focus/reparenting across fullscreen/resize/zoom/sidebar/sidebar-to-Browser/pane transitions, VoiceOver, real multi-display geometry, connected SSH preview browsing and remote OAuth have not been exercised by this renderer lane. Native sibling separately reported actual Electron manager focus/shortcut tests; those are not a substitute for integrated app acceptance.

## Historical Phase 0 evidence (before production approval)

**Status: runnable helper/UI-fixture proof and contract proposal only. No shared production file changed. This is not native, real-browser, packaged, VoiceOver, SSH, or Phase 2 acceptance.** Root approval is still required before treating the proposals below as frozen contracts.

## Executed evidence

Owned files:

- `packages/app/test/browser-spike-contract.ts`: allowlisted descriptor parsing, CSS-to-DIP clipping sketch, balanced overlay holds, complete presentation revision receiver.
- `packages/app/test/browser-spike-workspace.test.tsx`: nine executable tests, including a React StrictMode approval-control fixture with a delayed native-hide acknowledgment.
- This document.

Commands (executed from the authorized worktree):

```sh
npm run test:web -- packages/app/test/browser-spike-workspace.test.tsx
# 1 file, 9 tests passed; 2026-09-17 05:33:13 UTC, 1.44 s
npm run test:web -- packages/app/test/browser-spike-workspace.test.tsx packages/app/test/session-tabs.test.ts packages/app/test/workspace-views.test.ts
# 3 files, 61 tests passed (9 spike + 49 existing SessionTabs + 3 existing WorkspaceViews)
# 2026-09-17 05:34:35 UTC, 1.30 s
```

The combined command was repeated after aligning URL/title fixture limits with baseline §7: 3 files / 61 tests passed again at 2026-09-17 05:44:39 UTC (1.12 s). All three owned jobs exited normally. No server, native window, persistent profile, SSH resource or background process was created by this lane. No dependencies or generated schemas changed.

What these checks establish:

1. Prototype descriptor serialization preserves an unavailable SSH environment reference, works without any browser platform, strips live grants/controller/native partition data, and rejects unsafe/oversized restore URLs.
2. The existing positive `isSessionTab` guard excludes the proposed Browser kind. Existing draft movement and session lease tests remain green.
3. Prototype geometry uses renderer zoom, not DPR, clips to content bounds, rounds inward, and rejects invalid/empty/offscreen geometry.
4. Complete presentation snapshots are bounded to four unique visible slots; stale revision and renderer epoch messages do not resurface pages.
5. Nested overlay holds release only when their final unique owner exits, tolerate duplicate cleanup, cap concurrent owners, and retain no owners after 2,000 completed cycles.
6. StrictMode setup/cleanup/setup cannot reveal approval controls on an old acknowledgment. The fixture renders its Allow once button only after the active hide acknowledgment and releases its final hold on unmount.

What is **not** established: the production `SessionTabs` parser does not yet support Browser; no production overlay wrapper is wired; the receiver and geometry are sketches, not Electron code. Existing v1/v2/v3 migration tests passing does not establish Browser migration. DOM/jsdom assertions cannot establish compositor stacking, focus, native dialogs, beforeunload, resize latency, screenshot continuity or packaged behavior. The native lane owns the real compositor proof.

## Proposed minimal persisted descriptor

```ts
interface BrowserTab {
  readonly id: string;                    // same opaque ID as the native inventory
  readonly kind: 'browser';
  readonly url: string;                   // last committed address, not address edit draft
  readonly titleHint: string;             // bounded, plain text
  readonly environmentId?: string;        // absent = This Mac; native-owned SSH restore reference
}
```

`environmentId` is a restoration hint, **not** an SSH master handle, Chromium partition name, approved host identity, port list or route grant. A missing environment remains explicitly unavailable; it never converts to local browsing. The native environment owner resolves and verifies host/project identity and obtains any new consent. Persisting the native partition itself is forbidden. If root prefers a structured environment reference, freeze that before production parsing changes.

Do not add `runtimeId`, `rootId`, `agentId`, `attachmentId`, controller state, provider epoch, document/native generation, grant, cookies, navigation history, active debugger, or find/address drafts to this descriptor. Session association is a separate current grant. Deleting a conversation, closing a chat view, changing hosts or revoking an agent does not delete the person's browser tab.

Use the existing pane tree with the plan's non-destructive v4 migration, preserving v1/v2/v3 records and downgrade recovery. Preserve 32 total tabs/4 panes/20 closed/64-KiB aggregate limits, with at most 8 Browser descriptors; refuse overflow visibly rather than truncate valid metadata. Add an allowlisted Browser parse branch and no second workspace store. Browser-less web must parse and round-trip Browser exactly like desktop. All already-supported chat/REPL/trace/draft/terminal fields retain their current validation. Prototype URL/title limits follow §7: 8,192 UTF-8 URL bytes and 128 title codepoints. The lane's earlier keep-v3/32-Browser suggestion was withdrawn at 05:43 UTC after re-reading baseline constraints.

Restore open descriptors into **native metadata inventory only**; load a page lazily when presented or explicitly requested. The native instance and document generation are fresh after restore even when its persisted tab ID is retained. Restore neither attachment nor preview route authority. Reopen/Duplicate make new page IDs with the saved address/environment, never a second view of one WebContents or a resumed controller.

## Optional platform and native presentation contract

Browser stays out of SDK session truth. `AppPlatform.browser?: AppBrowser` is optional; the ordinary web adapter omits it. Existing Electron transport remains behind the narrow serialized bridge; no Electron types/imports enter app or UI.

The following presentation shape was agreed directly with browser-native; root must still approve:

```ts
type BrowserPresentation = {
  epoch: string;                           // issued by main for this app renderer/window
  revision: number;                        // monotonically increasing safe integer
  blocked: boolean;
  slots: readonly {
    tabId: string;
    slotId: string;                        // presentation identity, not grant or tab lifetime
    bounds: { x: number; y: number; width: number; height: number };
  }[];
};
// Bounds are viewport CSS pixels below toolbar, not desktop/screen coordinates.
present(presentation: BrowserPresentation): Promise<void>;
```

Rules:

- One complete per-window snapshot; at most four slots and one slot per tab. Main validates sender/window/epoch, bounds and ownership, rejects old revisions and unowned IDs, and clips to content-view bounds. Main converts CSS edges using the trusted app `webContents.getZoomFactor()`, never `devicePixelRatio`. Inward edge rounding avoids covering the adjacent pane or toolbar.
- Promise resolution acknowledges that hide/show/bounds were applied. Hiding cannot wait for an ordinary debounced geometry update. Showing after an overlay closes includes a new measurement, not old pre-overlay bounds.
- Empty slots on Settings, unsupported/missing route, document hidden, layout unmount/error boundary, renderer reload/crash, or adapter disposal. Renderer cleanup never calls native tab close. Main independently hides on privileged-renderer navigation/crash/window destruction, so a lost cleanup message is not trusted.
- Browser content measures its actual native viewport with `getBoundingClientRect`, observes resize and scroll/visual-viewport/window changes, and deduplicates the last submitted snapshot. Existing `WorkspaceLayout` keeps stable content DOM siblings when moving panes; reuse that model. No polling transcript/session data for human pages.
- Outgoing ownership cleanup must be immediate. If presentation registrations use a Map, cleanup must compare its unique registration token before deleting; old component cleanup must not delete a new owner of the same tab/slot.
- Main guest focus emits the tab ID. App verifies it is currently visible and focuses its owning pane/route; DOM `onFocusCapture` never observes focus inside a WebContentsView.

Provisional `AppBrowser` sketch recorded before the native lane's detailed API. **Superseded for method spelling by the 05:40 UTC cross-lane proposal below; neither is frozen until root approval.**

```ts
interface AppBrowser {
  getSnapshot(): BrowserSnapshot;           // bounded, main-owned per-tab state cache
  subscribe(listener: () => void): () => void;
  restore(tabs: readonly BrowserTab[]): Promise<void>; // metadata only; no implicit navigation
  create(input: { url: string; environmentId?: string }): Promise<BrowserPageState>;
  close(tabId: string): Promise<'closed' | 'cancelled'>;
  navigate(tabId: string, url: string): Promise<void>;
  back(tabId: string): Promise<void>;
  forward(tabId: string): Promise<void>;
  reload(tabId: string): Promise<void>;
  stop(tabId: string): Promise<void>;
  focus(tabId: string): Promise<void>;
  present(input: BrowserPresentation): Promise<void>;
  // Exact find/zoom/DevTools/profile-clear and on-focus/on-shortcut types freeze with native lane.
}
```

`BrowserSnapshot` needs the current native-issued epoch and bounded page states. `BrowserPageState` needs tab ID, native instance ID, event revision, document revision, committed URL/title, loading/back/forward state and explicit ready/restored/crashed/unavailable/closed state. These are observations, not grant authority, and are never persisted wholesale. No arbitrary IPC method/evaluate/partition/path fields belong in the human UI interface.

Creation/close ordering matters more than method spelling: main publishes state before returning a created ID; workspace admission is acknowledged before a model-facing open succeeds. Main may not silently create a 33rd page. On beforeunload cancellation, keep the descriptor selected and alive. Remove it from `SessionTabs` only after native close acknowledgment. If close outcome is unknown, reconcile state; do not claim it closed or automatically retry a mutation. Browser-less web can deliberately close its unavailable descriptor without pretending to destroy a native page on another device.

### Detailed native API response (05:40 UTC, awaiting root freeze)

Workspace accepts native's object-argument v1 bridge proposal: `snapshot(): Promise<BrowserSnapshot>`; `restore({epoch,tabs})`; `create({epoch,url,environmentId?})`; `admitted({epoch,tabId,generation})`; agreed `present`; `navigate({epoch,tabId,generation,url})`; target-bound `history({...,direction})`, `reload({...,ignoreCache?})`, `stop`, `focus`, `find({...,text,forward?,findNext?})`, `stopFind`, `zoom({...,factor})`, `devTools({...,open})`; `close({...}): Promise<{status:'closed'|'cancelled'}>`; `onEvent(listener): unsubscribe`. Events are full revisioned snapshots, focused tab/generation, fixed shortcut enum, and find result. A renderer adapter may expose the native observations through a normal external store; it does not create another inventory owner.

**05:43 UTC correction:** native correctly pointed out that baseline §7 proposes eight Browser descriptors within the 32-tab total, not 32 Browser descriptors. The workspace lane withdraws its contrary capacity recommendation. Restore preserves all valid saved descriptors (at most eight), refuses overflow visibly and leaves original records recoverable. Baseline also requires non-destructive v4 migration and specifies `/browser/$viewId`; these govern implementation. Accept native's final compact `act(BrowserAction)` discriminated union in place of the separate action methods listed above: still typed/validated, never arbitrary IPC or CDP. Native accepted explicit state `status: 'restored' | 'ready' | 'unavailable' | 'crashed'`, separate error text, and insert-if-absent restore. Reject old asynchronous initial snapshots after newer events. Keep `admitted` as an idempotent workspace ACK; abandoned/unadmitted creates require bounded cleanup. Restore must not overwrite a live URL with a stale stored URL.

## Balanced overlay lifecycle: exact integration strategy

Use a small generic UI provider, not app/native imports in UI. Proposed host-neutral surface:

```ts
interface OverlayHold { ready: Promise<void>; release(): void }
// Optional provider callback; ordinary browser and Storybook use an immediate inert hold.
acquireOverlay(): OverlayHold;
```

A renderer presentation coordinator owns a bounded Set of unique hold tokens. The first hold sends a blocked presentation and shares its acknowledgment with nested holds. `release` is idempotent. Only the last release may unblock, after remeasurement; every IPC state change has a newer revision. No effect cleanup decrements a naked global counter.

**Opening must be gated on hide acknowledgment**, not an effect that sends hide after an already-interactive popup rendered. Track requested open separately from effective Base UI open. Controlled/programmatic opens, keyboard opens, hover tooltips and default-open roots all use the same gate. Rejection leaves approval controls unavailable and reports native presentation failure; never continue to expose a popup behind the guest.

Hold through closing animations and Base UI focus return; release on `onOpenChangeComplete(false)` or actual unmount. A child popup/submenu acquires its own token. Parent closing, child staying open, StrictMode, owner replacement and route unmount cannot release the child's token. The fixture proves the primitive handshake, not every Base UI callback path.

Hiding all native slots is the minimal safe first policy: four is the maximum, and rectangle-intersection bookkeeping would add an unneeded second overlay system. Tooltip/toast flicker and screenshot continuity need real visual review; a single capped transient capture is optional and must not create continuous capture or unbounded image retention.

### Complete current owner audit

| Owner | Required coverage |
| --- | --- |
| `packages/ui/src/overlays.tsx:17-47` | Dialog, Sheet, AlertDialog, Menu, recursive submenu roots, ContextMenu, Popover, CommandPicker. Gate every root, not only Dialog. |
| `packages/ui/src/forms.tsx:60-64` | Select and Combobox portaled lists. Controlled and uncontrolled keyboard/pointer opening. |
| `packages/ui/src/themes.tsx:233-258` | ThemePicker popover uses BaseCombobox directly; generic Combobox instrumentation alone misses it. Dialog variant delegates to Dialog. |
| `packages/ui/src/actions.tsx:25` | Tooltip root/portal, including workspace tab labels. No native surface may cover it merely because it is non-modal. |
| `packages/ui/src/presentation.tsx:79-82` | UIProvider/ToastViewport fixed portal. Active toast lifetime must hold, or move toast to a natively reserved region; do not omit it. |
| `packages/ui/src/workspace-tab-drag.tsx:224` and `workspace-layout.tsx:156-190` | Drag preview portal and split drop target. Acquire before drag presentation becomes interactive, release on drop/cancel/unmount. Native surfaces cannot cover target feedback or swallow pointer targets. Resize gestures need coordinated presentation/focus and real pointer proof. |
| `packages/app/src/host-prompts.tsx` | SSH host-key/authentication prompts use shared Dialog; provider must wrap application children, not just router content. |
| `packages/app/src/shell.tsx:134-150` | Mobile navigation Sheet, session-search Dialog, Commands picker. Settings route must independently clear slots. |
| `packages/app/src/session-tab-strip.tsx:284-290` | Open-session picker Sheet and per-tab Menu/ContextMenu. |
| `packages/app/src/attention.tsx`, `session-search-dialog.tsx` | Permission/attention navigation surfaces and search, including pending decision state. |
| `packages/app/src/requests.tsx` | Permission and user-question cards are inline in chat, not portals. They must stay inside their chat pane, never under a stale browser slot after layout changes. Any Browser attachment permission variant retains the existing decide/uncertainty path. |
| `host-connection-dialog.tsx`, `host-dialog.tsx`, `remote-directory-dialog.tsx`, `directory-picker.tsx`, `provider-setup.tsx`, `model-selection.tsx`, `completion-picker.tsx`, `permission-mode.tsx`, `session-actions.tsx`, `settings/{section-layout,provider-connections,custom-themes,mcp-import,unsaved}.tsx` | All currently reuse the above shared primitives; preserve that fact rather than adding per-feature native hide flags. |
| `conversation.tsx`, `chat-activity.tsx`, `repl-view.tsx`, `trace-view.tsx`, `session-info-bar.tsx`, `session-sidebar.tsx`, `terminal-view.tsx`, `welcome.tsx`, `connection-notice.tsx` | Existing controls/popovers/menus/tooltips route through shared UI; covered by primitive hooks. |
| Native menus, file choosers, guest JS dialogs, beforeunload, DevTools | Main owns these; separate native suppression/focus restoration. They are not evidence that DOM overlay gating works. |

Audit search found no direct Base UI imports or React portals in `packages/app/src`. App has DOM role queries for focus/close prevention; those are insufficient as an authoritative native overlay lifecycle. Every future custom portal must register via the generic provider.

`createWhipApplication` currently wraps runtime/query/router and its children in `UIProvider` (`packages/app/src/index.tsx:17-31`); desktop host prompts are passed as Application children (`apps/web/src/bootstrap.tsx:20`). Put the native-aware generic provider above that entire subtree, including toast viewport and host prompts.

## SessionTabs, route and kind-switch implementation checklist

| Current source | Phase 2 change / invariant |
| --- | --- |
| `session-tabs.ts:5-46,157-187` | Add Browser union member and parser. Keep `isSessionTab` positive to chat/repl/trace. Do not infer session-backed from `kind !== 'new'`. |
| `session-tabs.ts:124-155` | Freeze generic descriptors without adding location. Closed history retains a safe Browser descriptor. Root purge continues to match only session-backed tabs. |
| `session-tabs.ts:271-367` | Implement v4 initialization/migration; retain v1/v2/v3 records for non-destructive downgrade and recovery. restorePrevious/openPrevious preserve Browser descriptors and do not start pages. Eight-Browser capacity is checked in addition to 32 total tabs; overflow reports a recoverable error, never silent discard. |
| `session-tabs.ts:503-563` | Close lifecycle requires native acknowledgment at app action boundary. Reopen Browser uses new page identity; split follows existing non-session transfer branch and preserves ID/page, not duplicate. |
| `session-tabs.ts:565-597` | Generic transfer/reorder/resize preserve browser identity; no native recreation from pane change. Explicit Duplicate is separate native creation from URL/environment only. |
| `session-tab-routing.ts:29-33,77-90,125-197` | Add `browserDestination`, route `/browser/$viewId`, tab destination and route observation. Direct missing Browser link is unavailable, never implicit creation. `openAfterLastClose` must not infer browser runtime/session host. |
| `routes/browser.$viewId.tsx` (new), `routeTree.gen.ts` | Follow terminal route placeholder pattern, with an explicit missing Browser state. Root coordinates route regeneration; child must not hand-edit shared generated schemas. |
| `session-tab-strip.tsx:47-55,81-106` | Recognize Browser route; add bounded title, browser icon, network-location label and unavailable state. No `tab.runtimeId` assumption or session status query. |
| `session-tab-strip.tsx:127-218` | Async browser close/cancel, reopen, Duplicate/new page, split-as-move and pane move menus. Creation offered only with platform capability; capacity checked before effect. Close-many acknowledges each real close before forgetting it. |
| `session-tab-strip.tsx:248-279` | Existing `visibleTabs.filter(isSessionTab)` feeds WorkspaceViews. Add Browser rendering before session-backed fallback; only selected visible panes report native slots. Compact layout keeps only focused native slot. |
| `workspace-views.ts` | No browser lease type or extra manager. Positive filtering means ordinary pages consume zero SDK root leases/summaries/history subscriptions. |
| `runtime.ts:169-176` | Reading-position cleanup currently tests `kind !== 'new'` then accesses runtimeId: change to a session-backed/terminal-specific guard. Never map Browser to a daemon host to satisfy the type checker. |
| `settings/navigation.ts:77-103` | Record Browser view ID on entry and return to exact tab via `tabDestination`. Current entry matcher records chat/draft only; extend to browser (and preserve terminal behavior). |
| `host-dialog.tsx:224` | Previous-tab label fallback currently assumes non-new/non-terminal means rootId. Add explicit Browser label; do not drop the descriptor in browser-less settings. |
| `shell.tsx:72-91` | Existing desktop close and configured hotkeys are renderer-only. Route permitted app shortcut events from main; close respects active overlays and beforeunload. Add Browser creation action and focus address action. |
| `composer.tsx:141`, `WorkspaceLayout` focus capture | Preserve existing composer focus avoidance for dialogs; never focus composer over native page or permission UI. Native focus event bridges pane focus instead. |

Source navigation uses one pane tree, one `selectedSessionTab`, one route-history hint, and one `SessionTabs` subscription. No second browsing workspace tree, history store, route manager, browser session SDK or attachment state in tab descriptors is justified.

## Focus, unavailable clients and visual intent

Main forwards a fixed allowlist of app actions from a guest: configured palette, composer, new-terminal actions; close/cycle tab; Browser address/find/reload actions. Forward once and prevent guest handling only when app consumes the shortcut. Preserve IME composition, text editing, page scrolling/selection, ordinary page shortcuts and guest DevTools behavior. Cmd+L focuses address; Cmd+F belongs to guest find when Browser is focused, not session search. Overlay dismissal restores DOM trigger or asks native focus only if the same page instance/visible slot is still valid. Never restore focus into a closed, moved-away, blocked or replaced page.

Browser-less web renders a truthful desktop-only/unavailable Browser pane with its saved address and explicit close/copy/open-externally action. No creation, native page control or attachment pretend-success. Parsing/persistence is capability-independent. The current Expo mobile app has no `SessionTabs`, `SessionTab` or `whip.web.workspace.v3` references; it is not a second owner of the desktop layout. Do not add that ownership. Shared protocol browser methods must remain optional/unsupported in mobile; do not claim a mobile build from this source audit.

Design stays within the approved shell: one contoured tab and a compact toolbar; current font, quiet borders, StyleX tokens and focus outlines; no new nested tab strip/theme. Human goal: inspect a development page next to conversation/terminal while seeing network provenance and agent control separately. Browser status uses This Mac / SSH preview; controller badge is separate. Vocabulary: page, address, preview, conversation, permission, pause, detach. The signature is the existing split-workspace relationship, not generic browser chrome; no decorative card/gradient/metric dashboard. Native unavailable/crashed/error/blocked states use existing restrained notices with exact recovery actions. Toolbar must remain usable at minimum pane width, in compact mode, all catalog themes, high contrast and reduced motion.

## Proposed edit ownership after root freeze

Workspace lane: `packages/app/src/{session-tabs,session-tab-routing,session-tab-strip,shell,runtime,host-dialog}.tsx?`, `settings/navigation.ts`, new Browser view/presentation helpers and Browser route; associated app tests. Shared UI overlay provider and wrappers in `packages/ui/src/{overlays,forms,themes,actions,presentation,workspace-tab-drag,workspace-layout}.tsx` can also be assigned to this lane because their lifecycle must be integrated as one audit, not split among feature owners.

Native lane: browser manager, security/profiles, IPC/preload validation, serialized bridge event contract and desktop adapter realization. Root should assign **one** owner to `packages/app/src/platform.ts`, `desktop-bridge.ts`, `apps/web/src/platform/desktop.ts` and coordinate imports before simultaneous implementation. Root owns generated route/schema integration and canonical guide/feature/desktop documentation as assigned; this evidence document does not replace them.

Unresolved blockers before production work: root freeze of persisted environment reference and native restore/admission/close ordering; final native state and shortcut/focus event shapes; before-open ACK integration across Base UI primitives (including default-open and animation completion); real native compositor results from native lane. Later acceptance still needs actual shell overlays/permissions, drag/resize, zoom/monitor/window changes, native focus/IME and VoiceOver, crash/reload/reopen, browser-less preservation, and packaged end-to-end evidence. None is waived by these helper tests.
