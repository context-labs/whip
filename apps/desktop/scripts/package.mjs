import { api as forge } from '@electron-forge/core';
import asar from '@electron/asar';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { buildDesktop } from './build.mjs';
import { verifyDesktop } from './verify.mjs';
import { bundleDigest, prepareDistribution } from './distribution.mjs';

const dir = await buildDesktop({ rendererReady: process.argv.includes('--renderer-ready') });
await forge.package({ dir, arch: 'arm64', platform: 'darwin' });
const { productName } = JSON.parse(await readFile(`${dir}/package.json`, 'utf8'));
const bundle = fileURLToPath(new URL(`../out/${productName}-darwin-arm64/${productName}.app`, import.meta.url));
const evidence = await verifyDesktop(bundle,
  { signed: !!process.env.WHIP_DESKTOP_SIGN_IDENTITY, notarized: process.env.WHIP_DESKTOP_NOTARIZE === '1' });
evidence.bundleDigest = await bundleDigest(bundle);
const exec = promisify(execFile);
evidence.toolchain = { node: process.version, arch: process.arch,
  go: (await exec('go', ['version'])).stdout.trim(),
  xcode: (await exec('/usr/bin/xcodebuild', ['-version'])).stdout.trim(),
  macOS: (await exec('/usr/bin/sw_vers', ['-productVersion'])).stdout.trim(),
  ...(process.env.ImageVersion ? { runnerImage: process.env.ImageVersion } : {}) };
const output = fileURLToPath(new URL('../out/release', import.meta.url));
await mkdir(output, { recursive: true });
await writeFile(`${output}/evidence.json`, JSON.stringify(evidence, null, 2) + '\n');
if (process.argv.includes('--make')) {
  const results = await forge.make({ dir, arch: 'arm64', platform: 'darwin', skipPackage: true });
  await prepareDistribution(results.flatMap(result => result.artifacts), output, evidence);
}
for (const name of ['sbom.cdx.json', 'THIRD_PARTY_NOTICES.txt'])
  await writeFile(`${output}/${name}`, asar.extractFile(`${bundle}/Contents/Resources/app.asar`, `licenses/${name}`));
