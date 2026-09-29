import { act, fireEvent, render, waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { assertValid, type ExternalBrowserStatus, type ExternalBrowserSession, type Session } from '@whip/protocol';
import { nativeFixture } from '../../test/native-fixtures';
import { ExternalBrowserSettings } from './external-browser-settings';
import { ExternalBrowserConnections } from './external-browser-connections';
let mockRuntime: ReturnType<typeof fixture>['runtime'], mockFocused = true;
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('expo-router', () => ({ useIsFocused: () => mockFocused }));
jest.mock('@expo/ui', () => {
  const React = require('react'), { View, Text, Pressable } = require('react-native');
  return { Host: View, Column: View, Button: ({ label, onPress, disabled }: { label: string; onPress(): void; disabled: boolean }) => React.createElement(Pressable, { onPress, disabled }, React.createElement(Text, {}, label)) };
});
const status = (patch: Partial<ExternalBrowserStatus> = {}): ExternalBrowserStatus => {
  const value: ExternalBrowserStatus = { revision: 'a'.repeat(64), driver: 'rod', driver_pinned: false, configuration: { mode: 'disabled', executable: '', live_endpoint: '', live_profile: '', allow_private_urls: false }, ...patch };
  assertValid('ExternalBrowserStatus', value); return value;
};
const entry = (patch: Partial<ExternalBrowserSession> = {}): ExternalBrowserSession => {
  const value: ExternalBrowserSession = { root_id: 'root', name: 'default', mode: 'headless', driver: 'rod', state: 'connected', generation: 'old-generation', resource: 'browser-external:' + 'a'.repeat(64), ...patch };
  assertValid('ExternalBrowserSession', value); return value;
};
function fixture() {
  mockFocused = true; const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } });
  const read = jest.fn(async () => status()), save = jest.fn(async (_revision: string, configuration: ExternalBrowserStatus['configuration'], _options: { signal: AbortSignal }) => status({ configuration, revision: 'b'.repeat(64) }));
  const list = jest.fn(async (_session: string, _options: { signal: AbortSignal }) => ({ items: [entry()] }));
  const reconnect = jest.fn(async (_root: string, _name: string, _generation: string, _options: { signal: AbortSignal }) => entry({ generation: 'new-generation', state: 'prepared' }));
  const disconnect = jest.fn(async (_root: string, _name: string, _generation: string, _options: { signal: AbortSignal }) => entry({ state: 'ended' }));
  const client = { runtimeID: 'runtime', processEpoch: 'epoch', hosts: { externalBrowser: read, setExternalBrowser: save, externalBrowserSessions: list, reconnectExternalBrowser: reconnect, disconnectExternalBrowser: disconnect } };
  const state = { ready: true, active: true, client };
  const runtime = { getSnapshot: () => ({ ...state }), requireReady: () => { if (!state.active || !state.ready) throw new Error('Offline'); return state.client; }, query }; mockRuntime = runtime;
  const session: Session = { ...nativeFixture('Session'), id: 'root', parent_id: null, lifecycle: 'active' };
  return { runtime, query, read, save, list, reconnect, disconnect, state, session,
    settings: () => <QueryClientProvider client={query}><ExternalBrowserSettings/></QueryClientProvider>,
    connections: (child = false) => <QueryClientProvider client={query}><ExternalBrowserConnections rootId="root" session={child ? { ...session, id: 'child', parent_id: 'root' } : session} online={state.active}/></QueryClientProvider> };
}
afterEach(() => jest.restoreAllMocks());
test('explicit mobile confirmation captures complete mode declaration and exact host revision without starting a connection', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.settings());
  await screen.findByText('Effective driver: rod'); await fireEvent.press(screen.getByRole('radio', { name: 'Headless Chrome' }));
  await fireEvent.changeText(screen.getByLabelText('Chrome executable'), '/host/Chrome'); await fireEvent.press(screen.getByText('Save external Chrome settings…'));
  expect(f.save).not.toHaveBeenCalled(); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.save).toHaveBeenCalledWith('a'.repeat(64), { mode: 'headless', executable: '/host/Chrome', live_endpoint: '', live_profile: '', allow_private_urls: false }, { signal: expect.any(AbortSignal) });
  await screen.findByText(/External Chrome settings saved/); expect(f.list).not.toHaveBeenCalled(); expect(f.reconnect).not.toHaveBeenCalled(); expect(f.disconnect).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
test('lost save reply preserves the edited draft and blocks repeated confirmation until a successful explicit read', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.settings());
  await screen.findByText('Effective driver: rod'); await fireEvent.press(screen.getByRole('radio', { name: 'Headless Chrome' })); await fireEvent.changeText(screen.getByLabelText('Chrome executable'), '/retained/draft');
  f.save.mockRejectedValueOnce(new Error('Reply lost')); await fireEvent.press(screen.getByText('Save external Chrome settings…'));
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.save).toHaveBeenCalledTimes(1);
  expect(screen.getByLabelText('Chrome executable')).toHaveDisplayValue('/retained/draft'); expect(screen.getByRole('button', { name: 'Save external Chrome settings…' })).toBeDisabled();
  f.read.mockRejectedValueOnce(new Error('Read unavailable')); await fireEvent.press(screen.getByText('Discard edits and read external Chrome settings')); await screen.findAllByText('Read unavailable');
  expect(screen.getByRole('button', { name: 'Save external Chrome settings…' })).toBeDisabled();
  f.read.mockResolvedValueOnce(status({ revision: 'c'.repeat(64) })); await fireEvent.press(screen.getByText('Discard edits and read external Chrome settings')); await screen.findByText('Current settings loaded. No change was replayed.');
  expect(screen.getByRole('radio', { name: 'Disabled' })).toBeChecked(); expect(f.save).toHaveBeenCalledTimes(1); await screen.unmount(); f.query.clear();
});
test('background cancellation rejects a late settings reply and a stale host confirmation never dispatches', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}); let complete!: (value: ExternalBrowserStatus) => void;
  f.save.mockImplementationOnce(() => new Promise(resolve => { complete = resolve; })); const screen = await render(f.settings());
  await screen.findByText('Effective driver: rod'); await fireEvent.press(screen.getByRole('radio', { name: 'Chrome extension' })); await fireEvent.press(screen.getByText('Save external Chrome settings…'));
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); f.state.active = false; await screen.rerender(f.settings()); expect(f.save.mock.calls[0][2].signal.aborted).toBe(true);
  await act(async () => { complete(status()); }); f.state.active = true; await screen.rerender(f.settings()); expect(screen.getByRole('radio', { name: 'Chrome extension' })).toBeChecked(); expect(screen.getByRole('button', { name: 'Save external Chrome settings…' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Discard edits and read external Chrome settings')); await screen.findByText('Current settings loaded. No change was replayed.'); await fireEvent.press(screen.getByRole('radio', { name: 'Chrome extension' })); await fireEvent.press(screen.getByText('Save external Chrome settings…'));
  f.state.client = { ...f.state.client }; await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.save).toHaveBeenCalledTimes(1); await screen.unmount(); f.query.clear();
});
test('unmounted settings confirmation cannot send and pinned driver metadata remains distinct from mode selection', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}); f.read.mockResolvedValue(status({ driver: 'chromedp', driver_pinned: true })); const screen = await render(f.settings());
  await screen.findByText('Effective driver: chromedp (pinned by this host process)'); await fireEvent.press(screen.getByRole('radio', { name: 'Existing Chrome' })); await fireEvent.changeText(screen.getByLabelText('Live Chrome endpoint'), 'http://127.0.0.1:1234'); await fireEvent.changeText(screen.getByLabelText('Live Chrome profile'), '/exact/profile');
  expect(screen.getByLabelText('Live Chrome endpoint')).toHaveDisplayValue(''); await fireEvent.press(screen.getByText('Save external Chrome settings…')); await screen.unmount(); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.save).not.toHaveBeenCalled(); f.query.clear();
});
test('root connection actions capture exact generation; lost reply requires a fresh read without replay', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.connections());
  await screen.findByText('Generation: old-generation'); await fireEvent.press(screen.getByText('Reconnect default…')); f.reconnect.mockRejectedValueOnce(new Error('Reply lost')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.reconnect).toHaveBeenCalledWith('root', 'default', 'old-generation', { signal: expect.any(AbortSignal) }); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.reconnect).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('button', { name: 'Reconnect default…' })).toBeDisabled(); f.list.mockResolvedValueOnce({ items: [entry({ generation: 'fresh-generation', state: 'prepared' })] });
  await fireEvent.press(screen.getByText('Read current external connections')); await screen.findByText('Generation: fresh-generation'); expect(f.reconnect).toHaveBeenCalledTimes(1);
  await fireEvent.press(screen.getByText('Disconnect default…')); await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.disconnect).toHaveBeenCalledWith('root', 'default', 'fresh-generation', { signal: expect.any(AbortSignal) }); await screen.unmount(); f.query.clear();
});
test('child connection view is read-only and foreign root metadata is rejected', async () => {
  const f = fixture(), screen = await render(f.connections(true)); await screen.findByText('Generation: old-generation'); expect(f.list).toHaveBeenCalledWith('child', { signal: expect.any(AbortSignal) });
  expect(screen.queryByRole('button', { name: 'Reconnect default…' })).toBeNull(); expect(screen.queryByRole('button', { name: 'Disconnect default…' })).toBeNull();
  f.list.mockResolvedValueOnce({ items: [entry({ root_id: 'foreign' })] }); await fireEvent.press(screen.getByText('Read current external connections')); await screen.findAllByText('Browser metadata belongs to another root.'); expect(f.reconnect).not.toHaveBeenCalled(); expect(f.disconnect).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
test('connection owner replacement and foreground loss cannot apply late acknowledgement or replay a mutation', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}); let complete!: (value: ExternalBrowserSession) => void;
  f.reconnect.mockImplementationOnce(() => new Promise(resolve => { complete = resolve; })); const screen = await render(f.connections()); await screen.findByText('Generation: old-generation'); await fireEvent.press(screen.getByText('Reconnect default…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  f.state.active = false; await screen.rerender(f.connections()); expect(f.reconnect.mock.calls[0][3].signal.aborted).toBe(true); await act(async () => { complete(entry({ generation: 'late-generation' })); });
  f.state.active = true; await screen.rerender(f.connections()); await waitFor(() => expect(screen.getByRole('button', { name: 'Reconnect default…' })).toBeDisabled()); expect(screen.queryByText('Generation: late-generation')).toBeNull(); expect(f.reconnect).toHaveBeenCalledTimes(1);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Read current external connections' })).not.toBeDisabled()); await fireEvent.press(screen.getByText('Read current external connections')); await waitFor(() => expect(screen.getByRole('button', { name: 'Reconnect default…' })).not.toBeDisabled()); await fireEvent.press(screen.getByText('Reconnect default…')); f.state.client = { ...f.state.client }; await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.reconnect).toHaveBeenCalledTimes(1); await screen.unmount(); f.query.clear();
});
