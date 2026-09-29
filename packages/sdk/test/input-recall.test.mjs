import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client } from '../dist/index.js';

const row = (ordinal, input = 'input') => ({ session_id: 'owner', input_id: input, ordinal, text: 'exact human text' });
const page = () => ({ items: [row('9007199254740994')], next_cursor: '9007199254740993', scanned_count: 2, skipped_count: 1 });
async function fixture() {
  let response = page();
  const calls = [];
  const client = await Client.connect(async request => {
    calls.push(request);
    return { jsonrpc: '2.0', id: request.id, result: request.method === 'initialize'
      ? { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'epoch', network_client: false, builtins: [] } : response };
  }, { clientID: 'recall' });
  return { client, calls, set: value => { response = value; } };
}

test('cross-session input recall preserves exact counters and empty-page continuation without effects', async () => {
  const { client, calls, set } = await fixture();
  assert.deepEqual(await client.sessions.recentInputText(), page());
  set({ items: [], next_cursor: '9007199254740992', scanned_count: 2, skipped_count: 0 });
  assert.equal((await client.sessions.recentInputText({ before_ordinal: '9007199254740993', limit: 2 })).next_cursor, '9007199254740992');
  assert.deepEqual(calls.slice(1).map(value => [value.method, value.params]), [
    ['inputs.recent_text', { limit: 100 }],
    ['inputs.recent_text', { limit: 2, before_ordinal: '9007199254740993' }],
  ]);
  for (const limit of [0, 501, 1.5]) await assert.rejects(client.sessions.recentInputText({ limit }), TypeError);
  assert.equal(calls.length, 3);
});

test('recall rejects overfull, repeated, reversed, foreign-window and byte-overflow evidence', async () => {
  const { client, set } = await fixture();
  for (const bad of [
    { ...page(), scanned_count: 1 },
    { ...page(), items: [row('9007199254740994'), row('9007199254740993')], skipped_count: 0 },
    { ...page(), items: [row('9007199254740994'), row('9007199254740995', 'second')], skipped_count: 0 },
    { ...page(), next_cursor: '9007199254740995' },
    { ...page(), items: [], scanned_count: 0, skipped_count: 0 },
    { ...page(), items: [{ ...row('9007199254740994'), text: '界'.repeat(90000) }] },
  ]) {
    set(bad);
    await assert.rejects(client.sessions.recentInputText(), TypeError);
  }
  set(page());
  await assert.rejects(client.sessions.recentInputText({ before_ordinal: '9007199254740994' }), /ordering/);
  set({ items: [], next_cursor: '9007199254740994', scanned_count: 1, skipped_count: 0 });
  await assert.rejects(client.sessions.recentInputText({ before_ordinal: '9007199254740994' }), /continuation/);
});
