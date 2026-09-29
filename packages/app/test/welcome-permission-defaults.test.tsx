import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { HostPermissionDefault } from '@whip/sdk';
import { providerFixture, revision } from './provider-fixture';
import { welcomeDraftKey } from '../src/session-tabs';
import { fixture } from './welcome-fixture';

function defaults(f: Awaited<ReturnType<typeof fixture>>, mode: string, host = 'host') {
  f.runtime.queries.setQueryData(['host-permission-default', host, 'boot'], { revision, mode });
}
async function pick(name: string) {
  fireEvent.click(await screen.findByRole('button', { name: 'Permission approval mode' }));
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(name) }));
}
async function send(f: Awaited<ReturnType<typeof fixture>>, mode: string) {
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Hello' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledWith(expect.objectContaining({ permission_mode: mode }), undefined));
}

it.each(['prompt', 'automatic'])('creates with the visible host permission default %s', async mode => {
  const f = await fixture(); defaults(f, mode); f.render();
  expect((await screen.findByRole('button', { name: 'Permission approval mode' })).textContent).toContain(mode === 'automatic' ? 'Full Access' : 'Ask for approval');
  expect(f.tab.permissionMode).toBeUndefined();
  await send(f, mode);
});

it('preserves an explicit Ask override through a Full Access host-default refetch', async () => {
  const f = await fixture(); defaults(f, 'automatic'); f.render();
  await pick('Ask for approval');
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id)).toMatchObject({ permissionMode: 'prompt' }));
  act(() => defaults(f, 'automatic'));
  expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Ask for approval');
  await send(f, 'prompt');
});

it('makes selecting the displayed inherited choice explicit before defaults change', async () => {
  const f = await fixture(); defaults(f, 'prompt'); f.render();
  await pick('Ask for approval');
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id)).toMatchObject({ permissionMode: 'prompt' }));
  act(() => defaults(f, 'automatic'));
  await send(f, 'prompt');
});

it('blocks button and Enter submission until a pending default is known', async () => {
  const f = await fixture();
  f.runtime.queries.removeQueries({ queryKey: ['host-permission-default', 'host', 'boot'] });
  let resolve!: (value: HostPermissionDefault) => void;
  f.rpc['host.permission_default'].mockImplementation(() => new Promise(done => { resolve = done; }));
  f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  fireEvent.change(input, { target: { value: 'Wait for the default' } });
  expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Permissions unavailable');
  expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', true);
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  await act(async () => resolve({ revision, mode: 'automatic' } as HostPermissionDefault));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Full Access'));
  await send(f, 'automatic');
});

it('shows a failed default read, preserves the draft, and permits an explicit safe override', async () => {
  const f = await fixture();
  f.runtime.queries.removeQueries({ queryKey: ['host-permission-default', 'host', 'boot'] });
  f.rpc['host.permission_default'].mockRejectedValue(new Error('Defaults unavailable'));
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Keep this task'); f.render();
  await screen.findByText('Could not load host defaults');
  expect(screen.getByRole('button', { name: 'Retry host defaults' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', true);
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep this task');
  await pick('Ask for approval');
  await send(f, 'prompt');
});

it('does not reuse host A permissions when an inherited draft switches to host B', async () => {
  const f = await fixture(); defaults(f, 'automatic');
  const state = f.runtime.getSnapshot();
  const hostA = state.hosts[0]!;
  const other = await providerFixture({ runtimeID: 'host-b', builtins: [{ id: 'coding', revision }] });
  Object.assign(other.data.handlers, f.data.handlers, { 'host.permission_default': () => new Promise(() => {}) });
  const clientB = other.client;
  const hostB = { ...hostA, id: 'host-b', runtimeId: 'host-b', name: 'Host B', client: clientB };
  vi.mocked(f.runtime.getSnapshot).mockReturnValue({ ...state, hosts: [hostA, hostB] });
  for (const key of ['provider-list', 'provider-catalogs']) f.runtime.queries.setQueryData([key, 'host-b'], f.runtime.queries.getQueryData([key, 'host']));
  f.render();
  expect((await screen.findByRole('button', { name: 'Permission approval mode' })).textContent).toContain('Full Access');
  act(() => f.runtime.tabs.updateNew(f.tab.id, { hostProfileId: 'host-b', runtimeId: 'host-b' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Permissions unavailable'));
  act(() => defaults(f, 'prompt', 'host-b'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Ask for approval'));
  expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id)).toMatchObject({ permissionMode: undefined });
  await send(f, 'prompt');
});
