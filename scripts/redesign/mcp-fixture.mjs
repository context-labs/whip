import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import http from 'node:http';
import { join } from 'node:path';

export async function mcpAcceptance(runtime, client, createParams, evidence, deadline) {
  const provider = http.createServer(async (request, response) => {
    let raw = ''; for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw); const last = body.messages.at(-1);
    const engine = body.messages.findLast(value => value.role === 'user').content;
    const code = engine === 'starlark' ? 'print(mcp.call(server="fixture",tool="visible",arguments={}))' : 'print(await mcp.call({server:"fixture",tool:"visible",arguments:{}}));';
    const message = last.role === 'tool' ? { role:'assistant', content:last.content } : { role:'assistant',content:null,tool_calls:[{id:'mcp-call',type:'function',function:{name:'execute',arguments:JSON.stringify({code})}}] };
    response.setHeader('content-type','application/json');response.end(JSON.stringify({choices:[{message,finish_reason:last.role==='tool'?'stop':'tool_calls'}]}));
  });
  provider.listen(0,'127.0.0.1');await once(provider,'listening');
  try {
    await runtime.stop();
    const executable=join(runtime.directory,'mcp-server.mjs');const pids=join(runtime.directory,'mcp-pids');
    await writeFile(executable, `import {appendFileSync} from 'node:fs';import {createInterface} from 'node:readline';
appendFileSync(process.env.PID_FILE,process.pid+'\\n');
for await(const line of createInterface({input:process.stdin})) {const m=JSON.parse(line);if(m.id===undefined)continue;let result={};
if(m.method==='initialize')result={protocolVersion:m.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'fixture',version:'1'},instructions:'Fixture instructions'};
if(m.method==='tools/list')result={tools:[{name:'visible',description:'Fixture echo',inputSchema:{type:'object'}}]};
if(m.method==='tools/call')result={content:[{type:'text',text:'fixture MCP call'}]};
process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result})+'\\n');}
`);
    const hostPath=join(runtime.directory,'state','host.json');const host=JSON.parse(await readFile(hostPath,'utf8'));
    host.providers.mcp_fixture={kind:'openai-chat',base_url:`http://127.0.0.1:${provider.address().port}/v1`,credential_env:'',models:{fixture:{max_output_tokens:128,timeout_millis:10000,max_attempts:1}}};
    host.mcp={servers:{},imports:{claude:{enabled:false},codex:{enabled:false},opencode:{enabled:false},project:{enabled:false}},brand_icons:false};
    await writeFile(hostPath,JSON.stringify(host),{mode:0o600});await runtime.start(null);
    const configuration=await client.mcpConfiguration(deadline());
    await client.configureMCP({revision:configuration.revision,name:'fixture',server:{command:[process.execPath,executable],env:{PID_FILE:pids},cwd:'',url:'',headers:{},enabled:null,note:'',startup_timeout_seconds:5,tool_timeout_seconds:5},remove:false,imports:null,brand_icons:null},deadline());
    const roots=[];
    for(const engine of ['starlark','quickjs']){
      const workspace=join(runtime.directory,`mcp-${engine}`);await mkdir(workspace);
      const {root}=await client.call('trees.create',{creation_id:randomUUID(),...createParams,engine,working_directory:workspace,overrides:{model:{provider:'mcp_fixture',name:'fixture',effort:''},mcp_servers:{all:false,servers:['fixture']}}},deadline());roots.push(root.id);
      assert.equal((await client.mcpStatus(root.id,deadline())).items[0].state,'not_started');
      await client.refreshMCP(root.id,deadline());
      let ready=false;for(let attempt=0;attempt<100;attempt++){if((await client.mcpStatus(root.id,deadline())).items[0].state==='ready'){ready=true;break}await new Promise(resolve=>setTimeout(resolve,20))}assert.ok(ready,'MCP connection ready');
      const tool=(await client.mcpTools(root.id,'fixture',deadline())).items[0];
      await client.call('grants.create',{id:`mcp-${engine}`,session_id:root.id,capability:tool.capability,resource:tool.resource},deadline());
      const requestID=`mcp-${engine}`;await client.submit(root.id,[{type:'text',text:engine}],requestID,deadline());
      const done=await client.wait(requestID,deadline());assert.equal(done.turn.state,'succeeded',done.turn.failure);
      const operations=await client.call('turns.operations',{turn_id:done.turn.id,limit:10},deadline());assert.equal(operations.items.length,1);assert.equal(operations.items[0].capability,'mcp.call.trusted');assert.equal(operations.items[0].state,'succeeded');
      const guidance=await client.mcpInstructions(root.id,'fixture',deadline());assert.equal(guidance.text,'Fixture instructions');
      evidence.push({mcp:{engine,done,operations}});
    }
    await runtime.stop();for(const pid of (await readFile(pids,'utf8')).trim().split('\n').map(Number))assert.throws(()=>process.kill(pid,0),error=>error.code==='ESRCH');
    await runtime.start(null);for(const root of roots)assert.equal((await client.mcpStatus(root,deadline())).items[0].state,'not_started');
  } finally {provider.closeAllConnections();await new Promise((resolve,reject)=>provider.close(error=>error?reject(error):resolve()))}
}
