import { act, fireEvent, render, waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComputerStatus } from '@whip/protocol';
import { ComputerSettings } from './computer-settings';
let mockRuntime: any, mockFocused = true;
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('expo-router', () => ({ useIsFocused: () => mockFocused }));
jest.mock('@expo/ui', () => {
  const React = require('react'), { View, Text, Pressable } = require('react-native');
  return { Host: View, Column: View, Button: ({ label, onPress, disabled }: any) => React.createElement(Pressable, { onPress, disabled }, React.createElement(Text, {}, label)) };
});
const status = (): ComputerStatus => ({ revision: 'a'.repeat(64), generation: 'generation', state: 'disabled', native_configured: false, platform_supported: true, bundled_available: true, configuration: { enabled: false, helper_executable: '', allow: [], deny: ['private.app'], default_deny: true } });
function fixture(value = status()) {
  mockFocused = true; const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } });
  const read = jest.fn(async () => structuredClone(value)), publish = jest.fn(async () => ({ ...value, native_configured: true, revision: 'b'.repeat(64), configuration: { ...value.configuration, helper_executable: '/private/runtime/helper' } }));
  const client = { runtimeID: 'runtime', processEpoch: 'epoch', computerStatus: read, useBundledComputer: publish }, state = { ready: true, active: true, client, host: { id: 'host', name: 'Host' } };
  mockRuntime = { getSnapshot: () => ({ ...state }), requireReady: () => state.client, query };
  return { query, read, publish, state, element: () => <QueryClientProvider client={query}><ComputerSettings /></QueryClientProvider> };
}
afterEach(() => jest.restoreAllMocks());
test('status reads never publish or enable; explicit publication keeps disabled policy and exact revision', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await waitFor(() => expect(screen.getByText('Computer control disabled')).toBeTruthy()); expect(f.publish).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Use bundled helper…')); expect(f.publish).not.toHaveBeenCalled(); expect(alert.mock.calls[0][1]).toContain('stays disabled');
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.publish).toHaveBeenCalledWith('a'.repeat(64), { signal: expect.any(AbortSignal) }); expect(screen.getByText('Computer control disabled')).toBeTruthy(); expect(screen.getByText('Denied apps: private.app')).toBeTruthy();
  await screen.unmount(); f.query.clear();
});
test.each([{ platform_supported: false }, { bundled_available: false }, { configuration: { ...status().configuration, helper_executable: '/chosen/helper' } }])('unavailable or configured helper cannot be implicitly replaced (%p)', async patch => {
  const f = fixture({ ...status(), ...patch }), screen = await render(f.element()); await waitFor(() => expect(f.read).toHaveBeenCalled());
  expect(screen.getByRole('button', { name: 'Use bundled helper…' })).toBeDisabled(); expect(f.publish).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
test('lost publication blocks stale confirmation until an explicit successful status read', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element()); f.publish.mockRejectedValueOnce(new Error('Lost response'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Use bundled helper…' })).not.toBeDisabled()); await fireEvent.press(screen.getByText('Use bundled helper…'));
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.publish).toHaveBeenCalledTimes(1);
  f.read.mockRejectedValueOnce(new Error('Read failed')); await fireEvent.press(screen.getByText('Read current computer status')); expect(screen.getByRole('button', { name: 'Use bundled helper…' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Read current computer status')); await waitFor(() => expect(screen.getByRole('button', { name: 'Use bundled helper…' })).not.toBeDisabled()); expect(f.publish).toHaveBeenCalledTimes(1); await screen.unmount(); f.query.clear();
});
test('a confirmation cannot act after host replacement or after leaving the screen', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await waitFor(() => expect(screen.getByRole('button', { name: 'Use bundled helper…' })).not.toBeDisabled()); await fireEvent.press(screen.getByText('Use bundled helper…'));
  f.state.client = { ...f.state.client }; await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.publish).not.toHaveBeenCalled();
  await screen.rerender(f.element()); await fireEvent.press(screen.getByText('Use bundled helper…')); await screen.unmount(); await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.publish).not.toHaveBeenCalled(); f.query.clear();
});

test('a delayed status response cannot be attached to a replacement client', async () => {
  const f = fixture(); let complete!: (value: ComputerStatus) => void;
  f.read.mockImplementationOnce(() => new Promise(resolve => { complete = resolve; }));
  const screen = await render(f.element()); await waitFor(() => expect(f.read).toHaveBeenCalledTimes(1));
  f.state.client = { ...f.state.client }; await act(async () => { complete(status()); });
  await waitFor(() => expect(screen.getByText('Host changed while reading computer status.')).toBeTruthy());
  expect(screen.queryByText('No helper configured')).toBeNull(); expect(f.publish).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
