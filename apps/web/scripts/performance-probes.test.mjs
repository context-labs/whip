import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { installPerformanceProbes } from './performance-probes.mjs';

function fixture(desktop = false) {
  let now = 0, mounted = '', mutate, desktopEvent;
  class Socket {
    addEventListener(_name, callback) { this.receive = callback; }
  }
  const window = {
    WebSocket: Socket,
    addEventListener() {},
    whipDesktop: { onEvent(callback) { desktopEvent = callback; return () => {}; } },
  };
  // Exercise the same self-contained serialization used by page.addInitScript.
  const install = vm.runInNewContext(`(${installPerformanceProbes.toString()})`, {
    window,
    performance: { now: () => now },
    document: { querySelectorAll: () => [{ textContent: mounted }] },
    MutationObserver: class { constructor(callback) { mutate = callback; } observe() {} },
  });
  install({ desktop, rootId: 'root' });
  const socket = desktop ? undefined : new window.WebSocket();
  return {
    receive(message, at) {
      now = at;
      const data = JSON.stringify(message);
      if (desktop) desktopEvent({ kind: 'frame', frame: data });
      else socket.receive({ data });
    },
    paint(text, at) { mounted = text; now = at; mutate(); },
    commits: () => JSON.parse(JSON.stringify(window.__performanceCommitDOM)),
    deltas: () => Array.from(window.__performanceEventLatency),
    overflow: () => window.__performanceProbeOverflow,
  };
}

const item = (seq = '910', text = ' commit-probe-001', kind = 'stream.text') => ({ seq, kind, payload: { text } });
const snapshot = (events, root = 'root') => ({ result: { root_id: root, cursor: '9223372036854775807', presentation: events } });
const notification = (event, root = 'root') => ({ params: { event: { ...event, root_id: root } } });

test('snapshot delivery measures each event sequence and its actual receipt and DOM times', async t => {
  for (const desktop of [false, true]) {
    await t.test(desktop ? 'desktop frames' : 'WebSocket messages', () => {
      const f = fixture(desktop);
      f.receive(snapshot([item()]), 10);
      f.paint('different live row', 20);
      assert.deepEqual(f.commits(), []);
      f.paint('live commit-probe-001', 30);
      assert.deepEqual(f.commits(), [{ sequence: '910', marker: ' commit-probe-001', received_ms: 10, browser_ms: 30 }]);
      assert.deepEqual(f.deltas(), []);
    });
  }
  await t.test('40 mixed-route markers keep exact sequence identities distinct from the snapshot cursor', () => {
    const f = fixture();
    const expected = [];
    let live = 'live';
    for (let i = 1; i <= 40; i++) {
      const sequence = String(9007199254740993n + BigInt(i));
      const marker = ' commit-probe-' + String(i).padStart(3, '0');
      const event = item(sequence, marker);
      f.receive(i % 2 ? snapshot([event]) : notification(event), i * 10);
      f.receive(snapshot([event]), i * 10 + 1);
      live += marker;
      f.paint(live, i * 10 + 5);
      expected.push({ sequence, marker, received_ms: i * 10, browser_ms: i * 10 + 5 });
    }
    assert.deepEqual(f.commits(), expected);
    assert.equal(new Set(f.commits().map(value => value.sequence)).size, 40);
    assert.equal(f.overflow(), false);
  });
});

test('duplicate commit deliveries retain the first receipt before and after DOM observation', async t => {
  for (const first of ['notification', 'snapshot']) {
    await t.test(`${first} first`, () => {
      const f = fixture();
      const messages = [notification(item()), snapshot([item()])];
      if (first === 'snapshot') messages.reverse();
      f.receive(messages[0], 10);
      f.receive(messages[1], 20);
      f.paint('live commit-probe-001', 30);
      f.receive(messages[0], 40);
      f.receive(messages[1], 50);
      f.paint('live commit-probe-001 extended', 60);
      assert.deepEqual(f.commits(), [{ sequence: '910', marker: ' commit-probe-001', received_ms: 10, browser_ms: 30 }]);
    });
  }
});

test('unrelated roots and snapshot deltas do not enter the measured populations', () => {
  const f = fixture();
  f.receive(snapshot([item()], 'other-root'), 1);
  f.receive(notification(item(), 'other-root'), 2);
  f.receive({ result: { root_id: 'root', presentation: [], agent_presentations: { child: [item()] } } }, 3);
  f.receive(snapshot([item('911', ' commit-probe-002', 'stream.reasoning')]), 4);
  f.receive(snapshot([item('912', ' **delta-000**')]), 5);
  f.paint('live commit-probe-001 commit-probe-002 delta-000', 10);
  assert.deepEqual(f.commits(), []);
  assert.deepEqual(f.deltas(), []);
  f.receive(notification(item('913', ' **delta-001**')), 20);
  f.paint('live delta-001', 35);
  assert.deepEqual(f.deltas(), [15]);
});

test('commit deduplication and pending receipts remain bounded and flag overflow', () => {
  const commits = fixture();
  const events = Array.from({ length: 129 }, (_, i) => item(String(i + 1), ' commit-probe-' + String(i + 1).padStart(3, '0')));
  commits.receive(snapshot(events), 10);
  assert.equal(commits.overflow(), true);
  commits.paint(events.map(event => event.payload.text).join(''), 20);
  assert.equal(commits.commits().length, 128);

  const pending = fixture();
  for (let i = 0; i < 513; i++) pending.receive(notification(item(String(i), ` **delta-${i}**`)), i);
  assert.equal(pending.overflow(), true);
});
