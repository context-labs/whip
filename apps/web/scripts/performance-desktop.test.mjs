import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { JSDOM } from 'jsdom';
import { assertDesktopViewport, captureDesktopMemoryPhase, closeDesktopProcess, desktopDOMMemoryCounts, setDesktopViewport, summaryPollEvidence } from './performance-desktop.mjs';
const requests = (...sets) => sets.map(rootIDs => ({ rootIDs, roots: rootIDs.length }));
test('summary poll evidence distinguishes exact scopes with equal counts', () => {
 assert.deepEqual(summaryPollEvidence(requests(['a','b'],['b','a'],['b','c'],['c','b']), ['a','b'], ['b','c']),
  { sameRootSet: false, combinedPolls: 4, tabPolls: 2, sidebarPolls: 2 });
});
test('identical tab and sidebar scopes retain honest combined evidence', () => {
 assert.deepEqual(summaryPollEvidence(requests(...Array(6).fill(['a','b'])), ['a','b'], ['b','a']),
  { sameRootSet: true, combinedPolls: 6, tabPolls: null, sidebarPolls: null });
 assert.throws(() => summaryPollEvidence(requests(...Array(9).fill(['a'])), ['a'], ['a']), /bounded aggregate/);
});
test('missing owners, unexpected roots and per-tab polling fail', () => {
 assert.throws(() => summaryPollEvidence(requests(['a'],['a']), ['a'], ['b']), /visible-sidebar/);
 assert.throws(() => summaryPollEvidence(requests(['a'],['b'],['c']), ['a'], ['b']), /Unexpected summary/);
 assert.throws(() => summaryPollEvidence(requests(...Array(9).fill(['a']), ['b'], ['b']), ['a'], ['b']), /shared tab/);
});
test('an unresponsive owned main process is killed and its pending inspector call is joined', async () => {
 const child = spawn(process.execPath, ['-e', 'process.on("SIGTERM",()=>{}); console.log("ready"); setInterval(()=>{},1000)'], { stdio: ['ignore', 'pipe', 'pipe'] });
 let joined = false;
 try {
  await once(child.stdout, 'data');
  await closeDesktopProcess({ process: () => child, evaluate: () => new Promise((_, reject) => {
   child.once('exit', () => { joined = true; reject(new Error('Target closed')); });
  }) });
  assert.equal(child.signalCode, 'SIGKILL'); assert.equal(joined, true);
  assert.throws(() => process.kill(child.pid, 0), { code: 'ESRCH' });
 } finally {
  if (child.exitCode === null && child.signalCode === null) { const exited = once(child, 'exit'); child.kill('SIGKILL'); await exited; }
 }
});

function viewportHost({ resize = true } = {}) {
 const content = { x: 50, y: 50, width: 1200, height: 800 };
 const document = { width: 1200, height: 800, visibility: 'visible', bounds: { x: 0, y: 0, width: 1200, height: 800 },
  composer: { x: 445, y: 671, width: 790, height: 220 }, composerFocused: true };
 const native = { content, bounds: content, visible: true, minimized: false, focused: true };
 let disposed = 0;
 const host = { electron: {
  browserWindow: async () => ({ evaluate: async (fn, size) => size ? fn({ setContentSize(width, height) {
   if (resize) Object.assign(content, { width, height });
  }, center() {} }, size) : native, dispose: async () => { disposed++; } }),
  evaluate: async () => ({ x: 0, y: 0, width: 2560, height: 1440 }),
 }, page: { setViewportSize: async size => { Object.assign(document, size); Object.assign(document.bounds, size); },
  evaluate: async () => document } };
 return { host, native, document, disposed: () => disposed };
}
test('desktop measurement rejects emulation-only sizing and disposes borrowed native handles', async () => {
 const fixture = viewportHost({ resize: false });
 await assert.rejects(setDesktopViewport(fixture.host, { width: 1360, height: 960 }), /viewport exceeds native/);
 assert.equal(fixture.disposed(), 2);
});
test('native sizing must contain the actual document and focused composer inside the visible display', async () => {
 const fixture = viewportHost();
 await setDesktopViewport(fixture.host, { width: 1360, height: 960 });
 await assertDesktopViewport(fixture.host, { composer: true });
 fixture.document.composer.y = 950;
 await assert.rejects(assertDesktopViewport(fixture.host, { composer: true }), /Composer lies outside/);
 fixture.document.composer.y = 671;
 fixture.native.visible = false;
 await assert.rejects(assertDesktopViewport(fixture.host), /window is not visible/);
 fixture.native.visible = true;
 fixture.native.content.x = 2000;
 await assert.rejects(assertDesktopViewport(fixture.host), /outside its display/);
 assert.equal(fixture.disposed(), 6);
});

test('memory census distinguishes whole Markdown subtrees and mounted attachments without retaining content', () => {
 const dom = new JSDOM('<main><div data-reading-id="one"><div data-markdown-block="body"><p><strong>secret text</strong><strong>more</strong></p></div></div><div role="group" aria-label="Message attachments"><img src="blob:private"><button aria-label="Remove private.bmp">Remove</button></div></main>');
 try {
  const value = desktopDOMMemoryCounts(dom.window.document);
  assert.equal(value.transcriptRows, 1); assert.equal(value.markdownBlocks, 1);
  assert.equal(value.markdownSubtreeNodes, 5); assert.equal(value.largestMarkdownBlockNodes, 6);
  assert.equal(value.mountedAttachments, 1); assert.equal(value.images, 1); assert.equal(value.blobImages, 1);
  assert.equal(value.truncated, false);
  assert(Object.values(value).every(item => typeof item === 'number' || typeof item === 'boolean'));
  dom.window.document.querySelector('[role=group]').remove();
  const removed = desktopDOMMemoryCounts(dom.window.document);
  assert.equal(removed.mountedAttachments, 0); assert.equal(removed.blobImages, 0);
  assert(removed.connectedNodes < value.connectedNodes);
 } finally { dom.window.close(); }
});

test('memory census caps connected and nested Markdown traversal with explicit lower-bound evidence', () => {
 const dom = new JSDOM('<div data-markdown-block="outer"><div data-markdown-block="inner"></div></div>');
 try {
  const fragment = dom.window.document.createDocumentFragment();
  for (let index = 0; index < 100_010; index++) fragment.appendChild(dom.window.document.createTextNode('x'));
  dom.window.document.querySelector('[data-markdown-block=inner]').appendChild(fragment);
  const value = desktopDOMMemoryCounts(dom.window.document);
  assert.equal(value.connectedNodes, 100_000); assert.equal(value.markdownSubtreeNodes, 100_000);
  assert.equal(value.truncated, true);
 } finally { dom.window.close(); }
});

for (const fail of [false, true]) test(`memory phase sampling is passive, sequential and detaches ${fail ? 'on failure' : 'on success'}`, async () => {
 const calls = [];
 const host = { page: {}, processMemory: async phase => { calls.push(phase); return { applicationRSSKiB: 7 }; }, context: {
  async newCDPSession() { return {
   async send(method, args) {
    calls.push(method);
    if (method === 'Runtime.getHeapUsage') return { usedSize: 4 };
    if (method === 'Memory.getDOMCounters') return { nodes: 8 };
    assert.equal(args.returnByValue, true); assert.equal(args.timeout, 1000);
    assert.match(args.expression, /100_000/);
    return fail ? { exceptionDetails: {} } : { result: { type: 'object', value: { connectedNodes: 6 } } };
   },
   async detach() { calls.push('detach'); },
  }; },
 } };
 if (fail) await assert.rejects(captureDesktopMemoryPhase(host, 'after-transfer'), /Connected DOM/);
 else {
  const value = await captureDesktopMemoryPhase(host, 'after-transfer');
  assert.equal(value.processes.applicationRSSKiB, 7); assert.equal(value.browser.collection, 'natural');
  assert.equal(value.connectedDOM.connectedNodes, 6); assert(value.sampleMilliseconds >= 0);
 }
 assert.deepEqual(calls, ['after-transfer', 'Runtime.getHeapUsage', 'Memory.getDOMCounters', 'detach', 'Runtime.evaluate', 'detach']);
});

test('stalled connected-DOM sampling detaches and joins its own protocol read', async t => {
 t.mock.timers.enable({ apis: ['setTimeout'] });
 let ready, rejectRead, joined = false, detached = 0;
 const waiting = new Promise(resolve => { ready = resolve; });
 const host = { page: {}, processMemory: async () => ({}), context: {
  async newCDPSession() { return {
   async send(method) {
    if (method !== 'Runtime.evaluate') return {};
    return new Promise((_, reject) => { rejectRead = reject; ready(); }).finally(() => { joined = true; });
   },
   async detach() { detached++; rejectRead?.(new Error('Detached')); },
  }; },
 } };
 const rejected = assert.rejects(captureDesktopMemoryPhase(host, 'after-transfer'), /10 seconds|Detached/);
 await waiting;
 t.mock.timers.tick(10_001);
 await rejected;
 assert.equal(joined, true); assert.equal(detached, 2);
});
