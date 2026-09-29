import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { randomUUID } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';

// Identical visible assertions against the approved reference and native app.
// Backend adapters only create disposable fixtures and map protocol evidence.
// Usage: node frontend-migration-parity.mjs reference|native /absolute/repository
const [mode, repositoryArg] = process.argv.slice(2);
assert(['reference', 'native'].includes(mode), 'Choose reference or native explicitly');
assert(repositoryArg?.startsWith('/'), 'An explicit absolute repository is required');
const repository = resolve(repositoryArg);
const output = process.env.WHIP_PARITY_RESULTS ?? `/tmp/whip-frontend-parity-${mode}`;
const browsers = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(browsers.length && browsers.length <= 2 && new Set(browsers).size === browsers.length && browsers.every(name => ['chromium', 'firefox'].includes(name)));
const load = relative => import(pathToFileURL(join(repository, relative)).href);
const selectedCases = (process.env.WHIP_PARITY_CASES ?? 'A1,A2,A3,A11').split(',');
assert(selectedCases.length && selectedCases.every(value => ['A1', 'A2', 'A3', 'A11'].includes(value)));
const native = mode === 'native';
const deadline = () => ({ signal: AbortSignal.timeout(20_000) });
const { startFixture, eventually } = await load(native ? 'apps/web/scripts/native-fixture.mjs' : 'packages/sdk/scripts/fixture.mjs');
const report = { mode, repository, browsers: {}, limitations: ['Browser fixture acceptance; no signed desktop or real host configuration claim.', 'Independent native tree/queue reads have no separate counterpart in the reference atomic snapshot.'] };
await mkdir(output, { recursive: true });
const profile = "PROMPT='fixture-human> '\nalias whip_fixture_alias='printf fixture-alias-ok'\nexport WHIP_FIXTURE_PROFILE=fixture-profile-ok\nexport PATH=\"$HOME/profile-bin:$PATH\"\n";

for (const name of browsers) {
  let fixture, browser, referenceHome, client;
  const checks = [], failures = [];
  const errors = [], sent = [], terminalOutput = new Map();
  let holdMetadata = false, holdWarm = true, holdSubmitReply = false;
  const held = [];
  let frameCount = 0, terminalBytes = 0;
  const commandMethod = native ? 'sessions.submit' : 'command.submit';
  const releaseReads = () => { holdMetadata = false; holdWarm = false; for (const send of held.splice(0)) send(); };
  let releaseAck;
  let page;
  const record = error => { if (errors.length < 32) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  const run = async (label, body) => {
    if (!selectedCases.includes(label.split('-')[0])) return;
    console.log(`${mode}/${name}: ${label}`);
    try { await body(); checks.push(label); }
    catch (error) {
      failures.push({ label, error: String(error.stack ?? error).slice(0, 12000) });
      await page?.screenshot({ path: join(output, `${name}-${label}-failure.png`), fullPage: true }).catch(() => {});
      await writeFile(join(output, `${name}-${label}-failure.txt`), (await page?.locator('body').innerText().catch(() => '') ?? '').slice(0, 16384));
    } finally { releaseReads(); releaseAck?.(); releaseAck = undefined; }
  };
  try {
    if (native) fixture = await startFixture({ networkTerminals: true, terminalProfile: true, lifetimeMs: 600_000 });
    else {
      referenceHome = await mkdtemp('/tmp/whip-reference-shell-');
      await mkdir(join(referenceHome, 'shell')); await mkdir(join(referenceHome, 'profile-bin'));
      await writeFile(join(referenceHome, 'shell/.zshrc'), profile, { mode: 0o600 });
      // The old fixture merges env with process.env. Explicitly unset inherited
      // credentials and private paths for its child; build tools keep their env.
      const clean = Object.fromEntries(Object.keys(process.env).map(key => [key, undefined]));
      fixture = await startFixture({ lifetimeMs: 600_000, env: { ...clean, PATH: '/usr/bin:/bin:/usr/sbin:/sbin', HOME: referenceHome,
        SHELL: '/bin/zsh', ZDOTDIR: join(referenceHome, 'shell'), TMPDIR: referenceHome, WHIP_FIXTURE_PROMPT_LABEL: 'fixture-human' } });
      await writeFile(join(fixture.directory, 'home/config.json'), JSON.stringify({ defaultModel: 'model', defaultProvider: 'provider',
        providers: { provider: { name: 'Isolated fixture', baseUrl: fixture.info.frontend + '/v1', api: 'openai-completions', auth: 'none' } }, models: { model: { providers: ['provider'] } } }));
      await writeFile(join(fixture.directory, 'home/models.json'), JSON.stringify({ provider: { baseUrl: fixture.info.frontend + '/v1', fetchedAt: new Date().toISOString(), models: [{ id: 'model' }] } }));
    }
    const origin = native ? fixture.info.web : fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    let root;
    if (native) { client = await fixture.connect(`parity-${randomUUID()}`); root = (await fixture.createRoot(client)).root.id; }
    else { const { createWhipClient } = await load('packages/sdk/dist/index.js'); client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `parity-${randomUUID()}`, clientKind: 'human' }); await client.connect(); root = fixture.info.root_id; }
    const session = client.session(root), path = `/h/${fixture.info.runtime_id}/s/${root}`;
    const completed = async text => {
      if (!native) return session.submit({ text }).result(deadline());
      const requestID = randomUUID(); await session.submit([{ type: 'text', text }], requestID, deadline()); return client.wait(requestID, deadline());
    };
    browser = await ({ chromium, firefox }[name]).launch();
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'dark' });
    context.setDefaultTimeout(15_000);
    page = await context.newPage(); page.on('pageerror', record);
    await page.addInitScript(() => {
      window.__parityOnboarding = [];
      const capture = () => {
        const title = [...document.querySelectorAll('h1,h2')].find(element => element.textContent.includes('Connect a provider to get started'));
        if (title && title.getClientRects().length && getComputedStyle(title).visibility !== 'hidden') window.__parityOnboarding.push(performance.now());
      };
      new MutationObserver(capture).observe(document, { subtree: true, childList: true, attributes: true });
    });
    await page.routeWebSocket(`**/api/v${native ? 4 : 3}/ws`, socket => {
      const upstream = socket.connectToServer(), pending = new Map();
      socket.onMessage(message => {
        try {
          const frame = JSON.parse(String(message));
          if (frame.method) {
            assert(++frameCount <= 6000, 'RPC fixture evidence bound exceeded');
            pending.set(frame.id, frame);
            if (sent.length < 3000) sent.push({ method: frame.method, params: frame.method.startsWith('terminal.') ? frame.params : undefined });
            const operation = frame.method === 'query' ? frame.params.operation : frame.method;
            const warm = ['providers.catalog', 'providers.catalogs', 'provider.catalogs', 'providers.presets', 'mcp.configuration', 'host.mcp'].includes(operation);
            const metadata = native && ['trees.get', 'inputs.list'].includes(frame.method);
            if ((holdWarm && warm) || (holdMetadata && metadata)) { assert(held.length < 64); held.push(() => upstream.send(message)); return; }
          }
          upstream.send(message);
        } catch (error) { record(error); }
      });
      upstream.onMessage(message => {
        try {
          const frame = JSON.parse(String(message)), request = pending.get(frame.id);
          if (request) pending.delete(frame.id);
          let terminal;
          if (native && request?.method === 'terminal.read' && frame.result) terminal = { id: request.params.id, data: frame.result.data_base64 };
          if (!native && frame.method === 'terminal.output') terminal = { id: frame.params.id, data: frame.params.bytes };
          if (terminal) {
            const text = Buffer.from(terminal.data, 'base64').toString('utf8'); terminalBytes += Buffer.byteLength(text);
            assert(terminalBytes <= (1 << 20), 'Terminal fixture output bound exceeded');
            terminalOutput.set(terminal.id, (terminalOutput.get(terminal.id) ?? '') + text);
          }
          if (holdSubmitReply && request?.method === commandMethod && frame.result) {
            holdSubmitReply = false; releaseAck = () => socket.send(message); return;
          }
          socket.send(message);
        } catch (error) { record(error); }
      });
    });
    const composer = () => page.getByRole('textbox', { name: 'Message WHIP', exact: true });
    const ready = async () => { await expect(composer()).toBeVisible(); await expect(composer()).toBeEnabled(); };
    await run('A1-configured-startup', async () => {
      await page.goto(origin + '/?new=1');
      const firstMessage = page.getByRole('textbox', { name: 'Your first message', exact: true });
      await expect(firstMessage).toBeVisible(); await expect(firstMessage).toBeEnabled();
      assert(held.length > 0, 'Nonessential metadata is actually held');
      await expect(page.getByRole('status').filter({ hasText: 'Loading whipcode' })).toHaveCount(0);
      assert.deepEqual(await page.evaluate(() => window.__parityOnboarding), [], 'Configured startup never visibly flashes onboarding');
      await firstMessage.fill('Unsent startup draft');
      await page.screenshot({ path: join(output, `${name}-startup.png`) });
    });
    await completed('Canonical parity baseline');
    await run('A2-independent-opening-and-draft', async () => {
      holdMetadata = true;
      await page.goto(origin + path); await ready();
      await expect(page.getByRole('region', { name: 'Conversation', exact: true })).toContainText('Canonical parity baseline');
      await composer().fill('Keep this unsent draft and caret'); await composer().focus(); await composer().press('Home'); await composer().press('ArrowRight');
      const before = await composer().evaluate(element => ({ value: element.value, start: element.selectionStart, end: element.selectionEnd }));
      releaseReads();
      await expect(composer()).toBeFocused();
      assert.deepEqual(await composer().evaluate(element => ({ value: element.value, start: element.selectionStart, end: element.selectionEnd })), before);
      await page.reload(); await ready(); await expect(composer()).toHaveValue(before.value);
      assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 1);
      await composer().fill('');
    });
    await run('A3-pending-ack-and-queue', async () => {
      await page.goto(origin + path); await ready();
      const prompt = 'hold:parity-live-' + name;
      holdSubmitReply = true;
      await composer().fill(prompt);
      const before = sent.filter(frame => frame.method === commandMethod).length;
      await page.getByRole('button', { name: 'Send message', exact: true }).click();
      await eventually(() => !!releaseAck, { description: 'held real acceptance reply' });
      await composer().press('Enter');
      assert.equal(sent.filter(frame => frame.method === commandMethod).length, before + 1, 'Pending admission does not duplicate a submission');
      releaseAck(); releaseAck = undefined;
      await expect(composer()).toHaveValue('');
      await composer().fill('Queued parity message'); await composer().press('Enter');
      const queue = page.getByRole('region', { name: 'Queued messages', exact: true });
      await expect(queue).toContainText('Queued parity message');
      await expect(page.getByRole('region', { name: 'Conversation', exact: true })).not.toContainText('Queued parity message');
      await composer().fill('Retained queue draft'); await page.reload(); await ready();
      await expect(composer()).toHaveValue('Retained queue draft'); await expect(queue).toContainText('Queued parity message');
      await fixture.release(prompt.slice(5));
      await expect(queue).toHaveCount(0);
      await expect(page.getByRole('region', { name: 'Conversation', exact: true })).toContainText('Queued parity message');
      assert.equal((await fixture.effects()).filter(text => text === prompt).length, 1);
      assert.equal((await fixture.effects()).filter(text => text === 'Queued parity message').length, 1);
      await composer().fill('');
    });
    await run('A11-human-terminal-profile', async () => {
      await page.goto(origin + path); await ready();
      await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click(); await page.getByRole('menuitem', { name: 'New terminal', exact: true }).click();
      const terminal = page.locator('[data-terminal-view]'); await expect(terminal).toHaveAttribute('data-terminal-status', 'live');
      const id = await terminal.getAttribute('data-terminal-view');
      const text = () => terminalOutput.get(id) ?? '';
      await eventually(() => text().includes('fixture-human>'), { description: 'synthetic host prompt' });
      const opens = sent.filter(frame => frame.method === 'terminal.open').length;
      await terminal.click();
      await page.keyboard.type('whip_fixture_alias; printf "|%s|%s|%s\\n" "$WHIP_FIXTURE_PROFILE" "$WHIP_FIXTURE_PROMPT_LABEL" "$PATH"'); await page.keyboard.press('Enter');
      await eventually(() => text().includes('fixture-alias-ok|fixture-profile-ok|fixture-human|'), { description: 'alias and custom prompt environment survive' });
      assert(text().includes('/profile-bin:'), 'Host profile PATH preserved');
      if (native) assert(sent.some(frame => frame.method === 'terminal.read' && frame.params.wait_ms > 0), 'Native UI uses bounded output waits');
      await page.setViewportSize({ width: 900, height: 700 });
      await eventually(() => sent.some(frame => frame.method === 'terminal.resize'), { description: 'terminal resize' });
      await page.setViewportSize({ width: 1280, height: 900 });
      const writes = sent.filter(frame => frame.method === 'terminal.write').length;
      await page.screenshot({ path: join(output, `${name}-terminal.png`) });
      await page.reload(); await expect(terminal).toHaveAttribute('data-terminal-status', 'live');
      assert.equal(sent.filter(frame => frame.method === 'terminal.open').length, opens, 'Reload reuses the existing shell');
      assert.equal(sent.filter(frame => frame.method === 'terminal.write').length, writes, 'Reload never replays input');
      const tab = page.locator(`[role="tab"][href*="/t/${encodeURIComponent(id)}"]`).first();
      // Let the terminal's initial focus effect settle before a deliberate tab focus.
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      await expect(tab).toBeVisible(); await tab.focus(); await expect(tab).toBeFocused(); await tab.press('Shift+F10');
      await page.getByRole('menuitem', { name: 'Close tab', exact: true }).click(); await expect(terminal).toHaveCount(0);
    });
    assert.deepEqual(errors, [], 'No browser or evidence errors');
    report.browsers[name] = { checks, failures, frameCount, terminalBytes, methods: [...new Set(sent.map(frame => frame.method))], browser: await browser.version(), errors };
  } catch (error) { failures.push({ label: 'fixture', error: String(error.stack ?? error) }); report.browsers[name] = { checks, failures, errors }; }
  finally {
    releaseReads(); releaseAck?.();
    for (const close of [() => browser?.close(), () => client?.close?.(), () => fixture?.close(), () => referenceHome && rm(referenceHome, { recursive: true, force: true })]) {
      try { await close(); } catch (error) { failures.push({ label: 'cleanup', error: String(error.stack ?? error).slice(0, 4096) }); }
    }
    await writeFile(join(output, 'report.json'), JSON.stringify(report, null, 2));
  }
}
console.log(JSON.stringify(report, null, 2));
if (Object.values(report.browsers).some(result => result.failures.length)) process.exitCode = 1;
