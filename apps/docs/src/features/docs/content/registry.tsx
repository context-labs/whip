import { lazy, type ComponentType } from 'react'
import { docsManifest } from './manifest.gen'
import type { DocMeta } from './types'

type MDXModule = { default: ComponentType<{ components?: Record<string, unknown> }> }
const modules = import.meta.glob<MDXModule>('/src/content/docs/**/index.mdx')
const pages = new Map(docsManifest.map((meta) => {
  const key = `/src/content/docs/${meta.path}/index.mdx`
  const importer = modules[key]
  if (!importer) throw new Error(`Missing compiled MDX module: ${key}`)
  let promise: Promise<MDXModule> | undefined
  const load = () => promise ??= importer()
  return [meta.path, { meta: meta as DocMeta, load, Content: lazy(load) }]
}))
export { docsManifest }
export function getDoc(path: string) { return pages.get(path) }
