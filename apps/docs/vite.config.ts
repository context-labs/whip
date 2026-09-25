import { defineConfig, type Connect, type Plugin, type ViteDevServer } from 'vite'
import react from '@vitejs/plugin-react'
import stylex from '@stylexjs/unplugin'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { fileURLToPath } from 'node:url'
import { generateManifest } from './scripts/content.mjs'
import { contentPlugin } from './scripts/content-plugin.mjs'
import { docsMdx } from './scripts/mdx-plugins.mjs'
import { docsSite } from './scripts/deployment.mjs'

export default defineConfig(async () => {
  const manifest = await generateManifest()
  const { origin, indexable } = docsSite()
  return {
    base: origin ? new URL(origin).pathname.replace(/\/$/, '') + '/' : '/',
    resolve: { alias: { '~': fileURLToPath(new URL('./src', import.meta.url)) } },
    define: { __DOCS_SITE_URL__: JSON.stringify(origin), __DOCS_INDEXABLE__: JSON.stringify(indexable) },
    server: { host: '127.0.0.1', port: 3100, strictPort: true },
    plugins: [
      contentPlugin(), docsMdx(),
      {
        name: 'docs-stylex-dev-constants',
        apply: 'serve',
        enforce: 'pre',
        configureServer(server: ViteDevServer) {
          const tokens = `/@fs/${fileURLToPath(new URL('./src/tokens.stylex.ts', import.meta.url))}`
          server.middlewares.use((request: Connect.IncomingMessage, _response: unknown, next: Connect.NextFunction) => {
            if (!request.url?.startsWith('/virtual:stylex.css')) return next()
            // StyleX collects partial module graphs during startup. Resolve defineConsts
            // (breakpoint media queries) before Lightning CSS sees unresolved var(...) selectors.
            void server.transformRequest(tokens).then(() => next(), next)
          })
        },
      } satisfies Plugin,
      tanstackStart({
        pages: [...manifest.map((doc) => `/docs/${doc.path}`), '/404'].map((path) => ({ path, sitemap: { exclude: path === '/404' } })),
        prerender: { enabled: true, failOnError: true, autoStaticPathsDiscovery: false, crawlLinks: false, retryCount: 0 },
        sitemap: { enabled: indexable, host: origin },
      }),
      // No useCSSLayers: docs base CSS is unlayered and must not win over component styles.
      // runtimeInjection stays disabled to preserve the static, JavaScript-disabled contract.
      stylex.vite({ runtimeInjection: false, aliases: { '~/*': ['/ROOT/apps/docs/src/*'] }, unstable_moduleResolution: { type: 'commonJS', rootDir: fileURLToPath(new URL('../../', import.meta.url)) } }),
      react(),
    ],
  }
})
