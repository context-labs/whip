import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import * as fs from 'node:fs/promises';
import Module from 'node:module';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import type { BrowserWindow } from 'electron';
import type { DesktopEvent } from '@whip/app/desktop-bridge';

function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
function gate() { return { entered: deferred(), resume: deferred() }; }
const hooks: { open?: ReturnType<typeof gate>; write?: ReturnType<typeof gate>; sync?: ReturnType<typeof gate> } = {};
const opened: fs.FileHandle[] = [];
let saveResult: Promise<{ canceled: boolean; filePath?: string }>;
let destroyed = false;
let focused = false;
let showCount = 0;
let focusCount = 0;
const notices: StubNotification[] = [];
class StubNotification extends EventEmitter {
  static isSupported() { return true; }
  closed = false;
  shown = false;
  constructor(readonly options: { title: string; body: string }) { super(); notices.push(this); }
  show() { this.shown = true; }
  // Intentionally no close event: native platforms need not deliver it before
  // another IPC request, so ownership bounds cannot depend on its timing.
  close() { this.closed = true; }
}
const electron = {
  dialog: { showSaveDialog: async () => saveResult },
  clipboard: { writeText() {} },
  shell: { openExternal: async () => {} },
  Notification: StubNotification,
};

// The normal runner bundles native sources and keeps Electron external. Load
// those modules with a process-local Electron adapter and real, gated file I/O.
const modules = (async () => {
  const loader = Module as unknown as { _load(request: string, ...args: unknown[]): unknown };
  const original = loader._load;
  loader._load = function (request, ...args) {
    if (request === 'electron') return electron;
    if (request === 'node:fs/promises') return { ...fs, open: async (...parameters: Parameters<typeof fs.open>) => {
      const file = await fs.open(...parameters); opened.push(file);
      if (hooks.open) { hooks.open.entered.resolve(); await hooks.open.resume.promise; }
      return new Proxy(file, { get(target, key) {
        const value = Reflect.get(target, key);
        if (key === 'write' || key === 'sync') return async (...values: unknown[]) => {
          const pause = hooks[key];
          if (pause) { pause.entered.resolve(); await pause.resume.promise; }
          return Reflect.apply(value, target, values);
        };
        return typeof value === 'function' ? value.bind(target) : value;
      } });
    } };
    return Reflect.apply(original, this, [request, ...args]);
  };
  try { return await import('../src/native'); }
  finally { loader._load = original; }
})();

async function fixture(t: TestContext) {
  const { NativeEffects } = await modules;
  const directory = await fs.mkdtemp('/tmp/whip-native-test-');
  const target = path.join(directory, 'selected.txt');
  await fs.writeFile(target, 'original');
  saveResult = Promise.resolve({ canceled: false, filePath: target });
  destroyed = false; focused = false; showCount = 0; focusCount = 0; notices.length = 0; opened.length = 0;
  delete hooks.open; delete hooks.write; delete hooks.sync;
  const events: DesktopEvent[] = [];
  const window = { isDestroyed: () => destroyed, isFocused: () => focused,
    show: () => { showCount++; }, focus: () => { focusCount++; } } as unknown as BrowserWindow;
  const native = new NativeEffects(window, event => events.push(event));
  t.after(async () => {
    for (const pause of Object.values(hooks)) pause?.resume.resolve();
    await native.dispose();
    for (const file of opened) await file.close().catch(() => {});
    await fs.rm(directory, { recursive: true, force: true });
  });
  return { native, directory, target, events };
}

test('native save commits exact ordered bytes only after completion and removes its temporary file', async t => {
  const f = await fixture(t);
  const id = await f.native.beginSave('../selected.txt', 'text/plain', 5);
  assert.ok(id);
  assert.equal(await fs.readFile(f.target, 'utf8'), 'original');
  await assert.rejects(f.native.writeSave(id, 1, Buffer.from('bad')), /chunk/);
  await assert.rejects(f.native.finishSave(id), /incomplete/);
  await f.native.writeSave(id, 0, Buffer.from('hello'));
  assert.equal(await fs.readFile(f.target, 'utf8'), 'original');
  await f.native.finishSave(id);
  assert.equal(await fs.readFile(f.target, 'utf8'), 'hello');
  assert.deepEqual(await fs.readdir(f.directory), ['selected.txt']);
  await assert.rejects(f.native.writeSave(id, 5, new Uint8Array()), /chunk/);
});

test('native disposal invalidates an unresolved save dialog', async t => {
  const f = await fixture(t);
  const dialog = deferred<{ canceled: boolean; filePath: string }>();
  saveResult = dialog.promise;
  const pending = f.native.beginSave('selected.txt', 'text/plain', 1);
  await f.native.dispose();
  dialog.resolve({ canceled: false, filePath: f.target });
  assert.equal(await pending, undefined);
  assert.equal(opened.length, 0);
  assert.deepEqual(await fs.readdir(f.directory), ['selected.txt']);
});

test('native disposal closes and removes a save file whose open resolves after disposal', async t => {
  const f = await fixture(t);
  const pause = hooks.open = gate();
  const pending = f.native.beginSave('selected.txt', 'text/plain', 1);
  await pause.entered.promise;
  await f.native.dispose();
  pause.resume.resolve();
  assert.equal(await pending, undefined);
  assert.equal(opened[0]!.fd, -1);
  assert.equal(await fs.readFile(f.target, 'utf8'), 'original');
  assert.deepEqual(await fs.readdir(f.directory), ['selected.txt']);
});

test('native disposal waits for an in-flight write and revokes permission to finalize it', async t => {
  const f = await fixture(t);
  const id = await f.native.beginSave('selected.txt', 'text/plain', 5); assert.ok(id);
  const pause = hooks.write = gate();
  const writing = f.native.writeSave(id, 0, Buffer.from('hello'));
  await pause.entered.promise;
  let disposed = false;
  const disposing = f.native.dispose().then(() => { disposed = true; });
  await Promise.resolve(); assert.equal(disposed, false);
  await assert.rejects(f.native.finishSave(id), /incomplete/);
  pause.resume.resolve(); await writing; await disposing;
  assert.equal(opened[0]!.fd, -1);
  assert.equal(await fs.readFile(f.target, 'utf8'), 'original');
  assert.deepEqual(await fs.readdir(f.directory), ['selected.txt']);
});

test('native disposal cancels a pending final flush before it can replace the selected file', async t => {
  const f = await fixture(t);
  const id = await f.native.beginSave('selected.txt', 'text/plain', 5); assert.ok(id);
  await f.native.writeSave(id, 0, Buffer.from('hello'));
  const pause = hooks.sync = gate();
  const finishing = assert.rejects(f.native.finishSave(id), /cancelled/);
  await pause.entered.promise;
  const disposing = f.native.dispose();
  pause.resume.resolve(); await finishing; await disposing;
  assert.equal(await fs.readFile(f.target, 'utf8'), 'original');
  assert.deepEqual(await fs.readdir(f.directory), ['selected.txt']);
});

test('native disposal revokes every save before waiting for the first pending operation', async t => {
  const f = await fixture(t);
  const first = await f.native.beginSave('selected.txt', 'text/plain', 5); assert.ok(first);
  const secondTarget = path.join(f.directory, 'second.txt');
  await fs.writeFile(secondTarget, 'original second');
  saveResult = Promise.resolve({ canceled: false, filePath: secondTarget });
  const second = await f.native.beginSave('second.txt', 'text/plain', 5); assert.ok(second);
  await f.native.writeSave(second, 0, Buffer.from('hello'));
  const pause = hooks.write = gate();
  const writing = f.native.writeSave(first, 0, Buffer.from('hello'));
  await pause.entered.promise;
  const disposing = f.native.dispose();
  await assert.rejects(f.native.finishSave(second), /incomplete|cancelled/);
  pause.resume.resolve(); await writing; await disposing;
  assert.equal(await fs.readFile(secondTarget, 'utf8'), 'original second');
  assert.deepEqual((await fs.readdir(f.directory)).sort(), ['second.txt', 'selected.txt']);
});

test('native notification throttling is per session and bounds ownership without close callbacks', async t => {
  const f = await fixture(t);
  const notification = (index: number) => ({ id: `session-${index}`, title: 'Needs attention', body: 'A fixture session', path: `/h/fixture/s/${index}` });
  await f.native.notify(notification(0)); await f.native.notify(notification(0)); await f.native.notify(notification(1));
  assert.equal(notices.length, 2);
  for (let index = 2; index < 34; index++) await f.native.notify(notification(index));
  assert.equal(notices.filter(notice => !notice.closed).length, 32);
  notices.at(-1)!.emit('click');
  assert.equal(showCount, 1); assert.equal(focusCount, 1);
  assert.deepEqual(f.events, [{ kind: 'navigate', path: '/h/fixture/s/33' }]);
  await f.native.dispose();
  assert.equal(notices.filter(notice => !notice.closed).length, 0);
});

test('native effects reject privileged external schemes and malformed notification routes', async () => {
  const { externalURL, navigationPath } = await modules;
  for (const value of ['file:///etc/passwd', 'javascript:alert(1)', 'data:text/html,x', 'https://user:secret@example.test/', 'whip-app://app/'])
    assert.throws(() => externalURL(value));
  for (const value of ['https://example.test/a', 'mailto:person@example.test']) assert.equal(externalURL(value), value);
  for (const value of ['/h/fixture/s/session', '/h/fixture/s/session?view=chat']) assert.doesNotThrow(() => navigationPath(value));
  for (const value of ['https://example.test/', '//example.test/h/x/s/y', '/h/x/s/y#fragment', '/h/x/s/a\\b', '/h/x/s/a\n'])
    assert.throws(() => navigationPath(value));
});
