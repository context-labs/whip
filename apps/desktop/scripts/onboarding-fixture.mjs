// Disposable stock-Electron acceptance only. Never alters the real stage or trust store.
import { cp, lstat, mkdir, readFile, writeFile } from 'node:fs/promises';
import { execFile } from 'node:child_process';
import path from 'node:path';
import { promisify } from 'node:util';
import { fileDigest, readRuntimeManifest } from '../src/runtime.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';
const exec = promisify(execFile);

export async function onboardingStage(directory) {
  const source = path.join(repositoryRoot, 'apps/desktop/.stage');
  const stage = path.join(directory, 'stage');
  await mkdir(stage);
  await cp(path.join(source, 'app'), path.join(stage, 'app'), { recursive: true });
  await cp(path.join(source, 'native'), path.join(stage, 'native'), { recursive: true });
  const manifest = await readRuntimeManifest(path.join(stage, 'app/runtime-manifest.json'));
  const transport = path.join(repositoryRoot, 'apps/desktop/scripts/fixtures/onboarding-transport.go.txt');
  const overlay = path.join(directory, 'onboarding-overlay.json');
  await writeFile(overlay, JSON.stringify({ Replace: {
    [path.join(repositoryRoot, 'cmd/whip/zz_onboarding_fixture.go')]: transport,
    [path.join(repositoryRoot, 'internal/computer/bin/whip-computer')]: path.join(stage, 'native/whip-computer'),
  } }));
  const runtime = path.join(stage, 'native/whipcode');
  await exec('go', ['build', '-overlay', overlay, '-trimpath', '-ldflags', `-s -w -X main.version=${manifest.buildId} -X github.com/context-labs/whip/internal/buildinfo.UpdateOwner=desktop`, '-o', runtime, './cmd/whip'],
    { cwd: repositoryRoot, env: { ...process.env, GOOS: 'darwin', GOARCH: 'arm64', CGO_ENABLED: '0' }, timeout: 180000, maxBuffer: 1 << 20 });
  await exec('/usr/bin/codesign', ['--force', '--sign', '-', '--identifier', 'com.contextlabs.whip.onboarding-fixture', runtime]);
  const originalDigest = manifest.files.whipcode.sha256;
  manifest.files.whipcode = { bytes: (await lstat(runtime)).size, sha256: await fileDigest(runtime) };
  await writeFile(path.join(stage, 'app/runtime-manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
  return { stage, provenance: { stagedRuntimeDigest: originalDigest, fixtureRuntimeDigest: manifest.files.whipcode.sha256,
    transportSourceDigest: await fileDigest(transport), transport: 'Test-only build overlay routes exactly the three canonical OpenRouter HTTP paths to loopback; all other outbound destinations fail closed.', signing: 'ad-hoc fixture only' } };
}
