import {readFile,writeFile} from 'node:fs/promises';
const raw=JSON.parse(await readFile('samples.json','utf8'));
const stat=values=>{const v=values.toSorted((a,b)=>a-b);return {count:v.length,min:v[0],median:v.length%2?v[(v.length-1)/2]:(v[v.length/2-1]+v[v.length/2])/2,p95:v[Math.ceil(v.length*.95)-1],max:v.at(-1)};};
const output={recordedAt:new Date().toISOString(),window:{start:raw.runs[0].recordedAt,end:raw.runs.at(-1).recordedAt},engines:{}};
for(const engine of ['electron','electrobun']){
 const all=raw.runs.filter(r=>r.engine===engine),runs=all.filter(r=>!r.warmup),steady=runs.filter(r=>r.settleMs===1500);
 output.engines[engine]={attempts:runs.length,warmups:all.length-runs.length,failures:runs.filter(r=>r.result!=='usable'||r.exit.code!==0).map(r=>r.id),usableMs:stat(runs.map(r=>r.events.find(e=>e.kind==='usable').elapsedMs)),shellReadyMs:stat(runs.map(r=>r.events.find(e=>e.kind==='shell-ready').elapsedMs)),steadyTreeMiB:stat(steady.map(r=>r.treeRSSKiB/1024)),steadyNewWebKitOutsideTreeMiB:stat(steady.map(r=>r.candidateRSSKiB/1024)),steadyTreePlusCandidateMiB:stat(steady.map(r=>(r.treeRSSKiB+r.candidateRSSKiB)/1024)),steadyDaemonMiB:stat(steady.map(r=>r.daemonRSSKiB/1024)),allPassedFeatures:runs.every(r=>{const e=r.events.find(e=>e.kind==='usable');return e.secure&&e.features.sha256Bytes===32&&e.features.webLock&&e.features.storage.localStorage&&e.features.storage.sessionStorage&&e.features.storage.indexedDB&&e.composer&&e.viewport[0]===1200&&e.viewport[1]===800&&e.fonts.some(f=>f.family==='Inter Variable'&&f.status==='loaded');}),errorEvents:runs.flatMap(r=>r.events.filter(e=>e.kind==='error'))};
}
await writeFile('summary.json',JSON.stringify(output,null,2));console.log(JSON.stringify(output,null,2));
