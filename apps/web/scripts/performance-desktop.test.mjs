import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { assertDesktopViewport, closeDesktopProcess, setDesktopViewport, summaryPollEvidence } from './performance-desktop.mjs';
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
