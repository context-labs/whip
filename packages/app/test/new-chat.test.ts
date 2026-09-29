import { expect, it, vi } from 'vitest';
import { DurableCommand, RecoveryJournal, type CreateTreeParams, type CreateTreeResult, type DurableMethod, type RecoveryRecord } from '@whip/sdk';
import fixtures from '../../protocol/schema/fixtures.json';
import { providerFixture, sessionRecord } from './provider-fixture';
import { startNewChat } from '../src/new-chat';
import { CompositionStore } from '../src/compositions';
import { SubmittedInputs } from '../src/input-presentation';
import { SessionTabs, welcomeDraftKey } from '../src/session-tabs';
import type { AppRuntime } from '../src/runtime';
const wire = (name: string) => structuredClone(fixtures.find(item => item.type === name && item.valid)!.value) as any;
async function fixture() {
  const f = await providerFixture();
  const records = new Map<string, RecoveryRecord>();
  const recovery = new RecoveryJournal({ list: async () => [...records.values()], put: async (_ns, key, record) => { records.set(key, record); }, delete: async (_ns, key) => { records.delete(key); } });
  const drafts = new Map<string, string>(); const tabs = new SessionTabs();
  const tab = tabs.openNew({ cwd: '/project' }); const source = welcomeDraftKey(tab.id);
  drafts.set(source, 'Original message');
  const params: Omit<CreateTreeParams, 'creation_id'> = { definition: { id: 'coding', revision: 'a'.repeat(64) }, engine: 'quickjs', working_directory: '/project', metadata: { title: null, pinned: false, archived: false }, permission_mode: 'prompt', overrides: { model: { provider: 'openrouter', name: 'fixture', effort: 'high' } } };
  const created = (): CreateTreeResult => ({ creation: { id: tab.id, root_id: 'created', tree_id: 'tree', created_at: '2026-09-28T00:00:00Z' }, root: { ...sessionRecord('created'), parent_id: null, definition: { ...params.definition } }, tree: { id: 'tree', engine: 'quickjs', revision: '9007199254740993', metadata: params.metadata, created_at: '2026-09-28T00:00:00Z' }, deleted: false });
  f.data.handlers['trees.create'] = created;
  f.data.handlers['trees.creation'] = created;
  f.data.handlers['sessions.submit'] = request => {
    if (request.method !== 'sessions.submit') throw new Error('wrong operation');
    const result = wire('Admission'); result.receipt.identity = request.params.identity; return result;
  };
  f.data.handlers['content.put'] = async request => {
    if (request.method !== 'content.put') throw new Error('wrong operation');
    const data = Uint8Array.from(atob(request.params.data_base64), c => c.charCodeAt(0));
    const digest = [...new Uint8Array(await crypto.subtle.digest('SHA-256', data))].map(v => v.toString(16).padStart(2, '0')).join('');
    return { id: request.params.reference_id, session_id: request.params.session_id, digest, size: String(data.length), media_type: request.params.media_type, created_at: '2026-09-28T00:00:00Z' };
  };
  const accepted: ((value: unknown) => void)[] = [];
  const handles: DurableCommand<DurableMethod>[] = [];
  const run = vi.fn(async (handle: DurableCommand<DurableMethod>, _label: string, onAccepted?: (value: any) => void) => {
    handles.push(handle); accepted.push(onAccepted ?? (() => {}));
    const value = await handle.send(); onAccepted?.(value); await handle.forget(); return value;
  });
  const runtime = {
    connections: { isAttached: vi.fn(() => true) }, recovery, tabs, compositions: new CompositionStore(() => true), submittedInputs: new SubmittedInputs(),
    getSnapshot: () => ({ commands: [] }), command: (client: typeof f.client, method: DurableMethod, params: any) => client.command(method, params, { journal: recovery }), run,
    draft: (key: string) => drafts.get(key) ?? '', setDraft: (key: string, text: string) => { drafts.set(key, text); }, report: vi.fn(),
  } as unknown as AppRuntime;
  return { ...f, runtime, run, handles, accepted, drafts, source, tab, params, created, recovery };
}
it('creates one exact immutable native root and hands off before admitting its first input', async () => {
  const f = await fixture();
  await startNewChat(f.runtime, f.client, f.tab.id, f.params);
  expect(f.calls.find(call => call.method === 'trees.create')?.params).toEqual({ ...f.params, creation_id: f.tab.id });
  expect(f.calls.find(call => call.method === 'sessions.submit')?.params).toMatchObject({ session_id: 'created', source: 'user', parts: [{ type: 'text', text: 'Original message' }], delivery: 'queued' });
  expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ kind: 'chat', rootId: 'created', runtimeId: 'host' });
  expect(f.runtime.draft(f.source)).toBe(''); expect(f.runtime.draft('host:created:created')).toBe('');
  expect(f.count('trees.create')).toBe(1); expect(f.count('sessions.submit')).toBe(1); expect(await f.recovery.list()).toEqual([]);
});
it('a first-input rejection keeps the created session and draft instead of creating a replacement', async () => {
  const f = await fixture(); f.data.handlers['sessions.submit'] = () => { throw new Error('Input refused'); };
  await startNewChat(f.runtime, f.client, f.tab.id, f.params);
  expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ kind: 'chat', rootId: 'created' });
  expect(f.runtime.draft('host:created:created')).toBe('Original message');
  expect(f.count('trees.create')).toBe(1); expect(f.runtime.report).toHaveBeenCalledOnce();
});
it('explicit receipt recovery after a lost creation ACK preserves a newer draft without sending it', async () => {
  const f = await fixture(); f.data.handlers['trees.create'] = () => { throw new Error('ACK lost'); };
  await expect(startNewChat(f.runtime, f.client, f.tab.id, f.params)).rejects.toThrow('ACK lost');
  expect(f.runtime.draft(f.source)).toBe('Original message');
  f.runtime.setDraft(f.source, 'Newer unsent draft');
  f.accepted[0]!(f.created());
  expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ kind: 'chat', rootId: 'created' });
  expect(f.runtime.draft('host:created:created')).toBe('Newer unsent draft');
  expect(f.count('sessions.submit')).toBe(0); expect(f.count('trees.create')).toBe(1);
});
it('a saved creation after reload blocks a fresh payload and retains its original immutable request', async () => {
  const f = await fixture();
  const command = f.client.command('trees.create', { ...f.params, creation_id: f.tab.id }, { journal: f.recovery });
  await f.recovery.put(command.record);
  await expect(startNewChat(f.runtime, f.client, f.tab.id, { ...f.params, working_directory: '/changed' })).rejects.toThrow('saved session creation');
  expect(f.count('trees.create')).toBe(0); expect((await f.recovery.list())[0]?.request).toBe(command.record.request);
  expect(f.runtime.draft(f.source)).toBe('Original message'); expect(f.runtime.compositions.get(f.source).sending).toBe(false);
});
it.each(['root', 'definition', 'deleted'])('rejects mismatched %s creation evidence without discarding the draft', async mismatch => {
  const f = await fixture(); f.data.handlers['trees.create'] = () => {
    const result = f.created(); if (mismatch === 'root') result.root!.id = 'foreign';
    if (mismatch === 'definition') result.root!.definition.revision = 'b'.repeat(64);
    if (mismatch === 'deleted') { result.deleted = true; result.root = null; result.tree = null; }
    return result;
  };
  await expect(startNewChat(f.runtime, f.client, f.tab.id, f.params)).rejects.toThrow('does not match');
  expect(f.runtime.draft(f.source)).toBe('Original message'); expect(f.count('sessions.submit')).toBe(0);
});
it('moves staged attachments to the accepted root and verifies them before first submission', async () => {
  const f = await fixture(); const bytes = new TextEncoder().encode('attached text');
  f.runtime.compositions.stage(f.source, [Object.assign(new File(['attached text'], 'note.txt', { type: 'text/plain' }), { arrayBuffer: async () => bytes.buffer })]);
  await startNewChat(f.runtime, f.client, f.tab.id, f.params);
  const uploaded = f.calls.find(call => call.method === 'content.put')!;
  expect(uploaded.params).toMatchObject({ session_id: 'created', media_type: 'text/plain' });
  expect(f.calls.find(call => call.method === 'sessions.submit')?.params).toMatchObject({ parts: [{ type: 'text', text: 'Original message' }, { type: 'content', reference_id: (uploaded.params as any).reference_id }] });
  expect(f.runtime.compositions.get('host:created:created').attachments).toEqual([]);
});
