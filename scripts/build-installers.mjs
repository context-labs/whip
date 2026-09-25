// One installer implementation, with immutable release policies in generated assets.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import semver from 'semver';

const sourcePolicy = 'INSTALLER_MODE=source\nINSTALLER_VERSION=\n';

export function buildInstallers(template, tag) {
  const version = typeof tag === 'string' && tag.startsWith('v') && semver.parse(tag.slice(1));
  assert(version && version.major >= 1 &&
    tag === `v${version.version}${version.build.length ? '+' + version.build.join('.') : ''}`,
  'Installer tag must be a canonical v1+ SemVer tag');
  assert(typeof template === 'string' && template.split(sourcePolicy).length === 2,
    'Installer template must contain exactly one source policy block');
  return {
    'install.sh': template.replace(sourcePolicy,
      `# Release-pinned installer: ignores WHIPCODE_VERSION and WHIPCODE_CHANNEL.\nINSTALLER_MODE=pinned\nINSTALLER_VERSION=${tag}\n`),
    'latest.sh': template.replace(sourcePolicy,
      '# Latest stable installer: ignores WHIPCODE_VERSION and WHIPCODE_CHANNEL.\nINSTALLER_MODE=latest\nINSTALLER_VERSION=\n'),
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [tag, directory, check] = process.argv.slice(2);
  assert(tag && directory && (check === undefined || check === '--check') && process.argv.length <= 5,
    'Usage: node scripts/build-installers.mjs TAG OUTDIR [--check]');
  const template = await readFile(new URL('../install.sh', import.meta.url), 'utf8');
  const installers = buildInstallers(template, tag);
  if (!check) await mkdir(directory, { recursive: true });
  for (const [name, expected] of Object.entries(installers)) {
    const destination = path.join(directory, name);
    if (check) assert.equal(await readFile(destination, 'utf8'), expected, `${name} differs from generated installer`);
    else await writeFile(destination, expected, { mode: 0o755 });
  }
}
