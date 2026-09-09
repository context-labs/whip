# Settings as a workspace tab

Branch: proposed `feat/settings-workspace-tab` (research performed on `codex/desktop-release`)

Status: Proposed for review, 2026-09-08. No implementation has started.

## What this does

Render the existing Settings surface as one real, selected workspace tab in the
shared web/Electron application instead of as a route page underneath an
unselected session-tab strip. The tab uses the existing tab strip, pane tree,
reorder/move, close/reopen, compact picker, URL, and window restoration behavior.

## Goal

A person can open Settings from any existing entry point, see a selected
**Settings** tab, move between Settings and session tabs without losing their
place, close/reopen it, and restore it after reload/relaunch. The behavior is the
same in the browser and Electron because both use `@whip/app`.

## Decisions

1. **One Settings tab per window.** Every `/settings` navigation selects the
   existing tab or opens one in the focused pane. Repeated sidebar, command
   palette, provider, or deep-link navigation never creates duplicates.
2. **Settings is a real workspace descriptor, not a decorative/pinned tab.** It
   participates in ordering, close/reopen, persistence, and moves between panes.
   It may be moved into a new split, but **Split right/down is not offered** for
   Settings because duplicating one singleton into two panels is ambiguous.
3. **Settings can coexist with sessions in a split.** Only selected pane views
   mount. A visible Settings pane does not acquire an SDK session view; selected
   session panes retain their normal leases.
4. **The URL remains authoritative for the focused view.** The public address is
   still `/settings`; validated `section` and `host` search values represent the
   Settings tab's place. Section/host changes continue to replace the current
   history entry, preserving today's subsection behavior. Back/Forward and deep
   links select/update the singleton tab.
5. **Preserve the existing session budget.** Keep the limit at 32 session views
   and allow one additional Settings descriptor (33 open workspace descriptors
   maximum). Settings has no transcript, summary poll, root lease, or draft, so
   making configuration inaccessible when 32 sessions are open would not serve
   the resource bound. The four-pane, 20-closed-entry, and 64 KiB metadata limits
   remain unchanged.
6. **New session remains a launcher, not a tab.** This request changes Settings
   only. `/` continues to preserve the workspace while showing no selected tab.
7. **Closing is ordinary tab behavior.** Closing active Settings selects the
   right neighbor, then the left, and uses replacement navigation. Closing the
   final workspace tab returns to New session. On Electron, Cmd-W closes
   Settings; it hides the window only when the current page has no active
   workspace tab. Browser-owned Cmd/Ctrl-W remains untouched.
8. **Do not persist secrets or half-edited configuration.** Only section and host
   selection join the descriptor/URL. Provider keys and in-progress forms remain
   component-local and ephemeral, and configuration still changes only after an
   explicit action.

## Research findings

### Current workspace and route behavior

- `SessionTabs` is already the one immutable owner of the window-local pane tree,
  ordering, selection, closed history, and persistence
  (`packages/app/src/session-tabs.ts:6-45,90-105,163-206`). Its current descriptor
  requires `kind: 'chat' | 'repl'`, `runtimeId`, and `rootId`, so it cannot
  honestly represent Settings.
- Selection, add/visit, close/reopen, reorder, split, and transfer are centralized
  in that store (`packages/app/src/session-tabs.ts:239-320,322-367`). Reusing this
  machinery is smaller and safer than adding a parallel settings-tab store.
- The session URL is authoritative and `bindSessionTabs` admits it into the model.
  At startup `/` may restore the selected session. `/settings` currently only
  disables startup restoration and does not select a tab
  (`packages/app/src/session-tab-routing.ts:47-99`).
- `SessionTabStrip` treats only a session route as a matched workspace. Otherwise
  it renders one unselected strip plus the route outlet, and it releases all
  visible root consumers (`packages/app/src/session-tab-strip.tsx:40-47,92-105,
  211-231`). That is why Settings currently looks like a page below the tabs
  rather than a tab.
- The generic UI pieces already support arbitrary tab labels/content. App code
  owns descriptor semantics and content; no new UI primitive or dependency is
  needed (`packages/ui/src/workspace-tabs.tsx:13-45`,
  `packages/ui/src/workspace-layout.tsx:21-37,138-182`).
- The canonical guide explicitly says the current Settings route retains the tree
  but releases visible consumers (`docs/frontend.md:485-493`), while the prior
  tab decision intentionally left Home/Settings unselected
  (`.ai-docs/plans/session-tabs/README.md:145-166`). This feature deliberately
  supersedes the Settings half of that behavior, not the Home behavior.

### Current Settings behavior

- `/settings` validates five sections and renders the shared `Settings` component
  (`packages/app/src/routes/settings.tsx:1-6`). The route must stay stable for
  bookmarks, browser tabs, and all existing callers.
- Settings chooses the last session host or Local in component state; host-backed
  sections expose an execution-host selector. Appearance and Device are local,
  while Providers, Runtime, and Recovery use the selected client
  (`packages/app/src/settings.tsx:32-75,297-320`; `docs/frontend.md:677-683`).
  Because component state disappears when a pane unmounts, the selected host
  should become bounded route/descriptor state along with the section.
- Settings queries are already runtime-scoped TanStack Query reads, and connection
  observation is local to mounted host settings (`packages/app/src/settings.tsx:
  219-229,297-320,734-759,896-900`). A Settings descriptor must never be passed to
  `useWorkspaceViews` or session summary polling.
- Settings includes intentionally ephemeral provider credentials and forms. The
  tab model must not capture those values (`docs/frontend.md:677-680`).
- Entry points already converge on `/settings`: sidebar
  (`packages/app/src/session-sidebar.tsx:63-78`), command palette
  (`packages/app/src/shell.tsx:136-163`), provider picker
  (`packages/app/src/model-selection.tsx:151-154`), and New session
  (`packages/app/src/welcome.tsx:170-176`). Route admission can therefore make all
  of them singleton-aware without bespoke click handlers.
- Electron already forwards Cmd-W to the app tab action
  (`packages/app/src/shell.tsx:60-66`; `apps/desktop/src/main.ts:307`). The existing
  test explicitly expects `/settings` to hide the window
  (`packages/app/test/desktop-close-tab.test.tsx:74-84`); that contract must change.
- Browser workspace storage is per browser tab (`sessionStorage`), while Electron
  uses its main-window-prefixed local storage
  (`apps/web/src/platform/browser.ts:4-8` and
  `apps/web/src/platform/desktop.ts:104-107`). Existing persistence is therefore
  already appropriate for a Settings workspace tab.

## Design

### 1. Make the descriptor a discriminated union

In `packages/app/src/session-tabs.ts`, retain the existing public class/file name
for a small diff, but separate the current session shape from the workspace shape:

```ts
interface SessionTab {
  id: string;
  kind: 'chat' | 'repl';
  runtimeId: string;
  rootId: string;
  titleHint: string;
  location: SessionLocation;
}

interface SettingsTab {
  id: string;
  kind: 'settings';
  location: { section: SettingsSection; host?: string };
}

type WorkspaceTab = SessionTab | SettingsTab;
```

`SessionPane.tabs`, `TabWorkspace.tabs`, and closed entries hold `WorkspaceTab`.
Add narrow helpers such as `isSessionTab`, `selectedWorkspaceTab`, and
`settingsSearch`; retain `selectedSessionTab` as a narrowing helper for composer
and conversation callers. Centralize the section list, `SettingsSection`, and
validation in `packages/app/src/navigation.ts` rather than repeating five string
arrays in route, shell, and Settings.

Use one reserved preferred ID (for example `settings`) but enforce uniqueness by
`kind`, not by the literal ID; existing add logic must mint another ID on a
collision with a real session root. Add `openSettings`/`visitSettings` alongside
`open`/`visit`. They reuse the open Settings descriptor anywhere in the tree,
otherwise append it to the focused pane, select/focus its pane, and update only
its validated location. Restore must deterministically retain at most one open or
closed Settings descriptor, and every ingress—not only route navigation—must
preserve that singleton invariant.

Keep storage version/key v3: this is an additive descriptor variant, not a new
layout shape. Extend parsing/freezing for the union. Older builds already reject
unknown `kind` values and therefore drop only the Settings descriptor while
retaining valid session descriptors; a later write by that downgraded build may
therefore forget the Settings tab, which is an accepted downgrade behavior because
no configuration edit is stored in the descriptor. Pin retained session safety in
a test. Do not invent a second storage key or Settings store.

Count only `isSessionTab` entries in `canOpen`; Settings remains singleton and
outside the 32-session resource budget. Update restore parsing to inspect up to 33
open descriptors while still admitting at most 32 sessions and one Settings tab.
`split()` must reject non-session sources, while `transfer()` and ordering remain
generic.

### 2. Synchronize `/settings` through the existing binder

In `packages/app/src/session-tab-routing.ts`:

- Recognize `/settings` as a workspace destination.
- On route resolution, call `visitSettings(validatedSearch, whipViewId?)` just as
  session routes call `visit`.
- On clean `/` startup restoration, route either the selected session descriptor
  or selected Settings descriptor. An explicit `/settings` or session deep link
  still wins over saved selection.
- Add one route serializer/helper used by the strip for Settings and session
  navigation. Session links retain runtime/root parameters; Settings uses
  `/settings?section=...&host=...`.
- Keep `rememberSession` session-only. Settings navigation must not change the
  last-session shortcut.

Extend TanStack `HistoryState.whipViewId` to Settings navigation as well. The ID
is primarily needed for duplicate sessions but gives close/reopen and browser
history one consistent focused-view contract.

In `packages/app/src/routes/settings.tsx`, use the shared validator. The route
component becomes admission/fallback only; the actual persistent Settings panel
is rendered by the workspace, analogous to the split workspace owning actual
`SessionContent` while `ConversationRoute` owns route status.

### 3. Render Settings in the existing workspace

In `packages/app/src/session-tab-strip.tsx`:

- Generalize `matched`, `active`, `go`, close/reopen, focus, picker, and strip item
  generation to `WorkspaceTab`.
- Filter to session descriptors before summary polling, title updates, draft
  checks, host labels, and `useWorkspaceViews`.
- Render selected session descriptors with the existing `SessionContent`; render
  the selected Settings descriptor as `<Settings>` with its descriptor section
  and host target. This allows Settings and a conversation to remain visible in
  separate panes without creating a fake session lease.
- Give Settings a neutral settings glyph and title, no activity status or host
  metadata, and a type-appropriate menu. It supports close, close others/right,
  reorder, move to another pane, and move to a new split. It omits Open REPL/chat,
  Session details, Copy session link, and duplicate Split actions.
- Change user-visible generic labels from “session tab(s)”/“Open sessions” to
  “tab(s)”/“Open tabs” where Settings is included. Keep session-specific safety
  copy specifically scoped to session tabs.
- On compact screens, show Settings as the active item in the existing picker;
  do not add a second mobile navigation pattern.

No changes should be required in `packages/ui`: `WorkspaceTabs` already accepts a
rendered link, status, metadata, menu, and generic item value, and
`WorkspaceLayout` already accepts generic panel content.

### 4. Make Settings location controlled and durable

In `packages/app/src/settings.tsx`:

- Accept a validated `section` and optional saved host-profile ID (`host`) from
  its workspace descriptor.
- Resolve the host against current profiles; fall back to the documented last
  session host, then Local, when the stored host is missing/unavailable. On first
  admission or invalid/missing saved host, wait until saved profiles are ready,
  then replace the URL with the resolved host ID so the URL and descriptor
  converge; do not prematurely pin Local while remote profiles are loading.
- Replace local `target` ownership with `/settings` replacement navigation on
  host selection. The binder records the resulting location on the singleton
  descriptor.
- Keep the existing section navigation as replace navigation.
- Remove **Back to sessions**. A settings tab now closes or switches through the
  workspace chrome; navigating to `/` would leave an open tab artificially
  unselected.

Do not retain provider secrets, dialog state, or unsaved form edits in the tab
metadata. If Settings unmounts because another tab in its pane is selected, those
values reset as they do under current route navigation.

### 5. Desktop and callers

No Electron main/preload protocol change is needed. Update `AppShell`'s imperative
close contract and wording from session-only to workspace tabs; Cmd-W will close
an active Settings tab and only call `hideWindow` when no workspace tab is active.
All Settings entry points continue to navigate to `/settings`, allowing the route
binder to deduplicate. Use the shared settings-section constants in the command
palette and route validator.

## Ordered implementation

1. Add shared Settings section/search types and validators in
   `packages/app/src/navigation.ts`; update `routes/settings.tsx`, `settings.tsx`,
   and `shell.tsx` to consume them.
2. Extend `packages/app/src/session-tabs.ts` with the descriptor union,
   type guards, singleton Settings admission/location updates, session-only
   capacity checks, generic close/reopen/reorder/transfer, and safe restore.
3. Generalize `packages/app/src/session-tab-routing.ts` to admit and restore both
   route-backed descriptor types.
4. Generalize `packages/app/src/session-tab-strip.tsx` to render mixed tab items
   and mixed panels while filtering every daemon/session concern to session tabs.
   Update `conversation.tsx` and `host-dialog.tsx` only where union narrowing or
   the session-only capacity helper requires it.
5. Update `settings.tsx` to use controlled host location and remove its standalone
   back link. Keep all mutations and secret inputs unchanged.
6. Update shell/tab/picker wording and Electron close behavior tests. Do not add a
   dependency or a desktop-only renderer path.
7. Add focused unit/integration/browser coverage, then update canonical docs and
   the feature/roadmap records.

## Validation plan

### Model and routing

Extend `packages/app/test/session-tabs.test.ts` to cover:

- singleton admission from repeated Settings visits;
- coexistence with same-named session roots and across hosts;
- section/host updates without changing tab identity or order;
- close/reopen with original position and Settings location;
- reorder and transfer between existing/new panes;
- rejection of Settings duplication via `split`;
- 32 session tabs plus Settings, then refusal of a 33rd session;
- v3 persistence round trip, malformed settings metadata, 64 KiB fallback, and
  restoration by a new instance.

Extend `packages/app/test/session-tab-routing.test.ts` to cover:

- direct `/settings` admission and invalid search normalization;
- Back/Forward between session and Settings descriptors;
- existing singleton selection from another pane;
- startup restoration to Settings from clean `/`, while explicit deep links win;
- no `rememberSession`, host connection, root lease, or summary admission for
  Settings;
- close replacement does not immediately re-admit the old Settings URL.

### Components and desktop behavior

- Add or extend an app integration test around `SessionTabStrip` for the selected
  Settings item/panel, settings-specific menu, mixed split panels, no session
  summary ID for Settings, and generic picker labels.
- Extend `packages/app/test/desktop-settings.test.tsx` for controlled section/host
  routing and fallback when a saved host disappears, while retaining update and
  secret/error tests.
- Change `packages/app/test/desktop-close-tab.test.tsx` so Cmd-W closes Settings,
  selects/navigates to the proper neighbor (or New session), preserves all
  session work, and hides only when no workspace tab is active.
- Keep existing `packages/ui` tab/layout suites unchanged unless the app work
  exposes a genuinely generic primitive defect.

### Product browser and Electron checks

Extend `apps/web/scripts/session-tabs.mjs` (or add a narrowly named settings-tab
fixture only if the existing script becomes unclear) to verify in Chromium and
Firefox:

1. session → sidebar Settings creates and selects one Settings tab;
2. repeated links/command actions reuse it and update its section;
3. tab switching and browser Back/Forward restore section/host and session place;
4. close/reopen and reload restore the Settings tab and order;
5. Settings moves between panes and remains usable beside a live session without
   an extra root subscription;
6. the narrow picker lists/selects/closes Settings with no horizontal overflow;
7. keyboard focus, Delete/close focus recovery, 200% zoom, long host names, light
   and dark themes, Axe, and strict CSP remain clean.

Extend the staged Electron smoke flow to assert Settings is selected as a tab,
restores after reload/relaunch, and Cmd-W closes it instead of hiding the window
when another workspace tab remains. No native API change should be necessary.

Run at minimum:

```sh
npm run check:web
npm run test:web
npm run test:tabs -w @whip/ui
npm run test:layout -w @whip/ui
npm run pack:web
node apps/web/scripts/session-tabs.mjs
node apps/web/scripts/workspace-layout.mjs
npm run check:desktop
npm run test:desktop
```

Then run `task check` for integrated completion. Record browser/desktop manual
coverage accurately; Playwright WebKit is not physical Safari or VoiceOver.

## Documentation on implementation

- Update `docs/frontend.md` state ownership, workspace limits, split-workspace,
  URL/navigation, Settings, compact picker, and validation sections. Replace the
  statement that Settings releases all visible consumers with the mixed-pane
  rule.
- Update the relevant web/desktop row in `docs/features.md` with Settings-tab
  behavior, implementation files, and named tests.
- Update `packages/app/README.md` from a session-only descriptor description to a
  workspace descriptor description.
- Add a checked roadmap item only when implementation and stated validation have
  landed. Keep this file as the historical design/acceptance record.

## Non-goals

- Making New session, Search, Attention, or Execution hosts into tabs.
- Multiple Settings tabs or independent Settings copies per host.
- Persisting secrets, dialogs, busy state, or unsaved configuration forms.
- Changing daemon configuration ownership, protocol/SDK APIs, query caching, or
  Electron IPC.
- Renaming `SessionTabs`/`session-tab-strip.tsx` solely for terminology; that can
  be a later cleanup if the workspace gains more non-session view kinds.
- Changing browser-owned tab/window shortcuts.

## Risks and safeguards

- **Union leakage into daemon calls:** require `isSessionTab` before summaries,
  root keys, drafts, copied session links, or view leases; test that Settings adds
  zero session IDs/subscriptions.
- **Route/store feedback loops:** preserve the current one-way contract—route
  resolution updates the descriptor, UI actions navigate, and close uses replace.
  Test the synchronous close transition and browser history.
- **Persistence regression:** sanitize both union variants, bound every string,
  retain the existing byte/count limits, and prove old v3 session-only records
  still restore.
- **Singleton ambiguity in split panes:** route navigation always focuses the one
  existing Settings descriptor; moving is allowed, duplicating is not. Enforce
  this in the store and restore parser, not only by hiding menu items.
- **Route-close race:** navigate to the selected replacement with `replace` after
  closing Settings so the still-current `/settings` location cannot immediately
  re-admit it; retain the existing observed-location guard and regression test.
- **Accidental secret retention:** only section and host identity enter URL or
  storage; provider key/form state remains local.
- **Concurrent work in this checkout:** the research tree already contains staged
  changes to `session-tab-strip.tsx`, `shell.tsx`, `workspace-tabs.tsx`,
  `workspace-layout.tsx`, `docs/frontend.md`, and desktop smoke/chrome files.
  Implementation must build on and preserve those changes rather than resetting
  or replacing them.

## Acceptance criteria

- Settings always appears as one selected tab when `/settings` is focused.
- All existing Settings entry points select/reuse that tab and preserve native
  modified-link behavior.
- Settings can be reordered, moved, closed, reopened, and restored; it cannot be
  duplicated.
- A split may show Settings and session content simultaneously, with the URL
  describing the focused pane.
- Settings creates no session summary request, SDK root lease, draft identity, or
  copied session link.
- Section and selected host survive ordinary tab switches and window restoration;
  secrets and unsaved forms do not persist.
- Thirty-two session views remain possible with Settings open; existing pane,
  closed-history, and metadata bounds still hold.
- Electron Cmd-W closes Settings as a tab; browser Cmd/Ctrl-W remains native.
- Existing session tabs, REPL mode, split drag/drop, drafts, reading positions,
  New session, deep links, and accessibility behavior remain green.
