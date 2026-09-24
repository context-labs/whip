# Full-window Settings and Appearance

Branch: research on `codex/desktop-release`; source baseline `40699639bb6f3096daa6cefb69968877f25a801a`.

Status: Implemented and verified, 2026-09-08. All five phases complete. The user explicitly waived the manual VoiceOver check; it is not claimed as passed.

## Goal and confirmed decisions

Make Settings a dedicated, full-window destination in the shared web/Electron
app, with its own navigation and a useful Appearance page inspired by the
provided Cursor screenshot. A person can adjust how they read and direct Whip,
then return to the exact workspace they left.

The user confirmed:

- Settings replaces the conversation sidebar and tab strip while it is open.
- A Back button returns to the workspace.
- Add working density, wrapping, typography, and accessibility controls, beyond
  restyling the existing theme controls.

This supersedes the unimplemented [Settings-as-a-workspace-tab proposal](../settings-workspace-tab/README.md).
Settings will not become a tab descriptor or participate in pane splitting.
“Full-window” describes the app layout; it does not enter macOS fullscreen or
create another window.

Scope is desktop and the shared browser renderer, including narrow browser
layouts. The native Expo app has a different renderer and is outside this change.
Appearance remains a preference of the viewing installation/browser origin;
host configuration continues to be shared by clients of that host.

## Research findings

| Current implementation | Consequence for this work |
| --- | --- |
| [`settings.tsx`](../../../packages/app/src/settings.tsx) has five horizontal sections: Appearance, Providers, Runtime, Device, Recovery. | Replace the navigation and reorganize existing controls; do not discard current setup/recovery capabilities. |
| [`shell.tsx`](../../../packages/app/src/shell.tsx) always supplies the conversation sidebar and tab strip. | Choose workspace or Settings chrome at the shell boundary while retaining the application runtime and global services. |
| [`session-tab-strip.tsx`](../../../packages/app/src/session-tab-strip.tsx) already releases visible session consumers on non-session routes. | Preserve that observation policy. Keep tab layout, drafts, attachments, and reading bookmarks without mounting hidden conversations. |
| Appearance currently offers a theme picker and host-resolved custom themes. | Theme selection is already implemented. New typography and display controls need actual renderer support. Custom import needs an explicit source host. |
| Device settings combine shortcuts, attention announcements, native notifications, and desktop updates. | Separate General from About & updates; retain platform capability checks. |
| Runtime defaults save a revision-checked form; provider keys/login inputs are ephemeral. | Category splitting must retain conflict handling and submit only the fields owned by that form. Never persist credentials or unsaved host configuration. |
| Recovery combines local saved drafts with host-bound command records behind a host availability gate. | Make local draft recovery available offline; gate only the host-dependent portion. |
| [`tokens.stylex.ts`](../../../packages/ui/src/tokens.stylex.ts) centralizes font families, but many components use literal pixel sizes. | Changing the root font size alone will not implement UI font size. Introduce semantic size roles and update their consumers. |
| [`timeline.tsx`](../../../packages/app/src/timeline.tsx) collapses tool, reasoning, and mailbox rows with native disclosures. | Add a tool-detail presentation preference without changing stored transcripts, fetching bodies automatically, or exposing reasoning by default. |
| [`CodeBlock`](../../../packages/ui/src/code-block.tsx) provides bounded highlighting; Markdown normally uses it, with a separate fallback `pre`. | Wrapping and code typography must cover both paths, raw tool output, and the REPL/inspectors. Preserve copy/download bytes and output bounds. |
| The catalog has diff colors, but [`code-highlight.ts`](../../../packages/ui/src/code-highlight.ts) has no diff grammar and the web app has no dedicated diff review surface. | Defer the screenshot's diff-background setting until there is a real consumer. A preview alone does not justify a setting. |
| Base UI 1.8.0, StyleX, Lucide, Inter, and JetBrains Mono are already installed. `SettingsRow`, `NumberField`, `Switch`, `Select`, and `Combobox` exist. | Reuse the current design system. Add only a small Slider wrapper if needed; no new component library. |

The [frontend guide](../../../docs/frontend.md), [feature map](../../../docs/features.md),
and [roadmap](../../../docs/roadmap.md) define the current boundaries. Prior
[web research](../web-app/README.md) and [OpenCode UX notes](../../../docs/learnings/other-harnesses/opencode/opencode-ux.md)
support quiet surfaces and progressive disclosure; the supplied screenshot is
the visual reference for this feature. The implementation should not import
the reference application's unrelated IDE/account categories.

### External sources and what they establish

- [Base UI Slider](https://base-ui.com/react/components/slider): discrete steps,
  accessible labels, keyboard interaction, and change/commit callbacks are
  available in the library we already use.
- [Base UI Number Field](https://base-ui.com/react/components/number-field):
  compose labeled decrement/input/increment controls for font sizes using the
  existing Whip wrapper.
- [Electron nativeTheme](https://www.electronjs.org/docs/latest/api/native-theme):
  exposes high-contrast and reduced-transparency preferences and change events.
  `inForcedColorsMode` is Windows-specific; it is not the macOS Increase Contrast
  signal. The installed Electron declarations include the relevant properties.
- [MDN prefers-contrast](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/prefers-contrast):
  browser contrast preference detection. Provide a manual choice as well.
- [MDN prefers-reduced-transparency](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/prefers-reduced-transparency):
  support is limited across browsers; do not promise universal automatic detection.
- [MDN font-smooth](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/font-smooth):
  this is nonstandard and platform-dependent. Omit a general Font Smoothing
  switch from the initial product surface.
- [W3C Reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html) and
  [Resize Text](https://www.w3.org/WAI/WCAG22/Understanding/resize-text.html):
  validate narrow layouts and text enlargement without losing controls or content.

## Visual direction

The person using this screen is moving briefly out of an active coding
conversation to improve readability, configure a model, or repair a connection.
Settings should feel like the quiet control area of the same workspace.

Design exploration:

- **Domain:** execution hosts, recursive agents, conversations, Starlark cells,
  model routes, reading position, and human attention.
- **Color world:** graphite canvas, charcoal panels, warm gray labels, white code
  text, amber attention, green success, and red failure. These describe semantic
  roles, not new hardcoded colors; the selected Whip theme supplies every value.
- **Signature:** a small live conversation sample showing a Whip tool execution,
  selectable Starlark code, and a readable response. It demonstrates what each
  display setting changes. Host labels on shared settings reinforce Whip's
  multi-machine workflow.
- **Defaults replaced:** horizontal tabs become dedicated navigation; a separate
  box around every control becomes grouped rows; generic account/IDE categories
  become categories supported by Whip's actual capabilities.

Layout proposal at desktop sizes:

```text
┌─────────────────────────┬────────────────────────────────────────────────────┐
│ native window controls  │                                                    │
│ ← Back to workspace     │             Appearance                             │
│                         │                                                    │
│ Search settings         │             Theme                                  │
│                         │             [ Color theme       Claude Code  ▾ ]   │
│ General                 │                                                    │
│ Appearance              │             Conversations                          │
│                         │             [ Tool details    Compact ── Detailed ]│
│ Providers & models      │             [ Wrap code                     ○─ ]   │
│ Agents & execution      │                                                    │
│ Connections             │             Typography                             │
│                         │             [ UI size                 − 13 + ↺ ]   │
│ Recovery                │             [ Code size               − 12 + ↺ ]   │
│ About & updates         │             [ Font families                   ▾ ]   │
│                         │             [ Live conversation / code preview ]   │
│                         │                                                    │
│ Whip · version          │             Accessibility                          │
└─────────────────────────┴────────────────────────────────────────────────────┘
```

- Sidebar around 248 px; main column up to 840 px. Use fluid outer gutters
  (roughly 32–64 px), independent navigation/content scrolling, and enough top
  inset for native traffic lights and a draggable title region.
- Sidebar and content belong to one theme. A fine vertical border separates
  them; use the existing navigation role only where the chosen theme calls for it.
- Group panels use the existing panel surface, 12 px corners, 16 px padding,
  quiet internal dividers, and rows that grow with content. Avoid fixed heights
  that clip larger text. No panel shadows.
- Labels and descriptions align left; controls align right at a consistent
  width. Selected navigation gets a quiet fill and an accessible current-page
  indication. Icons aid scanning without replacing labels.
- Under 768 px, show Back and a category selector that opens the existing Sheet.
  Stack controls beneath descriptions when needed. Retain 44 px touch targets
  and at least 16 px editable text on touch layouts.
- Loading, disconnected, invalid, saving, and failed states live beside their
  affected controls. Do not let a disconnected host block local Appearance.

## Categories and complete mapping

| Category | Contents | Ownership and save behavior |
| --- | --- | --- |
| **General** | Command/composer shortcuts; screen-reader attention announcements; existing desktop notification preference where supported | Current device preferences; apply immediately |
| **Appearance** | Theme/custom themes; tool details; code wrapping; UI/code fonts and sizes; accessibility; live preview | Viewing device/origin; apply immediately; custom import names its source host |
| **Providers & models** | Provider credentials/login/status; default provider/model/reasoning effort | Selected execution host; explicit actions and Save |
| **Agents & execution** | Compaction provider/model/threshold; goal rounds; retries; Claude/Codex configuration import toggles under an Integrations subsection | Selected execution host; explicit Save |
| **Connections** | Existing execution hosts, connection state, add/edit/remove, Test Connection, and This Mac runtime setup/diagnostics | Reuse connection managers and HostDialog. Saved host profiles remain owned by Local; native runtime selection remains a desktop capability |
| **Recovery** | Saved drafts on this device; command recovery for the selected host | Local and host sections remain distinct; preserve existing confirmation/status/forget semantics |
| **About & updates** | Whip version, existing update state/check/restart flow and available build diagnostics | Native controls only where supported; browser shows only information it can actually obtain |

Do not add empty Profile, Plan & Usage, Cloud Agents, Git & PRs, Worktrees,
Code Intelligence, or Beta pages. Existing notifications are being moved, not
expanded into new notification infrastructure.

Host-backed pages always show the target host's name and connection state near
the heading. Changing that host must not change the local appearance preferences.
Connections explains when profile edits require Local, while attached remote
hosts can continue working independently.

### Navigation and search

- Keep `/settings` as the route. Validate a shared allowlist of `section`, optional
  `host` profile ID, and optional `setting` anchor. Update every internal caller
  and command-palette entry to the same definitions.
- Normal entry pushes one history entry and captures the previous in-app
  location, focused view, and focus return target. Section/host changes replace
  that entry, so Back returns to work rather than traversing every category.
- Back returns to that validated workspace location. Direct entry/reload uses
  retained workspace selection as a fallback, then New session if none exists.
  A closed/deleted target falls back safely; no arbitrary external return URL.
- Persist only bounded navigation metadata within the existing window storage
  boundary if needed for reload. Never serialize DOM nodes, forms, or secrets.
- On Electron, Cmd-W while in Settings returns to the workspace. It must not
  close a hidden conversation. Overlay handling comes first. Browser Cmd-W
  retains native behavior. Escape dismisses the top overlay and does not silently
  discard a dirty form.
- Search uses a small static registry of labels, descriptions, keywords,
  category IDs, setting IDs, and capability requirements. It is navigation
  metadata, not a new schema-driven form engine.
- Results show category/context; selecting one reveals and focuses the setting.
  Search covers only available settings and performs no remote scans, credential
  searches, or eager provider queries. Missing hosts remain explicit rather than
  falling back silently to a different machine.
- Host forms with unsaved edits offer Save, Discard, or Stay before category,
  host, Back, or other in-app navigation loses the form. Never auto-save provider
  secrets. Preserve explicit login cancellation and unmount cleanup.

## Appearance controls and exact behavior

### Theme

- Compact, searchable theme selector with System and all existing built-in and
  imported themes. Retain hover/keyboard preview, commit, Escape cancellation,
  and before-paint theme restoration.
- Reuse the current ThemeProvider and picker internals; offer a compact popover
  presentation instead of opening a second Appearance dialog over this page.
- Put custom themes in a secondary expandable area: import JSON and browse a
  named host's themes. The host is a source, not the owner of the appearance
  preference. Previously resolved themes and built-ins continue working offline.
- Preserve JSON size limits, daemon resolution/validation, built-in protection,
  and visible storage/import failures. Theme imports do not accept executable CSS.

### Conversations

| Control | Proposed default | Behavior |
| --- | --- | --- |
| Tool call density | Compact | Three discrete values: Compact, Comfortable, Detailed. A labeled slider announces the selected word, not an unexplained number. |
| Code block word wrap | Off | Wrap long lines in read-only code/tool output throughout the shared app. Preserve indentation and original copy/download text. |

Density changes tool presentation only:

- **Compact:** summary and available status; details closed, closest to today's view.
- **Comfortable:** summary plus a short preview from already-loaded output
  (at most three lines / 512 characters); explicit expansion reveals details.
- **Detailed:** reveal already-loaded, bounded tool arguments/output by default
  within the existing scroll/byte limits. Bodies behind content handles still
  require an explicit read.
- All modes retain status/error notices and human-action controls. Reasoning and
  mailbox disclosures keep their existing defaults. No model request, prompt,
  transcript retention, or permission behavior changes.
- An explicit expand/collapse choice wins over the default while that row is
  mounted. Do not introduce an unbounded map of historical disclosure choices.
- Use the real timeline's presentation for the inert preview. The preview has
  synthetic bounded data, no SDK client, and no active work/permission buttons.

Apply code wrapping to CodeBlock, Markdown's fallback `pre`, raw tool-result
preformatted text, content readers, and REPL/inspector code. Do not wrap unrelated
structured tables or change the native mobile renderer.

### Typography

| Control | Initial choices / range | Default |
| --- | --- | --- |
| UI font size | 12–20 px, integer stepper, direct input, reset | 13 px base |
| Code font size | 10–24 px, integer stepper, direct input, reset | 12 px base |
| UI font family | Inter; System | Inter |
| Code font family | JetBrains Mono; System monospace | JetBrains Mono |

These are proposed product ranges, not a substitute for browser zoom or OS text
enlargement. Provide per-size reset and a clearly scoped Reset appearance action.
Reset restores theme/display defaults but retains imported theme definitions,
host configuration, connections, notification choices, and drafts.

Implementation must introduce semantic typography size/line-height roles.
At default values, preserve the existing hierarchy: roughly 13 px chrome,
14 px conversation text, 12 px code, and smaller metadata. Scale UI/reading roles
proportionally from the UI base; code size remains independent. Inline code
follows surrounding text size while using the chosen mono family.

Audit all shared controls, navigation, conversation/composer, tool output,
REPL/inspectors, dialogs, and portaled menus. Replace relevant literal sizes
with roles, and let control heights grow. Do not implement this using CSS zoom,
transforms, or just a root font size that leaves pixel-sized consumers unchanged.
Continue using self-hosted or system fonts; arbitrary font URLs and installed-font
enumeration are unnecessary.

The preview should contain a short assistant message, a tool summary/result,
and highlighted Starlark code with a long line. All controls change that sample
and the actual application immediately.

### Accessibility

Initial controls:

- **Contrast:** Follow system (default), Standard, Increased. Increased contrast
  strengthens text, control edges, focus, and selection differentiation across
  shared surfaces while preserving the selected base theme. Returning to Standard
  or a normal system setting restores that base theme.
- **Reduce motion:** Follow system (default) or Reduce. Apply to control and
  overlay transitions, progress movement, and tab drag/reorder animations.
  Essential running-state information remains visible without animation.
- Retain attention announcements in General, with search keywords making them
  discoverable from “accessibility” and “screen reader.”

Extend the existing web theme adaptation for increased-contrast role overrides;
do not edit the generated catalog or create a second theme system. Use explicit
contrast acceptance measurements and visibly distinct focus/selection states.
Retain the current normal-text contrast floor; target 7:1 for primary reading
text in Increased mode, with at least 3:1 for meaningful control boundaries and
focus indicators. These are implementation targets to verify across actual
rendered combinations, including syntax text on code backgrounds.
Respect browser forced-color palettes independently of the preference; do not
disable them globally. Status must remain understandable through text/icons.

Browser detection uses media queries. On desktop, pass only public, read-only
native preference flags and change notifications through the platform bridge
when needed for reliable macOS detection. Keep Electron imports in the desktop
main/preload layer and dispose subscriptions. Do not conflate high contrast with
dark mode or create a feedback loop by overriding `nativeTheme.themeSource`.

### Deliberate exclusions from the screenshot

- **Hue and intensity:** defer. Whip already supports complete themes; tinting
  every palette introduces contrast and semantic-color interactions without
  improving the requested practical controls.
- **Themed diff backgrounds:** defer until a real diff presentation is in scope.
- **Font smoothing:** omit initially because browser controls are nonstandard
  and platform-dependent.
- **Reduce transparency switch:** defer. Current primary surfaces are opaque;
  the translucent modal backdrop is not a strong reason for a separate setting.
  Use opaque Settings surfaces, and revisit this control with actual translucent
  surface design rather than adding effects just to make a switch useful.

## State, architecture, and lifecycle

| State | Owner |
| --- | --- |
| Theme selection, preview, resolved/custom themes | Existing UI ThemeProvider and theme storage |
| Generic display preferences: fonts, sizes, wrapping, contrast, motion | Extend the same UI appearance context; one validated, bounded display-preference record |
| Tool call density | Existing app `DevicePreferences` in `runtime.ts`; product-specific behavior stays out of UI |
| Section/host/setting location | TanStack Router; one shared category registry |
| Workspace and return location | Existing window-local workspace/navigation ownership; no Settings tab/store |
| Provider/runtime configuration and host identity | Existing SDK services, connection managers, and runtime-scoped Query reads |
| Unsaved host fields, credentials, search text | Ephemeral component state; never written to appearance storage |

Suggested generic record shape (UI package, no SDK dependency):

```ts
type DisplayPreferences = {
  uiFont: 'inter' | 'system';
  codeFont: 'jetbrains-mono' | 'system';
  uiSize: number;
  codeSize: number;
  wrapCode: boolean;
  contrast: 'system' | 'standard' | 'more';
  motion: 'system' | 'reduce';
};
```

- Use a versioned display key such as `whip.appearance.display.v1`, bounded to
  4 KiB. Validate finite integer ranges/enums and fall back to defaults on invalid
  data. Keep existing theme selection/custom-theme storage intact.
- Retain the existing storage boundary rather than inventing cross-device
  synchronization. Changes apply immediately in the current window and restore
  from that platform's storage on the next launch; multi-window propagation can
  follow existing storage-event behavior without creating a new sync service.
- Initialize display settings alongside `initializeTheme` before React mounts,
  using the same parser and apply function as the provider. Storage failure must
  produce an actionable notice; current-window changes may still apply.
- Apply fixed, validated CSS variables/static variants through the existing
  theme mechanism. Preserve the production CSP and portal inheritance; do not
  inject arbitrary stylesheets or accept raw CSS from saved preferences.
- A single Appearance screen composes the UI context and app density preference.
  Reset calls those existing owners; it does not introduce another preference store.
- Settings navigation must not recreate AppRuntime, QueryClient, SDK clients,
  notifications, or connection managers. Global attention and native prompts
  remain reachable; transcript consumers release while the workspace is hidden.
- Preserve reading anchors when text geometry changes: remeasure visible rows,
  restore the same message/offset, and retain follow-latest only if it was already
  enabled. Do not scroll an older-history reader to the end.
- Only the mounted category observes its relevant data. Provider login polling
  remains scoped to an active flow. Search/preview never hydrate conversations.
- Split host configuration writes into form-owned patches using the existing
  revision precondition. A stale revision keeps the draft and asks for review;
  never silently retry over another client's changes.
- No daemon database migration, new backend configuration fields, or wire
  protocol change is expected. A native accessibility bridge addition is a
  desktop contract change and must follow its existing versioning/validation rules.

## Implementation phases

### Phase 1 — Dedicated Settings shell

- [x] Add the full-window shell with Back, category navigation, responsive Sheet,
  independently scrolling content, and native draggable/inset regions.
- [x] Route Settings outside workspace chrome while retaining runtime/global UI.
- [x] Implement return navigation, direct-entry fallback, focus restoration, and
  Electron Cmd-W behavior; keep existing section contents working initially.
- [x] Add focused navigation/close-tab/lease-lifetime regression coverage.

Exit: Settings fills the app window; returning preserves split layout, active
host/child/view, drafts, attachments, and reading position. No Settings tab exists.

### Phase 2 — Categories, search, and settings ownership

- [x] Extract the current large settings module into focused category components
  and a small shared navigation registry; avoid a generic form framework.
- [x] Move every current control according to the category table.
- [x] Add local setting search, links to specific controls, and shared command entries.
- [x] Make host identity explicit; reuse host-management and runtime-setup flows.
- [x] Separate offline draft recovery from host command recovery.
- [x] Preserve revision conflicts, scoped configuration patches, login cleanup,
  and unsaved-form handling across category/host/Back navigation.

Exit: Every existing setting remains reachable; search finds it; offline local
settings work; edits demonstrably affect the intended host only.

### Phase 3 — Appearance layout and preference foundation

- [x] Build grouped Theme, Conversations, Typography, and Accessibility sections.
- [x] Reuse SettingsRow/NumberField and add a small accessible Slider primitive
  with StyleX and Storybook coverage.
- [x] Add compact theme picker presentation and explicit custom-theme source host.
- [x] Extend appearance initialization/persistence/validation/reset; keep the
  generated theme catalog and existing theme semantics.
- [x] Compose a bounded live preview using actual shared presentation components.

Exit: Theme selection/import works in the new layout, saved appearance initializes
before paint, portals inherit it, and persistence errors are visible. Expose each
new control only when its renderer behavior lands in the next phase.

### Phase 4 — Functional reading and accessibility preferences

- [x] Introduce typography roles and wire UI/code fonts and sizes to all consumers.
- [x] Wire code wrapping across Markdown, tool output, content readers, and REPL.
- [x] Add app-owned tool density with bounded, fetch-free rendering.
- [x] Preserve virtual reading anchors across font/wrapping/density changes (existing implementation retained; Chromium/Firefox production checks report 0.00px drift).
- [x] Implement contrast and motion preferences, OS detection, and the minimal
  desktop preference bridge where required; include tab animation consumers.
- [x] Verify per-control/default reset, reload, disconnected use, and live preview.

Exit: Every visible Appearance control changes actual app behavior. Important
errors/questions remain visible; no extra transcript/body requests appear.

### Phase 5 — Acceptance and documentation

- [x] Complete keyboard, zoom/reflow, production CSP, browser, native
  window-chrome, and multi-host acceptance below. Manual VoiceOver was explicitly
  waived by the user after they confirmed they were turning it off.
- [x] Run required repository checks and focused tests; review the changes for
  correctness and unnecessary state/dependencies.
- [x] Update the frontend guide's navigation, typography, appearance ownership,
  accessibility, and settings guidance to describe the implemented architecture.
- [x] Update `docs/features.md` with behavior → files → tests, add/complete the
  roadmap entry, and record actual evidence in this plan.
- [x] Validate a packaged desktop build alongside the shared web artifact. Use
  the existing release/install process when a local upgrade or release is requested.

Exit: Shared web/desktop implementation is verified with recorded evidence and
accurate docs. Native mobile parity and deferred cosmetic controls remain separate.

## File map

Paths for new modules are proposed; keep related code together and avoid tiny
one-use abstractions.

| Area | Likely files |
| --- | --- |
| Shell, route and entry points | `packages/app/src/shell.tsx`, `routes/settings.tsx`, `navigation.ts`, `session-sidebar.tsx`, `welcome.tsx`, `model-selection.tsx`; inspect `session-tab-routing.ts` for route/return behavior |
| Settings composition | `packages/app/src/settings.tsx` becomes a thin entry; new `settings/{layout,navigation,appearance,general,providers,execution,connections,recovery,about}.ts(x)` as warranted |
| Shared controls | `packages/ui/src/presentation.tsx`, `forms.tsx`, `styles.stylex.ts`, `index.ts`; new `slider.tsx` and associated story/tests |
| Display preferences and theme picker | `packages/ui/src/themes.tsx`, `tokens.stylex.ts`, `theme-data.ts` only if pure contrast adaptation needs extension; proposed `appearance-data.ts` for bounded parsing/defaults |
| Application preferences | `packages/app/src/runtime.ts`, Appearance and timeline consumers |
| Typography, wrapping and motion | `packages/ui/src/{code-block,workspace-tab-drag}.tsx`, shared UI styles; `packages/app/src/{styles,timeline,repl-view,reading-list}.tsx/ts` and actual composer/inspector consumers discovered by the typography audit |
| Before-paint initialization | `apps/web/src/bootstrap.tsx` and UI initialization exports |
| Native accessibility signals | `packages/app/src/{platform,desktop-bridge}.ts`, `apps/web/src/platform/desktop.ts`, `apps/desktop/src/{main,preload}.ts` |
| Tests | Existing app navigation/runtime/settings/desktop-adapter tests, UI theme/component/CSP tests, a focused `apps/web/scripts/settings.mjs` workflow, desktop bridge tests |
| Docs | `docs/frontend.md`, `docs/features.md`, `docs/roadmap.md`, this plan and the superseded Settings-tab proposal |

## Acceptance and test plan

1. **Workspace preservation:** enter from chat, child, REPL, multi-host split,
   empty workspace, and command palette. Back/Forward, deep links, reload, and
   repeated opening preserve the intended place. Cmd-W does not close hidden
   tabs. Verify no hidden root leases and no accidental daemon cancellation.
2. **Settings ownership:** test two hosts with distinct defaults, host disconnect
   during edit/login, stale revisions, category changes with dirty fields, and
   Local unavailable. Confirm patches cannot overwrite another category's fields.
   Local Appearance and draft recovery remain usable offline.
3. **Actual preferences:** exercise all density values, live/finished/failed tool
   rows, oversized body handles, manual disclosures, code wrap/copy, resets,
   invalid saved values, storage failures, reload, theme preview cancellation,
   and custom import errors. Assert no additional SDK reads caused by density.
4. **Typography and scroll:** test min/default/max sizes, both font families,
   long code lines, many tool rows, streaming, dialogs, menus, and REPL. Test an
   older-history anchor and follow-latest separately. Cover delayed font loading
   and visible-row remeasurement without losing selection or jumping to the end.
5. **Themes and accessibility:** run semantic contrast checks across the complete
   generated catalog and representative custom themes; visually inspect light,
   dark, Claude Code, and strong-color themes in Standard/Increased modes.
   Verify focus, non-color status cues, screen-reader labels, slider keys,
   steppers, theme-picker Escape, search results, and OS preference changes.
6. **Responsive/native:** inspect wide desktop, 1024 px, 768 px boundary, 390 px,
   and 320 CSS-pixel reflow; 200% text enlargement and 400% browser zoom. Native
   macOS traffic lights, drag regions, resize, and Back must work. Perform actual
   VoiceOver and macOS Increase Contrast/Reduce Motion checks; do not label them
   passed based only on emulation.
7. **Production boundaries:** build the real web artifact and test strict CSP
   in Chromium/Firefox and actual Safari for relevant style/portal behavior.
   Exercise a packaged Electron build and verify the native bridge adds only
   validated preference data. UI has no SDK/router/Electron imports.

Relevant existing commands include `npm run check:web`, `npm run test:web`,
`npm run check -w @whip/ui`, `npm run test -w @whip/ui`, UI browser/CSP checks,
and `npm run check:desktop` / `npm run test:desktop` if the bridge changes.
Run `task check` as required by the feature workflow. Broaden tests when a
shared dependency or failure warrants it. Record actual results below.

## Main risks and completion boundary

- Typography is a cross-app change because current styles contain pixel literals.
  The consumer audit and virtual-scroll checks are required work, not optional polish.
- Shell changes can accidentally unmount the runtime or invoke hidden-tab actions.
  Keep ownership above the route switch and test native close behavior explicitly.
- Category splitting can create wrong-host writes or stale whole-form updates.
  Retain runtime-scoped identity, revision checks, and form-owned patches.
- A new theme preview must not become a second renderer or fetch agent data.
  Reuse production presentation with inert, bounded examples.
- OS contrast, forced colors, dark mode, and reduced motion are distinct signals.
  Test each explicitly and preserve the chosen base theme.

The plan is complete when reviewed; implementation completion requires all five
phase exits and recorded acceptance. There are no remaining scope questions
blocking implementation planning.


## Implementation record — 2026-09-08

- Phases 1–4 implemented using existing package boundaries; no new dependencies.
  Settings uses one route-level guard and a small static category/search registry.
- Review fixes include pinning implicit host selection through disappearance,
  preserving disabled drafts after identity changes, exact dirty-value equality,
  focusing editable search targets, and hiding inapplicable workspace commands.
- `npm run test:web`: 344 tests across 41 files passed. `task check` passed,
  including Go formatting/vet/whipvet/tests, frontend build/tests and asset checks.
- `npm run check:desktop` and `npm run test:desktop` passed. The native adapter
  covers initial contrast reads, updated events, stale-read ordering, malformed
  events and disposal. Scheme/CSP probe passed with desktop bridge v2.
- Chromium and Firefox each passed six production Settings workflows: dedicated
  shell, draft/reload return, functional persisted controls, guard behavior, all
  categories/search/reset, and 1024/768/767/390/320px navigation/reflow.
- UI verification: 66 themes/13 interaction scenarios, contrast checks, strict
  CSP in Chromium/Firefox/actual Safari, and desktop/touch/RTL tab behavior.
  Existing visual comparisons passed after inspecting two narrow baseline updates.
- Visual inspection found two follow-up issues: insufficiently visible slider
  track in Claude Code and a clipped font-stepper value at 320px/UI20. Both were
  corrected; final screenshots show the visible track and unclipped values.
- Final production renderer: `a4be825d93afa12793f7b1544117a02b7330a92c1af05a34777253bf967b2cb1`.
  Both browsers passed six Settings workflows and eleven real-conversation
  checks against this artifact. Conversation scripts hash-check every served asset.
  Older-history anchor drift was exactly 0.00px; child/REPL/split identity, focus,
  isolated drafts, and follow-latest survived the round trips. Detailed tools
  produced no implicit HTTP/RPC content reads. The 1.52MB body retained its handle,
  the 1MiB preview guard held, and explicit download made exactly one HTTP read.
- Signed macOS arm64 package 0.1.3 / `local-settings-40699639b` built and verified
  with the exact shared renderer. Bundle digest:
  `8b57fb3850bd9547bb97bffdf876924c88aded4ff1d3fe1ce9b678cf7d59952b`.
  Isolated native launch, traffic-light layout, title-bar drag interaction and
  Cmd-W returning from Settings to Home were exercised. No installed app was replaced.
- Actual macOS Increase Contrast was observed live in the signed package and
  through staged production main/preload IPC. Actual Reduce Motion changed the
  renderer media query and transition duration from 100ms to the shared 0.01ms
  completion-event fallback. Native 400% zoom at 1280px fit a 320 CSS-pixel layout.
  Both OS preferences, and the automatically changed transparency preference,
  were restored to their original off values.
- VoiceOver's control tree was inspected, but the spoken-navigation manual check
  was **skipped at the user's explicit request**. Do not count it as passed.
- Final rerun: all 344 app tests and 14 UI unit tests passed after the visual fixes.
  Native suite: 71 tests; release/tooling suite: 88 tests; startup-probe self-test
  passed. `task check` passed before the final isolated slider/stepper styling
  fixes; final type/build/UI/browser/native checks cover those changes.
- Evidence: [browser Settings](evidence/settings-chromium.json),
  [Chromium conversations](evidence/conversation-chromium.json),
  [Firefox conversations](evidence/conversation-firefox.json),
  [native OS signals](evidence/native-signals.json),
  [signed package](evidence/signed-package.json), and
  [desktop](evidence/appearance-dark.png) / [narrow](evidence/appearance-narrow.png) screenshots.
- The concurrent chat-activity and openai-subscriptions work is outside this
  change. Its files and plan artifacts were preserved; the artifact digest above
  identifies the Settings implementation that was verified.

### Appearance polish — 2026-09-09

- Custom theme import now shares the top theme group and uses only the home
  connection. Removed the execution-host selector. The shared import dialog has
  a file action, inline validation errors, and cancellation on dismissal.
- The compact picker uses one searchable Base UI popup, opens with an empty
  query, retains the selected checkmark, and shows background/primary/accent
  palette swatches. Settings Back fills the sidebar content width.
- Verified eight Settings workflows each in Chromium and Firefox, including
  import errors/retry/persistence, preview cancellation, focus return, and reflow
  down to 320px. No runtime or CSP errors. Import cancellation and navigation
  unit tests passed; the coordinated full `task check` also passed (361 app tests).
- Signed local desktop 0.1.3 / `local-theme-polish-91ccde93` uses renderer
  `91ccde93e546d443cace37e4df9bdec93ec4bffca2ae9ee74114c06f88b80582`.
  Updated `/Applications/Whip.app` to the same verified bundle. The running
  desktop restored the workspace and reconnected successfully.
- The previously manually managed `/usr/local/bin/whipcode` required the current
  protocol minor version. All 27 agents were idle before replacing it via the
  verified runtime updater. The matching bundled binary is now desktop-managed;
  sessions and configuration remain in the existing `~/.whipcode` home.

### Swatch previews — 2026-09-09

- Replaced equal-weight background/primary/accent strips with miniature previews
  of the theme's background, panel, foreground, and code colors, using
  `adaptThemeForWeb`. Rows now show selection, name, Light/Dark, then preview.
- Eight Settings workflows pass in both Chromium and Firefox; UI TypeScript
  check and signed package verification pass. Also verified the new picker in
  the installed desktop. Built and installed `local-swatches-e99a173c` with
  renderer `e99a173c98c13e6221a2981fc437a6496aec57e8d6f64527460b2be5e873720c`.
  `/Applications/Whip.app` and the canonical managed backend match this build.
- Some light catalog entries (including Aura Light and Ayu Light) currently
  duplicate dark source palettes. Previews expose the actual palette; these
  catalog definitions need separate correction.
- Restart lesson: checking that all agents report idle is insufficient. An
  active turn in “Logo theming research plan” was interrupted by the backend
  restart despite all 30 agents reporting idle. Check active turns as well
  before future local upgrades; saved work remains intact.

### Theme picker refinement — 2026-09-09

- Replaced the accent underline with an inset search field and a neutral focus
  border. Moved empty-state padding inside the conditionally rendered message,
  removing the blank gap while preserving Base UI's mounted live region.
- Swatches now depict a small WHIP workspace: actual navigation and canvas,
  primary accent, conversation text, and composer. Applied themes and swatches
  share `browserSurfaces` and the resolved contrast preference; syntax colors
  no longer dominate the preview. Selected previews stay stable during browsing.
- UI typecheck and 14 UI tests pass. Nine Settings workflows pass in Chromium
  and Firefox, including exact Claude Code colors, neutral search focus, the
  empty-state gap, keyboard preview cancellation, and 320px/UI20 reflow.
  No runtime or CSP errors. Evidence is in [theme-picker](evidence/theme-picker/).
- Signed and installed desktop 0.1.3 / `local-theme-picker-6d99e1b2`, with shared
  renderer `6d99e1b26b237e0fda598754b0d579afe9041cb84a4aba3340715500b68d8542`.
  The managed daemon is generation 9 at `http://127.0.0.1:8080`. All 23 served
  files match the desktop renderer. No active turns or agents at restart;
  the interrupted-turn count stayed unchanged.
