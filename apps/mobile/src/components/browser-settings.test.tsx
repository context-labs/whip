import { act, fireEvent, render, waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { assertValid, type HostBrowserDriver } from '@whip/protocol';
import { BrowserSettings } from './browser-settings';
let mockRuntime: ReturnType<typeof fixture>['runtime'], mockFocused = true;
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('expo-router', () => ({ useIsFocused: () => mockFocused }));
jest.mock('@expo/ui', () => {
  const React = require('react'), { View, Text, Pressable } = require('react-native');
  return { Host: View, Column: View, Button: ({ label, onPress, disabled }: { label: string; onPress: () => void; disabled: boolean }) => React.createElement(Pressable, { onPress, disabled }, React.createElement(Text, {}, label)) };
});
const status = (patch: Partial<HostBrowserDriver> = {}): HostBrowserDriver => {
  const value = { revision: 'a'.repeat(64), configured_driver: 'rod' as const, driver: 'rod' as const, pinned: false, ...patch };
  assertValid('HostBrowserDriver', value); return value;
};
function fixture(value = status()) {
  mockFocused = true; const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } });
  const read = jest.fn(async () => structuredClone(value)), save = jest.fn(async (_revision: string, driver: HostBrowserDriver['driver'], _options: { signal: AbortSignal }) => status({ revision: 'b'.repeat(64), configured_driver: driver, driver, pinned: value.pinned }));
  const client = { runtimeID: 'runtime', processEpoch: 'epoch', hosts: { browserDriver: read, setBrowserDriver: save } }, state = { ready: true, active: true, client, host: { id: 'host', name: 'Host' } };
  const runtime = { getSnapshot: () => ({ ...state }), requireReady: () => { if (!state.active || !state.ready) throw new Error('Offline'); return state.client; }, query };
  mockRuntime = runtime;
  return { runtime, query, read, save, state, element: () => <QueryClientProvider client={query}><BrowserSettings /></QueryClientProvider> };
}
afterEach(() => jest.restoreAllMocks());
test('reading does not change the driver; explicit confirmation sends its exact reviewed revision', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await screen.findByText('Saved driver: Rod'); expect(f.save).not.toHaveBeenCalled();
  expect(screen.getByRole('button', { name: 'Use Rod…' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Use ChromeDP…')); expect(f.save).not.toHaveBeenCalled(); expect(alert.mock.calls[0][1]).toContain('no browser is opened');
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.save).toHaveBeenCalledWith('a'.repeat(64), 'chromedp', { signal: expect.any(AbortSignal) });
  await screen.findByText('Saved driver: ChromeDP'); expect(screen.getByText('Effective driver: ChromeDP')).toBeTruthy();
  await screen.unmount(); f.query.clear();
});
test('process pin is distinct from saved selection and only its own driver can be saved', async () => {
  const f = fixture(status({ configured_driver: 'chromedp', driver: 'rod', pinned: true })), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await screen.findByText('Saved driver: ChromeDP'); expect(screen.getByText('Effective driver: Rod')).toBeTruthy();
  expect(screen.getByText(/This host process pins Rod/)).toBeTruthy(); expect(screen.getByRole('button', { name: 'Use ChromeDP…' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Use Rod…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.save).toHaveBeenCalledWith('a'.repeat(64), 'rod', { signal: expect.any(AbortSignal) }); await screen.unmount(); f.query.clear();
});
test('lost CAS acknowledgment and failed refresh block every stale confirmation until a successful read', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element()); f.save.mockRejectedValueOnce(new Error('Lost response'));
  await screen.findByText('Saved driver: Rod'); await fireEvent.press(screen.getByText('Use ChromeDP…'));
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.save).toHaveBeenCalledTimes(1);
  f.read.mockRejectedValueOnce(new Error('Read failed')); await fireEvent.press(screen.getByText('Read current browser settings')); expect(screen.getByRole('button', { name: 'Use ChromeDP…' })).toBeDisabled();
  f.read.mockResolvedValueOnce(status({ revision: 'c'.repeat(64) })); await fireEvent.press(screen.getByText('Read current browser settings'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Use ChromeDP…' })).not.toBeDisabled());
  await fireEvent.press(screen.getByText('Use ChromeDP…')); await act(async () => { alert.mock.calls[1][2]![1].onPress!(); });
  expect(f.save).toHaveBeenLastCalledWith('c'.repeat(64), 'chromedp', { signal: expect.any(AbortSignal) }); await screen.unmount(); f.query.clear();
});
test('retired host and unmounted confirmations never dispatch settings', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await screen.findByText('Saved driver: Rod'); await fireEvent.press(screen.getByText('Use ChromeDP…'));
  f.state.client = { ...f.state.client }; await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.save).not.toHaveBeenCalled();
  await screen.rerender(f.element()); await fireEvent.press(screen.getByText('Use ChromeDP…')); await screen.unmount();
  await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.save).not.toHaveBeenCalled(); f.query.clear();
});
test('delayed status cannot attach to a replacement client', async () => {
  const f = fixture(); let complete!: (value: HostBrowserDriver) => void;
  f.read.mockImplementationOnce(() => new Promise(resolve => { complete = resolve; })); const screen = await render(f.element());
  await waitFor(() => expect(f.read).toHaveBeenCalledTimes(1)); f.state.client = { ...f.state.client }; await act(async () => { complete(status()); });
  await screen.findByText('Host changed while reading browser settings.'); expect(screen.queryByText('Saved driver: Rod')).toBeNull();
  await screen.unmount(); f.query.clear();
});
test('leaving foreground cancels the wait and requires inspection before another change', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}); let complete!: (value: HostBrowserDriver) => void;
  f.save.mockImplementationOnce(() => new Promise(resolve => { complete = resolve; })); const screen = await render(f.element());
  await screen.findByText('Saved driver: Rod'); await fireEvent.press(screen.getByText('Use ChromeDP…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  f.state.active = false; await screen.rerender(f.element()); expect(f.save.mock.calls[0][2].signal.aborted).toBe(true);
  await act(async () => { complete(status({ configured_driver: 'chromedp', driver: 'chromedp' })); });
  expect(screen.getByText('Saved driver: Rod')).toBeTruthy(); f.state.active = true; await screen.rerender(f.element());
  await waitFor(() => expect(screen.getByRole('button', { name: 'Use ChromeDP…' })).toBeDisabled()); expect(f.save).toHaveBeenCalledTimes(1);
  await screen.unmount(); f.query.clear();
});
