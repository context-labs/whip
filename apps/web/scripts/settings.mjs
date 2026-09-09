import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

process.env.OPENROUTER_API_KEY = 'fixture-settings-key';
process.env.INFERENCE_API_KEY = '';

const results = process.env.WHIP_SETTINGS_RESULTS ?? '/tmp/whip-settings-browser-results';
await mkdir(results, { recursive: true });
const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
for (const engine of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const fixture = await startFixture({ lifetimeMs: 600_000 });
  const browser = await ({ chromium, firefox }[engine]).launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1080 } });
  const errors = [], frames = [], checks = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push(JSON.parse(String(payload))); } catch {} }));
  await page.addInitScript(() => {
    window.cspErrors = [];
    document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
  });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const chatURL = `${origin}/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`;
  const auditServers = async selector => {
    await page.route(`${origin}/__settings-axe.js`, route => route.fulfill({ contentType: 'text/javascript', body: axeSource }));
    await page.addScriptTag({ url: `${origin}/__settings-axe.js` });
    const violations = await page.evaluate(async selector => (await axe.run(document.querySelector(selector), {
      runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] },
    })).violations.map(item => ({ id: item.id, nodes: item.nodes.map(node => node.target) })), selector);
    assert.deepEqual(violations, [], `Servers accessibility: ${selector}`);
  };
  const category = label => page.getByRole('navigation', { name: 'Settings categories', exact: true }).getByRole('button', { name: label, exact: true });
  const select = async (label, option) => {
    await page.getByRole('combobox', { name: label, exact: true }).click();
    await page.getByRole('option', { name: option, exact: true }).click();
  };
  try {
    await writeFile(join(fixture.directory, 'home', 'config.json'), JSON.stringify({ defaultModel: 'settings-model', defaultProvider: 'openrouter', models: { 'settings-model': { providers: ['openrouter'] }, 'unsaved-settings-model': { providers: ['openrouter'] } } }));
    await writeFile(join(fixture.directory, 'home', 'models.json'), JSON.stringify({ openrouter: { baseUrl: 'https://openrouter.ai/api/v1', fetchedAt: new Date().toISOString(), models: [{ id: 'settings-model' }, { id: 'unsaved-settings-model', reasoning_efforts: ['low', 'high'] }] } }));
    await page.goto(chatURL);
    const composer = page.locator('[data-whip-composer]');
    await composer.waitFor();
    await composer.fill('Keep this unsent draft while changing settings.');
    const before = await page.evaluate(() => sessionStorage.getItem('whip.web.workspace.v3'));
    await page.locator('#whip-settings-link').click();
    await expect(page.getByRole('heading', { name: 'Appearance', exact: true })).toBeVisible();
    await expect(page.locator('#whip-session-navigation')).toHaveCount(0);
    await expect(page.getByRole('tablist')).toHaveCount(0);
    assert.equal(await page.evaluate(() => sessionStorage.getItem('whip.web.workspace.v3')), before);
    checks.push('dedicated Settings shell preserves the workspace without hidden conversation navigation/tabs');

    const density = page.getByRole('slider', { name: 'Tool call density', exact: true });
    const contentReads = () => frames.filter(frame => /content\.(read|download)|root\.snapshot|history\.page/.test(frame.method ?? '')).length;
    const reads = contentReads();
    await density.focus(); await page.keyboard.press('Home'); await page.keyboard.press('ArrowRight');
    await expect(density).toHaveAttribute('aria-valuetext', 'Comfortable');
    await expect(page.locator('[data-tool-preview]')).toBeVisible();
    await page.keyboard.press('End');
    await expect(page.locator('[data-message-id="appearance-tool-preview"] details')).toHaveAttribute('open', '');
    assert.equal(contentReads(), reads, 'Density initiated transcript/content reads');
    await page.getByRole('switch', { name: 'Code block word wrap', exact: true }).click();
    const uiSize = page.getByRole('textbox', { name: 'UI font size', exact: true });
    await uiSize.fill('13.5'); await uiSize.press('Tab');
    await expect(uiSize).toHaveValue('14');
    await uiSize.fill('20'); await uiSize.press('Tab');
    const codeSize = page.getByRole('textbox', { name: 'Code font size', exact: true });
    await codeSize.fill('24'); await codeSize.press('Tab');
    await select('UI font family', 'System font');
    await select('Code font family', 'System monospace');
    await select('Contrast', 'Increased');
    await select('Reduce motion', 'Reduce');
    const previewCode = page.locator('[aria-label="Live appearance preview"] pre').last();
    await expect(previewCode).toHaveCSS('font-size', '24px');
    await expect(previewCode).toHaveCSS('white-space', 'pre-wrap');
    const codeText = await previewCode.textContent();
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'UI font size', exact: true })).toHaveValue('20');
    await expect(previewCode).toHaveCSS('font-size', '24px');
    assert.equal(await previewCode.textContent(), codeText);
    checks.push('density is fetch-free; font size/family, wrapping, accessibility, and direct numeric input persist through reload');

    await page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
    await expect(composer).toHaveValue('Keep this unsent draft while changing settings.');
    await expect(page).toHaveURL(chatURL);
    checks.push('Back after reload restores the exact session and unsent draft');
    await page.locator('#whip-settings-link').click();
    await category('Providers & models').click();
    const model = page.getByRole('button', { name: 'Default model', exact: true });
    await model.click();
    await page.getByRole('option', { name: 'unsaved-settings-model · openrouter', exact: true }).click();
    await expect(page.getByRole('combobox', { name: 'Reasoning effort' })).toContainText('Model default');
    await category('Appearance').click();
    const guard = page.getByRole('dialog', { name: 'Unsaved settings' });
    await expect(guard).toBeVisible();
    await expect(guard.locator('p').last()).toHaveCSS('font-size', '20px');
    await page.screenshot({ path: join(results, `${engine}-unsaved-large.png`) });
    await guard.getByRole('button', { name: 'Stay', exact: true }).click();
    await expect(model).toContainText('unsaved-settings-model');
    await category('Appearance').click();
    await guard.getByRole('button', { name: 'Discard changes', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Appearance', exact: true })).toBeVisible();
    checks.push('real host form keeps unsaved edits on Stay and discards only after the explicit action');

    for (const label of ['General', 'Agents & execution', 'Servers', 'Recovery', 'About & updates']) {
      await category(label).click();
      await expect(page.getByRole('heading', { name: label, exact: true })).toBeVisible();
      await expect(page.getByRole('button', { name: /^Attention ·/ })).toHaveCount(0);
      if (label === 'Servers') {
        await expect(page.getByRole('heading', { name: 'Servers', exact: true })).toHaveCount(1);
        await expect(page.getByRole('textbox', { name: 'Server address' })).toHaveCount(0);
        await expect(category('Servers')).toHaveAttribute('aria-current', 'page');
        await page.mouse.move(1100, 80);
        await auditServers('main');
        await page.screenshot({ path: join(results, `${engine}-servers-light.png`) });
        await page.getByRole('button', { name: 'Actions for Local' }).click();
        await page.getByRole('menuitem', { name: 'Rename server' }).click();
        const rename = page.getByRole('dialog', { name: 'Rename server' });
        await rename.getByRole('textbox', { name: 'Server name', exact: true }).fill('Studio Mac');
        await rename.getByRole('button', { name: 'Save name' }).click();
        await expect(page.getByRole('button', { name: 'Actions for Studio Mac' })).toBeFocused();
        await page.reload();
        await expect(page.getByRole('button', { name: 'Actions for Studio Mac' })).toBeVisible();
        await expect(page.getByText('Connected', { exact: true })).toBeVisible();
        checks.push('Local server rename persists across reload and keeps its connection');
        const serverActions = page.getByRole('button', { name: 'Actions for Studio Mac' });
        await serverActions.click();
        await expect(page.getByRole('menuitem').last()).toHaveText('Disconnect');
        await page.getByRole('menuitem', { name: 'Disconnect', exact: true }).click();
        const disconnect = page.getByRole('alertdialog', { name: 'Disconnect Studio Mac?' });
        await expect(disconnect).toBeVisible();
        await expect(page.getByText('Connected', { exact: true })).toBeVisible();
        await disconnect.getByRole('button', { name: 'Cancel', exact: true }).click();
        await expect(serverActions).toBeFocused();
        await expect(page.getByText('Connected', { exact: true })).toBeVisible();
        await serverActions.click();
        await page.getByRole('menuitem', { name: 'Disconnect', exact: true }).click();
        await disconnect.getByRole('button', { name: 'Disconnect', exact: true }).click();
        await expect(disconnect).toBeHidden();
        await expect(page.getByText('Disconnected', { exact: true })).toBeVisible();
        await serverActions.click();
        await page.getByRole('menuitem', { name: 'Connect', exact: true }).click();
        await expect(page.getByText('Connected', { exact: true })).toBeVisible();
        checks.push('Disconnect is last, cancellation preserves the connection, and confirmed disconnect allows reconnecting');
        const add = page.getByRole('button', { name: 'Add server', exact: true });
        await add.focus(); await page.keyboard.press('Enter');
        const dialog = page.getByRole('dialog', { name: 'Add server', exact: true });
        await expect(dialog.getByLabel('Server address', { exact: true })).toBeFocused();
        await expect(dialog.getByRole('textbox').first()).toHaveAttribute('required', '');
        await expect(dialog.getByRole('tab', { name: 'SSH', exact: true })).toHaveCount(0);
        await expect(dialog.getByRole('textbox')).toHaveCount(2);
        await expect(dialog.getByRole('button', { name: 'Advanced', exact: true })).toHaveAttribute('aria-expanded', 'false');
        await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
        await expect(dialog.getByRole('button', { name: 'Advanced', exact: true })).toHaveAttribute('aria-expanded', 'true');
        await expect(dialog.getByRole('button', { name: 'Advanced', exact: true }).locator('svg')).toHaveCSS('transform', 'matrix(0, 1, -1, 0, 0, 0)');
        await auditServers('[role="dialog"]');
        await page.screenshot({ path: join(results, `${engine}-server-add.png`) });
        await page.keyboard.press('Escape'); await expect(dialog).not.toBeVisible();
        await expect(add).toBeFocused();
        await page.setViewportSize({ width: 390, height: 844 });
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Servers overflows narrow viewport');
        await page.screenshot({ path: join(results, `${engine}-servers-narrow.png`) });
        await add.click();
        await expect(dialog.getByLabel('Server address', { exact: true })).toBeFocused();
        assert.ok(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth), 'Add server overflows narrow dialog');
        await page.screenshot({ path: join(results, `${engine}-server-add-narrow.png`) });
        await page.keyboard.press('Escape');
        await page.setViewportSize({ width: 1440, height: 1080 });
        checks.push('Servers has one heading, a focused two-field URL form with Advanced collapsed, desktop-only SSH, Escape focus return, no narrow page/dialog horizontal overflow, and WCAG 2/2.1 A/AA Axe checks');
      }

    }
    await page.getByRole('searchbox', { name: 'Search settings' }).fill('code font');
    await page.getByRole('button', { name: 'Code font size Appearance', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'Code font size', exact: true })).toBeFocused();
    await page.getByRole('button', { name: 'Reset appearance', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'UI font size', exact: true })).toHaveValue('13');
    await expect(page.getByRole('textbox', { name: 'Code font size', exact: true })).toHaveValue('12');
    await expect(page.getByRole('switch', { name: 'Code block word wrap', exact: true })).not.toBeChecked();
    await page.screenshot({ path: join(results, `${engine}-appearance-light.png`) });
    await page.getByRole('combobox', { name: /^Color theme:/ }).click();
    await page.getByRole('combobox', { name: 'Search themes', exact: true }).fill('Claude Code');
    await page.getByRole('option', { name: 'Claude Code Dark', exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'claude-code');
    await page.screenshot({ path: join(results, `${engine}-appearance-dark.png`) });
    await category('Servers').click();
    await auditServers('main');
    await page.screenshot({ path: join(results, `${engine}-servers-dark.png`) });
    await page.getByRole('button', { name: 'Add server', exact: true }).click();
    await auditServers('[role="dialog"]');
    await page.screenshot({ path: join(results, `${engine}-server-add-dark.png`) });
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: 'Add server', exact: true })).toHaveCount(0);
    await category('Appearance').click();

    checks.push('all categories render; settings search focuses the target; reset and custom palette selection work');

    const picker = page.getByRole('combobox', { name: /^Color theme:/ });
    await picker.click();
    const themeSearch = page.getByRole('combobox', { name: 'Search themes', exact: true });
    await expect(themeSearch).toHaveValue('');
    await expect(themeSearch).toBeFocused();
    await expect(page.getByRole('listbox')).toHaveCount(1);
    assert.equal(await themeSearch.locator('..').locator('..').getByRole('status').first().evaluate(element => element.getBoundingClientRect().height), 0,
      'Empty-results announcer must not reserve blank space when themes are present');
    await expect(page.getByRole('option', { name: 'Claude Code Dark', exact: true })).toHaveAttribute('aria-selected', 'true');
    const palette = element => {
      const color = node => getComputedStyle(node).backgroundColor;
      return { canvas: color(element), ...Object.fromEntries([...element.querySelectorAll('[data-swatch-role]')].map(node => [node.dataset.swatchRole, color(node)])) };
    };
    const selectedSwatch = picker.locator('[data-theme-swatch="claude-code"]');
    const claudePalette = await selectedSwatch.evaluate(palette);
    assert.deepEqual(claudePalette, { canvas: 'rgb(20, 20, 20)', navigation: 'rgb(17, 17, 16)', primary: 'rgb(200, 117, 85)', foreground: 'rgb(194, 192, 184)', composer: 'rgb(34, 34, 33)' });
    assert.deepEqual(await page.getByRole('option', { name: 'Claude Code Dark', exact: true }).locator('[data-theme-swatch]').evaluate(palette), claudePalette);
    const searchFocus = await themeSearch.evaluate(input => ({ border: getComputedStyle(input.parentElement).borderBottomColor,
      neutral: getComputedStyle(input, '::placeholder').color, radius: getComputedStyle(input.parentElement).borderRadius }));
    assert.equal(searchFocus.border, searchFocus.neutral, 'Theme search focus must use the neutral input role');
    assert.equal(searchFocus.radius, '6px');
    await page.screenshot({ path: join(results, `${engine}-theme-picker.png`) });
    await themeSearch.fill('Claude Code');
    await page.getByRole('listbox').screenshot({ path: join(results, `${engine}-claude-swatch.png`) });
    await themeSearch.fill('nord');
    await themeSearch.press('ArrowDown');
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'claude-code');
    assert.deepEqual(await selectedSwatch.evaluate(palette), claudePalette, 'Previewing another theme must not recolor the selected theme thumbnail');
    await themeSearch.press('Escape');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'claude-code');
    await expect(picker).toBeFocused();
    await picker.click();
    await expect(themeSearch).toHaveValue('');
    await themeSearch.fill('no theme matches this');
    await expect(page.getByText('No matching themes', { exact: true })).toBeVisible();
    await themeSearch.press('Escape');
    checks.push('single theme popup opens with empty focused search, marks selection, filters, and cancels keyboard preview');
    checks.push('neutral search focus and miniature workspace swatches match Claude Code canvas/navigation/accent/text/composer colors');

    assert.ok(await page.locator('#custom-themes').evaluate(row => row.getBoundingClientRect().top < document.getElementById('toolDensity').getBoundingClientRect().top), 'Custom themes must accompany the main theme control');
    await expect(page.getByLabel('Theme source host', { exact: true })).toHaveCount(0);
    const backWidth = await page.getByRole('button', { name: 'Back to workspace', exact: true }).evaluate(button => {
      const sidebar = button.parentElement, style = getComputedStyle(sidebar);
      return Math.abs(button.getBoundingClientRect().width - (sidebar.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)));
    });
    assert.ok(backWidth < 1, 'Back button should fill the sidebar content width');
    const importButton = page.getByRole('button', { name: 'Import theme', exact: true });
    await importButton.click();
    const importer = page.getByRole('dialog', { name: 'Import theme', exact: true });
    await expect(importer.getByRole('button', { name: 'Choose file', exact: true })).toBeVisible();
    await page.screenshot({ path: join(results, `${engine}-theme-import.png`) });
    const upload = importer.locator('input[type="file"]');
    const resolutions = () => frames.filter(frame => frame.method === 'host.themes.resolve').length;
    const beforeOversize = resolutions();
    await upload.setInputFiles({ name: 'too-large.json', mimeType: 'application/json', buffer: Buffer.alloc(64 * 1024 + 1, ' ') });
    await expect(importer.getByRole('alert')).toContainText('This file is too large');
    assert.equal(resolutions(), beforeOversize, 'Oversized file must not be sent to the resolver');
    await upload.setInputFiles({ name: 'broken.json', mimeType: 'application/json', buffer: Buffer.from('{broken') });
    await expect(importer.getByRole('alert')).toContainText('Could not import theme');
    await expect(importer.getByRole('alert')).not.toContainText('This file is too large');
    await upload.setInputFiles({ name: 'custom.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ name: 'Settings custom theme', dark: true, palette: { primary: '#c87555' } })) });
    await expect(importer).toBeHidden();
    await expect(picker).toHaveAccessibleName('Color theme: Settings custom theme');
    await expect(importButton).toBeFocused();
    await page.reload();
    await expect(picker).toHaveAccessibleName('Color theme: Settings custom theme');
    await picker.click();
    await themeSearch.fill('Claude Code');
    await page.getByRole('option', { name: 'Claude Code Dark', exact: true }).click();
    await expect(themeSearch).toBeHidden();
    checks.push('top-level local theme import: oversized and malformed file errors, retry, application, focus return, and persistence');

    for (const width of [1024, 768, 767, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), { message: `Page overflow at ${width}px` }).toBe(true);
      if (width < 768) {
        await page.getByRole('button', { name: 'Appearance', exact: true }).click();
        const sheet = page.getByRole('dialog', { name: 'Settings', exact: true });
        await sheet.getByRole('searchbox', { name: 'Search settings' }).fill('font');
        await page.keyboard.press('ArrowDown');
        assert.ok(await sheet.evaluate(element => element.contains(document.activeElement)), 'Mobile search focused a hidden sidebar');
        await page.keyboard.press('Enter');
        await expect(sheet).toBeHidden();
      }
      await expect(page.getByRole('textbox', { name: 'UI font size', exact: true })).toBeVisible();
    }
    await page.getByRole('textbox', { name: 'UI font size', exact: true }).fill('20');
    await page.getByRole('textbox', { name: 'UI font size', exact: true }).press('Tab');
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Large text overflow at 320px');
    await picker.click();
    await expect(themeSearch).toBeFocused();
    assert.ok(await page.getByRole('listbox').evaluate(element => { const box = element.getBoundingClientRect(); return box.left >= 0 && box.right <= innerWidth; }), 'Theme popup overflow at 320px');
    await page.screenshot({ path: join(results, `${engine}-theme-picker-narrow.png`) });
    await themeSearch.press('Escape');
    await page.screenshot({ path: join(results, `${engine}-appearance-narrow.png`) });
    checks.push('1024/768/767/390/320 widths, narrow category search keyboard focus, and maximum UI size reflow');
    assert.deepEqual(errors, [], 'Browser runtime errors');
    assert.deepEqual(await page.evaluate(() => window.cspErrors), [], 'Production CSP violations');
    await writeFile(join(results, `${engine}.json`), JSON.stringify({ engine, checks, pageErrors: errors, cspErrors: [], frameCount: frames.length }, null, 2));
    console.log(`${engine}: ${checks.length} Settings workflows passed`);
  } catch (error) {
    await page.screenshot({ path: join(results, `${engine}-failure.png`) }).catch(() => {});
    await writeFile(join(results, `${engine}-failure.txt`), `${error.stack}\nPage errors: ${JSON.stringify(errors)}\n${await page.locator('body').innerText().catch(() => '')}`);
    throw error;
  } finally { await browser.close(); await fixture.close(); }
}
