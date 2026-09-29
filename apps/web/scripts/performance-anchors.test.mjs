import assert from 'node:assert/strict';
import { test } from 'node:test';
import vm from 'node:vm';
import { installAnchorTrace, finishAnchorTrace } from './performance-anchors.mjs';

function fixture() {
  const listeners = new Map();
  const target = name => ({
    addEventListener(type, callback) { listeners.set(`${name}:${type}`, callback); },
    removeEventListener(type, callback) { if (listeners.get(`${name}:${type}`) === callback) listeners.delete(`${name}:${type}`); },
  });
  const button = { textContent: 'Latest', getClientRects: () => [{}] };
  let rowReads = 0;
  const row = { dataset: { readingId: 'child-message-006' }, getBoundingClientRect() { rowReads++; return { top: 95, bottom: 134 }; } };
  const viewport = { scrollTop: 300, clientHeight: 600, scrollHeight: 4000,
    getBoundingClientRect: () => ({ top: 100, bottom: 700 }),
    querySelectorAll: () => [row],
    closest: () => ({ dataset: { workspaceView: 'view-1' } }),
    parentElement: { querySelectorAll: () => [button] },
  };
  const window = target('window'), document = { ...target('document'), visibilityState: 'visible', hasFocus: () => true, querySelector: () => viewport };
  const context = vm.createContext({ window, document, performance: { now: () => 12 }, RangeError, Error });
  const install = limit => vm.runInContext(`(${installAnchorTrace.toString()})(${JSON.stringify({ limit })})`, context);
  const finish = () => JSON.parse(JSON.stringify(vm.runInContext(`(${finishAnchorTrace.toString()})()`, context)));
  const emit = (type, extra = {}) => listeners.get(`${type === 'popstate' || type === 'pagehide' ? 'window' : 'document'}:${type}`)?.({ type, isTrusted: true, ...extra });
  return { install, finish, emit, viewport, row, listeners, window, document, rowReads: () => rowReads };
}

test('records capped passive scalar evidence and keeps user input out', () => {
  const f = fixture(); f.install(3);
  f.emit('keydown', { key: 'private prompt' }); f.emit('wheel', { deltaY: -1 }); f.emit('scroll');
  const result = f.finish();
  assert.equal(result.samples.length, 3); assert.equal(result.discarded, 1); assert.equal(result.limit, 3);
  assert.equal(result.samples[0].key, null);
  assert.deepEqual(result.samples[0].anchor, { id: 'child-message-006', offset: -5 });
  assert.equal(result.samples[1].wheelY, -1);
  assert.equal(result.samples[2].latestVisible, true);
  assert.equal(result.samples[2].top, 300);
  assert(!JSON.stringify(result).includes('private prompt'));
  assert.match(result.boundary, /Neither.*performance acceptance/);
  assert.equal(f.listeners.size, 0); assert.equal(f.window.__whipAnchorTrace, undefined);
  assert.equal(f.finish(), null);
});

test('tears down on pagehide without losing already captured evidence', () => {
  const f = fixture(); f.install(4); f.emit('popstate');
  assert.equal(f.listeners.size, 9);
  f.emit('pagehide'); assert.equal(f.listeners.size, 0);
  f.emit('scroll');
  assert.deepEqual(f.finish().samples.map(item => item.type), ['start', 'popstate']);
});

test('bounds inspected DOM and IDs and reports incomplete inspection', () => {
  const f = fixture(); f.row.dataset.readingId = 'x'.repeat(513); f.install(2);
  assert.equal(f.finish().samples[0].anchor.id, null);
  f.row.getBoundingClientRect = () => ({ top: -100, bottom: -50 });
  f.viewport.querySelectorAll = () => Array(200).fill(f.row);
  f.viewport.parentElement.querySelectorAll = () => Array(200).fill({ textContent: 'Other' });
  let reads = 0;
  f.row.getBoundingClientRect = () => { reads++; return { top: -100, bottom: -50 }; };
  f.install(2);
  const sample = f.finish().samples[0];
  assert.equal(reads, 128); assert.equal(sample.rowsTruncated, true); assert.equal(sample.controlsTruncated, true); assert.equal(sample.anchor, null);
});

test('rejects invalid or duplicate installations and records absent viewport', () => {
  const f = fixture();
  for (const limit of [0, -1, 513, 1.5]) assert.throws(() => f.install(limit), /Invalid anchor trace limit/);
  f.install(2); assert.throws(() => f.install(2), /already installed/); f.finish();
  f.viewport.parentElement = null;
  f.install(2); f.emit('pointerdown', { pointerType: 'unbounded-private-string' });
  assert.equal(f.finish().samples[1].pointerType, 'other');
  f.document.querySelector = () => null; f.install(2);
  assert.equal(f.finish().samples[0].top, null);
});
