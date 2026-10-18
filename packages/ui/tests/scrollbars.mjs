import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, webkit, expect } from '@playwright/test';

const packageRoot = fileURLToPath(new URL('../', import.meta.url));
const root = resolve(packageRoot, 'tests/fixtures/scrollbars');
const outDir = resolve(packageRoot, 'ui-test-results/scrollbars');
await mkdir(outDir, { recursive: true });
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'";
await build({ configFile: false, root, logLevel: 'warn', plugins: [
  stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false, unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(packageRoot, '../..') } }), react(),
], build: { outDir, emptyOutDir: true, assetsInlineLimit: 0 } });
const server = await preview({ configFile: false, root, build: { outDir }, preview: { host: '127.0.0.1', port: 0, headers: { 'Content-Security-Policy': csp } } });
const origin = 'http://127.0.0.1:' + server.httpServer.address().port;

async function appearance(region) {
  return region.evaluate(el => {
    const style = getComputedStyle(el), bar = getComputedStyle(el, '::-webkit-scrollbar'), thumb = getComputedStyle(el, '::-webkit-scrollbar-thumb');
    return {
      width: style.scrollbarWidth, color: style.scrollbarColor, background: style.backgroundColor,
      barWidth: bar.width, barHeight: bar.height, thumb: thumb.backgroundColor,
      border: thumb.borderTopWidth, radius: thumb.borderTopLeftRadius, clip: thumb.backgroundClip,
      track: getComputedStyle(el, '::-webkit-scrollbar-track').backgroundColor,
    };
  });
}

try {
  for (const [name, engine] of Object.entries({ chromium, firefox, webkit })) {
    // Playwright hides Chromium scrollbars by default; headless macOS Firefox reports
    // scrollbar-width:none even for inline thin/auto. Test the actual controls.
    const browser = await engine.launch({ headless: !(name === 'firefox' && process.platform === 'darwin'), ignoreDefaultArgs: name === 'chromium' ? ['--hide-scrollbars'] : [] });
    try {
      const page = await browser.newPage({ viewport: { width: 960, height: 600 } });
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {
        window.__cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.__cspErrors.push({ directive: event.violatedDirective, blocked: event.blockedURI, source: event.sourceFile, line: event.lineNumber, target: event.target?.outerHTML }));
      });
      await page.goto(origin);
      const output = page.getByLabel('Output', { exact: true });
      const navigation = page.getByRole('region', { name: 'Navigation' });
      await expect(output).toBeVisible();
      expect(await page.evaluate(() => matchMedia('(pointer: fine)').matches)).toBe(true);
      const dark = await appearance(output);
      if (name === 'firefox') {
        expect(dark.width).toBe('thin');
        expect(dark.color).not.toBe('auto');
      } else {
        expect(dark.width).toBe('auto');
        expect(dark.color).toBe('auto');
        expect(dark.barWidth).toBe('9px');
        expect(dark.barHeight).toBe('9px');
        expect(dark.border).toBe('2px');
        expect(dark.radius).toBe('999px');
        expect(dark.clip).toBe('padding-box');
        expect(dark.track).toBe('rgba(0, 0, 0, 0)');
        expect((await appearance(navigation)).thumb).toBe(dark.thumb);
      }
      // Playwright WebKit screenshots inject a body{} style blocked by strict CSP.
      if (name !== 'webkit') await page.screenshot({ path: resolve(outDir, name + '-dark.png'), caret: 'initial' });
      await page.getByRole('button', { name: 'Switch theme' }).click();
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
      const light = await appearance(output);
      expect(name === 'firefox' ? light.color : light.thumb).not.toBe(name === 'firefox' ? dark.color : dark.thumb);
      if (name !== 'webkit') await page.screenshot({ path: resolve(outDir, name + '-light.png'), caret: 'initial' });
      // The same native viewport keeps focus, keyboard, wheel and both axes.
      await output.focus();
      await page.keyboard.press('PageDown');
      await expect.poll(() => output.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
      await output.evaluate(el => { el.scrollTop = 0; el.scrollLeft = 0; });
      await output.hover();
      await page.mouse.wheel(0, 180);
      await expect.poll(() => output.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
      await output.evaluate(el => { el.scrollLeft = 120; });
      await expect.poll(() => output.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
      // Native thumb dragging on macOS overlays remains a manual check: synthetic
      // drags do not scroll even an unstyled control. Hover is observable.
      if (name === 'chromium') {
        await output.evaluate(async el => {
          el.scrollTop = 0;
          await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        });
        const box = await output.boundingBox();
        await page.mouse.move(box.x + box.width - 4, box.y + 12);
        const hovered = await appearance(output);
        expect(hovered.thumb).not.toBe(light.thumb);
      }
      if (name !== 'webkit') {
        await page.emulateMedia({ forcedColors: 'active' });
        const forced = await appearance(output);
        expect(forced.width).toBe('auto');
        expect(forced.color).toBe('auto');
        if (name === 'chromium') expect(forced.barWidth).toBe('auto');
      }
      expect(errors).toEqual([]);
      expect(await page.evaluate(() => window.__cspErrors)).toEqual([]);
      const touch = await browser.newPage({ hasTouch: true });
      await touch.goto(origin);
      const touchOutput = touch.getByLabel('Output', { exact: true });
      await expect(touchOutput).toBeVisible();
      expect(await touch.evaluate(() => matchMedia('(pointer: coarse)').matches)).toBe(true);
      const nativeTouch = await appearance(touchOutput);
      expect(nativeTouch.width).toBe('auto');
      expect(nativeTouch.color).toBe('auto');
      if (name !== 'firefox') expect(nativeTouch.barWidth).toBe('auto');
      console.log(name + ': native geometry, themes, scrolling, platform fallbacks and CSP passed');
    } finally { await browser.close(); }
  }
} finally { await new Promise(resolve => server.httpServer.close(resolve)); }
