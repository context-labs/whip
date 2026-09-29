import { act, fireEvent, render } from '@testing-library/react-native';
import { Alert } from 'react-native';
import type { Session } from '@whip/protocol';
import type { SessionView } from '@whip/sdk/state';
import { SessionControls } from './session-controls';
import { nativeFixture } from '../../test/native-fixtures';
let mockRuntime: any, mockObserved: any, mockPolicy: any;
const mockPush = jest.fn(), mockTrace = jest.fn();
jest.mock('expo-crypto', () => ({ randomUUID: () => 'mobile-edit' }));
jest.mock('expo-router', () => ({ router: { push: (...args: unknown[]) => mockPush(...args) } }));
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('@tanstack/react-query', () => ({ useQuery: (options: { queryKey: unknown[] }) => ({ data: options.queryKey.includes('permission-mode') ? mockPolicy : undefined, refetch: async () => {} }) }));
jest.mock('@whip/sdk/react', () => ({ useSessionView: () => mockObserved, useTraceView: (view: any) => view.getSnapshot() }));
jest.mock('@whip/sdk/state', () => ({ createTraceView: (...args: unknown[]) => mockTrace(...args) }));
function fixture(root = false) {
  mockPush.mockClear(); mockTrace.mockReset();
  const lifecycle = jest.fn(async () => ({}));
  const client = { session: () => ({ lifecycle }), runtimeID: 'runtime', clientID: 'phone', processEpoch: 'boot' }, state = { ready: true, active: true, commands: [], client, host: { id: 'host', runtimeId: 'runtime' } };
  mockObserved = { activity: { lifecycle: 'stopped', active_turn: null }, status: 'live', sessionID: 'child', unavailable: false, history: { messages: [{ group_id: 'earlier', sequence: '9007199254740992' }, { group_id: 'later', sequence: '9007199254740993' }], snapshot: { session_id: 'child', revision: '9007199254740995', through_sequence: '9007199254740993' } } };
  mockPolicy = { ...nativeFixture('PermissionPolicy'), tree_id: 'tree', revision: '9007199254740997', mode: 'prompt', deny_interactive: false };
  const run = jest.fn(async (_method: string, _params: unknown, _options: unknown) => ({ root: { id: 'new-root' }, deleted: false }));
  mockRuntime = { getSnapshot: () => ({ ...state }), requireReady: () => { if (!state.ready) throw new Error('Offline'); return state.client; }, isBlocked: () => false, run, report: jest.fn(), query: { refetchQueries: jest.fn(async () => {}) } };
  const session: Session = { ...nativeFixture('Session'), id: root ? 'root' : 'child', parent_id: root ? null : 'root', lifecycle: 'stopped', tree_id: 'tree', config_revision: '9007199254740999' }, view = { getSnapshot: () => mockObserved, refresh: async () => {} } as SessionView;
  return { state, run, lifecycle, element: () => <SessionControls rootId="root" session={session} view={view} /> };
}
afterEach(() => jest.restoreAllMocks());
test('fork uses the exact child snapshot and navigates only to the returned root', async () => {
  const f = fixture(), screen = await render(f.element()); await fireEvent.changeText(screen.getByLabelText('Keep through message sequence'), '9007199254740992'); await fireEvent.press(screen.getByText('Fork through this message'));
  expect(f.run).toHaveBeenCalledWith('sessions.fork', expect.objectContaining({ session_id: 'child', expected_history_revision: '9007199254740995', expected_config_revision: '9007199254740999', observed_through: '9007199254740993', keep_through: '9007199254740992' }), { rootId: 'root' });
  expect(mockPush).toHaveBeenCalledWith({ pathname: '/session/[rootId]', params: { rootId: 'new-root', runtimeId: 'runtime', hostId: 'host' } });
});
test('confirmed root permission mode preserves exact CAS while a stale confirmation cannot switch hosts', async () => {
  const f = fixture(true), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await fireEvent.press(screen.getByText('Use automatic mode…')); expect(f.run).not.toHaveBeenCalled();
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.run).toHaveBeenCalledWith('permissions.set_mode', expect.objectContaining({ session_id: 'root', expected_revision: '9007199254740997', mode: 'automatic' }), { rootId: 'root' });
  await fireEvent.press(screen.getByText('Use automatic mode…')); f.state.client = { ...f.state.client };
  await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.run).toHaveBeenCalledTimes(1); expect(mockRuntime.report).toHaveBeenCalledWith(expect.objectContaining({ message: 'Host changed. Reopen these controls.' }));
});
test('uncertain rewind remains visible to recovery without an automatic second request', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element()); f.run.mockRejectedValueOnce(new Error('Lost response'));
  await fireEvent.press(screen.getByText('Rewind through this message…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.run).toHaveBeenCalledWith('sessions.rewind', expect.objectContaining({ session_id: 'child', expected_revision: '9007199254740995', keep_through: '9007199254740993' }), { rootId: 'root' });
  expect(f.run).toHaveBeenCalledTimes(1); expect(mockRuntime.report).toHaveBeenCalledWith(expect.objectContaining({ message: 'Lost response' }));
});
test('trace inspection has one bounded SDK owner, pauses in background and joins disposal', async () => {
  const f = fixture(), held = { start: jest.fn(async () => {}), suspend: jest.fn(async () => {}), dispose: jest.fn(async () => {}), latest: jest.fn(async () => {}), latestRoots: jest.fn(async () => {}), getSnapshot: () => ({ status: 'live', roots: [], rows: [] }) };
  mockTrace.mockReturnValue(held); const screen = await render(f.element()); expect(mockTrace).not.toHaveBeenCalled(); await fireEvent.press(screen.getByText('Inspect execution traces'));
  expect(mockTrace).toHaveBeenCalledWith(f.state.client, 'root', { maxRows: 64, maxRoots: 16, maxBytes: 256 << 10, pollIntervalMs: 2000 }); expect(held.start).toHaveBeenCalledTimes(1);
  f.state.active = false; await screen.rerender(f.element()); expect(held.suspend).toHaveBeenCalledTimes(1); await screen.unmount(); expect(held.dispose).toHaveBeenCalledTimes(1);
});

test('a lost stop response requires an explicit status read, never automatic resume or rewind', async () => {
  const f = fixture(); mockObserved.activity.lifecycle = 'active'; f.lifecycle.mockRejectedValueOnce(new Error('Lost response'));
  const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await fireEvent.press(screen.getByText('Stop this recipient…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.lifecycle).toHaveBeenCalledWith('stopped'); expect(f.lifecycle).toHaveBeenCalledTimes(1); expect(f.run).not.toHaveBeenCalled();
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.lifecycle).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('button', { name: 'Stop this recipient…' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Read current recipient status')); expect(f.lifecycle).toHaveBeenCalledTimes(1);
  expect(mockRuntime.query.refetchQueries).toHaveBeenCalledWith({ queryKey: ['runtime', 'session-metadata', 'root', 'child'], type: 'active' }, { throwOnError: true });
});

test.each([false, true])('interactive denial %s changes independently of automatic mode with exact root CAS', async initial => {
  const f = fixture(true); mockPolicy.mode = 'automatic'; mockPolicy.deny_interactive = initial;
  const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await fireEvent.press(screen.getByText(initial ? 'Allow interactive requests…' : 'Deny interactive requests…'));
  expect(f.run).not.toHaveBeenCalled(); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.run).toHaveBeenCalledWith('permissions.set_denial', expect.objectContaining({ session_id: 'root', expected_revision: '9007199254740997', deny_interactive: !initial }), { rootId: 'root' });
  expect(f.run.mock.calls[0][1]).not.toHaveProperty('mode');
});
test('child policy controls are read-only even when its tree policy can be inspected', async () => {
  const f = fixture(), screen = await render(f.element());
  expect(screen.getByRole('button', { name: 'Use automatic mode…' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Deny interactive requests…' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Capture current host settings…' })).toBeDisabled();
  expect(f.run).not.toHaveBeenCalled();
});
