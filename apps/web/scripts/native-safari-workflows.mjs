import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { deadline, eventually } from './native-fixture.mjs';

// Same actual UI actions for native Safari and explicitly labeled contract rehearsals.
export async function safariWorkflows(driver, fixture, directory, report) {
  const client = await fixture.connect('actual-safari-probe');
  const { root } = await fixture.createRoot(client, { title: 'Safari application smoke' });
  const { root: other } = await fixture.createRoot(client, { title: 'Safari second tab' });
  const origin = fixture.info.web;
  const response = await fetch(origin, { signal: AbortSignal.timeout(5000) });
  report.cspHeader = response.headers.get('content-security-policy'); await response.body.cancel();
  assert(report.cspHeader?.includes("script-src 'self'") && !report.cspHeader.includes("'unsafe-inline'"));
  const navigate = async path => {
    assert(path.startsWith('/') && !path.startsWith('//'));
    await driver.call('POST', '/url', { url: origin + path });
  };
  const evaluate = driver.evaluate;
  const until = (callback, ...args) => eventually(() => evaluate(callback, ...args), { timeout: 15000, interval: 75, description: callback.toString().slice(0, 180) });
  const click = async (selector, text) => {
    await until((selector, text) => [...document.querySelectorAll(selector)].some(element => !element.disabled && (text === null || (element.getAttribute('aria-label') ?? element.textContent.trim()) === text)), selector, text ?? null);
    await driver.click(selector, text ?? null);
  };
  const input = 'textarea[aria-label="Message WHIP"]';
  const type = value => evaluate((selector, value) => {
    const element = document.querySelector(selector); if (!element || element.disabled) throw new Error('Composer unavailable'); element.focus();
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  }, input, value);
  const ready = async () => {
    await until(() => document.querySelector('[data-startup-phase="visible"]') && document.querySelector('textarea[aria-label="Message WHIP"]:not(:disabled)'));
    await evaluate(() => {
      window.__nativeSafariErrors ??= [];
      const save = value => { if (window.__nativeSafariErrors.length < 64) window.__nativeSafariErrors.push(String(value).slice(0, 2048)); };
      window.addEventListener('error', event => save(event.message));
      window.addEventListener('unhandledrejection', event => save(event.reason));
      document.addEventListener('securitypolicyviolation', event => save(event.violatedDirective + ': ' + event.blockedURI));
    });
  };
  const errors = async () => assert.deepEqual(await evaluate(() => window.__nativeSafariErrors), []);
  const workspace = () => evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
  const tabs = node => node.type === 'pane' ? node.tabs : [...tabs(node.first), ...tabs(node.second)];
  const findTab = async owner => { const value = tabs((await workspace()).layout).find(tab => tab.kind === 'chat' && tab.rootId === owner); assert(value); return value.id; };
  const tabSelector = id => `[role="tab"][id=${JSON.stringify('whip-workspace-tab-' + encodeURIComponent(id))}]`;
  await navigate('/favicon.svg');
  await evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'nord' })));
  await navigate(`/h/${fixture.info.runtime_id}/s/${root.id}`); await ready();
  assert.equal(await evaluate(() => document.documentElement.dataset.theme), 'nord');
  report.userAgent = await evaluate(() => navigator.userAgent);
  report.checks.push('untouched native production CSP, saved theme selected before app attachment');
  const firstID = await findTab(root.id);
  await type('Safari production app round trip'); await click('button', 'Send message');
  await until(selector => document.querySelector(selector)?.value === '', input);
  const completed = await eventually(async () => {
    const page = await client.session(root.id).turns.page({ limit: 1 }, deadline()); return page.items[0]?.state === 'succeeded' ? page.items[0] : false;
  });
  const history = await client.session(root.id).history.page({ direction: 'backward', limit: 100 }, deadline());
  const canonical = history.messages.filter(message => message.parts.some(part => part.type === 'text' && part.text.includes('Safari production app round trip')));
  assert.equal(canonical.length, 2); assert.equal(completed.session_id, root.id);
  await until(() => [...document.querySelectorAll('[data-message-id]')].filter(element => element.textContent.includes('Safari production app round trip')).length >= 2);
  const effects = await fixture.effects(); assert.equal(effects.length, 1);
  report.checks.push('native form submission accepted once, exact root turn succeeded, both canonical user and assistant rows render');
  await type('Safari draft survives reload');
  await click('a', 'Safari second tab'); await until(id => location.pathname.endsWith('/s/' + id), other.id);
  await until(selector => document.querySelector(selector)?.value === '', input);
  await type('Independent Safari tab draft'); const otherID = await findTab(other.id);
  await click(tabSelector(firstID)); await until(selector => document.querySelector(selector)?.value === 'Safari draft survives reload', input);
  await click(`[data-workspace-tab=${JSON.stringify(firstID)}] button[aria-label^="Close "]`);
  await until(selector => !document.querySelector(selector), tabSelector(firstID)); await click('button', 'Reopen');
  await until(selector => document.querySelector(selector)?.value === 'Safari draft survives reload', input);
  assert.equal(await findTab(root.id), firstID);
  report.checks.push('two actual root tabs retain independent drafts; close and explicit Reopen preserve exact view identity');
  await click('#whip-settings-link'); await click('button', 'Appearance');
  await click('button[aria-label^="Color theme:"]');
  await until(() => [...document.querySelectorAll('[role="option"]')].some(element => element.textContent.replace(/\s/g, '') === 'darkDark'));
  await evaluate(() => [...document.querySelectorAll('[role="option"]')].find(element => element.textContent.replace(/\s/g, '') === 'darkDark').click());
  await until(() => JSON.parse(localStorage.getItem('whip.appearance.theme.v1')).id === 'dark'); await click('button', 'Back to workspace');
  await click(tabSelector(firstID)); await until(selector => document.querySelector(selector)?.value === 'Safari draft survives reload', input);
  await until(() => Object.keys(localStorage).some(key => key.startsWith('whip.web.draft.v1:') && localStorage.getItem(key) === 'Safari draft survives reload'));
  await errors(); report.checks.push('real settings theme selection preserves the open draft and does not submit');
  await driver.call('POST', '/refresh', {}); await ready();
  const restored = await workspace(); assert.equal(tabs(restored.layout).filter(tab => tab.kind === 'chat').length, 2);
  assert(tabs(restored.layout).some(tab => tab.id === firstID) && tabs(restored.layout).some(tab => tab.id === otherID));
  assert.equal(await evaluate(selector => document.querySelector(selector).value, input), 'Safari draft survives reload');
  assert.equal(await evaluate(() => document.documentElement.dataset.theme), 'dark');
  await click(tabSelector(otherID)); await until(selector => document.querySelector(selector)?.value === 'Independent Safari tab draft', input);
  await click(tabSelector(firstID)); await errors(); assert.deepEqual(await fixture.effects(), effects);
  report.checks.push('full-page reload restores actual native layout, both drafts and chosen theme, without provider replay');
  const screenshot = await driver.call('GET', '/screenshot'); assert(typeof screenshot === 'string' && screenshot.length < 8 << 20);
  await writeFile(join(directory, report.browser === 'actual Safari' ? 'safari.png' : 'rehearsal.png'), Buffer.from(screenshot, 'base64'));
}
