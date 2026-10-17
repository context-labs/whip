import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';

const web = fileURLToPath(new URL('../', import.meta.url));
const results = process.env.WHIP_AGENT_RESULTS ?? '/tmp/whip-agent-dialog-results';
await mkdir(results, { recursive: true });
process.chdir(web);
const config = {
  configFile: false, root: resolve(web, 'scripts/fixtures/agents'), logLevel: 'warn',
  plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
    unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react()],
  build: { outDir: resolve(results, 'dist'), emptyOutDir: true, assetsInlineLimit: 0 },
  preview: { host: '127.0.0.1', port: 0, headers: {
    'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'",
  } },
};
await build(config);
const server = await preview(config);
const origin = 'http://127.0.0.1:' + server.httpServer.address().port;
const report = [];
try {
  for (const [engine, browserType] of Object.entries({ chromium, firefox })) {
    const browser = await browserType.launch();
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {
        window.cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
      });
      for (const scenario of [
        { name: 'dark', width: 1440, height: 1000 },
        { name: 'light', width: 1440, height: 1000, theme: 'light' },
        { name: 'laptop', width: 1280, height: 800 },
        { name: 'tablet', width: 768, height: 1024, theme: 'light' },
        { name: 'phone', width: 390, height: 844 },
        { name: 'narrow', width: 320, height: 640, theme: 'light' },
        { name: 'large-text', width: 800, height: 900, size: 20 },
        { name: 'narrow-large-text', width: 320, height: 640, size: 20 },
      ]) {
        await page.setViewportSize({ width: scenario.width, height: scenario.height });
        await page.goto(origin + '/?theme=' + (scenario.theme ?? 'dark') + '&size=' + (scenario.size ?? 13));
        const trigger = page.getByRole('button', { name: 'New agent', exact: true });
        await trigger.click();
        const dialog = page.getByRole('dialog', { name: 'New agent' });
        await expect(page.locator('html')).toHaveAttribute('data-theme', scenario.theme ?? 'dark');
        await expect(dialog).toHaveCSS('font-size', (scenario.size ?? 13) + 'px');
        const modules = dialog.getByRole('group', { name: 'Host modules', exact: true });
        await expect(modules.getByRole('checkbox')).toHaveCount(14);
        await expect(dialog.getByRole('group', { name: 'Capabilities', exact: true }).getByRole('checkbox')).toHaveCount(6);
        await page.evaluate(() => document.fonts.ready);
        const nameInput = dialog.getByRole('textbox', { name: 'Agent Name', exact: true });
        await expect(nameInput).toBeFocused();
        const proseFont = await dialog.getByRole('textbox', { name: 'Persona' }).evaluate(element => getComputedStyle(element).fontFamily);
        await expect(nameInput).toHaveCSS('font-family', proseFont);
        await nameInput.fill('release-notes');
        await dialog.getByRole('textbox', { name: 'Persona' }).fill('You write release notes for the engineering team.');
        await dialog.getByRole('textbox', { name: 'Rules' }).fill('Cite commits.\nGroup changes by impact.');
        await modules.getByRole('checkbox', { name: 'context', exact: true }).click();
        await modules.getByRole('checkbox', { name: 'files', exact: true }).click();
        const geometry = await dialog.evaluate(element => {
          const bounds = element.getBoundingClientRect();
          const fieldsets = [...element.querySelectorAll('fieldset')];
          return {
            width: bounds.width, height: bounds.height,
            overflow: element.scrollWidth > element.clientWidth,
            crushed: fieldsets.some(fieldset => fieldset.querySelector('p').getBoundingClientRect().width < fieldset.clientWidth - 2),
            clipped: [...element.querySelectorAll('label')].some(label => {
              const rect = label.getBoundingClientRect();
              return rect.left < bounds.left || rect.right > bounds.right || label.scrollWidth > label.clientWidth + 1;
            }),
          };
        });
        assert.ok(!geometry.overflow && !geometry.crushed && !geometry.clipped, JSON.stringify({ scenario, geometry }));
        assert.equal(geometry.width, Math.min(800, scenario.width - 32), 'Agent editor should use the wider desktop width without overflowing small screens');
        if (scenario.width >= 1280) assert.ok(geometry.height < 740, 'Agent editor should fit a laptop viewport without shrinking text');
        await dialog.getByRole('heading').scrollIntoViewIfNeeded();
        await page.screenshot({ path: resolve(results, engine + '-' + scenario.name + '.png') });
        const submit = dialog.getByRole('button', { name: 'Register agent', exact: true });
        if (scenario.width >= 1280) await expect(submit).toBeInViewport({ ratio: 1 });
        await submit.scrollIntoViewIfNeeded();
        await expect(submit).toBeInViewport();
        await page.screenshot({ path: resolve(results, engine + '-' + scenario.name + '-bottom.png') });
        const options = dialog.getByRole('button', { name: 'More options' });
        await options.focus();
        await options.press('Enter');
        await expect(options).toHaveAttribute('aria-expanded', 'true');
        await dialog.getByRole('textbox', { name: 'Project files' }).fill('AGENTS.md, CLAUDE.md');
        await dialog.getByRole('switch', { name: 'Goal loop', exact: true }).click();
        await submit.scrollIntoViewIfNeeded();
        await expect(submit).toBeInViewport();
        assert.ok(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth), 'Expanded options overflow');
        await page.screenshot({ path: resolve(results, engine + '-' + scenario.name + '-expanded.png') });
        await options.click();
        await dialog.getByRole('button', { name: 'Register agent', exact: true }).click();
        await expect(dialog.getByRole('status')).toContainText('Registered release-notes');
        await page.keyboard.press('Escape');
        await expect(dialog).toBeHidden();
        await expect(trigger).toBeFocused();
        await expect(page.getByRole('list', { name: 'Agent definitions' })).toContainText('release-notes');
        await trigger.click();
        await expect(dialog.getByRole('textbox', { name: 'Agent Name', exact: true })).toBeFocused();
        await page.keyboard.press('Escape');
        await expect(dialog).toBeHidden();
        assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
        report.push({ engine, scenario: scenario.name, ...geometry });
      }
      assert.deepEqual(errors, []);
    } finally { await browser.close(); }
  }
  await writeFile(resolve(results, 'report.json'), JSON.stringify(report, null, 2));
  console.log('Agent dialog browser checks passed:', results);
} finally { await new Promise(resolve => server.httpServer.close(resolve)); }
