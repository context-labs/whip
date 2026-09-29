import assert from 'node:assert/strict';
import { mkdir, realpath, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

// Terminal tabs against a real daemon: open from the pane menu, type into the
// shell, reload and receive the replay, close and confirm the shell is gone.
const directory = process.env.WHIP_TERMINAL_RESULTS ?? '/tmp/whip-terminal-tabs-results';
await mkdir(directory, { recursive: true });
const results = {};
const decode = value => Buffer.from(value ?? '', 'base64').toString('utf8');
const browsers = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(browsers.length > 0 && browsers.length <= 2 && new Set(browsers).size === browsers.length && browsers.every(name => ['chromium', 'firefox'].includes(name)));
for (const name of browsers) {
  console.log(`${name}: starting native terminal tab fixture`);
  let fixture, browser, page;
  const sent = [], received = [], errors = [], csp = [], checks = [];
  let generation = 0, frameCount = 0, retainedBytes = 0;
  const recordError = error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  const outputs = () => received;
  const outputText = id => Buffer.concat(outputs().filter(item => item.id === id).map(item => Buffer.from(item.data_base64, 'base64'))).toString('utf8');
  const terminalView = () => page.locator('[data-terminal-view]');
  const live = async () => { await eventually(async () => (await terminalView().getAttribute('data-terminal-status')) === 'live', { description: 'terminal view live' }); };
  try {
    fixture = await startFixture({ networkTerminals: true });
    const client = await fixture.connect(`terminal-${crypto.randomUUID()}`);
    const { root } = await fixture.createRoot(client);
    const origin = fixture.info.web, runtimeId = client.runtimeID, rootId = root.id;
    browser = await ({ chromium, firefox }[name]).launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    context.setDefaultTimeout(20_000);
    page = await context.newPage();
    page.on('pageerror', recordError);
    page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive/i.test(event.text())) recordError(event.text()); });
    // Per-connection request identities restart after reload. Retain only bounded
    // terminal evidence, never ordinary transcript/provider payloads.
    page.on('websocket', socket => {
      const pending = new Map(), documentGeneration = generation;
      socket.on('close', () => pending.clear());
      socket.on('framesent', ({ payload }) => {
        try {
          assert(Buffer.byteLength(payload) <= (8 << 20));
          const frame = JSON.parse(String(payload));
          if (!frame.method?.startsWith('terminal.')) return;
          assert(++frameCount <= 2048, 'Terminal frame bound exceeded');
          assert(pending.size < 64, 'Terminal pending read bound exceeded');
          const bytes = Buffer.byteLength(JSON.stringify(frame.params));
          assert((retainedBytes += bytes) <= (2 << 20), 'Terminal retained byte bound exceeded');
          sent.push({ method: frame.method, params: frame.params, generation: documentGeneration });
          if (frame.method === 'terminal.read') pending.set(frame.id, frame.params);
        } catch (error) { recordError(error); }
      });
      socket.on('framereceived', ({ payload }) => {
        try {
          assert(Buffer.byteLength(payload) <= (8 << 20));
          const frame = JSON.parse(String(payload)), request = pending.get(frame.id);
          if (!request) return;
          pending.delete(frame.id);
          if (frame.error) return;
          const result = frame.result, bytes = Buffer.from(result.data_base64, 'base64');
          assert(++frameCount <= 2048, 'Terminal frame bound exceeded');
          assert((retainedBytes += Buffer.byteLength(result.data_base64)) <= (2 << 20), 'Terminal retained byte bound exceeded');
          assert.equal(result.terminal.id, request.id); assert.equal(result.terminal.process_epoch, request.process_epoch);
          assert.equal(result.from, request.cursor); assert.equal(result.truncated, false);
          assert.equal(BigInt(result.next) - BigInt(result.from), BigInt(bytes.length));
          assert(BigInt(result.next) <= BigInt(result.end)); assert(bytes.length <= request.limit);
          received.push({ id: request.id, epoch: request.process_epoch, generation: documentGeneration,
            from: result.from, next: result.next, bytes: bytes.length, data_base64: result.data_base64 });
        } catch (error) { recordError(error); }
      });
    });
    await page.exposeFunction('terminalCSP', value => { if (csp.length < 64) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.terminalCSP(event.violatedDirective).catch(() => {}); }));
    await page.goto(`${origin}/h/${runtimeId}/s/${rootId}`);
    await page.locator('[data-whip-composer]').waitFor();
    await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'New terminal', exact: true }).click();
    await eventually(() => /\/h\/[^/]+\/t\//.test(page.url()), { description: 'terminal route' });
    await live();
    const terminalId = await terminalView().getAttribute('data-terminal-view');
    const ref = { id: terminalId, process_epoch: client.processEpoch };
    assert.equal(decodeURIComponent(new URL(page.url()).pathname.split('/t/')[1]), terminalId);
    const opened = sent.find(frame => frame.method === 'terminal.open');
    assert.ok(opened, 'terminal.open was sent');
    assert.equal(opened.params.cwd, root.working_directory, 'pane menu resolves the selected session directory');
    assert.equal(opened.params.process_epoch, client.processEpoch);
    const shells = await client.listTerminals(deadline());
    assert.equal(shells.items.length, 1); assert.equal(shells.items[0].id, terminalId);
    assert.equal(shells.items[0].cwd, await realpath(root.working_directory));
    assert.ok(sent.some(frame => frame.method === 'terminal.read' && frame.params.id === terminalId && frame.params.cursor === '0'), 'first read starts at cursor 0');
    await assert.rejects(client.readTerminal({ ...ref, process_epoch: 'foreign-process-epoch' }, '0', 32768, deadline()), error => error.kind === 'IDENTITY');
    await eventually(() => outputText(terminalId).length > 0, { description: 'shell prompt output' });
    checks.push('pane menu opens one host shell in the selected directory and reads exact epoch-scoped output');

    await terminalView().click();
    await page.keyboard.type('echo whip-$((40+2))');
    await page.keyboard.press('Enter');
    await eventually(() => outputText(terminalId).includes('whip-42'), { description: 'shell echoed the command result' });
    const writes = sent.filter(frame => frame.method === 'terminal.write' && frame.params.id === terminalId);
    // Keystrokes coalesce into as few writes as the round trip allows; the bytes stay in order.
    assert.ok(writes.map(frame => decode(frame.params.data_base64)).join('').includes('echo whip-$((40+2))\r'), 'keystrokes and Enter reached the daemon in order');
    assert.deepEqual(csp, [], 'no CSP violations while rendering the terminal');
    await page.screenshot({ path: join(directory, `${name}-terminal.png`) });
    // Powerline separators, shade blocks and box drawing: a visual check saved for review.
    const glyphs = ' ░▒▓ ─│ glyphs-ok';
    const escapedGlyphs = [...Buffer.from(glyphs + '\n')].map(byte => '\\' + byte.toString(8).padStart(3, '0')).join('');
    await page.keyboard.type(`printf '${escapedGlyphs}'`);
    await page.keyboard.press('Enter');
    await eventually(() => outputText(terminalId).includes(' ░▒▓ ─│ glyphs-ok'), { description: 'actual glyph line printed' });
    await terminalView().screenshot({ path: join(directory, `${name}-glyphs.png`) });
    // Only the shell's cursor may show: the focused contenteditable must not draw the browser caret.
    const caret = await page.evaluate(() => {
      const input = document.querySelector('[data-terminal-view] [contenteditable]');
      return { focused: document.activeElement === input, caretColor: input && getComputedStyle(input).caretColor };
    });
    assert.equal(caret.focused, true, 'terminal input keeps focus');
    assert.equal(caret.caretColor, 'rgba(0, 0, 0, 0)', 'browser caret hidden inside the terminal');
    const box = await terminalView().boundingBox();
    await page.screenshot({ path: join(directory, `${name}-corner.png`), clip: { x: box.x, y: box.y, width: 160, height: 48 } });
    // A program with mouse tracking on must receive wheel travel as SGR mouse reports, not arrow keys.
    const writesBefore = sent.filter(frame => frame.method === 'terminal.write').length;
    await page.keyboard.type("printf '\\e[?1000h\\e[?1006h'; cat -v");
    await page.keyboard.press('Enter');
    await eventually(() => outputText(terminalId).includes('cat -v'), { description: 'mouse tracking program running' });
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.wheel(0, 120);
    await eventually(() => outputText(terminalId).includes('^[[<65;'), { description: 'wheel down arrived as an SGR mouse report' });
    await page.mouse.wheel(0, -120);
    await eventually(() => outputText(terminalId).includes('^[[<64;'), { description: 'wheel up arrived as an SGR mouse report' });
    const wheelWrites = sent.filter(frame => frame.method === 'terminal.write').slice(writesBefore).map(frame => decode(frame.params.data_base64)).join('');
    assert.ok(!wheelWrites.includes('\x1b[A') && !wheelWrites.includes('\x1b[B'), 'no arrow keys were sent while mouse tracking was on');
    // Shift+drag selects locally instead of reporting the drag to the program.
    const reportsBefore = sent.filter(frame => frame.method === 'terminal.write').length;
    await page.keyboard.down('Shift');
    await page.mouse.move(box.x + 20, box.y + 20);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width - 40, box.y + 60, { steps: 6 });
    await page.mouse.up();
    await page.keyboard.up('Shift');
    await eventually(async () => (await terminalView().getAttribute('data-terminal-selection')) === 'true', { description: 'Shift+drag selected text under mouse tracking' });
    assert.equal(sent.filter(frame => frame.method === 'terminal.write').length, reportsBefore, 'the Shift+drag sent no mouse reports');
    await page.keyboard.press('Control+c');
    await page.keyboard.type("printf '\\e[?1000l\\e[?1006lmouse-disabled\\n'");
    await page.keyboard.press('Enter');
    await eventually(() => outputText(terminalId).includes('mouse-disabled\r\n'), { description: 'shell resumed and disabled mouse tracking before reload' });
    checks.push('wheel input reaches a mouse-tracking program as SGR reports and Shift+drag still selects');
    checks.push('typing reaches the shell and its output returns in cursor order under the production CSP');

    // Reload restores only the local tab; native output is read explicitly.
    const before = outputs().length;
    const writesBeforeReload = sent.filter(frame => frame.method === 'terminal.write').length;
    generation++;
    await page.reload();
    await live();
    await eventually(() => outputs().slice(before).some(item => item.id === terminalId && decode(item.data_base64).includes('whip-42')), { description: 'replay after reload' });
    assert.equal(sent.filter(frame => frame.method === 'terminal.open').length, 1, 'reload reads instead of opening another shell');
    assert.equal(sent.filter(frame => frame.method === 'terminal.write').length, writesBeforeReload, 'reload does not replay keystrokes');
    assert.deepEqual(csp, []);
    checks.push('reload reads the same epoch-scoped shell from cursor zero without another open or input replay');

    // The retired attachment selected a push-output sink, not a write authority.
    // Native bounded cursor readers intentionally do not take one another over.
    const second = await fixture.connect(`terminal-reader-${crypto.randomUUID()}`);
    const replay = await second.readTerminal(ref, '0', 32768, deadline());
    assert.equal(replay.terminal.exited, false);
    assert(decode(replay.data_base64).includes('whip-42'));
    await live();
    assert.equal(sent.filter(frame => frame.method === 'terminal.open').length, 1);
    assert.equal((await second.listTerminals(deadline())).items.length, 1);
    await terminalView().click();
    await page.keyboard.type('echo reader-$((40+3))'); await page.keyboard.press('Enter');
    await eventually(() => outputText(terminalId).includes('reader-43'), { description: 'original tab still controls its shell after independent read' });
    checks.push('independent cursor observer reads the same shell without opening or displacing the original tab');
    for (const current of [0, 1]) {
      const pages = outputs().filter(item => item.id === terminalId && item.generation === current);
      assert(pages.length > 0); let cursor = '0';
      for (const item of pages) { assert.equal(item.from, cursor, 'exact native output cursor is contiguous'); cursor = item.next; }
    }

    // Close the tab: the shell ends and the daemon forgets it.
    // The terminal tab's own context menu; the shell may have retitled the tab by now.
    await page.locator(`a[href*="/t/${encodeURIComponent(terminalId)}"]`).first().click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Close tab', exact: true }).click();
    await eventually(() => sent.some(frame => frame.method === 'terminal.close' && frame.params.id === terminalId), { description: 'terminal.close sent' });
    await eventually(async () => {
      try { await client.readTerminal(ref, '0', 32768, deadline()); return false; }
      catch (error) { return error?.kind === 'NOT_FOUND'; }
    }, { description: 'daemon reports the closed terminal as gone' });
    assert.equal(await terminalView().count(), 0, 'terminal view unmounted');
    assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    assert.deepEqual(await fixture.effects(), [], 'Human terminal activity must not dispatch model work');
    checks.push('closing the tab ends the shell');
    results[name] = { browser: await browser.version(), checks, terminalId, writes: sent.filter(frame => frame.method === 'terminal.write').length, outputs: outputs().length, frameCount, retainedBytes, errors, csp };
    console.log(`${name}: ${checks.length} terminal workflows passed`);
  } catch (error) {
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${(await page?.locator('body').innerText().catch(() => '') ?? '').slice(0, 16384)}\n\n${JSON.stringify(errors)}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify({ sent, received: received.slice(-200) }, null, 2));
    throw error;
  } finally { try { await browser?.close(); } finally { await fixture?.close(); } }
}
await writeFile(join(directory, 'terminal-tabs.json'), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));
