import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { HostDialog } from '../src/host-dialog';
import { RuntimeContext } from '../src/context';
import { AppRuntime } from '../src/runtime';
import type { HostConnection } from '../src/hosts';
import { localProfile, type ConnectionProfile } from '../src/connections';

function fixture() {
  const values = new Map<string, string>();
  const ssh: ConnectionProfile = { id: 'ssh:saved', label: 'Saved SSH', runtimeId: 'ssh-runtime', target: { kind: 'ssh', host: 'saved-alias' } };
  values.set('whip.hosts.v2', JSON.stringify([localProfile, ssh]));
  values.set('whip.selectedHost.v2', JSON.stringify(ssh.id));
  const runtime = new AppRuntime({ defaultConnection: localProfile, connectionKinds: ['local', 'url', 'ssh'],
    storage: { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } },
    openExternal: async () => {}, copy: async () => {}, download: async () => 'saved',
  });
  const save = vi.spyOn(runtime.connections, 'saveNative').mockResolvedValue(ssh.id);
  vi.spyOn(runtime.connections, 'connect').mockResolvedValue();
  const onSaved = vi.fn(); const onOpenChange = vi.fn();
  let editing: HostConnection | undefined;
  const application = (open = true) => <RuntimeContext.Provider value={runtime}><UIProvider><HostDialog key={editing?.id} editing={editing} open={open} onOpenChange={onOpenChange} onSaved={onSaved} /></UIProvider></RuntimeContext.Provider>;
  const view = render(application());
  const edit = (name: string) => {
    editing = runtime.getSnapshot().hosts.find(host => host.name === name);
    view.rerender(application());
    fireEvent.click(screen.getByText('Advanced'));
  };
  return { runtime, ssh, save, onSaved, onOpenChange, edit,
    dispose() { view.unmount(); runtime.dispose(); },
    reopen() { view.rerender(application(false)); view.rerender(application(true)); },
  };
}
it('preserves saved SSH identity by default and requires an explicit replacement choice', async () => {
  const f = fixture(); f.edit('Saved SSH');
  expect(screen.getByRole('switch', { name: /Accept a new daemon identity/ }).getAttribute('aria-checked')).toBe('false');
  fireEvent.click(screen.getByRole('button', { name: /Save changes|Add server/ }));
  await waitFor(() => expect(f.save).toHaveBeenCalledExactlyOnceWith(f.ssh, false, expect.any(AbortSignal)));
  expect(f.ssh.runtimeId).toBe('ssh-runtime'); f.dispose();
});
it('passes explicit replacement consent for the exact saved profile', async () => {
  const f = fixture(); f.edit('Saved SSH');
  fireEvent.click(screen.getByRole('switch', { name: /Accept a new daemon identity/ }));
  fireEvent.click(screen.getByRole('button', { name: /Save changes|Add server/ }));
  await waitFor(() => expect(f.save).toHaveBeenCalledExactlyOnceWith(f.ssh, true, expect.any(AbortSignal))); f.dispose();
});
it('supports managed-local replacement through the same host manager', async () => {
  const f = fixture(); f.edit('This Mac');
  fireEvent.click(screen.getByRole('button', { name: /Save changes|Add server/ }));
  await waitFor(() => expect(f.save).toHaveBeenCalledWith({ ...localProfile, runtimeId: undefined }, false, expect.any(AbortSignal))); f.dispose();
});
it('canceling an edit preserves its identity and does not connect', () => {
  const f = fixture(); f.edit('Saved SSH');
  fireEvent.click(screen.getByRole('switch', { name: /Accept a new daemon identity/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel', exact: true }));
  expect(f.save).not.toHaveBeenCalled(); expect(f.ssh.runtimeId).toBe('ssh-runtime'); f.dispose();
});
it('creates SSH profiles without requiring Local’s shared URL registry', async () => {
  const f = fixture(); fireEvent.click(screen.getByRole('tab', { name: 'SSH', exact: true }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Server name (optional)', exact: true }), { target: { value: 'Remote' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'SSH host or alias' }), { target: { value: 'my-server' } });
  fireEvent.click(screen.getByRole('button', { name: /Save changes|Add server/ }));
  await waitFor(() => expect(f.save).toHaveBeenCalledWith(expect.objectContaining({ id: expect.stringMatching(/^ssh:/), label: 'Remote', target: { kind: 'ssh', host: 'my-server' } }), false, expect.any(AbortSignal))); f.dispose();
});
it('shows native setup errors without closing the form', async () => {
  const f = fixture(); f.edit('Saved SSH'); f.save.mockRejectedValueOnce(new Error('Host identity changed'));
  fireEvent.click(screen.getByRole('button', { name: /Save changes|Add server/ }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Host identity changed'));
  expect(f.onOpenChange).not.toHaveBeenCalled(); f.dispose();
});
it('does not close a reopened form when an old native request finishes', async () => {
  const f = fixture(); f.edit('Saved SSH'); let finish!: (id: string) => void;
  f.save.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  fireEvent.click(screen.getByRole('button', { name: /Save changes|Add server/ })); f.reopen();
  await act(async () => { finish(f.ssh.id); });
  expect(f.onOpenChange).not.toHaveBeenCalled(); f.dispose();
});
