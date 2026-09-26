import assert from 'node:assert/strict'
import { chromium } from '@playwright/test'
import { loadDocuments } from './content.mjs'
import { docsSite } from './deployment.mjs'
import { robotsText } from './static-policy.mjs'

const origin = process.argv[2] || 'http://127.0.0.1:3103'
const base = '/whipcode'
const site = docsSite()
assert(site.origin, 'Set DOCS_ENVIRONMENT=production or preview')
const docs = await loadDocuments()
for (const route of [base, base + '/', base + '/docs', base + '/docs/quickstart/', base + '/docs/quickstart.html', base + '/docs/quickstart/index.html']) {
  const response = await fetch(origin + route, { redirect: 'manual' })
  assert.equal(response.status, 308, route)
  assert.equal(response.headers.get('location'), base + '/docs/quickstart', route)
}
const entryQuery = await fetch(origin + base + '?ref=smoke')
assert.equal(new URL(entryQuery.url).pathname, base + '/docs/quickstart')
assert.equal(new URL(entryQuery.url).search, '?ref=smoke')
const query = await fetch(origin + base + '/docs/quickstart/?ref=smoke', { redirect: 'manual' })
assert.equal(query.headers.get('location'), base + '/docs/quickstart?ref=smoke')
for (const route of ['/missing', '/docs/troubleshooting', '/404', '/404.html', '/404/index.html', '/dist/server/server.js', '/src/router.tsx']) {
  assert.equal((await fetch(origin + base + route)).status, 404, route)
}
assert.equal((await fetch(origin + base + '/docs/quickstart', { method: 'POST' })).status, 405)
const head = await fetch(origin + base + '/docs/quickstart', { method: 'HEAD' })
assert.equal(head.status, 200)
assert.equal(await head.text(), '')
assert.equal(head.headers.get('cache-control'), 'no-cache')
assert.equal(head.headers.get('x-robots-tag'), site.indexable ? null : 'noindex, nofollow')
assert.equal(await (await fetch(origin + base + '/robots.txt')).text(), robotsText(site.origin, site.indexable))
const sitemapResponse = await fetch(origin + base + '/sitemap.xml')
if (site.indexable) {
  assert.equal(sitemapResponse.status, 200)
  const sitemap = await sitemapResponse.text()
  for (const doc of docs) assert(sitemap.includes(`${site.origin}/docs/${doc.path}`))
} else assert.equal(sitemapResponse.status, 404)

const browser = await chromium.launch()
try {
  for (const javaScriptEnabled of [true, false]) {
    const context = await browser.newContext({ javaScriptEnabled, viewport: { width: 390, height: 900 } })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    page.on('requestfailed', request => {
      // Chromium blocks module preloads via CSP when JavaScript is disabled.
      if (!javaScriptEnabled && request.failure()?.errorText === 'csp') return
      errors.push(request.url() + ': ' + request.failure()?.errorText)
    })
    page.on('response', response => { if (response.status() >= 400) errors.push(response.status() + ': ' + response.url()) })
    for (const doc of docs) {
      await page.goto(`${origin}${base}/docs/${doc.path}`, { waitUntil: 'networkidle' })
      assert.equal(await page.locator('h1').textContent(), doc.title)
      assert.equal(await page.locator('meta[name="robots"]').getAttribute('content'), site.indexable ? 'index,follow' : 'noindex,nofollow')
      assert.equal(await page.locator('link[rel="canonical"]').getAttribute('href'), `${site.origin}/docs/${doc.path}`)
      for (const href of await page.locator('a[href^="/"]').evaluateAll(nodes => nodes.map(node => node.getAttribute('href')))) assert(href.startsWith(base + '/'), href)
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), doc.path + ': overflow')
    }
    await page.goto(origin + base)
    await page.getByRole('link', { name: 'Download', exact: true }).click()
    assert.equal(new URL(page.url()).pathname, base + '/docs/download')
    if (javaScriptEnabled) {
      await page.getByRole('button', { name: 'Copy code' }).first().waitFor()
      await page.getByRole('button', { name: /Colour theme:/ }).click()
      await page.getByRole('menuitemradio', { name: 'Light' }).click()
      assert.equal(await page.locator('html').getAttribute('data-theme'), 'light')
      await page.keyboard.press('Escape')
    }
    // Mobile navigation uses adjacent-page links; the retained TOC is intentionally hidden.
    await page.getByRole('navigation', { name: 'Adjacent pages' }).getByRole('link', { name: 'Previous Quickstart', exact: true }).click()
    assert.equal(new URL(page.url()).pathname, base + '/docs/quickstart')
    assert.deepEqual(errors, [])
    const asset = await page.locator('script[src]').first().getAttribute('src')
    const response = await fetch(origin + asset)
    assert.equal(response.status, 200)
    assert(response.headers.get('cache-control').includes('immutable'))
    await context.close()
  }
} finally { await browser.close() }
console.log(`Worker smoke passed: ${docs.length} pages, JS/no-JS, links, themes, mobile navigation, assets, redirects and status codes at ${origin}${base}`)
