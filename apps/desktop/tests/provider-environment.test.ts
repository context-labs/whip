import assert from 'node:assert/strict';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import { runtimeEnvironment } from '../src/runtime';
import { providerEnvironmentNames } from '../src/provider-environment';

async function home(t: TestContext) {
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-provider-env-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}
const signal = () => new AbortController().signal;

for (const shell of ['/bin/zsh', '/bin/bash']) test(`${shell} recovers only supported keys from interactive login startup`, async t => {
  const directory = await home(t);
  const filename = shell.endsWith('zsh') ? '.zshrc' : '.bash_profile';
  const keys = providerEnvironmentNames.map(name => `export ${name}='fixture-${name}'`).join('\n');
  await writeFile(path.join(directory, filename), `printf 'startup noise\\n'\n${keys}\nexport XDG_DATA_HOME='${directory}/custom-data'\nexport XDG_CONFIG_HOME='${directory}/custom-config'\nexport INFERENCE_API_KEY='fixture shell key $not-expanded'\nexport OPENAI_BASE_URL='https://custom.example/v1'\nexport OPENAI_API_BASE='https://another.example/v1'\nexport UNRELATED_SECRET='not-imported'\n`);
  const inherited = { HOME: directory, ZDOTDIR: directory, SHELL: shell, PATH: '/usr/bin:/bin' };
  const env = await runtimeEnvironment(signal(), true, inherited);
  assert.equal(env.INFERENCE_API_KEY, 'fixture shell key $not-expanded');
  for (const name of providerEnvironmentNames.filter(name => name !== 'INFERENCE_API_KEY')) assert.equal(env[name], `fixture-${name}`);
  assert.equal(env.OPENAI_BASE_URL, 'https://custom.example/v1');
  assert.equal(env.OPENAI_API_BASE, 'https://another.example/v1');
  assert.equal(env.XDG_DATA_HOME, undefined);
  assert.equal(env.XDG_CONFIG_HOME, undefined);
  assert.equal(env.UNRELATED_SECRET, undefined);
  assert.equal('INFERENCE_API_KEY' in inherited, false);
  const explicit = await runtimeEnvironment(signal(), true, { ...inherited, INFERENCE_API_KEY: 'inherited', OPENROUTER_API_KEY: '', OPENAI_BASE_URL: 'https://inherited.example/v1', XDG_DATA_HOME: '/inherited', XDG_CONFIG_HOME: '' });
  assert.equal(explicit.INFERENCE_API_KEY, 'inherited');
  assert.equal(explicit.OPENROUTER_API_KEY, '');
  assert.equal(explicit.OPENAI_BASE_URL, 'https://inherited.example/v1');
  assert.equal(explicit.XDG_DATA_HOME, '/inherited');
  assert.equal(explicit.XDG_CONFIG_HOME, '');
  const remote = await runtimeEnvironment(signal(), false, inherited);
  for (const name of [...providerEnvironmentNames, 'OPENAI_BASE_URL', 'OPENAI_API_BASE', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME']) assert.equal(remote[name], undefined);
});

test('invalid recovered OpenAI endpoint overrides cannot expose their key as a canonical preset key', async t => {
  const directory = await home(t);
  const shell = path.join(directory, 'shell');
  for (const value of ['https://custom.example/\ninvalid', 'x'.repeat(16385)]) {
    const fields = ['OPENAI_API_KEY', 'fixture-custom-key', 'OPENAI_BASE_URL', value, 'GROQ_API_KEY', 'fixture-groq-key'];
    const output = '\x00WHIP_ENV\x00' + fields.join('\x00') + '\x00';
    await writeFile(shell, `#!/bin/sh\nprintf '%s' '${Buffer.from(output).toString('base64')}' | /usr/bin/base64 --decode\n`, { mode: 0o700 });
    const env = await runtimeEnvironment(signal(), true, { HOME: directory, SHELL: shell, PATH: '/usr/bin:/bin', OPENAI_API_KEY: 'inherited-custom-key' });
    assert.equal(env.OPENAI_API_KEY, undefined);
    assert.equal(env.OPENAI_BASE_URL, undefined);
    assert.equal(env.GROQ_API_KEY, 'fixture-groq-key');
  }
});

test('shell recovery is bounded, abortable, and falls back without exposing shell output', async t => {
  const directory = await home(t);
  const shell = path.join(directory, 'shell');
  const env = { HOME: directory, SHELL: shell, PATH: '/usr/bin:/bin', INFERENCE_API_KEY: 'inherited' };
  await writeFile(shell, '#!/bin/sh\nprintf "private-shell-output" >&2\nexit 1\n', { mode: 0o700 });
  const failed = await runtimeEnvironment(signal(), true, env);
  assert.equal(failed.INFERENCE_API_KEY, 'inherited');
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

test('generated provider names are unique shell identifiers', () => {
  assert.ok(providerEnvironmentNames.length > 0);
  assert.equal(new Set(providerEnvironmentNames).size, providerEnvironmentNames.length);
  for (const name of providerEnvironmentNames) assert.match(name, /^[A-Z_][A-Z0-9_]*$/);
  assert.ok(providerEnvironmentNames.includes('OPENAI_API_KEY'));
  assert.ok(providerEnvironmentNames.includes('DEEPINFRA_TOKEN'));
});
