import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major:4,minor:0,runtime_id:'runtime',process_epoch:'boot',network_client:false,builtins:[] };
const success = (request,result) => ({jsonrpc:'2.0',id:request.id,result});

test('direct host admission preserves exact arguments and recovers without automatic execution replay',async()=>{
  const calls=[];
  const admission=fixture('Admission');
  const client=await Client.connect(async request=>{
    if(request.method==='initialize')return success(request,initial);
    calls.push(structuredClone(request));
    if(request.method==='tool.schemas')return success(request,fixture('HostToolSchemasResult'));
    if(request.method==='receipts.get')return success(request,admission);
    throw new DeliveryError('lost acknowledgement after durable acceptance');
  },{clientID:'human'});
  await client.hostToolSchemas('root');
  const operation={module:'tools',name:'lookup',arguments_base64:Buffer.from('{"id":9007199254740993}').toString('base64')};
  await assert.rejects(client.callTool('root',operation,'stable'),DeliveryError);
  await client.recover('stable');
  assert.deepEqual(calls.map(value=>value.method),['tool.schemas','tool.call','receipts.get']);
  assert.deepEqual(calls[1].params,{session_id:'root',identity:{client_id:'human',request_id:'stable'},operation});
  assert.equal(Buffer.from(calls[1].params.operation.arguments_base64,'base64').toString(),'{"id":9007199254740993}');
  await assert.rejects(client.runShell('root','printf direct','shell',{timeout:12,interactive:true}),DeliveryError);
  assert.deepEqual(calls.at(-1).params,{session_id:'root',identity:{client_id:'human',request_id:'shell'},command:'printf direct',timeout:12,interactive:true});
  const count=calls.length;
  await assert.rejects(client.runShell('root','echo no','aborted',{signal:AbortSignal.abort()}),error=>error.name==='AbortError');
  await assert.rejects(client.callTool('root',{...operation,module:'agents'},'forged'),TypeError);
  assert.equal(calls.length,count);
});

test('direct operation projection has a real turn and no synthetic cell',async()=>{
  const input=structuredClone(fixtures.find(value=>value.type==='Input'&&value.value.kind==='host_operation').value);
  const client=await Client.connect(async request=>success(request,request.method==='initialize'?initial:{receipt:{identity:{client_id:'human',request_id:'direct'},digest:'a'.repeat(64),input_id:input.id,deleted_at:null,created_at:input.created_at},input,turn:null}),{clientID:'human'});
  const value=await client.callTool(input.session_id,input.host_operation,'direct');
  assert.equal(value.input.kind,'host_operation');assert.deepEqual(value.input.parts,[]);
  assert.equal(value.turn,null);assert.equal(value.input.turn_id,null);
});
