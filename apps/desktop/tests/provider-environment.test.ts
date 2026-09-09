import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import { providerEnvironmentNames, runtimeEnvironment } from '../src/runtime';

async function home(t: TestContext) {
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-provider-env-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}
const signal = () => new AbortController().signal;

for (const shell of ['/bin/zsh', '/bin/bash']) test(`${shell} recovers only supported keys from interactive login startup`, async t => {
  const directory = await home(t);
  const filename = shell.endsWith('zsh') ? '.zshrc' : '.bash_profile';
  await writeFile(path.join(directory, filename), "printf 'startup noise\\n'\nexport INFERENCE_API_KEY='fixture shell key $not-expanded'\nexport OPENROUTER_API_KEY='fixture-openrouter'\nexport UNRELATED_SECRET='not-imported'\n");
  const inherited = { HOME: directory, ZDOTDIR: directory, SHELL: shell, PATH: '/usr/bin:/bin' };
  const env = await runtimeEnvironment(signal(), true, inherited);
  assert.equal(env.INFERENCE_API_KEY, 'fixture shell key $not-expanded');
  assert.equal(env.OPENROUTER_API_KEY, 'fixture-openrouter');
  assert.equal(env.UNRELATED_SECRET, undefined);
  assert.equal('INFERENCE_API_KEY' in inherited, false);
  const explicit = await runtimeEnvironment(signal(), true, { ...inherited, INFERENCE_API_KEY: 'inherited', OPENROUTER_API_KEY: '' });
  assert.equal(explicit.INFERENCE_API_KEY, 'inherited');
  assert.equal(explicit.OPENROUTER_API_KEY, '');
  const remote = await runtimeEnvironment(signal(), false, inherited);
  assert.equal(remote.INFERENCE_API_KEY, undefined);
  assert.equal(remote.OPENROUTER_API_KEY, undefined);
});

test('shell recovery is bounded, abortable, and falls back without exposing shell output', async t => {
  const directory = await home(t);
  const shell = path.join(directory, 'shell');
  const env = { HOME: directory, SHELL: shell, PATH: '/usr/bin:/bin', INFERENCE_API_KEY: 'inherited' };
  await writeFile(shell, '#!/bin/sh\nprintf "private-shell-output" >&2\nexit 1\n', { mode: 0o700 });
  assert.equal((await runtimeEnvironment(signal(), true, env)).INFERENCE_API_KEY, 'inherited');
  await writeFile(shell, '#!/bin/sh\nexec /bin/sleep 10\n', { mode: 0o700 });
  const started = Date.now();
  assert.equal((await runtimeEnvironment(signal(), true, env)).INFERENCE_API_KEY, 'inherited');
  assert.ok(Date.now() - started < 6000, 'shell probe exceeded its timeout');
  const controller = new AbortController();
  const pending = runtimeEnvironment(controller.signal, true, env);
  controller.abort();
  await assert.rejects(pending, { name: 'AbortError' });
  await writeFile(shell, '#!/bin/sh\n/usr/bin/yes private-output\n', { mode: 0o700 });
  assert.equal((await runtimeEnvironment(signal(), true, env)).OPENROUTER_API_KEY, undefined);
});

test('desktop provider allowlist matches the enabled Go presets', async () => {
  const sources = await Promise.all(['providers.go', 'inferencenet.go', 'openrouter.go'].map(file => readFile(path.resolve('internal/config', file), 'utf8')));
  const references = [...sources[0]!.matchAll(/APIKeyEnv:\s+(\w+)/g)].map(match => match[1]!);
  const names = references.map(name => sources.join('\n').match(new RegExp(`${name}\\s*=\\s*"([^"]+)"`))?.[1]);
  assert.deepEqual(names.sort(), [...providerEnvironmentNames].sort());
});
