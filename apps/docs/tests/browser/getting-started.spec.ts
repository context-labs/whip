import { test, expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { appRoot } from '../../scripts/content.mjs'

test('all page outlines keep copy and download source actions', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  for (const slug of ['quickstart', 'download', 'typescript-sdk']) {
    const source = await readFile(`${appRoot}/src/content/docs/${slug}/index.mdx`, 'utf8')
    await page.goto(`/docs/${slug}`)
    await page.getByRole('button', { name: 'Copy page', exact: true }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(source)
    await page.getByRole('button', { name: 'More page actions' }).click()
    const action = page.getByRole('menuitem', { name: 'Download page source' })
    const downloadEvent = page.waitForEvent('download')
    await action.click()
    const download = await downloadEvent
    expect(download.suggestedFilename()).toBe(`${slug}.mdx`)
    expect(await readFile((await download.path())!, 'utf8')).toBe(source)
  }
})

test('failed page copy offers source-download recovery', async ({ page }) => {
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new Error('denied') } } }))
  await page.goto('/docs/quickstart')
  await page.getByRole('button', { name: 'Copy page', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Copy failed. Use the menu to download the page source.' })).toBeVisible()
  await page.getByRole('button', { name: 'More page actions' }).click()
  await expect(page.getByRole('menuitem', { name: 'Download page source' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: 'More page actions' })).toBeFocused()
})
