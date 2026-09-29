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
