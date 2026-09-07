import assert from 'node:assert/strict';
import { cp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, run, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

if (process.platform !== 'darwin') throw new Error('Actual Safari application smoke requires macOS. WebKit automation is separate coverage.');
const source = fileURLToPath(new URL('../dist/', import.meta.url));
const resultsDirectory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-web-browser-results';
await mkdir(resultsDirectory, { recursive: true });
const html = await readFile(join(source, 'index.html'), 'utf8');
const script = /<script\b[^>]*type="module"[^>]*src="([^"]+)"[^>]*><\/script>/;
const entry = html.match(script)?.[1];
assert.ok(entry, 'Build the production web application before running its Safari smoke');
const fixture = await startFixture();
const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `safari-app-smoke-${crypto.randomUUID()}`, clientKind: 'human' });
try {
  await client.connect();
  const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
  assert.equal(created.status, 'succeeded');
  const rootId = created.result.root_id;
  await client.session(rootId).rename('Safari application smoke').result();
  const directory = join(fixture.directory, 'public');
  await cp(source, directory, { recursive: true });
  await writeFile(join(directory, 'index.html'), html.replace(script, '<script type="module" src="/safari-application-smoke.js"></script>'));
  const config = { entry, endpoint: fixture.info.endpoint, frontend: fixture.info.frontend, route: `/h/${fixture.info.runtime_id}/s/${rootId}`, rootId };
  await writeFile(join(directory, 'safari-application-smoke.js'), `
const config = ${JSON.stringify(config)};
const failures = [];
document.addEventListener('securitypolicyviolation', event => failures.push(event.violatedDirective + ': ' + event.blockedURI));
window.addEventListener('error', event => failures.push(event.message));
const check = (value, message) => { if (!value) throw new Error(message); };
const until = async (read, message) => {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) { const value = await read(); if (value) return value; await new Promise(resolve => setTimeout(resolve, 30)); }
  throw new Error('Timed out: ' + message);
};
const button = name => [...document.querySelectorAll('button')].find(element => (element.getAttribute('aria-label') ?? element.textContent.trim()) === name);
const inputValue = (element, value) => {
  const prototype = element.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value);
  element.dispatchEvent(new Event('input', { bubbles: true }));
};
async function smoke() {
  const reloaded = localStorage.getItem('whip.safari.smoke.phase') === 'reload';
  localStorage.setItem('whip.web.endpoint', JSON.stringify(config.endpoint));
  if (!reloaded) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'nord' }));
  history.replaceState(null, '', config.route);
  await import(config.entry);
  const input = await until(() => document.querySelector('textarea[data-whip-composer]'), 'application composer');
  await until(() => [...document.querySelectorAll('span')].some(element => element.textContent === 'live'), 'live session');
  if (!reloaded) {
    check(document.documentElement.dataset.theme === 'nord', 'Saved theme was not applied before attachment');
    inputValue(input, 'Safari production app round trip');
    await until(() => !document.querySelector('button[aria-label="Send message"]').disabled, 'enabled submission');
    input.form.requestSubmit();
    await until(() => input.value === '', 'committed acceptance');
    await until(() => [...document.querySelectorAll('[data-message-id]')].filter(element => element.textContent.includes('Safari production app round trip')).length >= 2, 'daemon response in conversation');
    inputValue(input, 'Safari draft survives reload');
    const picker = await until(() => button('nord'), 'theme picker'); picker.click();
    const search = await until(() => document.querySelector('input[role="combobox"]'), 'theme search');
    search.focus(); search.click();
    inputValue(search, 'dark');
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    const option = await until(() => [...document.querySelectorAll('[role="option"]')].find(element => element.textContent.replace(/\\s/g, '') === 'darkDark'), 'dark theme choice');
    option.click();
    await until(() => JSON.parse(localStorage.getItem('whip.appearance.theme.v1')).id === 'dark', 'committed theme');
    button('Done').click();
    await until(() => Object.keys(localStorage).some(key => key.startsWith('whip.web.draft.v1:') && localStorage.getItem(key) === 'Safari draft survives reload'), 'persisted draft');
    localStorage.setItem('whip.safari.smoke.phase', 'reload');
    check(failures.length === 0, JSON.stringify(failures));
    location.assign(config.frontend + '/?safari-smoke=reload');
    return;
  }
  check(input.value === 'Safari draft survives reload', 'Reload lost the application draft');
  check(document.documentElement.dataset.theme === 'dark', 'Reload lost the chosen theme');
  check(failures.length === 0, JSON.stringify(failures));
  const result = { passed: true, browser: 'safari', user_agent: navigator.userAgent, production_bundle: true, real_daemon: true, checks: ['attached root route', 'native form submission and authoritative response', 'theme selection while draft retained', 'full-page reload restores draft and theme', 'strict CSP without inline execution'] };
  await fetch('/result/safari', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(result) });
  document.title = 'WHIP Safari application smoke passed';
}
await smoke().catch(async error => {
  await fetch('/result/safari', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ passed: false, browser: 'safari', user_agent: navigator.userAgent, error: String(error), stack: error.stack, csp: failures, options: [...document.querySelectorAll('[role="option"]')].map(element => element.textContent), inputs: [...document.querySelectorAll('input[role="combobox"]')].map(element => ({ value: element.value, focused: document.activeElement === element })), page: document.body.textContent.slice(-5000) }) });
});
`);
  await run('open', ['-a', 'Safari', fixture.info.frontend + '/?safari-smoke=start']);
  const result = await eventually(async () => JSON.parse(await readFile(join(fixture.directory, 'safari-result.json'), 'utf8')), { timeout: 60_000, description: 'actual Safari application result' });
  await writeFile(join(resultsDirectory, 'safari-report.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
  assert.equal(result.passed, true, 'Actual Safari application smoke failed');
  assert.ok(/Safari\//.test(result.user_agent) && !/Chrome\//.test(result.user_agent));
} finally { client.close(); await fixture.close(); }
