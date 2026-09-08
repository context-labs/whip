// Exercise the release binary in an isolated home and compare its served web
// bytes with the one renderer artifact shared by every release consumer.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { readRendererManifest, repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
const binary = path.resolve(process.argv[2]);
const directory = await mkdtemp('/tmp/whip-linux-release-');
const env = { HOME: directory, WHIPCODE_HOME: path.join(directory, 'state'), WHIPCODE_NETWORK: '1', WHIPCODE_LISTEN: '127.0.0.1:0', PATH: '/usr/bin:/bin' };
const run = async (...args) => (await exec(binary, args, { env, cwd: directory, timeout: 20_000 })).stdout;
try {
  const info = JSON.parse(await run('_desktop-runtime-info'));
  assert.equal(info.distribution, 'whipcode'); assert.equal(info.updateOwner, 'standalone');
  assert.equal(info.buildId, process.env.RELEASE_VERSION);
  const renderer = await readRendererManifest(path.join(repositoryRoot, 'apps/web/renderer-manifest.json'));
  await run('daemon', 'start');
  const base = (await run('web', '--no-open')).trim();
  for (const name of ['index.html', ...Object.keys(renderer.files).filter(name => name.endsWith('.js'))]) {
    const response = await fetch(new URL(name, base), { signal: AbortSignal.timeout(10_000) });
    assert.equal(response.status, 200);
    const bytes = Buffer.from(await response.arrayBuffer());
    assert.equal(createHash('sha256').update(bytes).digest('hex'), renderer.files[name].sha256, `Served renderer differs: ${name}`);
  }
  const status = JSON.parse(await run('daemon', 'status', '--json'));
  assert.equal(status.daemon_build, info.buildId);
  await writeFile(path.join(path.dirname(binary), 'linux-runtime.json'), JSON.stringify({ ...info,
    source: renderer.source, rendererDigest: renderer.digest, smoke: { embeddedRenderer: true, daemonReady: true } }, null, 2) + '\n');
  console.log(`Verified Linux ${info.buildId} and shared renderer ${renderer.digest}`);
} finally {
  // Preserve the fixture if normal shutdown fails; never delete a live owner.
  await run('daemon', 'stop');
  assert.equal(JSON.parse(await run('daemon', 'status', '--json')).state, 'stopped');
  await rm(directory, { recursive: true, force: true });
}
