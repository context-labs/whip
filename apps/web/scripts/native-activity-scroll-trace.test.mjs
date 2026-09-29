import assert from 'node:assert/strict';
import { test } from 'node:test';
import vm from 'node:vm';
import { finishActivityScrollTrace, installActivityScrollTrace } from './native-activity-scroll-trace.mjs';

function fixture() {
  const listeners = new Map(), frames = new Map(), timers = new Map();
  let next = 0, resize, observed = 0, disconnected = 0;
  class Element {
    constructor(name) { this.name = name; this.tagName = 'P'; this.scrollTop = 0; this.clientHeight = 100; this.scrollHeight = 100; }
    addEventListener(type, callback, options) { listeners.set(`${this.name}:${type}:${!!options?.capture}`, callback); }
    removeEventListener(type, callback, capture = false) { const key = `${this.name}:${type}:${capture}`; if (listeners.get(key) === callback) listeners.delete(key); }
    contains(node) { return node === this || node?.parentElement === this; }
  }
  const root = new Element('reader'), document = new Element('document'), window = new Element('window');
  const child = new Element('child'); child.parentElement = root;
  Object.assign(root, { isConnected: true, scrollTop: 1000, clientHeight: 600, scrollHeight: 1600, clientWidth: 800,
    firstElementChild: { firstElementChild: { getBoundingClientRect: () => ({ height: 1540 }) } },
    parentElement: { querySelectorAll: () => [{ textContent: 'Latest', getClientRects: () => [{}] }] },
  });
  document.querySelector = () => root;
  const context = vm.createContext({ window, document, Element, getComputedStyle: () => ({ overflowY: 'auto', overscrollBehaviorY: 'contain' }),
    performance: { now: () => 12 }, RangeError, Error,
    ResizeObserver: class { constructor(callback) { resize = callback; } observe() { observed++; } disconnect() { disconnected++; } },
    requestAnimationFrame(callback) { frames.set(++next, callback); return next; }, cancelAnimationFrame(id) { frames.delete(id); },
    setTimeout(callback, delay) { assert.equal(delay, 10000); timers.set(++next, callback); return next; }, clearTimeout(id) { timers.delete(id); },
  });
  const install = limit => vm.runInContext(`(${installActivityScrollTrace.toString()})(${JSON.stringify({ limit })})`, context);
  const finish = () => JSON.parse(JSON.stringify(vm.runInContext(`(${finishActivityScrollTrace.toString()})()`, context)));
  const emit = (name, type, extra = {}, capture = true) => listeners.get(`${name}:${type}:${capture}`)?.({ type, target: child, isTrusted: true, defaultPrevented: false, ...extra });
  const frame = () => { const callbacks = [...frames.values()]; frames.clear(); for (const callback of callbacks) callback(); };
  const timeout = () => { for (const callback of [...timers.values()]) callback(); };
  return { install, finish, emit, frame, timeout, resize: () => resize(), root, child, window, document, listeners, frames, timers,
    observed: () => observed, disconnected: () => disconnected };
}

test('records wheel propagation and later geometry without retaining content or nodes', () => {
  const f = fixture(); f.install(32);
  f.child.textContent = 'private message';
  f.emit('reader', 'wheel', { deltaY: -12, deltaMode: 0 });
  f.emit('document', 'wheel', { deltaY: -12, deltaMode: 0, defaultPrevented: true }, false);
  f.root.scrollTop = 988; f.emit('reader', 'scroll', { target: f.root }); f.frame();
  f.root.scrollHeight = 1650; f.resize();
  const result = f.finish();
  assert.deepEqual(result.samples.map(sample => sample.type), ['start', 'wheel', 'wheel-bubbled', 'scroll', 'frame', 'resize', 'finish']);
  assert.equal(result.samples[1].wheelY, -12); assert.equal(result.samples[1].wheelMode, 0);
  assert.equal(result.samples[1].prevented, false); assert.equal(result.samples[2].prevented, true);
  assert.deepEqual(result.samples[1].nested, [{ tag: 'P', top: 0, height: 100, total: 100, scrollable: true, contained: true }]);
  assert.equal(result.samples[3].top, 988); assert.equal(result.samples[3].target, 'reader');
  assert.equal(result.samples[5].total, 1650); assert.equal(result.samples[5].latest, true);
  assert(!JSON.stringify(result).includes('private message')); assert.match(result.boundary, /not performance acceptance/);
  assert.equal(result.ended, 'finished'); assert.equal(f.observed(), 2); assert.equal(f.disconnected(), 1);
  assert.equal(f.listeners.size + f.frames.size + f.timers.size, 0); assert.equal(f.window.__whipActivityScrollTrace, undefined);
  assert.equal(f.finish(), null);
});

test('bounds samples, examined controls, ancestors, and frames even for repeated wheels', () => {
  const f = fixture(); f.install(4);
  f.root.parentElement.querySelectorAll = () => Array(200).fill({ textContent: 'Other' });
  let node = f.child;
  for (let index = 0; index < 12; index++) { const parent = Object.create(f.child); parent.parentElement = f.root; node.parentElement = parent; node = parent; }
  for (let index = 0; index < 20; index++) { f.emit('reader', 'wheel', { deltaY: -12 }); f.frame(); }
  assert.equal(f.frames.size, 0);
  const result = f.finish();
  assert.equal(result.samples.length, 4); assert.equal(result.discarded, 26);
  assert.equal(result.samples[2].nested.length, 8); assert.equal(result.samples[2].nestedTruncated, true);
  assert.equal(result.samples[3].controlsTruncated, true); assert.equal(result.limit, 4);
});

test('closes on unmount, pagehide, or deadline and ignores already queued callbacks', () => {
  for (const reason of ['unmounted', 'pagehide', 'deadline']) {
    const f = fixture(); f.install(8); f.emit('reader', 'wheel', { deltaY: -12 });
    if (reason === 'unmounted') { f.root.isConnected = false; f.frame(); }
    if (reason === 'pagehide') f.emit('window', 'pagehide', {}, false);
    if (reason === 'deadline') f.timeout();
    assert.equal(f.listeners.size + f.frames.size + f.timers.size, 0);
    f.resize(); f.frame();
    const result = f.finish(); assert.equal(result.ended, reason); assert.equal(result.samples.length, 2);
    assert.equal(f.disconnected(), 1);
  }
});

test('rejects invalid or duplicate installation and missing owner', () => {
  const f = fixture();
  for (const limit of [0, -1, 513, 1.5]) assert.throws(() => f.install(limit), /Invalid activity trace limit/);
  f.install(8); assert.throws(() => f.install(8), /already installed/); f.finish();
  f.document.querySelector = () => null; assert.throws(() => f.install(8), /not mounted/);
  assert.equal(f.listeners.size + f.frames.size + f.timers.size, 0);
});
