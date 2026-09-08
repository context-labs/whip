import { createHash } from 'node:crypto';
import { execFile } from 'node:child_process';
import { lstat, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const exec = promisify(execFile);
export const repositoryRoot = fileURLToPath(new URL('../', import.meta.url));
export const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');

/** Reject unexpected files before either host consumes the release artifact. */
export async function rendererFiles(directory) {
  const files = {};
  let total = 0;
  async function visit(relative) {
    const filename = path.join(directory, relative);
    const stat = await lstat(filename);
    if (stat.isSymbolicLink()) throw new Error(`Unexpected web artifact: ${relative}`);
    if (stat.isDirectory()) {
      for (const name of (await readdir(filename)).sort()) {
        if (relative === '' && name === '.gitkeep') {
          const placeholder = await lstat(path.join(directory, name));
          if (!placeholder.isFile() || placeholder.isSymbolicLink() || placeholder.size !== 0)
            throw new Error('Unexpected web artifact: .gitkeep must be an empty regular file');
          continue;
        }
        if (name.startsWith('.') || name.includes('\\')) throw new Error(`Unexpected web artifact: ${name}`);
        await visit(relative ? `${relative}/${name}` : name);
      }
      return;
    }
    if (!stat.isFile() || relative.endsWith('.map') || stat.size > 32 << 20)
      throw new Error(`Invalid release web artifact: ${relative}`);
    total += stat.size;
    if (total > 256 << 20 || Object.keys(files).length >= 4096) throw new Error('Renderer artifact exceeds its release limits');
    const bytes = await readFile(filename);
    files[relative] = { bytes: bytes.length, sha256: sha256(bytes) };
  }
  await visit('');
  const html = await readFile(path.join(directory, 'index.html'), 'utf8').catch(() => {
    throw new Error('Build the web app first: npm run build:web');
  });
  if (!html.includes('<html') || !html.includes('<script')) throw new Error('The web build is missing its application entry.');
  return files;
}

export function rendererDigest(files, csp) { return sha256(JSON.stringify({ files, csp })); }

export async function readRendererManifest(filename) {
  const stat = await lstat(filename);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error('Invalid renderer manifest file');
  if (stat.size > 2 << 20) throw new Error('Renderer manifest is too large');
  const bytes = await readFile(filename);
  if (bytes.length > 2 << 20) throw new Error('Renderer manifest is too large');
  const manifest = JSON.parse(bytes.toString('utf8'));
  if (manifest?.schema !== 1 || !manifest.files || typeof manifest.files !== 'object' || Array.isArray(manifest.files) ||
      typeof manifest.csp !== 'string' || !manifest.csp || /[\r\n]/.test(manifest.csp) ||
      !/^[a-f0-9]{64}$/.test(manifest.digest ?? '')) throw new Error('Invalid renderer manifest');
  const entries = Object.entries(manifest.files);
  if (!entries.length || entries.length > 4096) throw new Error('Invalid renderer file count');
  for (const [name, value] of entries) {
    if (!name || path.posix.isAbsolute(name) || name.includes('\\') || name.split('/').some(part => !part || part.startsWith('.')) ||
        !Number.isSafeInteger(value?.bytes) || value.bytes < 0 || value.bytes > 32 << 20 || !/^[a-f0-9]{64}$/.test(value.sha256 ?? ''))
      throw new Error(`Invalid renderer manifest entry: ${name}`);
  }
  if (rendererDigest(manifest.files, manifest.csp) !== manifest.digest) throw new Error('Renderer manifest digest does not match');
  return manifest;
}

/** Release consumers bind the artifact to their checkout, not only to itself. */
export async function verifyRendererProvenance(manifest, root = repositoryRoot, release = false) {
  const { stdout } = await exec('git', ['rev-parse', 'HEAD'], { cwd: root });
  if (manifest.source?.commit !== stdout.trim() || typeof manifest.source?.dirty !== 'boolean' ||
      manifest.lockfile !== sha256(await readFile(path.join(root, 'package-lock.json'))))
    throw new Error('Renderer provenance differs from the requested source or dependency lockfile');
  if (release && manifest.source.dirty) throw new Error('A release renderer must come from a clean checkout');
  if (release) {
    const { stdout: changes } = await exec('git', ['status', '--porcelain', '--untracked-files=normal'], { cwd: root });
    if (changes) throw new Error('A release consumer must use a clean checkout');
  }
}

export async function verifyRenderer(directory, manifest) {
  const files = await rendererFiles(directory);
  if (rendererDigest(files, manifest.csp) !== manifest.digest) throw new Error('Renderer files differ from the built artifact');
}

export async function createRendererManifest(root = repositoryRoot) {
  const directory = path.join(root, 'apps/web/dist');
  const files = await rendererFiles(directory);
  const csp = (await readFile(path.join(root, 'internal/webassets/csp.txt'), 'utf8')).trim();
  const { stdout: commit } = await exec('git', ['rev-parse', 'HEAD'], { cwd: root });
  const { stdout: changes } = await exec('git', ['status', '--porcelain', '--untracked-files=normal'], { cwd: root });
  const manifest = { schema: 1, source: { commit: commit.trim(), dirty: changes.length > 0 },
    lockfile: sha256(await readFile(path.join(root, 'package-lock.json'))), csp, files, digest: rendererDigest(files, csp) };
  await writeFile(path.join(root, 'apps/web/renderer-manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
  return manifest;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const manifest = await createRendererManifest();
  console.log(`Renderer artifact ${manifest.digest}: ${Object.keys(manifest.files).length} files`);
}
