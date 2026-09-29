import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, BrowserProviderClient, selectBrowserProvider } from '../../packages/sdk/dist/index.js';
import { unixSocket, browserProviderSocket } from '../../packages/sdk/dist/node.js';
import { browserProviderDuplex } from '../../packages/sdk/dist/browser.js';
const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
const defer = () => { let resolve; const promise = new Promise(yes => { resolve = yes; }); return { promise, resolve }; };
async function until(read, predicate) { const end = Date.now() + 15000; while (Date.now() < end) { const value = await read(); if (predicate(value)) return value; await new Promise(resolve => setTimeout(resolve, 10)); } throw new Error('Browser provider fixture observation timed out'); }
const jpeg = Uint8Array.from(Buffer.from('/9j/2wCEAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDIBCQkJDAsMGA0NGDIhHCEyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMv/AABEIAAIAAgMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/APn+iiigD//Z', 'base64'));

test('production browser provider peers preserve consent, scoped screenshots and uncertain native effects over Unix and gateway', { timeout: 120_000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-browser-peer-'), binary = join(directory, 'runtime');
  let child, diagnostic = '';
  const stop = async () => { if (!child || child.exitCode !== null || child.signalCode !== null) return; const ended = once(child, 'exit'); child.kill('SIGTERM'); const timer = setTimeout(() => child.kill('SIGKILL'), 5000); try { await ended; } finally { clearTimeout(timer); } };
  t.after(async () => { await stop(); await rm(directory, { recursive: true, force: true }); });
  await promisify(execFile)('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], { timeout: 60000 });
  const start = async () => {
    child = spawn(binary, ['-directory', join(directory, 'state'), '-scripted', '-web', '-web-listen', '127.0.0.1:0'], { stdio: ['ignore', 'pipe', 'pipe'] });
    child.stderr.on('data', data => { diagnostic = (diagnostic + data).slice(-(1 << 20)); });
    return new Promise((resolve, reject) => {
      let text = '';
      const cleanup = () => { clearTimeout(timer); child.stdout.off('data', data); child.off('error', failed); child.off('exit', exited); };
      const failed = error => { cleanup(); reject(error); }, exited = () => failed(new Error('Runtime exited: ' + diagnostic));
      const data = chunk => { text += chunk; const end = text.indexOf('\n'); if (end < 0) return; try { const value = JSON.parse(text.slice(0, end)); cleanup(); resolve(value); } catch (error) { failed(error); } };
      const timer = setTimeout(() => failed(new Error('Runtime startup timed out: ' + diagnostic)), 15000);
      child.stdout.on('data', data); child.on('error', failed); child.on('exit', exited);
    });
  };
  let ready = await start();
  const pin = { expectedRuntimeID: ready.runtime_id, expectedProcessEpoch: ready.process_epoch };
  let client = await Client.connect(unixSocket(ready.socket), { clientID: 'browser-fixture', expectedRuntimeID: ready.runtime_id, ...deadline() });
  const evidence = [];
  for (const [mode, engine] of [['unix', 'starlark'], ['gateway', 'quickjs']]) {
    const { root } = await client.createTree({ engine, definition: client.builtins[0], working_directory: directory, overrides: { modules: ['agents', 'browser'], model: { provider: 'scripted', name: 'scripted', effort: '' }, automatic_title: false }, metadata: { title: null, pinned: false, archived: false } }, 'browser-' + mode, deadline());
    const other = await client.createTree({ engine, definition: client.builtins[0], working_directory: directory, overrides: { modules: ['agents', 'browser'], model: { provider: 'scripted', name: 'scripted', effort: '' }, automatic_title: false }, metadata: { title: null, pinned: false, archived: false } }, 'foreign-' + mode, deadline());
    const transport = mode === 'unix' ? await browserProviderSocket(ready.socket, deadline()) : await browserProviderDuplex(ready.web, { ...pin, ...deadline() });
    const peer = await BrowserProviderClient.connect(transport, { ...pin, ...deadline() });
    const offer = { root_id: root.id, version: 2, desktop_id: 'fake-desktop', window_id: 'fake-window', offer_revision: 'offer', create_profile_id: 'profile', offered_tabs: [{ tab_id: 'human-tab', tab_generation: 'generation', profile_id: 'profile', document_revision: 'doc', url: 'about:blank', title: 'Human-owned page' }, { tab_id: 'second-tab', tab_generation: 'generation-2', profile_id: 'profile', document_revision: 'doc', url: 'about:blank', title: 'Second human-owned page' }], offered_preview_hosts: [] };
    const commands = [], pending = new Map(), errors = []; let drop = false, dropTransfer = false, effects = 0, transfers = 0;
    const bridge = {
      async select() {}, onEvent() { return () => {}; }, async retire() {}, async release() { for (const wait of pending.values()) wait.resolve(); },
      cancel(value) { pending.get(value.command_id)?.resolve(); },
      async inventory(request) { return { request_id: request.request_id, root_id: request.root_id, provider_epoch: request.provider_epoch, tabs: request.tabs.map(tab => ({ ...tab, document_revision: 'doc', url: 'about:blank', title: 'Human-owned page', state: 'available', requestable: true })) }; },
      async dispatch(command) {
        const operation = await client.call('operations.get', { operation_id: command.operation_id }, deadline());
        assert.equal(operation.state, 'dispatched', 'native effect preceded durable authority'); commands.push(command);
        const result = { command_id: command.command_id, root_id: command.root_id, provider_epoch: command.provider_epoch, attachment_generation: command.scope.attachment_generation, document_revision: 'doc', url: 'about:blank', title: 'Human-owned page', result: {} };
        if (command.kind === 'transfer') { transfers++; if (dropTransfer) { const wait = defer(); pending.set(command.command_id, wait); await wait.promise; pending.delete(command.command_id); } }
        if (command.kind === 'cdp') switch (command.arguments.method) {
          case 'Page.getFrameTree': result.result = { frameTree: { frame: { id: 'frame', url: 'about:blank', securityOrigin: 'null', mimeType: 'text/html' } } }; break;
          case 'Runtime.evaluate': result.result = { result: { type: 'string', value: '{"url":"about:blank","title":"page","w":2,"h":2}' } }; break;
          case 'Page.getLayoutMetrics': result.result = { cssLayoutViewport: { clientWidth: 2, clientHeight: 2 } }; break;
          case 'Page.captureScreenshot': result.screenshotBytes = jpeg; break;
          case 'Input.insertText': effects++; if (drop) { const wait = defer(); pending.set(command.command_id, wait); await wait.promise; pending.delete(command.command_id); } break;
        }
        return result;
      },
    };
    const selection = await selectBrowserProvider(peer, offer, bridge, { onError: error => errors.push(error) });
    t.after(() => selection.release());
    assert.equal((await client.browserTabs(root.id, deadline())).tabs.length, 2);
    assert.equal((await client.browserAttachments(root.id, deadline())).attachments.length, 0);
    assert.equal(commands.length, 0);
    const invoke = (name, args, key, owner = root.id) => client.callTool(owner, { module: 'browser', name, arguments_base64: Buffer.from(JSON.stringify(args)).toString('base64') }, mode + '-' + key, deadline());
    await invoke('attach', { tab_id: 'human-tab' }, 'attach');
    const approvals = await until(() => client.call('permissions.list', { session_id: root.id, limit: 10 }, deadline()), value => value.items.some(item => item.state === 'pending'));
    assert.equal(commands.length, 0);
    await client.call('permissions.resolve', { operation_id: approvals.items.find(item => item.state === 'pending').operation_id, approved: true }, deadline());
    assert.equal((await client.wait(mode + '-attach', deadline())).turn.state, 'succeeded');
    const attachment = (await client.browserAttachments(root.id, deadline())).attachments[0]; assert.ok(attachment);
    await assert.rejects(peer.screenshotChunk({ command_id: 'not-pending', root_id: root.id, provider_epoch: selection.provider.provider_epoch, attachment_generation: attachment.scope.attachment_generation, offset: '0', data_base64: Buffer.from(jpeg).toString('base64') }, deadline()));
    await invoke('run', { attachment_id: attachment.scope.attachment_id, code: 'screenshot()' }, 'image');
    const imageTurn = await client.wait(mode + '-image', deadline()); assert.equal(imageTurn.turn.state, 'succeeded', imageTurn.turn.failure);
    const imageOp = (await client.call('turns.operations', { turn_id: imageTurn.turn.id, limit: 10 }, deadline())).items[0];
    assert.equal(imageOp.result.content_references.length, 1);
    const reference_id = imageOp.result.content_references[0], content = await client.call('content.read', { session_id: root.id, reference_id }, deadline());
    assert.equal(content.reference.session_id, root.id); assert.equal(content.reference.media_type, 'image/jpeg'); assert.deepEqual(Buffer.from(content.data_base64, 'base64'), Buffer.from(jpeg));
    await assert.rejects(client.call('content.read', { session_id: other.root.id, reference_id }, deadline()), error => error.kind === 'NOT_FOUND');
    const transferParams = { identity: client.identity(mode + '-transfer'), parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'Use the transferred browser' }], grant_ids: null, browser_attachments: [attachment.scope.attachment_id] };
    const transfer = client.command('sessions.spawn', transferParams);
    const creating = transfer.send();
    const transferApproval = await until(() => client.call('permissions.list', { session_id: root.id, limit: 10 }, deadline()), value => value.items.some(item => item.state === 'pending'));
    assert.equal(transfers, 0);
    await assert.rejects(transfer.check(deadline()), error => error.kind === 'BUSY');
    await client.call('permissions.resolve', { operation_id: transferApproval.items.find(item => item.state === 'pending').operation_id, approved: true }, deadline());
    const created = await creating; assert.ok(created.session); assert.equal(created.session.parent_id, root.id);
    await client.wait(mode + '-transfer', deadline());
    assert.equal(transfers, 1);
    assert.equal((await transfer.check(deadline())).state, 'found');
    assert.equal((await transfer.retry(deadline())).session.id, created.session.id);
    const controlled = (await client.browserAttachments(created.session.id, deadline())).attachments[0];
    assert.equal(controlled.scope.control_lineage, attachment.scope.control_lineage);
    assert.notEqual(controlled.scope.attachment_generation, attachment.scope.attachment_generation);
    await invoke('run', { attachment_id: controlled.scope.attachment_id, code: 'screenshot()' }, 'child-image', created.session.id);
    const childImage = await client.wait(mode + '-child-image', deadline()); assert.equal(childImage.turn.state, 'succeeded');
    const childImageOp = (await client.call('turns.operations', { turn_id: childImage.turn.id, limit: 10 }, deadline())).items[0];
    const childReference = childImageOp.result.content_references[0];
    assert.equal((await client.call('content.read', { session_id: created.session.id, reference_id: childReference }, deadline())).reference.session_id, created.session.id);
    await assert.rejects(client.call('content.read', { session_id: root.id, reference_id: childReference }, deadline()), error => error.kind === 'NOT_FOUND');
    await client.call('permissions.set_mode', { edit_id: mode + '-automatic', session_id: root.id, expected_revision: '1', mode: 'automatic' }, deadline());
    await invoke('attach', { tab_id: 'second-tab' }, 'second-attach'); await client.wait(mode + '-second-attach', deadline());
    const second = (await client.browserAttachments(root.id, deadline())).attachments.find(item => item.scope.tab_id === 'second-tab'); assert.ok(second);
    drop = true; const lostArgs = { attachment_id: controlled.scope.attachment_id, code: 'type("once")' };
    await invoke('run', lostArgs, 'lost', created.session.id); await until(async () => effects, value => value === 1);
    dropTransfer = true;
    const lostTransferParams = { ...transferParams, identity: client.identity(mode + '-lost-transfer'), browser_attachments: [second.scope.attachment_id] };
    const lostTransfer = client.command('sessions.spawn', lostTransferParams);
    const lostTransferResult = lostTransfer.send().then(() => { throw new Error('lost native transfer ACK succeeded'); }, error => error);
    await until(async () => transfers, value => value === 2);
    await assert.rejects(lostTransfer.check(deadline()), error => error.kind === 'BUSY');
    await selection.release();
    const lost = await client.wait(mode + '-lost', deadline()); assert.equal(lost.turn.state, 'failed');
    const lostOp = (await client.call('turns.operations', { turn_id: lost.turn.id, limit: 10 }, deadline())).items[0]; assert.equal(lostOp.state, 'uncertain');
    assert.equal((await invoke('run', lostArgs, 'lost', created.session.id)).input.id, lost.input.id); assert.equal(effects, 1);
    assert.equal((await client.browserAttachments(root.id, deadline())).attachments.length, 0);
    assert.equal((await lostTransferResult).kind, 'TRANSFER_UNCERTAIN');
    await assert.rejects(lostTransfer.check(deadline()), error => error.kind === 'TRANSFER_UNCERTAIN');
    await assert.rejects(lostTransfer.retry(deadline()), error => error.kind === 'TRANSFER_UNCERTAIN');
    assert.equal(transfers, 2);
    const sessions = await client.call('sessions.list', { tree_id: created.session.tree_id, limit: 100 }, deadline()); assert.equal(sessions.items.length, 2);
    evidence.push({ root, child: created.session, transferParams, lostTransferParams, childReference, attachment, reference_id, image: imageTurn.input.id, lost: lost.input.id, lostArgs, mode });
    assert.equal(errors.length, 0, errors.map(error => error.message).join('\n'));
  }
  const previous = ready; await stop(); ready = await start();
  assert.equal(ready.runtime_id, previous.runtime_id); assert.notEqual(ready.process_epoch, previous.process_epoch);
  await assert.rejects(BrowserProviderClient.connect(await browserProviderSocket(ready.socket, deadline()), { ...pin, ...deadline() }), error => error.kind === 'IDENTITY');
  client = await Client.connect(unixSocket(ready.socket), { clientID: 'browser-fixture', expectedRuntimeID: ready.runtime_id, ...deadline() });
  for (const item of evidence) {
    assert.equal((await client.browserAttachments(item.root.id, deadline())).attachments.length, 0);
    assert.equal((await client.browserAttachments(item.child.id, deadline())).attachments.length, 0);
    assert.equal((await client.call('sessions.spawn', item.transferParams, deadline())).session.id, item.child.id);
    await assert.rejects(client.call('sessions.spawn', item.lostTransferParams, deadline()), error => error.kind === 'TRANSFER_UNCERTAIN');
    assert.equal((await client.call('content.read', { session_id: item.child.id, reference_id: item.childReference }, deadline())).reference.session_id, item.child.id);
    assert.equal((await client.recover(item.mode + '-lost', deadline())).input.id, item.lost);
    assert.equal((await client.call('content.read', { session_id: item.root.id, reference_id: item.reference_id }, deadline())).reference.media_type, 'image/jpeg');
  }
  await stop(); assert.equal(child.exitCode, 0, diagnostic);
});
