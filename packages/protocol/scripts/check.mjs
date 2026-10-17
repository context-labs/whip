import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const cwd = fileURLToPath(new URL('../', import.meta.url));
for (const [command, args] of [
  ['go', ['run', '../../cmd/whip-contract', '-out', 'schema', '-check']],
  [process.execPath, ['scripts/generate.mjs', '--check']],
]) {
  const { status, error } = spawnSync(command, args, { cwd, stdio: 'inherit' });
  if (status !== 0) {
    if (error) console.error(error.message);
    console.error('Protocol artifacts could not be verified. Run task generate (run npm ci first on a new checkout).');
    process.exit(status ?? 1);
  }
}
