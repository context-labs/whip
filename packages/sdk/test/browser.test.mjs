import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
import { browserSocket, browserContent } from '../dist/browser.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: true, builtins: [] };
const response = (id, result) => JSON.stringify({ jsonrpc: '2.0', id, result });
class Socket {
  static OPEN=1; static CLOSING=2; static instances=[]; static respond;
  readyState=0; bufferedAmount=0;
  constructor(url) { assert.equal(typeof url,'string'); this.url=url; this.sent=[]; Socket.instances.push(this); queueMicrotask(()=> { if(this.readyState===0){this.readyState=1;this.onopen?.({});} }); }
  send(raw) { const value=JSON.parse(raw); this.sent.push(value); Socket.respond?.(this,value); }
  close() { this.readyState=3; }
  message(raw) { this.onmessage?.({data:raw}); }
}
function setup(t,respond) {
  Socket.instances=[];Socket.respond=respond;const original=globalThis.WebSocket;globalThis.WebSocket=Socket;t.after(()=>{globalThis.WebSocket=original;});
}
const transport=()=>browserSocket('http://example.test',{expectedRuntimeID:'runtime',expectedProcessEpoch:'boot_test'});

test('browser unary transport pins every connection, forces network mode and does not replay',async t=>{
 setup(t,(socket,request)=>queueMicrotask(()=>socket.message(response(request.id,request.method==='initialize'?initial:{revision:'2'}))));
 const options={expectedRuntimeID:'runtime',expectedProcessEpoch:'boot_test'};
 const wire=browserSocket('https://example.test',options); options.expectedRuntimeID='mutated';
 const client=await Client.connect(wire,{clientID:'browser',expectedRuntimeID:'runtime'});
 assert.equal((await client.treeCatalog()).revision,'2');assert.equal(Socket.instances.length,2);
 for(const socket of Socket.instances){assert.equal(socket.url,'wss://example.test/api/v4/ws');assert.equal(socket.sent[0].params.network_client,true);assert.equal(socket.sent[0].params.expected_runtime_id,'runtime');assert.equal(socket.sent[0].params.expected_process_epoch,'boot_test');assert.equal(socket.readyState,3);assert.equal(socket.onmessage,null);}
 Socket.respond=(socket,request)=>queueMicrotask(()=>{if(request.method==='initialize')socket.message(response(request.id,initial));else socket.onclose?.({code:1006,reason:'lost ACK'});});
 await assert.rejects(client.treeCatalog(),DeliveryError);assert.equal(Socket.instances.length,3);assert.equal(Socket.instances[2].sent.length,2);
});
test('browser rejects downgrade, host/generation mismatch and malformed frames before dependent calls',async t=>{
 setup(t,()=>{});
 for(const bad of [{...initial,network_client:false},{...initial,runtime_id:'other'},{...initial,process_epoch:'other'},{...initial,process_epoch:undefined}]){
  Socket.respond=(socket,request)=>queueMicrotask(()=>socket.message(response(request.id,bad)));
  await assert.rejects(Client.connect(transport(),{clientID:'browser',expectedRuntimeID:'runtime'}),TypeError);
  assert.equal(Socket.instances.at(-1).sent.length,1);
 }
 await assert.rejects(Client.connect(transport(),{clientID:'browser',expectedRuntimeID:'other'}),TypeError);
 const count=Socket.instances.length;
 for(const endpoint of ['file:///tmp/socket','http://user:password@example.test','https://example.test/api/v3/ws','https://example.test/?token=x','https://example.test/#fragment']) assert.throws(()=>browserSocket(endpoint,{expectedRuntimeID:'runtime'}),TypeError);
 assert.equal(Socket.instances.length,count);
 for(const raw of ['{',response('wrong',initial),new Uint8Array([1]),' '.repeat((8<<20)+1)]){
  Socket.respond=(socket)=>queueMicrotask(()=>socket.message(raw));
  await assert.rejects(Client.connect(transport(),{clientID:'browser'}));assert.equal(Socket.instances.at(-1).onmessage,null);
 }
});
test('native close reason wins over bare error and stays bounded; abort keeps caller reason',async t=>{
 setup(t,(socket)=>queueMicrotask(()=>{socket.onerror?.({});socket.onclose?.({code:1011,reason:'\nserver\u2028 unavailable '+ 'x'.repeat(400)});}));
 await assert.rejects(Client.connect(transport(),{clientID:'browser'}),error=>error instanceof DeliveryError&&error.message.includes('(1011): server unavailable')&&error.message.length<340&&!error.message.includes('\n'));
 const abort=new AbortController(), reason=new Error('stop only observation');
 Socket.respond=()=>abort.abort(reason);
 await assert.rejects(Client.connect(transport(),{clientID:'browser',signal:abort.signal}),error=>error===reason);
 assert.equal(Socket.instances.at(-1).onclose,null);assert.equal(Socket.instances.at(-1).readyState,3);
 Socket.respond=socket=>queueMicrotask(()=>socket.onerror?.({}));
 await assert.rejects(Client.connect(transport(),{clientID:'browser'}),/connection failed/);
});
test('browser send bounds bytes and native pending writes',async t=>{
 setup(t,socket=>queueMicrotask(()=>socket.message(response('initialize',initial))));
 const client=await Client.connect(transport(),{clientID:'browser'});
 Socket.respond=(socket,request)=>queueMicrotask(()=>{socket.bufferedAmount=8<<20;socket.message(response(request.id,initial));});
 await assert.rejects(client.treeCatalog(),/queue limit/);assert.equal(Socket.instances.at(-1).sent.length,1);
 const before=Socket.instances.length;
 const abort=new AbortController();abort.abort('cancelled');
 await assert.rejects(client.treeCatalog({signal:abort.signal}),error=>error==='cancelled');assert.equal(Socket.instances.length,before);
});
test('HTTP scoped content validates identity, bounds and digest without replay',async t=>{
 const saved=globalThis.fetch;t.after(()=>{globalThis.fetch=saved;});let calls=0;const body=new TextEncoder().encode('content');const hash=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',body))).map(x=>x.toString(16).padStart(2,'0')).join('');
 const reference={id:'reference',session_id:'owner',digest:hash,size:String(body.length),media_type:'text/plain',created_at:new Date().toISOString()};
 const content=browserContent('http://example.test',{expectedRuntimeID:'runtime',expectedProcessEpoch:'boot_test'});
 globalThis.fetch=async(url,options)=>{calls++;assert.equal(url.searchParams.get('runtime_id'),'runtime');assert.equal(url.searchParams.get('session_id'),'owner');assert.equal(url.searchParams.get('expected_process_epoch'),'boot_test');assert.equal(options.redirect,'error');return options.method==='POST'?new Response(JSON.stringify(reference),{status:201}):new Response(body,{headers:{'Content-Length':String(body.length),'X-Content-SHA256':hash}});};
 assert.deepEqual(await content.upload('owner','reference','text/plain',body),reference);assert.deepEqual(await content.download('owner','reference'),body);assert.equal(calls,2);
 await assert.rejects(content.upload('owner','reference','text/plain',new Uint8Array((4<<20)+1)),/4 MiB/);assert.equal(calls,2);
 globalThis.fetch=async()=>{calls++;return new Response(JSON.stringify({...reference,session_id:'other'}),{status:201});};
 await assert.rejects(content.upload('owner','reference','text/plain',body),/identity mismatch/);assert.equal(calls,3);
 globalThis.fetch=async()=>new Response(body,{headers:{'Content-Length':'2','X-Content-SHA256':hash}});
 await assert.rejects(content.download('owner','reference'),/exceeds limit/);
 globalThis.fetch=async()=>new Response(body,{headers:{'Content-Length':String(body.length),'X-Content-SHA256':'0'.repeat(64)}});
 await assert.rejects(content.download('owner','reference'),/digest mismatch/);
 globalThis.fetch=async()=>new Response('lost acknowledgement',{status:502});await assert.rejects(content.upload('owner','reference','text/plain',body),DeliveryError);
});
