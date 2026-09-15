import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { RuntimeContext } from '../src/context';
import { HostDialog } from '../src/host-dialog';
import type { AppRuntime } from '../src/runtime';
import type { ConnectionProfile } from '../src/connections';
import type { SSHProfileList } from '../src/platform';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const profiles: SSHProfileList = { profiles: [{ alias: 'gpu-box', hostname: 'gpu.internal', user: 'sam', port: 22 }, { alias: 'build-box' }, { alias: 'already-added' }], truncated: false };
function fixture(discover: (() => Promise<SSHProfileList>) | undefined = vi.fn(async () => profiles), legacyDesktop = false) {
  const state = { hosts: [{ id: 'saved', profile: { runtimeId: 'verified', target: { kind: 'ssh', host: 'already-added' } } }], home: { state: 'connected' }, profilesReady: true, legacyHosts: [] };
  const saveNative = vi.fn(async (profile: ConnectionProfile, _accept: boolean, _signal?: AbortSignal) => profile.id);
  const connections = { refreshProfiles: vi.fn(async () => {}), saveNative, select: vi.fn(), connect: vi.fn(async () => {}) };
  const runtime = { queries: new QueryClient(), platform: { connectionKinds: ['url', 'ssh'], listSSHProfiles: legacyDesktop ? undefined : discover },
    getSnapshot: () => state, subscribe: () => () => {}, connections } as unknown as AppRuntime;
  const onOpenChange = vi.fn();
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="whip-dark"><UIProvider><QueryClientProvider client={query}><HostDialog open onOpenChange={onOpenChange} /></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { discover, connections, onOpenChange };
}
const sshTab = () => fireEvent.click(screen.getByRole('tab', { name: 'SSH' }));
const advanced = () => screen.getByRole('button', { name: /Advanced SSH options/ });

it('loads only on the SSH tab; selects one alias using the existing verified save path', async () => {
  const f = fixture();
  expect(f.discover).not.toHaveBeenCalled();
  sshTab();
  const radio = await screen.findByRole('radio', { name: 'gpu-box' });
  expect(screen.getByRole('button', { name: 'Connect' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('radio', { name: 'already-added' }).getAttribute('aria-disabled')).toBe('true');
  fireEvent.click(radio);
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
  await waitFor(() => expect(f.connections.saveNative).toHaveBeenCalledOnce());
  expect(f.connections.saveNative.mock.calls[0][0].target).toEqual({ kind: 'ssh', host: 'gpu-box' });
  await waitFor(() => expect(f.onOpenChange).toHaveBeenCalledWith(false));
  expect(f.connections.select).toHaveBeenCalledWith(f.connections.saveNative.mock.calls[0][0].id);
});

it('supports keyboard selection, filtering, and keeping selection on refresh', async () => {
  const user = userEvent.setup();
  const f = fixture(); sshTab();
  const radio = await screen.findByRole('radio', { name: 'gpu-box' });
  act(() => radio.focus());
  await user.keyboard(' ');
  await user.keyboard('{ArrowDown}');
  expect(screen.getByRole('radio', { name: 'build-box' }).getAttribute('aria-checked')).toBe('true');
  fireEvent.change(screen.getByRole('textbox', { name: 'Filter SSH profiles' }), { target: { value: 'nothing' } });
  expect(screen.getByText('No profiles match')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Clear filter' }));
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  await waitFor(() => expect(f.discover).toHaveBeenCalledTimes(2));
  expect(screen.getByRole('radio', { name: 'build-box' }).getAttribute('aria-checked')).toBe('true');
});

it('preserves both manual drafts and profile choice when switching modes', async () => {
  fixture(); sshTab();
  fireEvent.click(await screen.findByRole('radio', { name: 'gpu-box' }));
  fireEvent.click(advanced());
  fireEvent.change(await screen.findByLabelText('SSH host or alias'), { target: { value: 'manual-host' } });
  fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'deploy' } });
  fireEvent.click(screen.getByRole('button', { name: 'Choose a profile' }));
  expect(screen.getByRole('radio', { name: 'gpu-box' }).getAttribute('aria-checked')).toBe('true');
  fireEvent.click(advanced());
  expect((await screen.findByLabelText('SSH host or alias') as HTMLInputElement).value).toBe('manual-host');
  expect((screen.getByLabelText('Username') as HTMLInputElement).value).toBe('deploy');
});

it('keeps manual entry available when discovery fails and can refresh to an empty configuration', async () => {
  const discover = vi.fn<() => Promise<SSHProfileList>>().mockRejectedValueOnce(new Error('Cannot read config')).mockResolvedValue({ profiles: [], truncated: false });
  fixture(discover); sshTab();
  await screen.findByText('Couldn’t read SSH profiles');
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await screen.findByText('No SSH profiles found');
  fireEvent.click(advanced());
  expect(await screen.findByLabelText('SSH host or alias')).toBeTruthy();
});

it('retains rows during refresh and exposes a bounded-list notice', async () => {
  let finish!: (value: SSHProfileList) => void;
  const discover = vi.fn<() => Promise<SSHProfileList>>().mockResolvedValueOnce(profiles).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  fixture(discover); sshTab();
  await screen.findByRole('radio', { name: 'gpu-box' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(screen.getByRole('radio', { name: 'gpu-box' })).toBeTruthy();
  await act(async () => finish({ ...profiles, truncated: true }));
  expect(await screen.findByText(/Showing a limited list/)).toBeTruthy();
});

it('retries the same target and identity after failure, without closing or losing input', async () => {
  const f = fixture();
  f.connections.saveNative.mockRejectedValueOnce(new Error('Host did not respond'));
  sshTab(); fireEvent.click(await screen.findByRole('radio', { name: 'gpu-box' }));
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
  const retry = await screen.findByRole('button', { name: 'Try again' });
  expect(f.onOpenChange).not.toHaveBeenCalled();
  fireEvent.click(retry);
  await waitFor(() => expect(f.connections.saveNative).toHaveBeenCalledTimes(2));
  expect(f.connections.saveNative.mock.calls[1][0]).toEqual(f.connections.saveNative.mock.calls[0][0]);
});

it('cancels only the pending native attempt, preserving the selected profile', async () => {
  const f = fixture();
  f.connections.saveNative.mockImplementationOnce((_profile, _accept, signal) => new Promise((_resolve, reject) => signal!.addEventListener('abort', () => reject(signal!.reason))));
  sshTab(); fireEvent.click(await screen.findByRole('radio', { name: 'gpu-box' }));
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
  const cancel = await screen.findByRole('button', { name: 'Cancel connection' });
  expect(screen.getAllByText('Connecting to gpu-box…')).toHaveLength(1);
  expect(screen.queryByRole('button', { name: 'Connecting…' })).toBeNull();
  fireEvent.click(cancel);
  await screen.findByRole('button', { name: 'Connect' });
  expect(f.connections.saveNative.mock.calls[0][2]?.aborted).toBe(true);
  expect(screen.getByRole('radio', { name: 'gpu-box' }).getAttribute('aria-checked')).toBe('true');
  expect(screen.queryByText('Couldn’t connect')).toBeNull();
  expect(f.onOpenChange).not.toHaveBeenCalled();
});


it('preserves SSH drafts across connection-method tabs', async () => {
  fixture(); sshTab();
  fireEvent.click(await screen.findByRole('radio', { name: 'gpu-box' }));
  fireEvent.click(advanced());
  fireEvent.change(await screen.findByLabelText('SSH host or alias'), { target: { value: 'manual-host' } });
  fireEvent.click(screen.getByRole('tab', { name: 'Server URL' }));
  sshTab();
  expect((await screen.findByLabelText('SSH host or alias') as HTMLInputElement).value).toBe('manual-host');
  fireEvent.click(screen.getByRole('button', { name: 'Choose a profile' }));
  expect((await screen.findByRole('radio', { name: 'gpu-box' })).getAttribute('aria-checked')).toBe('true');
});

it('retains manual connection on older desktop bridges without profile discovery', async () => {
  const f = fixture(undefined, true); sshTab();
  fireEvent.change(await screen.findByLabelText('SSH host or alias'), { target: { value: 'manual-host' } });
  fireEvent.change(screen.getByLabelText('Port'), { target: { value: '2222' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
  await waitFor(() => expect(f.connections.saveNative).toHaveBeenCalledOnce());
  expect(f.connections.saveNative.mock.calls[0][0].target).toEqual({ kind: 'ssh', host: 'manual-host', port: 2222 });
});


it('clears a selection removed by a successful profile refresh', async () => {
  const discover = vi.fn<() => Promise<SSHProfileList>>().mockResolvedValueOnce(profiles).mockResolvedValue({ profiles: [], truncated: false });
  fixture(discover); sshTab();
  fireEvent.click(await screen.findByRole('radio', { name: 'gpu-box' }));
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  await screen.findByText('No SSH profiles found');
  await waitFor(() => expect(screen.getByRole('button', { name: 'Connect' }).hasAttribute('disabled')).toBe(true));
});
