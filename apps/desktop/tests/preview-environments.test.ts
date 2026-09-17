import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readFile, rm, writeFile, stat } from 'node:fs/promises';
import { createServer, type Socket } from 'node:net';
import { request } from 'node:http';
import { once } from 'node:events';
import { Readable, Writable } from 'node:stream';
import path from 'node:path';
import { PreviewEnvironments, type PreviewScope } from '../src/preview-environments';
import { PreviewBudget } from '../src/preview-budget';
import { acquireSSHPreviewRoute } from '../src/preview-ssh-route';

const scope: PreviewScope = { savedHostId: 'selected-ssh', runtimeId: 'verified-runtime', projectId: 'project', connectionGeneration: 'generation', destinations: [{ remoteHost: '127.0.0.1', port: 3000 }] };
async function fixture(t: test.TestContext) {
  const directory = await mkdtemp('/tmp/whip-preview-test-'); const master = new AbortController();
  const calls: number[] = []; const revoked: string[] = []; const open = new Set<number>();
  const manager = new PreviewEnvironments({ directory,
    connection: () => ({ generation: scope.connectionGeneration, signal: master.signal,
      async acquirePreviewRoute(req, signal) {
        signal.throwIfAborted(); calls.push(req.port); open.add(req.port); const lifetime = new AbortController();
        return { generation: scope.connectionGeneration, signal: AbortSignal.any([master.signal, signal, lifetime.signal]),
          async open() { throw new Error('No network in metadata fixture'); }, async close() { open.delete(req.port); lifetime.abort(); } };
      } }),
    invalidateAttachments: (id, reason) => revoked.push(`${id}:${reason}`),
  });
  t.after(async () => { await manager.close(); await rm(directory, { recursive: true, force: true }); });
  return { directory, master, manager, calls, revoked, open };
}

test('describe allocates stable offer metadata without resolving connections or opening routes', async t => {
  const directory = await mkdtemp('/tmp/whip-preview-offer-');
  let connectionCalls = 0;
  const manager = new PreviewEnvironments({ directory,
    connection: () => { connectionCalls++; throw new Error('No preapproval connection access'); }, invalidateAttachments() {} });
  t.after(async () => { await manager.close(); await rm(directory, { recursive: true, force: true }); });
  const [first, second] = await Promise.all([manager.describe(scope), manager.describe(scope)]);
  assert.equal(first, second); assert.match(first, /^[a-f0-9]{48}$/);
  assert.notEqual(await manager.describe({ ...scope, runtimeId: 'other-runtime' }), first);
  assert.equal(connectionCalls, 0);
  await assert.rejects(manager.acquire(first), /renewed approval/); assert.equal(connectionCalls, 0);
  const metadata = await readFile(path.join(directory, 'profiles.json'), 'utf8');
  assert.equal(metadata.includes('connectionGeneration'), false); assert.equal(metadata.includes('destinations'), false);
});

test('environment metadata is inert, private, identity isolated and requires fresh scope after restart', async t => {
  const f = await fixture(t); const offered = await f.manager.describe(scope); const id = await f.manager.ensure(scope); assert.equal(id, offered);
  const lease = await f.manager.acquire(id, 'tab'); assert.equal(lease.environmentId, id); assert.match(lease.partition, /^persist:whip-preview-/);
  const serialized = await readFile(path.join(f.directory, 'profiles.json'), 'utf8');
  assert.equal(serialized.includes('generation'), false); assert.equal(serialized.includes('3000'), false); assert.equal(serialized.includes('password'), false);
  assert.equal((await stat(path.join(f.directory, 'profiles.json'))).mode & 0o777, 0o600);
  const other = await f.manager.acquire({ ...scope, runtimeId: 'another-runtime' }, 'other'); assert.notEqual(other.environmentId, id);
  await lease.release(); await assert.rejects(f.manager.acquire(id, 'restored'), /renewed approval/);
  const restored = await f.manager.acquire(scope, 'reapproved'); assert.equal(restored.environmentId, id);
  await restored.release(); await other.release(); assert.equal(f.open.size, 0);
});

test('explicit expansion invalidates prior scope and revoke/disconnect fail closed', async t => {
  const f = await fixture(t); const lease = await f.manager.acquire(scope, 'tab');
  const expanded: PreviewScope = { ...scope, destinations: [...scope.destinations, { remoteHost: '::1', port: 3001 }] };
  assert.throws(() => f.manager.assertURL(lease.environmentId, 'http://localhost:3001'), /not approved/);
  await f.manager.expand(lease.environmentId, expanded); assert.equal(f.revoked.length, 1);
  assert.throws(() => f.manager.assertScope(lease.environmentId, scope), /renewed approval/);
  assert.equal(f.manager.assertScope(lease.environmentId, expanded), 2);
  assert.doesNotThrow(() => f.manager.assertURL(lease.environmentId, 'http://[::1]:3001/path#hash'));
  assert.doesNotThrow(() => f.manager.assertURL(lease.environmentId, 'http://localhost:3001'));
  assert.doesNotThrow(() => f.manager.assertURL(lease.environmentId, 'http://localhost:3000'));
  assert.throws(() => f.manager.assertURL(lease.environmentId, 'http://127.0.0.1:3001'), /not approved/);
  assert.throws(() => f.manager.assertURL(lease.environmentId, 'http://[::1]:3000'), /not approved/);
  assert.throws(() => f.manager.assertURL(lease.environmentId, 'http://169.254.169.254'), /not approved/);
  await assert.rejects(f.manager.acquire(scope, 'narrow-controller'), /renewed approval/);
  await f.manager.revoke(lease.environmentId, 3001); assert.throws(() => f.manager.assertScope(lease.environmentId, expanded), /renewed approval/);
  f.master.abort(); assert.throws(() => f.manager.assertURL(lease.environmentId, 'http://localhost:3000'), /unavailable/);
  await assert.rejects(f.manager.acquire(lease.environmentId), /renewed approval/);
});

test('window allows four environments and rejects a fifth without creating routes', async t => {
  const f = await fixture(t);
  const leases = [];
  for (let index = 0; index < 4; index++) leases.push(await f.manager.acquire({ ...scope, projectId: `project-${index}` }, `tab-${index}`));
  await assert.rejects(f.manager.acquire({ ...scope, projectId: 'overflow' }, 'overflow'), /environment limit/);
  assert.equal(f.calls.length, 4);
  await leases[0]!.release();
  const replacement = await f.manager.acquire({ ...scope, projectId: 'replacement' }, 'replacement'); await replacement.release();
});

test('scope changes during an expansion cannot publish a revoked destination', async t => {
  const directory = await mkdtemp('/tmp/whip-preview-race-'); const master = new AbortController();
  let release: (() => void) | undefined; let started: (() => void) | undefined;
  const acquiring = new Promise<void>(resolve => { started = resolve; });
  const wait = new Promise<void>(resolve => { release = resolve; });
  const manager = new PreviewEnvironments({ directory, invalidateAttachments() {}, connection: () => ({ generation: scope.connectionGeneration, signal: master.signal,
    async acquirePreviewRoute(req, signal) {
      if (req.port === 3001) { started!(); await wait; }
      signal.throwIfAborted(); const controller = new AbortController();
      return { generation: scope.connectionGeneration, signal: controller.signal, async open() { throw new Error('unused'); }, async close() { controller.abort(); } };
    } }) });
  t.after(async () => { release!(); await manager.close(); await rm(directory, { recursive: true, force: true }); });
  const initial: PreviewScope = { ...scope, destinations: [...scope.destinations, { remoteHost: '127.0.0.1', port: 3002 }] };
  const lease = await manager.acquire(initial, 'tab');
  const result = assert.rejects(manager.expand(lease.environmentId, { ...initial, destinations: [...initial.destinations, { remoteHost: '127.0.0.1', port: 3001 }] }), /changed during approval/);
  await acquiring; await manager.revoke(lease.environmentId, 3002); release!(); await result;
  assert.throws(() => manager.assertURL(lease.environmentId, 'http://localhost:3001'), /not approved/);
});

test('proxy auth requires exact endpoint, realm, live contents and pointer-identical session', async t => {
  const f = await fixture(t); const lease = await f.manager.acquire(scope, 'tab');
  const proxy = new URL(lease.proxyConfig.proxyRules);
  const challenge = await new Promise<string>((resolve, reject) => {
    const req = request({ host: proxy.hostname, port: proxy.port, path: 'http://localhost:3000/' }, response => {
      response.resume(); response.once('end', () => resolve(String(response.headers['proxy-authenticate'])));
    }); req.on('error', reject); req.end();
  });
  const info = { isProxy: true, host: proxy.hostname, port: Number(proxy.port), scheme: 'basic', realm: challenge.match(/realm="([^"]+)"/)![1]! };
  const contents = { id: 42, session: {} };
  assert.equal(lease.authenticate(contents, info), undefined); const unbind = lease.bindContents(contents);
  assert.equal(lease.authenticate(contents, info)?.username, 'preview');
  for (const changed of [{ isProxy: false }, { host: 'example.com' }, { port: 80 }, { scheme: 'digest' }, { realm: 'website' }]) assert.equal(lease.authenticate(contents, { ...info, ...changed }), undefined);
  assert.equal(lease.authenticate(null, info), undefined);
  assert.equal(lease.authenticate({ id: contents.id, session: {} }, info), undefined);
  unbind(); assert.equal(lease.authenticate(contents, info), undefined);
  lease.bindContents(contents); await lease.release(); assert.equal(lease.authenticate(contents, info), undefined);
});

test('queued preview writes backpressure at 4MiB and cancellation releases every byte', async () => {
  const budget = new PreviewBudget(); const controllers = Array.from({ length: 65 }, () => new AbortController());
  const work = controllers.map(controller => {
    const source = Readable.from([Buffer.alloc(64 * 1024)]);
    const sink = new Writable({ write(_bytes, _encoding, _callback) { /* deliberately stalled */ } });
    return budget.pipe(source, sink, controller.signal).catch(() => {});
  });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(budget.usage.queuedBytes, 4 * 1024 * 1024);
  for (const controller of controllers) controller.abort();
  await Promise.all(work); assert.equal(budget.usage.queuedBytes, 0);
});

test('corrupt profile metadata is not overwritten or silently discarded', async t => {
  const f = await fixture(t); const target = path.join(f.directory, 'profiles.json'); await writeFile(target, '{broken');
  await assert.rejects(f.manager.ensure(scope), /corrupt/); assert.equal(await readFile(target, 'utf8'), '{broken'); assert.equal(f.calls.length, 0);
});

test('route helper emits literal forward/cancel, destroys streams and refuses generation changes', async t => {
  const directory = await mkdtemp('/tmp/whip-route-test-'); const controller = new AbortController();
  const calls: string[][] = []; const servers = new Map<string, ReturnType<typeof createServer>>(); const sockets = new Set<Socket>();
  t.after(async () => { for (const socket of sockets) socket.destroy(); for (const server of servers.values()) server.close(); await rm(directory, { recursive: true, force: true }); });
  const master = { generation: 'g', directory, signal: controller.signal, async command(args: string[]) {
    calls.push(args); const endpoint = args[3]!.split(':')[0]!;
    if (args[1] === 'forward') {
      const server = createServer(socket => { sockets.add(socket); socket.on('error', () => {}); socket.on('data', bytes => socket.write(bytes)); });
      server.listen(endpoint); await once(server, 'listening'); servers.set(endpoint, server);
    } else { servers.get(endpoint)?.close(); }
    return '';
  } };
  const signal = new AbortController().signal;
  await assert.rejects(acquireSSHPreviewRoute(master, { remoteHost: '127.0.0.1', port: 3000, expectedGeneration: 'stale' }, signal), /changed/);
  assert.equal(calls.length, 0);
  const route = await acquireSSHPreviewRoute(master, { remoteHost: '::1', port: 3000, expectedGeneration: 'g' }, signal);
  assert.match(calls[0]![3]!, /:\[::1\]:3000$/);
  const socket = await route.open(signal); socket.write('through route'); assert.equal((await once(socket, 'data'))[0].toString(), 'through route');
  const closed = new Promise<void>(resolve => socket.once('close', () => resolve())); await route.close(); await closed;
  await route.close(); assert.equal(calls.length, 2); assert.equal(calls[1]![1], 'cancel'); assert.equal(calls[0]![3], calls[1]![3]);
  await assert.rejects(route.open(signal));
});

test('one window budget caps all environments at 32 streams and releases idempotently', () => {
  const budget = new PreviewBudget(); const leases = Array.from({ length: 32 }, () => budget.acquire());
  assert.throws(() => budget.acquire(), /window stream limit/);
  leases[0]!(); leases[0]!(); assert.equal(budget.usage.streams, 31);
  const next = budget.acquire(); for (const lease of leases) lease(); next(); assert.equal(budget.usage.streams, 0);
});
