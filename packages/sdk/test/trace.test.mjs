import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const fixtures=JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json',import.meta.url),'utf8'));
const page=fixtures.find(value=>value.type==='TracePageResult'&&value.valid).value;
const params={root_id:'root',after:'0',expected_revision:null,trace_id:'',roots_only:false,limit:10,max_bytes:524288};
test('trace keeps exact cursor and tombstones with explicit conflict and export recovery',async()=>{
 const calls=[];
 const client=await Client.connect(async request=>{
  if(request.method==='initialize')return{jsonrpc:'2.0',id:request.id,result:{major:4,minor:0,runtime_id:'runtime',process_epoch:'boot',network_client:false,builtins:[]}};
  calls.push(request);
  if(request.method==='trace.export')throw new DeliveryError('lost acknowledgement');
  if(request.params.expected_revision!==null)return{jsonrpc:'2.0',id:request.id,error:{code:-32001,message:'changed',kind:'CONFLICT'}};
  return{jsonrpc:'2.0',id:request.id,result:page};
 },{clientID:'trace-reader'});
 const result=await client.tracePage(params);
 assert.equal(result.next,'9007199254740999');assert.equal(result.items[1].span,null);
 assert.equal(result.items[0].span.attributes[0].count,'9007199254740993');
 await assert.rejects(client.tracePage({...params,after:result.next,expected_revision:result.revision}),error=>error.kind==='CONFLICT');
 await assert.rejects(client.exportTrace({root_id:'root',trace_id:'',expected_revision:result.revision}),DeliveryError);
 assert.deepEqual(calls.map(call=>call.method),['trace.page','trace.page','trace.export']);
 await assert.rejects(client.tracePage({...params,after:0}),TypeError);
 await assert.rejects(client.tracePage(params,{signal:AbortSignal.abort()}),error=>error.name==='AbortError');
 assert.equal(calls.length,3);
});

test('backward trace reads use explicit newest or exact exclusive cursor and reject ambiguous directions', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] } };
    calls.push(request);
    return { jsonrpc: '2.0', id: request.id, result: page };
  }, { clientID: 'trace-reader' });
  const { after, ...common } = params;
  await client.tracePage({ ...common, before: null });
  await client.tracePage({ ...common, before: '9007199254740993' });
  assert.deepEqual(calls.map(call => call.params.before), [null, '9007199254740993']);
  for (const invalid of [common, { ...common, after: '0', before: null }, { ...common, after: null }, { ...common, before: 1 }]) {
    await assert.rejects(client.tracePage(invalid), TypeError);
  }
  assert.equal(calls.length, 2);
});
