import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
export async function hostViewsAcceptance(runtime,client,createParams,evidence,deadline) {
 const workspace=join(runtime.directory,'host-preview');await mkdir(join(workspace,'.agents','skills','preview'),{recursive:true});
 await writeFile(join(workspace,'.agents','skills','preview','SKILL.md'),'---\nname: preview\ndescription: Host preview metadata\ndisable-model-invocation: true\n---\nSECRET_BODY_NOT_METADATA');
 const before=await client.call('trees.catalog',{},deadline());
 const directories=await client.hostDirectories({path:workspace,after:'',prefix:'',show_hidden:true,limit:64},deadline());
 assert.ok(directories.entries.some(entry=>entry.name==='.agents'));
 const skills=await client.completeHostSkills({scope:'project',cwd:workspace,prefix:'pre',definition:null,limit:32},deadline());
 assert.deepEqual(skills.candidates,[{text:'$preview',description:'Host preview metadata'}]);
 assert.equal(JSON.stringify(skills).includes('SECRET_BODY'),false);
 const themes=join(runtime.directory,'state','themes');await mkdir(themes,{recursive:true});
 await writeFile(join(themes,'fixture.json'),' {"name":"host-fixture","dark":true,"palette":{"primary":"#abcdef"}}');
 const catalog=await client.hostThemes(deadline());assert.ok(catalog.themes.some(theme=>theme.id==='host-fixture'));
 const resolved=await client.resolveHostTheme({name:'host-fixture',json:''},deadline());assert.equal(resolved.colors.primary,'#abcdef');
 assert.deepEqual(await client.call('trees.catalog',{},deadline()),before);
 const {root}=await client.call('trees.create',{...createParams,creation_id:'host-attention',working_directory:workspace},deadline());
 const childDirectory=join(runtime.directory,'completion-child');await mkdir(childDirectory);
 await writeFile(join(workspace,'parent-note.md'),'PARENT_FILE_BODY');await writeFile(join(childDirectory,'child-note.md'),'CHILD_FILE_BODY');
 const child=await client.spawn({parent_id:root.id,overrides:{},working_directory:childDirectory,parts:[{type:'text',text:'completion child'}],grant_ids:[]},'completion-child',deadline());
 const parentCompletion=await client.completeWorkspace({session_id:root.id,kind:'mention',prefix:'note',limit:64},deadline());
 const childCompletion=await client.completeWorkspace({session_id:child.session.id,kind:'mention',prefix:'note',limit:64},deadline());
 assert.deepEqual(parentCompletion,{working_directory:workspace,candidates:[{text:'@parent-note.md',description:''}],truncated:false});
 assert.deepEqual(childCompletion,{working_directory:childDirectory,candidates:[{text:'@child-note.md',description:''}],truncated:false});
 const explicit=await client.completeWorkspace({session_id:child.session.id,kind:'path',prefix:workspace+'/parent',limit:64},deadline());
 assert.deepEqual(explicit.candidates,[{text:workspace+'/parent-note.md',description:''}]);
 assert.equal(JSON.stringify([parentCompletion,childCompletion,explicit]).includes('_FILE_BODY'),false);
 await client.wait('completion-child',deadline());
 await client.callTool(root.id,{module:'files',name:'read',arguments_base64:Buffer.from('{"path":".agents/skills/preview/SKILL.md"}').toString('base64')},'host-attention-read',deadline());
 let observed;
 for(let attempt=0;attempt<100;attempt++) {
   const page=await client.hostAttention({after:null,limit:100,max_bytes:524288},deadline());
   observed=page.items.find(item=>item.session_id===root.id);
   if(observed?.activity.pending_permission_count==='1')break;
   await new Promise(resolve=>setTimeout(resolve,10));
 }
 assert.equal(observed?.activity.pending_permission_count,'1');assert.equal(observed.root_id,root.id);
 const permissions=await client.call('permissions.list',{session_id:root.id,limit:10},deadline());
 await client.call('permissions.resolve',{operation_id:permissions.items.find(item=>item.state==='pending').operation_id,approved:false},deadline());
 assert.equal((await client.wait('host-attention-read',deadline())).turn.state,'failed');
 evidence.push({host_views:{directories,skills,theme:resolved.id,workspace_completion:{parent:parentCompletion,child:childCompletion}}});
}
