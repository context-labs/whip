import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { fixture } from '../../../packages/app/test/welcome-fixture';

// Component-shape contracts only; startup-frontdoor.test.tsx covers real bootstrap.
// Desktop owns the probe; the app has no dependency on its consumer.
// Geometry/fonts/navigation are favorable: this is not a native paint/connection test.
const source = readFileSync('apps/desktop/src/startup-probe.ts', 'utf8');
const script = source.split('const snapshotScript = String.raw`')[1]!.split('`;')[0]!;
async function observe(mutate?: (document: Document) => void) {
  const dom = new JSDOM('<aside aria-label="Session navigation"><button aria-label="Manage servers">Servers</button></aside>' + document.body.innerHTML,
    { url: 'whip-app://bundle/', runScripts: 'outside-only', pretendToBeVisual: true });
  try {
    Object.defineProperty(dom.window.document, 'fonts', { value: { status: 'loaded', ready: Promise.resolve() } });
    dom.window.HTMLElement.prototype.getClientRects = function () { return (this.closest('[hidden]') ? [] : [{}]) as unknown as DOMRectList; };
    dom.window.performance.getEntriesByName = () => [{ startTime: 1 }] as unknown as PerformanceEntry[];
    mutate?.(dom.window.document);
    return await dom.window.eval(script);
  } finally { dom.window.close(); }
}
async function home(ready = false) {
  const f = fixture(false, ready); f.render();
  const control = await screen.findByRole('button', { name: ready ? 'Model' : 'Connect OpenAI', exact: true });
  expect((control as HTMLButtonElement).disabled).toBe(false);
  return f;
}
const usable = { host: true, noNotice: true, home: true, visible: true, fonts: true, painted: true, pathname: '/' };

it('accepts actionable fresh provider setup alongside drafting with send disabled', async () => {
  const f = await home();
  expect((screen.getByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement).disabled).toBe(false);
  expect((screen.getByRole('button', { name: 'Send first message' }) as HTMLButtonElement).disabled).toBe(true);
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  expect(await observe()).toMatchObject(usable);
});
it('accepts the configured first-message composer with its Model control', async () => {
  await home(true);
  expect(screen.getByRole('textbox', { name: 'Your first message' })).toBeTruthy();
  expect(await observe()).toMatchObject(usable);
});
it('rejects the actual disconnected provider setup', async () => {
  const f = fixture(false, false);
  Object.assign(f.raw.getSnapshot(), { state: 'connecting' });
  f.render();
  expect((await screen.findByRole('button', { name: 'Connect OpenAI' }) as HTMLButtonElement).disabled).toBe(true);
  expect(await observe()).toMatchObject({ home: false, painted: false });
});
it('rejects pending provider inventory', async () => {
  const f = fixture(false, false);
  f.runtime.queries.removeQueries({ queryKey: ['provider-list', 'host'] });
  f.raw.providers.list.mockImplementation(() => new Promise(() => {}));
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(await observe()).toMatchObject({ home: false, painted: false });
});
it('rejects disabled provider choices', async () => {
  await home();
  expect(await observe(d => d.querySelectorAll<HTMLButtonElement>('[data-provider-choice]').forEach(b => { b.disabled = true; })))
    .toMatchObject({ home: false, painted: false });
});
it('does not accept an unrelated enabled Connect Remote control', async () => {
  await home();
  expect(screen.getByRole('button', { name: 'Connect Remote' })).toBeTruthy();
  expect(await observe(d => d.querySelectorAll('[data-provider-choice]').forEach(b => b.remove())))
    .toMatchObject({ home: false, painted: false });
});
it('rejects an editable composer with a disabled Model control', async () => {
  await home(true);
  expect(await observe(d => { d.querySelector<HTMLButtonElement>('[aria-label="Model"]')!.disabled = true; }))
    .toMatchObject({ home: false, painted: false });
});
it('requires the provider choice to belong to the visible setup region', async () => {
  await home();
  expect(await observe(d => d.querySelectorAll('[data-provider-choice]').forEach(b => d.body.append(b))))
    .toMatchObject({ home: false, painted: false });
});
it('requires the Model control to belong to the first-message form', async () => {
  await home(true);
  expect(await observe(d => d.body.append(d.querySelector('[aria-label="Model"]')!)))
    .toMatchObject({ home: false, painted: false });
});
it('rejects a disabled composer even with an enabled Model control', async () => {
  await home(true);
  expect(await observe(d => { d.querySelector<HTMLTextAreaElement>('[data-whip-composer]')!.disabled = true; }))
    .toMatchObject({ home: false, painted: false });
});
it('retains the connecting-notice rejection', async () => {
  await home();
  expect(await observe(d => d.body.insertAdjacentHTML('beforeend', '<p role="status">Connecting to Local…</p>')))
    .toMatchObject({ noNotice: false, painted: false });
});
it('retains the visible-error rejection', async () => {
  await home();
  expect(await observe(d => d.body.insertAdjacentHTML('beforeend', '<div role="alert">Provider discovery failed</div>')))
    .toMatchObject({ noNotice: false, painted: false });
});
it('rejects hidden provider setup', async () => {
  await home();
  expect(await observe(d => { d.querySelector<HTMLElement>('[aria-label="Provider setup"]')!.hidden = true; }))
    .toMatchObject({ home: false, painted: false });
});
