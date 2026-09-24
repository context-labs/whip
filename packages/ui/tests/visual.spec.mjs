import { test, expect } from '@playwright/test';

const fixtures = [
  { name: 'content', story: 'whip-library--content-states', ready: page => page.getByRole('heading', { name: 'Your work stays on the host' }) },
  { name: 'forms', story: 'whip-library--form-states', ready: page => page.getByRole('textbox', { name: 'Session title' }) },
  { name: 'overlay', story: 'whip-library--overlay-states', ready: page => page.getByRole('button', { name: 'Open dialog', exact: true }) },
  { name: 'narrow', story: 'whip-library--narrow-layout', narrow: true, ready: page => page.getByRole('textbox', { name: 'Session title' }) },
  { name: 'runtime', story: 'whip-visual-states--runtime', ready: page => page.getByRole('heading', { name: 'Session activity' }) },
];

for (const theme of ['light', 'dark']) for (const fixture of fixtures) {
  test(`${fixture.name} ${theme}`, async ({ page }) => {
    const errors = [];
    const fontOrigins = new Set();
    page.on('pageerror', error => errors.push(error.message));
    page.on('response', response => { if (response.request().resourceType() === 'font') fontOrigins.add(new URL(response.url()).origin); });
    await page.emulateMedia({ colorScheme: theme });
    if (fixture.narrow) await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`/iframe.html?id=${fixture.story}&viewMode=story&theme=${theme}`);
    await fixture.ready(page).waitFor();
    await page.waitForFunction(expected => document.documentElement.dataset.theme === expected, theme);
    await page.evaluate(async () => {
      await Promise.all([document.fonts.load('16px "Inter Variable"'), document.fonts.load('16px "JetBrains Mono Variable"')]);
      await document.fonts.ready;
    });
    expect(await page.evaluate(() => [...document.fonts].some(font => font.family.includes('Inter Variable') && font.status === 'loaded'))).toBe(true);
    expect([...fontOrigins]).toEqual([new URL(page.url()).origin]);
    if (fixture.name === 'content') {
      await page.getByRole('tab', { name: 'Activity', exact: true }).click();
      await page.getByRole('button', { name: 'Read tool output', exact: true }).click();
      await expect(page.getByText('Three child agents are still working.')).toBeVisible();
    }
    if (fixture.name === 'overlay') {
      await page.waitForFunction(() => document.getElementById('storybook-root')?.dataset.playComplete === 'true');
      await page.getByRole('button', { name: 'Open dialog', exact: true }).click();
      await page.getByRole('dialog', { name: 'Session settings' }).waitFor();
      await page.getByRole('button', { name: 'More actions', exact: true }).click();
      await page.getByRole('menu').waitFor();
    }
    if (fixture.name === 'runtime') {
      await page.locator('figure[data-highlighted="true"]').waitFor();
      await expect(page.getByText('Permission requested', { exact: true })).toBeVisible();
      await expect(page.getByText('Permission approved', { exact: true })).toBeVisible();
      await expect(page.getByText('Turn interrupted', { exact: true })).toBeVisible();
      if (process.env.WHIP_UI_VISUAL_MUTATE === '1') {
        // Negative proof changes real component geometry, never the baseline.
        await page.getByRole('heading', { name: 'Agent activity' }).evaluate(element => { element.style.fontSize = '48px'; });
      }
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect(page).toHaveScreenshot(`${fixture.name}-${theme}.png`, { fullPage: fixture.name !== 'overlay' });
    expect(errors).toEqual([]);
  });
}
