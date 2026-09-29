import { randomUUID } from 'node:crypto';
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';

export async function workspaceAcceptance(runtime, client, createParams, evidence, { dropAcknowledgement, unixSocket, deadline }) {
  const directory = join(runtime.directory, 'workspace ');
  const scope = join(directory, 'scope [literal] ');
  await mkdir(scope, { recursive: true });
  const execute = promisify(execFile);
  const git = (...args) => execute('git', args, { cwd: directory, timeout: 10_000, maxBuffer: 1 << 18 });
  const tracked = join(scope, 'tracked');
  const outside = join(directory, 'outside');
  await git('init', '-q');
  await git('config', 'user.name', 'Workspace Fixture');
  await git('config', 'user.email', 'workspace@localhost');
  await writeFile(tracked, 'base'); await writeFile(outside, 'outside base');
  await git('add', '.'); await git('commit', '-qm', 'fixture base');
  await writeFile(tracked, 'captured');
  const { root } = await client.call('trees.create', { creation_id: randomUUID(), ...createParams, working_directory: scope }, deadline());
  const snapshotID = 'workspace-snapshot';
  const loseAcknowledgement = async (method, actionID) => {
    const proxy = join(runtime.directory, 'workspace-drop.sock');
    let dropped;
    const close = await dropAcknowledgement(proxy, runtime.info.socket, '', value => { dropped = value; }, response => response.result?.action?.id === actionID);
    try {
      const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
      await assert.rejects(unreliable[method](root.id, snapshotID, actionID, deadline()), DeliveryError);
      assert.equal(dropped.action.state, 'succeeded');
      assert.deepEqual(await client.getWorkspaceAction(root.id, actionID, deadline()), dropped.action);
      return dropped;
    } finally { await close(); }
  };
  const captured = await loseAcknowledgement('captureWorkspace', 'workspace-capture');
  assert.equal(captured.snapshot.scope, 'session_working_directory');
  assert.match(captured.snapshot.semantics, /Untracked and later files may remain/);
  for (const secret of [directory, scope, 'object_id', 'binding']) assert.equal(JSON.stringify(captured).includes(secret), false);
  assert.deepEqual(await client.captureWorkspace(root.id, snapshotID, 'workspace-capture', deadline()), captured);
  await assert.rejects(client.captureWorkspace(root.id, 'changed', 'workspace-capture', deadline()), error => error.kind === 'CONFLICT');
  await assert.rejects(client.getWorkspaceAction('wrong-owner', 'workspace-capture', deadline()), error => error.kind === 'NOT_FOUND');
  await assert.rejects(client.call('sessions.delete', { session_id: root.id }, deadline()), error => error.kind === 'BUSY');
  await writeFile(tracked, 'later'); await writeFile(outside, 'outside later');
  await writeFile(join(scope, 'untracked'), 'untracked');
  const restored = await loseAcknowledgement('restoreWorkspace', 'workspace-restore');
  assert.equal(await readFile(tracked, 'utf8'), 'captured');
  assert.equal(await readFile(outside, 'utf8'), 'outside later');
  assert.equal(await readFile(join(scope, 'untracked'), 'utf8'), 'untracked');
  await writeFile(tracked, 'after restore');
  await runtime.stop('SIGKILL'); await runtime.start();
  assert.deepEqual(await client.getWorkspaceAction(root.id, 'workspace-restore', deadline()), restored.action);
  assert.deepEqual(await client.restoreWorkspace(root.id, snapshotID, 'workspace-restore', deadline()), restored);
  assert.equal(await readFile(tracked, 'utf8'), 'after restore', 'retry replayed Git checkout');
  const page = await client.listWorkspaceSnapshots({ session_id: root.id, limit: 1 }, deadline());
  assert.deepEqual(page.items, [captured.snapshot]);
  assert.deepEqual((await client.listWorkspaceSnapshots({ session_id: root.id, after: snapshotID, limit: 1 }, deadline())).items, []);
  const released = await loseAcknowledgement('releaseWorkspace', 'workspace-release');
  assert.ok(released.snapshot.released_at);
  const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 1 }, deadline());
  assert.deepEqual(history.items, [], 'human workspace action fabricated history');
  await client.call('sessions.delete', { session_id: root.id }, deadline());
  assert.deepEqual(await client.releaseWorkspace(root.id, snapshotID, 'workspace-release', deadline()), released);
  assert.equal((await client.restoreWorkspace(root.id, snapshotID, 'workspace-restore', deadline())).action.state, 'succeeded');
  assert.equal(await readFile(tracked, 'utf8'), 'after restore');
  assert.ok((await client.getWorkspaceSnapshot(root.id, snapshotID, deadline())).released_at);
  evidence.push({ workspace: { captured, restored, released, history } });
}
