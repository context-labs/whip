# Whip docs site

Branch: `feat/docs-site`
Worktree: `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-docs`
Base: fetched `origin/main` at `071fe7e34e75b04fcce19af20f47e5525fb46db7` (2026-09-24).
Status: scaffold, app-owned library and six launch articles implemented. Docs-local validation and the full repository `task check` pass. Public hosting/domain and final asset/visual approval remain launch gates. See [validation results](validation.md) and [implementation ownership](implementation-contract.md).

The phase boundaries below record the original plan. The user subsequently
approved all three phases end to end, without intermediate approval waits.
Implementation results and deliberate exceptions are recorded in validation.md;
this plan is not a substitute for the canonical docs/frontend.md guide.

## Goal

Build an independent public whipcode site in `apps/docs`, using the relevant
skeleton of inference's `apps/fast-web`: React, TypeScript, Vite, TanStack
Start/Router, repository-authored MDX, and an app-owned component library.
Deliver in three reviewable phases: scaffold, components, then pages.

## Confirmed decisions

The user selected these during planning:

- **Static pre-rendered output.** No runtime server or backend is required to
  serve the deployed site. Start can render during the build; deploy only the
  resulting public HTML/assets, not its server bundle.
- **Match fast-web's site structure.** Reserve `/` for the site landing page and
  use `/docs/...` for documentation. Other top-level sections can be added when
  they have real content; do not create empty pricing/dashboard/auth pages.
- **App-owned docs library.** Use Base UI with ordinary CSS and semantic custom
  properties, rather than importing or changing `@whip/ui`/StyleX.
- **Implement the supplied Paper board, not merely its general style.**
  [Design Tokens — Dark](https://app.paper.design/file/01M3A92K3HM4T5QF90SJFKQR0A/p-1-0/1O0-0)
  is the component-library acceptance reference. Use the companion Light board
  for light mode and the Fast Inference reference pages for page composition.
  See [board research and component inventory](design-research.md).
- **Multicolour syntax highlighting is required.** The user's updated brief
  supersedes the guide and Paper samples' monochrome-code rule. Use build-time
  tokenization and theme-aware Carbonfox syntax colours; keep code-block chrome
  faithful to the board.
- **Design authority:** latest user requirements, then the supplied component
  board and its light counterpart, then `docs/brand-guide.md` for complementary
  guidance. Record board/guide discrepancies rather than silently guessing. The
  copied guide is still unchanged; reconcile its monochrome and focus-only-blue
  wording before implementing the library. No Paper design was edited in this
  research pass. Fast-web remains a structural, not visual, reference.

Recommendations not requiring an extra repository or toolchain:

- Workspace `@whip/docs` in `apps/docs`; keep whip's Node 24, npm workspaces,
  private ESM packages, and single root `package-lock.json`.
- Author docs in `.mdx`: ordinary Markdown plus optional approved React content
  components, matching fast-web. No CMS, remote Markdown, or runtime parsing.
- Keep public site content in `apps/docs/src/content/docs`. Existing `docs/`
  remains the engineering/manual source inventory, not an automatically
  published directory. Decide migration ownership page by page in phase 3.

## Non-goals

No daemon/SDK/protocol dependencies, backend endpoints, server functions,
React Query, tRPC, auth, database, secrets, analytics, live release/model fetching,
CMS, public search service, or deployment-account provisioning. No migration from
npm to Bun/Turbo. No changes to the desktop/web renderer or runtime packaging.
No new shared UI package, docs framework, or separate design-token build system.
Search, versioned docs, localization, Mermaid, blog/guides collections, and
machine-readable exports are follow-ups unless a concrete page requires them.

## Research and prior art

Reference root:
`/Users/samheutmaker/desktop/context-labs/src/monorepo/inference/apps/fast-web`.
Research inspected the working files on 2026-09-24; surrounding monorepo HEAD was
`12ddcc780e`. This is an architectural reference, not a dependency or a promise
that the working files equal that commit.

| Evidence | Finding and application |
| --- | --- |
| fast-web `package.json:7-23,25-68` | Bun scripts; React 19, Vite 8, Start/Router, Base UI, MDX/remark, Vitest. Preserve the relevant framework shape, but use whip's packaging and compatible installed versions. |
| fast-web `README.md:46-55` | App-owned `components/ui`, feature slices, thin routes, CSS styles. This is the requested ownership pattern. |
| fast-web `vite.config.ts:31-68` | MDX/GFM/frontmatter precede Start and React. Cloudflare, auth validation, Buffer polyfill and API proxy belong to its full-stack app; omit them here. |
| fast-web `tsconfig.json:2-26` | Inherits inference config and backend project/type references. Write a standalone docs config; do not copy those references or Bun/Worker types. |
| fast-web `src/router.tsx:5-17` and `src/routes/__root.tsx:29-73` | Router factory, scroll restoration, root document/head. Retain those conventions, remove providers, third parties and unrelated global styles. |
| fast-web `scripts/generate-docs-manifest.ts:20-31,68-94,126-177` | Discovers `**/index.mdx`, validates metadata/order/path, extracts headings, and writes a manifest. Adapt the small content pipeline, not all product-specific rules. |
| fast-web `src/features/docs/content/docs-schema.ts:21-29,70-82` | Frontmatter and manifest contracts. Start with title, description, section and order; derive URL from the file path instead of requiring redundant path metadata. |
| fast-web `src/features/docs/content/docs-registry.tsx:25-45,68-107` | Bundled MDX modules via `import.meta.glob`, metadata-driven lookup/navigation, per-page component loading. No remote content API is necessary. |
| fast-web `src/routes/docs.$.tsx:12-32` | Splat route resolves compiled content and head metadata. Loader means a local module lookup, not backend data fetching. |
| fast-web `src/content/docs/getting-started/index.mdx:1-24,60-71` | Markdown + YAML frontmatter + optional Callout/Steps/CodeTabs. Keep the authoring model; do not copy inference product content. |
| fast-web `src/features/docs/components/DocsShell.tsx:8-28` | Header, skip link, responsive nav, article and TOC composition. Build equivalent docs components only in phase 2. |
| whip `package.json:5-12,43-62`, `docs/frontend.md:84-98,139-160` | Node 24/npm/root lockfile; existing React/Vite/Vitest/Playwright/Storybook tooling. Reuse rather than introduce another workspace manager. |
| whip `packages/ui/package.json:6-17,39-57` | Shared app UI requires StyleX and includes app-oriented fonts/themes. The selected app-owned docs boundary avoids importing this whole styling contract. |
| whip `docs/brand-guide.md:15-26,115-126,379-385,408-420` | Neutral reading-first design, system fonts, dark/light tokens; no step rails or pre-H1 eyebrows. Its monochrome-code rule is superseded by the updated user brief. |
| Paper WHIPCODE DOCS, supplied Dark board and companion Light board, token hash `36d994cb` | Read the actual token export, component hierarchy, representative computed styles and code-block JSX; captured the Dark board screenshot. Exact findings and unresolved discrepancies are in `design-research.md`. |
| whip `packages/ui/src/code-highlight.ts:1-17,21-28`, `packages/ui/package.json:49` | Refractor 5 is already adopted. Reuse the dependency and grammar approach at MDX build time, not the app's lazy runtime renderer or StyleX package. |
| whip `internal/theme/themes/carbonfox.json:28-36`, `carbonfox-light.json:28-36` | Existing eight-role syntax palettes; all measured pairs exceed 4.5:1 on the board's code-panel backgrounds. |
| whip `.github/workflows/ci.yml:3-11,253-254` | The required `go` aggregate gate must depend on new CI jobs for failures to block merges. |

Official framework verification:

- [TanStack Start static prerendering](https://tanstack.com/start/latest/docs/framework/react/guide/static-prerendering):
  supports static HTML output, explicit `pages`, automatic static-path discovery,
  optional link crawling and `failOnError`. Parameterized routes are not
  automatically enumerated; generate every `/docs/<path>` from the content
  manifest instead of relying on crawl coverage.
- [TanStack Start hosting](https://tanstack.com/start/latest/docs/framework/react/guide/hosting):
  deployment adapters vary. Do not carry over Cloudflare configuration simply
  because fast-web uses it. Validate the chosen version's actual public output
  directory and static behavior in phase 1.

The feature map and roadmap describe the existing application and engineering
manual, not an already shipped public docs site. The frontend guide remains
canonical for the application; implementation must document the docs-specific
boundary there instead of silently creating conflicting guidance.

## Architecture

```text
apps/docs/                         @whip/docs (private ESM npm workspace)
  package.json
  tsconfig.json                    strict, Vite/Node types, ~/ -> src/
  vite.config.ts                   MDX -> Start (prerender) -> React
  vitest.config.ts
  scripts/                         content generation and output verification
  public/                          local, public assets only
  src/
    router.tsx
    routeTree.gen.ts               generated
    routes/
      __root.tsx                   document, head, outlet; no app providers
      index.tsx                    / landing placeholder until phase 3
      docs.tsx                     docs layout boundary
      docs.index.tsx               /docs index
      docs.$.tsx                   compiled local MDX lookup + not-found
    content/docs/<path>/index.mdx
    features/docs/
      content/                     metadata schema, generated registry, lookup
      components/                  phase 2: article, TOC, nav, pagination
      docs-components.tsx          phase 2: approved MDX component mapping
    components/
      ui/                          phase 2: accessible reusable controls
      brand/                       phase 2: wordmark/brand elements
      navigation/                  phase 2: site header/mobile navigation
    styles/                        phase 2: tokens, base, UI, docs, site layouts
  .storybook/                      phase 2; app-local config, root tooling
  README.md                        commands, authoring, static output contract
```

Do not create empty feature folders or placeholder abstractions just to match
this diagram. Add files as their phase needs them. A modest amount of local
component state for menus, tabs and theme preferences is sufficient; there is
no application data store.

### Content/build contract

1. Trusted checked-in MDX is compiled during build. It is executable source;
   do not accept untrusted or remotely supplied MDX.
2. `index.mdx` directory path is the URL source of truth. Frontmatter supplies
   `title`, `description`, `section`, and `order`; section configuration supplies
   labels/order. Dates are optional until there is a real display/use case.
3. Validate metadata, unique paths/order and valid section references. Generate
   metadata for navigation/head/TOC and enumerate all prerender URLs.
4. Use one heading/slug rule for generated TOC and rendered headings, preferably
   through the existing Markdown compiler AST. Test inline markup, duplicate
   headings, punctuation, Unicode, and fenced code. Do not blindly copy the
   reference's regex-only heading parser and separately maintained slug logic.
5. Keep the global manifest metadata-only. Fast-web embeds complete raw bodies
   in its manifest; do not send every article's source to every visitor. No raw
   Markdown export/download feature is needed initially.
6. Compile page modules separately. Loading bundled JS/MDX chunks is normal
   static asset loading, not API data fetching. Route loaders must not call
   `fetch`, server functions, or external services.
7. Content add/edit/delete must refresh metadata and route enumeration in dev
   without a manual generation command. Use a small Vite watch integration with
   the generator, not a separate content service. Generated files must not
   trigger rebuild loops.
8. Generated files are ignored and reproducible. Dev/build/check generate what
   they need from a clean checkout before using it; no reliance on checked-in
   generated output. Prefer the Start plugin's route generation; add a route
   generation command only if standalone type checking requires it.
9. Prerender failures fail the build. Render `/`, `/docs`, each manifest URL and
   a not-found document. Verify nested direct requests, reloads, trailing-slash
   policy, redirects and real 404 status on the selected static host.
10. Public output is HTML, CSS, JS and local assets only. A server bundle may be
    a build intermediate but is neither deployed nor required for preview.
    No SPA catch-all hiding missing prerendered pages.

### Dependencies

Keep React/React DOM, TypeScript, Vite and its React plugin aligned with whip.
Resolve a compatible Start/Router set without unnecessarily upgrading existing
apps or forcing unrelated TanStack package versions to change. Record the
resolved versions in manifests/lockfile, not as historical hardcoded numbers in
this plan.

Add only the needed Start and MDX toolchain (`@mdx-js/rollup`, `remark-gfm`,
`remark-frontmatter`), YAML/frontmatter validation
using `yaml`/`zod`, and any directly imported compiler utilities. Declare direct
imports explicitly even if already transitively installed. Use Node 24 for
build scripts, not Bun-specific APIs. Base UI is used; small app-owned SVGs
avoid an unused Lucide dependency. Vitest, Playwright and
Storybook are existing workspace tools.

Declare `refractor` as a docs build-time dependency aligned with the existing
workspace version. Use its core entry point plus explicitly registered grammars
in a small rehype adapter; MDX compiles its HAST token nodes to React output.
The adapter is build tooling, never imported by client components. Reuse compiler
AST traversal utilities where available; do not introduce a second highlighter
or a large wrapper for unused line-number/diff features. See the comparison and
language/colour contracts in `design-research.md`.

Do not add Highlight.js, Shiki, Mermaid, Tailwind, a docs framework, state
management, or a second test/story toolchain for this scope.

### Syntax-highlighting contract

- Highlight fenced code during MDX compilation, including fences inside CodeTabs.
  Static HTML and client navigation both render the same compiled token spans;
  no runtime highlighter, grammar download, language detection, or theme reparse.
- Use explicit language labels and aliases; unknown/missing languages remain
  escaped, readable text. Lint unsupported explicit labels so typos are visible.
- Initially support Bash/curl, JavaScript/TypeScript, Python, Go, JSON and YAML.
  Starlark may use Python grammar as a documented approximation. Add actual
  JSX/TSX/JSONC grammars only when used, not inaccurate aliases.
- Map Prism token classes to eight `--color-syntax-*` roles with light twins;
  use the existing Carbonfox palettes as the starting values. Theme changes
  swap CSS variables only. Inline-code chips stay neutral, as on the board.
- Preserve source whitespace and characters exactly. Copy uses the active
  snippet's original text, never highlighted HTML, line labels or tab names.
  Keep raw text per snippet/page, not in the global content manifest.
- Build tests must cover token classes, unknown language fallback, escaped
  `<script>`/ampersands, source text fidelity and nested MDX code fences. Browser
  tests cover theme contrast, selected-tab copy, overflow and no-JS reading.

## Phase 1 — Scaffold and proof of the static pipeline

Deliverable: a clean checkout can develop, type-check, test, build and serve the
site without credentials, Go/daemon builds, or any running backend. This phase
proves framework/content infrastructure, not visual design.

- [x] Research fast-web, whip packaging, the brand guide and Start prerendering.
- [x] Confirm rendering, site URL structure and component ownership.
- [x] Create worktree from fetched main; copy the brand guide unchanged.
- [x] Approve this implementation plan.
- [x] Add `apps/docs` package/config, root npm commands and lockfile changes.
- [x] Add strict TypeScript and Vite/Start/React wiring with local aliases and
      generated file ignores; choose a nonconflicting dev port with strictPort.
- [x] Add root, docs index, MDX splat route and explicit not-found handling.
- [x] Add the minimal typed content pipeline and regeneration during development.
- [x] Add build-time fenced-code highlighting with Refractor, a minimal syntax
      stylesheet and a tokenized static-HTML fixture. This proves the compiler
      path only; finished code-block controls and board styling remain phase 2.
- [x] Exercise temporary/nested MDX fixtures in automated compiler/dev tests.
      End-to-end authorization allowed real launch content to replace page
      placeholders immediately; no temporary article is publicly prerendered.
- [x] Enumerate prerender paths from content; prove static-only output using a
      static server rather than an SSR preview server.
- [x] Add small generator/routing tests and Playwright static-output smoke tests.
- [x] Add a docs job to existing CI and the required aggregate gate. Install
      Node/npm dependencies, check, test, build and smoke the public artifact;
      no deployment secrets. Follow existing action pins/conventions.
- [x] Document commands, source/output boundaries and the app-owned CSS exception
      in `apps/docs/README.md`, `docs/frontend.md`, and applicable agent guidance.
      Add the feature-map/roadmap entry only with truthful implementation status.

Implemented root commands (also see apps/docs/README.md):
`dev:docs`, `check:docs`, `test:docs`, `build:docs`, `preview:docs`,
`test:docs:browser`, `test:docs:dev`, `storybook:docs`, `build:storybook:docs`. Existing `build:web`, `pack:web` and release scripts stay
unchanged; docs assets must never get embedded in the whipcode runtime.

Acceptance gates:

- Fresh `npm ci` + docs commands work on Node 24, without an `.env` file.
- Rendered HTML already includes article content/title and syntax token spans
  before JavaScript runs; the syntax stylesheet provides multiple token colours.
  No Refractor/Prism runtime or grammars appear in production browser chunks.
- Both flat and nested MDX routes load directly and after browser navigation;
  back/forward, fragments, unknown paths and reloads behave correctly.
- Static output contains an HTML document for every content URL, not just `/`.
- The production page makes no API/auth/analytics/daemon requests. No hydration
  or browser console errors; a no-JavaScript pass can read prose and links.
- Bad metadata, duplicate paths and a missing compiled page fail with useful
  source-file errors. Repeated heading text receives deterministic unique IDs;
  TOC IDs match rendered IDs.
- Content additions/deletions and metadata edits work in a running dev server.
- Existing workspace checks still pass. Inspect lockfile changes for unintended
  framework upgrades; full repository gates apply before merging.

Original intermediate stop was superseded by the user's end-to-end approval.
Scaffold validation is complete; repository-wide gates remain independently
required and are tracked in validation.md.

## Phase 2 — Component library

- [x] Implement board-family primitives, docs shell, theme/mobile behavior,
      Storybook reference/states and real MDX syntax integration.
- [x] Pass component/contrast tests and browser keyboard/copy/no-JS checks.
- [ ] Finish independent board-size review and establish user-approved visual
      baselines. Automated tests do not imply final visual sign-off.

The following numbered list is the retained design checklist:

Deliverable: app-owned primitives and docs compositions reviewed independently
of production page content, in an app-local Storybook using the existing tooling.

1. Implement the supplied Paper component board with the inventory and measurements
   in `design-research.md`. Re-read current tokens/computed styles before coding;
   do not copy exported div-heavy JSX verbatim or infer dimensions from screenshots.
   Reconcile the worktree brand guide with the user-approved highlighting change
   and board-specific values; list remaining discrepancies for design review.
2. Build foundation stories for colours, typography, spacing and radii. Use
   carbonfox dark/light CSS roles, system sans/mono and first-paint/system-theme
   handling without hydration mismatch. Add the syntax palette as an explicit
   extension to the board; do not recolour ordinary prose/navigation.
3. Build all board component families and states, not just the controls needed
   by the first article: primary buttons; copy/icon and split buttons; sidebar
   links; top-nav links; TOC links; community links; four callouts; single/tabbed
   code blocks; inline-code chips; tables; previous/next cards. Native elements
   first, Base UI for interaction/focus/keyboard behavior.
4. Add supporting tooltip/menu/tab behaviors, mobile drawer and theme control.
   These behaviors are necessary extensions, not fully specified by the desktop
   board. Build site header/brand and docs shell compositions using reference
   pages, then review proposed mobile/tablet arrangements separately.
5. Integrate compiled multicolour code spans into the board's 40px code header,
   20px body padding, 6px panel, tab strip and 28px copy control. Use semantic
   pre/code, preserve active-snippet copy exactly, announce success/failure, and
   keep overflow horizontal without page overflow. Inline code remains neutral.
6. Create a dedicated Storybook Board Reference composition matching the board's
   foundation/component order, plus state stories for each family. Include
   dark/light, hover/focus/disabled/active, long content and narrow widths.
   No production docs pages or branded marketing copy are needed for this step.
7. Test keyboard navigation, accessible names, contrast, zoom, reduced motion,
   mobile dismissal, tab selection/copy and no-JS code readability. Token spans
   must already be present in static HTML and must not change at hydration.
8. Capture board-sized and component-level comparisons, review deliberate
   deviations (especially multicolour code), and establish approved browser
   visual baselines. Review before proceeding to page construction.

Do not extract into `packages/*` unless a second real consumer justifies it.
Storybook and fixture pages are development artifacts, not production routes.

## Phase 3 — Pages and publication readiness

- [x] Audit and implement six focused launch articles, landing and docs index.
- [x] Verify static routes, links/fragments, preview/publication metadata modes,
      no-JS behavior, local-only requests and responsive browser smoke tests.
- [ ] Select/verify real static host, canonical domain and final social assets.
- [ ] Complete repository-wide regression review and final publication approval.

The following numbered list is the retained publication checklist:

Deliverable: reviewed site pages using the accepted library and checked-in MDX.

1. Agree an information architecture and launch page list. Candidate docs groups:
   getting started/install, Desktop and CLI usage, configuration/models,
   agents/tools/integrations, reference and troubleshooting. These are proposals,
   not an instruction to publish every internal document.
2. Audit existing `docs/README.md`, `setup.md`, `tools.md`,
   `browser-computer-use.md`, and `models-providers.md` as source material. Confirm
   commands against the intended release and choose canonical ownership when
   migrating user-facing content; avoid two divergent manuals.
3. Build `/` and `/docs`, then the approved articles. Remove scaffold fixtures.
   No placeholder pricing/dashboard/auth routes simply to resemble fast-web.
4. Finish metadata, social assets, canonical URLs, generated sitemap/robots and
   internal-link/fragment checks. All generation remains build-time.
5. Select the static hosting provider and canonical domain before public launch.
   Configure clean URLs, actual 404 responses, redirects/cache headers and
   preview indexing policy; verify the real deployment adapter behavior.
6. Review content/security, mobile and desktop visuals, keyboard accessibility,
   no-JavaScript reading, and no unexpected requests. Smoke every generated page.

The six-page launch list and verified GitHub destinations are implemented.
Host/domain and final public social assets remain launch decisions. Analytics, hosted search,
release fetching and other network features require separate scope approval.

## Files expected to change during implementation

- New `apps/docs/**` (only phase-appropriate files).
- Root `package.json`, `package-lock.json`; `.gitignore` only if app-local ignores
  cannot cover generated artifacts.
- `.github/workflows/ci.yml` docs job + required gate dependency.
- `docs/frontend.md` boundary/package/commands, `docs/features.md` and
  `docs/roadmap.md` implementation status; root `AGENTS.md` / new
  `apps/docs/AGENTS.md` to make the docs guide discoverable.
- `docs/brand-guide.md` (the preserved input; intentional revisions only).
- This plan as checkpoints and deviations are recorded.

No product runtime source, Go implementation or release pipeline changes were
needed. One approved boundary exception isolates ambient TypeScript types in
packages/protocol, packages/sdk and examples/client after the new MDX compiler's
legacy types exposed an existing implicit-global assumption. No global JSX shim
or skipLibCheck relaxation was added. See validation.md.

## Historical research-session verification (before implementation)

The statements below describe only the initial research turn. They are not the
current worktree/build status: dependencies and implementation now exist and
have been tested. Current evidence is in [validation.md](validation.md).

- Fetched `origin/main` and based the new branch on its then-current commit.
  Local `main` was behind; it and the original active branch were not reset.
- Removed automatic tracking of `origin/main` from the feature branch; no push,
  commit, staging, dependency install, build or deployment was performed.
- Copied the guide without overwrite and compared it byte-for-byte.
- Only the plan, its design-research companion, and the unchanged copied guide
  are new files in the docs worktree. No app implementation or Paper edits.
- A read-only Refractor smoke check against the original checkout's installed
  dependency produced tokens and preserved text for Bash/curl, TypeScript,
  Python, Go, JSON and YAML. Carbonfox syntax/panel contrast was calculated for
  both modes. These are research checks, not an implemented MDX pipeline test.
- Framework static support is documented research, not a tested scaffold result;
  the phase-1 static artifact smoke test is the implementation proof.
