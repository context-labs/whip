import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

// Hold only the actual canonical read that confirms this one authored input.
// Upload and admission remain real; cleanup discards held reads and never sends
// a delayed effect. No alternate event stream or content cache is installed.
const directory = process.env.WHIP_ATTACHMENT_CONFIRMATION_RESULTS ?? '/tmp/whip-attachment-confirmation-results';
const manifest = JSON.parse(await readFile(new URL('../renderer-manifest.json', import.meta.url), 'utf8'));
const text = 'Keep this exact preview open after confirmation.';
const bytes = Buffer.from('Verified attachment bytes survive canonical confirmation.');
const results = {};
await mkdir(directory, { recursive: true });
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  assert(['chromium', 'firefox'].includes(name));
  let fixture, browser, page, release;
  let closed = false, released = false, heldBytes = 0;
  const connections = new Set(), closing = new Set(), errors = [], content = [], held = [];
  const recordError = error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  const closeEndpoint = endpoint => {
    const pending = endpoint.close().catch(recordError).finally(() => closing.delete(pending)); closing.add(pending);
  };
  try {
    fixture = await startFixture();
    const client = await fixture.connect(`attachment-confirmation-${crypto.randomUUID()}`);
    const { root } = await fixture.createRoot(client, { title: 'Attachment confirmation' });
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${fixture.info.web}/${path}`); assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256, `Production renderer differs: ${path}`);
    }
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.setDefaultTimeout(15_000);
    page.on('pageerror', recordError);
    await page.exposeFunction('attachmentCSP', recordError);
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.attachmentCSP(event.violatedDirective); }));
    const forwards = [];
    release = () => { released = true; for (const forward of forwards.splice(0)) forward(); };
    await page.routeWebSocket('**/api/v4/ws', socket => {
      if (closed || connections.size >= 128) { if (!closed) recordError(new Error('Attachment proxy connection bound')); closeEndpoint(socket); return; }
      const server = socket.connectToServer(), requests = new Map(); let live = true;
      const retire = () => { if (!live) return; live = false; connections.delete(retire); closeEndpoint(socket); closeEndpoint(server); };
      const guarded = work => { try { if (!closed && live) work(); } catch (error) { recordError(error); retire(); } };
      connections.add(retire); socket.onClose(retire); server.onClose(retire);
      socket.onMessage(data => guarded(() => {
        assert(Buffer.byteLength(data) <= (4 << 20));
        const request = JSON.parse(String(data));
        assert(requests.size < 8); requests.set(request.id, { method: request.method, owner: request.params?.session_id });
        if (['content.put', 'content.get', 'content.read'].includes(request.method)) {
          assert.equal(request.params.session_id, root.id); assert(content.length < 128);
          content.push({ method: request.method, owner: request.params.session_id, reference: request.params.reference_id });
        }
        server.send(data);
      }));
      server.onMessage(data => guarded(() => {
        assert(Buffer.byteLength(data) <= (4 << 20));
        const reply = JSON.parse(String(data)), request = requests.get(reply.id); requests.delete(reply.id);
        if (!released && request?.owner === root.id && ['sessions.observe', 'sessions.history_page'].includes(request.method)
          && reply.result?.messages?.some(message => message.role === 'user' && message.parts.some(part => part.type === 'text' && part.text === text))) {
          assert(held.length < 4); heldBytes += Buffer.byteLength(data); assert(heldBytes <= (1 << 20));
          held.push({ method: request.method, owner: request.owner }); forwards.push(() => guarded(() => socket.send(data))); return;
        }
        socket.send(data);
      }));
    });
    const response = await page.goto(`${fixture.info.web}/h/${fixture.info.runtime_id}/s/${root.id}`);
    assert(response.headers()['content-security-policy'].includes("script-src 'self' 'wasm-unsafe-eval'"));
    const input = page.getByLabel('Message WHIP', { exact: true }), form = input.locator('xpath=ancestor::form');
    await expect(input).toBeVisible();
    await form.locator('input[type=file]').setInputFiles({ name: 'continuity.txt', mimeType: 'text/plain', buffer: bytes });
    await expect(form.getByRole('button', { name: 'Send message', exact: true })).toBeEnabled();
    await input.fill(text); await form.getByRole('button', { name: 'Send message', exact: true }).click();
    await eventually(() => held.length > 0, { description: 'canonical attachment confirmation is held' });
    const row = page.getByRole('region', { name: 'Conversation', exact: true }).locator('[data-message-role="user"]').filter({ hasText: text });
    await row.getByRole('button', { name: 'Preview Attachment 1', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Attachment 1', exact: true });
    await expect(dialog.getByText(bytes.toString(), { exact: true })).toBeVisible();
    await dialog.evaluate(node => { window.confirmationDialog = node; });
    assert.equal(content.filter(record => record.method === 'content.read').length, 1);
    const canonical = await eventually(async () => (await client.session(root.id).history.page({ direction: 'backward', limit: 10 }, deadline())).messages
      .find(message => message.role === 'user' && message.parts.some(part => part.type === 'text' && part.text === text)));
    const refs = canonical.parts.filter(part => part.type === 'content').map(part => part.reference_id);
    assert.equal(refs.length, 1);
    const reference = await client.session(root.id).content.get(refs[0], deadline());
    assert.equal(reference.digest, createHash('sha256').update(bytes).digest('hex'));
    const metadataReads = content.filter(record => record.method === 'content.get').length;
    release();
    await expect(page.locator(`[data-reading-seq="${canonical.sequence}"]`)).toBeVisible();
    await expect(dialog).toBeVisible();
    assert(await dialog.evaluate(node => node === window.confirmationDialog && node.isConnected), 'Confirmation must retain the same open dialog');
    await expect(dialog.getByText(bytes.toString(), { exact: true })).toBeVisible();
    assert.equal(content.filter(record => record.method === 'content.put').length, 1, 'Confirmation must not re-upload');
    assert.equal(content.filter(record => record.method === 'content.read').length, 1, 'Confirmation must not replace the verified preview read');
    assert.equal(content.filter(record => record.method === 'content.get').length, metadataReads, 'Confirmation does not re-fetch attachment metadata');
    await page.screenshot({ path: join(directory, `${name}-confirmed.png`) });
    assert.deepEqual(errors, []);
    results[name] = { rendererDigest: manifest.digest, browser: browser.version(), owner: root.id, sequence: canonical.sequence, held, content, errors };
    await writeFile(join(directory, 'results.json'), JSON.stringify(results, null, 2));
    console.log(`${name}: exact attachment preview survives canonical confirmation without re-upload or repeated reads`);
  } catch (error) {
    if (page) {
      await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n${JSON.stringify({ held, content, errors })}\n${await page.locator('body').innerText().catch(() => '')}`);
    }
    throw error;
  } finally {
    closed = true; release?.(); for (const retire of [...connections]) retire();
    try { await Promise.all(closing); await browser?.close(); } finally { await fixture?.close(); }
  }
}
