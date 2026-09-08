# WHIP session tabs

Feature design and acceptance history. For the maintained cross-feature
architecture and design contract, read [docs/frontend.md](../../../docs/frontend.md).

Branch: `whip-rlm`

Status: Implemented, 2026-09-07. Automated acceptance is recorded below.
Manual physical-mobile and VoiceOver release checks remain open.
[Interactive design study](design.html).

The implementation follows the study's recessed strip, evenly sized tabs,
outlined selected canvas, and close-only tab controls. Surfaces derive from the
shared theme palette through `surface.navigation`; the study's literal colors
are not copied into application styles. Secondary actions remain available from
the context menu and open-session picker. Browser checks verify visible selected
borders and borderless close controls, alongside accessibility across all 65
themes; application screenshots cover light and dark appearance.

## Goal

Let a person coordinate several WHIP sessions without losing their place, draft,
or understanding of which work needs them. Tabs are a window's working set of
sessions; the sidebar remains the catalog of saved sessions. The daemon continues
to own execution. Opening, switching, reordering, or closing a tab is a local UI
operation and must never submit, cancel, delete, or restart agent work.

The design should have OpenCode's restrained surfaces and polished interaction,
adapted to WHIP's recursive agents and existing React architecture.

## Decisions

The user approved the recommended defaults with the full implementation request.
The remaining choices follow the established web scope and research below.

| Decision | Reason |
| --- | --- |
| One strip across projects on the connected host | People switch between tasks, not just folders; preserve project identity in secondary context |
| Selecting a session opens or selects a persistent tab | Predictable working set; no preview-tab replacement or double-click distinction |
| Root sessions are tabs; children stay inside their root | A recursive task can create many children; spawning must not fill the tab strip |
| One host connection at a time | Preserve the current runtime ownership; simultaneous multi-host monitoring is a separate feature |
| URL determines the active session | Deep links and browser history must work without a second navigation authority |
| Window-local layout, restored on reload | Independent browser windows should not reorder or close each other's app tabs |
| Up to 32 open session tabs, with at most four retained full views | Many bookmarks do not require many transcripts, subscriptions, or mounted composers |
| Use existing theme tokens in every state | Support all 65 TUI theme fixtures, automatic appearance, and custom themes from launch |
| Reordering, reopen closed, and overflow search are included | These become daily usability requirements once several sessions are open |
| No tab groups, pinned tabs, preview tabs, split panes, or detached app windows | They add interaction rules without being necessary for the first good version |

Also excluded: editors, code review, terminals, Electron packaging, connection
authentication, offline submission, shared tab layouts, and a general docking
framework. Existing sidebar pinning is a catalog preference, not a new tab mode.

## Research and lessons

OpenCode source was read at development commit
`57ef3828431790c53f8f333c7ffbfe88770a1812`, observed 2026-09-07. This is a pinned
source snapshot, not a claim that every behavior is in the latest stable build.
The earlier [web-app research](../web-app/README.md) separately inspected release
v1.18.29 and its installed application. This review focuses on tab implementation;
it is not a new benchmark of OpenCode runtime performance.

| Observed behavior | WHIP decision |
| --- | --- |
| Session and draft tab identities, deduplicated opening, draft promotion, persisted window state | Borrow stable identity and opening semantics; start with one reusable New session launcher, since WHIP creates a session before composing |
| Close chooses the next tab, with a bounded reopen stack | Use the right neighbor, then left; preserve selection when closing a background tab; keep 20 closed entries |
| Tab-local memory has explicit disposal | Preserve useful view state, but bound full SDK views independently of open tabs |
| Tab navigation resolves session/prompt data for individual tabs | Avoid hydrating every WHIP root merely to paint its label; use bounded navigation summaries |
| Quiet separators, active surface, title truncation, hover metadata | Borrow the visual discipline and reserve status/close space so text does not jump |
| Tabs may shrink to tiny widths and eventually hide their labels | Keep readable minimum widths and provide overflow; use a list on phones |
| Mouse-down selection and touch-disabled dragging | Activate on completed click, with a drag threshold; use explicit movement actions for touch and keyboard |
| Draft cleanup occurs when draft tabs close | Do not copy this behavior: closing WHIP tabs preserves unsent text and attachment state |

Primary source references:

- [Tab model and lifecycle, especially lines 19–69 and 155–280](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/context/tabs.tsx#L19).
- [Closed-tab ordering and retention](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/context/closed-tabs.ts).
- [Per-tab memory ownership](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/context/tab-memory.ts).
- [Strip layout and per-tab data access](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/components/titlebar-tab-strip.tsx).
- [Tab navigation and interactions](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/components/titlebar-tab-nav.tsx),
  [visual states](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/components/titlebar-tab-nav.css),
  [pointer policy](https://github.com/anomalyco/opencode/blob/57ef3828431790c53f8f333c7ffbfe88770a1812/packages/app/src/components/titlebar-tab-gesture.ts).
- [OpenCode v1.17.13 release notes](https://github.com/anomalyco/opencode/releases/tag/v1.17.13)
  document per-window restoration, tab error isolation, and richer hover context.

## Visual and interaction design

### Intent and visual vocabulary

The person using this interface is coordinating root sessions, recursive child
work, pending permissions, questions, drafts, and projects on an execution host.
They should be able to leave a task without losing it and notice a request for
help without inspecting every conversation.

Use WHIP's existing palette: paper/off-white content, ink/graphite text, steel-like
quiet separators, amber human attention, muted blue activity/focus, and existing
error/success tokens where their meanings apply. Themes supply the actual colors.
Color is sparse and semantic; projects do not each receive a bright accent.

The signature is a calm working set with a small, legible indication of human
attention and recursive activity. A tab does not become “complete” merely because
its root turn ended. Three tempting defaults are deliberately avoided: shrinking
tabs into unlabeled slivers, using a project rainbow, and mounting every open
conversation to preserve its place.

### Desktop composition

Replace the existing global main header with a 48 px tab/utility row; do not add
a third horizontal header. Keep the session's context header below it, containing
the full title, project/path, agent selection, and Details action. Keep the sidebar
and its existing project/session navigation.

- Tabs: 36 px tall, 144–224 px wide, 6 px radius, 4 px gaps; 13 px medium labels.
- Active tab: conversation background, stronger text, quiet outline. Inactive
  tabs share the surrounding surface. Hover changes the surface, not position.
- A fixed status slot precedes the title. The close hit area reserves 24 px;
  reveal its glyph on active, hover, or keyboard focus. Touch gets 44 px targets.
- Keep the plus button and All open tabs control outside the horizontal scroller.
  Keep host connection, Attention, and a compact utility menu at the right.
- Clamp titles to one line. For collisions, append a short project label; the
  tooltip and open-tabs list always expose full title, project, and host context.
- Overflow scrolls horizontally without shrinking labels below the minimum.
  Selecting a hidden tab brings it into view. Status updates do not move the strip.
- With at most 32 entries, the strip and picker do not need virtualization.
  Keep virtualization where it already matters: the transcript.
- No new font or animation library: use Inter, existing spacing/radii, and current
  motion tokens. Selection is immediate; reorder motion is short and reduced-motion
  aware. Avoid perpetual spinners on every running tab.

### State vocabulary

| State | Presentation and meaning |
| --- | --- |
| Needs input | Amber request glyph; accessible label says permission/question count; takes visual priority over activity |
| Running or queued | Small activity glyph/dot; tooltip reports running and queued agents across the root tree |
| Quiet | Neutral session glyph; means no currently observed activity, not that schedules or the entire task are finished |
| Disconnected or summary unavailable | Retain title and last content; mark activity stale/unknown, never silently clear it to idle |
| Missing session | Explicit unavailable state after authoritative lookup; offer close and return to sessions; preserve local draft |
| Unsent draft | Subtle pencil marker or text in the menu/list; do not overload the running/attention glyph |

Do not introduce “unread” counts, completion checkmarks, or cross-client read
receipts without a protocol definition. These are different from activity and
pending human input. A cached title is acceptable; a cached permission answer is
not authoritative. Existing daemon conflict handling still resolves competing answers.

### Interaction contract

| Action | Result |
| --- | --- |
| Click a sidebar session | Reuse its existing tab or append one; activate it |
| Open session in background from its menu | Add without switching or opening its transcript |
| Click a tab | Navigate to that root's last selected child/inspector state; restore its reading position |
| Plus / New session command | Show one reusable local New session launcher; no root is created until the existing create action succeeds |
| Successful session creation | Add/select the returned root once; late acknowledgement cannot steal focus after navigation or recreate a closed tab |
| Close active tab | Choose right neighbor, then left; last close returns to New session; use history replacement for this fallback |
| Close background tab | Leave the current route and focus unchanged |
| Close any tab | Detach presentation only; preserve draft and accepted/uncertain work; make Reopen closed tab available |
| Reopen closed tab | Restore original position where possible, deduplicate by identity, and activate; retain up to 20 entries |
| Reorder | Pointer drag with about a 6 px activation threshold; keyboard/menu Move left/right provides the same operation |
| Tab menu | Close, Close others, Close to the right, Move left/right, Copy session link; rename/delete remain existing session actions |
| Back/forward or deep link | Honor URL; ensure its root is represented once, including reopening a previously closed tab if navigation requests it |
| Home/settings | Preserve the strip with no session selected; returning to a session restores its context |

The launcher is not a collection of pre-session draft objects. Multiple independent
unsent New session tabs and draft-to-session identity migration are deferred.
The launcher is outside the 32-session limit. Creating a 33rd session tab prompts
the person to close an existing tab; never silently evict an open tab.

### Keyboard and mobile

Use Base UI's controlled Tabs and link composition, with manual focus activation:
arrow keys move focus, Home/End select the focus boundary, Enter/Space activate.
Delete on a focused tab closes it and places focus on its replacement. Close
controls are siblings of the tab link, never nested interactive elements. The
selected route outlet has the corresponding panel label; no hidden full view is
mounted just to supply an ARIA panel.

Provide command-palette actions for next/previous, reopen, close, and search open
tabs. Do not intercept browser-owned Cmd/Ctrl+T, Cmd/Ctrl+W, or Ctrl+Tab. Modified
link activation keeps browser new-tab behavior. Middle-click on an app tab closes
it; ordinary sidebar links retain native middle-click behavior. Electron can
later map native window commands through its platform adapter.

Below the existing 767 px breakpoint, use a single current-session button with
the open count, a visible plus, and the host/attention controls in compact form.
The button opens a sheet listing sessions with title, project, status, and explicit
close actions. Rows and controls are at least 44 px. No tiny horizontal tabs and
no touch drag requirement. An overflow menu provides movement actions.

Validate screen readers, keyboard focus, 200% zoom, RTL/long titles, and reduced
motion. Mobile emulation is additional coverage, not a substitute for physical
touch and mobile browser validation.

Sources: [Base UI link tabs](https://base-ui.com/react/components/tabs#links) and
[WAI-ARIA tabs pattern](https://www.w3.org/WAI/ARIA/apg/patterns/tabs/).

## Architecture and state ownership

### Extend the existing package boundaries

| Owner | Work |
| --- | --- |
| `packages/ui` | Domain-free tab strip/items, focus/close composition, overflow treatment, StyleX states, stories |
| `packages/app` | Session tab metadata, TanStack route integration, status labels, open-tabs sheet, bookmarks, draft/attachment ownership |
| `apps/web` | Window storage using sessionStorage with memory fallback; browser-specific effects |
| `packages/sdk` | Existing views, commands, recovery; generated access to the narrow batch summary query |
| `internal/protocol`, `internal/daemon`, `internal/session` | Bounded navigation summary query; no execution or persistence redesign |

Proposed files: `packages/ui/src/workspace-tabs.tsx`, its story/test files;
`packages/app/src/session-tabs.ts`, `session-tab-strip.tsx`, and
`session-tabs.test.ts`; integrate with `shell.tsx`, `runtime.ts`, `platform.ts`,
`welcome.tsx`, `composer.tsx`, `timeline.tsx`, and the existing
`routes/h.$runtimeId.s.$rootId.tsx`. Keep these small; split only where ownership
or independent tests justify it. No new package, application state framework,
SDK session reducer, or general window manager.

Use a small immutable external store consistent with AppRuntime's existing
subscribe/getSnapshot model. TanStack Router owns navigation; TanStack Query owns
summary requests. Do not mirror either inside a second tab-state machine.

```ts
type SessionTab = {
  rootId: string; // runtimeId belongs to the containing workspace
  lastLocation: { agent?: string; panel?: InspectorPanel };
  titleHint: string; // bounded cached presentation, never identity
};

type WindowTabs = {
  version: 1;
  runtimeId: string;
  tabs: readonly SessionTab[];
  lastActiveRootId?: string; // restoration hint; the current URL wins
  closed: readonly { tab: SessionTab; index: number }[];
};
```

Names are illustrative, not a second wire contract. Key identity by persistent
runtime ID plus root ID. Do not key by title, endpoint, build, or daemon generation.
Child selection and inspector state belong to their root's last location, not
separate automatic tabs. Validate saved IDs and enum values; discard invalid
records without erasing otherwise valid entries.

Add window storage separately from existing persistent preferences/recovery
storage. Namespace records per runtime, cap each workspace at 32 open/20 closed
entries, cap the whole window record collection at 64 KiB, and retain at most four
host layouts. Drop closed history and inactive host layouts before rejecting a
new open entry; never silently discard current open tabs. Store bounded title
hints, identities, ordering, and route hints; no prompts, tool output, credentials,
content bytes, or command payloads.

SessionStorage survives reload and may participate in browser session restoration;
it does not promise restore after every browser shutdown or fresh window. New
windows have independent layouts; duplicated browser tabs may start as a copy
then diverge. Electron can supply per-window persistence later. Storage failure
falls back to memory with the existing nonblocking notification pattern.

On initial bare-home load, restore the last active root only after host identity
is established. An explicit deep link always wins. Subsequent intentional Home
navigation must remain Home. Same-runtime daemon restart preserves tabs; switching
hosts keeps a dormant namespaced layout without keeping its connection alive.
Runtime replacement must not silently reinterpret old root identities.

### Preserve place without mounting everything

The app already caps full SessionViews at four, with a 30-second unused-view
retention window. Each SDK view bounds retained payload to 8 MiB and history to
512 messages per opened agent. Keep those limits and mount only the selected
conversation. Update reuse order so eviction is actually least-recently-used;
the current Map loop evicts by insertion order. Hovering or restoring a tab must
not acquire a view. No extra connections to bypass the daemon's subscription cap.

Record a small reading bookmark per root/agent: stable message identity, history
revision, offset within its row, and whether the reader was following the tail.
Keep at most 128 bookmarks and 64 KiB total in memory; remove them with expired
closed-tab metadata. Reuse the existing TanStack Virtual timeline. Restore after
the relevant data is available; if rewind or eviction invalidates the anchor,
fall back explicitly to the nearest retained content, without an unbounded page
fetch loop. Store composer selection/focus hints only in memory and do not pop
the mobile keyboard open on every tab activation.

TanStack Router's [scroll restoration guide](https://tanstack.com/router/latest/docs/guide/scroll-restoration)
includes manual restoration for virtualized elements. Its default browser-history
key is not itself a root/agent bookmark. Integrate with the timeline's existing
prepend/follow logic rather than layering a competing scroll implementation.

### Drafts, uploads, and uncertainty

Text drafts already live outside React in AppRuntime with explicit bounds. Keep
that store and its existing runtime/root/agent keys. Do not copy prompt bodies
into the tab record. Closing a tab does not discard its text draft.

Attachments and their AbortController currently live in `composer.tsx`; unmounting
aborts uploads and loses selection. Move this small transient composition state
to AppRuntime, keyed by the same draft identity. Preserve attachments on switching
and closing/reopening app tabs; dispose on explicit removal, successful acceptance,
runtime disposal, or an explicit discard action. Keep the current 16-file limit
per draft and enforce a 20 MiB aggregate byte budget across retained unsent
attachments. Reject additional selections at the bound rather than evicting
another draft. Read/upload serially so transient copies remain bounded; free
source buffers after upload and retain scoped references as appropriate.

These attachments remain window-memory state; reload recovery of files is not
added here. Report that limitation when the browser is being left with such data.
Host replacement interrupts transfers and marks attachments unavailable; never
attach them to work on another runtime or silently replay interrupted uploads.

Command handles and uncertain acceptance remain runtime-owned. Tab closure must
not cause a new command ID, cancel a durable command, lose a recovery notice, or
allow resubmission while delivery is unresolved. Late create/submit callbacks must
check current navigation intent before changing selection or clearing a newer draft.

## Background activity: one narrow protocol addition

Current `sessions.list` supplies bounded catalog metadata but no activity counts.
`host.attention` supplies a paginated advisory list of active/attention roots;
absence from one page is not proof that an old open tab is idle or deleted.
Opening every root to fill this gap would defeat the existing resource bounds.

Add one read-only RPC, provisionally `sessions.summaries`, accepting an explicit
set of at most 32 root IDs. Return one result per requested ID: bounded title and
project metadata, running and queued agent counts, pending permission/question
counts, or an explicit missing-root result. Counts remain decimal strings. Reuse
catalog/attention SQL and daemon-owned live question data; do not load transcripts
or open root actors. No command journal, database schema change, or new event bus.

Bound the entire response to 64 KiB, truncate presentation strings with explicit
flags, and always preserve identity/status for all requested IDs. A request or
partial lookup error is not an authoritative missing-root result. Counts are an
advisory observation, not an atomic snapshot of the whole recursive runtime.

Use one TanStack Query per window for the current host's open IDs, polling every
two seconds while visible, paused while hidden and disconnected. Refetch on focus,
reconnect, tab-set changes, and relevant local commands. Reuse recent query data
when toggling the tabs menu. Coalesce active-view event invalidations; do not make
one request per delta or per tab. Background status updates within one polling
interval plus transport latency; do not describe them as push-real-time. Keep
`host.attention` for global attention outside the open working set.

Introduce this as a negotiated additive protocol minor capability and generate
the SDK contract normally. On a host without it, navigation remains functional
and off-view activity is explicitly unknown. Both Unix and WebSocket adapters use
the same handler. Preserve existing authority and permission behavior.

## Dependencies

Use existing Base UI, StyleX, TanStack Router/Query/Virtual, and the command system.
For pointer sorting, propose one focused dependency, `@dnd-kit/react`, after a
small compatibility spike with link tabs, close controls, horizontal overflow,
and keyboard focus. Its current [React API](https://dndkit.com/react/quickstart/)
provides sorting and sensor primitives. Pin the verified version in the existing
root lockfile. This avoids writing a bespoke drag gesture/measurement engine.

Keep sorting implementation inside the UI component package; do not leak dnd-kit
types into application state. Native HTML dragging alone does not cover the
desired keyboard/touch interaction. If the spike exposes unacceptable complexity,
ship explicit Move left/right actions first and document pointer drag as the one
deferred enhancement; do not silently replace the app's interaction framework.

## Ordered delivery

### Phase 1 — Tab model, routes, and restoration

- [x] Add bounded window state and the platform sessionStorage adapter.
- [x] Implement deduplicated open/select/close/reorder/reopen transitions.
- [x] Wire existing session links and successful creation to the model.
- [x] Keep canonical root URLs, query state, browser history, and host identity authoritative.
- [x] Cover corrupt storage, reload, deep-link precedence, independent windows,
  duplicate titles, root deletion, runtime replacement, and late create receipts.

Gate: every navigation operation preserves daemon state and command identity.
Tab closure alone causes zero execution/cancellation/deletion commands.

### Phase 2 — Components and polished navigation

- [x] Build reusable Base UI/StyleX tab components and Storybook states.
- [x] Replace the existing global header; integrate full-title context below it.
- [x] Add overflow search, close/reopen actions, focus behavior, and the sorting spike.
- [x] Add the mobile open-tabs sheet and 44 px actions.
- [x] Verify empty, one-tab, 32-tab, long-title, mixed-project, disconnected,
  attention, draft, overflow, keyboard, RTL, and reduced-motion states.

Gate: no clipping or inaccessible close actions at supported widths, 200% zoom,
and all theme fixtures. Switching does not flash an empty shell or shift layout.

### Phase 3 — Preserve working context

- [x] Restore transcript anchor/follow state and selected child/inspector.
- [x] Make retained view eviction least-recently-used, retaining existing bounds.
- [x] Lift only transient attachment/selection ownership out of the composer.
- [x] Ensure errors stay within the affected session view; the strip remains usable.
- [x] Test switching while streaming, selecting text, paging, uploading, waiting
  for approval, submitting with a lost receipt, rewinding, and reconnecting.

Gate: repeatedly switching does not lose drafts/files, duplicate transcript data,
move a reader to the bottom unexpectedly, or retain full views for every tab.

### Phase 4 — Accurate bounded background indicators

- [x] Implement `sessions.summaries` using existing stores and live question data.
- [x] Generate schemas/types/validators and add typed SDK coverage.
- [x] Wire a shared visible-window query and semantic activity/attention states.
- [x] Test roots outside the first catalog page, child activity after root completion,
  concurrent answers, missing IDs, disconnects, unavailable capability, and bounds.
- [x] Run the same summary scenarios over Unix sockets and WebSockets.

Gate: 32 open tabs use one batched poll, no root hydration for labels, and no
false “idle/deleted/complete” inference from missing or stale observations.

### Phase 5 — Acceptance and simplification

- [x] Run `npm run test:web`, `npm run check:web`, theme and generated-contract
  drift checks, `task check`, affected Go race suites, and release acceptance.
- [x] Add daemon-backed browser tests for root/child streaming across tab switches,
  two independent browser windows, command uncertainty, and reload/restart recovery.
- [x] Validate Chromium, Firefox and actual Safari; record results separately from emulation.
- [ ] Perform manual VoiceOver and basic physical mobile-device interaction.
- [x] Measure warm/cold switch latency, background request volume, event-to-view
  latency, retained payload, subscriptions, and heap behavior at 1/8/32 tabs.
- [ ] Certify the sub-100ms warm physical-paint target; current measurements include
  automation overhead and Firefox exceeds the target on that proxy.
- [x] Update `docs/features.md`, `docs/roadmap.md`, `docs/web-app.md`, protocol
  reference, and ownership/concurrency documentation with behavior-to-test links.
- [x] Delete duplicate metadata caches, unused tab options, and any second
  navigation/scroll/event authority introduced during implementation.

Performance targets to verify, not current measurements: warm tab selection to
paint p95 under 100 ms on the development Mac; active event-to-view behavior no
worse than the current baseline; at most four retained root views; one summary
poll per two visible seconds in steady state; zero background polling while
hidden; tab metadata within the stated bounds. Record cold hydration and tails
separately instead of hiding network/replay work behind a warm-switch average.

## Completion criteria

Multiple root sessions remain open in a clear, themed top strip. Switching restores
the relevant conversation context and unsent work. Closing affects presentation
only. Background child activity and human requests are understandable without
opening every root. Browser history, independent windows, reload, and daemon
reconnect behave predictably. Resource usage stays bounded as the working set grows.

The design study is a synthetic layout/interaction illustration, not production
code, an SDK integration, or acceptance evidence. It demonstrates selection,
closing/reopening, the session picker, and light/dark/mobile composition; the
implementation must use the existing packages and all-theme tokens above.

Design-study validation: Chromium exercised selection, draft retention across a
switch, closing/reopening, theme controls, and filtered mobile session selection.
Desktop light/dark and 390 px mobile screenshots were inspected; the study had no
page errors or document-width overflow. These checks do not establish production
accessibility, daemon behavior, or completion of the implementation gates.

## Implementation and acceptance record — 2026-09-07

Implemented all five code phases. No database migration, authentication path,
second transcript reducer, offline queue or additional client connection was added.
The UI sorting spike passed, so pointer reordering ships alongside explicit movement
commands. `@whip/ui/workspace-tabs` is a separate public subpath: ordinary UI imports
do not initialize browser-only sorting observers.

[Machine-readable acceptance](acceptance.json) records measurements and boundaries.
Repeat with:

```sh
task check
task acceptance
npm run test:tabs -w @whip/ui
npm run test:packed -w @whip/ui
npm run build:storybook
npm run build:web
node scripts/pack-web.mjs
node apps/web/scripts/session-tabs.mjs
node apps/web/scripts/browser.mjs
node apps/web/scripts/safari.mjs
node apps/web/scripts/performance.mjs
```

The tab suite covers real daemon streaming across switches, retained uploads through
close/reopen, two-client permission resolution while the root is in the background,
32-tab bounds, a blocked 33rd deep link, explicit missing-root status and scoped
errors, hidden polling, restart/reconnect, mobile picker, and independent windows.
The loaded-history fixture covers 10,000 root messages, 100 children, four history
prepends, stable selection, independent root/child anchors, 32 near-limit drafts and
16 concurrent fake agents. The full workflow suite and actual Safari round-trip,
theme, tab, close/reopen and reload checks pass. All 65 theme fixtures pass Axe;
component tests cover keyboard, pointer threshold/cancel, sibling action ownership,
zoom, RTL, overflow and strict CSP. Packed source installation and Storybook pass.

Review found and fixed storage-error reentrancy during close, delayed per-root
errors leaking to another tab, focus loss after portaled-menu closure, and Previous
from Home selecting the wrong tab. Late composer acceptance now clears only the
matching draft, including when the same recipient was closed and reopened. Relevant
state transitions have regression coverage; no automatic execution retries were added.

The running development daemon was rebuilt and restarted while idle on its existing
endpoint, preserving runtime identity and data. The web app remains on port 3000.
Physical mobile devices and manual VoiceOver are still release checks, not claimed
as completed by browser emulation or Axe. Physical-paint latency is likewise not
certified by the automation-inclusive timings in the report.
