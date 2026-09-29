import assert from 'node:assert/strict';
import test from 'node:test';
import { Client } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'epoch', network_client: false, builtins: [] };
const item = { tree: { id: 'tree', metadata: { title: null, pinned: false, archived: false }, engine: 'starlark', revision: '9007199254740993', created_at: '2026-09-28T12:00:00Z' }, root_id: 'root', working_directory: '/project', activity: { active_turn_count: '9007199254740993', queued_input_count: '2', pending_permission_count: '0', pending_question_count: '1', active_workspace_action_count: '0' } };

test('native navigation summary preserves exact counters and explicit missing roots', async () => {
  let calls = 0;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls++; assert.equal(request.method, 'trees.summaries'); assert.deepEqual(request.params.root_ids, ['root', 'missing']);
    return { jsonrpc: '2.0', id: request.id, result: { items: [item], missing_root_ids: ['missing'] } };
  }, { clientID: 'test' });
  const page = await client.trees.summaries(['root', 'missing']);
  assert.equal(page.items[0].activity.active_turn_count, '9007199254740993');
  assert.deepEqual(page.missing_root_ids, ['missing']);
  for (const ids of [[], ['root', 'root'], Array.from({ length: 65 }, (_, n) => 'root' + n)]) await assert.rejects(client.trees.summaries(ids));
  assert.equal(calls, 1, 'invalid identity sets never reach the host');
});

test('navigation responses cannot omit, duplicate, or substitute requested roots', async () => {
  let result;
  const client = await Client.connect(async request => ({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? initial : result }), { clientID: 'test' });
  for (result of [
    { items: [], missing_root_ids: [] },
    { items: [item], missing_root_ids: ['root'] },
    { items: [{ ...item, root_id: 'foreign' }], missing_root_ids: [] },
  ]) await assert.rejects(client.trees.summaries(['root']), /ownership|omitted/);
});
