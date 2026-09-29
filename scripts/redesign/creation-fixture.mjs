import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { Client, DeliveryError, RemoteError } from '../../packages/sdk/dist/index.js';

export async function creationCatalogAcceptance(runtime, client, createParams, evidence, { dropAcknowledgement, unixSocket, deadline }) {
  const path = join(runtime.directory, 'state', 'host.json');
  const originalHost = await readFile(path, 'utf8');
  try {
    for (const engine of ['starlark', 'quickjs']) {
      await runtime.stop();
      const host = JSON.parse(originalHost);
      host.providers.scripted = { kind: 'openai-chat', base_url: 'https://fixture.invalid/v1', credential_source: 'none' };
      host.defaults.model = { provider: 'scripted', name: 'scripted', effort: '', temperature: 0 };
      host.defaults.automatic_title = false;
      host.default_permission_mode = 'automatic';
      host.resources = [{ kind: 'descendants', limit: '7' }];
      host.engine = engine === 'starlark' ? 'quickjs' : 'starlark';
      await writeFile(path, JSON.stringify(host), { mode: 0o600 });
      await runtime.start();
      const creationID = `${engine}:MiXeD:Creation`;
      const params = { ...createParams, engine, metadata: { title: null, pinned: false, archived: false }, resources: [], overrides: { automatic_title: false } };
      const before = await client.treeCatalog(deadline());
      let accepted, killed;
      const proxy = join(runtime.directory, `creation-${engine}.sock`);
      const close = await dropAcknowledgement(proxy, runtime.info.socket, creationID, value => {
        accepted = value;
        killed = runtime.stop('SIGKILL');
      }, response => response.result?.creation?.id === creationID);
      try {
        const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
        await assert.rejects(unreliable.createTree(params, creationID, deadline()), DeliveryError);
        assert.ok(accepted?.root.id, 'proxy did not observe committed root creation');
        await killed;
      } finally { await close(); }
      host.defaults.model.name = 'changed-after-commit';
      host.default_permission_mode = 'prompt';
      host.resources = [{ kind: 'descendants', limit: '2' }];
      await writeFile(path, JSON.stringify(host), { mode: 0o600 });
      await runtime.start();
      const after = await client.treeCatalog(deadline());
      assert.equal(BigInt(after.revision), BigInt(before.revision) + 1n);
      assert.deepEqual(await client.getTreeCreation(creationID, deadline()), accepted);
      assert.deepEqual(await client.createTree(params, creationID, deadline()), accepted);
      assert.deepEqual(await client.treeCatalog(deadline()), after);
      assert.equal(accepted.tree.engine, engine);
      assert.equal(accepted.root.configuration.model.name, 'scripted');
      assert.equal(accepted.root.configuration.model.temperature, 0);
      assert.equal((await client.getPermissionPolicy(accepted.root.id, deadline())).mode, 'automatic');
      assert.equal((await client.call('resources.list', { session_id: accepted.root.id }, deadline())).items.find(item => item.kind === 'descendants').limit, '7');
      assert.deepEqual((await client.call('sessions.history', { session_id: accepted.root.id, after: '0', limit: 1 }, deadline())).items, []);
      await assert.rejects(client.createTree({ ...params, engine: host.engine }, creationID, deadline()), error => error instanceof RemoteError && error.kind === 'CONFLICT');
      const fresh = await client.createTree(params, `${creationID}:Fresh`, deadline());
      assert.equal(fresh.root.configuration.model.name, 'changed-after-commit');
      assert.equal((await client.getPermissionPolicy(fresh.root.id, deadline())).mode, 'prompt');
      const beforeFallback = await client.treeCatalog(deadline());
      const text = `${engine} root creation survived a lost response and process restart`;
      const inputID = `${creationID}:FirstInput`;
      await client.submit(accepted.root.id, [{ type: 'text', text }], inputID, deadline());
      assert.equal((await client.wait(inputID, deadline())).turn.state, 'succeeded');
      assert.equal(BigInt((await client.treeCatalog(deadline())).revision), BigInt(beforeFallback.revision) + 1n);
      const selected = await client.getTreeCreation(creationID, deadline());
      assert.deepEqual(selected.creation, accepted.creation);
      assert.equal(selected.tree.metadata.title, Array.from(text).slice(0, 64).join(''));
      await client.call('sessions.lifecycle', { session_id: accepted.root.id, lifecycle: 'stopped' }, deadline());
      await client.call('sessions.delete', { session_id: accepted.root.id }, deadline());
      const deletedHead = await client.treeCatalog(deadline());
      const deleted = await client.createTree(params, creationID, deadline());
      assert.equal(deleted.deleted, true);
      assert.equal(deleted.tree, null);
      assert.equal(deleted.root, null);
      assert.deepEqual(deleted.creation, accepted.creation);
      await runtime.stop('SIGKILL');
      await runtime.start();
      assert.deepEqual(await client.getTreeCreation(creationID, deadline()), deleted);
      assert.deepEqual(await client.createTree(params, creationID, deadline()), deleted);
      assert.deepEqual(await client.treeCatalog(deadline()), deletedHead);
      evidence.push({ engine, creation: accepted.creation, capturedModel: accepted.root.configuration.model, deleted });
    }
    const first = await client.listTrees({ limit: 1 }, deadline());
    assert.ok(first.next_cursor);
    const all = await client.listTrees({ limit: 100, expected_revision: first.revision }, deadline());
    const unopened = all.items.at(-1);
    assert.notEqual(unopened.tree.id, first.items[0].tree.id);
    const pages = client.treePages({ limit: 1 }, deadline());
    assert.equal((await pages.next()).value.revision, first.revision);
    const edited = await client.call('trees.update', { tree_id: unopened.tree.id, expected_revision: unopened.tree.revision, metadata: { title: 'Off-page human title', pinned: true, archived: true } }, deadline());
    const head = await client.treeCatalog(deadline());
    assert.equal(BigInt(head.revision), BigInt(first.revision) + 1n);
    await assert.rejects(client.listTrees({ limit: 1, after: first.next_cursor, expected_revision: first.revision }, deadline()), error => error instanceof RemoteError && error.kind === 'CONFLICT');
    await assert.rejects(pages.next(), error => error instanceof RemoteError && error.kind === 'CONFLICT');
    const filtered = await client.listTrees({ limit: 100, pinned: true, archived: true, expected_revision: head.revision }, deadline());
    assert.deepEqual(filtered.items.find(item => item.tree.id === edited.id).tree, edited);
    const refreshed = [];
    for await (const page of client.treePages({ limit: 2, expected_revision: head.revision }, deadline())) refreshed.push(...page.items);
    assert.equal(new Set(refreshed.map(item => item.tree.id)).size, refreshed.length);
    assert.deepEqual(refreshed.find(item => item.tree.id === edited.id).tree, edited);
    const same = await client.call('trees.update', { tree_id: edited.id, expected_revision: edited.revision, metadata: edited.metadata }, deadline());
    assert.equal(BigInt(same.revision), BigInt(edited.revision) + 1n, 'explicit manual intent retains its metadata CAS');
    assert.equal(BigInt((await client.treeCatalog(deadline())).revision), BigInt(head.revision) + 1n);
    evidence.push({ catalog: { old: first.revision, refreshed: head.revision, offPage: edited.id, count: refreshed.length } });
  } finally {
    await runtime.stop();
    await writeFile(path, originalHost, { mode: 0o600 });
    await runtime.start();
  }
}
