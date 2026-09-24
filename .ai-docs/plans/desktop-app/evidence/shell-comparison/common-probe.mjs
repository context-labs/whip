export function browserProbe(config, expose) {
  const send = event => fetch(config.collector + '/event/' + config.id, { method: 'POST', mode: 'no-cors', headers: { 'Content-Type': 'text/plain' }, body: JSON.stringify(event) }).catch(() => {});
  addEventListener('error', e => send({kind:'error',message:String(e.message)}));
  addEventListener('unhandledrejection', e => send({kind:'error',message:String(e.reason)}));
  const previous = localStorage.getItem('whip-comparison-persistence');
  localStorage.setItem('whip-comparison-persistence', config.id);
  if (config.endpoint) {
    const profile = {id:'url:'+config.endpoint,label:'Isolated comparison fixture',target:{kind:'url',endpoint:config.endpoint},runtimeId:config.runtimeId};
    localStorage.setItem('whip.hosts.v2',JSON.stringify([profile]));
    localStorage.setItem('whip.selectedHost.v2',JSON.stringify(profile.id));
  } else {
    localStorage.setItem('whip.hosts.v2','[]');
    localStorage.setItem('whip.selectedHost.v2',JSON.stringify('disconnected-probe'));
  }
  if (location.pathname === '/' || location.pathname === '/index.html') history.replaceState({},'',config.route || '/');
  const unavailable = () => Promise.reject(new Error('Native capability excluded from comparison'));
  const bridge = {version:1,appVersion:'comparison',connectionKinds:['url'],onEvent:()=>()=>{},setNotificationsEnabled:()=>{},ready:()=>send({kind:'shell-ready',at:performance.now()}),prepareConnection:unavailable,releaseConnection:()=>{},copy:unavailable,pickDirectory:unavailable,openExternal:unavailable,checkForUpdates:unavailable,installUpdate:unavailable,notify:async()=>{}};
  if(expose) expose(bridge); else globalThis.whipDesktop = bridge;
  const storageTest = async () => {
    sessionStorage.setItem('whip-comparison-session','ok');
    const db = await new Promise((resolve,reject)=>{const req=indexedDB.open('whip-comparison',1);req.onupgradeneeded=()=>req.result.createObjectStore('probe');req.onsuccess=()=>resolve(req.result);req.onerror=()=>reject(req.error);});
    await new Promise((resolve,reject)=>{const tx=db.transaction('probe','readwrite');tx.objectStore('probe').put('ok','key');tx.oncomplete=resolve;tx.onerror=()=>reject(tx.error);});
    db.close();
    return {localStorage:localStorage.getItem('whip-comparison-persistence')===config.id,sessionStorage:sessionStorage.getItem('whip-comparison-session')==='ok',indexedDB:true,previous};
  };
  const inspect = async () => {
    await document.fonts.ready;
    await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
    let features;
    try { features={uuid:crypto.randomUUID(),sha256Bytes:(await crypto.subtle.digest('SHA-256',new TextEncoder().encode('Whip'))).byteLength,webLock:await navigator.locks.request('whip-comparison',{ifAvailable:true},lock=>!!lock),storage:await storageTest()}; } catch(e) {features={error:String(e)};}
    send({kind:'usable',at:performance.now(),url:location.href,secure:isSecureContext,viewport:[innerWidth,innerHeight],features,body:document.body.innerText.slice(0,1500),bodyColor:getComputedStyle(document.body).color,fontFamily:getComputedStyle(document.body).fontFamily,styles:document.styleSheets.length,fonts:[...document.fonts].map(f=>({family:f.family,status:f.status})),messages:document.querySelectorAll('[data-message-id]').length,composer:!!document.querySelector('[data-whip-composer]'),svg:document.querySelectorAll('svg').length});
  };
  const deadline=performance.now()+12000;
  const check=()=>{
    const ready=config.endpoint ? !!document.querySelector('[data-whip-composer]:not([disabled])') && document.body.innerText.includes('Root message 10000') : document.body?.innerText.includes('What would you like to work on?');
    if(ready) void inspect();
    else if(performance.now()<deadline) setTimeout(check,16);
    else send({kind:'failed',url:location.href,body:document.body?.innerText.slice(0,2500)});
  };
  check();
}
