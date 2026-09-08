import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import Module from 'node:module';
import test, { type TestContext } from 'node:test';
import type { DesktopEvent } from '@whip/app/desktop-bridge';

class UpdaterFixture extends EventEmitter {
  feed?: unknown;
  checks = 0;
  installs = 0;
  checkFailure?: Error;
  installFailure?: Error;
  setFeedURL(value: unknown) { this.feed = value; }
  checkForUpdates() {
    this.checks++;
    if (this.checkFailure) throw this.checkFailure;
    this.emit('checking-for-update');
  }
  quitAndInstall() {
    this.installs++;
    if (this.installFailure) throw this.installFailure;
    this.emit('before-quit-for-update');
  }
}
const updater = new UpdaterFixture();
const modules = (async () => {
  const loader = Module as unknown as { _load(request: string, ...args: unknown[]): unknown };
  const original = loader._load;
  loader._load = function (request, ...args) {
    return request === 'electron' ? { autoUpdater: updater } : Reflect.apply(original, this, [request, ...args]);
  };
  try { return await import('../src/updates'); }
  finally { loader._load = original; }
})();

async function fixture(t: TestContext, requestInstall: () => Promise<void> = async () => {}) {
  const { DesktopUpdates } = await modules;
  updater.removeAllListeners(); updater.checks = 0; updater.installs = 0;
  delete updater.checkFailure; delete updater.installFailure;
  const events: DesktopEvent[] = [];
  const quitting: boolean[] = [];
  const updates = new DesktopUpdates({ channel: 'stable', updateURL: 'https://example.test/stable/arm64/RELEASES.json' },
    event => events.push(event), requestInstall, value => quitting.push(value));
  t.after(() => { updates.dispose(); updater.removeAllListeners(); });
  return { updates, events, quitting };
}
function downloaded() { updater.emit('update-downloaded', {}, 'Fixture notes', '2.0.0'); }

test('updater accepts only bounded HTTPS release feeds without embedded credentials', async () => {
  const { readDesktopConfig } = await modules;
  for (const url of ['http://example.test/RELEASES.json', 'https://user:secret@example.test/RELEASES.json',
    'https://example.test/RELEASES.json?token=x', 'https://example.test/RELEASES.json#fragment',
    'https://example.test/update.zip', 'x'.repeat(4097)])
    assert.throws(() => readDesktopConfig({ channel: 'stable', updateURL: url }));
  for (const value of [null, [], {}, { channel: 'nightly' }, { channel: 'beta', updateURL: 1 }])
    assert.throws(() => readDesktopConfig(value));
  assert.deepEqual(readDesktopConfig({ channel: 'beta' }), { channel: 'beta' });
  assert.deepEqual(readDesktopConfig({ channel: 'stable', updateURL: 'https://example.test/stable/RELEASES.json' }),
    { channel: 'stable', updateURL: 'https://example.test/stable/RELEASES.json' });
});

test('updater deduplicates active checks, permits retry after failure, and replays a downloaded update', async t => {
  const f = await fixture(t);
  await f.updates.check(); await f.updates.check(); assert.equal(updater.checks, 1);
  updater.emit('error', new Error('Fixture network failure'));
  await f.updates.check(); assert.equal(updater.checks, 2);
  downloaded(); await f.updates.check(); assert.equal(updater.checks, 2);
  assert.deepEqual(f.events.at(-1), { kind: 'update', state: 'downloaded', version: '2.0.0' });
  f.events.length = 0; f.updates.ready();
  assert.deepEqual(f.events, [{ kind: 'update', state: 'downloaded', version: '2.0.0' }]);
});

test('updater does not install before download or while the close handshake is unresolved', async t => {
  let allowClose!: () => void;
  const close = new Promise<void>(resolve => { allowClose = resolve; });
  let requests = 0;
  const f = await fixture(t, async () => { requests++; await close; });
  await assert.rejects(f.updates.install(), /No update/);
  assert.throws(() => f.updates.quitAndInstall(), /No update/);
  downloaded();
  const installing = f.updates.install();
  assert.equal(requests, 1); assert.equal(updater.installs, 0);
  allowClose(); await installing;
  assert.equal(updater.installs, 0, 'Close cancellation/approval is owned by main, not inferred by the updater');
  f.updates.quitAndInstall();
  assert.equal(updater.installs, 1); assert.deepEqual(f.quitting, [true]);
});

test('updater ignores an unsolicited quit event and rolls back approval on synchronous install failure', async t => {
  const f = await fixture(t);
  updater.emit('before-quit-for-update'); assert.deepEqual(f.quitting, []);
  downloaded(); updater.installFailure = new Error('Fixture install failure');
  assert.throws(() => f.updates.quitAndInstall(), /Fixture install failure/);
  assert.deepEqual(f.quitting, [false]);
  updater.emit('before-quit-for-update'); assert.deepEqual(f.quitting, [false]);
  delete updater.installFailure;
  f.updates.quitAndInstall(); assert.deepEqual(f.quitting, [false, true]);
});

test('updater revokes quit approval when installation fails asynchronously', async t => {
  const f = await fixture(t);
  downloaded(); f.updates.quitAndInstall(); assert.deepEqual(f.quitting, [true]);
  updater.emit('error', new Error('Fixture signature failure'));
  assert.deepEqual(f.quitting, [true, false]);
  assert.deepEqual(f.events.at(-1), { kind: 'update', state: 'error', error: 'Fixture signature failure' });
  updater.emit('before-quit-for-update'); assert.deepEqual(f.quitting, [true, false]);
  await assert.rejects(f.updates.install(), /No update/);
  await f.updates.check(); assert.equal(updater.checks, 1);
  downloaded();
  assert.deepEqual(f.events.at(-1), { kind: 'update', state: 'downloaded', version: '2.0.0' });
});

test('updater disposal removes its native listeners without removing unrelated subscribers', async t => {
  const f = await fixture(t);
  let externalErrors = 0;
  const external = () => { externalErrors++; };
  updater.on('error', external);
  downloaded(); const count = f.events.length;
  f.updates.dispose();
  updater.emit('error', new Error('After disposal'));
  updater.emit('before-quit-for-update'); updater.emit('update-downloaded', {}, '', '3.0.0');
  assert.equal(f.events.length, count); assert.deepEqual(f.quitting, []); assert.equal(externalErrors, 1);
  assert.deepEqual(updater.eventNames(), ['error']);
});
