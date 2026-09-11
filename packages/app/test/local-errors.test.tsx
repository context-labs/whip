import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { SessionView } from '@whip/sdk/state';
import { Action, QueryFeedback } from '../src/details/shared';
import { GeneralSettings } from '../src/settings/general';
import { RuntimeContext } from '../src/context';
import { AppRuntime } from '../src/runtime';

it('keeps an inspector action failure by its control and clears it after a successful retry', async () => {
  const run = vi.fn().mockRejectedValueOnce(new Error('Permission denied')).mockResolvedValue(undefined);
  const view = render(<UIProvider><Action run={run}>Reconnect integration</Action></UIProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Reconnect integration' }));
  await waitFor(() => expect(view.container.querySelector('[data-error-type="action"]')).not.toBeNull());
  expect(screen.getByRole('alert').textContent).toContain('Could not complete this action');
  const details = view.container.querySelector('details')!;
  expect(details.open).toBe(false);
  expect(details.textContent).toContain('Permission denied');
  fireEvent.click(screen.getByRole('button', { name: 'Reconnect integration' }));
  await waitFor(() => expect(screen.getByText('Applied')).toBeTruthy());
  expect(screen.queryByRole('alert')).toBeNull();
});

it('replaces a dependent resource error with availability status when the host disconnects', () => {
  let snapshot = { state: 'connected' };
  const listeners = new Set<() => void>();
  const client = { subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); }, getSnapshot: () => snapshot };
  const view = { session: { rootId: 'session-a', client } } as unknown as SessionView;
  const rendered = render(<UIProvider><QueryFeedback view={view} query={{ supported: true, isLoading: false, error: new Error('Socket disconnected') }} /></UIProvider>);
  expect(rendered.container.querySelector('[data-error-type="resource"]')).not.toBeNull();
  act(() => { snapshot = { state: 'disconnected' }; for (const listener of listeners) listener(); });
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByRole('status').textContent).toContain('Unavailable while this host is offline');
  expect(screen.queryByText('Socket disconnected')).toBeNull();
});

it('keeps device preference save errors in settings without reporting an application error', async () => {
  const data = new Map<string, string>();
  const storage = { keys: () => [...data.keys()], getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => data.set(key, value), removeItem: (key: string) => data.delete(key) };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  const save = vi.spyOn(runtime, 'setPreferences').mockImplementationOnce(() => { throw new Error('Device storage is full'); });
  const report = vi.spyOn(runtime, 'report');
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><GeneralSettings /></UIProvider></RuntimeContext.Provider>);
  fireEvent.click(screen.getByRole('switch', { name: 'Announce attention changes' }));
  expect(save).toHaveBeenCalledOnce();
  expect(view.container.querySelector('[data-error-type="action"]')).not.toBeNull();
  expect(report).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('switch', { name: 'Announce attention changes' }));
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  view.unmount(); runtime.dispose();
});
