import { build } from 'esbuild';
import { spawn } from 'node:child_process';
import { execFile } from 'node:child_process';
import { copyFile, cp, lstat, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { dependencyNotices } from './notices.mjs';
import { readRendererManifest, repositoryRoot, sha256, verifyRenderer, verifyRendererProvenance } from '../../../scripts/renderer-artifact.mjs';

const desktop = fileURLToPath(new URL('../', import.meta.url));
const stage = path.join(desktop, '.stage');
const appDirectory = path.join(stage, 'app');
const native = path.join(stage, 'native');
export async function command(executable, args, options = {}) {
  await new Promise((resolve, reject) => {
    const child = spawn(executable, args, { cwd: repositoryRoot, stdio: 'inherit', ...options });
    child.once('error', reject);
    child.once('exit', code => code === 0 ? resolve() : reject(new Error(`${executable} failed (${code})`)));
  });
}

export async function buildDesktop({ rendererReady = false } = {}) {
  if (process.platform !== 'darwin' || process.arch !== 'arm64') throw new Error('Desktop packaging currently targets macOS arm64');
  const metadata = JSON.parse(await readFile(path.join(desktop, 'package.json'), 'utf8'));
  const version = process.env.WHIP_DESKTOP_VERSION || metadata.version;
  if (!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(version)) throw new Error('Invalid desktop version');
  const buildId = process.env.WHIPCODE_VERSION || version;
  if (!/^[a-zA-Z0-9][a-zA-Z0-9.+-]{0,127}$/.test(buildId)) throw new Error('Invalid whipcode build ID');
  const channel = process.env.WHIP_DESKTOP_CHANNEL || (version.includes('-') ? 'beta' : 'stable');
  if (!['stable', 'beta'].includes(channel) || (channel === 'stable' && version.includes('-'))) throw new Error('Invalid desktop release channel');
  const appName = channel === 'beta' ? 'Whip Beta' : 'Whip';
  const bundleId = channel === 'beta' ? 'com.contextlabs.whip.beta' : 'com.contextlabs.whip';
  const updateURL = process.env.WHIP_DESKTOP_UPDATE_URL;
  if (updateURL) {
    const url = new URL(updateURL);
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || !url.pathname.endsWith('/RELEASES.json'))
      throw new Error('WHIP_DESKTOP_UPDATE_URL must be an HTTPS RELEASES.json feed');
  }
  await command('npm', ['run', rendererReady ? 'build' : 'build:web']);
  const renderer = await readRendererManifest(path.join(repositoryRoot, 'apps/web/renderer-manifest.json'));
  await verifyRendererProvenance(renderer, repositoryRoot, process.env.WHIP_DESKTOP_RELEASE === '1');
  await verifyRenderer(path.join(repositoryRoot, 'apps/web/dist'), renderer);
  await command(process.execPath, ['scripts/pack-web.mjs']);
  await rm(stage, { recursive: true, force: true });
  await mkdir(appDirectory, { recursive: true }); await mkdir(native, { recursive: true });
  const identity = process.env.WHIP_DESKTOP_SIGN_IDENTITY;
  const teamId = process.env.WHIP_DESKTOP_TEAM_ID;
  if (identity && !/^[A-Z0-9]{10}$/.test(teamId ?? '')) throw new Error('Signing requires WHIP_DESKTOP_TEAM_ID');
  if (process.env.WHIP_DESKTOP_RELEASE === '1' && (!identity || !updateURL || process.env.WHIP_DESKTOP_NOTARIZE !== '1'))
    throw new Error('Release builds require signing, an update feed and notarization');
  await command('swift', ['build', '-c', 'release', '--package-path', 'driver']);
  const helper = path.join(native, 'whip-computer');
  await copyFile(path.join(repositoryRoot, 'driver/.build/release/whip-computer'), helper);
  await command('/usr/bin/codesign', ['--force', '--sign', identity || '-', '--identifier', `${bundleId}.computer`,
    ...(identity ? ['--options', 'runtime', '--timestamp'] : []), helper]);
  // Supply go:embed through a build overlay; never rewrite the tracked empty
  // placeholder or let a concurrent CLI build pick up desktop signing inputs.
  const overlay = path.join(stage, 'go-overlay.json');
  await writeFile(overlay, JSON.stringify({ Replace: { [path.join(repositoryRoot, 'internal/computer/bin/whip-computer')]: helper } }));
  await command('go', ['build', '-overlay', overlay, '-trimpath', '-ldflags', `-s -w -X main.version=${buildId} -X github.com/context-labs/whip/internal/buildinfo.Name=whipcode -X github.com/context-labs/whip/internal/buildinfo.UpdateOwner=desktop`, '-o', path.join(native, 'whipcode'), './cmd/whip'],
    { env: { ...process.env, GOOS: 'darwin', GOARCH: 'arm64', CGO_ENABLED: '0' } });
  await command('/usr/bin/codesign', ['--force', '--sign', identity || '-', '--identifier', `${bundleId}.runtime`,
    ...(identity ? ['--options', 'runtime', '--timestamp', '--entitlements', path.join(desktop, 'resources/runtime.entitlements.plist')] : []), path.join(native, 'whipcode')]);
  const files = {};
  for (const name of ['whipcode', 'whip-computer']) {
    const bytes = await readFile(path.join(native, name));
    if (!(await lstat(path.join(native, name))).isFile() || !bytes.length) throw new Error(`Missing ${name}`);
    files[name] = { bytes: bytes.length, sha256: sha256(bytes) };
  }
  if ((await readFile(path.join(native, 'whipcode'))).indexOf(await readFile(helper)) < 0)
    throw new Error('Go did not embed the exact signed helper');
  const info = JSON.parse((await promisify(execFile)(path.join(native, 'whipcode'), ['_desktop-runtime-info'],
    { timeout: 5000, maxBuffer: 16 << 10, encoding: 'utf8' })).stdout);
  if (info.distribution !== 'whipcode' || info.updateOwner !== 'desktop' || info.buildId !== buildId || !Number.isSafeInteger(info.protocolMajor) || info.protocolMajor < 1 ||
      !Number.isSafeInteger(info.protocolMinor) || info.protocolMinor < 0 ||
      !Number.isSafeInteger(info.schemaVersion) || info.schemaVersion < 1) throw new Error('Go runtime metadata did not match the requested build');
  const manifest = { schema: 1, distribution: 'whipcode', version, buildId, architecture: 'arm64', ...(identity ? { teamId } : {}), rendererDigest: renderer.digest,
    source: { ...renderer.source, lockfile: renderer.lockfile }, compatibility: { protocolMajor: info.protocolMajor, protocolMinor: info.protocolMinor, schemaVersion: info.schemaVersion }, files };
  await writeFile(path.join(appDirectory, 'runtime-manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
  await writeFile(path.join(appDirectory, 'desktop-config.json'), JSON.stringify({ channel, ...(updateURL ? { updateURL } : {}) }, null, 2) + '\n');
  await copyFile(path.join(repositoryRoot, 'apps/web/renderer-manifest.json'), path.join(appDirectory, 'renderer-manifest.json'));
  const licenses = path.join(appDirectory, 'licenses');
  await mkdir(licenses);
  await copyFile(path.join(repositoryRoot, 'LICENSE'), path.join(licenses, 'Whip.txt'));
  for (const name of ['LICENSE', 'LICENSES.chromium.html'])
    await copyFile(path.join(repositoryRoot, 'node_modules/electron/dist', name), path.join(licenses, name));
  await dependencyNotices(path.join(native, 'whipcode'), licenses, version);
  await cp(path.join(repositoryRoot, 'apps/web/dist'), path.join(appDirectory, 'renderer'), { recursive: true });
  await verifyRenderer(path.join(appDirectory, 'renderer'), renderer);
  await build({ absWorkingDir: desktop, entryPoints: { main: 'src/main.ts', preload: 'src/preload.ts' },
    outdir: appDirectory, outExtension: { '.js': '.cjs' }, bundle: true, platform: 'node', format: 'cjs',
    target: 'node24', external: ['electron'], sourcemap: false, minify: true,
    define: { __APP_VERSION__: JSON.stringify(version), __APP_NAME__: JSON.stringify(appName) } });
  await writeFile(path.join(appDirectory, 'package.json'), JSON.stringify({ name: channel === 'beta' ? 'whip-desktop-beta' : 'whip-desktop', productName: appName, version,
    description: 'Whip desktop', author: 'Context Labs', main: 'main.cjs',
    devDependencies: { electron: metadata.devDependencies.electron }, config: { forge: '../../forge.config.cjs' } }, null, 2) + '\n');
  console.log(`Desktop staged: ${appDirectory}\nRenderer: ${renderer.digest}\nRuntime: ${files.whipcode.sha256}`);
  return appDirectory;
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  await buildDesktop({ rendererReady: process.argv.includes('--renderer-ready') });
