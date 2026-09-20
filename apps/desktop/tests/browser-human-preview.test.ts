import assert from 'node:assert/strict';
import test from 'node:test';
import { BrowserHumanPreview, humanPreviewRequest } from '../src/browser-human-preview';
import { nativeHumanPreviewConfirmation } from '../src/browser-human-confirmation';
import type { BrowserWindow } from 'electron';
import type { BrowserManager } from '../src/browser-manager';
import type { BrowserControl } from '../src/browser-control';
import type { BrowserPreviewAuthority } from '../src/browser-preview-authority';
const request = { epoch: 'epoch', connectionId: 'connection', runtimeId: 'runtime', projectId: 'cwd:/remote/project', url: 'http://127.0.0.1:3000/' };
test('human preview admission accepts only literal loopback HTTP(S) and inert absolute project identity', () => {
  assert.equal(humanPreviewRequest(request).port, 3000);
  assert.equal(humanPreviewRequest({ ...request, url: 'https://[::1]/' }).port, 443);
  assert.equal(humanPreviewRequest({ ...request, url: '127.0.0.1:80/' }).port, 80);
  for (const url of ['http://localhost:3000', 'http://127.1:3000', 'http://2130706433:3000', 'http://0x7f000001:3000', 'http://127.0.0.1.evil:3000', 'http://127.0.0.1:0', 'http://user:pass@127.0.0.1:3000', 'file:///tmp', 'http://10.0.0.1:3000'])
    assert.throws(() => humanPreviewRequest({ ...request, url }), url);
  for (const projectId of ['root-id', 'cwd:relative', 'cwd:/bad\npath']) assert.throws(() => humanPreviewRequest({ ...request, projectId }));
  assert.throws(() => humanPreviewRequest({ ...request, grant: true }));
});
function fixture(answer: ConstructorParameters<typeof BrowserHumanPreview>[3] = async () => []) {
  const offerLifetime = new AbortController();
  let epoch = request.epoch, prepared = 0, routes = 0, realized = 0, blocks = 0, busy = false;
  let release: (() => void) | undefined;
  let pause = false;
  const pages = new Map<string, any>();
  const manager = {
    snapshot: () => ({ epoch, tabs: [...pages.values()] }),
    blockNative: () => { blocks++; return () => { blocks--; }; },
    create: (input: any) => { const page = { id: 'tab-' + pages.size, generation: 'generation', ...input }; pages.set(page.id, page); return page; },
    controlledState: (input: any) => { const page = pages.get(input.tabId); if (!page || input.epoch !== epoch) throw Error('stale'); return page; },
    admitted: (input: any) => { if (!pages.has(input.tabId)) throw Error('stale'); },
    controlledContents: async () => { realized++; },
    discardCreated: async (input: any) => { pages.delete(input.tabId); },
    discardUnadmitted: (input: any) => { pages.delete(input.tabId); },
  } as unknown as BrowserManager;
  const previews = { prepareHuman: async () => {
    prepared++; return { preview: { environment_id: 'environment', host_id: 'selected', ports: [3000] }, host: 'selected SSH', signal: offerLifetime.signal,
      assertCurrent: () => {}, verifyCurrent: async () => {}, admit: async (_id: string, guard: () => void, realize: () => Promise<void>) => { guard(); routes++; if (pause) await new Promise<void>(resolve => { release = resolve; }); guard(); await realize(); } };
  } } as unknown as BrowserPreviewAuthority;
  const control = { humanEnvironmentBusy: () => busy, withHumanEnvironment: async (_id: string, work: () => Promise<void>) => { if (busy) throw Error('busy'); busy = true; try { await work(); } finally { busy = false; } } } as unknown as BrowserControl;
  const human = new BrowserHumanPreview(manager, previews, control, async (id, prompt, signal) => { assert.match(prompt.message, /All tabs/); assert.match(prompt.message, /does not grant any agent/); return answer(id, prompt, signal); });
  return { human, pages, abortOffer: () => offerLifetime.abort(), stats: () => ({ prepared, routes, realized, blocks }), changeEpoch: () => { epoch = 'new'; }, pause: () => { pause = true; }, resume: () => release!() };
}
test('denied human confirmation has no descriptor, route, guest or leaked visibility lock', async t => {
  const f = fixture(async () => null); t.after(() => f.human.dispose());
  assert.equal(await f.human.create(request), undefined); assert.equal(f.pages.size, 0);
  assert.deepEqual(f.stats(), { prepared: 1, routes: 0, realized: 0, blocks: 0 });
});
test('confirmed metadata creates no route before admission; duplicate in-flight admission fails closed', async t => {
  const f = fixture(); t.after(() => f.human.dispose()); const page = (await f.human.create(request))!;
  assert.equal(f.stats().routes, 0); const target = { epoch: request.epoch, tabId: page.id, generation: page.generation };
  f.pause(); const pending = f.human.admitted(target); await Promise.resolve();
  await assert.rejects(f.human.admitted(target), /already in progress/); assert.equal(f.stats().realized, 0);
  assert.throws(() => f.human.assertIdle(target), /in progress/);
  f.resume(); await pending; assert.equal(f.stats().realized, 1); assert.equal(f.stats().blocks, 0);
});
test('closed descriptor and stale window cannot start an approved route', async t => {
  const f = fixture(); t.after(() => f.human.dispose()); const page = (await f.human.create(request))!; f.pages.clear();
  await assert.rejects(f.human.admitted({ epoch: request.epoch, tabId: page.id, generation: page.generation }), /stale/); assert.equal(f.stats().routes, 0);
  const changed = fixture(async () => { changed.changeEpoch(); return []; }); t.after(() => changed.human.dispose());
  await assert.rejects(changed.human.create(request), /window changed/); assert.equal(changed.pages.size, 0); assert.equal(changed.stats().routes, 0);
});

test('dedicated native confirmation protects connected-host preview authority on denial, abort and approval', async t => {
  for (const mode of ['deny', 'abort', 'approve'] as const) {
    const window = { isDestroyed: () => false, show() {}, focus() {} } as unknown as BrowserWindow;
    let calls = 0;
    const confirm = nativeHumanPreviewConfirmation(window, async (parent, options) => {
      calls++; assert.equal(parent, window); assert.equal(options.cancelId, 0); assert.equal(options.defaultId, 0);
      for (const text of ['Host: selected SSH', 'Saved host: selected', 'Runtime: runtime', 'Project: /remote/project', 'URL: http://127.0.0.1:3000/', 'Approved ports: 3000', 'does not grant any agent control']) assert.ok(options.detail?.includes(text));
      assert.equal(f.stats().blocks, 1); assert.equal(f.stats().routes, 0); assert.equal(f.pages.size, 0);
      if (mode === 'abort') { f.abortOffer(); assert.equal(options.signal?.aborted, true); }
      return { response: mode === 'deny' ? 0 : 1, checkboxChecked: false };
    });
    const f = fixture(confirm); t.after(() => f.human.dispose());
    const page = await f.human.create(request);
    assert.equal(calls, 1); assert.equal(f.stats().routes, 0); assert.equal(f.stats().realized, 0); assert.equal(f.stats().blocks, 0);
    if (mode === 'approve') {
      assert.ok(page); assert.equal(f.pages.size, 1);
      await f.human.admitted({ epoch: request.epoch, tabId: page.id, generation: page.generation });
      assert.equal(f.stats().routes, 1); assert.equal(f.stats().realized, 1);
    } else { assert.equal(page, undefined); assert.equal(f.pages.size, 0); }
  }
});
