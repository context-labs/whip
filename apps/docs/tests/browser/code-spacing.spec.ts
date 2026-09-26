import { test, expect } from '@playwright/test'

for (const width of [390, 1440]) test(`code spacing matches Paper at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  await page.goto('http://127.0.0.1:6008/iframe.html?id=docs-components--installation-example&viewMode=story')
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
    await expect(pre.locator('code')).toHaveCSS('font-family', await pre.evaluate(node => getComputedStyle(node).fontFamily))
    const lines = (await pre.textContent())!.replace(/\n$/, '').split('\n').length
    // Content lines plus top/bottom insets: no phantom trailing line or extra vertical margin.
    expect((await pre.boundingBox())!.height).toBe(lines * 20 + 40)
  }

  await page.goto('http://127.0.0.1:6008/iframe.html?id=docs-components--installation-example&viewMode=story')
  const single = page.locator('.code-block:not(.code-block-tabbed)').first()
  await expect(single.locator('pre')).toHaveCSS('padding', '20px')
  await expect(single.locator('pre')).toHaveCSS('line-height', '20px')
  expect((await single.locator('.code-header').boundingBox())!.height).toBe(40)
})

test('library code tabs still copy active source and render multicolour tokens', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('http://127.0.0.1:6008/iframe.html?id=docs-components--code-tabs&viewMode=story')
  await page.getByRole('tab', { name: 'CLI', exact: true }).focus()
  await page.keyboard.press('ArrowRight')
  const selected = page.getByRole('tab', { name: 'TypeScript' })
  await expect(selected).toHaveAttribute('aria-selected', 'true')
  const panel = page.getByRole('tabpanel', { name: 'TypeScript' })
  const expected = await panel.locator('pre code').textContent()
  await page.getByRole('button', { name: 'Copy code' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(expected)
  const colours = await panel.locator('pre .token').evaluateAll(nodes => [...new Set(nodes.map(node => getComputedStyle(node).color))])
  expect(colours.length).toBeGreaterThan(2)
})
