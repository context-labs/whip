// @vitest-environment node
import { expect, it, vi } from 'vitest'
import worker from '../worker'
import { sitePath } from '../src/features/docs/content/site-path'

it('prefixes only local absolute links', () => {
  vi.stubEnv('BASE_URL', '/whipcode/')
  try {
    expect(sitePath('/docs/quickstart#tui')).toBe('/whipcode/docs/quickstart#tui')
    for (const href of ['#tui', 'https://github.com/context-labs/whip', '//example.test/path', 'mailto:help@example.test']) expect(sitePath(href)).toBe(href)
  } finally { vi.unstubAllEnvs() }
})

it('serves static docs with scoped redirects, caching, HEAD and real 404s', async () => {
  const assets = vi.fn(async (request: Request) => {
    const pathname = new URL(request.url).pathname
    const known = ['/docs/quickstart/index.html', '/404.html', '/assets/example.js'].includes(pathname)
    return new Response(known ? pathname : 'missing', { status: known ? 200 : 404 })
  })
  const request = (path: string, method = 'GET') => worker.fetch(new Request(`https://inference.net${path}`, { method }), { ASSETS: { fetch: assets } })
  for (const path of ['/whipcode', '/whipcode/', '/whipcode/docs', '/whipcode/docs/quickstart/index.html', '/whipcode/docs/quickstart.html', '/whipcode/docs/quickstart/']) {
    const response = await request(path + '?source=test')
    expect(response.status).toBe(308)
    expect(response.headers.get('Location')).toBe('/whipcode/docs/quickstart?source=test')
  }
  expect(assets).not.toHaveBeenCalled()
  const page = await request('/whipcode/docs/quickstart')
  expect(page.status).toBe(200)
  expect(await page.text()).toBe('/docs/quickstart/index.html')
  expect(page.headers.get('Cache-Control')).toBe('no-cache')
  const asset = await request('/whipcode/assets/example.js')
  expect(asset.headers.get('Cache-Control')).toContain('immutable')
  expect(await (await request('/whipcode/docs/quickstart', 'HEAD')).text()).toBe('')
  for (const path of ['/whipcode/missing', '/whipcode/docs/troubleshooting', '/whipcode/404', '/whipcode/404.html', '/whipcode/404/index.html']) {
    const response = await request(path)
    expect(response.status).toBe(404)
    expect(await response.text()).toBe('/404.html')
  }
  for (const path of ['/', '/whipcode-other', '/whipcode-other/docs/quickstart']) expect((await request(path)).status).toBe(404)
  expect((await request('/whipcode/docs/quickstart', 'POST')).status).toBe(405)
})
