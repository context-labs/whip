import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { test } from 'node:test';
import { promisify } from 'node:util';
import { seedSession } from '../../apps/desktop/scripts/startup.mjs';

test('desktop startup seed uses native provider CAS and canonical worker evidence', { timeout: 120_000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-desktop-seed-'), binary = join(directory, 'whipcode');
  const exec = promisify(execFile);
  const env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', HOME: join(directory, 'user'), WHIPCODE_HOME: join(directory, 'home'), WHIPCODE_NETWORK: '0' };
  await Promise.all(['user', 'home', 'work'].map(name => mkdir(join(directory, name), { mode: 0o700 })));
  let started = false;
  const run = args => exec(binary, args, { env, timeout: 20_000, maxBuffer: 256 << 10 });
  t.after(async () => {
    if (started) {
      await run(['daemon', 'stop']);
      assert.equal(JSON.parse((await run(['daemon', 'status', '--json'])).stdout).state, 'stopped');
    }
    await rm(directory, { recursive: true, force: true });
  });
  await exec('go', ['build', '-o', binary, './cmd/whip'], { timeout: 60_000 });
  started = true;
  await run(['daemon', 'start']);
  const before = JSON.parse((await run(['daemon', 'status', '--json'])).stdout);
  const seeded = await seedSession(directory, binary, env, 'whip');
  assert.equal(seeded.providerRequests, 2); assert.ok(seeded.turnID && seeded.cellID);
  assert.match(seeded.route, /^\/h\/[^/]+\/s\/[^/]+$/);
  const after = JSON.parse((await run(['daemon', 'status', '--json'])).stdout);
  assert.equal(after.process.process_epoch, before.process.process_epoch, 'Native provider setup must not restart the owner');
  await assert.rejects(readFile(join(env.WHIPCODE_HOME, 'config.json')), error => error.code === 'ENOENT');
  await assert.rejects(readFile(join(env.WHIPCODE_HOME, 'models.json')), error => error.code === 'ENOENT');
});
