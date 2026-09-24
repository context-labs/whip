import { test, expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { appRoot, loadDocuments } from '../../scripts/content.mjs'
const docs = await loadDocuments()

test('root redirects to getting started, including without JavaScript on a plain static host', async ({ page, request, browser }) => {
  const response = await request.get('/?from=home', { maxRedirects: 0 })
  expect(response.status()).toBe(308)
  expect(response.headers().location).toBe('/docs/getting-started?from=home')
  await page.goto('/')
  await expect(page).toHaveURL('/docs/getting-started')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Getting started')

  const context = await browser.newContext({ javaScriptEnabled: false })
  const staticPage = await context.newPage()
  // Simulate a host with no HTTP redirect rules: it serves the built index verbatim.
  await staticPage.route('http://127.0.0.1:3101/', (route) => route.fulfill({
    contentType: 'text/html', path: `${appRoot}/dist/client/index.html`,
  }))
  await staticPage.goto('http://127.0.0.1:3101/')
  await expect(staticPage).toHaveURL('http://127.0.0.1:3101/docs/getting-started')
  await expect(staticPage.getByRole('heading', { level: 1 })).toHaveText('Getting started')
  const index = await readFile(`${appRoot}/dist/client/index.html`, 'utf8')
  expect(index).not.toContain('A coding agent for work beyond one context window')
  await context.close()
})

test('every prerendered article works directly, reloads, and stays local', async ({ page }) => {
  const errors: string[] = []
  const requests: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('request', (request) => requests.push(request.url()))
  for (const doc of docs) {
    const response = await page.goto(`/docs/${doc.path}`)
    expect(response?.status()).toBe(200)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(doc.title)
    await expect(page).toHaveTitle(`${doc.title} — whipcode`)
    await expect(page.locator('main')).toHaveCount(1)
    await page.reload()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(doc.title)
    for (const heading of doc.headings) await expect(page.locator(`[id="${heading.id}"]`)).toHaveCount(1)
  }
  expect(errors).toEqual([])
  expect(requests.every((url) => new URL(url).origin === 'http://127.0.0.1:3101')).toBe(true)
  expect(requests.some((url) => /api|_server|localhost:8080/.test(new URL(url).pathname))).toBe(false)
})

test('client navigation, history, fragments and real 404 responses', async ({ page, request }) => {
  await page.goto('/docs')
  await page.getByRole('main').getByRole('link').filter({ hasText: 'Getting started' }).click()
  await expect(page).toHaveURL('/docs/getting-started')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(docs.find((doc) => doc.path === 'getting-started')!.title)
  await page.goBack()
  await expect(page).toHaveURL('/docs')
  await page.goForward()
  await expect(page).toHaveURL('/docs/getting-started')
  const first = docs.find((doc) => doc.path === 'getting-started')!.headings[0]!
  await page.locator(`.docs-toc a[href="#${first.id}"]`).click()
  await expect(page).toHaveURL(new RegExp(`#${first.id}$`))
  const redirect = await request.get('/docs/using-whipcode/cli/?test=yes', { maxRedirects: 0 })
  expect(redirect.status()).toBe(308)
  expect(redirect.headers().location).toBe('/docs/using-whipcode/cli?test=yes')
  for (const url of ['/not-a-page', '/docs/not-a-page', '/404']) {
    const response = await page.goto(url)
    expect(response?.status()).toBe(404)
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
  }
})

test('no-JavaScript article and both code tabs are readable', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false })
  const page = await context.newPage()
  await page.goto('http://127.0.0.1:3101/docs/installation')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Installation')
  expect(await page.locator('pre .token').count()).toBeGreaterThan(0)
  expect(await page.locator('.code-tabs-fallback pre').count()).toBe(2)
  for (const pre of await page.locator('.code-tabs-fallback pre').all()) await expect(pre).toBeVisible()
  await expect(page.locator('main a').first()).toHaveAttribute('href', /.+/)
  await context.close()
})

test('tab keyboard selection copies the active original snippet', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/docs/installation')
  const tabs = page.getByRole('tablist').first()
  await expect(tabs).toBeVisible()
  await tabs.getByRole('tab').first().focus()
  await page.keyboard.press('ArrowRight')
  const selected = tabs.getByRole('tab').nth(1)
  await expect(selected).toHaveAttribute('aria-selected', 'true')
  const panel = page.getByRole('tabpanel').first()
  await expect(panel).toHaveAttribute('aria-labelledby', (await selected.getAttribute('id'))!)
  const expected = await panel.locator('pre code').textContent()
  await tabs.locator('..').getByRole('button', { name: 'Copy code' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(expected)
})

for (const width of [320, 390, 768, 1440, 1920]) test(`responsive ${width}px layout has no page overflow`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  for (const url of ['/', '/docs', '/docs/installation', '/docs/configuration']) {
    await page.goto(url)
    await expect(page.locator('main')).toBeVisible()
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)
    expect(overflow).toBe(false)
    const header = await page.locator('.site-header-inner').boundingBox()
    const body = await page.locator('.docs-layout').boundingBox()
    const footer = await page.locator('.site-footer').boundingBox()
    expect(header).not.toBeNull()
    expect(body).not.toBeNull()
    expect(footer).not.toBeNull()
    for (const container of [body!, footer!]) {
      expect(container.x).toBeCloseTo(header!.x, 1)
      expect(container.width).toBeCloseTo(header!.width, 1)
    }
    expect(header!.width).toBeLessThanOrEqual(1280)
    if (width >= 1440) {
      expect(header!.width).toBe(1280)
      const toc = await page.locator('.docs-toc').boundingBox()
      expect(toc!.x + toc!.width).toBeCloseTo(body!.x + body!.width, 1)
    }
  }
})

test('saved theme is applied on first paint and syntax uses multiple colours', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('whipcode-docs-theme', 'dark'))
  await page.goto('/docs/configuration')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  const dark = await page.locator('pre .token').evaluateAll((nodes) => [...new Set(nodes.map((node) => getComputedStyle(node).color))])
  expect(dark.length).toBeGreaterThan(2)
  await page.getByRole('button', { name: 'Colour theme: dark' }).click()
  await page.getByRole('menuitemradio', { name: 'Light', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  const light = await page.locator('pre .token').evaluateAll((nodes) => [...new Set(nodes.map((node) => getComputedStyle(node).color))])
  expect(light.length).toBeGreaterThan(2)
  expect(light).not.toEqual(dark)
})
