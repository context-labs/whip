import { test, expect } from '@playwright/test'

const desktopUrl = 'https://github.com/context-labs/whip/releases/download/v1.0.0-alpha.5/Whip-Beta-1.0.0-alpha.5-arm64.dmg'
const installCommand = 'curl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | sh'

test('quickstart is the installation-first docs entry', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/')
  await expect(page).toHaveURL('/docs/quickstart')
  const article = page.locator('article')
  await expect(page.locator('.page-lead')).toContainText('RLM-based AI coding agent')
  await expect(article.locator('h2')).toHaveText(['TUI', 'Desktop', 'Web', 'Customize'])
  await expect(page.locator('.docs-sidebar .sidebar-item').first()).toHaveText('Quickstart')
  await expect(page.locator('.docs-sidebar').getByRole('link', { name: 'Introduction', exact: true })).toHaveCount(0)
  await expect(article.getByRole('link', { name: 'Download Desktop for macOS Apple Silicon' })).toHaveAttribute('href', desktopUrl)
  await expect(article).toContainText('Providers & models')
  await expect(article).toContainText('/connect')
  await expect(article).toContainText('whipcode web')
  await expect(article).toContainText('New Session')
  await expect(article.locator('pre code').first()).toHaveText(installCommand)
  await article.getByRole('button', { name: 'Copy code' }).first().click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(installCommand + '\n')
  await expect(article.locator('hr')).toHaveCount(0)
  await expect(article).not.toContainText('brew install')
  await expect(article).not.toContainText('npm install')
  await article.getByRole('link', { name: 'Configuration', exact: true }).click()
  await expect(page).toHaveURL('/docs/configuration')
})

for (const javaScriptEnabled of [true, false]) test.describe(`article styles with JavaScript ${javaScriptEnabled ? 'enabled' : 'disabled'}`, () => {
  test.use({ javaScriptEnabled })
  test('public articles share typography, spacing and dividers', async ({ page }) => {
    for (const width of [320, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      for (const slug of ['download', 'quickstart']) {
        await page.goto(`/docs/${slug}`)
        for (const theme of ['light', 'dark']) {
          await page.evaluate(value => { document.documentElement.dataset.theme = value }, theme)
          await expect(page.locator('.doc-title-row h1')).toHaveCSS('font-size', '32px')
          await expect(page.locator('.doc-title-row h1')).toHaveCSS('line-height', '40px')
          await expect(page.locator('.doc-title-row h1')).toHaveCSS('font-weight', '500')
          await expect(page.locator('.page-lead')).toHaveCSS('font-size', '15px')
          await expect(page.locator('.page-lead')).toHaveCSS('line-height', '24px')
          await expect(page.locator('.page-lead')).toHaveCSS('margin-top', '16px')
          await expect(page.locator('.doc-heading')).toHaveCSS('border-bottom-width', '1px')
          await expect(page.locator('.doc-heading')).toHaveCSS('padding-bottom', '32px')
          await expect(page.locator('.doc-heading')).toHaveCSS('margin-bottom', '32px')
          const headings = page.locator('article > h2')
          await expect(headings.first()).toHaveCSS('border-top-width', '0px')
          await expect(headings.first()).toHaveCSS('padding-top', '0px')
          await expect(headings.first()).toHaveCSS('margin-top', '0px')
          expect(await headings.count()).toBeGreaterThan(1)
          for (const heading of await headings.all()) {
            await expect(heading).toHaveCSS('font-size', '22px')
            await expect(heading).toHaveCSS('line-height', '28px')
            await expect(heading).toHaveCSS('font-weight', '500')
          }
          for (const heading of (await headings.all()).slice(1)) {
            await expect(heading).toHaveCSS('border-top-width', '1px')
            await expect(heading).toHaveCSS('border-top-style', 'solid')
            await expect(heading).toHaveCSS('padding-top', '32px')
            await expect(heading).toHaveCSS('margin-top', '64px')
          }
          for (const block of await page.locator('article > h2 + *, article > p + p').all()) {
            await expect(block).toHaveCSS('margin-top', '20px')
          }
          await expect(page.locator('article > hr')).toHaveCount(0)
          expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
        }
      }
    }
  })
})

test('download shows the verified desktop asset and copyable CLI installation', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/docs/download')
  const article = page.locator('article')
  await expect(article.getByRole('link', { name: 'Download Desktop for macOS Apple Silicon' })).toHaveAttribute('href', desktopUrl)
  await expect(article).toContainText('v1.0.0-alpha.5')
  await expect(article).toContainText('Python 3')
  await expect(article.locator('tbody tr')).toHaveCount(4)
  await expect(article.locator('tbody tr').last()).toContainText('Windows')
  await expect(article.locator('tbody tr').last()).not.toContainText('Available')
  const tabs = article.locator('.code-tabs')
  await expect(tabs.getByRole('tabpanel', { name: 'Install', exact: true }).locator('pre code')).toHaveText(installCommand)
  await tabs.getByRole('button', { name: 'Copy code' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(installCommand + '\n')
  await tabs.getByRole('tab', { name: 'Inspect first' }).click()
  const panel = tabs.getByRole('tabpanel', { name: 'Inspect first' })
  await expect(panel).toContainText('less install-whipcode.sh')
  const source = await panel.locator('pre code').textContent()
  await tabs.getByRole('button', { name: 'Copy code' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(source)
  await expect(article).toContainText('whipcode --version')
  await expect(article).toContainText('Do not update a Desktop-managed backend independently')
  await expect(article).not.toContainText('npm install')
  await expect(article).not.toContainText('brew install')
})

for (const width of [320, 1440]) test(`onboarding pages remain usable without JavaScript at ${width}px`, async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width, height: 900 } })
  const page = await context.newPage()
  await page.goto('http://127.0.0.1:3101/docs/quickstart')
  await expect(page.locator('article pre')).toHaveCount(3)
  for (const block of await page.locator('article pre').all()) await expect(block).toBeVisible()
  await page.goto('http://127.0.0.1:3101/docs/download')
  await expect(page.getByRole('link', { name: 'Download Desktop for macOS Apple Silicon' })).toBeVisible()
  await expect(page.locator('.code-tabs-fallback pre')).toHaveCount(2)
  for (const block of await page.locator('.code-tabs-fallback pre').all()) await expect(block).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  await context.close()
})
