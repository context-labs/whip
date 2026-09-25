# Implementation coordination

User approved end-to-end implementation of all three phases. Work only in
`/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-docs` (`feat/docs-site`).
Do not edit original whip checkout or reference fast-web. No commits/push/deploy.

## Ownership

- Scaffold agent: apps/docs package/config, scripts, src/router.tsx, src/routes/**,
  src/features/docs/content/**, generated metadata + type declarations, static
  build/preview and tests of pipeline/routing. Root package scripts/lockfile and
  npm install ONLY owned by scaffold. Add dependencies requested by other lanes.
- Components agent: apps/docs/src/components/**, src/styles/**,
  src/features/docs/components/**, src/features/docs/docs-components.tsx,
  .storybook/** and stories; component tests. May reconcile docs/brand-guide.md.
  No package/lockfile changes or install. Request dependencies via messages.
- Content agent: apps/docs/src/content/docs/** MDX and public launch copy only;
  produce source provenance and landing copy in launch-content.md. Do not edit
  routes or shared code. Fact-check against current main worktree docs/source.
- Root: integration, CI, canonical docs/feature map, review and browser validation.

## Contracts (coordinate changes by message)

MDX sources: src/content/docs/<slug>/index.mdx (including nested directories).
Frontmatter: title, description, section, order. Slug derived from file path.
Sections: start (label Get started), usage (Using whipcode), reference (Reference).
No H1 in MDX; article layout renders title + description. H2/H3 provide TOC.

Initial launch pages:
- getting-started (start, 1): choose Desktop or CLI and run first session
- installation (start, 2): supported release/install instructions
- using-whipcode/desktop (usage, 1)
- using-whipcode/cli (usage, 2)
- configuration (reference, 1)
- tools-and-permissions (reference, 2)
Keep it focused and factual; do not claim unreleased functionality or publish
internal docs wholesale. Public links use /docs/<slug>, no trailing slash.
No invented Discord/domain URLs; GitHub context-labs/whip is verified.

Shared view data (scaffold exports types from features/docs/content/types.ts):
DocHeading = { id: string; text: string; level: 2 | 3 }
DocMeta = { path: string; title: string; description: string; section: string;
            order: number; headings: DocHeading[] }
path is relative slug, e.g. installation (NOT /docs/installation).

Components lane public APIs (re-export from relevant index.ts):
- components/navigation/SiteHeader.tsx: SiteHeader() global nav, brand, mobile
  site nav/theme (docs mobile nav belongs DocsLayout). Use documented public
  URLs only. No dead links or unimplemented search.
- features/docs/components/DocsLayout.tsx:
  DocsLayout({entries: readonly DocMeta[], current?: DocMeta, children})
  provides sidebar/mobile docs nav, main article, optional TOC/pagination.
  For current, renders H1 and lead description before children. For docs index,
  route children include heading. Only one main landmark per page.
- features/docs/docs-components.tsx: export docsComponents mapping for MDX
  (pre/code/Callout/CodeTabs/CodeTab etc). Support <Callout title="..."
  type="info|tip|warning|alert">Markdown</Callout>, and
  <CodeTabs><CodeTab label="CLI"> fenced markdown </CodeTab>...</CodeTabs>.
  Prefer Markdown fences, no raw code-as-prop authoring.
- components/ui/index.ts: useful primitives; scaffold may use ordinary HTML
  and these public CSS classes: site-main, prose, page-lead, doc-grid, doc-card,
  landing, landing-hero, landing-actions, button, button-secondary, inline-code.

Code pipeline agreement:
- Rehype adapter highlights fenced pre > code with Refractor at build time.
- Preserve original code source as data-raw on pre; language as data-language.
- Generated inner span className arrays include token + Prism categories.
- React MDX pre mapping receives data-raw/data-language + children; wrap children
  (already code element) in semantic pre without flattening token nodes.
- Normal inline code receives no pre wrapper and is neutral.
- Prism CSS scoped to pre .token. Parent tokens do not override nested roles.
- All docs navigation through TanStack or actual semantic links, never onclick
  only. No backend/analytics/remote fonts. Static assets only.
- User theme preference in localStorage, system preference fallback, first-paint
  theme script integrated in root document with no hydration errors. Components
  lane owns theme helper and tells scaffold its export path.

Root layout renders SiteHeader + route outlet + minimal footer as appropriate.
Use static pre-render for /, /docs, all metadata paths and /404. Build only public
output required at runtime. No Cloudflare plugin/SSR deployment, no SPA fallback.

Paper board is visual authority except user mandates multicolour syntax.
Read README.md + design-research.md + brand guide. No formal intermediate user
approval waits now: all three phases authorized; internal validation gates remain.
