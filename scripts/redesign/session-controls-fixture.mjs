import assert from 'node:assert/strict';
import { mkdir, realpath, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

export async function sessionControlsAcceptance(runtime, client, createParams, evidence, deadline, dropAcknowledgement) {
  await runtime.stop(); await runtime.start('0');
  const { root } = await client.call('trees.create', { ...createParams, creation_id: 'session-controls' }, deadline());
  const destination = join(runtime.directory, 'controlled directory ');
  await mkdir(destination);
  const path = await realpath(destination);
  const request = { id: 'workspace-edit', session_id: root.id, expected_revision: root.config_revision, path };
  const proxy = join(runtime.directory, 'workspace-control.sock');
  let dropped;
  const close = await dropAcknowledgement(proxy, runtime.info.socket, request.id, value => { dropped = value; }, response => response.result?.id === request.id);
  try {
    const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.setWorkingDirectory(request, deadline()), DeliveryError);
    assert.equal(dropped.session.working_directory, path);
  } finally { await close(); }
  const changed = await client.setWorkingDirectory(request, deadline());
  assert.equal(changed.session.working_directory, path);
  assert.equal(changed.session.history_revision, root.history_revision);
  const configuration = { system: 'Exact run instructions', max_turns: 0, headless: true, cache_key: 'fixture-cache' };
  const runRequest = { id: 'run-edit', session_id: root.id, expected_revision: changed.revision, configuration };
  const configured = await client.configureRun(runRequest, deadline());
  assert.deepEqual(configured.session.configuration.run, configuration);
  await client.submit(root.id, [{ type: 'text', text: 'configured response' }], 'configured-response', deadline());
  const completed = await client.wait('configured-response', deadline());
  assert.equal(completed.turn.state, 'succeeded', completed.turn.failure);
  assert.equal(completed.turn.config_revision, configured.revision);
  await runtime.stop(); await runtime.start('0');
  assert.deepEqual(await client.configureRun(runRequest, deadline()), configured);
  await rm(path, { recursive: true });
  assert.deepEqual(await client.setWorkingDirectory(request, deadline()), changed);
  const inspection = await client.inspectWorkspace(root.id, deadline());
  assert.equal(inspection.working_directory, path);
  await client.call('sessions.delete', { session_id: root.id }, deadline());
  const deleted = await client.setWorkingDirectory(request, deadline());
  assert.equal(deleted.deleted, true); assert.equal(deleted.session, null);
  evidence.push({ session_controls: { changed, configured, deleted } });
}
