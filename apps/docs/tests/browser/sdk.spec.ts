import { test, expect } from '@playwright/test'

const sections = ["Availability and installation", "Connect to a host", "Create a session and run a turn", "Recover uncertain delivery", "Observe history and execution", "Decisions, content and accounting", "Author tools and agents"]

// The TypeScript SDK page is currently a draft (draft: true in frontmatter), so it is
// not published. These tests are skipped until the page is published again.
test.skip('SDK reference renders complete highlighted examples and preserves copied code', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/docs/typescript-sdk')
  const article = page.locator('article')
  await expect(article.locator('h2')).toHaveText(sections)
  await expect(article).toContainText('private ESM workspace package')
  await expect(article).toContainText('permissions.resolve')
  await expect(article).toContainText('A timeout stops observation')
  await expect(article).toContainText('context.progress')
  await expect(article.locator('pre[data-language="typescript"]')).toHaveCount(10)
  await expect(article).not.toContainText('npm install @whip/sdk')
  const block = article.locator('.code-block').filter({ hasText: "const result = await command.wait({ signal });" }).first()
  const code = block.locator('pre code')
  const source = await code.textContent()
  await expect(code.locator('.token.keyword').first()).toBeVisible()
  await block.getByRole('button', { name: 'Copy code' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(source)
  await page.locator('.docs-toc a').filter({ hasText: 'Observe history and execution' }).click()
  await expect(page).toHaveURL('/docs/typescript-sdk#observe-history-and-execution')
})

for (const width of [320, 1440]) test.skip(`SDK examples are readable without JavaScript at ${width}px`, async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width, height: 900 } })
  const page = await context.newPage()
  await page.goto('http://127.0.0.1:3101/docs/typescript-sdk')
  await expect(page.locator('article h2')).toHaveText(sections)
  await expect(page.locator('article pre')).toHaveCount(12)
  for (const pre of await page.locator('article pre').all()) await expect(pre).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  await context.close()
})
