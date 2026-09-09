// Interactive OS-signal acceptance against the real staged main/preload and renderer.
// Toggle macOS Increase Contrast and Reduce Motion on, then restore each to off.
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, copyFile, rm, writeFile } from 'node:fs/promises';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { _electron, expect } from '@playwright/test';
const root = fileURLToPath(new URL('../../../', import.meta.url));
const fixture = await mkdtemp('/tmp/whip-native-settings-');
for (const name of ['home', 'data', 'bin', 'user']) await mkdir(`${fixture}/${name}`, {mode: 0o700});
const binary = `${fixture}/bin/whipcode`;
await copyFile(`${root}/apps/desktop/.stage/native/whipcode`, binary);
const env = { ...process.env, HOME: `${fixture}/user`, WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: `${fixture}/home`, WHIPCODE_NETWORK: '0', WHIP_DESKTOP_USER_DATA: `${fixture}/data`, WHIP_DESKTOP_EXECUTABLE: binary };
let electron;
const observations = [], errors = [];
try {
  console.log('Launching staged desktop fixture');
  electron = await _electron.launch({args: [`${root}/apps/desktop/.stage/app`], env, timeout:30_000});
  console.log('Desktop launched');
  const page = await electron.firstWindow(); page.on('pageerror', error => errors.push(error.message));
  // Playwright defaults to emulated "no-preference"; explicitly observe the OS.
  await page.emulateMedia({reducedMotion:null, contrast:null, colorScheme:null, forcedColors:null});
  page.setDefaultTimeout(15_000);
  await page.getByRole('heading', {name:'What would you like to work on?', exact:true}).waitFor();
  await page.locator('#whip-settings-link').click();
  await page.getByRole('heading', {name:'Appearance', exact:true}).waitFor();
  console.log('Settings loaded');
  await electron.evaluate(({BrowserWindow}) => {const window = BrowserWindow.getAllWindows()[0]; window.setSize(1280,960); window.webContents.setZoomFactor(4);});
  await expect(page.getByRole('button', {name:'Appearance', exact:true})).toBeVisible();
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Native 400% zoom overflow');
  await electron.evaluate(({BrowserWindow}) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1));
  console.log('400% native zoom/reflow passed; waiting for OS contrast/motion on → off.');
  const deadline = Date.now()+120_000;
  while (Date.now()<deadline) {
    const state=await page.evaluate(async () => ({contrast: await window.whipDesktop.getSystemContrast(), renderedContrast:document.documentElement.dataset.contrast, motion:matchMedia('(prefers-reduced-motion: reduce)').matches, transition:getComputedStyle(document.querySelector('button')).transitionDuration}));
    if (JSON.stringify(state)!==JSON.stringify(observations.at(-1))) {observations.push(state);console.log(JSON.stringify(state));}
    const contrastOn=observations.findIndex(x=>x.contrast && x.renderedContrast==='more');
    const motionOn=observations.findIndex(x=>x.motion && x.transition.split(',').every(v=>parseFloat(v)<=0.00001));
    if(contrastOn>=0 && motionOn>=0 && observations.slice(Math.max(contrastOn,motionOn)+1).some(x=>!x.contrast && !x.motion)) break;
    await new Promise(resolve=>setTimeout(resolve,250));
  }
  assert.ok(observations.some(x=>x.contrast && x.renderedContrast==='more'), 'Actual OS contrast did not propagate through native bridge');
  // The shared reset keeps a 0.01ms duration so transition-end listeners still fire.
  assert.ok(observations.some(x=>x.motion && x.transition.split(',').every(v=>parseFloat(v)<=0.00001)), 'Actual OS reduce-motion did not remove visible CSS transition');
  assert.ok(!observations.at(-1)?.contrast && !observations.at(-1)?.motion, 'Restore OS contrast and motion before completing this check');
  assert.deepEqual(errors,[]);
  await writeFile('/tmp/whip-settings-native-signals.json',JSON.stringify({observations,errors,zoom400:true},null,2));
} finally {
  await electron?.close();
  await promisify(execFile)(binary,['daemon','stop'],{env}).catch(()=>{});
  await rm(fixture,{recursive:true,force:true});
}
