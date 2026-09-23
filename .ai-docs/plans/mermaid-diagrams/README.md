# Theme-native Mermaid diagrams in chat

Branch: `whip-rlm` (research only; no feature branch created)

Status: **Proposed — awaiting approval before implementation.**
Research date: 2026-09-23.

## Goal

Render fenced `mermaid` blocks in web and desktop conversations as readable,
polished diagrams that belong to WHIP. Keep the original Markdown intact for
copying, history, export, and clients that cannot render diagrams.

**Recommendation:** use `beautiful-mermaid`, behind a small shared UI component,
without replacing TanStack Markdown. Render generated SVG in an isolated image
context, not as inline page markup. First validate the complete library output,
fonts, worker build, and browser compatibility under the existing production CSP.

**Important product trade-off:** this is a supported Mermaid subset, not a promise
of complete mermaid.js compatibility. The researched library advertises flowchart,
state, sequence, class, entity-relationship, and XY charts. Unsupported types or
unsupported constructs retain source with an honest explanation. If full Mermaid
compatibility is required from day one, choose official `mermaid` instead and
revisit the rendering/CSP architecture before implementation; do not quietly ship
two engines.

## Non-goals

- Replacing the Markdown parser, chat timeline, syntax highlighter, or theme system.
- Server-side rendering, external rendering services, CDN assets, or sending chat
  content to third parties.
- Native mobile or terminal diagram rendering in this phase; those retain source.
  Responsive web and the shared desktop renderer are included.
- A diagram editor, automatic model repair, clickable diagram actions, remote
  images/icons, arbitrary diagram CSS, animation, or full-screen browser APIs.
- SVG/PNG export in v1. Copy source is included. Downloading raw SVG changes its
  security context and requires a separate export design.
- SDK, protocol, daemon, persistence, or model-prompt changes.

## Existing implementation and constraints

- `docs/frontend.md` is authoritative: reading-first conversation UI, restrained
  semantic color, shared StyleX tokens, all themes/custom themes, bounded retained
  state, stable reading anchors, and no style-source CSP relaxation.
- `packages/app/src/streaming-markdown.tsx:53-75` parses assistant prose into stable
  virtualized blocks; its document cache is already bounded. At lines 160-164 it
  replaces fenced code with `CodeBlock` during the streaming/selection-aware walk.
- `packages/app/src/timeline.tsx` has a second path: `markdownComponents.pre` and
  `Prose`. Both must use the same fence dispatch. Changing only the static renderer
  would miss ordinary assistant blocks; changing only the streaming walker would
  leave other Markdown surfaces inconsistent.
- `packages/ui/src/code-block.tsx` is a reusable *source* viewer, also used for tool
  bodies and REPL code. Do not make every `CodeBlock language="mermaid"` execute a
  diagram renderer. Opt in from Markdown presentation instead.
- `packages/ui/src/themes.tsx`, `tokens.stylex.ts`, and `theme-contrast.ts` already
  own resolved palettes, display preferences, contrast adaptation, and browser
  surfaces. `fonts.css` loads local Inter and JetBrains Mono assets. Reuse these;
  do not import the diagram library's theme catalog or add Shiki.
- The shared `internal/webassets/csp.txt` has `style-src 'self'`,
  `style-src-attr 'none'`, and permits `img-src ... blob:` and
  `worker-src 'self' blob:`. This is a design constraint, not a reason to add
  `unsafe-inline` or an iframe permission.
- `docs/roadmap.md` / `docs/features.md` have no Mermaid feature entry found in this
  research. Existing harness notes discuss streaming Markdown, not a reusable
  Mermaid integration. This is a new presentation feature.

## Library research

| Option | Strengths | Costs / limitations | Decision |
| --- | --- | --- | --- |
| [`beautiful-mermaid`](https://github.com/lukilabs/beautiful-mermaid) | Designed for coding-agent diagrams; restrained SVG output; small theme input surface; pure TypeScript/no DOM; usable in a worker | Subset of Mermaid; ELK dependency; synchronous layout; generated inline styles and Google Font imports need handling; must verify grammar fidelity | **Preferred for v1**, subject to the gates below |
| [Official `mermaid`](https://mermaid.js.org/config/usage.html) | Broadest syntax compatibility; established ecosystem; accessibility metadata; configurable base theme | DOM-dependent rendering/measurement, generated CSS, shared initialization, significant lazy bundle; strict CSP integration needs more work | Prefer if complete syntax coverage is the deciding requirement |
| [`@streamdown/mermaid`](https://streamdown.ai/docs/mermaid) / Streamdown | Chat-oriented source/render state, expand/copy/pan-zoom prior art | Built for another Markdown stack; does not remove underlying engine/CSP constraints | Borrow interaction ideas, not the stack |
| Hosted Mermaid/Kroki-style rendering | Moves rendering off the client | Disclosure of conversation contents, network dependency, extra service, offline failure | Reject |

### What the primary sources actually establish

1. Beautiful Mermaid's README lists six families and SVG/ASCII rendering. Its
   `src/index.ts` implements `renderMermaidSVGAsync()` by calling the synchronous
   renderer: **awaiting it does not move layout off the main thread**. Its speed
   claims are upstream claims, not a WHIP benchmark.
2. The inspected `main/package.json` reports version `1.1.3`, MIT licensing, and
   dependencies on `elkjs` and `entities`. This is a source snapshot, not a verified
   npm latest-version claim. At implementation time, verify the published release,
   advisories, worker compatibility, and lock an exact reviewed version.
3. `src/theme.ts` generates an SVG style block, inline root CSS variables, and
   Google Fonts `@import` rules, including JetBrains Mono for some diagram types.
   Therefore the README's `dangerouslySetInnerHTML` example is **not suitable for
   WHIP unchanged**, and “zero DOM dependencies” does not mean “zero integration
   work.” Strip the library-owned remote font imports and supply only bundled
   fonts. Do not allow diagram input to choose a URL or CSS configuration.
4. Beautiful Mermaid has already fixed an attribute-injection issue in style
   values ([upstream fix](https://github.com/lukilabs/beautiful-mermaid/commit/68f3ab8c9658e7f4a3b749e06a6b96e4c3f55db1)).
   Treat source as untrusted even when a library escapes labels.
5. Official Mermaid's [theming guide](https://mermaid.js.org/config/theming.html)
   says `base` is the customizable theme and theme calculations require hex colors.
   Its `securityLevel: strict` is useful but not an all-purpose security boundary:
   [GHSA-87f9-hvmw-gh4p](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-87f9-hvmw-gh4p)
   describes configuration-driven CSS injection; the advisory lists fixes in
   11.15.0 and 10.9.6. Any official-engine alternative must use a reviewed patched
   release and lock down configuration, not just set `strict`.
6. [SVG image restrictions](https://developer.mozilla.org/en-US/docs/Web/SVG/Guides/SVG_as_an_image)
   disable script and external resources in image contexts in supporting browsers.
   These restrictions do **not** apply to SVG opened as a document. They also mean
   page CSS variables and page-loaded fonts cannot simply be assumed to reach the
   diagram image.

### Research experiment performed

A temporary loopback HTTP server served a minimal page with the repository's exact
production CSP. A Playwright probe loaded a Blob-backed SVG image containing an
inline CSS variable and style rule, drew it to canvas, and checked the pixel.

- Chromium: expected `[18, 171, 52, 255]`, actual same; no console errors.
- Firefox: expected `[18, 171, 52, 255]`, actual same; no console errors.
- WebKit: not run; the Playwright WebKit executable was not installed.
- The server and browser processes were closed; no runtime was restarted.

This validates a **minimal SVG-image mechanism only**. It is not validation of
Beautiful Mermaid, font embedding, hostile input, layout fidelity, accessibility,
or packaged desktop behavior. Those remain implementation gates. No library was
installed and no application code or lockfile was changed for this experiment.

## Visual and interaction direction

**Intent:** an architectural sketch embedded in a coding conversation, not a
separate diagramming application. The user's screenshot reinforces the existing
quiet dark conversation canvas, readable prose, and sparing color—not a fixed
palette to hardcode.

- Domain: agent trees, tool execution, mailboxes, lifecycle states, runtime links,
  and data flow. Use these as the actual preview fixtures rather than generic
  marketing charts.
- Color world: theme canvas, panel fill, foreground ink, readable muted labels,
  connector/border tone, and semantic accent. Preserve the same roles in light,
  dark, Claude Code, and custom themes.
- Signature: diagram/source are two views of the same retained conversation fence;
  source is never rewritten, and the reader's place remains stable as the view
  changes or a theme is previewed.
- Reject three defaults: rainbow nodes become mostly neutral structures; a heavy
  dashboard card becomes quiet code-block-like chrome; a giant floating toolbar
  becomes a compact, keyboard-accessible header.

Proposed block:

```text
┌ Mermaid · Flowchart      Diagram | Source   Copy source   Expand ┐
│                                                                │
│       Theme-colored nodes, readable labels, restrained arrows   │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

Use shared StyleX spacing/type/control tokens and existing Base UI controls.
Mirror `CodeBlock`'s visual language, without adding a card around the message.
Controls are discoverable on touch and keyboard, not hover-only. Narrow layouts
wrap or collapse secondary actions, with at least 44px touch targets.

- **Diagram** is the settled default. **Source** uses the existing `CodeBlock`.
- **Copy source** copies the original fence body via the app's existing clipboard
  boundary. The assistant response-copy footer still copies original Markdown.
- **Expand** opens the existing accessible dialog primitive, returns focus on
  close, and shows the same image at a readable size. Provide Fit, 100%, and
  bounded zoom buttons with ordinary scrolling; no new pan/zoom library in v1.
  Do not hijack wheel scrolling in the conversation.
- Give small diagrams comfortable padding. Wide diagrams scroll or expand rather
  than becoming unreadable thumbnails. Constrain inline height using the existing
  code-block scale (approximately its 520px ceiling), with deliberate expansion.
- Reserve height while rendering/re-theming and use existing virtual-row resize
  measurement/reading-anchor behavior. No global scroll-to-bottom on completion.
- No decorative animation. Respect reduced motion and appearance density.

### Theme mapping

Use `useTheme().resolvedTheme` (already adapted for browser contrast), not raw
catalog entries or OS dark-mode guesses. Resolve image colors to literal values;
an SVG image cannot inherit the application's CSS variables.

| Diagram role | WHIP source |
| --- | --- |
| Background / foreground | `colors.background` / `colors.foreground` |
| Node fill | `colors.element` or `colors.panel`, checked with label contrast |
| Secondary labels | `browserSurfaces(...).secondaryText` / existing readable-color helper |
| Node borders and connectors | Contrast-adapted border/control role; meaningful edges target 3:1 |
| Accent | `colors.accent`, used sparingly rather than one color per node |
| UI labels / diagram labels | Selected UI font and UI size preference |
| Source | Existing code font, code palette, and wrapping preference |

The library's default faint-label blends are not automatically accessible. Test
all text-bearing roles, especially class/ER compartments and XY axes. Target at
least 4.5:1 normal text and existing increased-contrast behavior. Inspect named
and custom themes, preview/cancel, system appearance, and font/size changes.

The SVG image must be self-contained. Remove upstream remote font imports; embed
only the required existing local font assets when the selected font needs them,
with system-font fallbacks and non-Latin coverage checked. This is a specific
library-output adaptation, not a general-purpose CSS rewriting engine. Verify
font metrics and asset/byte cost before committing to this route. If faithful
font rendering needs a large fork or global CSP changes, stop and revise.

## Rendering and state design

### Boundaries

- App owns Markdown dispatch, streaming readiness, stable block identity, copy
  behavior, and any retained view preference. No source conversion in SDK state.
- UI owns generic diagram visuals, the renderer adapter, theme mapping, worker
  lifecycle, and bounded output. It receives source/readiness/copy callbacks; it
  imports neither SDK nor host/platform state.
- Keep a single engine and a single fenced-block dispatch component. Do not add a
  pluggable renderer framework or make generic code viewers render diagrams.

### Streaming policy

For v1, show source with a quiet “Diagram will render when this response finishes”
label while its message is live. Render after settlement, including history loads.
This intentionally avoids both per-token layout and a bespoke code-fence parser.
A parser may close fences synthetically during streaming: do not infer completion
from successful parsing, the current fence regex, or the last AST node alone.

A completed fence within an otherwise live response may remain source until the
response settles. That is an explicit first-version trade-off. Early fence-level
rendering can follow only if TanStack exposes reliable original-source completion
metadata and both Markdown paths can use it without duplicating grammar.

Interrupted/failed messages still preserve source; attempt a bounded render once
settled, and fall back if incomplete. Partial/truncated retained content is not
represented as a complete diagram. Do not render a clipped source excerpt.

### Work scheduling and output

1. Check language, supported family, readiness, and source limit before requesting
   rendering. Unsupported types go straight to source. Audit library behavior for
   unsupported statements that it might silently ignore; the compatibility corpus
   must not accept misleading partial diagrams as a successful render.
2. Lazy-load one reviewed engine into a module worker only for a mounted, eligible
   diagram (the existing virtual list bounds mounted history). No engine/layout
   code in the initial chat chunk and no synchronous ELK work in React render.
3. Use one worker and a bounded queue. Tag requests by source, palette, display
   settings, and generation; discard stale results on edits/theme changes/unmount.
   Coalesce superseded work and remove abandoned queued requests.
4. Return SVG text/dimensions, then create a Blob URL for an `<img>`. Never place
   generated markup in `innerHTML`, `<object>`, `<embed>`, or a navigable iframe.
   Do not bind engine click handlers or expose “open SVG” links.
5. Revoke URLs after replacement/unmount; keep the old image only until its
   replacement is decoded. Keep theme changes dimensionally stable where possible.
6. A bounded cache may retain completed SVG strings and dimensions, not live URLs
   or DOM. Include exact source, resolved palette, font/size, renderer version,
   and rendering policy in identity. Prevent stale completions and duplicated SVG
   IDs from affecting another block (image isolation naturally helps here).
7. A worker deadline must terminate stuck computation; `Promise.race` alone is not
   cancellation. Recover for later requests without an infinite retry loop. Shut
   down the worker when the last consumer is gone; do not start a worker per block.

Initial budgets to measure/tune in the spike (not benchmark-derived promises):
32 KiB UTF-8 source per diagram; 1 MiB generated SVG including font data; 32 cached
entries / 8 MiB total retained source+SVG; 16 queued jobs / 512 KiB queued source;
2 seconds active layout per job, with cold module loading measured separately.
Include intermediate/response size checks, finite positive dimensions, and tests
for dense graphs and oversized labels. Exceeding a budget produces source with a
clear explanation, not a blank block or an automatic retry storm.

Retain Diagram/Source preference across ordinary virtual remounts using the
existing bounded app presentation-state pattern (at most 128 block choices per
conversation, reset on conversation disposal). Zoom is dialog-local. Selection or
focus inside source must not be destroyed by an automatic source-to-image swap.

### Untrusted content and failure behavior

- All model/history content is untrusted. Never execute a click directive, allow
  source-selected fonts/URLs, or opt into library interactive modes.
- Keep theme configuration application-owned. Unsupported init/frontmatter/style
  features should retain source with an explanation rather than silently
  overriding the user's appearance or rewriting what the diagram means.
- Confirm the library's supported grammar and escaping against malicious labels,
  entity encoding, attribute breakouts, CSS URLs, `foreignObject`, and scripts.
  An image-context boundary is not permission to accept arbitrary SVG elsewhere.
- Confirm no network request is caused by diagram contents, font imports, or
  external references. Keep renderer assets local and usable offline.
- Invalid syntax: “Couldn't render this diagram. Showing source.” Unsupported:
  “This diagram syntax isn't supported yet. Showing source.” Limits/timeouts and
  load failure get distinct truthful wording. No raw stack traces or error SVG.
- Allow a user-triggered retry for load/transient failure, not automatic repeated
  retries of invalid/over-budget input.
- Accessibility: meaningful image alt/caption, original source always reachable,
  authored `accTitle`/`accDescr` used when reliably supported; never fabricate an
  understanding of the graph. Source is a fallback, not a claim of equivalent
  screen-reader comprehension. Test actual announcements and keyboard behavior.

## Proposed file scope

Names of new helper files may be consolidated after the spike; avoid speculative
layers.

| Files | Planned change |
| --- | --- |
| `packages/app/src/streaming-markdown.tsx` | Route Mermaid fences through shared opt-in dispatch; pass settled/partial state; preserve selection and offsets |
| `packages/app/src/timeline.tsx` | Wire the other Markdown path to the same dispatch; preserve copy/read/virtualization behavior |
| `packages/app/src/markdown-code-block.tsx` (new) | Small shared Markdown-only language/readiness dispatcher and app callbacks |
| `packages/ui/src/mermaid-block.tsx` (new) | Theme-native image/source shell and accessible expanded view |
| `packages/ui/src/mermaid-renderer.ts`, `mermaid-worker.ts` (new) | Lazy worker adapter, bounded lifecycle/cache, library-output font preparation |
| `packages/ui/src/index.ts`, `packages/ui/package.json`, root `package-lock.json` | Export and exact reviewed library dependency; verify MIT/transitive licensing |
| `packages/ui/stories/` | Real diagram family, state, theme, narrow-layout and accessibility stories |
| `packages/app/test/streaming-transcript.test.tsx`, `timeline.test.tsx`, new Mermaid tests | Both Markdown paths, streaming/settled/history, failure/copy/selection tests |
| `packages/ui/tests/`, `apps/web/scripts/mermaid-diagrams.mjs` (new) | Actual renderer/worker, image, production-CSP, visual and reading-position browser coverage |
| `docs/frontend.md`, `docs/features.md` | Document owner boundaries, supported syntax, limits, security/display decision, and behavior-to-test references when shipped |

No generated theme files, native mobile code, protocol types, daemon config, or
production CSP changes are planned. Add a roadmap entry only if useful for tracking;
there is no pre-existing Mermaid checkbox to mark complete.

## Ordered implementation plan and acceptance gates

- [x] Trace both Markdown paths, code/theme conventions, and current CSP.
- [x] Compare primary library/API/security sources and perform minimal CSP probe.
- [ ] **Approval:** accept the six-family subset and source fallback, or explicitly
  prioritize full Mermaid compatibility and revise the recommendation.
- [ ] **Small integration spike, before product wiring:** exact published library,
  real examples from every claimed family, syntax-fidelity corpus, actual module
  worker build, local fonts in SVG images, complete CSP/network checks. Measure
  lazy chunk bytes, cold load, warm layout, memory, and image decode. No upstream
  marketing performance number substitutes for these measurements.
- [ ] **Go/no-go:** prove readable fonts/themes, no CSP relaxation or outbound
  requests, no misleading parser fallbacks, and acceptable bounded performance.
  Test Chromium, Firefox, Safari/WebKit and packaged desktop. If the engine needs
  a substantial fork, stop and revise rather than accumulating patches.
- [ ] Build the minimal UI/adapter and approved failure states. Review real
  light/dark/custom-theme screenshots at wide/narrow sizes before wiring chat.
- [ ] Integrate both Markdown paths and lifecycle handling; verify live, settled,
  interrupted, resumed, and partially retained responses. Non-Mermaid fences,
  tool code/output, REPL, and response copy must remain unchanged.
- [ ] Add automated coverage and inspect actual browser output; preserve selection,
  scroll anchors, focused controls, and state through virtualization/remounts.
- [ ] Update canonical frontend/features docs, dependency audit, and this plan with
  actual results and deviations. Obtain an adversarial simplicity/security review.

### Validation matrix

- **Unit/component:** dispatch and readiness; original-source copy; all fallback
  reasons; queue/cache count+byte bounds; stale results; worker timeout/recovery;
  object URL cleanup; theme/contrast/font change; malformed/truncated source.
- **Real rendering:** flowchart with groups/edge labels, sequence with loops,
  state transitions, class compartments, ER cardinalities, XY axes/legends; Unicode,
  long labels, disconnected graphs, repeated diagrams, and unsupported types such
  as Gantt/pie/mindmap. Do not claim family-wide compatibility from one happy path.
- **Security/CSP:** actual production policy; malicious labels/directives/URLs;
  no script execution, external requests or global styling; library font imports
  removed; valid blob image rendering; no unsafe-inline/eval additions.
- **Interaction:** keyboard-only controls; dialog focus return; touch widths;
  source selection preserved; reduced motion; browser zoom; 100%/Fit; no transcript
  wheel hijack; correct behavior when rows resize above the reading anchor.
- **Theme/visual:** all-theme smoke coverage; reviewed baselines for light, dark,
  Claude Code, high contrast, and custom themes; preview/cancel and UI/code fonts;
  graph labels remain legible rather than blindly inheriting low-contrast defaults.
- **Performance:** zero renderer load for ordinary chats, one lazy engine/worker,
  bounded dense-input behavior, stable frame responsiveness during layout, bounded
  memory over repeated virtual remounts/theme changes. Record actual artifact sizes.
- **Build/platform:** web and desktop packaged assets, worker/font asset URLs,
  offline behavior; Storybook and packed UI consumers. Mobile/TUI source unchanged.

Implementation checks: `npm run check:web`, `npm run test:web`,
`npm run check -w @whip/ui`, `npm run test -w @whip/ui`, affected Storybook/browser/
visual/CSP tests and `npm run test:packed -w @whip/ui`; then packed web diagram and
reading-position browser tests. The generic UI CSP fixture is stricter than the
application on images, so retain it and add a diagram fixture using the *actual*
production policy rather than weakening that fixture globally. Run repository-wide
release checks as appropriate; record unrelated baseline failures, do not fix
other in-progress work opportunistically. No active daemon restart is needed.

## Research status / remaining uncertainty

This document is a plan, not an implementation or a completed security review.
Only the minimal Chromium/Firefox CSP experiment ran. Actual library rendering,
fonts, syntax coverage, accessibility, bundle/performance measurements, and Safari
remain unverified. Existing unrelated working-tree changes were left untouched.
