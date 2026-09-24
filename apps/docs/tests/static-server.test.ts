// @vitest-environment node
import { it, expect } from 'vitest'
import { mkdtemp, mkdir, writeFile, rm, symlink } from 'node:fs/promises'
import path from 'node:path'
import os from 'node:os'
import { createStaticServer } from '../scripts/preview.mjs'

it('serves only public files, clean URLs and true 404s without a runtime bundle', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'whip-docs-static-'))
  let server
  try {
    await mkdir(path.join(root, 'docs/nested'), { recursive: true })
    await mkdir(path.join(root, 'assets'))
    await writeFile(path.join(root, 'index.html'), '<h1>Home</h1>')
    await writeFile(path.join(root, '404.html'), '<h1>Page not found</h1>')
    await writeFile(path.join(root, 'docs/nested/index.html'), '<h1>Nested article</h1>')
    await writeFile(path.join(root, 'assets/app.js'), 'console.log("static")')
    await symlink('/etc/passwd', path.join(root, 'outside.txt'))
    server = await createStaticServer(root)
    await new Promise<void>((resolve) => server!.listen(0, '127.0.0.1', resolve))
    const address = server.address() as { port: number }
    const origin = `http://127.0.0.1:${address.port}`
    for (const url of ['/', '/index.html']) {
      for (const method of ['GET', 'HEAD']) {
        const home = await fetch(`${origin}${url}?from=home`, { method, redirect: 'manual' })
        expect(home.status).toBe(308)
        expect(home.headers.get('location')).toBe('/docs/getting-started?from=home')
      }
    }
    const nested = await fetch(`${origin}/docs/nested`)
    expect(nested.status).toBe(200)
    expect(await nested.text()).toContain('Nested article')
    for (const suffix of ['/', '/index.html', '.html']) {
      const result = await fetch(`${origin}/docs/nested${suffix}?query=yes`, { redirect: 'manual' })
      expect(result.status).toBe(308)
      expect(result.headers.get('location')).toBe('/docs/nested?query=yes')
    }
    for (const url of ['/missing', '/docs/missing', '/404', '/outside.txt', '/.git/config', '/%2e%2e%2fetc/passwd']) {
      const result = await fetch(origin + url)
      expect(result.status).toBe(404)
      expect(await result.text()).toContain('Page not found')
    }
    const notFoundRedirect = await fetch(origin + '/404.html', { redirect: 'manual' })
    expect(notFoundRedirect.status).toBe(308)
    expect(notFoundRedirect.headers.get('location')).toBe('/404')
    expect((await fetch(origin + '/404.html')).status).toBe(404)
    expect((await fetch(origin + '/docs/nested', { method: 'HEAD' })).status).toBe(200)
    expect(await (await fetch(origin + '/docs/nested', { method: 'HEAD' })).text()).toBe('')
    expect((await fetch(origin, { method: 'POST' })).status).toBe(405)
    expect((await fetch(origin + '/%E0%A4%A')).status).toBe(400)
    const asset = await fetch(origin + '/assets/app.js')
    expect(asset.headers.get('cache-control')).toContain('immutable')
    expect(asset.headers.get('content-type')).toContain('javascript')
  } finally {
    if (server) await new Promise<void>((resolve) => server!.close(() => resolve()))
    await rm(root, { recursive: true, force: true })
  }
})
