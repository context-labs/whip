import {readFile,writeFile,mkdtemp,mkdir,rm} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
const repository=process.env.WHIP_COMPARISON_REPOSITORY;if(!repository)throw new Error('WHIP_COMPARISON_REPOSITORY is required');
const require=createRequire(path.join(repository,'package.json'));
const ts=require('typescript'),asar=require('@electron/asar');
const folder=await mkdtemp(path.join(tmpdir(),'whip-local-profile-'));
const bundle=path.join(repository,'apps/desktop/out/Whip-darwin-arm64/Whip.app');
const source=path.join(bundle,'Contents/Helpers');
const originalManifest=JSON.parse(asar.extractFile(path.join(bundle,'Contents/Resources/app.asar'),'runtime-manifest.json').toString());
// Diagnostic copy only. This historical package predates the protocolMinor field.
const manifest=structuredClone(originalManifest);manifest.compatibility.protocolMinor??=1;
const originalSource=await readFile(path.join(repository,'apps/desktop/src/runtime.ts'),'utf8');
const tree=ts.createSourceFile('runtime.ts',originalSource,ts.ScriptTarget.Latest,true);
const meta={run:'{executable,args:args.slice(0,3)}',fileDigest:'{filename}',verifyRuntime:'{directory}',installRuntime:'{source,root}',runtimeEnvironment:'{}',prepareLocal:'{}'};
const edits=[];
for(const fn of tree.statements)if(ts.isFunctionDeclaration(fn)&&fn.body&&meta[fn.name?.text]){
 const name=fn.name.text;
 edits.push([fn.body.getStart(tree)+1,`\nconst __whipProfileStart=performance.now();try {\n`]);
 edits.push([fn.body.end-1,`\n} finally {globalThis.__whipRuntimeProfile.push({name:${JSON.stringify(name)},detail:${meta[name]},started:__whipProfileStart,durationMs:performance.now()-__whipProfileStart});}\n`]);
}
let instrumented=originalSource;
for(const [offset,text] of edits.sort((a,b)=>b[0]-a[0]))instrumented=instrumented.slice(0,offset)+text+instrumented.slice(offset);
const compiled=ts.transpileModule(instrumented,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022}}).outputText;
await writeFile(path.join(folder,'runtime.instrumented.mjs'),compiled);
const runtime=await import(pathToFileURL(path.join(folder,'runtime.instrumented.mjs')));
globalThis.__whipRuntimeProfile=[];
const signal=AbortSignal.timeout(180000);
const rounds=[];const homes=[];
try {
 for(let round=0;round<4;round++){
  const home=path.join(folder,`h${round}`),retainedRoot=path.join(folder,`r${round}`);await mkdir(home);homes.push(home);
  await writeFile(path.join(home,'config.json'),JSON.stringify({providers:{fixture:{name:'Isolated fixture',baseUrl:'http://127.0.0.1:1/v1',apiKeyEnv:'WHIP_UNUSED_FIXTURE_KEY'}},models:{fixture:{providers:['fixture']}},defaultModel:'fixture',defaultProvider:'fixture',mcpServers:{},mcpImport:{}}));
  process.env.WHIP_HOME=home;
  for(const scenario of ['first-install','warm-attach','warm-attach','retained-restart']){
   if(scenario==='retained-restart')await runtime.run(path.join(source,'whip'),['daemon','stop'],process.env,signal);
   globalThis.__whipRuntimeProfile=[];const progress=[];const started=performance.now();
   const socket=await runtime.prepareLocal({source,retainedRoot,manifest,signal,progress:message=>progress.push({message,elapsedMs:performance.now()-started})});
   const elapsedMs=performance.now()-started;
   const spans=globalThis.__whipRuntimeProfile.map(e=>({...e,started:e.started-started}));
   const status=JSON.parse(await runtime.run(path.join(source,'whip'),['daemon','status','--json'],process.env,signal));
   if(status.state!=='running'||status.socket!==socket||status.network_endpoint)throw new Error('Unexpected fixture daemon state');
   rounds.push({round,scenario,elapsedMs,progress,spans,status:{pid:status.pid,state:status.state,socket:status.socket,network:!!status.network_endpoint}});
   console.log(round,scenario,elapsedMs.toFixed(1),spans.filter(s=>['verifyRuntime','runtimeEnvironment','installRuntime'].includes(s.name)).map(s=>`${s.name}:${s.durationMs.toFixed(1)}`).join(' '));
  }
  await runtime.run(path.join(source,'whip'),['daemon','stop'],process.env,signal);
 }
 const output={recordedAt:new Date().toISOString(),purpose:'Read-only diagnostic of source prepareLocal using unchanged signed packaged helper bytes; excludes GUI and SDK connection',bundle,sourceSHA256:createHash('sha256').update(originalSource).digest('hex'),originalManifest,diagnosticManifest:manifest,manifestSupplement:'protocolMinor=1 from current internal/protocol/types.go; not a packaged-artifact acceptance claim',startingPATH:process.env.PATH,shell:process.env.SHELL,rounds};
 await writeFile(process.env.WHIP_RUNTIME_PROFILE_OUTPUT || '/tmp/whip-runtime-profile-results.json',JSON.stringify(output,null,2));
}finally{
 for(const home of homes){process.env.WHIP_HOME=home;await runtime.run(path.join(source,'whip'),['daemon','stop'],process.env,new AbortController().signal).catch(()=>{});}
 await rm(folder,{recursive:true,force:true});
}
