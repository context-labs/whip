// Explicit manual acceptance only. The bundle, home, runtime and target image
// belong to the calling disposable fixture; no installed application is used.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { cp, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { expect } from '@playwright/test';
import { repository } from './native-fixture.mjs';

export async function dropFixtureExecutable(directory) {
  const exec = promisify(execFile), bundle = join(directory, 'Whip Drop Fixture.app');
  await cp(join(repository, 'node_modules/electron/dist/Electron.app'), bundle, { recursive: true, verbatimSymlinks: true });
  await exec('/usr/libexec/PlistBuddy', ['-c', 'Set :CFBundleIdentifier com.contextlabs.whip.dropfixture', join(bundle, 'Contents/Info.plist')], { timeout: 5000 });
  await exec('/usr/bin/codesign', ['--force', '--deep', '--sign', '-', bundle], { timeout: 30000 });
  console.log(`Owned manual drop fixture: ${bundle}`);
  return join(bundle, 'Contents/MacOS/Electron');
}

export async function checkManualFinderDrop(page, directory) {
  const source = await page.evaluate(() => {
    document.title = 'Whip — isolated file-drop check';
    window.nativeFileDrops = []; window.nativeFileDropOverflow = false;
    for (const type of ['dragenter', 'dragover', 'dragleave', 'drop']) document.addEventListener(type, event => {
      if (window.nativeFileDrops.length === 256) { window.nativeFileDropOverflow = true; return; }
      window.nativeFileDrops.push({ type, trusted: event.isTrusted, files: event.dataTransfer?.files.length ?? 0 });
    }, true);
    const canvas = document.createElement('canvas'); canvas.width = 360; canvas.height = 240;
    const context = canvas.getContext('2d'); context.fillStyle = '#456347'; context.fillRect(0, 0, 360, 240);
    context.fillStyle = '#fff'; context.font = '22px sans-serif'; context.fillText('Native file-drop fixture', 24, 120);
    return canvas.toDataURL('image/png').split(',')[1];
  });
  const path = join(directory, 'native-drop.png');
  await writeFile(path, Buffer.from(source, 'base64'));
  console.log(`Within four minutes, drag ${path} from Finder into this isolated fixture's transcript, then press Enter here. No drag is automated.`);
  await new Promise((resolve, reject) => {
    const finish = error => {
      clearTimeout(timer); process.stdin.off('data', accepted); process.stdin.pause();
      process.off('SIGINT', interrupted); process.off('SIGTERM', interrupted);
      if (error) reject(error); else resolve();
    };
    const accepted = () => finish();
    const interrupted = () => finish(new Error('Manual Finder drop was interrupted'));
    const timer = setTimeout(() => finish(new Error('Manual Finder drop was not confirmed within four minutes')), 240000);
    process.once('SIGINT', interrupted); process.once('SIGTERM', interrupted);
    process.stdin.once('data', accepted); process.stdin.resume();
  });
  await expect(page.getByRole('button', { name: 'Preview native-drop.png', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send message', exact: true })).toBeEnabled();
  const evidence = await page.evaluate(() => ({ events: window.nativeFileDrops, overflow: window.nativeFileDropOverflow }));
  assert.equal(evidence.overflow, false);
  assert(evidence.events.some(event => event.type === 'drop' && event.trusted && event.files === 1), 'A real trusted Finder drop is required');
  await page.screenshot({ path: join(directory, 'electron-native-finder-drop.png') });
  return evidence.events;
}
