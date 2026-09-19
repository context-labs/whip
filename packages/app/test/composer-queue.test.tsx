import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { UIProvider } from '@whip/ui';
import type { SessionView } from '@whip/sdk/state';
import { ComposerQueue } from '../src/composer-queue';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import type { QueuedInputRow } from '../src/input-presentation';

const row = (seq: string): QueuedInputRow => ({
  id: `inbox:root:${seq}`, text: 'A follow-up', status: 'Queued',
  item: { root_id: 'root', agent_id: 'root', seq, kind: 'submit', status: 'queued', origin: 'client',
    payload: { text: 'A follow-up', reference_id: '', digest: '', size: '11', media_type: 'text/plain', source: '' } },
});

function fixture(rows = [row('1')], connected = true, turn: string | undefined = 'turn') {
  let resolve: (value: unknown) => void = () => {};
  const state = { commands: [] };
  const runtime = { getSnapshot: () => state, subscribe: () => () => {},
    run: vi.fn(() => new Promise(done => { resolve = done; })), checkCommand: vi.fn(), retryCommand: vi.fn() };
  const session = { rootId: 'root', inbox: { steer: vi.fn(() => ({})), remove: vi.fn(() => ({})) }, client: { content: vi.fn() } };
  const view = { session, refresh: vi.fn(async () => {}), getSnapshot: () => ({ root: { cursor: '1' }, collections: {} }), loadCollection: vi.fn(async () => {}) };
  const queries = new QueryClient();
  const app = (items = rows, live = connected, active = turn) => <QueryClientProvider client={queries}><RuntimeContext.Provider value={runtime as unknown as AppRuntime}><UIProvider>
    <form><ComposerQueue rows={items} view={view as unknown as SessionView} runtimeId="runtime" agentId="root" activeTurn={active} connected={live} hasMore={false} />
      <textarea aria-label="Current draft" data-whip-composer defaultValue="Do not overwrite this" /></form>
  </UIProvider></RuntimeContext.Provider></QueryClientProvider>;
  return { app, runtime, session, view, finish: async (status: string) => { await act(async () => resolve({ result: { status } })); } };
}

it('steers the exact queued identity and active turn once without resubmitting the draft', async () => {
  const f = fixture();
  render(f.app());
  const steer = screen.getByRole('button', { name: 'Steer queued message: A follow-up' });
  fireEvent.click(steer); fireEvent.click(steer);
  expect(f.session.inbox.steer).toHaveBeenCalledExactlyOnceWith('root', '1', 'turn');
  expect(f.runtime.run).toHaveBeenCalledWith(expect.anything(), 'Steer queued message', undefined, 'runtime:root:queue:root:1');
  await f.finish('steering');
  expect(f.view.refresh).toHaveBeenCalledOnce();
  expect((screen.getByLabelText('Current draft') as HTMLTextAreaElement).value).toBe('Do not overwrite this');
});

it('leaves the row in place until authoritative removal and explains an already-started race', async () => {
  const f = fixture();
  render(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Remove queued message: A follow-up' }));
  expect(f.session.inbox.remove).toHaveBeenCalledExactlyOnceWith('root', '1');
  expect(screen.getByRole('region', { name: 'Queued messages' })).toBeTruthy();
  await f.finish('already_started');
  expect(screen.getByRole('status').textContent).toBe('That message has already started.');
  expect(f.session.inbox.steer).not.toHaveBeenCalled();
});

it('does not enable controls for unaccepted or unverified messages or offline connections', () => {
  const f = fixture([{ ...row('1'), stale: true }, { id: 'local', text: 'Still sending', status: 'Sending…' }]);
  const rendered = render(f.app());
  for (const button of screen.getAllByRole('button', { name: /^(Steer|Remove) queued message/ })) expect((button as HTMLButtonElement).disabled).toBe(true);
  rendered.rerender(f.app([row('1')], false));
  expect((screen.getByRole('button', { name: /^Remove queued/ }) as HTMLButtonElement).disabled).toBe(true);
});

it('opens complete inline input and shows the count for an attachment-only queue entry', async () => {
  const attachment = row('1');
  attachment.text = '';
  attachment.preview = { text: '', attachment_count: 2 };
  attachment.item = { ...attachment.item!, kind: 'submit.parts', payload: { ...attachment.item!.payload, text: JSON.stringify({ text: 'Complete message from storage', attachments: [] }) } };
  const f = fixture([attachment]);
  render(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Preview queued message: 2 attachments' }));
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Queued message' })).toBeTruthy());
  expect(screen.getByText('Complete message from storage')).toBeTruthy();
  expect(f.session.client.content).not.toHaveBeenCalled();
});

it('does not retarget a steer when its turn ends', async () => {
  const f = fixture();
  const rendered = render(f.app());
  fireEvent.click(screen.getByRole('button', { name: /^Steer queued/ }));
  rendered.rerender(f.app([row('1')], true, 'replacement'));
  await f.finish('turn_ended');
  expect(f.session.inbox.steer).toHaveBeenCalledExactlyOnceWith('root', '1', 'turn');
  expect(screen.getByRole('status').textContent).toContain('That turn has ended');
});

it('bounds mounted rows for a long queue without losing the total or message identities', () => {
  const height = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function(this: HTMLElement) { return this.tagName === 'OL' ? 144 : 44; });
  const width = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(400);
  const rows = Array.from({ length: 128 }, (_, i) => ({ ...row(String(i + 1)), text: `Message ${i + 1}` }));
  const f = fixture(rows);
  const rendered = render(f.app());
  const mounted = rendered.container.querySelectorAll('[data-queue-row]');
  expect(mounted.length).toBeGreaterThan(0);
  expect(mounted.length).toBeLessThan(16);
  expect(mounted[0]?.getAttribute('aria-setsize')).toBe('128');
  expect(screen.getByRole('status').textContent).toContain('128 queued messages');
  height.mockRestore(); width.mockRestore();
});

it('retains the queue and draft when the action fails', async () => {
  const f = fixture();
  f.runtime.run.mockRejectedValueOnce(new Error('Connection lost'));
  render(f.app());
  fireEvent.click(screen.getByRole('button', { name: /^Steer queued/ }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Connection lost'));
  expect(screen.getByRole('alert').closest('[data-composer-queue]')).toBeTruthy();
  expect(screen.getByRole('region', { name: 'Queued messages' })).toBeTruthy();
  expect((screen.getByLabelText('Current draft') as HTMLTextAreaElement).value).toBe('Do not overwrite this');
});
