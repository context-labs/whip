import { execFile, spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { constants, tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs, promisify } from 'node:util';
import { repositoryRoot } from './renderer-artifact.mjs';

const exec = promisify(execFile);
const help = `Usage: task onboarding:docker
       node scripts/onboarding-docker.mjs

Build the current working files and open a clean Whip TUI plus web app at
http://localhost:4000. Quitting removes the container and its application state;
build caches remain. Use a fresh private browser session for clean web onboarding.
Requires a local Linux Docker engine, Git, Node 24, and an interactive terminal.
The source checkout and installed Whip are not used as the test workspace.`;

export async function checkPort(port = 4000) {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once('error', () => reject(new Error(`Port ${port} is unavailable. Stop its existing listener and retry.`)));
    server.listen({ host: '127.0.0.1', port, exclusive: true }, () => server.close(resolve));
  });
}

export async function main(args = process.argv.slice(2), {
  root = repositoryRoot, env = process.env, interactive = process.stdin.isTTY && process.stdout.isTTY,
  ensurePort = checkPort,
} = {}) {
  const { values } = parseArgs({ args, options: { help: { type: 'boolean', short: 'h' } } });
  if (values.help) { console.log(help); return; }
  if (!interactive) throw new Error('Run task onboarding:docker in an interactive terminal (stdin and stdout must be TTYs).');
  const packageJSON = JSON.parse(await readFile(path.join(root, 'package.json'), 'utf8'));
  const nodeVersion = /^>=(\d+) <\d+$/.exec(packageJSON.engines.node)?.[1];
  if (!nodeVersion || process.versions.node.split('.')[0] !== nodeVersion)
    throw new Error(`Use Node matching package.json (${packageJSON.engines.node}).`);
  const goVersion = /^go (\d+\.\d+(?:\.\d+)?)$/m.exec(await readFile(path.join(root, 'go.mod'), 'utf8'))?.[1];
  if (!goVersion) throw new Error('Cannot read the Go version from go.mod.');

  // docker build uses the current engine's builder. Ignore an ambient Buildx
  // override so source and output stay on that same local engine.
  const dockerEnv = { ...env, DOCKER_BUILDKIT: '1' };
  delete dockerEnv.BUILDX_BUILDER;
  delete dockerEnv.DOCKER_DEFAULT_PLATFORM;
  const capture = async (file, argv) => (await exec(file, argv,
    { cwd: root, env: dockerEnv, timeout: 30_000, maxBuffer: 2 << 20 })).stdout.trim();
  let endpoint;
  try {
    const context = env.DOCKER_CONTEXT || (!env.DOCKER_HOST && await capture('docker', ['context', 'show']));
    if (context) {
      dockerEnv.DOCKER_CONTEXT = context;
      endpoint = JSON.parse(await capture('docker', ['context', 'inspect', context, '--format', '{{json .Endpoints.docker.Host}}']));
    } else endpoint = env.DOCKER_HOST;
    if (!endpoint?.startsWith('unix://')) throw new Error(`Select a local Docker context with a Unix socket; current endpoint: ${endpoint}`);
    const platform = await capture('docker', ['info', '--format', '{{.OSType}}/{{.Architecture}}']);
    if (!/^linux\/(aarch64|arm64|x86_64|amd64)$/.test(platform))
      throw new Error(`A local Linux arm64 or amd64 Docker engine is required; got ${platform}.`);
  } catch (error) {
    throw new Error(`Docker is not ready: ${error.message}. Start Docker Desktop or OrbStack, then retry.`);
  }
  await ensurePort();
  const commit = await capture('git', ['rev-parse', 'HEAD']);
  const dirty = (await capture('git', ['status', '--porcelain', '--untracked-files=normal'])).length > 0;
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(commit)) throw new Error('Cannot determine the source commit.');
  const version = `dev-docker-${commit.slice(0, 12)}${dirty ? '-dirty' : ''}`;
  const container = `whip-onboarding-${randomUUID()}`;
  const temporary = await mkdtemp(path.join(tmpdir(), 'whip-onboarding-build-'));
  const iidfile = path.join(temporary, 'image-id');
  let child;
  let interrupted;
  let created = false;
  const interrupt = signal => {
    interrupted ??= signal;
    child?.kill(signal);
  };
  const handlers = new Map(['SIGINT', 'SIGTERM', 'SIGHUP'].map(signal => [signal, () => interrupt(signal)]));
  for (const [signal, handler] of handlers) process.on(signal, handler);
  const checkInterrupted = () => {
    if (interrupted) throw Object.assign(new Error(`Interrupted by ${interrupted}`), { exitCode: 128 + constants.signals[interrupted] });
  };
  const run = argv => new Promise((resolve, reject) => {
    child = spawn('docker', argv, { cwd: root, env: dockerEnv, stdio: 'inherit' });
    child.once('error', reject);
    child.once('close', (code, signal) => {
      child = undefined;
      if (code === 0) resolve();
      else reject(Object.assign(new Error(`docker ${argv[0]} failed (${signal || code})`),
        { exitCode: signal ? 128 + constants.signals[signal] : code || 1 }));
    });
  });
  try {
    console.log(`Building ${version} from ${root}`);
    await run(['build', '--file', 'scripts/docker/onboarding.Dockerfile', '--tag', 'whip-onboarding:local',
      '--iidfile', iidfile, '--build-arg', `NODE_VERSION=${nodeVersion}`, '--build-arg', `GO_VERSION=${goVersion}`,
      '--build-arg', `WHIP_BUILD_VERSION=${version}`,
      '--build-arg', `WHIP_RENDERER_LOCAL_SOURCE=${JSON.stringify({ commit, dirty })}`, '.']);
    checkInterrupted();
    const image = (await readFile(iidfile, 'utf8')).trim();
    if (!/^sha256:[a-f0-9]{64}$/.test(image)) throw new Error('Docker did not return a valid image ID.');
    await ensurePort();
    console.log(`Image: ${image}\nContainer: ${container}\nLogs: docker exec ${container} whip daemon logs -n 60`);
    const terminalEnv = ['--env', `TERM=${env.TERM || 'xterm-256color'}`];
    if (env.COLORTERM) terminalEnv.push('--env', `COLORTERM=${env.COLORTERM}`);
    // Create separately so interruption during attachment cannot lose ownership
    // of a container that the engine is still creating. Run the exact built ID.
    created = true;
    await capture('docker', ['create', '--rm', '--init', '-it', '--name', container,
      '--publish', '127.0.0.1:4000:4000', ...terminalEnv, image]);
    checkInterrupted();
    await run(['start', '--attach', '--interactive', container]);
    checkInterrupted();
  } finally {
    if (created) {
      // Auto-remove normally already did this. On attachment failure, give the
      // entrypoint time to stop the daemon, then remove only our unique container.
      await capture('docker', ['stop', '--time', '8', container]).catch(() => {});
      await capture('docker', ['rm', '--force', container]).catch(error => {
        if (!/No such container/.test(error.stderr || '')) console.error(`Cleanup failed for ${container}: ${error.message}`);
      });
    }
    for (const [signal, handler] of handlers) process.off(signal, handler);
    await rm(temporary, { recursive: true, force: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => { console.error(error.message); process.exitCode = error.exitCode || 1; });
}
