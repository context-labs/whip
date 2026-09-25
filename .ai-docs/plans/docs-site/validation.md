# Docs-site implementation validation

Date: 2026-09-24. Worktree: `whip-docs`, branch `feat/docs-site`, base
`071fe7e34e75b04fcce19af20f47e5525fb46db7`. Runtime: Node 24.14.1, npm 11.11.0,
macOS. The user approved all three implementation phases without intermediate
waits. This is an implementation record, not a claim of public deployment or
user-approved visual baselines.

## Follow-up: Quickstart replaces Introduction (2026-09-24)

Removed the Introduction MDX page and made Quickstart the first sidebar item.
`/`, `/docs`, `/docs/introduction` and `/docs/getting-started` redirect directly
to `/docs/quickstart`, including legacy aliases and no-JavaScript static fallbacks.
Download remains the next page; the site now has 21 pages (three written, 18
outlines). No other navigation groups or SDK content changed.

Read OpenCode's live `/v2/docs/` intro: concise product overview; CLI install tabs;
Desktop and Web options; Connect and Customize sections. Measured its H1 30/36,
H2 20/30, 48px section margin and 12px following gap. Applied that installation-first
hierarchy and page-local typography/spacing to Quickstart while retaining whip's
1280px shell, theme, header, nav, code components and a short first-task example.
Removed numbered steps and repeated heading borders. Omitted OpenCode-specific
package managers, Docker images and unsupported platforms.

Passed docs typecheck, 69 unit tests, live dev/redirect checks, preview and
publication builds, Storybook and all 30 browser tests. Tests cover the removed
page redirect, single sidebar entry, ordering, first-install copy, non-numbered
borderless headings, no-JS reading and updated pagination. Native browser attach
was unavailable in this pass; Playwright captured responsive screenshots and
independent axe results in `/tmp/whip-docs-quickstart-intro`: six combinations of
320/390/1440px and dark/light passed with no WCAG A/AA violations, page errors or
horizontal overflow. No deployment/commit.
Full repository task check not rerun for this docs-only change.

## Follow-up: TypeScript SDK reference (2026-09-24)

Filled the six approved SDK sections with ten TypeScript examples: local socket
and HTTP connections, session/run lifecycle, upload references, streaming with
child separation and discarded drafts, explicit human permission decisions,
deadlines, served custom tools with typed output, state-view ownership and React
subscriptions. Documents private package status, Node 24 source-checkout usage,
gateway trust boundary, host paths/configuration, cancellation versus detach,
command acceptance/recovery and executor lifetime. No public npm installation
claim or automatic permission approval.

`tests/sdk-examples.test.ts` extracts the actual fenced examples and typechecks
them against the current SDK source, not handwritten mock declarations. This
caught incorrect draft permissions/instructions and a source-vs-dist test mapping;
all ten final examples pass. Five additional offline behavior checks exercise
stream/discard/failure, upload order, deadline signal and explicit allow/deny
through local doubles. No live daemon, provider/model or tool side effect runs.

Docs typecheck, 69 unit tests, SDK build, docs build, Storybook and all 30 browser
tests pass. Browser checks include highlighted source-copy fidelity and complete
no-JS examples at 320/1440px. Independent 320/390/1440 dark/light axe checks report
no WCAG A/AA violations or page overflow; screenshots/report at
`/tmp/whip-docs-sdk-review`. Full repository task check was not rerun. No SDK
production code, dependencies, commits or deployment changed. The other 19 pages
remain outlines; Quickstart, Download and TypeScript SDK are now written.

## Follow-up: Quickstart and Download (2026-09-24)

Filled only Quickstart and Download (about 300 words each), preserving their
agreed H2 outlines. Quickstart uses a five-step Desktop-first path with a TUI
alternative and two small prompts. Download includes a macOS Apple Silicon row,
CLI Install/Inspect first tabs, prerequisites, version check, platform table and
Desktop-versus-CLI update ownership. Existing library styling is reused; the
examples inspire the information hierarchy, not unrelated package-manager or
platform support. Other 20 articles remain outlines.

Source checks: worktree README, docs/setup.md, docs/desktop.md and install.sh.
Read-only GitHub release metadata confirmed public, non-draft v1.0.0-alpha.5
with its Desktop DMG and all four macOS/Linux CLI assets. The direct pinned DMG
URL returned HTTP 200 to a redirect-following HEAD request. Draft and historical
release artifacts were not used. No installer, binary or provider request ran.
The pinned version/link and test assertion should be updated together; no
backend or runtime release fetching was added.

Passed docs typecheck, 63 unit tests, static build, Storybook and all 26 browser
tests. Added onboarding coverage for content, direct download destination,
copying both installer variants, no invented package-manager options, and
no-JS usability at 320/1440px. Independent axe audits at 320/390/1440px in both
themes found no WCAG A/AA violations, page errors or horizontal overflow.
Screenshots/report: `/tmp/whip-docs-onboarding-review`. Full repository task check
was not rerun for this docs-only change. No commits or deployment.

## Follow-up: V1 heading-only pages (2026-09-24)

Created all 22 approved pages with 104 H2 headings, brief metadata descriptions,
and no drafted body text. The five sidebar groups share one definition with the
content validator. Introduction, Quickstart and Download lead Getting Started.
All outlines have generated TOCs, sequential pagination and page-source actions.

`/` and `/docs` now enter Introduction. Existing getting-started, installation,
CLI and tools/permissions URLs redirect to Introduction, Download, TUI and
Permissions. Preview HTTP redirects, plain-host/no-JS fallback HTML and dev routes
are tested. Header Download links to the new Download page. Prior full articles
needed by component tests were retained as Storybook fixtures, never published.

Passed typecheck, 63 unit tests, live-dev add/edit/delete plus all legacy route
redirects, preview/publication builds (23 rendered pages including 404), Storybook,
and 22 browser tests. Browser coverage asserts every page's H1/H2/TOC, all 22
ordered sidebar entries in five groups, no-JS reading, source copy/download,
legacy URLs, mobile navigation and widths. Component interaction/style/syntax
checks now use standalone Storybook examples and no longer depend on article
copy. No dependencies installed, no commit/push/deployment. Prior theme-import
fix preserved; full repository task check not rerun for this docs-only change.

## Follow-up: transcript links and inline code (2026-09-24)

Matched the user-supplied whip transcript reference using the existing product
source (`packages/app/src/timeline.tsx` code/link styles) and Carbonfox palette.
Prose links are blue (#33B1FF dark / #0072C3 light), persistently underlined with
3px offset. Inline code is green on the subtle element surface, 0.85em mono,
inherited line height, borderless, 4px radius and horizontal-only 4px padding.
Long identifiers wrap without page overflow. Navigation, cards, buttons and
fenced code retain their existing styling. Added dedicated semantic roles,
updated the board specimen and added a LinksAndInlineCode Storybook story.

Passed typecheck, 63 unit tests, all 28 browser tests, static build and Storybook.
Eight added contrast assertions cover links on page/panel/element and inline code
on its background in both themes. Browser checks cover geometry, relative font
size, hover/focus, table inline code and isolation from nav/fenced code. Independent
axe audits of installation/getting-started at 390/1440px in both modes found no
WCAG A/AA violations. Screenshots/report: `/tmp/whip-docs-inline-review`.
No product application code or dependency changes; no full repository test rerun.

## Follow-up: code-block spacing (2026-09-24)

Compared the component board and page code blocks. Both specify 20px body
padding and 12px/20px mono text; existing body geometry already matched (60px
for one line, 100px for three). Page headers `12G-0`/`13A-0` additionally specify
12px left inset and a 16px gap to the copy control. Removed the flush-left tab
header override, restored those measurements, and centred 27px-line-height
labels as in page node `12J-0`. Moved the negative border overlap from each tab
to the horizontal scroller so the active underline is no longer clipped.

Passed docs typecheck, 55 unit tests, static build, all 26 browser tests and
Storybook. New geometry checks cover single/tabbed/no-JS code, 390/1440px widths,
20px padding, 40px header, exact line-count heights and unclipped underline.
The first added test used an ambiguous panel locator during a tab transition;
scoping by the intended panel name fixed the test. No code source/copy changes.
Full repository tests were not rerun for this focused CSS change.

## Follow-up: Getting started article Y3-0 (2026-09-24)

The user chose accurate whipcode onboarding copy in the Paper article structure,
not the reference's Fast Inference API copy. Implemented intro/info callout, five
numbered setup steps, warning callout, first-task and terminal-format code tabs,
workspace scope, troubleshooting table, section-only TOC and next-CLI card.
H1 reads Getting started; optional navTitle preserves the existing Get started
sidebar label. The left navigation's appearance, ordering and destinations are
unchanged, as are the header and shared 1280px shell.

Title-row copy and download-source actions use the authored MDX in a page-local
lazy chunk. Browser testing caught the MDX Rollup plugin compiling `?raw` imports
before Vite's raw loader; a shared adapter now bypasses those requests in both
site and Storybook. Unit and real browser tests assert exact source fidelity,
including download bytes, visible failure recovery and no-JS content.

Final checks passed: typecheck, 55 unit tests, all 22 browser tests, live content
add/edit/delete, preview/publication builds and Storybook. Eight independent
320/390/768/1440 × dark/light axe audits reported zero WCAG A/AA violations,
no page errors and no horizontal page overflow. Measured article header gap is
36px, step heading/body gap 12px and split-action width 65px. Screenshots and
JSON: `/tmp/whip-docs-getting-started-review`.

The split controls have 28px hit targets (30px outer) rather than the reference's
24px; this documented accessibility accommodation does not change their width.
Full repository task check was not rerun for this docs-only follow-up. No
external provider/installer calls, design edits, commits or deployment.

## Follow-up: Paper header Y8-0 (2026-09-24)

Header now matches the supplied Paper node: wordmark, GitHub/Discord icons,
1×16px divider and Download button, preserving the shared 1280px shell. Discord
uses the user-provided invite; Download links to repository releases. Text nav
and site-menu chrome were removed; docs navigation remains in the docs shell.
Theme selection moved to the footer. Added standalone dark/light header stories.

Typecheck, 53 unit tests, build, all 16 browser tests and Storybook pass. Browser
checks assert destinations, header/control/divider dimensions, spacing, footer
theme behavior and no page overflow from 320 to 1920px. Independent axe checks
at 320/390/1440px in both modes report no WCAG A/AA violations; screenshots and
JSON are at `/tmp/whip-docs-header-review`. Live header visually reviewed against
Paper: aligned groups, measured spacing/type and neutral contrast match; narrow
screens retain usable actions (icon-only Download below 370px). No Paper edits.
Full repository checks were not rerun for this focused header change.

## Follow-up: shared 1280px shell (2026-09-24)

Header, docs body, fallback page and footer share `--site-max-width: 1280px`
and identical responsive side gutters. The reading column fills the remaining
grid space; sidebar/TOC widths stay fixed. Browser geometry assertions check
aligned edges at 390/768/1440/1920px and exact 1280px width on large screens.

Typecheck, 53 unit tests, static build, all 14 browser tests and Storybook pass.
The first browser run exposed an existing tab-test timing race: it read the old
panel before keyboard activation completed. The assertion now waits for the
intended tab and associated panel before checking copied text; no component
behavior changed. Full repository checks were not rerun for this CSS-only change.

## Follow-up: root redirect (2026-09-24)

The landing page and its unused CSS were removed at the user's request. `/`
redirects to `/docs/getting-started` through the router and static preview (308).
The static build adds a minimal no-JavaScript meta-refresh/link fallback;
redirect URLs are excluded from the publication sitemap.

Follow-up checks passed: docs typecheck, 53 unit tests, 13 browser tests (including
no-JS redirect on a plain static host), live-dev redirect/content refresh, preview
and publication builds, and Storybook. The previous full `task check` below
predates this focused route change; it was not rerun for this follow-up.

## Docs-local commands and outcomes

All commands below ran in this worktree, with its own installed dependencies.
No original-checkout `node_modules` symlink was used.

| Command | Actual result |
| --- | --- |
| `npm install --no-audit --no-fund` | Passed; 1,764 packages installed. Existing transitive deprecation warnings; no unrelated version upgrades. |
| `npm ci --no-audit --no-fund` | Passed fresh install; 1,764 packages in 31 seconds. |
| `npm run check:docs` | Passed generation and strict app TypeScript. |
| `npm run test:docs` | **53/53 passed**, five files: 24 compiler/content, 1 static server, 2 static-policy, 17 syntax/contrast, 9 component tests. |
| `npm run build:docs` | Passed Start prerender and public-artifact verification for nine URLs. |
| `DOCS_SITE_URL=https://docs.example.test npm run build:docs` | Passed publication-mode canonical/robots/sitemap assertions. Reserved test origin only, not an approved domain. |
| `npm run test:docs:browser` | **12/12 Chromium tests passed** against the static server, not Vite/SSR. |
| `npm run test:docs:dev` | Passed live add/edit/delete and deleted-route 404 test on port 3102. |
| `npm run build:storybook:docs` | Passed isolated Storybook build. Generic >500 kB development chunk warning (Storybook/axe), not a docs runtime build warning. |
| `git diff --check` | Passed. |

Final integrated docs sequence passed in the scaffold lane at approximately
15:34 PDT and independently in the root lane at 15:37 PDT after final polish
and direct axe-core dependency declaration:

```sh
npm run check:docs && npm run test:docs && npm run build:docs &&   npm run test:docs:browser && npm run test:docs:dev &&   npm run build:storybook:docs
```

Earlier failures found and corrected during development included unavailable
in-progress CSS, test locator ambiguity/title mismatch, escaping in generated
script text, app-local component test harness CSS/MDX loading, and global MDX
type leakage described below. Final counts above supersede those interim runs.

## Static artifact, metadata and compiler proof

The build explicitly enumerates `/`, `/docs`, six manifest pages and `/404`:

- `/docs/getting-started`
- `/docs/installation`
- `/docs/using-whipcode/desktop`
- `/docs/using-whipcode/cli`
- `/docs/configuration`
- `/docs/tools-and-permissions`

Final default build: 408 client modules and 84 build-server modules transformed;
29 public files in `apps/docs/dist/client`, including ten HTML files (nine route
documents plus the `404.html` host copy). The metadata manifest is approximately
5.3 kB and contains only title/description/order/section/headings/path, never
article bodies or code source. Article MDX remains in separate lazy chunks.
`dist/server` is an intermediate and is not used by preview or deployed.

`verify-static.mjs` parses every HTML document and asserts one main/H1, title,
description, exact manifest heading IDs/text, local emitted asset existence,
valid final-page internal links/fragments, and precompiled syntax spans.
The compiler reads Markdown AST nodes rather than regex for headings and links.
Tests cover repeated/Unicode/formatted headings, nested docs paths, bad
frontmatter, duplicate metadata order/path, broken links/fragments, escaped
script/ampersand text, exact source whitespace and real nested fenced CodeTabs.
Unknown explicit languages warn and fall back to plain text; aliases use own
properties (prototype names cannot select a grammar). Starlark/bzl use the
explicitly documented Python approximation.

`content-plugin.mjs` inspects actual emitted chunk module IDs and fails if a
Refractor/Prism module enters a bundle. Additional static scan sentinels are unit
tested against deliberate leaked implementation text. There is no client
highlighter or runtime grammar fetching. CSS-only theme switches retain token
nodes. Code copy uses original source, not token markup.

The live dev smoke creates a uniquely reserved temporary nested-authoring page,
observes its manifest and SSR addition, edits its title, then deletes it and
asserts both manifest removal and HTTP 404. It cleans its own page/server and
regenerates the real manifest. The normal compiler also exercises nested
add/delete fixtures. No smoke fixture is a public route after testing.

## Browser proof

The 12 tests exercise:

- Every article by direct URL and reload, metadata and real heading IDs.
- Landing navigation, history back/forward, TOC fragments, sidebar navigation.
- Real 404 statuses and 308 known-alias redirects preserving query strings.
- JavaScript-disabled prose/code/tokens and both labelled tab snippets.
- Keyboard tab changes and exact active-snippet clipboard text.
- Visible clipboard-denial manual-copy recovery.
- No document overflow at 390, 768 and 1440 px across representative routes.
- First-paint saved theme, actual theme-menu radio selection, system media flips,
  persisted reload, and storage synchronization from a second real tab.
- Mobile dialog opening, Escape/focus restore and link navigation/closing.
- No-JavaScript mobile native disclosures and working docs links.
- No page errors or console errors on the normal article pass, no API/daemon
  calls, and all resource requests staying on the local static origin.

Component tests separately check 16 unrounded 4.5:1 syntax-token/panel contrast
pairs, scoped nested token CSS and component keyboard/clipboard/theme behavior.
Independent Paper-board visual review belongs to the components/root lanes;
passing automated tests alone is not final design approval.

## Preview/publication policy

The default artifact was restored after configured-origin testing. It emits
`noindex,nofollow`, real newline-separated robots `Disallow: /`, no canonical URL
and no sitemap. An explicitly configured HTTPS origin emits exact clean
canonical/Open Graph URLs, indexing robots and sitemap entries for the eight
real pages, never `/404`. Both modes are asserted during build; robots newline
and highlighter sentinel behavior have focused unit tests.

The static preview serves only real public files, supports GET/HEAD, normalizes
known trailing-slash/index/HTML aliases with 308, returns real 404 HTML/status
for unknown URLs, and rejects traversal/non-public/symlink escapes. It has no
SPA fallback. These are reproducible host requirements, not a configured public
provider. Root command proxies forward dev/preview/Storybook/browser arguments.

## Dependencies and approved boundary exceptions

Exact Start `1.168.49` depends on existing Router `1.170.32`; React/React DOM
`19.2.8`, Vite `8.2.2`, TypeScript `5.9.3`, Vitest `5.0.0` and the existing
Storybook/Base UI versions were retained. No product dependency was upgraded.

A structural comparison of HEAD/current package-lock package records found:

- 1,715 old records → 1,889 current records.
- 174 additions, **zero removals, zero existing version changes**.
- 99 `dev`-flag reclassifications; npm also rewrote record ordering/metadata.
- Large textual diff is not evidence of unrelated framework upgrades.

The new compiler transitively brings `@types/mdx@2.0.14`, whose global JSX
assumptions conflict with React 19 when unrelated workspaces implicitly load
all hoisted ambient types. Parent-approved narrow exceptions:

| Config | Change | Direct validation |
| --- | --- | --- |
| `packages/protocol/tsconfig.json` | `types: []` for wire-only contracts | `npm run check -w @whip/protocol` passed TypeScript, 16 interop tests and drift. |
| `packages/sdk/tsconfig.json` | `types: ["node"]`; explicit imports retain React types | `npm run check -w @whip/sdk` passed build and 458 tests. |
| `examples/client/tsconfig.json` | `types: ["node"]`; React types imported normally | `npm run build -w @whip/client-example` passed. |

No global JSX shim, `skipLibCheck` relaxation, production runtime change or
cross-app MDX declaration was added. App `env.d.ts` is noEmit and included only
by docs tooling. No installs are planned after this record without coordination.

## Repository-wide checks — root-owned update

Root independently verified:

- Production static installation page at 390/768/1440px in both dark/light:
  no browser errors, no external requests, no horizontal document overflow,
  correct initial system theme, and five rendered syntax colours per mode.
- Automated axe-core WCAG 2 A/AA and 2.1 AA checks on `/`, `/docs`,
  `/docs/installation`, `/docs/configuration` at 390/1440px in both modes:
  zero violations across 16 configurations. This is not exhaustive accessibility
  certification. Evidence: `/tmp/whip-docs-root-review/{report.json,axe.json}`.
- Reproducible `.storybook/audit.mjs` passed all six board/theme viewport
  configurations: no errors, no overflow, zero automated WCAG violations;
  measured 34px buttons, 6px corners, 40px code headers and 20px code padding.
  Thirty-six screenshots plus report JSON are at `/tmp/whip-docs-board-review`,
  including open menus/drawers and copy success/failure in both modes. The
  audit found a low-contrast pager-hover label; it now uses the secondary token.
- Visually inspected live dark Board Reference, dark code-tab component, light
  callouts and the production installation page through the desktop browser.
  Styling is consistent with the measured board roles; no pixel-perfect or
  user-approved golden-image claim is made.
- Independent content review checked 47 internal URLs/fragments, all public
  assets, unique H1/title/description, no-JS article content, and absence of
  private machine paths/planning documents/source maps in emitted output.
- `npm audit --omit=dev --audit-level=high` passed. It reported 14 moderate
  advisories in the existing Expo dependency tree; no forced dependency updates.
- `task contract`, `task sdk`, web/app typechecking and tests, shared UI tests,
  pack-web tests, dev-proxy tests, update-local tests and onboarding/renderer
  artifact tests passed individually.

The first `task check` passed Go formatting/vet/whipvet and all Go tests before
exposing the MDX ambient-types issue fixed above. A retry encountered intermittent
`TestProtocolBoundsInitializationConnectionsAndInFlightWork` (daemon test line
437); 20 focused repetitions then passed. No daemon code was changed.

The StyleX cold-dev test initially stalled in this fresh worktree but passed in
the warm original checkout. Investigation reproduced the stall in the original
checkout with forced cold optimization: StyleX eagerly loads imports while Vite
waits for the intentionally incomplete test import graph to become idle.
`apps/web/scripts/stylex-dev.test.mjs` now forces a cold cache and sets test-only
`optimizeDeps.holdUntilCrawlEnd: false`; product configuration and dependencies
are unchanged. Three exact cold test runs plus a fourth bounded run all passed
and exited normally. See `stylex-validation.md` for reproduction and exact checks.

**Final whole-repository `task check` PASSED** at 15:39 PDT after the test-only
fix, including Go checks/tests, protocol, SDK, web/UI tests, the cold StyleX test,
dev-proxy, update-local and onboarding/renderer-artifact tests. The earlier
daemon intermittency is retained above for transparency; final full run passed.

## Remaining launch gates and hygiene

- Static provider/domain have not been selected or deployed. Reproduce verified
  clean URLs, 308/404 and caching behavior on the eventual host before launch.
- Local favicon/social SVGs are provisional; final public asset approval remains.
- Final Paper-board visual sign-off and approved visual baselines remain a human
  review gate; no automatic golden files are asserted to be approved.
- No analytics, hosted search, third-party runtime fonts, backend integration,
  paid model calls, installer execution, commit, staging, push or deployment.
- Final diff scan found no credential-pattern matches and no generated bundles,
  Storybook output or generated source artifacts among intentional new/changed
  files. CI YAML parses and every docs command is connected to the required gate.
- Scaffold-started test servers terminate after tests. Only this worktree was
  edited by the scaffold; original checkout dependencies were not linked.
