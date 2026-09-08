import { act, fireEvent, render } from '@testing-library/react-native';
import { WhipError } from '@whip/sdk';
import type { MobileRuntime } from '../runtime/runtime';
import ServerScreen from '../app/server';

let mockRuntime: MobileRuntime;
const mockProbe = jest.fn();
const mockReplace = jest.fn();
const mockBack = jest.fn();
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('../runtime/connection-test', () => ({ ...jest.requireActual('../runtime/connection-test'), testConnection: (...args: unknown[]) => mockProbe(...args) }));
jest.mock('expo-router', () => ({ router: { replace: (...args: unknown[]) => mockReplace(...args), back: () => mockBack() }, useLocalSearchParams: () => ({}) }));
jest.mock('../theme/theme', () => ({ useTheme: () => ({ dark: true, colors: { foreground: '#fff', muted: '#aaa', panel: '#222', element: '#333', background: '#111', border: '#444', primary: '#55aaff', error: '#f55' } }) }));
jest.mock('@expo/ui', () => {
  const React = require('react'); const { View, Text, Pressable } = require('react-native');
  return { Host: View, Column: View,
    Button: ({ label, onPress, disabled, testID }: { label: string; onPress(): void; disabled?: boolean; testID?: string }) => React.createElement(Pressable, { onPress, disabled, testID }, React.createElement(Text, {}, label)) };
});
function fixture() {
  jest.clearAllMocks();
  let state = { hosts: [], active: true, host: undefined } as unknown as ReturnType<MobileRuntime['getSnapshot']>;
  const host = { id: 'host', url: 'https://host.example.ts.net', clientId: 'phone', name: 'Host' };
  const connect = jest.fn(async () => {});
  const detach = jest.fn(async () => {});
  mockRuntime = { getSnapshot: () => state, connect, detach, newHost: jest.fn(() => host), clearError: jest.fn(), report: jest.fn() } as unknown as MobileRuntime;
  mockProbe.mockResolvedValue({ runtimeId: 'runtime', empty: true });
  return { connect, detach, host, setState: (patch: Partial<typeof state>) => { state = { ...state, ...patch }; } };
}
async function open() {
  const screen = await render(<ServerScreen />);
  await fireEvent.changeText(screen.getByTestId('server-url'), 'https://host.example.ts.net');
  return screen;
}

test('Connect failure is visible inside the server sheet, including native error details', async () => {
  const f = fixture(); f.connect.mockRejectedValue(new WhipError('disconnected', 'WebSocket closed (1006): TLS rejected'));
  const screen = await open(); await fireEvent.press(screen.getByTestId('connect-server'));
  expect(screen.getByTestId('connection-error')).toBeTruthy();
  expect(screen.getByText('The live connection could not open')).toBeTruthy();
  expect(screen.getByText('Details: WebSocket closed (1006): TLS rejected')).toBeTruthy();
  expect(mockReplace).not.toHaveBeenCalled();
});

test('errors before host identity creation are no longer swallowed', async () => {
  fixture(); jest.mocked(mockRuntime.newHost).mockImplementation(() => { throw new Error('UUID unavailable'); });
  const screen = await open(); await fireEvent.press(screen.getByTestId('connect-server'));
  expect(screen.getByText('Details: UUID unavailable')).toBeTruthy(); expect(mockRuntime.connect).not.toHaveBeenCalled();
});

test('Test Connection reports an empty but healthy host without saving, switching or navigating', async () => {
  fixture(); const screen = await open(); await fireEvent.press(screen.getByTestId('test-connection'));
  expect(screen.getByText(/Connection test passed.*no sessions yet/)).toBeTruthy();
  expect(mockRuntime.connect).not.toHaveBeenCalled(); expect(mockRuntime.newHost).not.toHaveBeenCalled(); expect(mockReplace).not.toHaveBeenCalled();
  await fireEvent.changeText(screen.getByTestId('server-url'), 'https://another.example.ts.net');
  expect(screen.queryByText(/Connection test passed/)).toBeNull();
});

test('probe failure stays in the sheet and can be retried', async () => {
  fixture(); mockProbe.mockRejectedValueOnce(new WhipError('timeout', 'No reply within 15 seconds.'));
  const screen = await open(); await fireEvent.press(screen.getByTestId('test-connection'));
  expect(screen.getByText('The connection timed out')).toBeTruthy();
  await fireEvent.press(screen.getByTestId('test-connection'));
  expect(screen.queryByText('The connection timed out')).toBeNull(); expect(screen.getByText(/Connection test passed/)).toBeTruthy();
});

test('cancelling a test aborts it and ignores a late success', async () => {
  fixture(); let finish!: (result: unknown) => void;
  mockProbe.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  const screen = await open(); await fireEvent.press(screen.getByTestId('test-connection'));
  const signal = mockProbe.mock.calls[0][1].signal;
  await fireEvent.press(screen.getByText('Cancel attempt'));
  expect(signal.aborted).toBe(true); expect(mockRuntime.detach).not.toHaveBeenCalled();
  await act(async () => { finish({ runtimeId: 'runtime', empty: true }); });
  expect(screen.queryByText(/Connection test passed/)).toBeNull();
});

test('unmount closes probe observation without detaching the existing host', async () => {
  const f = fixture(); f.setState({ host: f.host }); mockProbe.mockImplementationOnce(() => new Promise(() => {}));
  const screen = await open(); await fireEvent.press(screen.getByTestId('test-connection'));
  const signal = mockProbe.mock.calls[0][1].signal;
  await screen.unmount(); expect(signal.aborted).toBe(true); expect(f.detach).not.toHaveBeenCalled();
});

test('background cancels a test and gives an explicit retry state on return', async () => {
  const f = fixture(); mockProbe.mockImplementationOnce(() => new Promise(() => {}));
  const screen = await open(); await fireEvent.press(screen.getByTestId('test-connection'));
  const signal = mockProbe.mock.calls[0][1].signal;
  f.setState({ active: false }); await screen.rerender(<ServerScreen />);
  expect(signal.aborted).toBe(true); expect(screen.getByText('Connection test paused')).toBeTruthy();
});

test('cancelling Connect detaches only its own pending host and ignores late completion', async () => {
  const f = fixture(); let finish!: () => void;
  f.connect.mockImplementationOnce(async () => { f.setState({ host: f.host }); return new Promise(resolve => { finish = resolve; }); });
  const screen = await open(); await fireEvent.press(screen.getByTestId('connect-server'));
  await fireEvent.press(screen.getByText('Cancel attempt')); expect(f.detach).toHaveBeenCalledTimes(1);
  await act(async () => { finish(); }); expect(mockReplace).not.toHaveBeenCalled();
});

test('successful Connect navigates and unmount does not disconnect it', async () => {
  const f = fixture(); const screen = await open(); await fireEvent.press(screen.getByTestId('connect-server'));
  expect(mockReplace).toHaveBeenCalledWith('/'); await screen.unmount(); expect(f.detach).not.toHaveBeenCalled();
});
