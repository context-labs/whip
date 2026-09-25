import { test, expect } from '@playwright/test'

const sections = ['Availability and installation', 'Connect to a host', 'Submit work', 'Events and cancellation', 'Define agents and tools', 'State views and React']

test('SDK reference renders complete highlighted examples and preserves copied code', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/docs/typescript-sdk')
  const article = page.locator('article')
  await expect(article.locator('h2')).toHaveText(sections)
  await expect(article).toContainText('not a public npm package')
  await expect(article).toContainText('permission_mode:')
  await expect(article).toContainText('client.close() only disconnects')
  await expect(article).toContainText('context.invocationId')
  await expect(article.locator('pre[data-language="typescript"]')).toHaveCount(10)
  await expect(article).not.toContainText('npm install @whip/sdk')
  const block = article.locator('.code-block').filter({ hasText: "const result = await turn.result();" }).first()
  const code = block.locator('pre code')
  const source = await code.textContent()
  await expect(code.locator('.token.keyword').first()).toBeVisible()
  await block.getByRole('button', { name: 'Copy code' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(source)
  await page.locator('.docs-toc a').filter({ hasText: 'State views and React' }).click()
  await expect(page).toHaveURL('/docs/typescript-sdk#state-views-and-react')
})

for (const width of [320, 1440]) test(`SDK examples are readable without JavaScript at ${width}px`, async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width, height: 900 } })
  const page = await context.newPage()
  await page.goto('http://127.0.0.1:3101/docs/typescript-sdk')
  await expect(page.locator('article h2')).toHaveText(sections)
  await expect(page.locator('article pre')).toHaveCount(12)
  for (const pre of await page.locator('article pre').all()) await expect(pre).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  await context.close()
})
