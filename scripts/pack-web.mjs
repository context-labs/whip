// Copy only the built browser output into go:embed's input. Never reads host data.
import { cp, mkdir, readFile, readdir, rm, lstat, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { readRendererManifest, verifyRenderer, verifyRendererProvenance } from './renderer-artifact.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const source = path.join(root, 'apps/web/dist');
const target = path.join(root, 'internal/webassets/dist');
const manifest = await readRendererManifest(path.join(root, 'apps/web/renderer-manifest.json'));
if (process.argv.includes('--release')) await verifyRendererProvenance(manifest, root, true);
await verifyRenderer(source, manifest);
const csp = (await readFile(path.join(root, 'internal/webassets/csp.txt'), 'utf8')).trim();
if (csp !== manifest.csp) throw new Error('Renderer CSP differs from the checked-out server policy');
await mkdir(target, { recursive: true });
if ((await lstat(target)).isSymbolicLink()) throw new Error('Web asset target must not be a symlink');
for (const entry of await readdir(target)) await rm(path.join(target, entry), { recursive: true, force: true });
await cp(source, target, { recursive: true });
await writeFile(path.join(target, '.gitkeep'), '');
await verifyRenderer(target, manifest);
console.log(`Web assets packaged for go:embed: ${manifest.digest}`);
