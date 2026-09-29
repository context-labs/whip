import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { RecoveryJournal, RemoteError, type RecoveryRecord, type WorkspaceAction } from '@whip/sdk';
import { assertValid, type ContractTypes } from '@whip/protocol';
import fixtures from '../../protocol/schema/fixtures.json';
import { RecoverySettings, recoveryStatus } from '../src/settings/recovery';
import { CompositionStore } from '../src/compositions';
import { SessionTabs, welcomeDraftKey } from '../src/session-tabs';
import { providerFixture, sessionRecord } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
function wire<T extends keyof ContractTypes>(name: T, match: (value: ContractTypes[T]) => boolean = () => true): ContractTypes[T] {
  const value: unknown = structuredClone(fixtures.find(item => item.type === name && item.valid && match(item.value as unknown as ContractTypes[T]))?.value);
  assertValid(name, value); return value;
}
async function fixture() {
  const f = await providerFixture();
  const records = new Map<string, RecoveryRecord>();
  const journal = new RecoveryJournal({
    list: async (_namespace, limit) => [...records.values()].slice(0, limit),
    put: async (_namespace, key, record) => { records.set(key, structuredClone(record)); },
    delete: async (_namespace, key) => { records.delete(key); },
  });
  const listeners = new Set<() => void>();
  let state = { hosts: [{ name: 'Workstation', state: 'connected', runtimeId: f.client.runtimeID, client: f.client }] };
  Object.assign(f.runtime, { recovery: journal, getSnapshot: () => state, subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); } });
  const params = { ...wire('SubmitParams'), identity: { client_id: f.client.clientID, request_id: 'saved' } };
  const admission = wire('Admission'); admission.receipt.identity = params.identity;
  const command = f.client.command('sessions.submit', params, { journal });
  await journal.put(command.record);
  return { ...f, journal, records, params, admission, command,
    disconnected() { act(() => { state = { hosts: [] }; listeners.forEach(listener => listener()); }); },
  };
}
it('opens local records without host reads and checks exact payload acceptance without replay', async () => {
  const f = await fixture(); f.data.handlers['receipts.match'] = () => f.admission;
  f.mount(<RecoverySettings />); await screen.findByText('sessions.submit · Workstation');
  expect(f.calls.map(call => call.method)).toEqual(['initialize']);
  fireEvent.click(screen.getByRole('button', { name: 'Check delivery' }));
  await screen.findByText('Accepted request; its session was deleted.');
  expect(f.count('sessions.submit')).toBe(0); expect((await f.journal.list())[0]?.accepted).toBe(true);
  const request = f.calls.find(call => call.method === 'receipts.match')!;
  assertValid('MatchReceiptParams', request.params);
  expect(JSON.parse(atob(request.params.params_base64))).toEqual(f.params);
});
it('shows a payload collision without marking acceptance or sending the mutation', async () => {
  const f = await fixture(); f.data.handlers['receipts.match'] = () => { throw new RemoteError({ code: -32002, kind: 'CONFLICT', message: 'Identity already has a different payload' }); };
  f.mount(<RecoverySettings />); fireEvent.click(await screen.findByRole('button', { name: 'Check delivery' }));
  await screen.findByText(/Identity already has a different payload/);
  expect((await f.journal.list())[0]?.accepted).toBe(false); expect(f.count('sessions.submit')).toBe(0);
});
it('distinguishes absent receipts and requires confirmation before resending the original request', async () => {
  const f = await fixture(); let accepted = false;
  f.data.handlers['receipts.match'] = () => { if (!accepted) throw new RemoteError({ code: -32001, kind: 'NOT_FOUND', message: 'Missing' }); return f.admission; };
  f.data.handlers['sessions.submit'] = () => { accepted = true; return f.admission; };
  f.mount(<RecoverySettings />); fireEvent.click(await screen.findByRole('button', { name: 'Check delivery' }));
  await screen.findByText(/No matching receipt was found/);
  fireEvent.click(screen.getByRole('button', { name: 'Retry exact request…' }));
  const dialog = await screen.findByRole('dialog', { name: 'Retry this exact request?' });
  expect(f.count('sessions.submit')).toBe(0);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Retry exact request' }));
  await screen.findByText('Accepted request; its session was deleted.');
  expect(f.calls.find(call => call.method === 'sessions.submit')?.params).toEqual(f.params);
  expect(f.count('sessions.submit')).toBe(1);
});
it('keeps identity-only creation evidence unconfirmed', async () => {
  const f = await fixture(); await f.journal.forget(f.command.record);
  const params = wire('CreateTreeParams', value => value.engine === 'quickjs'), result = wire('CreateTreeResult'); params.creation_id = result.creation.id;
  await f.journal.put(f.client.command('trees.create', params).record);
  f.data.handlers['trees.creation'] = () => result;
  f.mount(<RecoverySettings />); fireEvent.click(await screen.findByRole('button', { name: 'Check delivery' }));
  await screen.findByText(/An identity receipt exists, but it does not verify this exact payload/);
  expect((await f.journal.list())[0]?.accepted).toBe(false); expect(f.count('trees.create')).toBe(0);
  expect(screen.queryByRole('button', { name: 'Restore created session' })).toBeNull();
});
it.each(['claimed', 'uncertain'] as const)('reports acknowledged workspace action %s without claiming success', async state => {
  const f = await fixture(); await f.journal.forget(f.command.record);
  const params = { session_id: 'root', action_id: 'capture', snapshot_id: 'snapshot' };
  const action: WorkspaceAction = { id: 'capture', session_id: 'root', snapshot_id: 'snapshot', kind: 'capture', state, failure: null, created_at: '2026-09-28T00:00:00Z', finished_at: null };
  await f.journal.put({ ...f.client.command('workspace.capture', params).record, accepted: true });
  f.data.handlers['workspace.action'] = () => action;
  f.mount(<RecoverySettings />); fireEvent.click(await screen.findByRole('button', { name: 'Check delivery' }));
  await screen.findByText(`Accepted workspace action · ${state}.`); expect(f.count('workspace.capture')).toBe(0);
});
it('makes unresolved forgetting explicit and removes only local tracking', async () => {
  const f = await fixture(); f.mount(<RecoverySettings />); fireEvent.click(await screen.findByRole('button', { name: 'Forget tracking…' }));
  const dialog = await screen.findByRole('dialog', { name: 'Forget command tracking?' });
  expect(within(dialog).getByText(/does not cancel or undo host work/)).toBeTruthy(); expect((await f.journal.list()).length).toBe(1);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Forget tracking' }));
  await screen.findByText('No saved commands.'); expect(f.calls.map(call => call.method)).toEqual(['initialize']);
});
it('disables remote actions when disconnected and clears earlier live outcome evidence', async () => {
  const f = await fixture(); f.data.handlers['receipts.match'] = () => f.admission;
  f.mount(<RecoverySettings />); fireEvent.click(await screen.findByRole('button', { name: 'Check delivery' })); await screen.findByText('Accepted request; its session was deleted.');
  f.disconnected(); await screen.findByText(/Host acknowledgement saved. Execution outcome has not been checked/);
  expect((screen.getByRole('button', { name: 'Check delivery' }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole('button', { name: 'Retry exact request…' }) as HTMLButtonElement).disabled).toBe(true);
});
it('shows invalid local records without contacting a host', async () => {
  const f = await fixture(); f.records.set('invalid', { ...f.command.record, request: '{"method":"accounts.inference.login","params":{}}' });
  f.mount(<RecoverySettings />); await screen.findByText(/Operation cannot be journaled or replayed/);
  await waitFor(() => expect(f.calls.map(call => call.method)).toEqual(['initialize']));
});


it('reports native steering receipts without confusing them with permission edits', () => {
  const evidence = wire('InputSteeringResult');
  expect(recoveryStatus({ state: 'found', evidence: { ...evidence, deleted: true } }, true)).toBe('Input steering accepted · session deleted.');
  expect(recoveryStatus({ state: 'found', evidence: { ...evidence, deleted: false, input: null } }, true)).toBe('Input steering accepted · waiting for consumption.');
});

it('restores only an explicitly checked accepted creation from Settings without sending its draft', async () => {
  const f = await fixture(); await f.journal.forget(f.command.record);
  const tabs = new SessionTabs(), tab = tabs.openNew({ runtimeId: f.client.runtimeID, cwd: '/project' });
  const drafts = new Map([[welcomeDraftKey(tab.id), 'Keep this unsent task']]);
  Object.assign(f.runtime, { tabs, compositions: new CompositionStore(() => true), connections: { isAttached: () => true },
    draft: (key: string) => drafts.get(key) ?? '', setDraft: (key: string, text: string) => { drafts.set(key, text); } });
  const params = { ...wire('CreateTreeParams', value => value.engine === 'quickjs'), creation_id: tab.id };
  const created = { ...wire('CreateTreeResult'), deleted: false, root: { ...sessionRecord('created'), parent_id: null, definition: params.definition },
    tree: { id: 'tree', engine: params.engine, metadata: params.metadata, revision: '1', created_at: '2026-09-28T00:00:00Z' },
    creation: { id: tab.id, root_id: 'created', tree_id: 'tree', created_at: '2026-09-28T00:00:00Z' } };
  f.data.handlers['trees.creation'] = () => created;
  await f.journal.put({ ...f.client.command('trees.create', params).record, accepted: true });
  f.mount(<RecoverySettings />); await screen.findByText('trees.create · Workstation');
  expect(screen.queryByRole('button', { name: 'Restore created session' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Check delivery' }));
  const restore = await screen.findByRole('button', { name: 'Restore created session' });
  expect(tabs.workspace().tabs[0]).toMatchObject({ kind: 'new' });
  fireEvent.click(restore);
  await screen.findByText('No saved commands.');
  expect(tabs.workspace().tabs[0]).toMatchObject({ id: tab.id, kind: 'chat', rootId: 'created' });
  expect(drafts.get('host:created:created')).toBe('Keep this unsent task');
  expect(f.count('trees.creation')).toBe(2); expect(f.count('trees.create')).toBe(0); expect(f.count('sessions.submit')).toBe(0);
});


it('checks and cancels an exact saved pending reload after reopening without resending it', async () => {
  const f = await fixture(); await f.journal.forget(f.command.record);
  let receipt: ContractTypes['ReloadEdit'] = { ...wire('ReloadEdit'), state: 'pending', revision: null, settled_at: null };
  const params = { session_id: receipt.session_id, edit_id: receipt.id, expected_revision: receipt.expected_revision };
  await f.journal.put(f.client.command('sessions.reload', params).record);
  f.data.handlers['sessions.reload_edit'] = () => receipt;
  f.data.handlers['sessions.cancel_reload'] = request => { expect(request.params).toEqual({ session_id: receipt.session_id, edit_id: receipt.id }); receipt = { ...receipt, state: 'interrupted', settled_at: '2026-09-28T00:00:00Z' }; return receipt; };
  f.mount(<RecoverySettings />); await screen.findByText('sessions.reload · Workstation');
  expect(f.count('sessions.reload_edit')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Check delivery' })); await screen.findByText('Reload pending.');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel pending reload' })); await screen.findByText('Reload interrupted.');
  expect(f.count('sessions.reload')).toBe(0); expect(f.count('sessions.reload_edit')).toBe(3); expect(f.count('sessions.cancel_reload')).toBe(1);
  expect((await f.journal.list()).length).toBe(1);
});
