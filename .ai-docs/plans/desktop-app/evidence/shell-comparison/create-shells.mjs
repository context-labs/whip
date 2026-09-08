// Run from a fresh disposable directory; never from a shipping app directory.
import {writeFile,copyFile} from 'node:fs/promises';
import {browserProbe} from './common-probe.mjs';
const probe=browserProbe.toString();
await writeFile('electron-preload.cjs',`const {contextBridge}=require('electron');const config=JSON.parse(process.argv.find(x=>x.startsWith('--whip-comparison=')).slice(18));(${probe})(config,b=>contextBridge.exposeInMainWorld('whipDesktop',b));`);
await copyFile(new URL('./electron-main.cjs',import.meta.url),'electron-main.cjs');
await writeFile('comparison-main.ts',`import {BrowserWindow,Utils} from 'electrobun/main';const config=JSON.parse(process.env.WHIP_COMPARISON_CONFIG);const script='('+${JSON.stringify(probe)}+')('+JSON.stringify(config)+');';const w=new BrowserWindow({title:'Whip shell comparison — Electrobun 2.0.1',url:config.url,preload:script,renderer:'native',sandbox:true,frame:{width:1200,height:800}});fetch(config.collector+'/control/'+config.id).finally(()=>Utils.quit());setTimeout(()=>Utils.quit(),16000);`);
await copyFile(new URL('./electrobun.config.ts',import.meta.url),'electrobun.config.ts');
await copyFile(new URL('./hutch.config.ts',import.meta.url),'hutch.config.ts');
