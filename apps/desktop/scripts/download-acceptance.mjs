import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { open, mkdir, writeFile, unlink } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
assert.equal(process.env.GITHUB_ACTIONS, 'true');
assert.equal(process.env.RUNNER_ENVIRONMENT, 'github-hosted');
const url = new URL(process.env.ARCHIVE_URL);
assert.equal(url.origin, 'https://whipcode-releases.inference.net');
assert(url.pathname.endsWith('.zip') && !url.search && !url.hash && !url.username && !url.password);
assert(/^[a-f0-9]{64}$/.test(process.env.ARCHIVE_SHA256));
await mkdir('acceptance', { recursive: true });
await mkdir('downloaded', { recursive: true });
const result = await fetch(url, { redirect: 'error', signal: AbortSignal.timeout(120_000) });
assert.equal(result.status, 200);
const archive = await open('acceptance/app.zip', 'wx', 0o600);
let bytes = 0; const hash = createHash('sha256');
try {
  for await (const chunk of result.body) {
    bytes += chunk.length; assert(bytes <= 1 << 30);
    hash.update(chunk); await archive.writeFile(chunk);
  }
} finally { await archive.close(); }
assert.equal(hash.digest('hex'), process.env.ARCHIVE_SHA256);
execFileSync('/usr/bin/ditto', ['-x', '-k', 'acceptance/app.zip', 'downloaded']);
await unlink('acceptance/app.zip');
execFileSync('/usr/bin/codesign', ['--verify', '--deep', '--strict', '-R', '=anchor apple generic and certificate leaf[subject.OU] = "JAPWPV5JY2"', 'downloaded/Whip Beta.app']);
execFileSync('/usr/sbin/spctl', ['--assess', '--type', 'execute', '--verbose=4', 'downloaded/Whip Beta.app']);
await writeFile('acceptance/download.json', JSON.stringify({ url: url.href, sha256: process.env.ARCHIVE_SHA256, bytes, platform: process.platform, arch: process.arch, version: process.env.RELEASE_VERSION }, null, 2)+'\n');
