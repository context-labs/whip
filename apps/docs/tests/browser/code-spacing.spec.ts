import { test, expect } from '@playwright/test'

for (const width of [390, 1440]) test(`code spacing matches Paper at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  await page.goto('/docs/installation')
  const tabs = page.locator('.code-tabs').first()
  const header = tabs.locator('.code-header')
  await expect(tabs.getByRole('tablist')).toBeVisible()
  await expect(header).toHaveCSS('padding-left', '12px')
  await expect(header).toHaveCSS('padding-right', '8px')
  expect((await header.boundingBox())!.height).toBe(40)
  const tab = tabs.getByRole('tab').first()
  await expect(tab).toHaveCSS('padding-left', '12px')
  await expect(tab).toHaveCSS('padding-right', '12px')
  await expect(tab).toHaveCSS('line-height', '27px')
  // The underline must reach the header divider without the horizontal scroller clipping it.
  const listBox = (await tabs.getByRole('tablist').boundingBox())!
  const tabBox = (await tab.boundingBox())!
  const headerBox = (await header.boundingBox())!
  expect(listBox.x - headerBox.x).toBe(12)
  expect(tabBox.y + tabBox.height).toBe(headerBox.y + headerBox.height)
  expect(tabBox.y + tabBox.height).toBeLessThanOrEqual(listBox.y + listBox.height)

  for (const name of ['Install directly', 'Inspect first']) {
    await tabs.getByRole('tab', { name }).click()
    const pre = tabs.getByRole('tabpanel', { name, exact: true }).locator('pre')
    await expect(pre).toHaveCSS('padding', '20px')
    await expect(pre).toHaveCSS('margin', '0px')
    await expect(pre).toHaveCSS('line-height', '20px')
    const lines = (await pre.textContent())!.replace(/\n$/, '').split('\n').length
    // Content lines plus top/bottom insets: no phantom trailing line or extra vertical margin.
    expect((await pre.boundingBox())!.height).toBe(lines * 20 + 40)
  }

  await page.goto('/docs/getting-started')
  const single = page.locator('.code-block:not(.code-block-tabbed)').first()
  await expect(single.locator('pre')).toHaveCSS('padding', '20px')
  expect((await single.locator('pre').boundingBox())!.height).toBe(60)
  expect((await single.locator('.code-header').boundingBox())!.height).toBe(40)
})

test('no-JavaScript code blocks retain the same body and header padding', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false })
  const page = await context.newPage()
  await page.goto('http://127.0.0.1:3101/docs/installation')
  for (const block of await page.locator('.code-tabs-fallback .code-tabs').all()) {
    await expect(block.locator('pre')).toHaveCSS('padding', '20px')
    await expect(block.locator('.code-header')).toHaveCSS('padding', '0px 8px 0px 12px')
    expect((await block.locator('.code-header').boundingBox())!.height).toBe(40)
  }
  await context.close()
})
