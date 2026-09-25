/// <reference types="vite/client" />
declare module '*.mdx' {
  import type { ComponentType } from 'react'
  const MDXContent: ComponentType<{ components?: Record<string, unknown> }>
  export default MDXContent
}
declare const __DOCS_SITE_URL__: string
declare const __DOCS_INDEXABLE__: boolean
