import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import type { BrowserWindow } from 'electron';
import { nativeHumanPreviewConfirmation } from '../src/browser-human-confirmation';

const value = { title: 'Open SSH preview?', fields: [], confirmLabel: 'Open preview', message: 'Host: selected SSH\nRuntime: runtime\nProject: /project\nURL: http://127.0.0.1:3000/\nApproved ports: 3000\nThis does not grant any agent control.' };
function owner() {
  let destroyed = false, shows = 0, focuses = 0;
  return { window: { isDestroyed: () => destroyed, show: () => shows++, focus: () => focuses++ } as unknown as BrowserWindow,
    destroy: () => { destroyed = true; }, counts: () => ({ shows, focuses }) };
}
test('trusted human confirmation uses parented native consent with exact detail, Cancel default and caller signal', async () => {
  const f = owner(), controller = new AbortController();
  const confirm = nativeHumanPreviewConfirmation(f.window, async (parent, options) => {
    assert.equal(parent, f.window);
    assert.deepEqual(options, { type: 'question', title: value.title, message: value.title, detail: value.message,
      buttons: ['Cancel', 'Open preview'], defaultId: 0, cancelId: 0, noLink: true, signal: controller.signal });
    return { response: 1, checkboxChecked: false };
  });
  assert.deepEqual(await confirm('already_connected', value, controller.signal), []);
  assert.deepEqual(f.counts(), { shows: 1, focuses: 1 });
});
test('Cancel, close and every non-approval response deny native consent', async () => {
  for (const response of [0, -1, 2]) {
    const f = owner(), confirm = nativeHumanPreviewConfirmation(f.window, async () => ({ response, checkboxChecked: false }));
    assert.equal(await confirm('connected', value, new AbortController().signal), null);
  }
});
test('pre-abort and a closed owner never open a native prompt', async () => {
  const f = owner(); let calls = 0;
  const confirm = nativeHumanPreviewConfirmation(f.window, async () => { calls++; return { response: 1, checkboxChecked: false }; });
  assert.equal(await confirm('connected', value, AbortSignal.abort()), null);
  f.destroy(); assert.equal(await confirm('connected', value, new AbortController().signal), null);
  assert.equal(calls, 0); assert.deepEqual(f.counts(), { shows: 0, focuses: 0 });
});
test('late native approval after cancellation or parent destruction is denied', async () => {
  for (const close of [false, true]) {
    const f = owner(), controller = new AbortController();
    const confirm = nativeHumanPreviewConfirmation(f.window, async (_parent, options) => {
      assert.equal(options.signal, controller.signal);
      if (close) f.destroy(); else controller.abort();
      return { response: 1, checkboxChecked: false };
    });
    assert.equal(await confirm('connected', value, controller.signal), null);
  }
});
test('only one human sheet is pending and errors release that bound; credential requests are rejected', async () => {
  const f = owner(); let release!: () => void, calls = 0;
  const confirm = nativeHumanPreviewConfirmation(f.window, async () => {
    calls++; if (calls === 1) { await new Promise<void>(resolve => { release = resolve; }); throw new Error('native unavailable'); }
    return { response: 0, checkboxChecked: false };
  });
  const signal = new AbortController().signal, pending = confirm('connected', value, signal);
  await assert.rejects(confirm('connected', value, signal), /already open/);
  release(); await assert.rejects(pending, /native unavailable/);
  assert.equal(await confirm('connected', value, signal), null);
  await assert.rejects(confirm('connected', { ...value, fields: [{ label: 'Password', secret: true }] }, signal), /credentials/);
  assert.equal(calls, 2);
});
test('production main wires human previews to native confirmation, not the connection-authentication prompt', async () => {
  const main = await readFile('apps/desktop/src/main.ts', 'utf8');
  assert.ok(main.includes("import { nativeHumanPreviewConfirmation } from './browser-human-confirmation'"));
  assert.ok(main.includes('new BrowserHumanPreview(browsers, previews, browserControl,\n    nativeHumanPreviewConfirmation(window, (parent, options) => dialog.showMessageBox(parent, options)))'));
  assert.ok(!main.includes('new BrowserHumanPreview(browsers, previews, browserControl, prompt)'));
});
