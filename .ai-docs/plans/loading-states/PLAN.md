# Loading states and layout stability

Status: decisions 1–4 confirmed by Sam 2026-09-13 (recommendations taken); items 1–3 implemented in the working tree (uncommitted), `npm run test:web` 646 passing, `npm run check:web` green, live re-audit clean (see Verification results).
Evidence: [evidence/audit-2026-09-13.md](evidence/audit-2026-09-13.md) (layout-shift traces, first-frame screenshots, code map).

## Why this matters

The app already has the right bones: the SDK owns loading state, the reading list keeps its scroll anchors, the startup splash hides the cold path, and the two dialogs that read from a host use placeholders and delayed indicators. The problems are all in the seam between “opening” and “failed”: three surfaces derive their *unavailable* copy from `!root` or `!ready` and therefore show an error-flavoured placeholder during every normal open. On Local that is a 50–75 ms flicker plus a 21 px composer shift; on an SSH host the same wrong state sits on screen for the whole round trip and the toolbar looks unconfigured.

Measured on Local (details in evidence):

| Flow | What the user sees first | Shift |
|---|---|---|
| Open a saved session | `Local / Directory unavailable / Root`, hint “Session content is unavailable…”, no mode/model/effort pickers | composer 21 px, info bar 58 px |
| New Chat | heading “Connect a provider to get started” + “Checking providers…”, then the composer | column re-centers 60 px |
| Leave a New Chat for Settings | “This New Chat is not available” for one frame | full-width flash |
| Cold reload, Session details | nothing wrong | none |

## Principle

Sam’s brief: minimal skeleton, only where the UI would otherwise jump or look incomplete. Applied as four rules, in priority order:

1. **Never show a wrong state as a placeholder.** “Unavailable”, “Connect a provider”, “not available” are outcomes, not waits. While opening, copy is neutral (“Opening…”) or absent.
2. **Reserve the footprint, don’t swap components.** Header, composer and toolbar exist from the first paint with the same height; only their contents fill in. Reserving usually needs no skeleton: render the real control disabled, or nothing in a fixed-height slot.
3. **A skeleton only where a blank slot reads as “incomplete”.** In this app that is the composer’s mode/model/effort pill row, which sits under the user’s cursor for the whole wait on a remote host. Reuse `Skeleton` from `@whip/ui` (already used for SSH profiles).
4. **Status text goes where the content will be.** A bare “Loading X…” line above a section shifts the section when it disappears; a status line inside the slot it describes does not.

Delayed indicators (the directory dialog’s 180 ms) stay for spinners. Footprint reservation is structural and never delayed; a reserved slot that appears late is itself a flash.

## Decisions (confirmed 2026-09-13)

1. **Composer pill row while a session opens: three `Skeleton` pills** (mode, model, effort) in the `modelControl` slot, no delay. Recommended over leaving the slot empty: the pickers fill in from the right so nothing else moves either way, but an empty row on a remote host reads as “no model configured” for seconds. Same pills serve New Chat while the provider inventory is pending.
2. **New Chat renders the composer from the first paint** whenever a host is selected, disabled until the handshake and inventory land. Replaces the text-only “Connecting to {host}…” body and the premature provider-setup panel. Provider setup still takes over the moment the inventory says the selection is not ready.
3. **Sidebar sessions keep the one-line “Loading sessions…”**; no skeleton rows. The list is a natural grow region inside a scroll container and pushes nothing but itself.
4. **Settings loading lines are deferred** (item 5). Host-bound sections only, one text line, and the host-defaults form already retains the previous config across refetches.

## Items

### 1. Session open: honest placeholders, stable composer

**Why.** Every tab open, deep link and reconnect passes through `SessionContent` with `root` undefined for one round trip. Today that paints “Directory unavailable”, “Session unavailable”/“Idle”, and the composer hint “Session content is unavailable. Your draft stays here; it will not be sent automatically.”, then removes the hint (21 px shift) and pops three pickers into the toolbar. The pre-lease fallback in the tab strip repeats the same info bar with `Loading session…` but no composer; it is rarely painted because the lease resolves in a layout effect, but it should agree with the real one.

**Target.** One `opening` flag in `SessionContent`: connected, no `root`, no `state.error`, `state.status` in `idle`/`loading`. While opening: info bar shows host and, when the tab summary knows it, the project, with the activity slot keeping the existing “Loading session…” (already produced by `activityStatus` when there is no root and no error); composer shows textarea, disabled attach/@, pill skeletons in the picker slot, no hint; body keeps `SessionLoading`. When the view fails (`state.error`, `status === 'error'`, `unavailable`), every current “unavailable” message stays exactly as it is.

**Steps.**
1. `packages/app/src/conversation.tsx` (~line 176–182, 266–278, 355–370): derive `opening`; pass `cwd={root ? root.meta.cwd : summaryCwd}` where `summaryCwd` comes from the tab strip: the summaries poll when it has answered, else the sidebar catalog's `cwd` for that session (`knownCwd`), which is present before the first poll; activity fallback unchanged (`activityStatus` already returns “Loading session…” for a rootless, error-free snapshot); `modelControl={root ? <>pickers</> : opening ? <PickerSkeletons /> : undefined}`.
2. `packages/app/src/session-info-bar.tsx` (line 20–21): add `pending?: boolean`; when pending and `cwd` is empty, render the host alone (no “Directory unavailable”) and tooltip “Opening…”. `kind === 'new'` copy unchanged.
3. `packages/app/src/composer.tsx` (line 483–485): add `pending?: boolean`; the hint block renders only when `!connected && !pending`. Buttons stay disabled through `connected`.
4. `PickerSkeletons`: three `Skeleton`s sized to the trigger buttons (roughly 128/108/72 px wide, button height), `aria-hidden`, defined next to `SessionModelPicker` in `model-selection.tsx` so New Chat reuses it.
5. `packages/app/src/session-tab-strip.tsx` (line 270–274): pass the same `pending` flag and “Opening…” text to the fallback info bar so the two branches match.

**Tests.** New `packages/app/test/session-opening.test.tsx`: a view whose snapshot is `status: 'loading'` renders no text matching /unavailable/i, no composer hint, three skeleton pills, and info-bar host without “Directory unavailable”; after the view goes live the pickers replace the pills and “Idle” appears. Keep green: the existing history-error cases in that file (hint and Refresh notice on `state.error`), `session-info-bar.test.tsx`, `composer.test.tsx`, `desktop-close-tab.test.tsx`.

**Trade-offs.** The summary `cwd` can be a poll behind the root; it is the same directory the sidebar groups by, so a stale value is still the right project.

### 2. New Chat: composer footprint before the host answers

**Why.** `ready` is false until the provider inventory resolves, so `setupVisible` is true on the first paint and the tab opens on “Connect a provider to get started” with a provider panel, then swaps to the composer and re-centers 60 px. A host that has not finished its handshake shows heading + “Connecting to {host}…” and no composer at all, so the composer arrives seconds later on remote hosts. Both are the centered column changing height.

**Target.** With a host selected, the first paint is heading “What do you want to work on?”, the composer with disabled controls and the pill skeletons in the model/effort slot, and the host/folder toolbar. Copy under the composer says “Connecting to {host}…” or “Reconnecting…” as appropriate. Provider setup appears only once `providers.inventory.data` exists and the selection is not ready, or when the user opens it.

**Steps.**
1. `packages/app/src/welcome.tsx` line 147–148: `setupVisible = ((providers.inventory.data ? !ready : false) || showProviders) && !recovery && !recoveryError`. Heading follows `setupVisible` as today, so it no longer flips during a normal load.
2. Line 159–163: `ready ? <CatalogModelPicker …> : providers.inventory.data ? <Button>Connect a provider</Button> : <PickerSkeletons />`. `DraftEffortPicker` already disables on `catalog.isPending`; hide it behind the same skeleton while inventory is pending so one slot holds both.
3. Line 66–70: drop the text-only `NewSession` body. Render `WelcomeComposer` as soon as `host.client` exists; `connected` already gates every control, and the existing note at line 197 becomes “Connecting to {host}…” when `connection.info` is absent and “Reconnecting to {host}…” otherwise. `runtimeId` falls back to `host.runtimeId`, which may be undefined before the first handshake; queries are disabled in that state, so keys with `undefined` never fetch.
4. `submit()` guards are unchanged: `!connected`, `requiresUpdate`, `!ready` (opens provider setup), `!cwd` all still block sending.

**Tests.** `packages/app/test/welcome.test.tsx`: with inventory pending, the textarea is present, the heading is “What do you want to work on?”, no “Connect a provider” text; when inventory resolves with `selection.ready === false`, provider setup appears (existing behaviour); with a client whose `connection.info` is unset, the composer renders disabled with the connecting note. Keep green: the rest of `welcome.test.tsx`, `welcome-recovery.test.tsx`, `welcome-submission.test.ts`, `provider-connections.test.tsx` (the “Update Whip on host” path when `inventory.data` has no `selection` still routes through `ProviderSetup`).

### 3. Route fallbacks paint during transitions

**Why.** `routes/new.$draftId.tsx` renders `<EmptyWorkspace missing />` unconditionally and `routes/h.$runtimeId.s.$rootId.tsx` renders “Opening session…” copy. The tab strip hides the outlet while a tab matches, but the router commits location and matches in separate renders, so leaving a tab for `/settings` paints the previous route’s fallback for a frame (measured value 0.26, the largest shift in the audit, one frame).

**Target.** Route components render `null` while the workspace still owns the tab they describe; the “missing” and “opening” copy appears only when the tab strip genuinely has nothing to show.

**Steps.**
1. `routes/new.$draftId.tsx`: `useSessionTabs()`; return `null` when `runtime.tabs.workspace().tabs.some(tab => tab.id === draftId)`.
2. `conversation.tsx` `ConversationRoute`: same guard on `runtimeId`/`rootId` before the `canOpen` and host checks. Keep the “tabs are full” and “connect the host” branches for the unmatched case.
3. `routes/h.$runtimeId.t.$terminalId.tsx` has the same shape (`<EmptyWorkspace missing subject="terminal" />`, unconditional); guard on a matching terminal tab the same way.

**Tests.** None added: each guard is a one-line `tabs.some(...)`; the live re-run of the audit script (below) is the check that the flash is gone, and typecheck covers the rest.

### 4. Tab title width on deep links (accept, document)

Tabs opened from a URL or restored from storage start as “Untitled session” until the root snapshot or first summaries poll supplies the title (`session-tabs.ts:483`, `conversation.tsx:128`). The tab is content-sized, so neighbours move once. Sidebar, search and attention opens pass the title, so only deep links are affected, and on Local it happens under the splash. Not worth a min-width that would make every tab wider; note it in `docs/frontend.md` and move on.

### 5. Settings section loading lines (deferred, decision 4)

“Loading providers…”, “Loading agents…”, “Loading host defaults…” are bare `<p role="status">` lines above their section that vanish on load. Rule 4 says the line belongs inside the `SettingsGroup` panel it describes. One-line change per section when someone is next in `settings/`; not part of this plan.

## Already correct, keep as is

- Startup splash covering the cold path; the 3 s release for offline hosts.
- `SessionLoading` spinner for the empty conversation body.
- Reading list: `visibility: hidden` exhausted button, anchored prepend, follow/Latest.
- Tab-strip status icons and sidebar row indicators (fixed slots).
- Remote directory dialog and SSH profile list (house patterns for placeholders and delayed indicators).
- `ConfigurationSettings` retaining the previous config during refetch.

## Preserved / changed / not built

**Preserved.** Every failure message and its trigger (`state.error`, `unavailable`, `status === 'error'`, disconnected host, wrong runtime); the provider-setup flow and the “Update Whip on {host}” warning; recovery-record handling in New Chat; all submit guards; the tab strip’s lease fallback for disconnected hosts.

**Changed.** Copy and structure of the opening state in `SessionContent`, `SessionInfoBar`, `Composer`, `WelcomeComposer`; two route components render `null` while the tab strip owns their tab; one shared `PickerSkeletons`.

**Not built.** Skeleton rows anywhere except the picker slot; delayed indicators for structural placeholders; tab min-widths; settings-section changes; anything for the compact/phone layout beyond what the shared components carry.

## Verification

- `npm run test:web` and `npm run check:web` (typecheck plus production StyleX build); `task check` before merging.
- Repeat the audit script from the evidence file on Local for the three flows; expect only the tab-insertion entry and no text matching /unavailable|Connect a provider|not available/ in the first frame.
- One remote pass on `kuzco-4090` (see memory note): open a saved session, open New Chat, leave for Settings; the opening states should hold their footprint for the whole round trip. Not yet done; no remote host was configured in this environment.
- Update `docs/frontend.md` with the four rules under Principle so the next surface follows them. Done: “Loading states and placeholders” under Component and styling contract.

## Verification results (2026-09-13)

- `npm run test:web`: 61 files, 646 tests passing (3 new in `session-opening.test.tsx`, 2 new in `welcome.test.tsx`). `npm run check:web`: typecheck and production build green.
- Live re-audit against the onboarding container's daemon (harness and table in the evidence file): no forbidden copy in any flow; session open 0.0067 → 0.0002 with the 21 px composer move gone, New Chat 0.0053 → 0.0001, Settings 0.2585 → 0. The residual entries are the status text narrowing in its right-aligned slot and the mode button moving 29 px when the real model label replaces its pill.
- Not verified live: a remote host, and the `/h/$runtimeId/t/$terminalId` guard (no terminal tab was open); both follow the same code path as the verified cases.

## Follow-up 2026-09-13: New Chat first paint

Sam saw a flicker when a New Chat opened on a host with no ready provider. Trace: at 51 ms the composer and two pills painted; at 92 ms the inventory answered and provider setup replaced them (shift 0.0116, the largest measured in this work). Root cause: New Chat has two mutually exclusive layouts and picks with `ready`, which is unknown for one round trip; item 2 above moved the default from setup to composer, so it moved which hosts flicker rather than removing it. The inventory is cached per host afterwards, so it recurred once per reload.

Fix (option 1 of three, chosen by Sam): `AppRuntime.primeProviders` prefetches the provider inventory when a host connects, pinning `gcTime` because the client default of 0 would drop an unobserved prefetch immediately; the web bootstrap awaits Local's prime inside the splash; `useProviderConnections` remembers each answer per host in device storage (`provider-readiness.ts`) and exposes `lastKnownReady`, which `WelcomeComposer` uses to choose the layout while the inventory is pending. Only the very first New Chat ever on a provider-less host, before any answer, still flashes once. Tests: `welcome.test.tsx` (setup is the first paint from the remembered answer; the answer is written), `runtime.test.ts` (one fetch per connection, cached, remembered), `bootstrap.test.tsx` (startup promise chained).

## Follow-up 2026-09-13: first-message recovery removed

Sam asked what the "Recover saved draft" block was and decided to remove the machinery behind it. Removed: `welcome-submission.ts` (the per-draft journal that froze text and parameters, tracked three command ids through creating → sending → accepted/absent/failed, locked the composer, imported a legacy format, and listed orphan drafts), `welcome-recovery.tsx`, `settings/recovery.tsx` and the Settings → Recovery category, the runtime's orphan scan, promotion callback and draft-protection hooks, `SessionTabs.ensureNew`, and 843 lines of tests. The first send is now create → optional effort → submit through the command runner; acceptance promotes the tab in place; failure keeps the draft. The SDK command journal (Layer 1) stays and is surfaced only beside the composer that sent the command. Because the two Settings buttons were the only exits from two hard caps, both caps are now silent: drafts beyond 32 or 1 MiB evict the earliest drafts no open or recently closed tab owns (then the earliest of the rest, never the one being written), and the 1024-record journal drops its earliest record. Retired storage keys are swept at startup. Net: about 1,450 lines removed, under 120 added.
