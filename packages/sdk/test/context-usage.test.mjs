import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
const fixtures=JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json',import.meta.url),'utf8'));
const fixture=()=>structuredClone(fixtures.find(item=>item.type==='ContextUsage'&&item.valid&&item.value.prefill).value);

test('context usage exposes exact latest prefill and stale tail without converting it into cumulative occupancy',async()=>{
 let value=fixture();const calls=[];
 const client=await Client.connect(async request=>{
  if(request.method==='initialize')return {jsonrpc:'2.0',id:request.id,result:{major:4,minor:0,runtime_id:'runtime',process_epoch:'boot',network_client:false,builtins:[]}};
  calls.push(request);return {jsonrpc:'2.0',id:request.id,result:value};
 },{clientID:'context'});
 const session=client.session('session_root');
 const read=await session.context.usage();
 assert.equal(read.basis,'latest_prefill');
 assert.equal(read.prefill.input_tokens,'9007199254740993');
 assert.equal(read.prefill.stale,true);
 assert.equal(read.through_sequence,'9007199254740994');
 assert.deepEqual(calls.map(r=>[r.method,r.params]),[['context.usage',{session_id:'session_root'}]]);
 value={...fixture(),session_id:'foreign'};
 await assert.rejects(session.context.usage(),/another session/);
 value=fixture();value.prefill.stale=false;
 await assert.rejects(session.context.usage(),/tail evidence/);
 value=fixture();value.prefill.through_sequence='9007199254740995';
 await assert.rejects(session.context.usage(),/tail evidence/);
 value=fixture();value.unavailable_reason='configuration_changed';
 await assert.rejects(session.context.usage(),/availability/);
 value=fixture();value.prefill.context_window_tokens='0';
 await assert.rejects(session.context.usage(),/capacity/);
 value=fixture();value.prefill.context_window_tokens=null;
 assert.equal((await session.context.usage()).prefill.context_window_tokens,null);
 value={...fixture(),prefill:null,unavailable_reason:'selection_changed'};
 assert.equal((await session.context.usage()).prefill,null);
 const count=calls.length;
 await assert.rejects(session.context.usage({signal:AbortSignal.abort()}),error=>error.name==='AbortError');
 assert.equal(calls.length,count);
});
