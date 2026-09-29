import { act, fireEvent, render, waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReloadEdit } from '@whip/protocol';
import { ReloadControls } from './reload-controls';
import { nativeFixture } from '../../test/native-fixtures';
import type { DeliveryState } from '../runtime/commands';
let mockRuntime: any;
jest.mock('expo-crypto', () => ({ randomUUID: () => 'mobile-edit' }));
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
function fixture(initial = true, child = false) {
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } });
  const session = { ...nativeFixture('Session'), id: child ? 'child' : 'root', parent_id: child ? 'root' : null, tree_id: 'tree' };
  let outcome: ReloadEdit = { ...nativeFixture('ReloadEdit'), id: 'reload', session_id: 'root', tree_id: 'tree' };
  const command = (id = 'reload'): DeliveryState => ({ record: { version: 4, runtimeId: 'runtime', clientId: 'phone', sessionId: 'root', rootId: 'root', commandId: id, operation: 'sessions.reload', requestHash: 'a'.repeat(64) }, intent: {}, knownAccepted: true, status: 'accepted', retryable: false });
  const get = jest.fn(async () => structuredClone(outcome)), cancel = jest.fn(async () => { outcome = { ...outcome, state: 'interrupted', settled_at: '2026-09-28T00:00:00Z' }; return structuredClone(outcome); });
  const client = { runtimeID: 'runtime', processEpoch: 'boot', clientID: 'phone', session: () => ({ reloads: { get, cancel } }) };
  const state = { ready: true, active: true, client, commands: initial ? [command()] : [] };
  const run = jest.fn(async (_method: string, params: { edit_id: string }) => { outcome = { ...outcome, id: params.edit_id }; state.commands = [command(params.edit_id)]; return structuredClone(outcome); });
  const check = jest.fn(async (value: DeliveryState): Promise<DeliveryState> => ({ ...value, status: 'accepted' as const })), forget = jest.fn(async () => { state.commands = []; });
  const refresh = jest.fn(async () => {}), view = { refresh };
  mockRuntime = { query, getSnapshot: () => ({ ...state }), requireReady: () => { if (!state.active || !state.ready) throw new Error('Offline'); return state.client; }, isBlocked: () => false, run, checkCommand: check, forgetCommand: forget };
  return { state, query, session, get, cancel, run, check, refresh, forget, setOutcome: (patch: Partial<ReloadEdit>) => { outcome = { ...outcome, ...patch }; },
    element: () => <QueryClientProvider client={query}><ReloadControls rootId="root" session={session} view={view} online={state.ready && state.active} /></QueryClientProvider> };
}
afterEach(() => jest.restoreAllMocks());
test('capture keeps exact revision and accepted pending does not install settings or refresh history as applied', async () => {
  const f = fixture(false), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await fireEvent.press(screen.getByText('Capture current host settings…')); expect(f.run).not.toHaveBeenCalled();
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.run).toHaveBeenCalledWith('sessions.reload', { session_id: 'root', edit_id: expect.any(String), expected_revision: '9007199254740993' }, { rootId: 'root' });
  await waitFor(() => expect(screen.getByText('Reload: pending')).toBeTruthy());
  expect(f.refresh).not.toHaveBeenCalled(); expect(screen.getByRole('button', { name: 'Capture current host settings…' })).toBeDisabled();
  expect(f.session.config_revision).toBe('9007199254740993'); await screen.unmount(); f.query.clear();
});
test('explicit receipt check distinguishes applied and preserves exact revision beyond Number precision', async () => {
  const f = fixture(), screen = await render(f.element()); await screen.findByText('Reload: pending');
  f.setOutcome({ state: 'applied', revision: '9007199254740994', settled_at: '2026-09-28T00:00:00Z' });
  await fireEvent.press(screen.getByText('Check captured reload'));
  await screen.findByText('Reload: applied'); expect(screen.getByText('Expected configuration 9007199254740993 · applied configuration 9007199254740994')).toBeTruthy();
  expect(f.refresh).toHaveBeenCalledTimes(1); expect(f.run).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Clear inspected reload record')); expect(f.forget).toHaveBeenCalledTimes(1);
  await screen.unmount(); f.query.clear();
});
test('cancellation freshly checks the exact receipt and a lost reply requires another read before cancel', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element()); await screen.findByText('Reload: pending');
  f.cancel.mockRejectedValueOnce(new Error('cancel reply lost'));
  await fireEvent.press(screen.getByText('Cancel this captured reload…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  expect(f.check).toHaveBeenCalledTimes(1); expect(f.cancel).toHaveBeenCalledWith('reload', { signal: expect.any(AbortSignal) });
  expect(screen.getByRole('button', { name: 'Cancel this captured reload…' })).toBeDisabled();
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.cancel).toHaveBeenCalledTimes(1);
  f.setOutcome({ state: 'interrupted', settled_at: '2026-09-28T00:00:00Z' });
  await fireEvent.press(screen.getByText('Check captured reload')); await screen.findByText('Reload: interrupted');
  expect(f.cancel).toHaveBeenCalledTimes(1); expect(f.run).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
test('a pending reload that applies before cancellation is never cancelled or replayed', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element()); await screen.findByText('Reload: pending');
  await fireEvent.press(screen.getByText('Cancel this captured reload…')); f.setOutcome({ state: 'applied', revision: '9007199254740994', settled_at: '2026-09-28T00:00:00Z' });
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); await screen.findByText('Reload: applied'); expect(f.cancel).not.toHaveBeenCalled(); expect(f.run).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
test('child reads are explicit root receipts and cannot request or cancel root reloads', async () => {
  const f = fixture(true, true), screen = await render(f.element()); await screen.findByText('Reload: pending');
  expect(f.get).toHaveBeenCalledWith('reload', { signal: expect.any(AbortSignal) });
  expect(screen.getByRole('button', { name: 'Capture current host settings…' })).toBeDisabled(); expect(screen.getByRole('button', { name: 'Cancel this captured reload…' })).toBeDisabled();
  await screen.unmount(); f.query.clear();
});
test('failed exact request verification cannot authorize cancellation even with a pending identity read', async () => {
  const f = fixture(), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element()); await screen.findByText('Reload: pending');
  f.check.mockImplementationOnce(async value => ({ ...value, status: 'unknown', message: 'Request mismatch' }));
  await fireEvent.press(screen.getByText('Cancel this captured reload…')); await act(async () => { alert.mock.calls[0][2]![1].onPress!(); });
  await screen.findByText('Request mismatch'); expect(f.cancel).not.toHaveBeenCalled(); await screen.unmount(); f.query.clear();
});
test('old host and unmounted confirmations cannot dispatch a captured reload', async () => {
  const f = fixture(false), alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {}), screen = await render(f.element());
  await fireEvent.press(screen.getByText('Capture current host settings…')); f.state.client = { ...f.state.client };
  await act(async () => { alert.mock.calls[0][2]![1].onPress!(); }); expect(f.run).not.toHaveBeenCalled();
  await screen.rerender(f.element()); await fireEvent.press(screen.getByText('Capture current host settings…')); await screen.unmount();
  await act(async () => { alert.mock.calls[1][2]![1].onPress!(); }); expect(f.run).not.toHaveBeenCalled(); f.query.clear();
});
