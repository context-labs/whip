import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { promisify } from 'node:util';
import { build } from 'esbuild';
import { deadline, repository, startFixture } from '../../../apps/web/scripts/native-fixture.mjs';

const exec = promisify(execFile);
const directory = await mkdtemp('/tmp/whip-sdk-consumer-');
let fixture;
try {
  const archives = {};
  for (const name of ['protocol', 'sdk']) {
    const { stdout } = await exec('npm', ['pack', '--json', '--pack-destination', directory], { cwd: join(repository, 'packages', name) });
    const [packed] = JSON.parse(stdout);
    archives[`@whip/${name}`] = 'file:' + join(directory, packed.filename);
    assert(!packed.files.some(file => /(^|\/)(node_modules|src|test|scripts)\//.test(file.path)), 'Package contains development source');
  }
  const consumer = join(directory, 'consumer');
  await mkdir(consumer);
  // Pack the already installed lockfile dependencies too. A fresh npm ci cache
  // contains package tarballs, but need not contain registry metadata for an
  // offline resolver; this consumer must not depend on a warmed global cache.
  for (const name of ['react', '@types/react', '@types/node', 'csstype', 'undici-types']) {
    const { stdout } = await exec('npm', ['pack', '--json', '--ignore-scripts', '--pack-destination', directory],
      { cwd: join(repository, 'node_modules', name) });
    const [packed] = JSON.parse(stdout);
    archives[name] = 'file:' + join(directory, packed.filename);
  }
  await writeFile(join(consumer, 'package.json'), JSON.stringify({ private: true, type: 'module',
    dependencies: archives }));
  await exec('npm', ['install', '--offline', '--ignore-scripts', '--package-lock=false', '--no-audit', '--no-fund',
    '--cache', join(directory, 'empty-npm-cache')], { cwd: consumer, timeout: 60_000 });
  await writeFile(join(consumer, 'imports.mjs'), `
import assert from 'node:assert/strict';
import * as protocol from '@whip/protocol';
import { Client } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import * as browser from '@whip/sdk/browser';
import * as state from '@whip/sdk/state';
import * as react from '@whip/sdk/react';
import { defineAgent } from '@whip/sdk/agents';
assert.equal(typeof Client.connect, 'function');
assert.equal(typeof unixSocket, 'function');
assert.equal(typeof defineAgent, 'function');
for (const value of [protocol, browser, state, react]) assert(Object.keys(value).length);
export { Client, unixSocket };
`);
  await exec(process.execPath, ['--disallow-code-generation-from-strings', 'imports.mjs'], { cwd: consumer });
  await writeFile(join(consumer, 'smoke.ts'), `
import { Client } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
async function check() {
  const client = await Client.connect(unixSocket('/tmp/unused.sock'), { clientID: 'consumer' });
  const identity: string = client.runtimeID;
  await client.session('root').submit([{ type: 'text', text: identity }], 'request');
  // @ts-expect-error Queries cannot enter the durable command journal.
  client.command('sessions.get', { session_id: 'root' });
  // @ts-expect-error Parameters are generated from the Go contract.
  await client.call('sessions.get', { session_id: 42 });
}
void check;
`);
  await exec(process.execPath, [join(repository, 'node_modules/typescript/bin/tsc'), '--noEmit', '--strict', '--target', 'es2022', '--module', 'nodenext', '--moduleResolution', 'nodenext', '--skipLibCheck', 'false', 'smoke.ts'], { cwd: consumer, timeout: 30_000 });
  // Resolve the installed archives, not workspace source, as a browser consumer.
  const bundle = await build({ stdin: { contents: `export * from '@whip/sdk'; export * from '@whip/sdk/browser'; export * from '@whip/sdk/state';`, resolveDir: consumer },
    bundle: true, platform: 'browser', format: 'esm', target: 'es2022', write: false, metafile: true });
  assert(Object.keys(bundle.metafile.inputs).every(path => !path.includes('/packages/sdk/src/') && !path.includes('/legacy-')));
  assert(bundle.outputFiles[0].contents.length > 0);
  fixture = await startFixture();
  const { Client, unixSocket } = await import(pathToFileURL(join(consumer, 'imports.mjs')).href);
  const client = await Client.connect(unixSocket(fixture.info.socket), {
    clientID: 'packed-consumer', expectedRuntimeID: fixture.info.runtime_id, ...deadline() });
  const { root } = await fixture.createRoot(client);
  const session = client.session(root.id);
  await session.submit([{ type: 'text', text: 'packed native SDK' }], 'packed-input', deadline());
  await client.wait('packed-input', deadline());
  const page = await session.history.page({ limit: 10, direction: 'forward' }, deadline());
  assert.equal(page.messages.filter(message => message.role === 'user').length, 1);
  assert(page.messages.some(message => message.role === 'assistant'));
  const installed = JSON.parse(await readFile(join(consumer, 'node_modules/@whip/sdk/package.json'), 'utf8'));
  assert.equal(installed.version, '4.0.0'); assert.equal(installed.private, true);
  console.log('Packed v4 protocol and SDK imports, browser bundle, consumer types and actual native input/history passed.');
} finally {
  await fixture?.close();
  await rm(directory, { recursive: true, force: true });
}
