import { createFileRoute, Link } from '@tanstack/react-router'
import { DocsLayout } from '~/features/docs/components/DocsLayout'
import { docsManifest } from '~/features/docs/content/manifest.gen'
import { docSections } from '~/features/docs/content/types'
import { pageHead } from '~/features/docs/content/head'

export const Route = createFileRoute('/docs/')({
  head: () => pageHead('Documentation', 'Install whipcode, start a session, and learn how to direct your coding agents.', '/docs'),
  component: DocsIndex,
})
function DocsIndex() {
  return <DocsLayout entries={docsManifest}>
    <h1>Documentation</h1>
    <p className="page-lead">A practical guide to working with whipcode. Start with installation, then choose the Desktop app or the command line.</p>
    {docSections.map((section) => <section key={section.id}>
      <h2>{section.label}</h2>
      <div className="doc-grid">{docsManifest.filter((doc) => doc.section === section.id).map((doc) => <Link className="doc-card" key={doc.path} to="/docs/$" params={{ _splat: doc.path }}><h3>{doc.title}</h3><p>{doc.description}</p></Link>)}</div>
    </section>)}
  </DocsLayout>
}
