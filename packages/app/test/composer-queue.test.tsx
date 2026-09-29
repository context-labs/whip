import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { RecoveryJournal, RemoteError, type DurableCommand, type Input, type InputSteeringResult } from '@whip/sdk';
import { assertValid } from '@whip/protocol';
import fixtures from '../../protocol/schema/fixtures.json';
import { ComposerQueue } from '../src/composer-queue';
import { providerFixture } from './provider-fixture';
import type { QueuedInputRow } from '../src/input-presentation';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const row = (id: string): QueuedInputRow => ({
  id: `input:child:${id}`, text: 'A follow-up', status: 'Queued',
  item: { id, session_id: 'child', ordinal: id, kind: 'prompt', state: 'queued', source: 'user', turn_id: null,
    created_at: '2026-09-28T00:00:00Z', text_preview: 'A follow-up', preview_truncated: false, attachment_count: '0' },
});
function input(id: string): Input {
  const value: unknown = structuredClone(fixtures.find(item => item.type === 'Input' && item.valid)?.value);
  assertValid('Input', value);
  return { ...value, id, session_id: 'child', kind: 'prompt', host_operation: null, source: 'user', state: 'queued', turn_id: null,
    parts: [{ type: 'text', text: 'Complete message from storage' }], steering: null, design_context: null };
}
async function fixture(rows = [row('1')]) {
  const f = await providerFixture();
  const session = f.client.session('child');
  const state = { commands: [] };
  const journal = new RecoveryJournal({ list: async () => [], put: async () => {}, delete: async () => {} });
  const controller = new AbortController();
  Object.assign(f.runtime, { getSnapshot: () => state, subscribe: () => () => {}, recovery: journal,
    connections: { signal: () => controller.signal },
    run: vi.fn((command: DurableCommand<'inputs.steer'>) => command.send()), checkCommand: vi.fn(), retryCommand: vi.fn() });
  const refresh = vi.fn(async () => {}), loadMore = vi.fn(async () => {});
  let complete!: (value: unknown) => void;
  f.data.handlers['inputs.steer'] = () => new Promise(resolve => { complete = resolve; });
  f.data.handlers['inputs.get'] = request => input(request.params.input_id as string);
  f.data.handlers['inputs.cancel'] = () => new Promise(resolve => { complete = resolve; });
  const app = (items = rows, connected = true, activeTurn = 'turn') => <form>
    <ComposerQueue rows={items} session={session} rootId="root" runtimeId={f.client.runtimeID} activeTurn={activeTurn} connected={connected} hasMore={false} refresh={refresh} loadMore={loadMore} />
    <textarea aria-label="Current draft" data-whip-composer defaultValue="Do not overwrite this" />
  </form>;
  return { ...f, app, session, refresh, loadMore,
    finish: async (state: 'steering' | 'cancelled' | 'claimed') => {
      await waitFor(() => expect(complete).toBeTypeOf('function'));
      await act(async () => {
        if (state === 'steering') {
          const params = f.calls.find(call => call.method === 'inputs.steer')!.params;
          const result: InputSteeringResult = { id: params.edit_id as string, session_id: 'child', input_id: '1', turn_id: params.turn_id as string,
            created_at: '2026-09-28T00:00:00Z', deleted: false, input: { ...input('1'), steering: { id: params.edit_id as string, turn_id: params.turn_id as string, consumed: false } } };
          complete(result);
        } else complete({ ...input('1'), state, turn_id: state === 'claimed' ? 'started' : null });
      });
    },
  };
}

it('steers the exact selected child input and active turn once without resubmitting the draft', async () => {
  const f = await fixture(); f.mount(f.app());
  const steer = screen.getByRole('button', { name: 'Steer queued message: A follow-up' });
  fireEvent.click(steer); fireEvent.click(steer);
  await waitFor(() => expect(f.count('inputs.steer')).toBe(1));
  expect(f.calls.find(call => call.method === 'inputs.steer')?.params).toEqual({ session_id: 'child', input_id: '1', turn_id: 'turn', edit_id: expect.any(String) });
  expect(f.runtime.run).toHaveBeenCalledWith(expect.anything(), 'Steer queued message', undefined, 'host:root:queue:child:1');
  await f.finish('steering'); expect(f.refresh).toHaveBeenCalledOnce(); expect(f.count('sessions.submit')).toBe(0);
  expect((screen.getByLabelText('Current draft') as HTMLTextAreaElement).value).toBe('Do not overwrite this');
});
it('leaves the row until authoritative removal and reports an already-started race', async () => {
  const f = await fixture(); f.mount(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Remove queued message: A follow-up' }));
  await waitFor(() => expect(f.count('inputs.cancel')).toBe(1));
  expect(f.calls.find(call => call.method === 'inputs.get')?.params).toEqual({ session_id: 'child', input_id: '1' });
  expect(f.calls.find(call => call.method === 'inputs.cancel')?.params).toEqual({ input_id: '1' });
  expect(screen.getByRole('region', { name: 'Queued messages' })).toBeTruthy();
  await f.finish('claimed'); expect(screen.getByRole('status').textContent).toBe('That message has already started.'); expect(f.count('inputs.steer')).toBe(0);
});
it('disables unaccepted, foreign, stale and offline queue controls', async () => {
  const f = await fixture([{ ...row('1'), stale: true }, { id: 'local', text: 'Still sending', status: 'Sending…' }, { ...row('2'), item: { ...row('2').item!, session_id: 'root' } }]);
  const rendered = f.mount(f.app());
  for (const button of screen.getAllByRole('button', { name: /^(Steer|Remove) queued message/ })) expect((button as HTMLButtonElement).disabled).toBe(true);
  rendered.rerender(f.wrap(f.app([row('1')], false)));
  expect((screen.getByRole('button', { name: /^Remove queued/ }) as HTMLButtonElement).disabled).toBe(true);
});
it('reads the full canonical input only when preview opens, with the exact child owner', async () => {
  const attachment = { ...row('1'), text: '', preview: { text: '', attachment_count: 2 }, item: { ...row('1').item!, attachment_count: '2' } };
  const f = await fixture([attachment]); f.mount(f.app()); expect(f.count('inputs.get')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Preview queued message: 2 attachments' }));
  await screen.findByText('Complete message from storage');
  expect(f.calls.find(call => call.method === 'inputs.get')?.params).toEqual({ session_id: 'child', input_id: '1' }); expect(f.count('content.read')).toBe(0);
});
it('keeps the captured steer target when a later render shows another turn', async () => {
  const f = await fixture(); const rendered = f.mount(f.app());
  fireEvent.click(screen.getByRole('button', { name: /^Steer queued/ }));
  rendered.rerender(f.wrap(f.app([row('1')], true, 'replacement')));
  await f.finish('steering'); expect(f.calls.find(call => call.method === 'inputs.steer')?.params.turn_id).toBe('turn'); expect(f.count('inputs.steer')).toBe(1);
});
it('refreshes a changed target conflict without choosing another turn or resubmitting', async () => {
  const f = await fixture(); f.data.handlers['inputs.steer'] = () => { throw new RemoteError({ code: -32002, kind: 'CONFLICT', message: 'Target ended' }); };
  f.mount(f.app()); fireEvent.click(screen.getByRole('button', { name: /^Steer queued/ }));
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('queue or target turn changed'));
  expect(f.refresh).toHaveBeenCalledOnce(); expect(f.count('inputs.steer')).toBe(1); expect(f.count('sessions.submit')).toBe(0);
});
it('bounds mounted rows for a long queue while retaining exact identities and total', async () => {
  const height = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function(this: HTMLElement) { return this.tagName === 'OL' ? 144 : 44; });
  const width = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(400);
  const rows = Array.from({ length: 128 }, (_, i) => ({ ...row(String(i + 1)), text: `Message ${i + 1}` }));
  const f = await fixture(rows); const rendered = f.mount(f.app());
  const mounted = rendered.container.querySelectorAll('[data-queue-row]');
  expect(mounted.length).toBeGreaterThan(0); expect(mounted.length).toBeLessThan(16); expect(mounted[0]?.getAttribute('aria-setsize')).toBe('128');
  expect(screen.getByRole('status').textContent).toContain('128 queued messages'); height.mockRestore(); width.mockRestore();
});
it('retains the queue and draft when delivery fails', async () => {
  const f = await fixture(); vi.mocked(f.runtime.run).mockRejectedValueOnce(new Error('Connection lost'));
  f.mount(f.app()); fireEvent.click(screen.getByRole('button', { name: /^Steer queued/ }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Connection lost'));
  expect(screen.getByRole('alert').closest('[data-composer-queue]')).toBeTruthy(); expect(screen.getByRole('region', { name: 'Queued messages' })).toBeTruthy();
  expect((screen.getByLabelText('Current draft') as HTMLTextAreaElement).value).toBe('Do not overwrite this');
});
