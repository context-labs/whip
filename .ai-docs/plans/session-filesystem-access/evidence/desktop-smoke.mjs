// Reuse the maintained isolated Electron fixture, substituting a sibling-file
// Starlark operation and checking the mode through the UI after daemon restart.
import assert from 'node:assert/strict';
import { readFile, writeFile, unlink } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../../../../', import.meta.url));
const sourcePath = path.join(root, 'apps/desktop/scripts/onboarding-smoke.mjs');
const temporary = path.join(path.dirname(sourcePath), `.filesystem-access-${process.pid}.mjs`);
let source = await readFile(sourcePath, 'utf8');
function replace(before, after) {
  assert(source.includes(before), `Fixture source changed: ${before}`);
  source = source.replace(before, after);
}
replace("const work = path.join(fixture, 'project');", `const work = path.join(fixture, 'project');
  const sibling = path.join(fixture, 'sibling');
  await mkdir(sibling, { recursive: true });
  const outsideFile = path.join(sibling, 'result.txt');`);
replace('code: `print(${JSON.stringify(marker)})\\n{"answer": 6 * 7}`',
  'code: `files.list(path=${JSON.stringify(sibling)})\\nfiles.write(path=${JSON.stringify(outsideFile)}, content="42")\\nprint(${JSON.stringify(marker)})\\n{"answer": 6 * 7}`');
replace('assert.equal(snapshot.meta.cwd, work);', `assert.equal(await readFile(outsideFile, 'utf8'), '42');
    assert.equal(snapshot.meta.cwd, work);`);
replace('return { provider:', `const resumedRoot = client.session(rootId);
    const readOutside = () => resumedRoot.command('tool.call', { tool: 'read', arguments: { path: outsideFile } }).result({ signal: AbortSignal.timeout(5000) });
    assert.equal((await readOutside()).status, 'succeeded');
    const resumedWrite = await resumedRoot.command('tool.call', { tool: 'write', arguments: { path: outsideFile, content: 'after-restart' } }).result({ signal: AbortSignal.timeout(5000) });
    assert.equal(resumedWrite.status, 'succeeded');
    assert.equal(await readFile(outsideFile, 'utf8'), 'after-restart');
    await resumed.getByRole('button', { name: 'Permission approval mode', exact: true }).click();
    await resumed.getByRole('option', { name: /Ask for approval/ }).click();
    await expect.poll(async () => (await resumedRoot.snapshot()).permission_mode).toBe('prompt');
    const deniedRead = await readOutside();
    assert.equal(deniedRead.status, 'failed');
    assert.match(deniedRead.failure.message, /outside this agent's allowed filesystem scope/);
    await capture(resumed, 'desktop-ask-denies-sibling');
    return { siblingStarlarkWriteFromUI: true, restartAllowsSiblingReadAndWrite: true, askSelectedInUIDeniesSiblingRead: true, scopeDenial: deniedRead.failure.message, provider:`);
replace("purpose: 'Isolated staged Electron first-run onboarding; not signed/Finder acceptance or public-provider compatibility validation'",
  "purpose: 'Isolated shared renderer in Electron: Full Access sibling-file Starlark, daemon restart, and Ask downgrade'");

await writeFile(temporary, source);
try {
  const child = spawn(process.execPath, [temporary], { cwd: root, stdio: 'inherit', env: {
    ...process.env,
    WHIP_ONBOARDING_SMOKE_OUTPUT: fileURLToPath(new URL('./desktop.json', import.meta.url)),
  } });
  const code = await new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => signal ? reject(new Error(signal)) : resolve(code));
  });
  assert.equal(code, 0, 'Desktop fixture failed');
} finally {
  await unlink(temporary);
}
