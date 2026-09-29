import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../dist/index.js';
const initial={major:4,minor:0,runtime_id:'runtime',process_epoch:'boot_test',network_client:false,builtins:[]};
const terminal={process_epoch:'boot_test',id:'terminal',cwd:'/workspace',shell:'/bin/sh',cols:80,rows:24,closing:false,exited:false,exit_code:0,signal:'',start:'9007199254740993',end:'9007199254740996',created_at:new Date().toISOString()};

test('human terminal helpers retain process identity, exact cursors and raw bytes without replay',async()=>{
 const calls=[];let failure;const client=await Client.connect(async request=>{calls.push(structuredClone(request));if(failure)throw failure;let result=initial;switch(request.method){case 'terminal.open':case 'terminal.resize':result=terminal;break;case 'terminal.list':result={process_epoch:'boot_test',items:[terminal]};break;case 'terminal.read':result={terminal,from:terminal.start,next:terminal.end,end:terminal.end,truncated:true,data_base64:'AAH/'};break;case 'terminal.write':case 'terminal.close':result={accepted:true};break;}return{jsonrpc:'2.0',id:request.id,result};},{clientID:'human'});
 assert.equal(client.processEpoch,'boot_test');const opened=await client.openTerminal({cwd:'/workspace',cols:80,rows:24});assert.deepEqual(opened,terminal);assert.equal(calls.at(-1).params.process_epoch,'boot_test');assert.equal((await client.listTerminals()).items[0].id,'terminal');const page=await client.readTerminal(opened,terminal.start,3);assert.equal(page.next,'9007199254740996');assert.equal(calls.at(-1).params.cursor,'9007199254740993');
 await client.writeTerminal(opened,new Uint8Array([0,1,255]));assert.equal(calls.at(-1).params.data_base64,'AAH/');await client.resizeTerminal(opened,90,30);await client.closeTerminal(opened);
 const before=calls.length;assert.throws(()=>client.writeTerminal(opened,new Uint8Array()),/1..16384/);await assert.rejects(client.readTerminal(opened,9007199254740993,1),TypeError);assert.equal(calls.length,before);
 failure=new DeliveryError('lost acknowledgement');await assert.rejects(client.openTerminal({cwd:'/workspace',cols:80,rows:24}),DeliveryError);assert.equal(calls.length,before+1);await assert.rejects(client.writeTerminal(opened,new Uint8Array([65])),DeliveryError);assert.equal(calls.length,before+2);
 failure=undefined;await client.readTerminal({...opened,process_epoch:'old_epoch'},'0',1);assert.equal(calls.at(-1).params.process_epoch,'old_epoch');
});
test('terminal write uncertainty remains explicit and is never retried',async()=>{
 let calls=0;const client=await Client.connect(async request=>request.method==='initialize'?{jsonrpc:'2.0',id:request.id,result:initial}:(calls++,{jsonrpc:'2.0',id:request.id,error:{code:-32016,kind:'TERMINAL_WRITE_UNCERTAIN',message:'input may have been written'}}),{clientID:'human'});
 await assert.rejects(client.writeTerminal(terminal,new Uint8Array([65])),error=>error instanceof RemoteError&&error.kind==='TERMINAL_WRITE_UNCERTAIN');assert.equal(calls,1);
});
