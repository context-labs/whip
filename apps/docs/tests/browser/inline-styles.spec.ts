import { test, expect } from '@playwright/test'

for (const mode of ['dark', 'light'] as const) test(`transcript-style links and inline code in ${mode} mode`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: mode })
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/docs/installation')
  const linkColor = mode === 'dark' ? 'rgb(51, 177, 255)' : 'rgb(0, 114, 195)'
  const codeColor = mode === 'dark' ? 'rgb(37, 190, 106)' : 'rgb(25, 128, 56)'
  const background = mode === 'dark' ? 'rgb(30, 30, 30)' : 'rgb(244, 244, 244)'
  const link = page.locator('article p a').first()
  await expect(link).toHaveCSS('color', linkColor)
  await expect(link).toHaveCSS('text-decoration-line', 'underline')
  await expect(link).toHaveCSS('text-underline-offset', '3px')
  await link.hover()
  await expect(link).toHaveCSS('color', linkColor)
  await link.focus()
  await expect(link).toHaveCSS('outline-style', 'solid')
  const code = page.locator('article p code').first()
  await expect(code).toHaveCSS('color', codeColor)
  await expect(code).toHaveCSS('background-color', background)
  await expect(code).toHaveCSS('padding', '0px 4px')
  await expect(code).toHaveCSS('border-width', '0px')
  await expect(code).toHaveCSS('border-radius', '4px')
  const ratio = await code.evaluate(node => parseFloat(getComputedStyle(node).fontSize) / parseFloat(getComputedStyle(node.parentElement!).fontSize))
  expect(ratio).toBeCloseTo(0.85, 2)
  await expect(page.locator('article td code').first()).toHaveCSS('color', codeColor)

  // Navigation and fenced-code output must not inherit prose decoration.
  await expect(page.locator('.header-download')).not.toHaveCSS('color', linkColor)
  await expect(page.locator('.header-download')).toHaveCSS('text-decoration-line', 'none')
  await expect(page.locator('pre code').first()).toHaveCSS('padding', '0px')
  await expect(page.locator('pre code').first()).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(page.locator('pre code').first()).toHaveCSS('font-size', '12px')
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)

  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(page.locator('.sidebar-item').first()).not.toHaveCSS('color', linkColor)
  await expect(page.locator('.sidebar-item').first()).toHaveCSS('text-decoration-line', 'none')
})
