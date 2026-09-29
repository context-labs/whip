// Retained command entrypoint. Each native runner owns and joins its disposable
// fixture; scenarios are separate so their fault injection cannot leak between
// activity, history, attachment and agent-dock acceptance.
import assert from 'node:assert/strict';
import { join } from 'node:path';
import { mkdir, writeFile } from 'node:fs/promises';

const directory = process.env.WHIP_CHAT_ACTIVITY_RESULTS ?? '/tmp/whip-chat-activity-results';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 3 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox', 'electron'].includes(name)), 'Choose Chromium, Firefox or the staged Electron fixture');
const selected = ['COMPOSER_ONLY', 'MESSAGES_ONLY', 'HISTORY_ONLY', 'AGENTS_ONLY', 'NATIVE_DROP'].filter(name => process.env[`WHIP_CHAT_${name}`] === '1');
assert(selected.length <= 1, 'Select at most one specialized chat acceptance mode');
const mode = selected[0] ?? 'activity';
assert(mode !== 'NATIVE_DROP' || names.length === 1 && names[0] === 'electron', 'Manual Finder drop requires only the staged Electron fixture');
assert(mode !== 'NATIVE_DROP' || process.stdin.isTTY, 'Manual Finder drop requires an interactive terminal');
await mkdir(directory, { recursive: true });
const completed = [];
const run = async (file, variable, name) => {
  process.env[variable] = join(directory, name);
  await import(`./${file}.mjs`);
  completed.push({ name, results: join(directory, name, 'results.json') });
};
if (mode === 'COMPOSER_ONLY' || mode === 'MESSAGES_ONLY') {
  process.env.WHIP_CONTENT_MODE = mode === 'COMPOSER_ONLY' ? 'composer' : 'composer,stored';
  await run('native-content-probes', 'WHIP_CONTENT_RESULTS', 'content');
} else if (mode === 'HISTORY_ONLY') {
  await run('native-history-recovery', 'WHIP_HISTORY_RECOVERY_RESULTS', 'history');
} else if (mode === 'AGENTS_ONLY') {
  await run('native-agent-dock', 'WHIP_AGENT_DOCK_RESULTS', 'agents');
} else {
  await run('native-chat-activity', 'WHIP_CHAT_ACTIVITY_RESULTS', mode === 'NATIVE_DROP' ? 'manual-drop' : 'activity');
  if (mode !== 'NATIVE_DROP') await run('native-history-recovery', 'WHIP_HISTORY_RECOVERY_RESULTS', 'history');
}
await writeFile(join(directory, 'results.json'), JSON.stringify({ mode, surfaces: names, completed,
  limits: ['Synthetic providers and disposable native hosts; no real accounts or installed runtime.',
    'Electron activity uses local native IPC; history/content/agent fault probes use the normal URL connector to a distinct owned gateway.',
    'Finder drag is never automated or claimed by the default gate; its explicit mode requires an observed trusted one-file drop.'] }, null, 2));
