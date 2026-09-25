import { docRedirect } from './src/features/docs/content/redirects'
import { docsManifest } from './src/features/docs/content/manifest.gen'

const base = '/whipcode'
const pages = new Set(docsManifest.map(doc => `/docs/${doc.path}`))

export default {
  async fetch(request: Request, env: { ASSETS: { fetch(request: Request): Promise<Response> } }) {
    const url = new URL(request.url)
    const headers = { 'Cache-Control': 'no-cache', 'X-Content-Type-Options': 'nosniff', 'Referrer-Policy': 'strict-origin-when-cross-origin' }
    if (!['GET', 'HEAD'].includes(request.method)) {
      return new Response('Method not allowed', { status: 405, headers: { ...headers, Allow: 'GET, HEAD' } })
    }
    if (url.pathname !== base && !url.pathname.startsWith(base + '/')) return new Response('Not found', { status: 404, headers })
    const pathname = url.pathname === base ? '/' : url.pathname.slice(base.length)
    const canonical = pathname.replace(/(?:\/index\.html|\.html|\/+)$/, '') || '/'
    const target = docRedirect(canonical)
    if (target || (pages.has(canonical) && canonical !== pathname)) {
      return new Response(null, { status: 308, headers: { ...headers, Location: base + (target ?? canonical) + url.search } })
    }
    // Only the verified static artifact is served; there is no SSR or SPA fallback.
    url.pathname = pages.has(pathname) ? `${pathname}/index.html` : pathname
    const response = await env.ASSETS.fetch(new Request(url, request))
    const notFound = canonical === '/404' || response.status === 404
    const body = notFound ? await env.ASSETS.fetch(new Request(new URL('/404.html', url), request)) : response
    const result = new Response(request.method === 'HEAD' ? null : body.body, { status: notFound ? 404 : response.status, headers: body.headers })
    for (const [key, value] of Object.entries(headers)) result.headers.set(key, value)
    if (!notFound && pathname.startsWith('/assets/') && response.ok) result.headers.set('Cache-Control', 'public,max-age=31536000,immutable')
    return result
  },
}
