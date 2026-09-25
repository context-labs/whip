# whipcode documentation site

Private `@whip/docs` workspace: React, TanStack Start/Router and repository-authored
MDX. Node 24 and npm are required. No `.env`, daemon, SDK, credentials, remote font,
analytics, API or React runtime server is needed. Production uses a small
Cloudflare Worker to route requests to the prebuilt static asset binding.

## Commands (from the repository root)

```sh
npm ci
npm run dev:docs                  # http://127.0.0.1:3100, strict port
npm run check:docs                # regenerate metadata/routes, strict TypeScript
npm run test:docs                 # compiler, static-policy and component tests
npm run test:docs:dev             # live add/edit/delete integration, port 3102
npm run build:docs                # prerender + verify public artifact
npm run preview:docs              # http://127.0.0.1:3101, static files only
npm run test:docs:browser         # Playwright starts/stops its own static preview
npm run storybook:docs            # http://localhost:6007
npm run build:storybook:docs
```

Install Chromium once with `npx playwright install chromium` (CI uses
`--with-deps`). Browser tests require a completed `build:docs`, build Storybook
automatically, and start static servers on ports 3101 and 6008. Content-independent
component regression checks use Storybook fixtures rather than public article
copy. Storybook is development-only and is not part of the public site.

## Content authoring

Create `src/content/docs/<slug>/index.mdx`; nested slugs are supported. Each
segment is lowercase kebab-case. The file path owns the URL, `/docs/<slug>`.
Use this frontmatter:

```yaml
title: A useful title
description: A concise summary for the page and search metadata.
section: start
order: 1
```

Sections are `start`, `usage`, `configuration`, `agents`, `developers` (labels and
order live in `src/features/docs/content/sections.ts`); page order must be a positive integer,
unique within its section. Metadata is strict. Do not put H1 in an article: the
layout renders the title. H2/H3 form the TOC. Heading text and IDs come from the
same Markdown AST transform (including inline formatting, Unicode, duplicate
headings). Link with root-relative clean URLs, e.g. `/docs/installation`, and
real heading fragments. Bad metadata, duplicate order/path, missing MDX modules,
broken internal links and fragments fail validation with source context.

MDX is **trusted executable source**, never untrusted user or remote content.
Keep examples in ordinary fenced Markdown. Available components:

````mdx
<Callout type="tip" title="A useful detail">
Markdown content goes here.
</Callout>

<CodeTabs>
<CodeTab label="CLI">

```bash
whipcode --help
```

</CodeTab>
</CodeTabs>
````

Actual articles need no imports. `docsComponents` supplies Callout, CodeTabs/CodeTab, table and pre/code.
Only article metadata is global; compiled articles are separate lazy chunks.
Optional `navTitle` keeps a short sidebar label without changing the page H1/title.
The site has 21 pages in five sidebar groups. Quickstart, Download and TypeScript
SDK have article content; the other 18 remain heading-only outlines. Quickstart and Download are first. The Desktop DMG link on Download is pinned to
a verified public release; update its version, link and browser assertion together
when changing the recommended build. There is no runtime release fetching.
Every page retains copy/download controls. Its original MDX is a page-local lazy
import; `docsMdx()` bypasses MDX compilation for Vite `?raw` requests. Copy and
download preserve the complete authored source, including frontmatter and MDX
components. No raw-page API or global article-body index is introduced.
The Vite watcher regenerates metadata on add/change/delete; generation writes
only if bytes change. Generated route/manifest files are ignored and regenerated
by dev/build/check. No manual generation is required after editing content.

### Syntax and source fidelity

Refractor tokenizes fences at **build time**, never in browser components.
Supported labels/aliases: bash/sh/shell/curl, javascript/js, typescript/ts,
python/py, go/golang, json, yaml/yml; starlark/bzl use Python as an explicitly
approximate grammar. text/txt/plaintext and unlabelled fences stay neutral.
Unknown labels emit an author warning and safely render plain text; do not use
an inaccurate alias for TSX, JSX or JSONC. Add actual grammars when needed.

The rehype adapter passes original Markdown code text (including its final
newline) as `data-raw` to the pre component. Copy uses exactly the selected
snippet's source, not token markup. Light/dark changes only CSS variables. With
JavaScript disabled all tabbed snippets remain visible and labelled.

## Static artifact and host contract

**Deploy only `apps/docs/dist/client/`.** TanStack Start's `dist/server` is a build
intermediate; the preview does not import it. Build enumerates every manifest
path and `/404`, adds static entry/legacy redirect documents, and verifies every HTML
page/title/heading, local assets, token output and absence of runtime grammars.
There is no SPA fallback, backend request or dynamic page rendering at runtime.

The preview implements the host policy to reproduce on the chosen provider:

- `/`, `/docs`, `/docs/introduction` and `/docs/getting-started` redirect with **308** to
  `/docs/quickstart`. Old installation, CLI and tools/permissions paths redirect
  to Download, TUI and Permissions. Aliases are defined once in
  `src/features/docs/content/redirects.ts`; dev routes and preview share them.
  Queries are preserved by HTTP redirects. Each alias has a static meta-refresh
  document for no-JavaScript hosts without redirect rules.
- `/docs/download` and nested clean URLs serve their actual HTML with 200.
- Known trailing-slash, `.html` and `/index.html` aliases redirect with **308**
  to the clean URL, preserving the query. Unknown aliases still return 404.
- Missing pages and `/404` serve `404.html` with **404**, not home HTML with 200.
- GET and HEAD only; non-public files/symlinks and traversal are not served.
- Hashed `/assets/*` files may be cached immutably for one year; HTML is revalidated.
- Do not deploy Storybook, test fixtures, source or server output.

The default local build emits `noindex,nofollow`, robots `Disallow: /`, and no
canonical URL or sitemap. Publication is explicit and branch-owned:

| Branch | `DOCS_ENVIRONMENT` | URL | Worker | GitHub environment |
| --- | --- | --- | --- | --- |
| `main` | `production` | https://inference.net/whipcode | `whipcode-docs` | `docs-production` |
| `development` | `preview` | https://inference.cool/whipcode | `whipcode-docs-preview` | `docs-preview` |

`DOCS_ENVIRONMENT` fixes the canonical URL and indexing policy together.
Production emits indexable article metadata and `/whipcode/sitemap.xml`.
Preview uses its own canonical URLs but emits `noindex,nofollow`, an
`X-Robots-Tag: noindex, nofollow` response header, `Disallow: /`, and no sitemap.
Local `DOCS_SITE_URL` overrides still support an HTTPS lowercase kebab-case base
path, but never enable indexing alone; conflicting environment/URL inputs fail.
Vite/TanStack own the asset/router base; plain links use `sitePath`. MDX source
and styling remain host-independent. SVG favicon and social card are local.

## Automatic Cloudflare Workers deployment

`.github/workflows/docs-deploy.yml` runs on direct pushes to `main` and
`development`, separately from reusable release CI. PRs and called app-release
workflows cannot deploy. A manual dispatch is available on those two branches
for recovery; there are no user-supplied target/URL/Worker inputs.

Each run checks out its immutable source SHA, validates docs and the root
noindex browser build, then builds **the target-specific** artifact. It bundles
`worker.ts` and copies only `dist/client` to `dist/deploy/public`, with a source
receipt. A local Worker smoke tests this exact package before artifact upload.
No Start server bundle, Storybook, fixtures, SDK or runtime rendering ships.

Only the deploy job enters the branch-restricted GitHub environment. Install,
build and test steps have no Cloudflare credentials. The final step uses its
environment's separate `CLOUDFLARE_API_TOKEN`, restricted to the corresponding
Worker's **Editor** role, to `versions upload --no-bundle` and activate the exact
returned version ID at 100%. Tokens need no zone routes, DNS, R2 or other Worker
access. Wrangler is pinned to 4.140.0. CI configuration deliberately has **no
routes**: version upload/activation preserves the operator-managed connections.
A credential-free final job checks all live pages with and without JavaScript.

Deployments serialize per branch and never cancel an in-flight activation.
Immediately before upload, CI checks that its source is still the branch head;
superseded reruns skip deployment. A push arriving during an activation queues a
new deployment; it cannot be overwritten later by an older run. The deployment
summary records URL, Worker, source SHA and version ID. If upload/activation
fails, inspect the Worker versions and run output before retrying—an upload may
already have completed. Rerun the current branch workflow, not an old SHA.

### Route ownership and one-time operator setup

`wrangler.jsonc` records only the exact `/whipcode` and `/whipcode/*` path routes
for each domain. The root websites (`web-mainnet` / `web-dev`), DNS, `/assets`,
`/robots.txt`, `/sitemap.xml` and sibling paths such as `/whipcode-other` remain
untouched. Workers.dev and version preview URLs stay disabled. These are zone
**path routes**, not custom domains. The entry redirects to
`/whipcode/docs/quickstart`. Cloudflare exact routes do not match query strings:
`/whipcode?query` uses the existing website's slash redirect before entering the
subtree; the query survives. Root robots policy remains website-owned
(`inference.cool` already disallows crawling).

An operator bootstraps a missing named Worker and those two routes once, using
a reviewed target build and existing Wrangler OAuth. Then create the
Worker-specific Editor token and save it only in the corresponding GitHub
environment. Neither token can provision the other Worker or alter routes.
Do not replace per-Worker roles with account-wide Scripts Edit permissions.

### Local validation and operator recovery

Run builds/tests without provider credentials and with a disposable `HOME` plus
explicit `WHIPCODE_HOME`. No app/daemon launch is needed. With Wrangler 4.140.0
on `PATH`, choose a target explicitly:

```sh
DOCS_ENVIRONMENT=preview npm run build:docs
DOCS_ENVIRONMENT=preview npm run package:deploy -w @whip/docs
DOCS_ENVIRONMENT=preview node apps/docs/scripts/worker-test.mjs
DOCS_ENVIRONMENT=preview node apps/docs/scripts/worker-smoke.mjs https://inference.cool
# Use production + https://inference.net for the main site.
```

Prefer rerunning **Deploy docs** on the current approved branch. For an operator
version-only deployment, build and test from a clean checkout of that branch;
`source.json` records a SHA but does not attest that a local working tree was
clean. Then deploy the tested package (credentials only for this step):

```sh
DOCS_ENVIRONMENT=preview DOCS_SOURCE_SHA=$(git rev-parse HEAD) npm run deploy -w @whip/docs
```

Rollback uses `wrangler versions deploy <previous-version-id>@100% --yes
--config apps/docs/wrangler.jsonc --env preview` (or `production`) after checking
the intended Worker. Do not use `triggers deploy` for routine updates. Removing
only that Worker's two docs routes restores the root website's fallback; never
delete or redeploy the website Worker. Branch pushes to `development` also
trigger the independently configured automatic app alpha release; docs do not
change that release policy.

## Boundaries and validation

See [the canonical frontend guide](../../docs/frontend.md) and
[brand guide](../../docs/brand-guide.md). This app owns ordinary CSS and Base UI
components; it intentionally does not import `@whip/ui`, SDK or product state.
System fonts only. Shared public types live in `features/docs/content/types.ts`.

`tests/sdk-examples.test.ts` extracts TypeScript fences from the SDK page, typechecks
them against the actual SDK source, and exercises helpers with local doubles.
It does not connect to a real host or run model requests; SDK code stays out of
the docs browser bundle.

`tests/content.test.tsx` exercises metadata/AST IDs, grammar aliases, exact source,
escaping and real MDX nested tabs. `static-server.test.ts` tests host routing and
filesystem isolation; `static-policy.test.ts` tests robots and highlighter leak
sentinels. `tests/browser/*.spec.ts` visits every real page, reload/history,
fragments/404/redirects, no-JS, clipboard/tab keyboard behavior, responsive widths,
local-only requests, hydration errors, theme token changes, system/media and
cross-tab theme sync, mobile dialog focus/links, no-JS mobile navigation and
clipboard rejection recovery. Component tests
and the Board Reference Storybook cover library behavior and visual reference.

Workspace typing note: the MDX compiler currently depends on legacy global-JSX
`@types/mdx`. The wire-only protocol config explicitly excludes unrelated ambient
types; SDK and the client example declare their needed ambient scope instead of
widening globals or weakening `skipLibCheck`. Docs compiler imports and its MDX
module declaration remain inside this app.
