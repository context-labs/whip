# whipcode documentation site

Private `@whip/docs` workspace: React, TanStack Start/Router and repository-authored
MDX. Node 24 and npm are required. No `.env`, daemon, SDK, credentials, remote font,
analytics, API or runtime server is needed.

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
`--with-deps`). Browser tests require a completed `build:docs`. Storybook is a
development-only board/component reference and is not part of the public site.

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

Sections are `start`, `usage`, `reference`; order must be a positive integer,
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
Getting started uses the Paper article layout with section-only TOC, numbered
steps and page copy/download controls. Its original MDX is a page-local lazy
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
intermediate; the preview does not import it. Build enumerates `/docs`, every
manifest path and `/404`, adds a minimal root redirect document, and verifies every HTML
page/title/heading, local assets, token output and absence of runtime grammars.
There is no SPA fallback, backend request or dynamic route handler at runtime.

The preview implements the host policy to reproduce on the chosen provider:

- `/` and `/index.html` redirect with **308** to `/docs/getting-started`, preserving
  the query. The root route also redirects in development/client navigation.
  `index.html` contains a no-JavaScript meta-refresh fallback and a direct link
  for static hosts without redirect rules; no landing-page content is shipped.
- `/docs/installation` and nested clean URLs serve their actual HTML with 200.
- Known trailing-slash, `.html` and `/index.html` aliases redirect with **308**
  to the clean URL, preserving the query. Unknown aliases still return 404.
- Missing pages and `/404` serve `404.html` with **404**, not home HTML with 200.
- GET and HEAD only; non-public files/symlinks and traversal are not served.
- Hashed `/assets/*` files may be cached immutably for one year; HTML is revalidated.
- Do not deploy Storybook, test fixtures, source or server output.

No domain or hosting provider is assumed. The default preview build emits
`noindex,nofollow`, robots `Disallow: /`, and no canonical URL or sitemap. Once
an HTTPS canonical origin is approved, build with it explicitly:

```sh
DOCS_SITE_URL=https://docs.example.test npm run build:docs
```

The example is intentionally a reserved test domain, not a publication claim.
Configured builds emit canonical/Open Graph URLs and sitemap entries for real
pages (never the root redirect or `/404`), and robots permit indexing. `DOCS_SITE_URL` accepts only an
HTTPS origin without path, credentials, query or fragment. The chosen static
host must independently verify redirects/status/cache headers before launch.
SVG favicon and social card are local provisional assets; no external fetches.

## Boundaries and validation

See [the canonical frontend guide](../../docs/frontend.md) and
[brand guide](../../docs/brand-guide.md). This app owns ordinary CSS and Base UI
components; it intentionally does not import `@whip/ui`, SDK or product state.
System fonts only. Shared public types live in `features/docs/content/types.ts`.

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
