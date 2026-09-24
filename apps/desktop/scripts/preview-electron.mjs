import { build } from 'esbuild';
import electron from 'electron';
import { spawn, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const directory = await mkdtemp(path.join(tmpdir(), 'whip-preview-electron-'));
try {
  const entry = path.join(directory, 'proof.cjs');
  const key = path.join(directory, 'fixture-key.pem'); const cert = path.join(directory, 'fixture-cert.pem');
  await promisify(execFile)('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', key, '-out', cert, '-days', '1', '-subj', '/CN=localhost']);
  await build({ entryPoints: [fileURLToPath(new URL('./preview-electron-entry.mjs', import.meta.url))], outfile: entry,
    bundle: true, platform: 'node', format: 'cjs', external: ['electron'] });
  const child = spawn(electron, [entry], { stdio: 'inherit', env: { ...process.env, PREVIEW_PROOF_USER_DATA: path.join(directory, 'user-data'), PREVIEW_PROOF_KEY: key, PREVIEW_PROOF_CERT: cert } });
  const timeout = setTimeout(() => { console.error('preview proof timed out'); child.kill('SIGKILL'); }, 45_000);
  const code = await new Promise((resolve, reject) => { child.on('error', reject); child.on('exit', code => resolve(code ?? 1)); });
  clearTimeout(timeout); process.exitCode = code;
} finally { await rm(directory, { recursive: true, force: true }); }
