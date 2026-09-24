import { createFileRoute, notFound } from '@tanstack/react-router'
import { lazy, Suspense } from 'react'
import { docsManifest, getDoc } from '~/features/docs/content/registry'
import { DocsLayout } from '~/features/docs/components/DocsLayout'
import { docsComponents } from '~/features/docs/docs-components'
import { pageHead } from '~/features/docs/content/head'

// Keep the source used by page actions in a page-local chunk, not the global manifest.
const GettingStartedActions = lazy(async () => {
  const [{ DocPageActions }, { default: source }] = await Promise.all([
    import('~/features/docs/components/DocPageActions'),
    import('~/content/docs/getting-started/index.mdx?raw'),
  ])
  return { default: () => <DocPageActions source={source} filename="getting-started.mdx" /> }
})

export const Route = createFileRoute('/docs/$')({
  loader: async ({ params }) => {
    const page = getDoc(params._splat ?? '')
    if (!page) throw notFound()
    await page.load()
    return page.meta
  },
  head: ({ loaderData }) => loaderData ? pageHead(loaderData.title, loaderData.description, `/docs/${loaderData.path}`) : pageHead('Page not found', 'This documentation page could not be found.', '/404'),
  component: DocArticle,
})
function DocArticle() {
  const meta = Route.useLoaderData()
  const page = getDoc(meta.path)!
  const gettingStarted = meta.path === 'getting-started'
  return <DocsLayout entries={docsManifest} current={meta}
    actions={gettingStarted ? <Suspense fallback={null}><GettingStartedActions /></Suspense> : undefined}
    className={gettingStarted ? 'getting-started-page' : undefined}
    tocHeadings={gettingStarted ? meta.headings.filter(heading => heading.level === 2) : undefined}
    nextPage={gettingStarted ? getDoc('using-whipcode/cli')?.meta : undefined}
  ><Suspense fallback={<p>Loading article…</p>}><page.Content components={docsComponents} /></Suspense></DocsLayout>
}
