import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { attachDevelopment, developmentOptions } from '../src/development';

const desktop = '/repo/apps/desktop';

test('attach uses the main daemon and installed executable without adopting its managed settings', t => {
  const home = mkdtempSync(path.join(tmpdir(), 'whip-dev-target-'));
  t.after(() => rmSync(home, { recursive: true, force: true }));
  const settings = path.join(home, 'Library/Application Support/Whip/native-local-runtime.json');
  mkdirSync(path.dirname(settings), { recursive: true });
  const saved = JSON.stringify({ executable: '/custom/bin/whipcode', managed: { sha256: 'a'.repeat(64), channel: 'stable' } });
  writeFileSync(settings, saved);
  const managed = developmentOptions([], desktop, {});
  const attach = developmentOptions(['--attach'], desktop, { HOME: home });
  assert.equal(attach.env.WHIPCODE_HOME, path.join(home, '.whipcode'));
  assert.equal(attach.env.WHIP_DESKTOP_EXECUTABLE, '/custom/bin/whipcode');
  assert.notEqual(attach.env.WHIPCODE_HOME, managed.env.WHIPCODE_HOME);
  assert.notEqual(attach.env.WHIP_DESKTOP_USER_DATA, managed.env.WHIP_DESKTOP_USER_DATA);
  assert.equal(readFileSync(settings, 'utf8'), saved);
  assert.equal(attach.env.WHIP_DESKTOP_FIXTURE, undefined);
  assert.equal(attach.env.WHIP_DESKTOP_ATTACH, '1');
  assert.equal(managed.env.WHIP_DESKTOP_FIXTURE, '1');
  assert.equal(managed.env.WHIP_DESKTOP_ATTACH, undefined);
  assert.equal(attachDevelopment(false, attach.env), true);
  assert.throws(() => attachDevelopment(true, attach.env), /packaged/);
  assert.throws(() => attachDevelopment(false, { WHIP_DESKTOP_ATTACH: '1' }), /explicit/);
});

test('missing or invalid installed settings never fall back to the isolated daemon', t => {
  const home = mkdtempSync(path.join(tmpdir(), 'whip-dev-target-'));
  t.after(() => rmSync(home, { recursive: true, force: true }));
  assert.throws(() => developmentOptions(['--attach'], desktop, { HOME: home }), /Supply --home and --executable/);
  assert.equal(developmentOptions(['--attach', '--help'], desktop, { HOME: home }).help, true);
  const settings = path.join(home, 'Library/Application Support/Whip/native-local-runtime.json');
  mkdirSync(path.dirname(settings), { recursive: true });
  for (const contents of ['not JSON', JSON.stringify({ executable: 'relative/whipcode' }), ' '.repeat(4097)]) {
    writeFileSync(settings, contents);
    assert.throws(() => developmentOptions(['--attach'], desktop, { HOME: home }), /Supply --home and --executable/);
  }
  const fixture = developmentOptions(['--attach', '--home', `${desktop}/.dev/home`, '--executable', `${desktop}/.dev/bin/whipcode`], desktop, { HOME: home });
  assert.equal(fixture.env.WHIPCODE_HOME, `${desktop}/.dev/home`);
  assert.equal(fixture.env.WHIP_DESKTOP_EXECUTABLE, `${desktop}/.dev/bin/whipcode`);
});

test('explicit flags override environment targets and each target gets stable GUI settings', () => {
  const inherited = { WHIPCODE_HOME: '/old/home', WHIP_DESKTOP_EXECUTABLE: '/old/bin',
    WHIP_DESKTOP_USER_DATA: '/production/settings', WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_LISTEN: '0.0.0.0:8080' };
  const flags = ['--attach', '--home', '/new/home', '--executable', '/new/bin', '--port', '3002'];
  const selected = developmentOptions(flags, desktop, inherited);
  assert.equal(selected.env.WHIPCODE_HOME, '/new/home');
  assert.equal(selected.env.WHIP_DESKTOP_EXECUTABLE, '/new/bin');
  assert.equal(selected.env.WHIP_DESKTOP_DEV_URL, 'http://127.0.0.1:3002/');
  assert.equal(selected.env.WHIPCODE_LISTEN, undefined);
  assert.equal(selected.env.WHIP_DESKTOP_USER_DATA, developmentOptions(flags, desktop, {}).env.WHIP_DESKTOP_USER_DATA);
  const external = developmentOptions(['--attach'], desktop, inherited);
  assert.equal(external.env.WHIPCODE_HOME, '/old/home');
  assert.notEqual(external.env.WHIP_DESKTOP_USER_DATA, selected.env.WHIP_DESKTOP_USER_DATA);
  assert.notEqual(external.env.WHIP_DESKTOP_USER_DATA, inherited.WHIP_DESKTOP_USER_DATA);
  assert.equal(developmentOptions([], desktop, inherited).env.WHIPCODE_HOME, '/repo/apps/desktop/.dev/home');
});

test('invalid and ambiguous launcher targets fail before starting anything', () => {
  for (const args of [ ['--home', '/tmp/home'], ['--attach', '--home', '/tmp/home'],
    ['--attach', '--executable', '/tmp/bin'], ['--attach', '--home', '', '--executable', ''],
    ['--port', '0'], ['--port', '65536'], ['--port', 'NaN'], ['--unknown'] ])
    assert.throws(() => developmentOptions(args, desktop, {}));
});
