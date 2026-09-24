import http from 'node:http'
import { readFile, realpath, stat } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const publicRoot = fileURLToPath(new URL('../dist/client/', import.meta.url))
const mime = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon', '.txt': 'text/plain; charset=utf-8', '.xml': 'application/xml; charset=utf-8', '.json': 'application/json' }

// Serve only real static files. An unknown URL is never rewritten to the SPA.
export async function createStaticServer(root = publicRoot) {
  const rootPath = await realpath(root)
  const notFound = await readFile(path.join(rootPath, '404.html'))
  return http.createServer(async (request, response) => {
    const send = (status, body, type = mime['.html']) => {
      response.writeHead(status, { 'Content-Type': type, 'X-Content-Type-Options': 'nosniff', 'Referrer-Policy': 'strict-origin-when-cross-origin', 'Cache-Control': response.getHeader('Cache-Control') ?? 'no-cache' })
      response.end(request.method === 'HEAD' ? undefined : body)
    }
    if (!['GET', 'HEAD'].includes(request.method)) { response.setHeader('Allow', 'GET, HEAD'); send(405, 'Method not allowed', mime['.txt']); return }
    try {
      const url = new URL(request.url, 'http://localhost')
      const pathname = decodeURIComponent(url.pathname)
      if (pathname.includes(String.fromCharCode(92)) || pathname.includes(String.fromCharCode(0)) || pathname.split('/').some((part) => part.startsWith('.'))) { send(404, notFound); return }
      let canonical = pathname
      if (canonical.endsWith('/index.html')) canonical = canonical.slice(0, -11) || '/'
      else if (canonical.endsWith('.html')) canonical = canonical.slice(0, -5) || '/'
      if (canonical !== '/') canonical = canonical.replace(/[/]+$/, '')
      if (canonical === '/') {
        response.writeHead(308, { Location: '/docs/getting-started' + url.search })
        response.end()
        return
      }
      if (canonical !== pathname) {
        // Do not issue an open redirect or redirect a missing document.
        const target = path.join(rootPath, canonical === '/' ? 'index.html' : `${canonical}.html`)
        const nested = path.join(rootPath, canonical, 'index.html')
        const exists = await stat(target).catch(() => stat(nested).catch(() => null))
        if (exists?.isFile() && !canonical.startsWith('//')) { response.writeHead(308, { Location: canonical + url.search }); response.end(); return }
      }
      const relative = pathname === '/' ? 'index.html' : pathname.slice(1)
      const candidates = path.extname(relative) ? [relative] : [`${relative}.html`, path.join(relative, 'index.html')]
      for (const candidate of candidates) {
        const filename = await realpath(path.join(rootPath, candidate)).catch(() => null)
        if (!filename || !filename.startsWith(rootPath + path.sep) || !(await stat(filename)).isFile()) continue
        const status = pathname === '/404' || pathname === '/404.html' ? 404 : 200
        response.setHeader('Cache-Control', filename.includes(`${path.sep}assets${path.sep}`) ? 'public,max-age=31536000,immutable' : 'no-cache')
        send(status, await readFile(filename), mime[path.extname(filename)] ?? 'application/octet-stream')
        return
      }
      send(404, notFound)
    } catch (error) {
      if (error instanceof URIError) send(400, 'Bad request', mime['.txt'])
      else { console.error(error); send(500, 'Static server error', mime['.txt']) }
    }
  })
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const argument = process.argv.indexOf('--port')
  const port = Number(argument === -1 ? process.env.PORT ?? 3101 : process.argv[argument + 1])
  const server = await createStaticServer()
  server.listen(port, '127.0.0.1', () => console.log(`Static docs: http://127.0.0.1:${port}`))
  for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, () => server.close(() => process.exit(0)))
}
