import assert from 'node:assert/strict';

export async function discoveryAcceptance(runtime, client, createParams, evidence, deadline) {
  const instructions = text => ({ text, project_root: null, project_files: [], discover_skills: false, standing_instructions: false, skill_roots: [] });
  const created = await client.call('trees.create', {
    ...createParams,
    metadata: { title: 'Discover retained work', archived: true, pinned: true },
    overrides: { ...createParams.overrides, instructions: instructions('private discovery instructions') },
  }, deadline());
  const definitions = [];
  for (const name of ['Discovery first', 'Discovery second']) {
    definitions.push(await client.call('definitions.register', {
      id: 'discovery-definition', name,
      defaults: { instructions: instructions('private discovery definition') },
    }, deadline()));
  }
  const page = await client.listTrees({ archived: true, pinned: true, limit: 100 }, deadline());
  const summary = page.items.find(item => item.tree.id === created.tree.id);
  assert.equal(summary.root_id, created.root.id);
  assert.equal(summary.tree.metadata.title, 'Discover retained work');
  assert.ok(!JSON.stringify(page).includes('private discovery'));
  const seen = new Set();
  let after;
  for (let calls = 0; ; calls++) {
    assert.ok(calls < 100, 'bounded fixture catalog traversal');
    const next = await client.listDefinitions({ after, limit: 1 }, deadline());
    assert.ok(next.items.length <= 1);
    for (const item of next.items) {
      const key = `${item.ref.id}:${item.ref.revision}`;
      assert.ok(!seen.has(key), 'no duplicate immutable revision');
      seen.add(key);
      assert.ok(!JSON.stringify(item).includes('private discovery'));
    }
    if (next.next_cursor === null) break;
    after = next.next_cursor;
  }
  for (const item of definitions) assert.ok(seen.has(`${item.ref.id}:${item.ref.revision}`));
  await runtime.stop();
  await runtime.start();
  const retained = await client.listTrees({ archived: true, pinned: true, limit: 100 }, deadline());
  assert.deepEqual(retained.items.find(item => item.tree.id === created.tree.id), summary);
  const unarchived = await client.listTrees({ archived: false, limit: 100 }, deadline());
  assert.ok(!unarchived.items.some(item => item.tree.id === created.tree.id));
  evidence.push({ discovery: { root: summary, definitionCount: seen.size } });
}
