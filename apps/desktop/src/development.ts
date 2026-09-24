import { createHash } from 'node:crypto';
import { lstatSync, readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import path from 'node:path';
import { parseArgs } from 'node:util';

export const developmentHelp = `Usage: npm run dev:desktop -- [--attach [--home PATH --executable PATH]] [--port PORT]

--attach      Use an already-running daemon; never install, start, or restart it.
              Defaults to ~/.whipcode and the installed Whip app's executable.
--home        Daemon home (or WHIPCODE_HOME); supply together with --executable.
--executable  Existing whipcode path (or WHIP_DESKTOP_EXECUTABLE).
--port        Vite loopback port (default 3001).

Without --attach, build and manage the isolated development backend as before.`;

function installedExecutable(userHome: string) {
  const settings = path.join(userHome, 'Library/Application Support/Whip/native-local-runtime.json');
  try {
    const info = lstatSync(settings);
    if (!info.isFile() || info.size > 4096) throw new Error('Invalid local runtime settings');
    const value = JSON.parse(readFileSync(settings, 'utf8')) as { executable?: unknown };
    if (typeof value.executable !== 'string' || !path.isAbsolute(value.executable)) throw new Error('Invalid executable path');
    return value.executable;
  } catch {
    throw new Error('Cannot read the installed Whip app’s executable selection. Supply --home and --executable for the daemon to attach to.');
  }
}

export function developmentOptions(args: string[], desktop: string, inherited = process.env) {
  const { values } = parseArgs({ args, options: {
    attach: { type: 'boolean' }, home: { type: 'string' }, executable: { type: 'string' },
    port: { type: 'string', default: '3001' }, help: { type: 'boolean' },
  } });
  const fixture = path.join(desktop, '.dev');
  const attach = values.attach === true;
  if (!attach && (values.home || values.executable)) throw new Error('--home and --executable require --attach.');
  const home = attach ? values.home ?? inherited.WHIPCODE_HOME : undefined;
  const executable = attach ? values.executable ?? inherited.WHIP_DESKTOP_EXECUTABLE : undefined;
  if ([home, executable].some(value => value !== undefined && !value.trim())) throw new Error('Development target paths cannot be empty.');
  if (!!home !== !!executable) throw new Error('Supply both --home and --executable (or WHIPCODE_HOME and WHIP_DESKTOP_EXECUTABLE).');
  const port = Number(values.port);
  if (!Number.isSafeInteger(port) || port < 1 || port > 65535) throw new Error('--port must be between 1 and 65535.');
  const userHome = inherited.HOME || homedir();
  const env: NodeJS.ProcessEnv = { ...inherited,
    WHIPCODE_HOME: path.resolve(home ?? (attach ? path.join(userHome, '.whipcode') : path.join(fixture, 'home'))),
    WHIP_DESKTOP_EXECUTABLE: path.resolve(executable ?? (attach && !values.help ? installedExecutable(userHome) : path.join(fixture, 'bin/whipcode'))),
    WHIP_DESKTOP_USER_DATA: path.join(fixture, 'user-data'),
    WHIP_DESKTOP_DEV_URL: `http://127.0.0.1:${port}/`,
  };
  for (const value of [env.WHIPCODE_HOME!, env.WHIP_DESKTOP_EXECUTABLE!])
    if (Buffer.byteLength(value) > 2048 || /[\u0000-\u001f\u007f]/.test(value)) throw new Error('Invalid development target path.');
  for (const key of ['WHIPCODE_NETWORK', 'WHIPCODE_LISTEN', 'WHIPCODE_ALLOWED_HOSTS', 'WHIPCODE_ALLOWED_ORIGINS', 'WHIP_COMPUTER_BIN',
    'WHIP_DESKTOP_ATTACH', 'WHIP_DESKTOP_FIXTURE']) delete env[key];
  env.WHIPCODE_NETWORK = '0';
  if (attach) {
    env.WHIP_DESKTOP_ATTACH = '1';
    const target = createHash('sha256').update(JSON.stringify([env.WHIPCODE_HOME!, env.WHIP_DESKTOP_EXECUTABLE!])).digest('hex').slice(0, 16);
    env.WHIP_DESKTOP_USER_DATA = path.join(fixture, 'attach', target, 'user-data');
  } else env.WHIP_DESKTOP_FIXTURE = '1';
  return { attach, help: values.help, port, env, appDirectory: path.join(fixture, 'attach-app') };
}

/** A packaged release must never enter the development-only attachment policy. */
export function attachDevelopment(isPackaged: boolean, env = process.env) {
  if (env.WHIP_DESKTOP_ATTACH !== '1') return false;
  if (isPackaged) throw new Error('Attach development mode is unavailable in a packaged app.');
  if (!env.WHIP_DESKTOP_DEV_URL || ![env.WHIPCODE_HOME, env.WHIP_DESKTOP_EXECUTABLE, env.WHIP_DESKTOP_USER_DATA]
    .every(value => value && path.isAbsolute(value))) throw new Error('Attach development requires an explicit executable, home, user data, and renderer URL.');
  return true;
}
