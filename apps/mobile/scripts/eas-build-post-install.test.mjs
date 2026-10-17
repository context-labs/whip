import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

test('EAS initializes from source with verified Go and stops on failures', async () => {
  const root = await realpath(await mkdtemp(path.join(tmpdir(), 'whip-eas-init-test-')));
  try {
    const scripts = path.join(root, 'apps/mobile/scripts');
    const bin = path.join(root, 'bin');
    const source = path.join(root, 'archive/go/bin');
    await Promise.all([mkdir(scripts, { recursive: true }), mkdir(bin), mkdir(source, { recursive: true })]);
    await copyFile(new URL('./eas-build-post-install.mjs', import.meta.url), path.join(scripts, 'eas-build-post-install.mjs'));
    await writeFile(path.join(root, 'go.mod'), 'module fixture\n\ngo 1.2.3\n');
    await writeFile(path.join(bin, 'go'), '#!/bin/sh\nprintf "%s\\n" "$FAKE_GO_VERSION"\n', { mode: 0o755 });
    await writeFile(path.join(source, 'go'), '#!/bin/sh\nprintf "go1.2.3\\n"\n', { mode: 0o755 });
    await writeFile(path.join(bin, 'npm'), `#!/bin/sh
printf '%s|%s|%s\n' "$PWD" "$*" "$(go env GOVERSION)" >> "$FAKE_NPM_LOG"
if [ "$FAIL_GENERATE" = 1 ] && [ "$*" = 'run generate' ]; then exit 7; fi
`, { mode: 0o755 });
    const archive = path.join(root, 'go.tar.gz');
    execFileSync('tar', ['-czf', archive, '-C', path.join(root, 'archive'), 'go']);
    const preload = path.join(root, 'download.mjs');
    await writeFile(preload, `
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
const bytes = await readFile(process.env.FAKE_GO_ARCHIVE);
const url = 'https://dl.google.com/go/go1.2.3.' + process.platform + '-' + ({ x64: 'amd64', arm64: 'arm64' }[process.arch]) + '.tar.gz';
globalThis.fetch = async actual => {
  assert.notEqual(process.env.FORBID_DOWNLOAD, '1', 'Existing exact Go must not download');
  assert([url, url + '.sha256'].includes(actual));
  if (process.env.FAIL_DOWNLOAD === '1') return new Response('', { status: 503 });
  const checksum = process.env.BAD_CHECKSUM === '1' ? '0'.repeat(64) : createHash('sha256').update(bytes).digest('hex');
  return new Response(actual.endsWith('.sha256') ? checksum : bytes);
};
`);
    for (const scenario of [
      { name: 'existing', env: { FAKE_GO_VERSION: 'go1.2.3', FORBID_DOWNLOAD: '1' }, commands: ['run generate', 'run build'] },
      { name: 'download', commands: ['run generate', 'run build'] },
      { name: 'checksum', env: { BAD_CHECKSUM: '1' }, error: /Go archive checksum/, commands: [] },
      { name: 'network', env: { FAIL_DOWNLOAD: '1' }, error: /Go download failed/, commands: [] },
      { name: 'generation', env: { FAIL_GENERATE: '1' }, error: /npm run generate/, commands: ['run generate'] },
    ]) {
      const log = path.join(root, `${scenario.name}.log`);
      const result = spawnSync(process.execPath, ['--import', preload, path.join(scripts, 'eas-build-post-install.mjs')], {
        cwd: path.join(root, 'apps/mobile'), encoding: 'utf8',
        env: { ...process.env, PATH: `${bin}:/usr/bin:/bin`, FAKE_GO_VERSION: 'go0.0.0',
          FAKE_GO_ARCHIVE: archive, FAKE_NPM_LOG: log, ...scenario.env },
      });
      const details = `${scenario.name}: ${result.stdout}\n${result.stderr}`;
      if (scenario.error) {
        assert.notEqual(result.status, 0, details);
        assert.match(result.stderr, scenario.error, details);
      } else assert.equal(result.status, 0, details);
      const lines = (await readFile(log, 'utf8').catch(error => {
        if (error.code === 'ENOENT') return '';
        throw error;
      })).trim().split('\n').filter(Boolean);
      assert.deepEqual(lines, scenario.commands.map(command => `${root}|${command}|go1.2.3`), details);
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
