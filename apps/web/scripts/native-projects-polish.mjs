import assert from 'node:assert/strict';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { randomUUID } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';

// Run built native product assets from an explicit checkout, using only owned
// fixture hosts/directories. No response rewriting or fake browser state.
const repository = resolve(process.argv[2] ?? fileURLToPath(new URL('../../../', import.meta.url)));
const { startFixture, deadline } = await import(pathToFileURL(join(repository, 'apps/web/scripts/native-fixture.mjs')));
const output = process.env.WHIP_PROJECTS_POLISH_RESULTS ?? '/tmp/whip-projects-polish';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(output, { recursive: true });
const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
const report = { rendererDigest: manifest.digest, source: manifest.source, browsers: {}, limits: ['RPC-backed browser folder picker on both fixture hosts; macOS native panel chrome is separate.', '20px product UI font and 530/390/320 CSS pixel reflow, not native browser zoom.'] };

for (const name of names) {
  const fixtures = [], checks = [], errors = [], requests = [];
  let browser, page;
  try {
    const local = await startFixture({ lifetimeMs: 600_000 }); fixtures.push(local);
    const remote = await startFixture({ allowedOrigins: [local.info.web], lifetimeMs: 600_000 }); fixtures.push(remote);
    const hosts = [];
    for (const [index, fixture] of fixtures.entries()) {
      const client = await fixture.connect(`projects-polish-${name}-${index}`);
      const created = await fixture.createRoot(client, { title: `Project host ${index}` });
      hosts.push({ fixture, client, root: created.root, cwd: (await client.session(created.root.id).get(deadline())).working_directory, name: index ? 'Remote fixture' : 'Local', id: fixture.info.runtime_id });
    }
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    page.setDefaultTimeout(15_000);
    page.on('pageerror', error => { if (errors.length < 32) errors.push(error.message); });
    await page.exposeFunction('__projectsCSP', value => { if (errors.length < 32) errors.push(`CSP: ${value}`); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => void window.__projectsCSP(event.violatedDirective)));
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try {
        const request = JSON.parse(String(payload));
        assert(requests.length < 8000, 'Bounded native request evidence');
        if (request.method) requests.push({ method: request.method, directory: request.method === 'host.directory.create' ? request.params : undefined });
      } catch (error) { if (errors.length < 32) errors.push(String(error)); }
    }));
    const ready = async () => { await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeVisible(); await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))); };
    const navigate = async host => { await page.goto(`${local.info.web}/h/${host.id}/s/${host.root.id}`); await ready(); };
    await navigate(hosts[0]);
    await page.locator('#whip-session-navigation').getByRole('button', { name: 'Manage servers', exact: true }).click();
    await page.getByRole('button', { name: 'Add server', exact: true }).click();
    const add = page.getByRole('dialog', { name: 'Add server', exact: true });
    await add.getByLabel(/Server name/).fill(hosts[1].name);
    await add.getByLabel('Server address', { exact: true }).fill(remote.info.web);
    await add.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(add).toHaveCount(0);
    await page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
    const projects = page.getByRole('region', { name: 'Projects', exact: true });
    for (const host of hosts) await expect(projects.locator(`[data-sidebar-runtime="${host.id}"][data-sidebar-session="${host.root.id}"]`)).toBeVisible();
    assert(requests.some(request => request.method === 'trees.recent'));
    // Recent activity moves directory groups truthfully across host boundaries.
    const initial = await projects.locator('[data-sidebar-directory]').evaluateAll(nodes => nodes.map(node => node.dataset.sidebarRuntime));
    assert.equal(initial[0], hosts[1].id);
    const submitID = randomUUID();
    await hosts[0].client.session(hosts[0].root.id).submit([{ type: 'text', text: 'Project activity ordering fixture' }], submitID, deadline());
    await hosts[0].client.wait(submitID, deadline());
    await expect.poll(() => projects.locator('[data-sidebar-directory]').first().getAttribute('data-sidebar-runtime')).toBe(hosts[0].id);
    checks.push('one Projects surface uses native recent activity to order exact host/directory groups');

    for (const [index, host] of hosts.entries()) {
      await projects.locator(`[data-sidebar-runtime="${host.id}"][data-sidebar-directory]`).getByRole('link', { name: `New session in ${host.cwd}`, exact: true }).click();
      const composer = page.getByRole('textbox', { name: 'Your first message', exact: true });
      await expect(composer).toBeVisible(); await composer.fill(`Retain folder draft ${index}`);
      const before = requests.filter(request => ['trees.create', 'sessions.submit'].includes(request.method)).length;
      await page.getByRole('button', { name: 'Project folder', exact: true }).click();
      const picker = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
      await picker.getByRole('button', { name: 'New folder', exact: true }).click();
      const form = picker.getByRole('form', { name: 'New folder', exact: true });
      await expect(form.getByRole('textbox', { name: 'Folder name', exact: true })).toBeFocused();
      await form.getByRole('textbox', { name: 'Folder name', exact: true }).fill('../outside');
      await expect(form.getByRole('button', { name: 'Create', exact: true })).toBeDisabled();
      const folder = `Created folder ${index} # ü`, destination = join(host.cwd, folder);
      await form.getByRole('textbox', { name: 'Folder name', exact: true }).fill(folder);
      const creates = requests.filter(request => request.method === 'host.directory.create').length;
      await form.getByRole('button', { name: 'Create', exact: true }).click();
      await expect(form).toHaveCount(0);
      assert((await stat(destination)).isDirectory());
      assert.equal(requests.filter(request => request.method === 'host.directory.create').length, creates + 1);
      await expect(picker).toBeVisible();
      await picker.getByRole('button', { name: 'Choose folder', exact: true }).click();
      await expect(picker).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveAttribute('title', destination);
      await expect(composer).toHaveValue(`Retain folder draft ${index}`);
      assert.equal(requests.filter(request => ['trees.create', 'sessions.submit'].includes(request.method)).length, before);
    }
    checks.push('actual local/remote native mkdir validates one name, requires explicit choice, preserves drafts, and admits no session/turn');

    await navigate(hosts[0]);
    const tab = page.getByRole('tab').filter({ hasText: 'Project host 0' }).first();
    for (const key of ['Shift+F10', 'ContextMenu']) {
      await tab.focus(); await tab.press(key);
      await expect.poll(() => page.getByRole('menu').evaluate(node => node.contains(document.activeElement))).toBe(true);
      const items = await page.getByRole('menuitem').allTextContents();
      assert(items.includes('Session details'));
      assert.deepEqual(items.slice(-3), ['Close tab', 'Close other tabs', 'Close tabs to the right']);
      await page.keyboard.press('Escape');
      await expect(page.getByRole('menuitem', { name: 'Session details', exact: true })).toHaveCount(0);
    }
    await tab.focus(); await tab.press('Shift+F10');
    await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
    const details = page.getByRole('dialog', { name: 'Session details', exact: true });
    await expect(details).toBeVisible(); await details.getByRole('button', { name: 'Close', exact: true }).click();
    checks.push('Shift+F10 and ContextMenu open tab actions, close actions are last, Session details opens its retained inspector');

    const geometry = async (dialog, label) => {
      const result = await dialog.evaluate(node => {
        const box = node.getBoundingClientRect();
        return { width: innerWidth, pageWidth: document.documentElement.scrollWidth, left: box.left, right: box.right,
          dialogWidth: node.clientWidth, dialogScrollWidth: node.scrollWidth };
      });
      assert(result.left >= -1 && result.right <= result.width + 1 && result.pageWidth <= result.width + 1
        && result.dialogScrollWidth <= result.dialogWidth + 1, `${label}: ${JSON.stringify(result)}`);
    };
    await page.goto(`${local.info.web}/settings?section=execution`);
    await page.getByRole('button', { name: 'New agent', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'New agent', exact: true });
    await expect(editor.getByRole('textbox', { name: 'Agent id', exact: true })).toBeFocused();
    const more = editor.getByRole('button', { name: 'More options', exact: true });
    await expect(more).toHaveAttribute('aria-expanded', 'false');
    assert((await editor.boundingBox()).height < 900, 'Ordinary agent dialog keeps its natural content height');
    await expect(editor.getByRole('textbox', { name: 'Project files', exact: true })).toHaveCount(0);
    await editor.getByRole('textbox', { name: 'Display name', exact: true }).fill('Retained compact editor');
    await editor.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('button', { name: 'Continue editing', exact: true }).click();
    await expect(editor.getByRole('textbox', { name: 'Display name', exact: true })).toHaveValue('Retained compact editor');
    await more.click(); await expect(editor.getByRole('textbox', { name: 'Project files', exact: true })).toBeVisible();
    await more.click();
    await editor.getByRole('button', { name: 'Discard draft', exact: true }).click();
    await editor.getByRole('button', { name: 'Close', exact: true }).click();
    await page.evaluate(() => localStorage.setItem('whip.appearance.display.v1', JSON.stringify({ version: 1, display: {
      uiFont: 'system', codeFont: 'system', uiSize: 20, codeSize: 24, wrapCode: true, contrast: 'more', motion: 'reduce',
    } })));
    for (const width of [530, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.reload(); await page.getByRole('button', { name: 'New agent', exact: true }).click();
      await geometry(editor, `${width}px compact agent editor`);
      await page.screenshot({ path: join(output, `${name}-${width}-agent.png`) });
      await editor.getByRole('button', { name: 'Close', exact: true }).click();
      await page.goto(`${local.info.web}/?new=1&runtimeId=${hosts[0].id}&cwd=${encodeURIComponent(hosts[0].cwd)}`);
      await page.getByRole('button', { name: 'Project folder', exact: true }).click();
      const picker = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
      await picker.getByRole('button', { name: 'New folder', exact: true }).click();
      await picker.getByRole('textbox', { name: 'Folder name', exact: true }).fill('Narrow form does not create');
      await geometry(picker, `${width}px folder create`);
      await expect(picker.getByRole('button', { name: 'Create', exact: true })).toBeInViewport();
      await page.screenshot({ path: join(output, `${name}-${width}-folder.png`) });
      await picker.getByRole('button', { name: 'Close', exact: true }).click();
      await navigate(hosts[0]);
      await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
      const navigation = page.getByRole('dialog', { name: 'WHIP', exact: true });
      await geometry(navigation, `${width}px Projects navigation`);
      await expect(navigation.getByRole('region', { name: 'Projects', exact: true })).toBeVisible();
      await page.screenshot({ path: join(output, `${name}-${width}-projects.png`) });
      await navigation.getByRole('button', { name: 'Close', exact: true }).click();
      await page.goto(`${local.info.web}/settings?section=execution`);
    }
    checks.push('compact agent options and drafts retained; Projects navigation and agent/folder forms reflow at530/390/320px with maximum product UI font');
    assert.deepEqual(errors, []);
    report.browsers[name] = { checks, errors, requestMethods: [...new Set(requests.map(request => request.method))] };
  } catch (error) {
    report.browsers[name] = { checks, errors, failure: String(error.stack ?? error), focus: await page?.evaluate(() => ({ tag: document.activeElement?.tagName, role: document.activeElement?.getAttribute('role'), label: document.activeElement?.getAttribute('aria-label'), text: document.activeElement?.textContent?.slice(0, 120) })).catch(() => null) };
    await page?.screenshot({ path: join(output, `${name}-failure.png`), fullPage: true }).catch(() => {});
    await writeFile(join(output, `${name}-failure.txt`), (await page?.locator('body').innerText().catch(() => '') ?? '').slice(0, 16384));
  } finally {
    for (const close of [() => browser?.close(), ...fixtures.toReversed().map(fixture => () => fixture.close())]) {
      try { await close(); } catch (error) { report.browsers[name] ??= { checks, errors }; report.browsers[name].failure = `Cleanup failed: ${String(error)}`; }
    }
    await writeFile(join(output, 'report.json'), JSON.stringify(report, null, 2));
  }
}
console.log(JSON.stringify(report, null, 2));
if (Object.values(report.browsers).some(value => value.failure)) process.exitCode = 1;
