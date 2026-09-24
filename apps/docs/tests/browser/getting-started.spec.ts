import { test, expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { appRoot } from '../../scripts/content.mjs'

const sections = ['Start your first session', 'Continue from the terminal', 'Understand workspace scope', 'Troubleshooting']
const steps = ['1. Install whipcode', '2. Connect a model provider', '3. Open your project', '4. Send a focused task', '5. Review the result']

test('getting started follows the Paper article structure without changing sidebar labels', async ({ page }) => {
  await page.goto('/docs/getting-started')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Getting started')
  await expect(page.locator('article h2')).toHaveText(sections)
  await expect(page.locator('article h3')).toHaveText(steps)
  await expect(page.locator('.docs-toc a')).toHaveText(sections)
  await expect(page.locator('.docs-sidebar a[aria-current="page"]')).toHaveText('Get started')
  await expect(page.locator('article .callout')).toHaveCount(2)
  await expect(page.locator('article tbody tr')).toHaveCount(4)
  await expect(page.locator('.doc-pagination a')).toHaveAttribute('href', '/docs/using-whipcode/cli')
  await expect(page.getByRole('tablist')).toHaveCount(2)
  const examples = page.getByRole('tablist', { name: 'First task examples' })
  await examples.getByRole('tab', { name: 'CLI', exact: true }).click()
  await expect(page.getByRole('tabpanel').first().locator('pre .token').first()).toBeVisible()
  expect(await page.locator('article').innerText()).not.toContain('Fast Inference')
})

test('page actions copy the complete original MDX and download it without fetching', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  const source = await readFile(`${appRoot}/src/content/docs/getting-started/index.mdx`, 'utf8')
  await page.goto('/docs/getting-started')
  await page.getByRole('button', { name: 'Copy page', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(source)
  await page.getByRole('button', { name: 'More page actions' }).click()
  const action = page.getByRole('menuitem', { name: 'Download page source' })
  const href = await action.getAttribute('href')
  expect(decodeURIComponent(href!.split(',').slice(1).join(','))).toBe(source)
  const downloadEvent = page.waitForEvent('download')
  await action.click()
  const download = await downloadEvent
  expect(download.suggestedFilename()).toBe('getting-started.mdx')
  expect(await readFile((await download.path())!, 'utf8')).toBe(source)
  await page.goto('/docs/installation')
  await expect(page.getByRole('button', { name: 'Copy page', exact: true })).toHaveCount(0)
})

test('failed page copy offers a working source-download recovery', async ({ page }) => {
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new Error('denied') } } }))
  await page.goto('/docs/getting-started')
  await page.getByRole('button', { name: 'Copy page', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Copy failed. Use the menu to download the page source.' })).toBeVisible()
  await page.getByRole('button', { name: 'More page actions' }).click()
  await expect(page.getByRole('menuitem', { name: 'Download page source' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: 'More page actions' })).toBeFocused()
})

for (const width of [320, 768, 1440]) test(`getting started at ${width}px is readable without JavaScript`, async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width, height: 900 } })
  const page = await context.newPage()
  await page.goto('http://127.0.0.1:3101/docs/getting-started')
  await expect(page.locator('article h2')).toHaveText(sections)
  await expect(page.locator('article h3')).toHaveText(steps)
  await expect(page.locator('article pre')).toHaveCount(5)
  await expect(page.getByRole('button', { name: 'Copy page' })).toHaveCount(0)
  for (const pre of await page.locator('article pre').all()) await expect(pre).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await context.close()
})
