import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppRuntime } from '../src/runtime';
import { PermissionModeControl, PermissionModePicker } from '../src/permission-mode';
import { providerFixture, sessionRecord } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const automaticDescription = 'Approve eligible root actions automatically; child grants and resource scopes still apply';
async function fixture(mode: 'prompt' | 'automatic' = 'prompt', id = 'root') {
  const f = await providerFixture();
  const record = sessionRecord(id);
  let policy = { tree_id: record.tree_id, mode, deny_interactive: false, revision: '9007199254740993', updated_at: '2026-09-28T00:00:00Z' };
  f.data.handlers['permissions.policy'] = () => policy;
  f.data.handlers['permissions.set_mode'] = request => {
    if (request.method !== 'permissions.set_mode') throw new Error('Wrong method');
    const { params } = request;
    const previous_mode = policy.mode;
    policy = { ...policy, mode: params.mode, revision: (BigInt(policy.revision) + 1n).toString() };
    return { id: params.edit_id, session_id: params.session_id, expected_revision: params.expected_revision, mode: params.mode, previous_mode, policy, created_at: policy.updated_at };
  };
  const state = { commands: [] } as unknown as ReturnType<AppRuntime['getSnapshot']>;
  f.runtime.subscribe = () => () => {};
  f.runtime.getSnapshot = () => state;
  f.runtime.command = (client, method, params) => client.command(method, params);
  f.runtime.run = vi.fn(async command => command.send()) as AppRuntime['run'];
  const app = (connected = true) => <PermissionModePicker session={f.client.session(id)} rootId="root" selected={record} connected={connected} />;
  return { ...f, state, app, record, setPolicy: (next: typeof policy) => { policy = next; }, policy: () => policy };
}
async function open() {
  const trigger = screen.getByRole('button', { name: 'Permission approval mode' });
  await waitFor(() => expect(trigger).not.toHaveProperty('disabled', true));
  fireEvent.click(trigger);
  return screen.findByRole('option', { name: /Full Access/ });
}

describe('PermissionModePicker', () => {
  it('uses exact native policy revision and a stable edit identity', async () => {
    const f = await fixture(); f.mount(f.app());
    const option = await open();
    expect(option.textContent).toContain(automaticDescription);
    expect(screen.getByRole('option', { name: /Ask for approval/ }).getAttribute('aria-selected')).toBe('true');
    fireEvent.click(option);
    await waitFor(() => expect(f.count('permissions.set_mode')).toBe(1));
    expect(f.calls.find(call => call.method === 'permissions.set_mode')?.params).toEqual({ session_id: 'root', edit_id: expect.any(String), expected_revision: '9007199254740993', mode: 'automatic' });
    expect(f.runtime.run).toHaveBeenCalledWith(expect.anything(), 'Enable Full Access', undefined, 'host:root:permission-mode');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Full Access'));
  });

  it('can return to prompts and can edit a stopped root', async () => {
    const f = await fixture('automatic'); f.record.lifecycle = 'stopped'; f.mount(f.app()); await open();
    fireEvent.click(screen.getByRole('option', { name: /Ask for approval/ }));
    await waitFor(() => expect(f.calls.find(call => call.method === 'permissions.set_mode')?.params).toMatchObject({ mode: 'prompt' }));
  });

  it('does not read or change a child policy and stays disabled offline', async () => {
    const child = await fixture('prompt', 'child'); const mounted = child.mount(child.app());
    expect(mounted.container.firstChild).toBeNull(); expect(child.count('permissions.policy')).toBe(0); mounted.unmount();
    const offline = await fixture(); offline.mount(offline.app(false));
    expect(screen.getByRole('button', { name: 'Permission approval mode' })).toHaveProperty('disabled', true);
    expect(offline.count('permissions.policy')).toBe(0);
  });

  it('does not silently rebase a choice while its menu is open', async () => {
    const f = await fixture(); f.mount(f.app()); await open();
    f.setPolicy({ ...f.policy(), revision: '9007199254740994' });
    await act(async () => { await f.queries.invalidateQueries({ queryKey: ['permission-mode'] }); });
    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Policy changed'));
    f.data.handlers['permissions.set_mode'] = () => { throw new Error('Policy revision conflict'); };
    fireEvent.click(screen.getByRole('option', { name: /Full Access/ }));
    await screen.findByText('Policy revision conflict');
    expect(f.calls.find(call => call.method === 'permissions.set_mode')?.params).toMatchObject({ expected_revision: '9007199254740993' });
    expect(f.count('permissions.set_mode')).toBe(1);
  });

  it('blocks a second change while the original has unresolved delivery', async () => {
    const f = await fixture();
    f.state.commands = [{ id: 'edit', commandId: 'edit', runtimeId: 'host', label: 'Permission mode', status: 'Unknown', delivery: 'uncertain', draftKey: 'host:root:permission-mode' }];
    f.mount(f.app()); await waitFor(() => expect(f.count('permissions.policy')).toBe(1));
    expect(screen.getByRole('button', { name: 'Permission approval mode' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('status').textContent).toContain('command recovery');
    expect(f.count('permissions.set_mode')).toBe(0);
  });

  it('rejects a policy from another tree', async () => {
    const f = await fixture(); f.setPolicy({ ...f.policy(), tree_id: 'foreign' }); f.mount(f.app());
    await screen.findByText('Permission policy belongs to another tree');
    expect(screen.getByRole('button', { name: 'Permission approval mode' })).toHaveProperty('disabled', true);
    expect(f.count('permissions.set_mode')).toBe(0);
  });
});

it('keeps a failed edit visible and allows an explicit retry', async () => {
  const f = await fixture(); vi.mocked(f.runtime.run).mockRejectedValueOnce(new Error('Mode change rejected'));
  f.mount(f.app()); fireEvent.click(await open());
  const alert = await screen.findByRole('alert');
  expect(alert.closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('action');
  expect(alert.textContent).toContain('Mode change rejected'); expect(f.runtime.report).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('option', { name: /Full Access/ }));
  await waitFor(() => expect(f.count('permissions.set_mode')).toBe(1));
});

it('reuses choices and the scoped explanation with a labelled settings trigger', async () => {
  const f = await fixture(); const change = vi.fn();
  f.mount(<PermissionModeControl presentation="settings" label="Default permission level" value="automatic" onChange={change} />);
  const trigger = screen.getByRole('button', { name: 'Default permission level' });
  expect(trigger.textContent).toContain('Full Access'); expect(trigger.querySelector('.lucide-shield-alert')).not.toBeNull();
  trigger.focus(); expect(document.activeElement).toBe(trigger); fireEvent.click(trigger);
  const option = await screen.findByRole('option', { name: /Full Access/ });
  expect(option.textContent).toContain(automaticDescription); expect(option.getAttribute('aria-selected')).toBe('true');
  fireEvent.click(screen.getByRole('option', { name: /Ask for approval/ })); expect(change).toHaveBeenCalledWith('prompt');
});
