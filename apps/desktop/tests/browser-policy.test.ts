import assert from 'node:assert/strict';
import test from 'node:test';
import { browserAction, browserID, browserTitle, browserURL, guestResourceAllowed, nativeBounds, presentation, restoreTabs, target } from '../src/browser-policy';

test('browser address admission normalizes human addresses and denies privileged/credential URLs', () => {
  assert.equal(browserURL('localhost:5173/x'), 'http://localhost:5173/x');
  assert.equal(browserURL('127.0.0.1:5173'), 'http://127.0.0.1:5173/');
  assert.equal(browserURL('[::1]:5173'), 'http://[::1]:5173/');
  assert.equal(browserURL('example.org/path'), 'https://example.org/path');
  assert.equal(browserURL('about:blank'), 'about:blank');
  for (const value of ['javascript:alert(1)', 'file:///tmp/private', 'whip-app://bundle/', 'devtools://devtools/', 'chrome://settings', 'data:text/html,secret', 'https://user:secret@example.org', '', 'https://example.org/\nsecret', 'https://example.org/' + 'x'.repeat(8192)]) assert.throws(() => browserURL(value), value);
  for (const value of ['https://site/a.js', 'data:image/png;base64,AA', 'blob:https://site/id', 'wss://site/socket']) assert.equal(guestResourceAllowed(value), true);
  for (const value of ['whip-app://bundle', 'file:///etc/passwd', 'javascript:x', 'chrome://version']) assert.equal(guestResourceAllowed(value), false);
});

test('browser identities, bounded titles and discriminated actions do not trust TypeScript callers', () => {
  assert.equal(browserID('a-b_c12'), 'a-b_c12');
  assert.equal([...browserTitle('🙂'.repeat(200))].length, 128);
  assert.throws(() => browserID('../escape'));
  assert.throws(() => target({ epoch: 'one', tabId: 'id', generation: 'g', extra: true }));
  assert.deepEqual(browserAction({ kind: 'navigate', url: 'example.org' }), { kind: 'navigate', url: 'https://example.org/' });
  for (const action of [{ kind: 'evaluate', code: '1' }, { kind: 'focus', code: '1' }, { kind: 'zoom', factor: Infinity }, { kind: 'zoom', factor: 0 }, { kind: 'devtools', open: 'true' }, { kind: 'reload', ignoreCache: 1 }, { kind: 'find', text: 'x'.repeat(4097) }]) assert.throws(() => browserAction(action));
});

test('presentation validates a complete bounded slot set and rounds CSS edges without DPR', () => {
  const slot = { tabId: 'tab', slotId: 'pane', bounds: { x: 10.25, y: 20.25, width: 301.5, height: 220.5 } };
  const input = { epoch: 'epoch', revision: 1, blocked: false, slots: [slot] };
  assert.deepEqual(presentation(input), input);
  assert.deepEqual(nativeBounds(slot.bounds, 1.25, 1000, 700), { x: 13, y: 25, width: 377, height: 276 });
  assert.deepEqual(nativeBounds({ x: -20, y: 80, width: 80, height: 50 }, 1, 100, 100), { x: 0, y: 80, width: 60, height: 20 });
  for (const value of [{ ...input, revision: 0 }, { ...input, slots: [slot, slot] }, { ...input, slots: Array.from({ length: 5 }, (_, i) => ({ ...slot, tabId: `tab${i}`, slotId: `slot${i}` })) }, { ...input, slots: [{ ...slot, bounds: { ...slot.bounds, x: NaN } }] }, { ...input, slots: [{ ...slot, bounds: { ...slot.bounds, width: -1 } }] }]) assert.throws(() => presentation(value));
});

test('restore retains thirty-two metadata entries, rejects duplicates/overflow/aggregate excess', () => {
  const tabs = Array.from({ length: 32 }, (_, i) => ({ id: `tab${i}`, url: 'about:blank' }));
  assert.equal(restoreTabs({ epoch: 'epoch', tabs }).tabs.length, 32);
  assert.throws(() => restoreTabs({ epoch: 'epoch', tabs: [...tabs, { id: 'extra', url: 'about:blank' }] }));
  assert.throws(() => restoreTabs({ epoch: 'epoch', tabs: [tabs[0], tabs[0]] }));
  assert.throws(() => restoreTabs({ epoch: 'epoch', tabs: tabs.map(tab => ({ ...tab, url: 'https://site/' + 'x'.repeat(3000) })) }));
});
