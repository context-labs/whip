// CI-only keychain and notarization-key lifecycle. Never print subprocess argv:
// security receives secret passwords as arguments and execFile errors echo them.
import { execFile } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { appendFile, chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';

const exec = promisify(execFile);
if (process.env.GITHUB_ACTIONS !== 'true' || process.platform !== 'darwin' || !path.isAbsolute(process.env.RUNNER_TEMP ?? ''))
  throw new Error('Signing setup requires an isolated macOS GitHub Actions runner');
const stateFile = path.join(process.env.RUNNER_TEMP, 'whip-desktop-signing.json');
async function security(args) {
  try { return (await exec('/usr/bin/security', args, { maxBuffer: 1 << 20, timeout: 30_000 })).stdout; }
  catch { throw new Error(`macOS signing command failed: ${args[0]}`); }
}
async function cleanup() {
  let state;
  try { state = JSON.parse(await readFile(stateFile, 'utf8')); }
  catch (error) { if (error.code === 'ENOENT') return; throw error; }
  if (path.dirname(state.directory) !== process.env.RUNNER_TEMP || !path.basename(state.directory).startsWith('whip-signing-') ||
      state.keychain !== path.join(state.directory, 'release.keychain-db') || !Array.isArray(state.previous) ||
      state.previous.some(name => typeof name !== 'string' || !path.isAbsolute(name))) throw new Error('Invalid CI signing cleanup state');
  // Always attempt every cleanup action, even if a partially completed import
  // never created the keychain. Report failures after removing secret files.
  const failures = [];
  try { await security(['list-keychains', '-d', 'user', '-s', ...state.previous]); } catch (error) { failures.push(error); }
  try { await security(['delete-keychain', state.keychain]); } catch (error) { failures.push(error); }
  await rm(state.directory, { recursive: true, force: true });
  await rm(stateFile, { force: true });
  if (failures.length) throw new AggregateError(failures, 'Could not completely restore the CI signing keychain');
}
async function setup() {
  const required = ['WHIP_DESKTOP_CERTIFICATE_P12_BASE64', 'WHIP_DESKTOP_CERTIFICATE_PASSWORD', 'WHIP_DESKTOP_NOTARY_PRIVATE_KEY',
    'WHIP_DESKTOP_SIGN_IDENTITY', 'WHIP_DESKTOP_TEAM_ID', 'WHIP_DESKTOP_NOTARY_KEY_ID', 'WHIP_DESKTOP_NOTARY_ISSUER', 'GITHUB_ENV'];
  for (const name of required) if (!process.env[name]?.trim()) throw new Error(`Missing signing configuration: ${name}`);
  if (!/^[A-Z0-9]{10}$/.test(process.env.WHIP_DESKTOP_TEAM_ID) || !/^[A-Z0-9]{10}$/.test(process.env.WHIP_DESKTOP_NOTARY_KEY_ID) ||
      !/^[a-f0-9-]{36}$/i.test(process.env.WHIP_DESKTOP_NOTARY_ISSUER)) throw new Error('Invalid Apple signing identifiers');
  const encoded = process.env.WHIP_DESKTOP_CERTIFICATE_P12_BASE64.replace(/\s/g, '');
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(encoded) || encoded.length > 4 << 20) throw new Error('Invalid signing certificate encoding');
  const certificate = Buffer.from(encoded, 'base64');
  if (certificate.toString('base64') !== encoded) throw new Error('Invalid signing certificate encoding');
  const notaryKey = process.env.WHIP_DESKTOP_NOTARY_PRIVATE_KEY;
  if (!notaryKey.includes('-----BEGIN PRIVATE KEY-----') || !notaryKey.includes('-----END PRIVATE KEY-----') || notaryKey.length > 16 << 10)
    throw new Error('Invalid notarization private key');
  const searchList = await security(['list-keychains', '-d', 'user']);
  const previous = [...searchList.matchAll(/"([^"\r\n]+)"/g)].map(match => match[1]);
  if (!previous.length) throw new Error('Could not capture the runner keychain search list');
  const directory = await mkdtemp(path.join(process.env.RUNNER_TEMP, 'whip-signing-')); await chmod(directory, 0o700);
  const keychain = path.join(directory, 'release.keychain-db');
  const p12 = path.join(directory, 'certificate.p12');
  const apiKey = path.join(directory, `AuthKey_${process.env.WHIP_DESKTOP_NOTARY_KEY_ID}.p8`);
  await writeFile(stateFile, JSON.stringify({ directory, keychain, previous }), { mode: 0o600, flag: 'wx' });
  const password = randomBytes(32).toString('hex');
  console.log(`::add-mask::${password}`);
  try {
    await writeFile(p12, certificate, { mode: 0o600 }); await writeFile(apiKey, notaryKey, { mode: 0o600 });
    await security(['create-keychain', '-p', password, keychain]);
    await security(['set-keychain-settings', '-lut', '21600', keychain]);
    await security(['unlock-keychain', '-p', password, keychain]);
    await security(['import', p12, '-k', keychain, '-P', process.env.WHIP_DESKTOP_CERTIFICATE_PASSWORD, '-T', '/usr/bin/codesign', '-T', '/usr/bin/security']);
    await security(['set-key-partition-list', '-S', 'apple-tool:,apple:,codesign:', '-s', '-k', password, keychain]);
    await security(['list-keychains', '-d', 'user', '-s', keychain, ...previous]);
    const identities = await security(['find-identity', '-v', '-p', 'codesigning', keychain]);
    if (!identities.includes(process.env.WHIP_DESKTOP_SIGN_IDENTITY)) throw new Error('Configured Developer ID identity is absent from the imported certificate');
    await rm(p12);
    await appendFile(process.env.GITHUB_ENV, `WHIP_DESKTOP_NOTARY_KEY=${apiKey}\n`);
  } catch (error) {
    await cleanup().catch(() => {});
    throw error;
  }
}
if (process.argv[2] === 'setup') await setup();
else if (process.argv[2] === 'cleanup') await cleanup();
else throw new Error('Usage: ci-signing.mjs setup|cleanup');
