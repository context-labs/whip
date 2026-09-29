import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
export async function hostViewsAcceptance(runtime,client,evidence,deadline) {
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
 evidence.push({host_views:{directories,skills,theme:resolved.id}});
}
