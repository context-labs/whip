// EAS supplies Node (eas.json), but its images do not guarantee Go. Initialize
// once here after dependency installation; normal npm installs do not run this.
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const version = /^go (\d+\.\d+\.\d+)$/m.exec(await readFile(path.join(root, 'go.mod'), 'utf8'))?.[1];
assert(version, 'Cannot read the Go version from go.mod');
const env = { ...process.env, GOTOOLCHAIN: 'local' };
let temporary;
const run = (command, args) => execFileSync(command, args, { cwd: root, env, stdio: 'inherit' });
const goVersion = () => spawnSync('go', ['env', 'GOVERSION'], { cwd: root, env, encoding: 'utf8' }).stdout?.trim();
async function download(url) {
  const response = await fetch(url, { signal: AbortSignal.timeout(120_000) });
  if (!response.ok) throw new Error(`Go download failed: ${url} (${response.status})`);
  return response;
}

try {
  if (goVersion() !== `go${version}`) {
    assert(['darwin', 'linux'].includes(process.platform), `Unsupported EAS host: ${process.platform}`);
    const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
    assert(arch, `Unsupported EAS architecture: ${process.arch}`);
    const url = `https://dl.google.com/go/go${version}.${process.platform}-${arch}.tar.gz`;
    console.log(`Installing Go ${version} for protocol generation`);
    const checksum = (await (await download(`${url}.sha256`)).text()).trim();
    const bytes = Buffer.from(await (await download(url)).arrayBuffer());
    assert(/^[a-f0-9]{64}$/.test(checksum) && createHash('sha256').update(bytes).digest('hex') === checksum,
      'Go archive checksum does not match');
    temporary = await mkdtemp(path.join(tmpdir(), 'whip-eas-go-'));
    const archive = path.join(temporary, 'go.tar.gz');
    await writeFile(archive, bytes);
    run('tar', ['-xzf', archive, '-C', temporary]);
    env.GOROOT = path.join(temporary, 'go');
    env.PATH = `${path.join(env.GOROOT, 'bin')}${path.delimiter}${env.PATH ?? ''}`;
  }
  assert.equal(goVersion(), `go${version}`, 'EAS must use the Go version from go.mod');
  run('npm', ['run', 'generate']);
  run('npm', ['run', 'build']);
} finally {
  if (temporary) await rm(temporary, { recursive: true, force: true });
}
