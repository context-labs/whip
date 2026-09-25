import type { DocHeading } from '../../src/features/docs/content/types'
import { test, expect } from '@playwright/test'
import { appRoot, loadDocuments } from '../../scripts/content.mjs'
import { docRedirects } from '../../src/features/docs/content/redirects'
import { docSections } from '../../src/features/docs/content/sections'
const docs = await loadDocuments()

test('all entry and legacy URLs redirect, even without JavaScript or host rules', async ({ page, request, browser }) => {
  for (const [from, to] of Object.entries(docRedirects)) {
    for (const suffix of from === '/' ? ['', 'index.html'] : ['', '/', '/index.html', '.html']) {
      const response = await request.get(`${from}${suffix}?from=old`, { maxRedirects: 0 })
      expect(response.status()).toBe(308)
      expect(response.headers().location).toBe(`${to}?from=old`)
    }
    await page.goto(from)
    await expect(page).toHaveURL(to)
    const context = await browser.newContext({ javaScriptEnabled: false })
    const staticPage = await context.newPage()
    const url = `http://127.0.0.1:3101${from}`
    await staticPage.route(url, route => route.fulfill({ contentType: 'text/html', path: `${appRoot}/dist/client/${from === '/' ? 'index.html' : from.slice(1) + '/index.html'}` }))
    await staticPage.goto(url)
    await expect(staticPage).toHaveURL(`http://127.0.0.1:3101${to}`)
    await expect(staticPage.getByRole('heading', { level: 1 })).toBeVisible()
    await context.close()
  }
})

test('all 21 pages render their headings and ordered sidebar without errors', async ({ page }) => {
  const errors: string[] = []
  const requests: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('request', request => requests.push(request.url()))
  for (const doc of docs) {
    const response = await page.goto(`/docs/${doc.path}`)
    expect(response?.status()).toBe(200)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(doc.title)
    await expect(page).toHaveTitle(`${doc.title} — whipcode`)
    await expect(page.locator('article h2, article h3')).toHaveText(doc.headings.map((heading: DocHeading) => heading.text))
    if (!['quickstart', 'download', 'typescript-sdk'].includes(doc.path)) await expect(page.locator('article > :not(h2)')).toHaveCount(0)
    await expect(page.locator('.docs-toc a')).toHaveText(doc.headings.map((heading: DocHeading) => heading.text))
    await expect(page.locator('.docs-sidebar .sidebar-label')).toHaveText(docSections.map(section => section.label))
    await expect(page.locator('.docs-sidebar .sidebar-item')).toHaveText(docs.map(entry => entry.title))
    await expect(page.locator('.docs-sidebar [aria-current="page"]')).toHaveText(doc.title)
    await page.reload()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(doc.title)
  }
  expect(errors).toEqual([])
  expect(requests.every(url => new URL(url).origin === 'http://127.0.0.1:3101')).toBe(true)
  expect(requests.some(url => /api|_server|localhost:8080/.test(new URL(url).pathname))).toBe(false)
})

test('pagination, history, heading anchors and unknown routes work', async ({ page }) => {
  await page.goto('/docs/quickstart')
  await page.locator('.doc-pagination').getByRole('link').filter({ hasText: 'Download' }).click()
  await expect(page).toHaveURL('/docs/download')
  await page.goBack()
  await expect(page).toHaveURL('/docs/quickstart')
  await page.goForward()
  await expect(page).toHaveURL('/docs/download')
  await page.locator('.docs-toc a').first().click()
  await expect(page).toHaveURL('/docs/download#desktop')
  for (const url of ['/not-a-page', '/docs/not-a-page', '/404']) {
    const response = await page.goto(url)
    expect(response?.status()).toBe(404)
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
  }
})

for (const width of [320, 390, 768, 1440, 1920]) test(`responsive ${width}px layout has aligned containers and no overflow`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  for (const url of ['/','/docs','/docs/download','/docs/agents-subagents','/docs/typescript-sdk']) {
    await page.goto(url)
    await expect(page.locator('main')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    const header = (await page.locator('.site-header-inner').boundingBox())!
    const body = (await page.locator('.docs-layout').boundingBox())!
    const footer = (await page.locator('.site-footer').boundingBox())!
    for (const box of [body, footer]) { expect(box.x).toBeCloseTo(header.x,1); expect(box.width).toBeCloseTo(header.width,1) }
    expect(header.width).toBeLessThanOrEqual(1280)
    if (width >= 1440) expect(header.width).toBe(1280)
  }
})

test('all outline pages remain readable without JavaScript', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 390, height: 844 } })
  const page = await context.newPage()
  for (const doc of docs) {
    await page.goto(`http://127.0.0.1:3101/docs/${doc.path}`)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(doc.title)
    await expect(page.locator('article h2, article h3')).toHaveText(doc.headings.map((heading: DocHeading) => heading.text))
  }
  await context.close()
})
