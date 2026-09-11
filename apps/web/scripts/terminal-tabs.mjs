import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Terminal tabs against a real daemon: open from the pane menu, type into the
// shell, reload and receive the replay, close and confirm the shell is gone.
const directory = process.env.WHIP_TERMINAL_RESULTS ?? '/tmp/whip-terminal-tabs-results';
await mkdir(directory, { recursive: true });
const results = {};
const decode = value => Buffer.from(value ?? '', 'base64').toString('utf8');
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: starting terminal tab fixture`);
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[name]).launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  context.setDefaultTimeout(20_000);
  const page = await context.newPage();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `terminal-${crypto.randomUUID()}`, clientKind: 'human' });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const runtimeId = fixture.info.runtime_id, rootId = fixture.info.root_id;
  const sent = [], received = [], errors = [], checks = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive/i.test(event.text())) errors.push(event.text()); });
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => { try { sent.push(JSON.parse(String(payload))); } catch {} });
    socket.on('framereceived', ({ payload }) => { try { received.push(JSON.parse(String(payload))); } catch {} });
  });
  await page.addInitScript(() => {
    window.cspErrors = [];
    document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(`${event.violatedDirective}: ${event.blockedURI}`));
  });
  const outputs = () => received.filter(frame => frame.method === 'terminal.output').map(frame => ({ id: frame.params.id, cursor: Number(frame.params.cursor), text: decode(frame.params.bytes) }));
  const outputText = id => outputs().filter(item => item.id === id).map(item => item.text).join('');
  const terminalView = () => page.locator('[data-terminal-view]');
  const live = async () => { await eventually(async () => (await terminalView().getAttribute('data-terminal-status')) === 'live', { description: 'terminal view live' }); };
  try {
    await client.connect();
    await page.goto(`${origin}/h/${runtimeId}/s/${rootId}`);
    await page.locator('[data-whip-composer]').waitFor();
    await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'New terminal', exact: true }).click();
    await eventually(() => /\/h\/[^/]+\/t\//.test(page.url()), { description: 'terminal route' });
    const terminalId = decodeURIComponent(new URL(page.url()).pathname.split('/t/')[1]);
    assert.ok(terminalId.startsWith('term-'), `terminal id ${terminalId}`);
    await live();
    const opened = sent.find(frame => frame.method === 'terminal.open');
    assert.ok(opened, 'terminal.open was sent');
    assert.equal(opened.params.root_id, rootId, 'pane menu passes the selected session as the cwd hint');
    assert.ok(sent.some(frame => frame.method === 'terminal.attach' && frame.params.id === terminalId && frame.params.cursor === '0'), 'first attach starts at cursor 0');
    await eventually(() => outputText(terminalId).length > 0, { description: 'shell prompt output' });
    checks.push('pane menu opens a shell on the host and attaches the tab');

    await terminalView().click();
    await page.keyboard.type('echo whip-$((40+2))');
    await page.keyboard.press('Enter');
    await eventually(() => outputText(terminalId).includes('whip-42'), { description: 'shell echoed the command result' });
    const writes = sent.filter(frame => frame.method === 'terminal.write' && frame.params.id === terminalId);
    assert.ok(writes.some(frame => decode(frame.params.bytes).includes('e')), 'keystrokes reached the daemon as terminal.write');
    assert.ok(writes.some(frame => decode(frame.params.bytes) === '\r'), 'Enter was sent as a carriage return');
    // Cursors are contiguous per terminal.
    let expected = -1;
    for (const item of outputs().filter(item => item.id === terminalId)) {
      if (expected >= 0) assert.equal(item.cursor, expected, 'output cursors are contiguous');
      expected = item.cursor + Buffer.byteLength(item.text);
    }
    assert.deepEqual(await page.evaluate(() => window.cspErrors), [], 'no CSP violations while rendering the terminal');
    await page.screenshot({ path: join(directory, `${name}-terminal.png`) });
    checks.push('typing reaches the shell and its output returns in cursor order under the production CSP');

    // Reload: the tab is restored from window storage and the daemon replays.
    const before = outputs().length;
    await page.reload();
    await live();
    await eventually(() => outputs().slice(before).some(item => item.id === terminalId && item.text.includes('whip-42')), { description: 'replay after reload' });
    assert.ok(sent.filter(frame => frame.method === 'terminal.open').length === 1, 'reload attaches instead of opening another shell');
    assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
    checks.push('reload reattaches the same shell and replays earlier output');

    // A second client taking over detaches the page; Reattach here recovers.
    const takeover = await client.terminals.attach(terminalId, -1);
    assert.equal(takeover.exited, false);
    await eventually(async () => (await terminalView().getAttribute('data-terminal-status')) === 'detached', { description: 'page notices the takeover' });
    await page.getByRole('button', { name: 'Reattach here', exact: true }).click();
    await live();
    checks.push('another connection can take the shell over and the tab can reattach');

    // Close the tab: the shell ends and the daemon forgets it.
    // The terminal tab's own context menu; the shell may have retitled the tab by now.
    await page.locator(`a[href*="/t/${encodeURIComponent(terminalId)}"]`).first().click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Close tab', exact: true }).click();
    await eventually(() => sent.some(frame => frame.method === 'terminal.close' && frame.params.id === terminalId), { description: 'terminal.close sent' });
    await eventually(async () => {
      try { await client.terminals.attach(terminalId, 0); return false; }
      catch (error) { return error?.code === -32003; }
    }, { description: 'daemon reports the closed terminal as gone' });
    assert.equal(await terminalView().count(), 0, 'terminal view unmounted');
    assert.deepEqual(errors, []);
    checks.push('closing the tab ends the shell');
    results[name] = { browser: await browser.version(), checks, terminalId, writes: writes.length, outputs: outputs().length };
    console.log(`${name}: ${checks.length} terminal workflows passed`);
  } catch (error) {
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify({ sent, received: received.slice(-200) }, null, 2));
    throw error;
  } finally { client?.close(); await browser?.close(); await fixture.close(); }
}
await writeFile(join(directory, 'terminal-tabs.json'), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));
