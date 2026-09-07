// Copy only the built browser output into go:embed's input. Never reads host data.
import { cp, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const source = path.join(root, 'apps/web/dist');
const target = path.join(root, 'internal/webassets/dist');
const html = await readFile(path.join(source, 'index.html'), 'utf8').catch(() => {
  throw new Error('Build the web app first: npm run build:web');
});
if (!html.includes('<html') || !html.includes('<script')) throw new Error('The web build is missing its application entry.');
// Validate before replacing the previous output. Symlinks and source maps do not
// belong in the released browser bundle.
async function validate(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name);
    if (entry.isSymbolicLink() || (!entry.isFile() && !entry.isDirectory())) throw new Error(`Unexpected web artifact: ${file}`);
    if (entry.isDirectory()) await validate(file);
    else if (entry.name.endsWith('.map') || (await stat(file)).size > 32 * 1024 * 1024) throw new Error(`Invalid release web artifact: ${file}`);
  }
}
await validate(source);
await mkdir(target, { recursive: true });
for (const entry of await readdir(target)) if (entry !== '.gitkeep') await rm(path.join(target, entry), { recursive: true, force: true });
await cp(source, target, { recursive: true });
await writeFile(path.join(target, '.gitkeep'), '');
console.log('Web assets packaged for go:embed.');
