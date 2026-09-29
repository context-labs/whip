import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = () => structuredClone(fixtures.find(item => item.type === 'Usage' && item.valid).value);

test('usage reads preserve exact aggregate values, field presence and empty or in-flight distinctions', async () => {
  let value = fixture(); const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] } };
    calls.push(request); return { jsonrpc: '2.0', id: request.id, result: value };
  }, { clientID: 'test' });
  const session = client.session('session_root');
  const read = await session.usage();
  assert.equal(read.reported_cost.value, '9007199254740993'); assert.equal(read.input_tokens.value, '9007199254740993');
  assert.equal(read.input_tokens.missing_attempts, '1'); assert.equal(read.output_tokens.known_attempts, '0');
  assert.equal(read.unknown_cost, '1'); assert.equal(read.attempts.in_flight, '1');
  assert.deepEqual(calls.map(call => call.method), ['usage.get']);
  assert.equal(calls[0].params.session_id, 'session_root');
  value = { ...fixture(), session_id: 'foreign' };
  await assert.rejects(session.usage(), /belongs to another session/);
  value = fixture(); value.input_tokens.value = 9007199254740993;
  await assert.rejects(session.usage(), TypeError);
  value = fixture(); value.reported_cost.attempts = '0';
  await assert.rejects(session.usage(), /counts disagree/);
  value = fixture(); value.output_tokens.missing_attempts = '0';
  await assert.rejects(session.usage(), /presence counts disagree/);
  const count = calls.length;
  await assert.rejects(session.usage({ signal: AbortSignal.abort() }), error => error.name === 'AbortError');
  assert.equal(calls.length, count);
});


test('turn usage pins exact owner and turn while preserving compaction attempts and committed folds', async () => {
 const zero = { reserved:'0', in_flight:'0', settled:'0', not_dispatched:'0', uncertain:'0' };
 let value = { turn_id:'turn', usage:fixture(), compaction_attempts:{ ...zero, settled:'1' }, compactions:'1' };
 const client = await Client.connect(async request => {
  if (request.method === 'initialize') return { jsonrpc:'2.0', id:request.id, result:{ major:4, minor:0, runtime_id:'runtime', process_epoch:'boot', network_client:false, builtins:[] } };
  assert.equal(request.method,'usage.turn');
  assert.deepEqual(request.params,{ session_id:'session_root',turn_id:'turn' });
  return { jsonrpc:'2.0', id:request.id, result:value };
 },{clientID:'usage'});
 const session=client.session('session_root');
 const read=await session.turns.usage('turn');
 assert.equal(read.usage.input_tokens.value,'9007199254740993');
 assert.equal(read.compactions,'1');
 value={...value,turn_id:'foreign'};
 await assert.rejects(session.turns.usage('turn'),/another turn/);
 value={...value,turn_id:'turn',usage:{...fixture(),session_id:'foreign'}};
 await assert.rejects(session.turns.usage('turn'),/another session/);
 value={...value,usage:fixture(),compactions:'2'};
 await assert.rejects(session.turns.usage('turn'),/counts disagree/);
 value={...value,compactions:'1',compaction_attempts:{...zero,settled:'999'}};
 await assert.rejects(session.turns.usage('turn'),/exceed turn attempts/);
});
