# Shared code-block copy controls

Branch: `whip-rlm` (current working branch; no branch change made)
Status: Implemented; independent final review complete with no actionable findings. Validation results below.

## Goal

Every `@whip/ui` CodeBlock in the shared web/desktop renderer has exactly one
copy button at the top right, by default. Chat, REPL source/output/return values,
tool details, trace/raw views, stored messages and previews inherit the behavior.
Remove the REPL-specific copies of this UI without changing what gets copied.

## Non-goals

- No new code renderer, dependency, SDK/protocol changes or backend work.
- No changes to inline code, native mobile rendering or terminal/TUI rendering.
- No automatic fetching of offloaded content and no changes to display limits.
- No redesign of unrelated message-level, path, ID or snippet copy actions.
- No implementation as part of this planning turn.

## Evidence and current design

- `packages/ui/src/code-block.tsx:10-19,27-46`: shared bounded/highlighted renderer;
  header currently exposes only the caller-owned `downloadAction` slot.
- `packages/app/src/timeline.tsx:316-326`: ordinary Markdown uses CodeBlock.
- `packages/app/src/streaming-markdown.tsx:160-164`: streaming Markdown constructs
  CodeBlock directly (so changing only the timeline adapter would miss chat).
- `packages/app/src/repl-view.tsx:114-129`: source copy is in the cell header;
  output copy is supplied through downloadAction; return value has no copy.
  Output copy uses raw `row.output`, not formatted/collapsed `output.text`.
- `packages/ui/src/actions.tsx:42-48`: reusable CopyButton already handles
  clipboard promise success/failure, accessible labels and copied feedback.
- `packages/ui/src/code-data.ts:3-10`: displayed code is capped at 16 KiB.
- `packages/app/src/index.tsx:25-34`: shared application provider boundary.
- `packages/ui/src/presentation.tsx:80`: existing UIProvider.
- `docs/frontend.md`: generic visuals belong in UI; platform effects are injected
  from app; browser and desktop share the renderer. Clipboard adapters live in
  `apps/web/src/platform/{browser,desktop}.ts`.
- `packages/app/test/repl-view.test.tsx:57-70`: regression coverage already asserts
  exact original output copying, including expandable previews.

This extends existing in-repository behavior rather than porting another harness.
The frontend roadmap already covers shared themed read-only code; no separate
backend feature or external harness investigation is needed.

## Design

### 1. Inject clipboard behavior once

Extend UIProvider with an optional `copy(text): Promise<void>` callback backed by
a small UI-local clipboard context (e.g. `packages/ui/src/clipboard.tsx`). Wire it
to `platform.copy` in createWhipApplication. Do not import AppRuntime, Electron or
SDK modules into UI, and do not thread clipboard callbacks through every renderer.

Make the existing CopyButton resolve its operation in this order:
explicit `copy` prop, provider callback, browser clipboard fallback. Keep explicit
callbacks working for existing independent copy actions. Standalone UI/Storybook
continues working with the fallback; unavailable clipboard access rejects with a
useful message, not false success. Preserve method binding when wiring callbacks.

### 2. Make copy part of CodeBlock

Compose the existing CopyButton into every CodeBlock header, always visible and
keyboard/touch accessible, using current theme/StyleX controls. No opt-in prop.

Minimal additions to CodeBlockProps:

- `copyText?: string`: defaults to the original `code`, before visual truncation
  or text decoration. Use nullish fallback so an explicit empty string is valid.
- `copyLabel?: string`: contextual accessible label when needed; otherwise derive
  a useful label from the block label, with a generic `Copy code` fallback.
- Rename `downloadAction` to `headerActions?: ReactNode`, render these additional
  actions beside (not instead of) copy. Migrate all callers/stories atomically;
  these are private workspace packages, so no obsolete alias is needed.

Preserve CopyButton's copied/check feedback. Failures belong to the affected
block: show one accessible local error with retry through the same button; clear
it on successful retry or changed copy content. Reuse existing UI error/status
primitives rather than importing app ErrorNotice. Avoid duplicate global toasts.
Ensure streaming updates cannot show a stale successful copy for newer content.

Copy semantics:
- Copy code text only: no language label, fences, highlighting or animation DOM.
- Preserve whitespace, newlines and Unicode exactly as provided to CodeBlock.
- Copy all text already available via `copyText ?? code`, even when display is
  bounded. Do not claim to fetch/copy content absent from the loaded record.
- An externally truncated record stays truncated; retain its existing notice.
- Keep the button for empty content too; copying an empty string is valid.

### 3. Remove special-case implementations

In repl-view.tsx:
- Delete the source CopyButton from the execution-cell header: the source block
  now owns it. Preserve a useful source copy label if needed.
- Delete the output CopyButton passed via downloadAction; supply
  `copyText={row.output}` and `copyLabel="Copy output"` to CodeBlock instead.
- Return-value blocks acquire copy automatically.
- Remove now-unused CopyButton imports, runtime hook, copy-error state and the
  REPL copy ErrorNotice, retaining anything still used by other behavior.

Audit every CodeBlock and adjacent copy control. Remove only duplicated controls
for the same block, not unrelated message/path/ID actions. Both Markdown paths
should gain copying without a separate chat wrapper or repeated action wiring.
Migrate the Storybook downloadAction example to headerActions and preserve its
additional action. Update test providers to inject mocked platform copying.

## Files expected to change

Core:
- `packages/ui/src/{code-block,actions,presentation}.tsx`
- Small `packages/ui/src/clipboard.tsx` context module if needed to avoid cycles
- `packages/app/src/index.tsx`
- `packages/app/src/repl-view.tsx`
- `packages/ui/stories/Library.stories.tsx`

Validation/documentation:
- Shared code-block/copy tests in the existing UI or web test harness
- `packages/app/test/repl-view.test.tsx`
- Existing timeline/streaming tests and a provider-integration regression test
- `packages/ui/README.md`, `docs/frontend.md`, `docs/features.md`

Do not modify timeline.tsx or streaming-markdown.tsx merely to inject buttons;
change them only if testing reveals a real rendering-path gap.

## Validation and acceptance

- One top-right copy button for each shared CodeBlock, including unlabeled/plain
  text, unknown language, empty content and return values.
- Exact copy for whitespace/Unicode, >16 KiB visual excerpts, raw override text,
  highlighted code and streaming updates; no labels/fences/markup copied.
- Callback precedence: explicit override > provider > browser fallback. The app
  provider delegates to platform.copy (desktop must not fall back to browser).
- Failure is visible and accessible, does not claim success, and supports retry;
  success clears failure. Settling an older copy during streaming must not mark
  newer content as copied. No duplicate error surfaces.
- REPL source/output/return value each copy once. Collapsed, expanded and formatted
  output all copy raw row.output; preserve the existing regression assertion.
- Both ordinary Markdown and streaming Markdown have working per-block copying.
  Add integration coverage through application provider wiring, not only mocks
  that bypass the provider contract.
- Additional headerActions coexist with copy. Narrow panes, keyboard focus,
  touch, theme contrast and selection/scroll behavior remain usable.
- Run focused UI/app tests, full `npm run test:web`, `npm run check:web`, existing
  UI checks and Storybook build; inspect shared web/desktop presentation. Run the
  repository's applicable final checks, reporting unrelated baseline failures
  honestly rather than modifying unrelated work to get green.

## Ordered tasks

- [x] Add regression tests for shared default copy and provider resolution.
- [x] Add provider injection and shared CodeBlock copy/error behavior.
- [x] Migrate REPL and header-action callers; delete redundant state/controls.
- [x] Add chat/streaming, REPL and app-provider integration coverage.
- [x] Update stories and verify narrow-pane/browser behavior; desktop routing is
      covered through the application provider test, not a native clipboard smoke.
- [x] Update frontend architecture, UI API docs and feature behavior/test map.
- [x] Complete independent review for duplicate controls, import cycles, clipboard
      failures, content-limit semantics and unnecessary abstractions. No actionable
      findings; no files changed by the reviewer.
- [x] Run validation; limitations and baseline differences recorded below.

## Working-tree hygiene

The repository already has substantial unrelated changes, including UI exports,
README and frontend documentation. Preserve them; make only targeted edits and
stage intentional files only if later requested. No commit or staging is planned.
## Implementation and validation — 2026-09-23

Implemented the provider-backed shared copy control, `copyText`/`copyLabel`
overrides, and `headerActions` replacement. Both Markdown paths inherit the
button unchanged. Removed REPL-owned controls, clipboard callbacks and error
state; output still copies raw loaded text. Copy feedback ignores superseded
attempts and source changes, and failures support local retry. No dependencies
added, no unrelated copy controls removed, no files staged or committed.

Checks on the current working tree:

- `npm run test:web`: **93 files / 1,240 tests passed**, including the new copy,
  provider, REPL and streaming coverage and desktop renderer contracts.
- `npm run check:web`: passed, including production renderer artifact generation.
- UI typecheck and unit tests: passed (**14 tests**).
- Storybook build: passed.
- UI browser suite: **66 themes / 14 interaction scenarios passed**.
- UI strict-CSP Chromium and Firefox probes: passed. Added keyboard/touch copy,
  full loaded source beyond display limits, single controls, 320px layout,
  local failure/retry checks. Inspected narrow-error and dark-runtime screenshots.
- Packed UI/app isolated production and development consumer checks: passed.
- `git diff --check`: passed.
- `task check`: blocked by existing formatting in another worktree:
  `.claude/worktrees/session-trace/internal/daemon/session.go` and
  `.claude/worktrees/session-trace/internal/session/otlp_export.go`. Left untouched.
- Existing screenshot comparison: **4 passed / 6 differed** (content, narrow and
  runtime in light/dark). Runtime includes the expected new copy header; content
  and narrow also have differences outside code blocks. Baselines were not
  broadly regenerated in this shared dirty working tree. Artifacts are under
  `packages/ui/ui-test-results/visual/`.
- Actual Safari/native Electron clipboard smoke was not run. Chromium/Firefox
  probes stub the clipboard; provider integration verifies AppPlatform delegation
  and receiver binding. No existing daemon or desktop session was restarted.

The initial raw-JSON REPL test fixture lacked the legacy result's required
`value` field; it was corrected to `value: null`. Earlier workflow-inventory
failure no longer occurs on the current tree; no inventory edit was made here.
