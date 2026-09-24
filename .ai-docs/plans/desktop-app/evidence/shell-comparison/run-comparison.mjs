import {createServer} from 'node:http';
import {spawn,execFileSync} from 'node:child_process';
import {readFile,writeFile,cp,mkdir} from 'node:fs/promises';
import path from 'node:path';
import {performance} from 'node:perf_hooks';
import {fileURLToPath,pathToFileURL} from 'node:url';
const repository=process.env.WHIP_COMPARISON_REPOSITORY || fileURLToPath(new URL('../../../../../',import.meta.url));
const {startFixture}=await import(pathToFileURL(path.join(repository,'packages/sdk/scripts/fixture.mjs')));
const {readRendererManifest,verifyRenderer}=await import(pathToFileURL(path.join(repository,'scripts/renderer-artifact.mjs')));
const root=path.resolve('.');
const manifest=await readRendererManifest(path.join(root,'renderer-manifest.json'));
await verifyRenderer(path.join(root,'renderer'),manifest);
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const binary=path.join(root,'build/stable-macos-arm64/Whip Electrobun Comparison.app/Contents/MacOS/launcher');
const electron=path.join(repository,'node_modules/electron/dist/Electron.app/Contents/MacOS/Electron');
const runs=[];let current,control,fixture;let stopping=false;process.on('SIGTERM',()=>{stopping=true;current?.ready({kind:'stopped'});});
const processes=()=>execFileSync('/bin/ps',['-axo','pid=,ppid=,rss=,comm='],{encoding:'utf8'}).trim().split('\n').map(line=>{const m=/^\s*(\d+)\s+(\d+)\s+(\d+)\s+(.+)$/.exec(line);return {pid:+m[1],ppid:+m[2],rssKiB:+m[3],command:m[4]};});
const descendants=(rows,pid)=>{const ids=new Set([pid]);let size;do{size=ids.size;for(const p of rows)if(ids.has(p.ppid))ids.add(p.pid);}while(size!==ids.size);return rows.filter(p=>ids.has(p.pid));};
const server=createServer((req,res)=>{
 if(req.url.startsWith('/control/')){if(current&&req.url.endsWith('/'+current.id)){control=res;current.controlReady();}else{res.end('quit');}return;}
 let body='';req.on('data',c=>body+=c);req.on('end',()=>{res.writeHead(204,{'Access-Control-Allow-Origin':'*'});res.end();if(current){const event=JSON.parse(body);event.elapsedMs=performance.now()-current.started;current.events.push(event);if(event.kind==='usable'||event.kind==='failed')current.ready(event);}});
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const collector=`http://127.0.0.1:${server.address().port}`;
let index=0;
async function launch(engine,base={}) {
 const config={collector,id:`${engine}-${++index}`,renderer:path.join(root,'renderer'),userData:path.join(root,'electron-profile'),...base};
 const before=processes();
 let ready,controlReady;const readyEvent=new Promise(r=>ready=r);const controlEvent=new Promise(r=>controlReady=r);
 current={id:config.id,engine,config,events:[],started:performance.now(),ready,controlReady};control=undefined;
 const child=spawn(engine==='electron'?electron:binary,engine==='electron'?[path.join(root,'electron-main.cjs')]:[],{env:{...process.env,WHIP_COMPARISON_CONFIG:JSON.stringify(config)},stdio:['ignore','pipe','pipe']});
 current.pid=child.pid;let output='';child.stdout.on('data',b=>output+=b);child.stderr.on('data',b=>output+=b);
 const exit=new Promise(r=>child.once('exit',(code,signal)=>r({code,signal})));
 let timeout;const result=await Promise.race([readyEvent,exit.then(e=>({kind:'early-exit',...e})),new Promise(r=>timeout=setTimeout(()=>r({kind:'timeout'}),14000))]);clearTimeout(timeout);
 if(base.memory)await sleep(1500);else await sleep(30);
 const rows=processes();
 const tree=descendants(rows,child.pid);
 const prior=new Set(before.map(p=>p.pid));
 const webkitCandidates=rows.filter(p=>!prior.has(p.pid)&&/com\.apple\.WebKit/.test(p.command)&&!tree.some(q=>q.pid===p.pid));
 const daemon=fixture?rows.find(p=>p.command===path.join(fixture.directory,'daemon.test')):undefined;
 const record={recordedAt:new Date().toISOString(),daemonRSSKiB:daemon?.rssKiB,engine,id:config.id,mode:base.endpoint?'retained-session-loopback':'bundled-scheme',settleMs:base.memory?1500:30,warmup:!!base.warmup,result:result.kind,events:current.events,processTree:tree,webkitNewOutsideTreeCandidates:webkitCandidates,treeRSSKiB:tree.reduce((n,p)=>n+p.rssKiB,0),candidateRSSKiB:webkitCandidates.reduce((n,p)=>n+p.rssKiB,0)};
 await Promise.race([controlEvent,sleep(2000)]);record.controlConnected=!!control;control?.writeHead(200);control?.end('quit');
 const completed=await Promise.race([exit,sleep(6000).then(()=>null)]);
 if(!completed){for(const p of tree.reverse())try{process.kill(p.pid,'SIGTERM')}catch{};}
 record.quitRequestedAtMs=performance.now()-current.started;record.exit=completed??await Promise.race([exit,sleep(1500).then(()=>({timeout:true}))]);if(record.exit.timeout){for(const p of tree)try{process.kill(p.pid,'SIGKILL')}catch{};await exit;}
 record.output=output;
 runs.push(record);await writeFile(base.output||'samples.json',JSON.stringify({runs},null,2));
 console.log(config.id,record.result,result.elapsedMs?.toFixed(1),`${record.treeRSSKiB} KiB tree`,`${record.candidateRSSKiB} KiB WK candidates`);
 current=null;await sleep(150);
 return record;
}
try {
 if(process.argv.includes('--deep-route')){
  for(const engine of ['electrobun','electron'])await launch(engine,{url:engine==='electron'?'whip-compare://bundle/settings':'views://bundle/settings',output:'deep-route-samples.json'});
 } else if(process.argv.includes('--scheme')){
  for(const engine of ['electrobun','electron','electrobun'])await launch(engine,{url:engine==='electron'?'whip-compare://bundle/index.html':'views://bundle/index.html',memory:true,output:'scheme-samples.json'});
 } else {
  process.env.WHIP_WEB_PERF_FIXTURE='1';
  fixture=await startFixture();await cp(path.join(root,'renderer'),path.join(fixture.directory,'public'),{recursive:true});
  console.log('isolated fixture',fixture.info);
  const base={url:fixture.info.frontend+'/',endpoint:fixture.info.endpoint,runtimeId:fixture.info.runtime_id,route:`/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`};
  await writeFile('fixture-info.json',JSON.stringify(fixture.info,null,2));
  const count=process.argv.includes('--pilot')?1:32;
  for(let i=0;i<count&&!stopping;i++)for(const engine of i%2?['electrobun','electron']:['electron','electrobun'])await launch(engine,{...base,warmup:i<2,memory:process.argv.includes('--pilot')||i>=29});
 }
} finally {
 if(fixture)await fixture.close();
 server.closeAllConnections();await new Promise(r=>server.close(r));
}
