import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { fileURLToPath } from 'node:url'
import { generateManifest } from './scripts/content.mjs'
import { contentPlugin } from './scripts/content-plugin.mjs'
import { docsMdx } from './scripts/mdx-plugins.mjs'
import { siteUrl } from './scripts/site-url.mjs'

export default defineConfig(async () => {
  const manifest = await generateManifest()
  const origin = siteUrl(process.env.DOCS_SITE_URL)
  return {
    base: origin ? new URL(origin).pathname.replace(/\/$/, '') + '/' : '/',
    resolve: { alias: { '~': fileURLToPath(new URL('./src', import.meta.url)) } },
    define: { __DOCS_SITE_URL__: JSON.stringify(origin) },
    server: { host: '127.0.0.1', port: 3100, strictPort: true },
    plugins: [
      contentPlugin(), docsMdx(),
      tanstackStart({
        pages: [...manifest.map((doc) => `/docs/${doc.path}`), '/404'].map((path) => ({ path, sitemap: { exclude: path === '/404' } })),
        prerender: { enabled: true, failOnError: true, autoStaticPathsDiscovery: false, crawlLinks: false, retryCount: 0 },
        sitemap: { enabled: Boolean(origin), host: origin },
      }),
      react(),
    ],
  }
})
