import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const fixtures=JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json',import.meta.url),'utf8'));
const fixture=name=>structuredClone(fixtures.find(value=>value.type===name&&value.valid).value);
const initial={major:4,minor:0,runtime_id:'runtime',process_epoch:'boot',network_client:false,builtins:[]};
test('host bootstrap queries need no root and never retry a lost picker acknowledgement',async()=>{
 const calls=[];
 const results={'host.attention':'HostAttentionResult','host.directories.list':'HostDirectoriesResult','host.skills.complete':'HostSkillsResult','host.themes.list':'HostThemesResult'};
 const client=await Client.connect(async request=>{
  if(request.method==='initialize')return{jsonrpc:'2.0',id:request.id,result:initial};
  calls.push(request);
  if(request.method==='host.directory.pick'||request.method==='host.themes.resolve')throw new DeliveryError('lost acknowledgement');
  return{jsonrpc:'2.0',id:request.id,result:fixture(results[request.method])};
 },{clientID:'host-view'});
 await client.hostDirectories({path:'~',after:'',prefix:'work',show_hidden:false,limit:64});
 await client.completeHostSkills({scope:'global',cwd:'',prefix:'',definition:null,limit:32});
 await client.hostThemes();
 await client.hostAttention({after:null,limit:100,max_bytes:524288});
 await assert.rejects(client.pickHostDirectory('/tmp/space '),DeliveryError);
 await assert.rejects(client.resolveHostTheme({name:'dark',json:''}),DeliveryError);
 assert.deepEqual(calls.map(value=>value.method),['host.directories.list','host.skills.complete','host.themes.list','host.attention','host.directory.pick','host.themes.resolve']);
 assert.deepEqual(calls[4].params,{start:'/tmp/space '});
 const count=calls.length;
 await assert.rejects(client.pickHostDirectory('',{signal:AbortSignal.abort()}),error=>error.name==='AbortError');
 await assert.rejects(client.hostDirectories({path:'/',after:'',prefix:'',show_hidden:false,limit:0}),TypeError);
 assert.equal(calls.length,count);
});


test('workspace completion uses exact selected session, validates bounds, and preserves explicit truncation',async()=>{
 const calls=[];
 const client=await Client.connect(async request=>{
  if(request.method==='initialize')return{jsonrpc:'2.0',id:request.id,result:initial};
  calls.push(request);return{jsonrpc:'2.0',id:request.id,result:fixture('WorkspaceCompletionResult')};
 },{clientID:'completion'});
 const params={session_id:'child',kind:'mention',prefix:'~/project/',limit:64};
 const result=await client.completeWorkspace(params);
 assert.deepEqual(calls[0].params,params);assert.equal(result.truncated,true);assert.equal(result.working_directory,'/workspace');
 assert.deepEqual(result.candidates,[{text:'@docs/roadmap.md',description:''},{text:'@docs/',description:'dir'}]);
 for(const invalid of [{...params,limit:65},{...params,kind:'skill'},{...params,prefix:'x'.repeat(4097)}])await assert.rejects(client.completeWorkspace(invalid),TypeError);
 await assert.rejects(client.completeWorkspace(params,{signal:AbortSignal.abort()}),error=>error.name==='AbortError');
 assert.equal(calls.length,1);
});
