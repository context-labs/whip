import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

export async function hostOperationAcceptance(runtime, client, createParams, evidence, deadline, dropAcknowledgement) {
  for (const engine of ['starlark','quickjs']) {
    const workspace=join(runtime.directory,`direct-${engine}`);await mkdir(workspace);
    const {root}=await client.call('trees.create',{...createParams,creation_id:randomUUID(),engine,working_directory:workspace,overrides:{report_mode:'message',modules:['shell','files']}},deadline());
    assert.equal(root.configuration.model.provider,'');
    assert.equal((await client.hostToolSchemas(root.id,deadline())).items.length,7);
    await client.call('grants.create',{id:`direct-shell-${engine}`,session_id:root.id,capability:'shell.run',resource:root.working_directory},deadline());
    const requestID=`direct-${engine}`;const command='printf once >> count; printf direct';
    const proxy=join(runtime.directory,`direct-${engine}.sock`);let dropped;
    const close=await dropAcknowledgement(proxy,runtime.info.socket,requestID,value=>{dropped=value});
    try {
      const unreliable=await Client.connect(unixSocket(proxy),{clientID:client.clientID,expectedRuntimeID:client.runtimeID,...deadline()});
      await assert.rejects(unreliable.runShell(root.id,command,requestID,deadline()),DeliveryError);
      assert.equal(dropped.input.kind,'host_operation');
    } finally {await close()}
    const done=await client.wait(requestID,deadline());assert.equal(done.turn.state,'succeeded',done.turn.failure);
    const operations=await client.call('turns.operations',{turn_id:done.turn.id,limit:10},deadline());
    assert.equal(operations.items.length,1);assert.equal(operations.items[0].origin,'host_operation');assert.equal(operations.items[0].cell_id,null);assert.equal(operations.items[0].result.value.output,'direct');
    assert.deepEqual((await client.call('turns.cells',{turn_id:done.turn.id,limit:10},deadline())).items,[]);
    assert.deepEqual((await client.call('turns.attempts',{turn_id:done.turn.id,limit:10},deadline())).items,[]);
    assert.deepEqual((await client.call('sessions.history',{session_id:root.id,after:'0',limit:10},deadline())).items,[]);
    await runtime.stop();await runtime.start(null);
    assert.equal((await client.runShell(root.id,command,requestID,deadline())).input.id,done.input.id);
    assert.equal(await readFile(join(workspace,'count'),'utf8'),'once');
    const fileRequest=`direct-read-${engine}`;
    await client.callTool(root.id,{module:'files',name:'read',arguments_base64:Buffer.from('{"path":"count"}').toString('base64')},fileRequest,deadline());
    let permissions;
    for(let attempt=0;attempt<100;attempt++){permissions=await client.call('permissions.list',{session_id:root.id,limit:10},deadline());if(permissions.items.some(item=>item.state==='pending'))break;await new Promise(resolve=>setTimeout(resolve,10))}
    const pending=permissions.items.find(item=>item.state==='pending');assert.ok(pending);
    await client.call('permissions.resolve',{operation_id:pending.operation_id,approved:true},deadline());
    assert.equal((await client.wait(fileRequest,deadline())).turn.state,'succeeded');
    evidence.push({host_operation:{engine,done,operations}});
  }
}
