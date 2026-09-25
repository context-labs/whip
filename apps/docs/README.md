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

The default local preview build emits
`noindex,nofollow`, robots `Disallow: /`, and no canonical URL or sitemap. Once
an HTTPS canonical URL is approved, build with it explicitly:

```sh
DOCS_SITE_URL=https://inference.net/whipcode npm run build:docs
```

Configured builds emit canonical/Open Graph URLs and sitemap entries for real
pages (never the root redirect or `/404`), and robots permit indexing. `DOCS_SITE_URL` accepts only an
HTTPS URL with an optional lowercase kebab-case base path, without credentials,
query or fragment. Vite/TanStack own the asset/router base; plain links use
`sitePath`. MDX source remains host-independent and unchanged.
SVG favicon and social card are local provisional assets; no external fetches.

## Cloudflare Workers deployment

The approved URL is **https://inference.net/whipcode** in the Inference.net
Cloudflare account. `wrangler.jsonc` owns only `inference.net/whipcode` and
`inference.net/whipcode/*`; it does not replace `web-mainnet`, change DNS, or
claim `/assets`, `/robots.txt`, or `/sitemap.xml` on the main site. The entry URL
redirects to `/whipcode/docs/quickstart`. Workers.dev and preview URLs are disabled.

```sh
# Authenticate with Wrangler if needed; credentials never enter the build.
npx --yes wrangler@4.140.0 whoami
npm run deploy -w @whip/docs
node apps/docs/scripts/worker-smoke.mjs https://inference.net
```

The deploy command always rebuilds with the approved canonical URL before
uploading. Only `dist/client` and the small `worker.ts` routing handler ship:
no Start server bundle, Storybook, test fixtures, SDK or remote data fetching.
The Worker strips the base path for the asset binding, implements the shared
308 aliases, preserves queries, applies cache headers, and returns real 404s.
The docs sitemap is `/whipcode/sitemap.xml`; root robots/sitemap remain owned by
the main site. Cloudflare exact routes do not match query strings: a request to
`/whipcode?query` first uses the existing main-site slash redirect, then enters
our `/whipcode/*` route; queries are preserved. The root robots policy currently allows this path. Search-engine
submission or inclusion in the main site's sitemap is separate from deployment.

Local production-routing check (after the canonical build above):

```sh
npx --yes wrangler@4.140.0 dev --config apps/docs/wrangler.jsonc --port 3103 --local
# In another terminal:
node apps/docs/scripts/worker-smoke.mjs
```

`worker-smoke.mjs` checks every public page in Chromium with and without
JavaScript, subpath navigation/assets, canonical URLs, mobile layout, hydration,
themes, redirects, queries, HEAD, 405 and 404 statuses. Default root-path browser
regressions still run against a default `npm run build:docs` artifact.

For a later bad release, use `wrangler rollback --config apps/docs/wrangler.jsonc`
with the previous deployment version. To undo the initial launch, remove only the
two `whipcode-docs` zone routes; requests then fall back to the existing
`inference.net/*` route. Do not delete or redeploy `web-mainnet`.

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
