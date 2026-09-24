# Desktop slash skill suggestions

Branch: `whip-rlm` (research only; no implementation branch created)
Status: implemented; shared-web Chromium/Firefox acceptance passed. Native Desktop
and visual inspection remain unverified.

Follow-up: [Instant filtering plan and delivery evidence](instant-filtering.md)
is complete. It supersedes the per-prefix fetching/cache decisions below and the
earlier transport-blocked validation record, which is retained as history.
Date: 2026-09-23

## Goal

Let a person type `/` in a desktop message composer, filter a compact list of
available skills, navigate with Up/Down, and press Enter to insert a skill
reference without sending the message. Keep typing focus in the composer.

### Confirmed product decisions

The user confirmed these decisions during planning:

- **Skills only** in v1; no application commands mixed into the list.
- **Prefix search only**, reusing the existing completion matching semantics; no
  substring matching or new search-mode API is needed.
- `/pon…` selection inserts **`$ponytail`**, using the existing daemon invocation
  mechanism. Slash is the discovery gesture, not new invocation syntax.
- Include **the new-session/first-message composer**, not just existing root and
  child conversations. Opening suggestions must not create a session.

Implementation belongs in the shared web/desktop renderer (`packages/app`), so
web gets the same behavior without an Electron-only fork. Native mobile and TUI
interaction changes are out of scope.

## Non-goals

- No new command execution framework, plugin registry, script execution, rich-text
  editor, reference chips, fuzzy-search dependency, or global keyboard shortcut.
- No change to `$name` expansion or automatic model selection of skills.
- No slash alias in the daemon; an unselected `/name` remains ordinary text.
- No replacement of the existing Add context dialog or its workspace-file picker.
- No redesign of skill import, permissions, or duplicate-name precedence.
- No eager session creation, filesystem reads in Electron/browser, or persistent
  frontend skill catalog.

## Research and existing implementation

1. **Composer input and send behavior.**
   `packages/app/src/composer.tsx:298–336` uses the shared `Textarea`, retains
   selections, and sends on unmodified Enter outside IME composition. Completion
   handling must run before that send branch. `:370–384,428–445` implements the
   existing Add context dialog and insertion with caret restoration.
2. **First-message is a distinct composer.**
   `packages/app/src/welcome.tsx:68–186,209–216` retains a draft before creating a
   root on send. It has a separate Enter handler. Both composers need the same
   completion controller, not copied event logic.
3. **Existing query and picker.**
   `packages/app/src/completion-picker.tsx:22–46,49–81` calls `workspace.complete`
   on the execution host and inserts the returned text. The endpoint requires a
   root and derives CWD from the selected agent:
   `internal/daemon/completion.go:33–45`. Skill matching is currently
   case-sensitive prefix-only, descriptions are bounded, and results are capped:
   `:48–79`. Reuse this prefix query rather than introducing substring search.
   Send the active prefix to the host before limiting results; filtering just the
   first 32/64 items from an empty-prefix query would miss later matching skills.
4. **Invocation is already implemented.**
   `internal/daemon/input.go:53–127` maps `$name` to an authorized skill, reads the
   bounded file, and appends its instructions. No invocation changes are needed.
5. **Catalog parity needs care.**
   `internal/rlm/environment.go:225–284,348–373` builds the authorized prompt
   catalog, including applicable ancestor skill directories and the configured
   user directory. `internal/daemon/prompt.go:46–63` supplies effective definition
   and capability boundaries. The legacy completion scan is not identical.
   Duplicate names currently resolve last-entry-wins in explicit invocation
   (`input.go:80–83`); the picker must not invent a conflicting winner.
6. **Design system is mandatory.**
   `docs/frontend.md`, especially “Product intent and visual philosophy”, “State
   ownership”, “Data fetching and synchronization”, “Component and styling
   contract”, and “Development and validation”, is authoritative.
   `packages/ui/README.md` assigns focus/ARIA/overlays to Base UI, visuals to
   StyleX, and application I/O to app controllers.
7. **Existing primitives are close, not drop-in.**
   `packages/ui/src/forms.tsx:23–24,64–68` has a multiline Textarea and a Combobox
   that owns a single-line input. `overlays.tsx:51–57` has trigger-based Popover
   and modal CommandPicker; neither should steal focus from this composer.
   Installed Base UI 1.8.0 has Autocomplete with `filteredItems`, controlled open
   and value, `autoHighlight='always'`, and non-submitting item selection
   (`node_modules/@base-ui/react/autocomplete/root/AutocompleteRoot.d.ts`).
   Its Input is typed as HTMLInputElement, not HTMLTextAreaElement
   (`combobox/input/ComboboxInput.d.ts`). Prove supported multiline composition
   before committing to that adapter; do not hide an incompatible ref with casts.
   Its default no-highlight Enter behavior can allow form submission
   (`combobox/input/ComboboxInput.js:351–365`); our handler must guard it.
8. **Native overlay coordination matters.**
   `packages/ui/src/native-surfaces.tsx:11–45` waits for native surface hiding
   before opening an overlay and releases the hold on close/unmount. New popup
   composition must use that lifecycle, not a naked portal above a Browser pane.
9. **Prior art already researched in this repo.**
   `docs/learnings/other-harnesses/live-ux-probe.md:22–32` records a live-filtered
   slash menu with descriptions and keyboard hints. OpenCode's unified command
   namespace is described at
   `docs/learnings/other-harnesses/opencode/opencode-ux.md:175–189`; reuse the
   interaction lesson, not its broader command/template framework. The roadmap
   has no existing dedicated slash-skill-picker item. The shipped skill contract
   is documented at `docs/features.md:933–950`.

Research used the frontend-design, interface-design, new-feature-development,
and ponytail guidance. This is a behavior addition inside an established visual
system: reuse Whip's fonts, palette roles, scale and overlay finish rather than
introducing a new palette or decorative visual direction.

## Interaction contract

### Opening and filtering

- Detect an active slash token at the caret: `/` at the beginning of the draft or
  immediately after whitespace, followed by skill-name characters. Support later
  lines and insertion into existing prose, not just an empty draft.
- Require a collapsed selection. Work from the actual selection/caret, not merely
  the last token in the whole draft. Compute the complete token replacement range
  so editing in the middle of `/pony` cannot leave a stale suffix behind.
- Do not activate inside words/URLs (`https://…`, `path/to/file`), backtick code
  spans/fences, or a slash token containing a second slash. A bare `/tmp` is
  intrinsically ambiguous with a skill name; Escape leaves literal text intact.
  Keep code-context detection small and tested; no new Markdown parser.
- The query is the token's text before the caret, minus `/`. Empty query lists
  skills. Search is **prefix matching on skill names**, using the existing
  case-sensitive host completion semantics. Descriptions explain results but are
  not additional search keywords in v1. No substring or fuzzy matching.
- Use the host's deterministic skill-name ordering. No additional relevance
  ranking, usage history or frecency.
- The first result is highlighted when a current-query result arrives and when
  the query changes. Up/Down then moves selection and scrolls it into view.
  Use Base UI's looped list navigation if supported by the chosen composition.
- Request at most 32 candidates. Keep the existing visible `truncated` state:
  “More matches are available. Keep typing to narrow the list.” Search happens
  before limiting results. No full-catalog client fetch or virtualizer is needed.

### Accepting and dismissing

- Unmodified Enter with an active current result replaces only the slash token
  with the candidate's canonical text (e.g. `$ponytail`), adds/separates with a
  space as needed, restores the caret after the insertion, and closes the panel.
  Preserve all surrounding text and existing following whitespace.
- That Enter must never also submit the form. Ignore auto-repeat Enter for send
  after selection so holding the key cannot accept and immediately send.
- Clicking a result performs exactly the same insertion. Prevent pointer focus
  changes from invalidating the selection before insertion.
- Escape dismisses without editing the draft. Do not reopen on the same unchanged
  token merely because a query resolves; reopen after a deliberate edit/trigger.
- Tab dismisses and retains normal focus movement; do not trap it. Shift+Enter
  keeps normal newline behavior and ends the current slash token. Modified
  arrows/Enter retain their ordinary composer behavior.
- No menu interception during IME composition (including compatibility key 229).
- Close on leaving the token, moving focus outside, submission, recipient/tab/host
  change, or opening another composer overlay. No automatic focus jump on close
  when the user intentionally focused a different control.
- While the panel is loading, empty, offline, unsupported, or failed, bare Enter
  is consumed rather than accidentally sending the partial slash query. Escape
  returns to ordinary editing/send behavior. The Send button remains explicit.

### Feedback

Distinguish “Loading skills…”, “No matching skills”, “No skills available”,
“Reconnect to search skills”, and “Skill suggestions require a newer host”.
Errors use the existing error ownership/presentation conventions with Retry;
warnings/truncation remain visible. Do not render old-host/old-query results as
selectable during a scope change. Empty/loading messages are not list options.

## Visual and accessibility design

A quiet, composer-width suggestion panel sits above the input, with collision
fallback when there is insufficient room. It overlays the transcript instead of
resizing it or moving the user's reading position. Align its edges to the composer
box, not the window; constrain it to the active split pane/viewport.

```text
┌────────────────────────────────────────────────────────────┐
│ /ponytail           Review code for unnecessary complexity  │ ← active
│ /golang-code-style  Go code style and readability            │
│ /golang-testing     Production-ready Go tests                │
│                                      ↑↓ Move  ↵ Insert      │
└────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────┐
│ /                                                          │
│ …existing attachment, model and Send controls…              │
└────────────────────────────────────────────────────────────┘
```

- Names: existing mono typography, 13px token; descriptions: sans, 12px token,
  accessible secondary text. No custom font or new icon per skill.
- Roles: `colors.panel`, `colors.foreground`, `colors.hover`,
  `surface.secondaryText`, `surface.quietBorder`; shared scale for spacing/radii.
- Bounded height (about 6–8 comfortable rows, within the existing 360px popup
  ceiling), vertical scrolling, subtle existing overlay shadow. At narrow widths,
  descriptions wrap below names rather than forcing horizontal scrolling.
- Single clear full-row active highlight. Do not add checkboxes, selected-value
  ticks, cards, colored category pills, or a second search box.
- Optional tiny keyboard footer is inside the popup only, not permanent composer
  chrome. Selection inserts `$name`; display `/name` as the discoverable command
  label, with the accessible description explaining skill-reference insertion.
- Keep textarea focus and expose controlled expanded/listbox/active-option state,
  with stable IDs and proper option semantics. Announce result/empty status
  without announcing every background fetch repeatedly. Do not choose role names
  solely to satisfy Axe; verify the multiline accessibility tree and VoiceOver.
- Popup placement/size must respect autosizing, zoom, large text, split panes,
  theme switching, reduced motion, and desktop native Browser surfaces. No new
  runtime CSS injection or CSP relaxation.

## Implementation architecture

### A. Small reusable UI composition, not a second editor

Add a focused `TextareaAutocomplete` (working name) under `packages/ui/src`,
exported from the UI package. UI owns generic popup/list interaction and ARIA;
app owns slash parsing, candidates, query state and actual insertion.

Implementation finding: installed Base UI Autocomplete is typed for a single-line
HTMLInputElement. We are taking the planned native-textarea + Base UI non-modal
popup fallback, preserving truthful multiline textbox semantics rather than
casting incompatible refs. Browser/accessibility validation is still required.

Original integration gate: a bounded Storybook/browser spike would render the
existing textarea through Base UI Autocomplete's supported composition, retaining
multiline typing, native ref, autosize measurement, IME, caret and normal textarea
shortcuts. Use controlled results and avoid Base UI replacing the entire message
with an item label. Avoid a new editor and avoid modifying ordinary Combobox
behavior for unrelated callers.

**Go/no-go:** if Base UI's input contract cannot safely support a textarea, use a
small generic textarea-attached listbox with a Base UI positioned, non-modal
popup and the existing Textarea. Keep the necessary active-descendant keyboard
adapter in UI, document the exception, and test it explicitly. Do not ship a
single-line input disguised as a composer, cast-away incompatible refs, or
independently hand-built portal positioning. Settle this during the first task,
not late in feature integration.

The UI accepts controlled open/options/status, input/popup anchors, highlight and
select/dismiss callbacks, and ordinary refs/events/xstyle. It has no SDK/Query or
skill-specific knowledge. Use `useNativeOverlay` (or the existing presence helper
for the selected portal lifecycle) with cleanup tied to actual visibility.

### B. Shared app controller for both composers

Add small pure helpers and one shared controller (e.g.
`packages/app/src/skill-completion.ts` and `use-skill-completion.ts`):

- Parse active trigger/range and produce replacement text + caret.
- Own ephemeral trigger/dismissal state and scope guards.
- Query the selected execution host only while suggestions are requested.
- Apply selections through each composer's existing draft update function.
- Share keyboard arbitration, including the rule that completion consumes Enter
  before the existing send handler.

Use the existing runtime/composition store for draft text and selection; do not
add a second persisted draft or a reference-to-text side table. Welcome keeps its
existing pre-session draft owner. Menu/highlight state is ephemeral and resets on
scope change; selected `$name` text survives navigation naturally as draft text.

Query keys include persistent runtime ID, root/agent OR pre-session CWD/definition/
permission context, prefix and limit. Forward Query's AbortSignal,
use a short ~120ms debounce, preserve the documented Query defaults, and disable
requests while disconnected/unsupported/closed. Clear selectable stale candidates
immediately when the raw query changes; no client filtering of a truncated roster.
No polling, catalog background warmer, persistent cache, or filesystem watcher.

Keep the Add context dialog. Share typed query construction where appropriate,
but do not turn this into a general file-mention rewrite.

### C. Bounded host queries, including pre-session

Existing-session path:

- Reuse `workspace.complete` with `kind: 'skill'`, the active `prefix`, and the
  result limit. Keep existing matching semantics for desktop, TUI and older
  callers. No matching-mode field or search-specific capability is needed.
- Gate this path on the existing `workspace_completion` capability.
- Resolve current effective skill catalog from daemon-owned agent definition,
  CWD and filesystem authority, using the prompt-catalog code rather than a new
  directory list. Keep explicit-only skills available. Produce a single candidate
  per name matching current invocation's winner; no precedence-policy change.
- Existing root-agent association checks remain. A child picker must not simply
  reuse the root's larger catalog or the browser's local filesystem.

Pre-session path:

- Require a connected selected host and a chosen project folder before querying.
  Without a folder, show “Choose a project folder to browse skills” rather than
  a global-only catalog. Model/provider readiness does not gate this metadata
  query. Do not require a session or a configured model to browse skills.
- Changing host, project, definition or planned permission context closes/resets
  suggestions and cancels the old request. Preserve any already-inserted `$name`
  as ordinary draft text; do not silently delete or rewrite it.
- There are no session grants yet. Share the derivation of initial project
  boundaries and definition settings, not a nonexistent session's authorization
  state. Session creation and actual invocation remain authoritative on send.

- Add a narrow read-only host operation, proposed `host.skills.complete`, alongside
  the existing host directory browsing APIs (`internal/daemon/host.go:25–40`,
  `internal/protocol/registry.go:99–107`). Parameters: selected CWD, selected agent
  definition and relevant planned permission mode, prefix, limit. Reuse existing
  prefix matching and the same completion result shape (`text`, `description`,
  warnings, truncated). Advertise support for this new pre-session operation
  separately; its availability must not gate existing-session completion.
- Treat this as an authenticated host metadata query, not a root/agent grant.
  Normalize/validate CWD using the same rules as new-session setup. Resolve the
  selected definition and preview the catalog that its initial session would get;
  do not scan arbitrary ancestors, unrelated project roots, or file bodies.
  Any planned permission mode affects preview boundaries only: it grants nothing.
- Bound discovery using the existing bounded catalog reader (metadata byte,
  directory-entry and skill-count ceilings), bound query/description/warning
  bytes and response count, check cancellation, and report invalid/inaccessible
  directories as errors. Do not use the legacy unbounded scan for the new query.
- No session/agent creation, tool execution, model request, permission mutation,
  or setting change to open/search/accept suggestions. Selecting a skill inserts
  plain text; actual invocation is still authorized when the first message sends.
- Disabled skill discovery in the selected agent definition is reflected honestly.
  Preview is not a permanent promise: file/definition/authority changes between
  selection and send can change what is invocable, as with today's `$name`.

Regenerate protocol schema/TypeScript from Go; never hand-edit generated files.
Use typed `client.call` through the SDK rather than inventing another transport.
Update capability discovery/contract tests for the pre-session operation. On
older hosts without it, show unsupported state only in the new-session picker;
existing-session suggestions continue using `workspace_completion`. Manual
`$name` and existing Add context behavior remain usable. No protocol-major bump
is proposed for the additive, negotiated pre-session operation.

### D. Intended files

| Layer | Existing / proposed files |
| --- | --- |
| UI | `packages/ui/src/textarea-autocomplete.tsx` (new), `src/index.ts`, shared styles as needed; dedicated Storybook fixture/story and browser tests |
| App | `packages/app/src/composer.tsx`, `welcome.tsx`, small shared skill-completion helpers/hook; `completion-picker.tsx` only for query reuse if useful |
| App tests | `packages/app/test/composer.test.tsx`, `welcome.test.tsx`, new pure helper/controller tests |
| Daemon | `internal/daemon/completion.go`, `completion_test.go`, `host.go`, `server.go`; focused new host skill tests; reuse/refactor catalog option assembly from `prompt.go` only as needed |
| Protocol | `internal/protocol/completion_types.go`, `registry.go`, generated `packages/protocol/schema` and `generated` outputs, affected negotiation/SDK contract tests |
| Product validation | Isolated skill fixtures and a focused browser script under `apps/web/scripts`, exercised in Electron as well as browsers |
| Docs | `docs/features.md` Skills section, `docs/frontend.md` composer/query/primitive contract, `packages/ui/README.md`; roadmap checkbox when implemented |

Do not add dependencies. Keep scope to the skill query branch; do not rewrite
file completions, all prompt composition, or the existing dialog to ship this.

## Test and acceptance plan

### Pure and component tests

- Bare slash, prefix matches/nonmatches and existing case sensitivity, empty
  query, middle-of-draft and multiline
  insertion, replacement while caret is mid-token, adjacent whitespace, selected
  ranges, URLs/paths/code contexts, Escape suppression and intentional reopening.
- Initial first highlight; filtering resets it; Up/Down and active-row scrolling;
  pointer selection; Tab, Shift+Enter, modified keys and IME remain correct.
- Accept inserts exactly one canonical `$name` plus appropriate spacing, keeps
  surrounding text and caret, closes popup, and makes **zero submit calls**.
- Repeated Enter cannot send on the acceptance keypress. Enter in loading/empty/
  failed state does not send. After dismissal, normal Enter sends as before.
- Out-of-order results and delayed native-overlay readiness cannot reopen a
  dismissed popup, modify another draft, or select old-query options.
- Recipient/session/host/definition/CWD changes, offline/reconnect, unmount and
  StrictMode cancel requests and drop ephemeral state without losing drafts.
- Existing attachment paste/drop, autosize, queue/steer, send-acceptance and
  uncertain-delivery tests remain green. No second owner of draft or selection.

### Host/contract tests

- Prefix matching happens before limiting; a matching skill beyond the first
  32/64 unfiltered entries is found. A substring-only match is not returned.
  Existing skill prefix semantics and file/path completions remain unchanged.
- Deterministic ordering, exact canonical text, bounds/truncation/warnings, empty
  catalogs, malformed metadata, inaccessible roots and cancellation.
- Prompt-catalog parity: ancestor skills, configured home, explicit-only skills,
  duplicate winner, restricted child, definition with discovery disabled.
- Pre-session host/CWD/definition isolation; query and selection create no session
  and change no authority. First send creates only the expected session and its
  `$skill` expands through the existing daemon path.
- Negotiated pre-session capability, unsupported pre-session state on old hosts,
  working existing-session prefix completion on those hosts, contract drift.

### Real UI validation

Storybook: light/dark, long labels/descriptions, empty/loading/error, narrow pane,
large text, long list, custom theme and reduced motion. Production app fixture:
Chromium + Firefox and Electron, root/child/first-message composers, pane resize,
transcript reading anchors, and desktop native Browser-pane overlay hiding and
restoration. Run Axe and inspect the accessibility tree; manually check VoiceOver
with the multiline composer. Do not claim VoiceOver/Safari coverage from Axe or
Playwright alone. Capture/review screenshots before updating visual baselines.

### Commands at implementation time

- `npm run check:web` and `npm run test:web`.
- `npm run check -w @whip/ui`, `npm run test -w @whip/ui`, affected Storybook/browser/
  CSP/visual checks; packed-component check if adding a UI export.
- `npm run generate`, `npm run check`, `npm test` for protocol/SDK contract work.
- Focused Go completion/host/catalog tests, then affected Go race suites.
- Pack production assets and run the new isolated product browser fixture;
  build desktop to include Electron/native-overlay validation.
- Repository `task check` and relevant integration acceptance before calling the
  implementation complete. Do not restart the user's running daemon for tests.

The original planning change was documentation-only. Implementation checks and
remaining validation gates are recorded in the validation log below.

## Ordered tasks / approval boundary

- [x] Trace both composers, invocation, completion API, design system and prior art.
- [x] Confirm skills-only, slash-to-dollar insertion, pre-session support, and
  prefix-only searching (updated after user feedback).
- [x] Write the plan with interaction, authority, boundedness and validation rules.
- [x] Get approval for implementation.
- [x] Prove the smallest safe Base UI + textarea composition in Storybook/browser.
- [x] Add shared bounded skill search + pre-session operation and contract tests.
- [x] Add shared app parsing/query/insertion controller and pure tests.
- [x] Integrate both composers; preserve dialog, submission and attachment behavior.
- [ ] Run interaction, host, accessibility, production browser and Electron checks.
- [x] Update feature/frontend/UI docs and roadmap; review simplicity and edge cases.

## Implementation validation log

- Backend: focused daemon tests and focused race suite across daemon, rlm, skills,
  session and protocol passed before concurrent gateway refactor edits.
- Protocol check passed (14 interop tests plus generated drift); SDK check passed
  (452 tests) before the separate gateway operation was registered.
- Additional no-write row-count, pinned named-child definition and metadata-bound
  tests passed under one isolated Go overlay race run:
  `go test -race -overlay <temporary-overlay.json> ./internal/daemon -run
  'TestHostSkills|TestWorkspaceSkill' -count=1` (daemon 1.917s).
  Overlay used HEAD versions of unrelated gateway/transport edits and retained
  the skill capability and registry additions in mixed files. All other skill
  source/tests came from the worktree. This is not a combined-worktree pass.
- Parent's current-tree focused run passed rlm/skills/session/protocol but daemon
  compilation was blocked by in-progress `internal/webgateway/http.go` references
  to missing `websocket` and `contentHandler`. Unrelated files were not modified.
- Independent backend static review found no blocking correctness/security issue.
  Frontend review identified stale request resurrection and non-ASCII skill-name
  insertion cases; fixes and regression checks are part of the implementation.
- Final Storybook Chromium probe passed native-textarea focus, arrows, Enter
  without submit, pointer selection, Escape, Shift+Enter, IME, empty-state Enter,
  query highlight cycling, 375px layout, 32-row list-only scrolling, delayed
  native-hide/cancel/late-acknowledgment/release, and authored ARIA checks. Axe
  excludes Base UI-generated focus guards only. This is not manual VoiceOver or
  a real Electron native-pane verification.
- Parent UI type-check, 14 UI unit tests, packed-component build/render, desktop
  type-check and desktop test command passed. Current protocol check (14 tests
  and generated drift) passed again after concurrent gateway changes.
- `task check` stopped on unrelated formatting in nested
  `.claude/worktrees/session-trace` files and in-progress `cmd/whip` gateway files;
  those files were not reformatted by this task.
- `apps/web/scripts/slash-skills.mjs` was added and passed `node --check`. The
  real-app acceptance attempt was blocked before browser launch by unrelated
  daemon transport test compilation: `protocol_edge_test.go` referenced removed
  `unixMessageTransport`; `protocol_test.go` and `subscription_lifecycle_test.go`
  referenced unexported transport fields. No Chromium/Firefox product assertions
  or screenshots ran. Failure evidence: `/tmp/whip-slash-skills-results/report.json`
  and `chromium-failure.txt`. Rerun after shared transport work compiles.
- Review rechecked and approved scope/dismissal reset plus Unicode reference
  filtering/insertion fixes; no remaining blocking findings in reviewed scope.


- Final production pack passed with Unicode support and query/open highlight reset
  (renderer artifact `62612dc8668b678c0c26791454ae6e46c70f2c00d50994c9f3e098e9fa0c2c67`).
- Final full `npm run test:web`: **1,142 passed, 1 failed** across 89 files. The
  only failure is workflow inventory missing the concurrently added unrelated
  `rpc:gateway.status` operation. `host.skills.complete` is documented in that
  inventory and all feature/component/desktop-renderer tests passed. Earlier
  feature regressions (provider gating, fixture QueryProvider, separator caret,
  Unicode, old hidden-composer expectations) were fixed rather than suppressed.

## Worktree safety

Research found unrelated existing edits, including frontend/feature/roadmap docs
and desktop files. This turn adds only this plan. Implementation must reread the
current files and preserve those edits; do not stage or revert unrelated work.
