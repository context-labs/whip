import { chromium } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const axePath = require.resolve('axe-core/axe.min.js');

const base = process.env.STORYBOOK_URL || 'http://127.0.0.1:6007';
const output = process.env.DOCS_AUDIT_DIR || '/tmp/whip-docs-board-review';
await mkdir(output, { recursive: true });
const browser = await chromium.launch();
const reports = [];
try {
  for (const theme of ['dark', 'light']) {
    for (const width of [1440, 768, 390]) {
      const page = await browser.newPage({ viewport: { width, height: 1000 }, deviceScaleFactor: 1 });
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.goto(`${base}/iframe.html?id=docs-board-reference--${theme}&viewMode=story`, { waitUntil: 'networkidle' });
      await page.locator('.board-reference').waitFor();
      await page.screenshot({ path: `${output}/board-${theme}-${width}.png`, fullPage: true });
      const geometry = await page.evaluate(() => {
        const button = document.querySelector('.board-state .button');
        const code = document.querySelector('.code-block');
        const style = getComputedStyle(button);
        return { width: innerWidth, scrollWidth: document.documentElement.scrollWidth, theme: document.documentElement.dataset.theme, buttonHeight: button.getBoundingClientRect().height, buttonRadius: style.borderRadius, codeHeaderHeight: code.querySelector('.code-header').getBoundingClientRect().height, codePadding: getComputedStyle(code.querySelector('pre')).padding, syntaxColours: [...new Set([...document.querySelectorAll('pre .token')].map(token => getComputedStyle(token).color))] };
      });
      await page.addScriptTag({ path: axePath });
      const accessibility = await page.evaluate(async () => (await window.axe.run(document.querySelector('.board-reference'), { runOnly: ['wcag2a', 'wcag2aa', 'wcag21aa'] })).violations.map(({ id, nodes }) => ({ id, nodes: nodes.map(node => node.target) })));
      reports.push({ theme, width, errors, accessibility, ...geometry });
      if (accessibility.length) throw new Error(`Board accessibility violations: ${JSON.stringify(accessibility)}`);
      if (geometry.scrollWidth > width || errors.length) throw new Error(`Board failed at ${theme}/${width}: ${JSON.stringify(reports.at(-1))}`);
      if (geometry.buttonHeight !== 34 || geometry.buttonRadius !== '6px' || geometry.codeHeaderHeight !== 40 || geometry.codePadding !== '20px') throw new Error(`Board geometry drift: ${JSON.stringify(geometry)}`);
      await page.close();
    }
    const page = await browser.newPage({ viewport: { width: 840, height: 900 } });
    for (const story of ['callouts', 'code-tabs', 'syntax-languages', 'code-overflow', 'copy-feedback', 'button-states', 'navigation-drawer', 'contents', 'table-overflow', 'pagination']) {
      await page.goto(`${base}/iframe.html?id=docs-components--${story}&viewMode=story&globals=theme:${theme}`, { waitUntil: 'networkidle' });
      await page.locator('.component-story').waitFor();
      await page.screenshot({ path: `${output}/${story}-${theme}.png`, fullPage: true });
      if (story === 'code-tabs') {
        await page.getByRole('tab', { name: 'TypeScript' }).click();
        await page.screenshot({ path: `${output}/code-typescript-${theme}.png`, fullPage: true });
      }
      if (story === 'button-states') {
        await page.getByRole('button', { name: 'More actions for Run example' }).click();
        await page.screenshot({ path: `${output}/split-menu-${theme}.png`, fullPage: true });
      }
      if (story === 'navigation-drawer') {
        await page.getByRole('button', { name: 'Documentation' }).click();
        await page.screenshot({ path: `${output}/drawer-open-${theme}.png`, fullPage: true });
      }
      if (story === 'copy-feedback') {
        await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => {} } }));
        await page.getByRole('button', { name: 'Copy code' }).click();
        await page.getByRole('button', { name: 'Copied' }).waitFor();
        await page.screenshot({ path: `${output}/copy-success-${theme}.png`, fullPage: true });
        await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new Error('denied'); } } }));
        await page.getByRole('button', { name: 'Copied' }).click();
        await page.getByRole('status').filter({ hasText: 'Copy failed' }).waitFor();
        await page.screenshot({ path: `${output}/copy-failure-${theme}.png`, fullPage: true });
      }
    }
    await page.close();
  }
} finally {
  await writeFile(`${output}/report.json`, JSON.stringify(reports, null, 2));
  await browser.close();
}
console.log(`Board and component screenshots: ${output}`);
console.log(JSON.stringify(reports, null, 2));
