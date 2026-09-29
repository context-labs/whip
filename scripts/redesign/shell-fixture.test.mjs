import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import { join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

const exec = promisify(execFile);
const options = () => ({ signal: AbortSignal.timeout(10_000) });
async function until(read, predicate) {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) { const value = await read(); if (predicate(value)) return value; await new Promise(resolve => setTimeout(resolve, 15)); }
  throw new Error('Shell fixture observation deadline');
}

test('production shell keeps scoped jobs, transient human input, bounded content and joined stop', { timeout: 120_000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-shell-');
  const binary = join(directory, 'runtime');
  let child;
  let diagnostic = '';
  const record = chunk => { diagnostic = (diagnostic + chunk.toString()).slice(-(1 << 20)); };
  const stop = async () => {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const ended = once(child, 'exit'); child.kill('SIGTERM');
    const timer = setTimeout(() => child.kill('SIGKILL'), 5000);
    try { await ended; } finally { clearTimeout(timer); }
  };
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) { raw += chunk; if (raw.length > 4 << 20) { response.destroy(); return; } }
    const body = JSON.parse(raw), last = body.messages.at(-1);
    const action = body.messages.findLast(value => value.role === 'user').content;
    const interactive = "stty -echo; printf prompt; read value; printf first; read extra; printf 'len:%s' ${#value}";
    const large = "head -c 1100000 /dev/zero | tr '\\0' x";
    const code = body.model === 'starlark'
      ? (action === 'interact' ? `j=shell.start(command="printf background; sleep 30"); print(shell.run(command=${JSON.stringify(interactive)},interactive=True))` : `print(shell.poll(id=j["id"])); print(shell.run(command=${JSON.stringify(large)}))`)
      : (action === 'interact' ? `var j=await shell.start({command:"printf background; sleep 30"}); console.log(await shell.run({command:${JSON.stringify(interactive)},interactive:true}))` : `console.log(await shell.poll({id:j.id})); console.log(await shell.run({command:${JSON.stringify(large)}}))`);
    const message = last.role === 'tool' ? { role: 'assistant', content: last.content } : { role: 'assistant', content: null, tool_calls: [{ id:'shell-call',type:'function',function:{name:'execute',arguments:JSON.stringify({code})} }] };
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({choices:[{message,finish_reason:last.role === 'tool' ? 'stop' : 'tool_calls'}]}));
  });
  t.after(async () => { await stop(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); await rm(directory,{recursive:true,force:true}); });
  await exec('go',['build','-race=false','-o',binary,'./cmd/whip-runtime'],{cwd:resolve('.'),timeout:60_000});
  const start = async scripted => {
    child=spawn(binary,['-directory',join(directory,'state'),...(scripted?['-scripted']:[])],{stdio:['ignore','pipe','pipe']});
    child.stderr.on('data',record);
    return new Promise((resolve,reject)=>{
      let raw='';
      const cleanup=()=>{clearTimeout(timer);child.stdout.off('data',data);child.off('error',failed);child.off('exit',exited);};
      const failed=error=>{cleanup();reject(error);};
      const exited=()=>failed(new Error('Shell runtime exited\n'+diagnostic));
      const data=chunk=>{raw+=chunk;if(raw.includes('\n')){try{const value=JSON.parse(raw.slice(0,raw.indexOf('\n')));cleanup();resolve(value);}catch(error){failed(error);}}};
      const timer=setTimeout(()=>failed(new Error('Runtime startup deadline\n'+diagnostic)),15_000);
      child.stdout.on('data',data);child.on('error',failed);child.on('exit',exited);
    });
  };
  await start(true); await stop();
  server.listen(0,'127.0.0.1');await once(server,'listening');
  const path=join(directory,'state','host.json'), host=JSON.parse(await readFile(path,'utf8'));
  host.providers.shell_fixture={kind:'openai-chat',base_url:`http://127.0.0.1:${server.address().port}/v1`,credential_env:'',models:{starlark:{max_attempts:1},quickjs:{max_attempts:1}}};
  await writeFile(path,JSON.stringify(host),{mode:0o600});
  const ready=await start(false), transport=unixSocket(ready.socket);
  let loseReply=false;
  const client=await Client.connect(async (...args)=>{
    const result=await transport(...args);
    if(loseReply && args[0].method==='shell.input'){loseReply=false;throw new DeliveryError('deliberately lost accepted input reply');}
    return result;
  },{clientID:'shell-fixture',...options()});
  for(const engine of ['starlark','quickjs']) {
    const {root}=await client.call('trees.create',{creation_id:'shell-'+engine,engine,definition:client.builtins[0],working_directory:directory,metadata:{title:null,pinned:false,archived:false},overrides:{model:{provider:'shell_fixture',name:engine,effort:''},automatic_title:false}},options());
    for(const verb of ['run','start','poll']) await client.call('grants.create',{id:`${engine}-${verb}`,session_id:root.id,capability:`shell.${verb}`,resource:root.working_directory},options());
    await client.submit(root.id,[{type:'text',text:'interact'}],`${engine}-interact`,options());
    const current=await until(()=>client.shellInteraction(root.id,'0',options()),value=>value.interaction && Buffer.from(value.interaction.data_base64,'base64').includes('prompt'));
    const params={session_id:root.id,operation_id:current.interaction.operation_id,sequence:'1',data_base64:Buffer.from('private-input\n').toString('base64')};
    loseReply=true;await assert.rejects(client.shellInput(params,options()),DeliveryError);
    assert.equal((await client.shellInteraction(root.id,'0',options())).interaction.next_input,'2');
    await client.shellInput(params,options());
    await assert.rejects(client.shellInput({...params,data_base64:'eA=='},options()),error=>error.kind==='CONFLICT');
    await client.shellInput({...params,sequence:'2',data_base64:'Cg=='},options());
    const done=await client.wait(`${engine}-interact`,options());assert.equal(done.turn.state,'succeeded',done.turn.failure);
    assert.equal((await client.shellInteraction(root.id,'0',options())).interaction,null);
    const effects=await client.call('turns.operations',{turn_id:done.turn.id,limit:10},options());
    const foreground=effects.items.find(value=>value.capability==='shell.run');
    assert.equal(foreground.state,'succeeded');assert.ok(JSON.stringify(foreground.result.value).includes('len:13'));assert.equal(JSON.stringify(foreground).includes('private-input'),false);
    const job=effects.items.find(value=>value.capability==='shell.start').result.value;
    await client.submit(root.id,[{type:'text',text:'large'}],`${engine}-large`,options());
    const large=await client.wait(`${engine}-large`,options());assert.equal(large.turn.state,'succeeded',large.turn.failure);
    const outputs=await client.call('turns.operations',{turn_id:large.turn.id,limit:10},options());
    assert.equal(outputs.items.find(value=>value.capability==='shell.poll').result.value.running,true);
    const output=outputs.items.find(value=>value.capability==='shell.run').result.value;
    assert.equal(output.bytes,'1100000');assert.equal(output.retained_bytes,'1048576');assert.equal(output.truncated,true);
    const content=await client.call('content.read',{session_id:root.id,reference_id:output.content_ref},options());assert.equal(Buffer.from(content.data_base64,'base64').length,1<<20);
    await client.call('sessions.lifecycle',{session_id:root.id,lifecycle:'stopped'},options());
    assert.throws(()=>process.kill(-job.pid,0),error=>error.code==='ESRCH');
    await assert.rejects(client.shellInput({...params,sequence:'3'},options()),error=>error.kind==='NOT_FOUND');
  }
  await stop();assert.equal(child.exitCode,0,diagnostic);
});
