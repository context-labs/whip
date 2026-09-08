import { api as forge } from '@electron-forge/core';
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
const output = fileURLToPath(new URL('../out/release', import.meta.url));
await mkdir(output, { recursive: true });
await writeFile(`${output}/evidence.json`, JSON.stringify(evidence, null, 2) + '\n');
if (process.argv.includes('--make')) {
  const results = await forge.make({ dir, arch: 'arm64', platform: 'darwin', skipPackage: true });
  await prepareDistribution(results.flatMap(result => result.artifacts), output, evidence);
}
