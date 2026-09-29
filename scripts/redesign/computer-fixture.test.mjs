import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

const deadline=()=>({signal:AbortSignal.timeout(15_000)});
async function until(read,predicate){const end=Date.now()+15_000;while(Date.now()<end){const value=await read();if(predicate(value))return value;await new Promise(resolve=>setTimeout(resolve,10))}throw new Error('computer fixture observation deadline')}

test('production computer controls require reviewed authority and retain scoped fake-helper images without replay',{timeout:120_000},async t=>{
 const directory=await mkdtemp('/tmp/whip-computer-public-'),binary=join(directory,'runtime'),helper=join(directory,'fake-helper');
 let child,diagnostic='';
 const stop=async()=>{if(!child||child.exitCode!==null||child.signalCode!==null)return;const ended=once(child,'exit');child.kill('SIGTERM');const timer=setTimeout(()=>child.kill('SIGKILL'),5000);try{await ended}finally{clearTimeout(timer)}};
 t.after(async()=>{await stop();await rm(directory,{recursive:true,force:true})});
 const exec=promisify(execFile);
 await exec('go',['build','-race=false','-o',binary,'./cmd/whip-runtime'],{timeout:60_000});
 await exec('go',['build','-o',helper,'./internal/computer/testdata/controlledhelper/main.go'],{timeout:60_000});
 const start=async()=>{
  child=spawn(binary,['-directory',join(directory,'state')],{stdio:['ignore','pipe','pipe']});child.stderr.on('data',chunk=>diagnostic=(diagnostic+chunk).slice(-(1<<20)));
  return new Promise((resolve,reject)=>{let text='';const cleanup=()=>{clearTimeout(timer);child.stdout.off('data',data);child.off('error',failed);child.off('exit',exited)};const failed=error=>{cleanup();reject(error)};const exited=()=>failed(new Error('runtime exited: '+diagnostic));const data=chunk=>{text+=chunk;const end=text.indexOf('\n');if(end<0)return;try{const info=JSON.parse(text.slice(0,end));cleanup();resolve(info)}catch(error){failed(error)}};const timer=setTimeout(()=>failed(new Error('runtime startup timeout: '+diagnostic)),15_000);child.stdout.on('data',data);child.on('error',failed);child.on('exit',exited)});
 };
 let info=await start(),drop=false;
 const connect=async()=>{const transport=unixSocket(info.socket);return Client.connect(async(...args)=>{const result=await transport(...args);if(drop&&args[0].method==='tool.call'){drop=false;throw new DeliveryError('accepted reply deliberately lost')}return result},{clientID:'computer-fixture',expectedRuntimeID:info.runtime_id,...deadline()})};
 let client=await connect();
 const initial=await client.computerStatus(deadline());assert.equal(initial.state,'disabled');
 const configured=await client.configureComputer({revision:initial.revision,configuration:{enabled:true,helper_executable:helper,allow:[],deny:[],default_deny:true}},deadline());
 assert.equal(configured.state,'available');await assert.rejects(readFile(helper+'.calls'),error=>error.code==='ENOENT');
 for(const engine of ['starlark','quickjs']){
  const {root}=await client.call('trees.create',{creation_id:'computer-'+engine,engine,definition:client.builtins[0],working_directory:directory,metadata:{title:null,pinned:false,archived:false},overrides:{modules:['computer'],automatic_title:false}},deadline());
  assert.equal(root.configuration.model.provider,'');
  const schemas=await client.hostToolSchemas(root.id,deadline());assert.deepEqual(schemas.items.map(value=>[value.module,value.name]),[['computer','run']]);
  const operation={module:'computer',name:'run',arguments_base64:Buffer.from(JSON.stringify({code:'state("Test App");click("Test App",0)'})).toString('base64')};
  const requestID='native-'+engine;drop=true;await assert.rejects(client.callTool(root.id,operation,requestID,deadline()),DeliveryError);
  const pending=await until(()=>client.call('permissions.list',{session_id:root.id,limit:10},deadline()),value=>value.items.some(row=>row.state==='pending'));
  const approval=pending.items.find(row=>row.state==='pending');const waiting=await client.call('operations.get',{operation_id:approval.operation_id},deadline());
  assert.equal(waiting.capability,'computer.run');assert.deepEqual(waiting.arguments.intent.applications,['test app']);assert.equal(waiting.arguments.intent.broad_applescript,false);
  await client.call('permissions.resolve',{operation_id:approval.operation_id,approved:true},deadline());
  const done=await client.wait(requestID,deadline());assert.equal(done.turn.state,'succeeded',done.turn.failure);
  const effects=await client.call('turns.operations',{turn_id:done.turn.id,limit:10},deadline());const effect=effects.items[0];assert.equal(effect.cell_id,null);assert.equal(effect.origin,'host_operation');assert.equal(effect.result.content_references.length,2);
  for(const reference_id of effect.result.content_references){const content=await client.call('content.read',{session_id:root.id,reference_id},deadline());assert.equal(content.reference.media_type,'image/jpeg');assert.ok(Buffer.from(content.data_base64,'base64').length>0)}
  assert.deepEqual((await client.call('sessions.history',{session_id:root.id,after:'0',limit:10},deadline())).items,[]);
  assert.deepEqual((await client.call('turns.cells',{turn_id:done.turn.id,limit:10},deadline())).items,[]);
  assert.deepEqual((await client.call('turns.attempts',{turn_id:done.turn.id,limit:10},deadline())).items,[]);
  const recorded=await readFile(helper+'.calls','utf8');await stop();info=await start();client=await connect();
  const restarted=await client.computerStatus(deadline());assert.notEqual(restarted.generation,configured.generation);assert.equal(restarted.state,'available');
  assert.equal((await client.callTool(root.id,operation,requestID,deadline())).input.id,done.input.id);assert.equal(await readFile(helper+'.calls','utf8'),recorded);
 }
 await stop();assert.equal(child.exitCode,0,diagnostic);
});
