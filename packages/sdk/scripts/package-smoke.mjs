import { execFile } from 'node:child_process';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { repository, run } from './fixture.mjs';

const execute = promisify(execFile);
const directory = await mkdtemp(join(tmpdir(), 'whip-sdk-consumer-'));
try {
  const archives = {};
  for (const name of ['protocol', 'sdk']) {
    const { stdout } = await execute('npm', ['pack', '--json', '--pack-destination', directory], { cwd: join(repository, 'packages', name) });
    const [packed] = JSON.parse(stdout);
    archives[`@whip/${name}`] = 'file:' + join(directory, packed.filename);
    if (packed.files.some(file => /(^|\/)(node_modules|src|test|build)\//.test(file.path))) throw new Error(`Unexpected development files in @whip/${name}`);
  }
  const consumer = join(directory, 'consumer');
  await mkdir(consumer);
  await writeFile(join(consumer, 'package.json'), JSON.stringify({ private: true, type: 'module', dependencies: archives }));
  await run('npm', ['install', '--ignore-scripts', '--package-lock=false', '--no-audit', '--no-fund'], { cwd: consumer });
  await writeFile(join(consumer, 'smoke.mjs'), `
import assert from 'node:assert/strict';
import * as protocol from '@whip/protocol';
import { createWhipClient } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import * as state from '@whip/sdk/state';
assert.equal(typeof createWhipClient, 'function');
assert.equal(typeof unixSocket, 'function');
assert.ok(Object.keys(protocol).length > 0);
assert.ok(Object.keys(state).length > 0);
console.log('Packed protocol, browser core, Node, and state entry points import outside the repository.');
`);
  await run(process.execPath, ['smoke.mjs'], { cwd: consumer });
  await writeFile(join(consumer, 'smoke.ts'), `
import { createWhipClient } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
const client = createWhipClient({ endpoint: unixSocket('/tmp/whip.sock'), clientId: 'consumer', clientKind: 'automation' });
async function check() {
  const ping = await client.call('daemon.ping', {});
  const generation: string = ping.generation;
  client.submit('submit', { text: generation }, { rootId: 'root' });
  // @ts-expect-error Queries must not enter the durable command journal.
  client.submit('workspace.inspect', {}, { rootId: 'root' });
  // @ts-expect-error Parameters are generated from the Go contract.
  client.call('command.status', { command_id: 42 });
}
void check;
`);
  await run(process.execPath, [join(repository, 'node_modules/typescript/bin/tsc'), '--noEmit', '--strict', '--target', 'es2022', '--module', 'nodenext', '--moduleResolution', 'nodenext', '--skipLibCheck', 'false', 'smoke.ts'], { cwd: consumer });
  const installed = JSON.parse(await readFile(join(consumer, 'node_modules/@whip/sdk/package.json'), 'utf8'));
  if (!installed.private) throw new Error('SDK unexpectedly became public');
} finally {
  await rm(directory, { recursive: true, force: true });
}
