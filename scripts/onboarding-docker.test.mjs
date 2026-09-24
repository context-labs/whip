import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';
import { checkPort, main } from './onboarding-docker.mjs';

const exec = promisify(execFile);
const image = 'sha256:' + 'a'.repeat(64);

async function fixture(t, mode = '') {
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-docker-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const root = path.join(directory, 'checkout with spaces');
  const bin = path.join(directory, 'bin');
  const log = path.join(directory, 'docker.jsonl');
  await mkdir(root); await mkdir(bin);
  await writeFile(path.join(root, 'package.json'), JSON.stringify({ engines: { node: '>=24 <25' } }));
  await writeFile(path.join(root, 'go.mod'), 'module example.invalid/test\n\ngo 1.27.0\n');
  const git = args => exec('git', args, { cwd: root });
  await git(['init', '--quiet']); await git(['add', '.']);
  await git(['-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', 'fixture']);
  await writeFile(path.join(bin, 'docker'), `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.appendFileSync(process.env.WHIP_DOCKER_TEST_LOG, JSON.stringify(args) + '\\n');
const mode = process.env.WHIP_DOCKER_TEST_MODE;
if (args[0] === 'context' && args[1] === 'show') console.log('fixture');
else if (args[0] === 'context') console.log(JSON.stringify(mode === 'remote' ? 'ssh://example.invalid' : 'unix:///fixture.sock'));
else if (args[0] === 'info') {
  if (mode === 'unavailable') { console.error('Cannot connect to Docker daemon'); process.exit(1); }
  console.log('linux/aarch64');
} else if (args[0] === 'build') {
  if (mode === 'build-failed') process.exit(7);
  fs.writeFileSync(args[args.indexOf('--iidfile') + 1], ${JSON.stringify(image)});
} else if (args[0] === 'create') {
  if (mode === 'create-failed') process.exit(9);
  console.log('fixture-container-id');
} else if (args[0] === 'start') {
  if (mode === 'start-failed') process.exit(11);
  if (mode === 'interrupt') process.kill(process.ppid, 'SIGTERM');
}
`, { mode: 0o755 });
  const env = { ...process.env, PATH: bin + path.delimiter + process.env.PATH,
    DOCKER_CONTEXT: 'fixture', WHIP_DOCKER_TEST_LOG: log, WHIP_DOCKER_TEST_MODE: mode,
    INFERENCE_API_KEY: 'must-not-be-forwarded', WHIP_HOME: '/host/state', TERM: 'xterm-256color', COLORTERM: 'truecolor' };
  return { root, git, env, options: { root, env, interactive: true, ensurePort: () => checkPort(0) },
    calls: async () => (await readFile(log, 'utf8')).trim().split('\n').map(line => JSON.parse(line)) };
}

test('builds working-tree metadata and attaches to the exact new image with only terminal environment', async t => {
  const f = await fixture(t);
  await writeFile(path.join(f.root, 'go.mod'), 'module example.invalid/edited\n\ngo 1.27.0\n');
  await writeFile(path.join(f.root, 'untracked.go'), 'package edited\n');
  await main([], f.options);
  const calls = await f.calls();
  const build = calls.find(args => args[0] === 'build');
  const source = JSON.parse(build.find(arg => arg.startsWith('WHIP_RENDERER_LOCAL_SOURCE=')).split('=').slice(1).join('='));
  assert.equal(source.commit, (await f.git(['rev-parse', 'HEAD'])).stdout.trim());
  assert.equal(source.dirty, true);
  assert.equal(build.at(-1), '.');
  const create = calls.find(args => args[0] === 'create');
  assert.equal(create.at(-1), image);
  assert.equal(create[create.indexOf('--publish') + 1], '127.0.0.1:4000:4000');
  assert(create.includes('--rm')); assert(create.includes('--init')); assert(create.includes('-it'));
  assert.deepEqual(create.filter((arg, index) => create[index - 1] === '--env'), ['TERM=xterm-256color', 'COLORTERM=truecolor']);
  assert(!create.includes('--volume'));
  const name = create[create.indexOf('--name') + 1];
  assert(calls.filter(args => ['start', 'stop', 'rm'].includes(args[0])).every(args => args.at(-1) === name));
  assert(!JSON.stringify(calls).includes('must-not-be-forwarded'));
  assert(!JSON.stringify(calls).includes('/host/state'));
});

test('resolves metadata from a linked worktree whose .git is a pointer file', async t => {
  const f = await fixture(t);
  const linked = path.join(path.dirname(f.root), 'linked worktree');
  await f.git(['worktree', 'add', '--detach', linked, 'HEAD']);
  assert.match(await readFile(path.join(linked, '.git'), 'utf8'), /^gitdir: /);
  await main([], { ...f.options, root: linked });
  const build = (await f.calls()).find(args => args[0] === 'build');
  assert(build.some(arg => arg.startsWith('WHIP_RENDERER_LOCAL_SOURCE=') && arg.includes('"dirty":false')));
});

test('Docker failures stop before launch or clean up only the owned container', async t => {
  for (const mode of ['remote', 'unavailable', 'build-failed', 'create-failed', 'start-failed', 'interrupt']) {
    await t.test(mode, async t => {
      const f = await fixture(t, mode);
      await assert.rejects(main([], f.options));
      const calls = await f.calls();
      if (['remote', 'unavailable', 'build-failed'].includes(mode)) {
        assert(!calls.some(args => ['create', 'start', 'stop', 'rm'].includes(args[0])));
      } else {
        assert(calls.some(args => args[0] === 'stop'));
        assert(calls.some(args => args[0] === 'rm'));
      }
      if (mode === 'create-failed') assert(!calls.some(args => args[0] === 'start'));
    });
  }
});

test('rejects noninteractive launches and unknown arguments; help needs no Docker', async () => {
  await assert.rejects(main([], { interactive: false }), /interactive terminal/);
  await assert.rejects(main(['--reuse']), /Unknown option/);
  await main(['--help'], { interactive: false, env: { PATH: '' } });
});

test('port checks reject an occupied listener without closing it', async t => {
  const server = createServer();
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  await assert.rejects(checkPort(server.address().port), /unavailable/);
  assert.equal(server.listening, true);
  await checkPort(0);
});
