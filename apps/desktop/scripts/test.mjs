import { build } from 'esbuild';
import { mkdtemp, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { command } from './build.mjs';

const desktop = fileURLToPath(new URL('../', import.meta.url));
const directory = await mkdtemp(path.join(tmpdir(), 'whip-desktop-tests-'));
try {
  const tests = (await readdir(path.join(desktop, 'tests'))).filter(file => file.endsWith('.test.ts'));
  if (!tests.length) throw new Error('No desktop tests found');
  await build({ entryPoints: tests.map(file => path.join(desktop, 'tests', file)), outdir: directory,
    bundle: true, platform: 'node', format: 'cjs', external: ['electron'], outExtension: { '.js': '.cjs' } });
  await command(process.execPath, ['--test', ...tests.map(file => path.join(directory, file.replace(/\.ts$/, '.cjs')))]);
} finally { await rm(directory, { recursive: true, force: true }); }
