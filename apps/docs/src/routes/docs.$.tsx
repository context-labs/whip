import { createFileRoute, notFound, redirect } from '@tanstack/react-router'
import { Suspense } from 'react'
import { docsManifest, getDoc } from '~/features/docs/content/registry'
import { DocsLayout } from '~/features/docs/components/DocsLayout'
import { docsComponents } from '~/features/docs/docs-components'
import { pageHead } from '~/features/docs/content/head'
import { sitePath } from '~/features/docs/content/site-path'
import { docRedirect } from '~/features/docs/content/redirects'

export const Route = createFileRoute('/docs/$')({
  beforeLoad: ({ location }) => {
    const target = docRedirect(location.pathname)
    if (target) throw redirect({ href: sitePath(target) + location.searchStr + location.hash, replace: true, statusCode: 308 })
  },
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
  return <DocsLayout entries={docsManifest} current={meta}
    actions={<Suspense fallback={null}><page.Actions /></Suspense>}
  ><Suspense fallback={<p>Loading article…</p>}><page.Content components={docsComponents} /></Suspense></DocsLayout>
}
