import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { promisify } from 'node:util';
import { deadline, repository, startFixture } from './native-fixture.mjs';

/** Synthetic canonical store history, followed by the unmodified production
 * runtime. This proves real owner/paging/rendering behavior, not historical
 * provider execution, native effect timing or tool-output externalization. */
export async function startHistoryFixture(options = {}) {
  const fixture = await startFixture({ lifetimeMs: 900_000, ...options });
  try {
    const client = await fixture.connect('history-fixture-setup');
    const created = await fixture.createRoot(client, { title: 'Native history fixture' });
    const executable = join(fixture.directory, 'seed-history'), nonce = randomUUID();
    await promisify(execFile)('go', ['build', '-o', executable, './apps/web/scripts/fixtures/native-history'], {
      cwd: repository, timeout: 120_000, maxBuffer: 128 << 10, env: { ...process.env, GOTOOLCHAIN: 'go1.27.0' },
    });
    await writeFile(join(fixture.directory, 'history-seed-owner'), nonce, { mode: 0o600 });
    const args = ['-directory', dirname(fixture.info.socket), '-root', created.root.id, '-nonce', nonce];
    const run = () => promisify(execFile)(executable, args, { timeout: 125_000, maxBuffer: 128 << 10 });
    // Prove the seed cannot race an execution owner, even in this disposable dir.
    await assert.rejects(run(), /fixture runtime must be stopped before seeding/);
    let history;
    await fixture.crashAndRestart({ beforeRestart: async () => { history = JSON.parse((await run()).stdout); } });
    const current = await fixture.connect('history-fixture-check');
    assert.equal((await current.session(created.root.id).history.snapshot(deadline())).message_count, '10000');
    assert.equal(history.children.length, 100);
    return Object.assign(fixture, { history });
  } catch (error) { await fixture.close(); throw error; }
}
