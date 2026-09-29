/// <reference types="node" />
/** @jest-environment node */
import { createHash } from 'node:crypto';
import { Client, DeliveryError, type Admission, type Request, type Response, type Transport } from '@whip/sdk';
import { MobileCommands } from './commands';
import { nativeFixture } from '../../test/native-fixtures';
import { metadataKey, type MetadataStorage, type StoredMetadata } from './recovery-metadata';

const hash = async (text: string) => createHash('sha256').update(text).digest('hex');
function deferred<T = void>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { promise, resolve }; }
function fixture() {
  const records = new Map<string, StoredMetadata>();
  const requests: Request[] = [];
  let putError = false; let acceptError = false;
  const storage: MetadataStorage = {
    list: async () => structuredClone([...records.values()]),
    put: async value => { if (putError) throw new Error('disk full'); records.set(metadataKey(value.record), structuredClone(value)); },
    accept: async record => { if (acceptError) throw new Error('disk full after acceptance'); records.get(metadataKey(record))!.knownAccepted = true; },
    delete: async record => { records.delete(metadataKey(record)); },
  };
  let method: (request: Request) => Promise<Response> = async request => success(request, admission());
  async function client(epoch = 'boot', runtimeID = 'runtime') {
    const transport: Transport = async request => {
      if (request.method === 'initialize') return success(request, { major: 4, minor: 0, runtime_id: runtimeID, process_epoch: epoch, network_client: true, builtins: [] });
      requests.push(structuredClone(request)); return method(request);
    };
    return Client.connect(transport, { clientID: 'phone' });
  }
  const commands = new MobileCommands(storage, hash);
  return { commands, storage, records, requests, client, method(fn: typeof method) { method = fn; }, failPut() { putError = true; }, failAccept() { acceptError = true; } };
}
function success(request: Request, result: unknown): Response { return { jsonrpc: '2.0', id: request.id, result }; }
function remote(request: Request, kind: string): Response { return { jsonrpc: '2.0', id: request.id, error: { code: -32000, kind: kind as 'NOT_FOUND', message: kind } }; }
function admission(id = 'request', sessionId = 'child'): Admission {
  return { receipt: { identity: { client_id: 'phone', request_id: id }, digest: 'a'.repeat(64), input_id: 'input', deleted_at: null, created_at: '2026-09-28T00:00:00Z' }, input: { id: 'input', session_id: sessionId, source: 'user', kind: 'prompt', parts: [{ type: 'text', text: 'private prompt' }], state: 'queued', turn_id: null, goal: null, schedule: null, host_operation: null, created_at: '2026-09-28T00:00:00Z' }, turn: null };
}
function submit(client: Client, id = 'request') { return client.session('child').submission([{ type: 'text', text: 'private prompt' }], id); }

test('acceptance is persisted before returning, without storing prompt or execution state', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  await f.commands.run(submit(client), { rootId: 'root', intent: { draftKey: 'draft', draftRevision: 'r1' } });
  expect(f.requests.map(r => r.method)).toEqual(['sessions.submit']);
  expect(f.commands.getSnapshot()[0]).toMatchObject({ status: 'accepted', knownAccepted: true, retryable: false, record: { sessionId: 'child', rootId: 'root' } });
  expect([...f.records.values()][0].knownAccepted).toBe(true);
  expect(JSON.stringify([...f.records.values()])).not.toContain('private prompt');
  expect(JSON.stringify(f.commands.getSnapshot())).not.toContain('private prompt');
  expect(f.commands.isBlocked('child')).toBe(false);
});

test('persistence failure stops before the actual SDK send', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client); f.failPut();
  await expect(f.commands.run(submit(client))).rejects.toThrow('disk full');
  expect(f.requests).toEqual([]); expect(f.records.size).toBe(0);
  expect(f.commands.getSnapshot()[0]).toMatchObject({ status: 'failed', message: 'Request was not sent: disk full' });
});

test('lost acknowledgement stays unknown; same-runtime rebind never sends, explicit exact check finds admission', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  f.method(async request => { if (request.method === 'sessions.submit') throw new DOMException('Observation disconnected', 'AbortError'); return success(request, admission()); });
  await expect(f.commands.run(submit(client))).rejects.toMatchObject({ name: 'AbortError' });
  expect(f.commands.getSnapshot()[0]).toMatchObject({ status: 'unknown', retryable: false });
  f.commands.detach(); const replacement = await f.client('new-boot'); await f.commands.bind(replacement);
  expect(f.requests).toHaveLength(1);
  const record = f.commands.getSnapshot()[0].record;
  expect(await f.commands.check(record)).toMatchObject({ status: 'accepted', knownAccepted: true });
  expect(f.requests.map(request => request.method)).toEqual(['sessions.submit', 'receipts.match']);
  const checked = JSON.parse(Buffer.from((f.requests[1].params as { params_base64: string }).params_base64, 'base64').toString());
  expect(checked).toEqual(f.requests[0].params);
});

test('explicit retry requires confirmed absence and preserves exact payload/identity after rebind', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  let delivered = false;
  f.method(async request => {
    if (request.method === 'receipts.match') return remote(request, 'NOT_FOUND');
    if (!delivered) { delivered = true; throw new DeliveryError('lost before admission'); }
    return success(request, admission());
  });
  await expect(f.commands.run(submit(client))).rejects.toThrow('lost before');
  const record = f.commands.getSnapshot()[0].record;
  await expect(f.commands.retry(record)).rejects.toThrow('confirmed missing');
  f.commands.detach(); await f.commands.bind(await f.client('replacement'));
  expect(await f.commands.check(record)).toMatchObject({ status: 'missing', retryable: true });
  await f.commands.retry(record);
  const writes = f.requests.filter(request => request.method === 'sessions.submit');
  expect(writes).toHaveLength(2); expect(writes[0].params).toEqual(writes[1].params);
  expect(f.commands.getSnapshot()[0]).toMatchObject({ status: 'accepted', knownAccepted: true });
});

test('restart restores read-only metadata inspection, never reconstructs an original or clears a draft', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  f.method(async request => { if (request.method === 'sessions.submit') throw new DeliveryError('lost ACK'); return success(request, admission()); });
  await expect(f.commands.run(submit(client), { intent: { draftKey: 'draft', draftRevision: 'r1' } })).rejects.toThrow();
  f.commands.dispose();
  const restarted = new MobileCommands(f.storage, hash); await restarted.bind(await f.client('new-process'));
  const record = restarted.getSnapshot()[0].record;
  expect(await restarted.check(record)).toMatchObject({ status: 'identity_only', knownAccepted: false, retryable: false });
  expect(f.requests.at(-1)?.method).toBe('receipts.get');
  await expect(restarted.retry(record)).rejects.toThrow('original request');
  f.method(async request => remote(request, 'NOT_FOUND'));
  expect(await restarted.check(record)).toMatchObject({ status: 'missing', retryable: false });
  expect(f.requests.filter(request => request.method === 'sessions.submit')).toHaveLength(1);
});

test('known acceptance survives failed local durability and can never become retryable missing', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client); f.failAccept();
  await expect(f.commands.run(submit(client))).rejects.toThrow('disk full after acceptance');
  expect(f.commands.getSnapshot()[0]).toMatchObject({ knownAccepted: true, status: 'unknown' });
  f.commands.detach(); await f.commands.bind(await f.client());
  f.method(async request => remote(request, 'NOT_FOUND'));
  const record = f.commands.getSnapshot()[0].record;
  expect(await f.commands.check(record)).toMatchObject({ knownAccepted: true, status: 'unknown', retryable: false });
  await expect(f.commands.retry(record)).rejects.toThrow('confirmed missing');
});

test.each(['BUSY', 'TRANSFER_FAILED', 'TRANSFER_UNCERTAIN', 'TRANSFER_INTERRUPTED', 'TRANSFER_CANCELLED', 'TRANSFER_DELETED'])('receipt error %s never becomes absence or automatic replay', async kind => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  f.method(async request => { if (request.method === 'sessions.submit') throw new DeliveryError('lost ACK'); return remote(request, kind); });
  await expect(f.commands.run(submit(client))).rejects.toThrow();
  const checked = await f.commands.check(f.commands.getSnapshot()[0].record);
  expect(checked.status).toBe(kind === 'BUSY' ? 'unknown' : 'failed'); expect(checked.retryable).toBe(false);
  expect(f.requests.filter(request => request.method === 'sessions.submit')).toHaveLength(1);
});

test('a pending local write reserves its recipient and host detach stops unsent delivery', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  const gate = deferred(); const originalPut = f.storage.put;
  f.storage.put = async value => { await gate.promise; await originalPut(value); };
  const first = f.commands.run(submit(client));
  while (!f.commands.getSnapshot().length) await Promise.resolve();
  await expect(f.commands.run(submit(client, 'second'))).rejects.toThrow('previous delivery');
  f.commands.detach(); gate.resolve(); await expect(first).rejects.toThrow('Host changed');
  expect(f.requests).toHaveLength(0); expect(f.records.size).toBe(1);
});

test('switching to a different runtime clears original authority and never checks through that host', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  f.method(async () => { throw new DeliveryError('lost'); });
  await expect(f.commands.run(submit(client))).rejects.toThrow();
  const record = f.commands.getSnapshot()[0].record;
  await f.commands.bind(await f.client('other-boot', 'other-runtime'));
  expect(f.commands.getSnapshot()).toEqual([]);
  await expect(f.commands.check(record)).rejects.toThrow('another host'); expect(f.requests).toHaveLength(1);
});

test('acceptance learned while detached survives a failed save when the same host reconnects', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  const gate = deferred<Response>(); f.method(async () => gate.promise); f.failAccept();
  const sending = f.commands.run(submit(client));
  while (!f.requests.length) await Promise.resolve();
  f.commands.detach(); gate.resolve(success(f.requests[0], admission()));
  await expect(sending).rejects.toThrow('disk full after acceptance');
  await f.commands.bind(await f.client('replacement'));
  expect(f.commands.getSnapshot()[0]).toMatchObject({ knownAccepted: true, retryable: false });
  f.method(async request => remote(request, 'NOT_FOUND'));
  expect(await f.commands.check(f.commands.getSnapshot()[0].record)).toMatchObject({ knownAccepted: true, status: 'unknown', retryable: false });
});

test('a queued restore cannot overwrite acceptance learned during its storage read', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  f.method(async () => { throw new DeliveryError('lost ACK'); });
  await expect(f.commands.run(submit(client))).rejects.toThrow();
  const read = f.storage.list; const gate = deferred(); let first = true;
  f.storage.list = async () => { const result = await read(); if (first) { first = false; await gate.promise; } return result; };
  const restoring = f.commands.bind(client);
  await Promise.resolve();
  f.method(async request => success(request, admission()));
  const record = f.commands.getSnapshot()[0].record;
  await f.commands.check(record); gate.resolve(); await restoring;
  expect(f.commands.getSnapshot()[0]).toMatchObject({ status: 'accepted', knownAccepted: true });
});

test('published metadata is immutable and cannot redirect later recovery', async () => {
  const f = fixture(); const client = await f.client(); await f.commands.bind(client);
  await f.commands.run(submit(client));
  const record = f.commands.getSnapshot()[0].record;
  expect(Object.isFrozen(record)).toBe(true);
  expect(Reflect.set(record, 'sessionId', 'other')).toBe(false);
  await f.commands.check(record);
  expect(f.requests.at(-1)?.method).toBe('receipts.get');
  expect(f.commands.getSnapshot()[0].record.sessionId).toBe('child');
});

test.each(['permissions.set_denial', 'sessions.reload'] as const)('%s preserves exact recovery before restart and identity-only inspection afterwards', async method => {
  const f = fixture(), client = await f.client(); await f.commands.bind(client);
  const receipt = method === 'permissions.set_denial' ? nativeFixture('PermissionDenialEdit') : nativeFixture('ReloadEdit');
  const params = { session_id: receipt.session_id, edit_id: receipt.id, expected_revision: receipt.expected_revision };
  const prepare = () => method === 'permissions.set_denial' ? client.command(method, { ...params, deny_interactive: false }) : client.command(method, params);
  f.method(async request => { if (request.method === method) throw new DeliveryError('lost acknowledgement'); return success(request, receipt); });
  await expect(f.commands.run(prepare(), { rootId: receipt.session_id })).rejects.toThrow('lost acknowledgement');
  expect(JSON.stringify([...f.records.values()])).not.toContain('configuration');
  expect(JSON.stringify([...f.records.values()])).not.toContain('deny_interactive');
  f.commands.detach(); await f.commands.bind(await f.client('new-epoch'));
  expect(f.requests).toHaveLength(1);
  const record = f.commands.getSnapshot()[0].record;
  expect(await f.commands.check(record)).toMatchObject({ knownAccepted: true, status: 'accepted', retryable: false });
  expect(f.requests.filter(request => request.method === method)).toHaveLength(1);
  // A separate lost delivery restores only metadata, not original request bytes.
  const g = fixture(), other = await g.client(); await g.commands.bind(other);
  g.method(async request => { if (request.method === method) throw new DeliveryError('lost'); return success(request, receipt); });
  const command = method === 'permissions.set_denial' ? other.command(method, { ...params, deny_interactive: false }) : other.command(method, params);
  await expect(g.commands.run(command, { rootId: receipt.session_id })).rejects.toThrow('lost');
  g.commands.dispose(); const restarted = new MobileCommands(g.storage, hash); await restarted.bind(await g.client('restart'));
  expect(await restarted.check(restarted.getSnapshot()[0].record)).toMatchObject({ status: 'identity_only', knownAccepted: false, retryable: false });
  expect(g.requests.filter(request => request.method === method)).toHaveLength(1);
});

test('restored permission denial receipt must retain its exact edit and recipient identities', async () => {
  const f = fixture(), client = await f.client(); await f.commands.bind(client); const receipt = nativeFixture('PermissionDenialEdit');
  f.method(async request => { if (request.method === 'permissions.set_denial') throw new DeliveryError('lost'); return success(request, { ...receipt, session_id: 'other' }); });
  await expect(f.commands.run(client.command('permissions.set_denial', { session_id: receipt.session_id, edit_id: receipt.id, expected_revision: receipt.expected_revision, deny_interactive: false }))).rejects.toThrow();
  f.commands.dispose(); const restarted = new MobileCommands(f.storage, hash); await restarted.bind(await f.client());
  expect(await restarted.check(restarted.getSnapshot()[0].record)).toMatchObject({ status: 'unknown', knownAccepted: false, retryable: false, message: 'Permission edit identity or recipient mismatch' });
});
