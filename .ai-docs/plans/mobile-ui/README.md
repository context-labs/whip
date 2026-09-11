# Whip mobile UI implementation

Branch: `codex/mobile-ui` (from `feature/agent-definition`).

Status: implementation delivered, 2026-09-11, with one commit per phase. Android native flows and all 66 theme previews were exercised; 160 mobile tests, both platform exports and repository checks pass. iOS native, physical-device accessibility and performance acceptance remain release gates. Actual validation is recorded in [EVIDENCE.md](EVIDENCE.md).

## Goal

Implement the complete [Paper mobile storyboard](https://app.paper.design/file/01M1YJSY1KZZMV9R11WBK1WBAN/2-0): host onboarding and management, Sessions as home, host/folder/session creation, and a polished conversation experience. Build a reusable native component library and support every web theme, including custom themes, through a dedicated Settings → Appearance page.

Keep Cursor's reading hierarchy, restrained controls, and reachable composer, with Whip's typography, semantic colors, host identity, and execution semantics. The screenshots are visual references; their conversation text is not an instruction. Their static images do not establish animation behavior or timing.

The [storyboard inventory](../mobile-storyboard/README.md) remains the visual reference. [docs/frontend.md](../../../docs/frontend.md) remains canonical architecture. This plan describes proposed changes, not current shipped behavior.

## Working scope and questions

Questions have been sent to the user. Until answered, plan for:

1. **A combined session list across connected hosts**, with a visible host filter. Up to four saved hosts remains the device limit. This is an intentional expansion from today's one-host runtime.
2. **iOS and Android in the same delivery.** Paper's 390 × 844 iPhone composition establishes hierarchy; Android uses appropriate system back, keyboard, sheet, and accessibility behavior. Support narrow phones, landscape and tablet widths without creating a desktop workspace on mobile.
3. **Complete core flows first.** Preserve existing capabilities and add all storyboard states. Attachments, voice, QR pairing, authentication, push, terminals, and the entire web administration surface are separate features. Do not ship decorative microphone/upload buttons that do nothing.

Full theme support includes the 66 current built-ins, future generated catalog entries, system appearance, custom theme discovery/import, and persistence. It does not mean pixel-identical syntax tokenization between native Markdown and the browser's Chroma renderer.

## Research findings

| Finding | Evidence | Consequence |
| --- | --- | --- |
| Native runtime, durable commands, revisioned drafts, creation recovery, attention and bounded transcript rendering already exist | `apps/mobile/src/runtime/`, `src/features/creation.ts`, `src/components/{requests,conversation,paged-text}.tsx`; `docs/frontend.md:156` | Refactor presentation around these behaviors; do not replace the SDK or command journal |
| One active host is assumed throughout runtime/context/routes | `src/runtime/runtime.ts:16`, `src/runtime/context.tsx`, `(tabs)/index.tsx`, `features/attention.tsx` | Combined home needs explicit per-host ownership, identity-qualified routing, partial results and global resource limits |
| Theme catalog is already imported on mobile; there are 66 generated IDs | `src/theme/theme.tsx`, `packages/ui/src/theme-data.ts`, `packages/ui/src/generated/theme-catalog.ts` | Reuse the catalog. Do not manually reproduce palettes or hard-code Claude Code colors in screens |
| Native action groups seed a generated Material palette on Android | `src/components/primitives.tsx:34`; installed `@expo/ui/src/universal/Host/types.ts` | A primary seed alone does not produce exact Whip semantic colors. Use themed RN controls for branded surfaces |
| Expo UI already provides a native bottom sheet with RN children and explicit container colors | Installed `@expo/ui/src/universal/BottomSheet/{types,index.ios,index.android}.tsx` | Start with an adapter over this primitive; no new sheet dependency by default |
| Expo sheet detents differ by platform; Android supports partial/full rather than arbitrary exact heights | Same installed types and implementations | Design medium/full content, not fixed screenshot-height sheets. Long forms grow to full height |
| Native-stack form sheets have platform caveats, including nested stacks on Android | Installed `react-native-screens/src/types.tsx:566` | Use normal stack routes for substantial flows and one native contextual sheet; avoid nested form-sheet navigators |
| Markdown sets paragraph/link/code/quote colors but leaves other library defaults | `src/components/markdown.tsx`; installed `react-native-enriched-markdown/src/types/MarkdownStyle.ts` | Explicitly theme headings, strong text, code syntax, lists, tables, selection and all supported surfaces |
| Web custom theme normalization currently lives inside a DOM component | `packages/app/src/settings/custom-themes.tsx:12` | Extract only that pure conversion into the existing presentation subpath; share validation from `theme-data` |
| Mobile settings storage has a 4 KiB entry / 16 KiB total bound | `src/runtime/storage.ts:68` | Custom resolved themes need their own bounded encrypted storage, with a tested migration; do not inflate all preference quotas |

External research, checked 2026-09-11:

- [Expo SDK 57](https://docs.expo.dev/versions/v57.0.0/) is the required versioned reference per `apps/mobile/AGENTS.md`. Keep the existing Expo 57/RN 0.86 stack; verify installed types before using newer documentation examples.
- [Expo UI](https://docs.expo.dev/versions/v57.0.0/sdk/ui/) exposes SwiftUI/Compose controls. [Universal BottomSheet](https://docs.expo.dev/versions/v57.0.0/sdk/ui/universal/bottomsheet/) and [Host](https://docs.expo.dev/versions/v57.0.0/sdk/ui/universal/host/) support the adapter decision above. Explicit theme colors are necessary for RN sheet content.
- [React Navigation native stack](https://reactnavigation.org/docs/native-stack-navigator/) supplies navigation/modal behavior, but its sheet constraints make it unsuitable as a universal nested form wizard here.
- [KeyboardStickyView](https://kirillzyusko.github.io/react-native-keyboard-controller/docs/api/components/keyboard-sticky-view) moves an accessory with the keyboard; it does not resize the transcript. Use the installed 1.21.9 API, measured composer height and list insets together. Test the documented iOS commit/animation issue before adopting any build flag.
- [Apple sheets](https://developer.apple.com/design/human-interface-guidelines/sheets) and [motion](https://developer.apple.com/design/human-interface-guidelines/motion) inform focused modal tasks and accessible transitions. Timing values below are Whip proposals.
- Repository prior art, `docs/learnings/other-harnesses/opencode/opencode-ux.md:43`, supports one reusable searchable picker and deliberate modal ownership. Reuse the interaction idea with native primitives, not its TUI implementation.

## Component library

Create `apps/mobile/src/ui/` with a documented public `index.ts`. It is a real reusable library inside the native workspace, with a development gallery and behavior tests. A separately published package is unnecessary for one native consumer. Keep product components under `components/` and state/workflows under `features/` and `runtime/`.

Do not import the DOM/StyleX `@whip/ui` barrel or transplant Base UI components into React Native. Mobile continues to share only portable presentation and theme data.

| Library component | Primitive and responsibilities |
| --- | --- |
| `Text`, `CodeText`, `Surface`, `Divider` | RN Text/View; named type and surface roles, font scaling, selectable text, semantic colors |
| `Button`, `IconButton` | RN Pressable; primary/secondary/quiet/destructive variants, loading/disabled/focus/pressed states, labels and adequate targets |
| `TextField`, `SearchField` | RN TextInput; visible labels, validation/help text, correct keyboard/autofill/capitalization, clear action and theme selection colors |
| `ListRow`, `Section`, `StatusBadge` | Reusable leading/trailing slots and separators; status conveyed with text/icon as well as color |
| `ChoiceGroup`, `SwitchRow` | Accessible radio/selected semantics and native switch behavior; explicit theme colors |
| `Screen`, `ScreenHeader` | Safe areas, constrained wide layout, circular navigation controls, scroll ownership; no nested list inside a page ScrollView |
| `Sheet`, `SheetHeader` | Thin Expo UI BottomSheet adapter with themed chrome, medium/full size, close/back behavior, accessible title and focus restoration |
| `PickerList` | Search + virtualized choices + selected state, loading/error/empty/footer; reused by theme, host, model and agent pickers |
| `Notice`, `EmptyState`, `Toast` | Inline resource/action feedback and ephemeral confirmed results; recovery errors remain durable and actionable |

Product compositions: `HostRow`, `SessionRow`, `HostStatus`, `ChatHeader`, `Composer`, `ActivitySummary`, `AgentRow`, and request forms. These consume the library; generic controls must not know host IDs, SDK clients, or command operations.

Visual tokens: 4-unit spacing rhythm; 20-unit phone inset; 44 pt iOS / 48 dp Android minimum touch targets; 52-unit primary action minimum; 12 control, 18 user-message, 24 composer and approximately 28 sheet radii. Use native sheet chrome where the OS owns its geometry. Inter body, Inter semibold headings, JetBrains Mono code. Body/input text starts at 16, then respects platform scaling. No fixed-height text rows.

Use RN StyleSheet and memoized styles by resolved theme/preferences. Share color math and semantic roles, not CSS strings such as `color-mix()`. Resolve derived native colors to actual hex values. All icon colors, keyboard appearance, navigation backgrounds, overlays, code blocks and status surfaces must read from the same provider.

Motion: 100 ms press feedback, approximately 160 ms disclosure/message-state transitions; native keyboard and sheet curves remain native. Use installed Reanimated for small transforms/opacity changes, not per-token transcript animation. Reduced Motion suppresses translation, scale and repeating shimmer; newly inserted history must not animate as fresh messages. Keep stable row identities and reading anchors.

Dependency decision: no new UI framework, custom gesture engine, blur package, or second list/Markdown renderer. Add Expo's SDK-compatible DocumentPicker/FileSystem modules only for local custom-theme JSON import if not already direct dependencies. This file picker is unrelated to chat attachments.

## Theme architecture and Appearance

Extend `src/theme/` into three small responsibilities: validated preferences, portable-to-native role mapping, and the provider. Keep `themeCatalog`, `validateTheme`, `adaptThemeForWeb` and contrast helpers as the shared source. Its current adapter name does not require duplication; extract additional pure color helpers only where native cannot consume CSS output.

Proposed preferences:

```ts
type MobileAppearance = {
  version: 2;
  mode: 'system' | 'light' | 'dark';
  light: string;
  dark: string;
  contrast: 'system' | 'standard' | 'increased';
  motion: 'system' | 'reduce';
  font: 'inter' | 'system';
  codeFont: 'jetbrains-mono' | 'system';
  textScale: number;
  codeSize: number;
  wrapCode: boolean;
  toolDensity: 'compact' | 'comfortable' | 'detailed';
};
```

Validate bounded values; define text scale 0.9–1.3 and code size 12–24, composed with OS accessibility scaling. Mobile defaults stay appropriate for touch rather than importing the web's 13 px layout density. Retain existing saved mode/light/dark selections on migration. Default system pair stays GitHub Light / Claude Code. Unknown or invalid selections fall back with a visible explanation, not a crash or silent data reset.

Settings → Appearance includes:

1. System / Light / Dark mode and the corresponding light/dark theme choices.
2. A searchable picker over the actual full catalog, Light/Dark filters, checked selection and accurate miniature workspace previews. Choosing a theme for a pair only lists its compatible appearance; a browse-all view may explicitly switch the forced mode.
3. A bounded live preview built from real message, tool, code and composer components with static sample data. Preview selection is transient; Apply commits once, Cancel/back restores the persisted theme. Saving errors keep the previous committed preference and show recovery.
4. Custom themes: select a connected host to discover its custom catalog or resolve a picked JSON file through the existing daemon resolver. Native has no web-style Local/home host, so the target host is explicit. Raw file limit 64 KiB; no credentials or TUI config updates. Keep host-import IDs runtime-qualified, separate file imports, and prevent built-in ID replacement.
5. Save validated resolved imports locally, at most 16 themes / 256 KiB in total including record overhead; each record is bounded. Existing imports work offline. Removal repairs either selected pair atomically; Appearance reset restores preferences but preserves imports.
6. Native text/font, code wrapping/density, increased contrast, and reduced-motion controls. System settings remain respected. Use OS high-contrast reporting where available, and offer the explicit Increased setting on both platforms.

Add a versioned `themes` storage bucket with a transactionally migrated schema v1 → v2, keeping all old hosts/drafts/recovery rows. Update bucket validation, quotas, reset and native reopen tests together. An older binary may reject the newer schema; it must preserve it, never wipe it. Migration is forward-only unless a separate downgrade path is implemented and tested.

Markdown maps all exposed native syntax roles to resolved theme roles and maps Chroma categories where representable. Native tree-sitter and web Chroma token boundaries/attributes are not identical; verify readable themed output without claiming identical highlighting. Unsupported languages remain themed plain code. Use the installed highlighter configuration; confirm actual native compilation in both builds.

## Runtime and host ownership

Extend the existing runtime instead of copying web runtime/DOM state. Separate the current connection-owned machinery into `runtime/host-runtime.ts`, retaining its tested command, view and reconciliation methods. `runtime/runtime.ts` becomes the device owner of storage/preferences and up to four host runtimes. `runtime/context.tsx` supplies the application context and an explicit host scope to creation/chat/settings resources.

```ts
type SessionLocation = { hostId: string; runtimeId: string; rootId: string; agentId?: string };
type HostConnection = {
  host: SavedHost;
  state: 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'identity-mismatch';
  // Existing client/catalog/command machinery is owned by one host runtime.
};
```

Saved profile identity and live connection state are separate. Connect adds a live host without closing healthy hosts. Persist each profile's startup-connection preference; Disconnect turns it off, Remove deletes the profile after confirmation while retaining recovery data. Migrate the previously selected host to startup-enabled; do not silently enable every saved profile. Reject multiple profiles attached to the same verified runtime to prevent duplicate sessions/actions.

Every query key, list row, command lookup, draft, bookmark and route must include its existing runtime/client identity as appropriate. Host ID locates a connection; it never substitutes for verified runtime ID. Legacy session links may be recovered only with one unambiguous matching runtime, otherwise show host recovery. Never submit through whichever host happens to be selected last.

Foreground/background handling lives once at the device owner: pause all clients and cancel reads on actual background; resume independently and reconcile before enabling each host's actions. Cancelled tests/imports are scoped; reconnect failure does not close healthy hosts. Connection probe remains one temporary, bounded, cancellable client.

Resource budgets remain explicit: four saved/live hosts maximum; one focused conversation view with existing root/child leases and its 8 MiB / 512-message bound. Aggregate catalogs admit at most 512 summaries / 1 MiB across hosts. Search keeps only the current query generation. Attention retains the existing four 64-entry / 128 KiB pages as a device-wide budget, distributing initial coverage across connected hosts and exposing partial status/pagination. At most two host index reads run at once; one owner schedules attention polling every 10 seconds while foregrounded. No catalog/attention screen hydrates root transcripts. Storage budgets remain device-wide, not multiplied by host.

Combine only loaded summaries, ordered by pin and recency with stable host/runtime/root tie-breaks. Label incomplete results; do not imply globally exhaustive recency before every host is loaded. Refresh and pagination have host-scoped errors. A missing host is not an empty successful result.

## Routes and complete surface coverage

Replace the visible three-tab shell with Sessions home, gear → Settings and inline attention access. Preserve existing external paths where feasible with redirects. Use normal native stack routes for long tasks and one active contextual sheet for short choices. Creation steps progress within one flow; do not stack sheets or pass callback functions through route parameters.

| Paper screens / additional states | Proposed owner | Required behavior |
| --- | --- | --- |
| 01–03, 28 onboarding/add/help/error | `app/index.tsx`, `app/server.tsx`, `features/hosts/` | First launch connect; HTTPS/name fields; read-only test; progress; cancel; actionable TLS/protocol/identity/setup guidance; no invented QR backend |
| 04–08 home/empty/search/filter/attention | `app/index.tsx`, `app/search.tsx`, `app/attention.tsx`, `features/sessions/` | Center New session and top + call the same entry point; combined host metadata; no-results vs disconnected vs partial vs true empty; search debounce/cancel; accessible current/partial/stale attention |
| 09–12 creation/options | `app/new-session.tsx`, `features/creation.ts`, `features/creation/` | Host → browse/direct path/recent folders → review/create; optional prompt; host model/provider, effort, immutable language and advertised agent definition; preserve partially created root and draft |
| 13–15, 24 chat/working/complete/keyboard | `app/session/[rootId].tsx`, `components/{composer,conversation,markdown,paged-text}.tsx` | Spacious transcript, warm user bubbles, compact tools, recipient-aware composer, queue/steer, exact-turn Stop, saved draft, stable scroll and latest chip |
| 16, 18, 26 session menu/agents/activity | `components/session-actions.tsx`, `components/agent-picker.tsx`, session detail route | Rename, pin/unpin, archive/restore, copy ID, inspect details/activity, root/child navigation; only expose actions supported by current protocol/capabilities |
| 17, 25 permission/questions | `components/requests.tsx` and focused request sheets | Agent/host/command context, Allow once/Deny, single/batch/custom/skip answers, review, outdated/resolved elsewhere, delivery uncertainty |
| 19–21 settings/hosts/edit | `app/settings/{index,hosts}.tsx`, shared host form | Add/edit/test/connect/disconnect/remove; duplicate URL/runtime handling; no destructive edit of client identity; retain drafts/recovery |
| 22–23 offline/uncertain | `components/host-status.tsx`, `app/settings/activity.tsx` | Local draft retention, per-host reconnect, original-command check, explicit retry only after authoritative missing result and retained original payload |
| 27 appearance + new picker/import/preview | `app/settings/appearance.tsx`, `theme/`, `features/appearance/` | Full catalog and custom themes, paired system selection, accessible previews, persistence and display preferences |
| Additional existing functionality | `app/settings/{drafts,activity,diagnostics}.tsx` | Saved drafts copy/discard, command/permission recovery and resolved cleanup, metadata-only diagnostics and explicit local-data reset |
| Additional leaf states | Shared sheets/routes in each feature | Model/effort/definition choice, folder failure/pagination, no results, rename, archive undo/restoration, removal confirmation, storage failure, long-output inspection and unavailable content |

Creation must invalidate host-dependent folder/model/definition choices when the chosen host changes, preserving authored prompt text. Host defaults are the default option; no hard-coded model roster. The newly required `definition` and `definition_revision` fields must be understood before extending the existing workflow. A failed first input does not create a second root on retry.

Chat keeps the existing command-admission and draft-revision logic. An optimistic row is reconciled by submitted identity, never matching text. KeyboardStickyView alone is insufficient: measure the growing composer, provide matching list bottom space and keyboard inset, and preserve reading position when keyboard/composer size changes. Back navigation flushes drafts; source/recipient changes never combine drafts. Body reads remain explicit, scope/hash verified and at most 256 KiB; 8,192-unit text pages and 512-unit collapsed previews remain intact.

Session menus must not inherit unavailable Cursor actions such as Share or Review diff just because they appear in the reference. Archive succeeds before returning to the list; offer Restore through the supported operation, and keep archived sessions reachable. Destructive permanent deletion is outside this storyboard; existing local reset/removal confirmations remain available. Child-agent selection is distinct from choosing the definition for a new root.

## Implementation sequence and checkpoints

Each milestone ends with working routes and recorded evidence. Avoid a parallel replacement app or a long-lived second component system.

1. **Baseline and native feasibility.** Record the current branch and worktree state, resolve stale mobile protocol fixtures, reproduce outstanding runtime failures, verify native build tooling. Inspect Paper nodes/styles for the components being implemented. Build one representative sheet + text input + keyboard composer on iOS/Android to validate the selected primitive and theme contract. If Expo UI's adapter fails accessibility/layout requirements, prefer a standard stack modal; evaluate an additional sheet library only against a documented remaining gap.
2. **Library and themes.** Implement tokens/provider, core controls and gallery; Appearance route, catalog picker, preview/cancel, custom normalization/import, migration and storage tests. Migrate existing primitives' consumers incrementally, then remove the compatibility wrapper. At this checkpoint every built-in is selectable and the representative chat/control preview renders on both platforms.
3. **Host lifecycle and onboarding.** Extract one host runtime, add the device coordinator, identity-scoped context/routes and bounded index ownership. Ship onboarding, host management/edit/help and disconnected/replacement states with multi-host regressions.
4. **Sessions and creation.** Replace tab home, implement search/filter/attention and every empty/loading/partial state. Recompose the existing creation journal into host/folder/review/options. Preserve original root identity through creation recovery and expose host-advertised choices.
5. **Conversation and remaining settings.** Compose the finished transcript/composer, subtle motion, agent/activity menus, questions/permissions and archived-session access. Restyle all recovery/drafts/diagnostics/reset flows. Verify every storyboard transition and leaf state is reachable.
6. **Device acceptance and documentation.** Run the full regression, theme and native matrix, fix actual failures, record screenshots/evidence and update canonical docs. Only then call the UI complete. Packaging/store submission is a separate release operation.

## Validation and definition of done

Behavior tests extend the existing colocated suites; do not add snapshots that merely restate style objects.

- Library: button double activation/loading, labelled fields and errors, picker focus/selection/cancel, sheet back/dismiss/focus restoration, large text without clipping.
- Themes: iterate the generated catalog rather than asserting a fixed count; validate all mapped roles/contrast on their actual surfaces. Exercise selection, preview cancel, OS change, reload, bad preferences, import quotas/malformed JSON/cancellation, duplicate IDs, offline imports, migration interrupted/failure/reopen, reset and selected-import removal.
- Runtime: same root/command IDs on different hosts, healthy-host independence, duplicate runtime rejection, changed identity, stale async completion after edit/remove, all-client background pause, staggered resume/reconcile, one focused view and aggregate quotas. Prove no duplicate attention poll owners.
- Creation: both New session controls share one workflow; host switch resets dependent choices; nested folders/direct path/pagination; no models/provider setup; language/definition selection; rapid double submit; partial create/effort/input outcomes and restart.
- Chat: streamed and completed messages, identical authored messages, queued child admission, queue vs steer, Stop targets, permission/question revisions, reconnect/unknown delivery, saved draft revision and agent switch. Retain source-fallback/link/content-read bounds and reading-anchor tests.
- Visual: compare representative actual native screenshots against Paper for onboarding, empty/list, creation, working/completed chat, permission, settings and Appearance. Capture a compact real-component gallery for every shipped theme on both platforms; deeply inspect Claude Code, GitHub Light and representative bright/low-contrast/custom palettes. No theme flash after app-owned initialization, including modal and system chrome.
- Devices: iPhone and Android native builds; small phone, large text, keyboard open/closed, predictive/system back, sheet drag, VoiceOver/TalkBack focus, Reduced Motion, light/dark, offline/resume and host replacement. Tablet/landscape must remain usable with sensible width constraints. Simulator checks alone do not establish physical-device network/background/accessibility acceptance.
- Performance: retain virtualization and bounded text; use synthetic long transcripts and four-host catalogs; profile streaming while typing/scrolling for whole-list rerenders, blank recycled rows, unbounded memory growth and keyboard frame stalls. Record device/build/scenario and measured behavior; do not claim a frame-rate result from static screenshots.

Commands: `npm run check:mobile`, `npm run test:mobile`, `npm run export:mobile`. For changed shared theme/presentation code also run UI/app tests, `npm run check:web`, and `npm run check:themes` when touching generation/resolution. Native builds via the existing Expo development-client workflow; synthetic daemon via `apps/mobile/scripts/fixture.mjs`. Run the required final `task check` before calling repository implementation complete; separately identify environmental or pre-existing failures. No live provider work or host restarts are necessary for UI acceptance.

## Baseline evidence from this planning pass

- Read the current mobile implementation, canonical frontend guide, feature catalog, roadmap, mobile guide, previous implementation evidence and Paper storyboard plan. Confirmed all 66 generated theme IDs.
- `npm run check:mobile`: SDK build succeeded, mobile typecheck failed at `src/runtime/runtime.test.ts:24`: the root fixture lacks required `definition` and `definition_revision`. No production-code fix has been made in this planning pass.
- `npm run test:mobile`: 17 suites passed / 2 failed; 139 tests passed / 7 failed. Six runtime tests did not settle; the native storage test also failed because Swift could not write its default compiler cache inside the sandbox. Do not assume all runtime failures share the fixture cause before verifying.
- Re-ran `storage.native.test.ts` with writable temporary Swift/Clang caches: 18 passed / 1 failed. The cache permission failure was removed, but the Swift path/reopen fixture then failed a precondition (`<stdin>:24`). Its cause remains unverified and belongs in the baseline milestone; it cannot be dismissed as only a compiler-cache issue. No new native device build or release verification is implied by these tests.

## Documentation and task ledger

Update `docs/frontend.md` when implementing native component boundaries, per-host lifetimes, cache budgets, preferences and custom-theme storage. Update `docs/mobile.md` for navigation, host connection preferences and Appearance. Update `docs/features.md` with concrete behavior → files → validation; update the relevant `docs/roadmap.md` mobile work only when delivered, leaving distribution/pairing/push gates separate. Add `apps/mobile/src/ui/README.md` for component usage and a new `EVIDENCE.md` here for actual screenshots/build/test outcomes. Link this plan from the storyboard plan.

- [x] Research existing architecture and primitives; inspect baseline checks.
- [x] Define theme parity, complete screen coverage, file ownership and acceptance.
- [x] Incorporate product answers; confirm final defaults in this plan.
- [x] Resolve baseline and validate native sheet/composer primitives on Android.
- [x] Implement library, Appearance and custom-theme persistence.
- [x] Implement host runtime ownership and host flows.
- [x] Implement Sessions/search/attention and creation.
- [x] Implement conversation, request/menu/recovery and remaining settings.
- [x] Complete repository/mobile regression, Android theme captures and canonical documentation.
- [ ] Complete iOS native acceptance after resolving Xcode destination eligibility.
- [ ] Complete physical-device accessibility, layout and measured performance release matrix described above.
