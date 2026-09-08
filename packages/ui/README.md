# @whip/ui

The canonical [frontend architecture and design guide](../../docs/frontend.md)
explains the product philosophy and package boundaries. This README owns the
component APIs, theme contract, and UI-specific checks.

WHIP's private React 19 component library. Base UI owns focus, keyboard navigation,
ARIA state and overlay behavior; StyleX owns the authored visuals. The library has
no SDK, protocol, router, Query, filesystem, host or permission authority.

The design serves a developer directing recursive work: compact controls, calm
reading, quiet surface boundaries, and color reserved for meaningful status. A
4px spacing scale and stable geometry apply to every theme. Inter Variable is the
reading/chrome font; JetBrains Mono Variable is reserved for code, IDs and shortcuts.
Fonts are self-hosted. Lucide provides one consistent icon vocabulary.

## Consumer setup

This is an intentional **source distribution**. Configure the official StyleX
unplugin before the React transform, including the UI package in compilation.
Disable runtime injection in production. Import these once at the browser entry:

```tsx
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
import {initializeTheme, ThemeProvider, UIProvider} from '@whip/ui';

// The application supplies storage, including any error handling policy.
const storage = window.localStorage;
initializeTheme({storage}); // before createRoot / first app paint

<ThemeProvider storage={storage}>
  <UIProvider>{/* application */}</UIProvider>
</ThemeProvider>
```

Obtain localStorage inside try/catch if the browser may disallow accessing the
property itself. `UIProvider` installs shared tooltip timing, toast delivery and
Base UI's CSP configuration (`disableStyleElements`). Portals inherit the document
root theme. Parent-app CSS must not override library properties with an unlayered
reset. The supplied reset is isolated in `@layer whip-reset`. Configure
StyleX with `useCSSLayers: {before: ['whip-reset']}`. This explicit layer order is
required: Vite loads generated styles before the reset in development, while
production bundles can load them after it. Without it, reset focus defaults can
override component styles. Text inputs use their neutral focus border; buttons,
links, tabs and splitters use a 1px neutral keyboard-focus outline.

StyleX requires a `.stylex` import suffix for cross-package variables, so use
`@whip/ui/tokens.stylex` in authored style modules. The plain tokens export is
available for runtime inspection.

With Vite, keep `use-sync-external-store/shim` and
`use-sync-external-store/shim/with-selector` in `optimizeDeps.include`. The
official StyleX plugin discovers UI/app source packages from the consumer's
package metadata and excludes them from prebundling. Their Base UI/TanStack
CommonJS store shims still need prebundling, as described in
[Vite's dependency optimization guidance](https://vite.dev/config/dep-optimization-options#optimizedeps-exclude).
Run the plugin from the consumer's working directory; a programmatic `root`
alone does not change StyleX package discovery.

Use named imports from `@whip/ui`, `@whip/ui/tokens.stylex`, `@whip/ui/themes`, or
the browser-only `@whip/ui/workspace-tabs`. Do not
import component implementation paths. Native refs, ARIA/data attributes, events,
and runtime `style` are preserved where an underlying control is exposed. `xstyle`
is a typed StyleX extension, not a raw style object; Base UI positioning remains
in its native style path. Composition uses Base UI's `mergeProps` and `render`.

## Component inventory

| Group | Exports | Interaction/ownership |
| --- | --- | --- |
| Actions | Button, IconButton, ButtonGroup, ToggleGroup, Link, Tooltip, Kbd, CopyButton | Buttons default to `type=button`; busy controls cannot repeat actions; icon actions require a label. Clipboard errors belong to the caller. |
| Forms | Field, Fieldset, Label, Input, Textarea, NumberField, Checkbox, RadioGroup, Switch, Select, Combobox | Field connects labels/descriptions/errors with controls. Select/Combobox use keyboard navigation and typeahead/filtering; errors remain visible. |
| Overlays | Dialog, AlertDialog, Sheet, Menu, ContextMenu, Popover, CommandPicker | Controlled open state; focus trap/return and escape/outside behavior from Base UI. Confirming asynchronous work never silently closes a dialog. |
| Structure | Tabs, Collapsible, Accordion, Separator, Stack, Row, Panel, ScrollArea, SettingsRow, Breadcrumbs, VisuallyHidden | Native scrollbars/touch scrolling; explicit selected sections. App owns navigation and virtualization. |
| Workspace navigation | WorkspaceTabs, workspaceTabId (from `@whip/ui/workspace-tabs`) | Controlled session strip, link composition, close, pointer reorder, and leading/trailing utility slots. No session state or content loading. |
| Feedback | Badge, StatusIndicator, Progress, Meter, Spinner, Skeleton, Alert, EmptyState, ErrorState, Avatar, useToast | Status includes text, never color alone. Error/stale/loading/empty remain distinct. |
| Appearance | ThemeProvider, initializeTheme, useTheme, ThemePicker, ThemePreview | Per-device selection, reversible previews, all generated TUI themes and validated custom colors. |
| Read-only code | CodeBlock | Lazy focused syntax grammars; React text nodes, exact source text, theme-aware Chroma styling, bounded presentation. No editing or content fetching. |

Buttons accept `variant=primary|secondary|ghost|danger`, `size=sm|md|lg`,
`loading`, and standard button props. Badge accepts `tone` (or `variant`) of
`neutral|success|warning|error|info`. Dialog/Sheet share `open`, `onOpenChange`,
`title`, optional `description`, `children` and `footer`.

Select and Combobox use options `{value,label,description?,disabled?,icon?}`,
string `value`, and `onValueChange(value)`. The label is required. A Combobox also
accepts loading, emptyMessage, onInputValueChange, and onHighlightedValueChange;
its caller owns asynchronous fetching. UI never starts a background request.

`CodeBlock` takes `code`, optional `language`, `label`, `maxBytes`, `truncated`,
`downloadAction`, and `xstyle`. Starlark/Python, JavaScript/TypeScript, Go, JSON and
shell grammars load only when needed. Unknown languages remain plain text. It
preserves code node identities across theme changes and renders text directly,
including strings that resemble HTML. A 16 KiB input cap and a 4,096-token cap
bound highlighting and DOM work. Larger bodies show a visible excerpt notice;
the caller supplies the authorized full-content/download action. Resolved Chroma
backgrounds and token bold, italic, underline and backgrounds are retained.

## Workspace tabs

Import the browser-only control from `@whip/ui/workspace-tabs`; this keeps its DOM
drag engine out of ordinary component imports and server-side tooling. It accepts
controlled `value: string | null`, `items`, `onClose(value)`, optional
`onReorder(orderedValues)`, `utilities`, `label`, and `panelId`. Each item has a
unique `value`, renderable `label`, and optional `accessibleLabel`, `status`,
`metadata`, `tooltip`, sibling `menu`, and `wrap(element)` for a context menu.

Supply a router link through each item's `render` prop, with intent preloading
disabled. That link owns navigation, including Enter, Space and modified clicks.
Button-only items instead call `onValueChange(value)`. Arrow keys and Home/End move
focus without changing the selected route. Delete and middle-click close a tab;
the built-in close flow focuses the right neighbor, then the left, then the first
utility when no tabs remain. Direct application menu/shortcut closes must handle
their own replacement focus. Set supplied menu triggers to ghost buttons with
`tabIndex={isActive ? 0 : -1}` so inactive menus do not add a Tab stop per session.

The route outlet supplies its single panel; use `workspaceTabId(value)` as the
panel's `aria-labelledby` and give the panel the matching `panelId`. No hidden
panels or session views are created. A semantic tablist uses `aria-owns` to own
only the tab links, while the physical Base UI list retains the sibling close
and menu buttons outside that accessibility ownership. The component's native
Chromium accessibility tree and Axe checks verify this structure.

Tabs retain 144–224px widths within a horizontal scroller and a 48px row. Selection
and keyboard focus reveal the relevant tab; status changes do not scroll it.
The strip uses the theme-derived `surface.navigation` role: a quiet panel blend
in light palettes and a recessed canvas in dark palettes. Selected tabs use the
canvas with a fine border and a 6px radius. Equal-width slots reserve room for
status, title, and close, while inactive close controls appear on hover or focus.
The application keeps secondary actions in the context menu and open-session
picker instead of adding an ellipsis button to every tab. Keep border width,
style, and color explicit so the StyleX compiler preserves the declarations.
Pointer sorting starts after 6px and preserves the controlled selection. Touch
dragging is disabled; expose explicit move actions in the menu. Sorting uses
native Web Animations (including reduced-motion handling), with explicit pointer
collision refresh instead of dnd-kit's CSS-injecting floating feedback. All authored
visuals remain compiled StyleX. Applications own their alternative mobile sheet.

## Themes

`go run ./cmd/themegen` produces the complete TUI-derived source theme data and static
`createTheme` modules. `go run ./cmd/themegen -check` rejects drift. Never edit the
generated catalog or copy theme palettes into React code.

**Claude Code** (`claude-code`) is the Paper-derived dark desktop palette. Optional
catalog `displayName` keeps human labels separate from stable IDs. The resolved
`web` block may pin `navigation`, `quietBorder`, `codeBackground`, and
`inlineCodeBackground`; these are validated hex colors, not CSS. Missing roles
retain their existing derivation, and switching themes resets every override.
Use `surface.inlineCode` for inline chips; it defaults to `colors.element`.

`auto` follows `prefers-color-scheme`; named themes retain their declared dark or
light appearance. Selection persists in `whip.appearance.theme.v1` through the
application's optional storage adapter. The currently selected validated custom
palette is cached with the preference for startup without an available daemon.
Storage errors fall back visibly; they do not prevent changing this tab's theme.

Raw TUI JSON imports go through the daemon's shared resolver for ANSI and Chroma
semantics. The app namespaces custom IDs by runtime/source, then calls
`useTheme().addTheme(resolved)` and selects it separately. Built-ins cannot be
replaced. `validateTheme` accepts only resolved hexadecimal colors and bounded
syntax data. Runtime data updates a fixed allowlist of variables; no CSS source is
accepted or compiled. Code renderers can consume `resolvedTheme.code.tokens` for
full Chroma attributes, plus semantic `syntax`/`markdown` tokens for live themes.

`themeCatalog` retains the exact terminal palette. `useTheme().resolvedTheme`
returns the browser's displayed palette. `adaptThemeForWeb` preserves hue while
moving only insufficient-contrast foregrounds toward black/white to meet at least
4.65:1 on the surface ladder. Status foregrounds account for their tinted fills;
syntax tokens use the actual code background. A conflicting custom surface moves
toward its canvas when necessary. The fixed token allowlist applies these derived
values after static theme classes, including to portaled content. This deliberate
difference prevents small terminal colors becoming unreadable browser text.

The browser cannot observe a custom terminal ANSI palette. The Go resolver uses
the documented reference ANSI palette for portable output.

## Catalog and validation

```sh
npm run check -w @whip/ui
npm run test -w @whip/ui
npm run storybook -w @whip/ui
npm run build:storybook -w @whip/ui
npm run test:browser -w @whip/ui
npm run test:tabs -w @whip/ui
npm run test:csp -w @whip/ui
npm run test:packed -w @whip/ui
npm run test:visual -w @whip/ui
```

`stories/Library.stories.tsx` covers every exported control in workflow-shaped
examples: disabled/loading/long content, configuration conflicts, nested overlays,
focus return, responsive controls, and theme switching. The product browser suite
owns runtime flows; component tests do not need a daemon. Run keyboard and touch
checks on representatives, and compact fixture/token checks across every theme.

The component browser suite checks all 66 palettes with Axe contrast, label and
ARIA rules, plus keyboard/focus, custom-theme persistence, preview cancellation,
system appearance, storage failure, and narrow touch controls. `test:csp` builds
an isolated production fixture with no `unsafe-eval` or `unsafe-inline`; Chromium
and Firefox verify highlighting, code selection, custom Chroma attributes and
portaled controls. On macOS it also opens a synthetic actual Safari test tab,
which reports its own assertions without changing Safari preferences. Set
`WHIP_UI_SKIP_SAFARI=1` for headless macOS CI; this is explicitly not Safari
coverage. The fixture disables Vite asset inlining to keep fonts under
`font-src 'self'`.

`test:packed` installs real protocol/SDK/UI/app archives into an isolated temporary
consumer and exercises both production and Vite development rendering. Build the
SDK first. It checks the computed pointer/keyboard focus styles for text inputs,
textareas, comboboxes, number fields, buttons and the reset's link fallback in
both modes. Neither component fixture connects to a daemon. Runtime flows and
retained session views belong to the parent application's acceptance suite.

`test:tabs` builds a separate strict-CSP production fixture. Chromium and Firefox
exercise link/button keyboard activation, native modified links, close/focus,
context menu movement, pointer sorting/cancellation, overflow, 200% zoom, RTL and
touch-drag exclusion. All 66 themes receive Axe checks for contrast and ARIA
ownership. This is automated browser coverage, not a physical-device or screen
reader usability claim. Reports are written to `ui-test-results/workspace-tabs-report.json`.

### Visual comparisons

`test:visual` compares ten screenshots against reviewed, checked-in PNGs with
Playwright's `toHaveScreenshot`. It covers light/dark content, forms, nested
dialog/menu portals, a 390-pixel layout, and pending/resolved/interrupted/agent
states composed from the shipped components. This is an explicit **macOS local
release gate**, separate from the portable interaction, Axe and CSP suites.
Unsupported platforms fail with an explanation; they never silently skip.

Build Storybook first. The runner serves that build on an isolated loopback port;
it needs no daemon or SDK build. It waits for self-hosted fonts, disables screenshot
animations and caret painting, and fixes viewport, scale, locale and timezone.
Use the pinned npm lockfile and Playwright browser. The exact reviewed reference
and fixtures are recorded in [the baseline README](tests/visual-baselines/README.md).
Other macOS versions or architectures may rasterize differently and are not an
established reference. Linux CI does not establish this visual gate.

```sh
npm run build:storybook -w @whip/ui
npm run test:visual -w @whip/ui
npm run test:visual:proof -w @whip/ui
```

`test:visual:proof` deliberately enlarges one rendered heading. It succeeds only
when Playwright produces an actual image diff, and verifies that the reviewed
baseline hash is unchanged. The expected failing test is printed by Playwright;
the enclosing proof command exits successfully only after verifying that failure.
Normal comparisons must then pass again. The report is
`ui-test-results/visual-report.json`; the separately retained negative evidence is
`ui-test-results/visual-diff-proof.json` and `visual-diff-proof/*.png`.

For an intended appearance change, run `npm run test:visual:update -w @whip/ui`,
open and review every changed PNG, and commit those baseline changes alongside
the source. Normal tests never write or accept a missing baseline. CI must never
update baselines. Screenshot comparisons cover these component fixtures; actual
application workflows, assistive technology and physical-device checks remain
separate acceptance work.

Dialog accepts an optional `header` slot for controls such as a search input; its
`title` remains the accessible dialog name. `initialFocus` and `finalFocus`
forward Base UI focus destinations. Default titled dialogs are unchanged.

Text inputs, textareas, combobox inputs, and number inputs indicate focus by
changing their one-pixel border to `surface.secondaryText`. They do not add an
outer accent ring. A composed input can put that focus treatment on its containing
surface while keeping its accessible label. Other shared form controls retain a
one-pixel neutral focus-visible outline, offset from the control.

### Workspace layout

`@whip/ui/workspace-layout` exports `WorkspaceLayout`, `WorkspaceLayoutNode`,
`WorkspaceDrop`, and `workspacePanelId(viewId)`. The app supplies a binary tree of
`pane` leaves and `split` nodes (`direction`, `ratio`, `first`, `second`), selected
content by view ID, pane headers, and callbacks for focus, resize, and drops. The
UI does not own tabs, routing, persistence, sessions, or resource limits.

Each header can use `WorkspaceTabs` with `groupId={paneId}` to join the layout's
shared drag context. Drops supply `{ viewId, paneId, index?, edge? }`; an index is
an insertion boundary in the target pane's array **before** removing the source.
An omitted index appends. `canDrop` can reject product limits before displaying a
preview and again at commit. Touch and keyboard move/split commands remain
application menu actions; pointer movement uses existing dnd-kit primitives.

Only selected content is passed in `panels`. Its wrappers remain flat siblings,
keyed **and ordered** by view ID, with measured rectangles over pane content slots.
This preserves DOM identity, input focus, and scroll when the split tree changes
or a selected view moves; inactive tab bodies are not retained by this primitive.
`labelledBy` is applied only while the corresponding tab exists, otherwise the
panel uses its supplied label. Call `onCompactChange` to reconcile app observers:
when the window is below 768 px or the tree cannot fit 320 × 240 px minimum panes,
the UI displays only the focused pane without changing the saved layout.

`react-resizable-panels` owns separator keyboard behavior and resize geometry;
authored appearance remains extracted StyleX and semantic theme tokens. Dividers
use a one-pixel quiet border with a transparent pointer target above the sibling
content overlays, so both split directions remain easy to resize. Pane focus does
not recolor the tab-strip border; keyboard focus retains its visible outline.
The library's `disableCursor` option prevents cursor rules, but version 4.12.4 still allocates
one empty adopted stylesheet after pointer interaction. This sheet has **zero CSS
rules**; no style elements, vendor CSS, or CSP relaxation are used. The production
fixture asserts those boundaries alongside actual drag/cancel, nested resizing,
compact restoration, a separate editable notes renderer, drag after menu-driven
transfer/pruning, and unchanged DOM/focus/scroll across moves in Chromium and
Firefox (`npm run test:layout -w @whip/ui`).
