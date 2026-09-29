import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
import { createTraceView } from '../dist/state.js';

const base = 9007199254740993n;
const hex = (value, width) => BigInt(value).toString(16).padStart(width, '0');
const row = (index) => {
  const root = Math.floor(index / 8) * 8;
  return {
    sequence: String(base + BigInt(index)),
    root_id: 'root',
    session_id: index % 8 ? 'child' : 'root',
    turn_id: 'turn_' + root,
    source_kind: index % 8 ? 'attempt' : 'turn',
    source_id: 'source_' + index,
    span_id: hex(index, 16),
    span: {
      trace_id: hex(root, 32),
      parent_span_id: index % 8 ? hex(root, 16) : null,
      kind: index % 8 ? 'llm' : 'agent',
      name: 'span ' + index,
      state: 'succeeded',
      start_ns: String(1790600000000000000n + BigInt(index) * 1000n),
      end_ns: String(1790600000000000000n + BigInt(index) * 1000n),
      attributes: [],
    },
  };
};
async function backend(count = 100) {
  const state = {
    rows: Array.from({ length: count }, (_, i) => row(i + 1)).reverse(),
    calls: [],
    epoch: 'boot',
    runtimeID: 'runtime',
    intercept: undefined,
    mutate: undefined,
  };
  const revision = () => state.rows[0]?.sequence ?? '0';
  const connect = () =>
    Client.connect(
      async (request, options) => {
        if (request.method === 'initialize')
          return {
            jsonrpc: '2.0',
            id: request.id,
            result: {
              major: 4,
              minor: 0,
              runtime_id: state.runtimeID,
              process_epoch: state.epoch,
              network_client: false,
              builtins: [],
            },
          };
        assert.equal(request.method, 'trace.page');
        state.calls.push(request);
        await state.intercept?.(request, options);
        const p = request.params;
        if (p.expected_revision !== null && p.expected_revision !== revision())
          return {
            jsonrpc: '2.0',
            id: request.id,
            error: { code: -32001, kind: 'CONFLICT', message: 'trace changed' },
          };
        const candidates = state.rows.filter(
          (item) => p.before === null || BigInt(item.sequence) < BigInt(p.before),
        );
        const items = [];
        let scanned = 0;
        let next = '0';
        for (const item of candidates) {
          if (scanned === 2048 || items.length === p.limit) break;
          scanned++;
          next = item.sequence;
          if (
            item.span &&
            ((p.trace_id && item.span.trace_id !== p.trace_id) ||
              (p.roots_only && item.span.parent_span_id !== null))
          )
            continue;
          items.push(item);
        }
        const has_more = scanned < candidates.length;
        let page = structuredClone({
          items,
          revision: revision(),
          next: has_more ? next : '0',
          has_more,
        });
        page = state.mutate?.(page, p) ?? page;
        return { jsonrpc: '2.0', id: request.id, result: page };
      },
      { clientID: 'trace-test' },
    );
  return { state, connect, client: await connect() };
}
function viewFor(t, client, options = {}) {
  const view = createTraceView(client, 'root', { pollIntervalMs: 60000, ...options });
  t.after(() => view.dispose());
  return view;
}

test('trace view replaces bounded canonical pages and older polling never jumps to latest', async (t) => {
  const { state, client } = await backend(5000);
  const view = viewFor(t, client, { maxRows: 3, maxRoots: 2 });
  await view.start();
  let snapshot = view.getSnapshot();
  assert.equal(snapshot.status, 'live');
  assert.equal(snapshot.rows.length, 3);
  assert.equal(snapshot.roots.length, 2);
  assert.equal(state.calls.length, 2);
  assert.equal(snapshot.rows[0].sequence, String(base + 5000n));
  assert.equal(snapshot.truncated, true);
  assert.equal(snapshot.latestMissing, false);
  assert.ok(Object.isFrozen(snapshot.rows[0].span));
  const cursor = snapshot.olderCursor;
  await view.loadOlder();
  snapshot = view.getSnapshot();
  assert.equal(snapshot.windowBefore, cursor);
  assert.equal(snapshot.rows[0].sequence, String(base + 4997n));
  state.rows.unshift(row(5001));
  await view.refresh();
  assert.equal(view.getSnapshot().rows[0].sequence, String(base + 4997n));
  assert.equal(view.getSnapshot().latestMissing, true);
  assert.equal(state.calls.at(-2).params.before, cursor);
  await view.latest();
  assert.equal(view.getSnapshot().rows[0].sequence, String(base + 5001n));
  const rootsBefore = view.getSnapshot().olderRootsCursor;
  await view.loadOlderRoots();
  assert.equal(view.getSnapshot().rootsBefore, rootsBefore);
  await view.refresh();
  assert.equal(view.getSnapshot().rootsBefore, rootsBefore);
  await view.latestRoots();
  assert.equal(view.getSnapshot().rootsBefore, null);
});

test('trace selection keeps exact scope while tombstones pass filters and zero-duration spans stay canonical', async (t) => {
  const { state, client } = await backend(100);
  state.rows[0].span = null;
  const view = viewFor(t, client);
  await view.start();
  const traceID = hex(80, 32);
  await view.selectTrace(traceID);
  const snapshot = view.getSnapshot();
  assert.equal(snapshot.traceID, traceID);
  assert.equal(snapshot.rows[0].span, null);
  assert.ok(snapshot.rows.every((item) => item.span === null || item.span.trace_id === traceID));
  const span = snapshot.rows[1].span;
  assert.equal(span.start_ns, span.end_ns);
  assert.equal(state.calls.at(-1).params.trace_id, '');
  assert.equal(state.calls.at(-1).params.expected_revision, snapshot.revision);
  assert.throws(() => view.selectTrace('invalid'), TypeError);
  await view.selectTrace('');
  assert.equal(view.getSnapshot().traceID, '');
});

test('revision conflict never publishes mixed root and row pages and next refresh stays read-only', async (t) => {
  const { state, client } = await backend(100);
  const view = viewFor(t, client);
  await view.start();
  const original = view.getSnapshot();
  state.intercept = (request) => {
    if (request.params.roots_only) state.rows.unshift(row(101));
  };
  await view.refresh();
  assert.equal(view.getSnapshot().status, 'stale');
  assert.equal(view.getSnapshot().error.kind, 'CONFLICT');
  assert.equal(view.getSnapshot().revision, original.revision);
  assert.equal(view.getSnapshot().rows, original.rows);
  state.intercept = undefined;
  await view.refresh();
  assert.equal(view.getSnapshot().status, 'live');
  assert.equal(view.getSnapshot().rows[0].sequence, String(base + 101n));
});

test('empty filtered scan pages retain an explicit exact continuation instead of crawling automatically', async (t) => {
  const { state, client } = await backend(3000);
  const view = viewFor(t, client);
  await view.start();
  const before = state.calls.length;
  await view.selectTrace(hex(8, 32));
  assert.equal(state.calls.length - before, 2);
  assert.equal(view.getSnapshot().rows.length, 0);
  assert.ok(view.getSnapshot().olderCursor);
  const edge = view.getSnapshot().olderCursor;
  await view.loadOlder();
  assert.equal(view.getSnapshot().windowBefore, edge);
  assert.equal(view.getSnapshot().rows.length, 8);
});

test('trace view rejects foreign, unordered, wider, stalled or mixed-revision pages without presenting them', async (t) => {
  for (const mutate of [
    (page) => ({
      ...page,
      items: page.items.map((item, i) => (i === 0 ? { ...item, root_id: 'foreign' } : item)),
    }),
    (page) => ({ ...page, items: [...page.items].reverse() }),
    (page) => ({ ...page, next: page.revision, has_more: true }),
    (page) => ({ ...page, items: [...page.items, page.items[0]] }),
    (page, params) =>
      params.roots_only ? { ...page, revision: String(BigInt(page.revision) + 1n) } : page,
    (page, params) =>
      params.roots_only
        ? {
            ...page,
            items: [
              { ...page.items[0], span: { ...page.items[0].span, parent_span_id: hex(999, 16) } },
            ],
          }
        : page,
  ]) {
    const { state, client } = await backend();
    state.mutate = mutate;
    const view = viewFor(t, client, { maxRows: 4 });
    await view.start();
    assert.equal(view.getSnapshot().status, 'stale');
    assert.equal(view.getSnapshot().unavailable, true);
    assert.deepEqual(view.getSnapshot().rows, []);
  }
});

test('row count and bytes stay bounded for a slow consumer and oversize evidence is explicit', async (t) => {
  const { state, client } = await backend(10000);
  const view = viewFor(t, client, { maxRows: 4, maxRoots: 2, maxBytes: 16384 });
  let notifications = 0;
  const unsubscribe = view.subscribe(() => {
    notifications++;
  });
  await view.start();
  for (let i = 1; i <= 12; i++) {
    state.rows.unshift(row(10000 + i));
    await view.refresh();
  }
  assert.equal(view.getSnapshot().rows.length, 4);
  assert.ok(view.getSnapshot().retainedBytes <= 16384);
  assert.ok(notifications < 30);
  unsubscribe();
  const retained = view.getSnapshot().rows;
  state.mutate = (page, params) =>
    params.roots_only
      ? page
      : {
          ...page,
          items: page.items.map((item) => ({
            ...item,
            span: {
              ...item.span,
              attributes: Array.from({ length: 64 }, (_, i) => ({
                key: 'key' + i,
                text: 'x'.repeat(2048),
                count: null,
                flag: null,
              })),
            },
          })),
        };
  await view.refresh();
  assert.equal(view.getSnapshot().status, 'stale');
  assert.equal(view.getSnapshot().rows, retained);
});

test('suspend joins one read and concurrent refreshes do not queue; restart clears old epoch evidence', async (t) => {
  const { state, client, connect } = await backend();
  const view = viewFor(t, client);
  await view.start();
  let began;
  const started = new Promise((resolve) => {
    began = resolve;
  });
  state.intercept = (_request, { signal }) =>
    new Promise((resolve, reject) => {
      began();
      signal.addEventListener('abort', () => reject(signal.reason), { once: true });
    });
  const before = state.calls.length;
  const pending = view.refresh();
  assert.equal(view.refresh(), pending);
  await started;
  const navigation = view.loadOlder();
  await view.suspend();
  await Promise.all([pending, navigation]);
  assert.equal(state.calls.length - before, 1);
  assert.equal(view.getSnapshot().status, 'suspended');
  await view.refresh();
  assert.equal(state.calls.length - before, 1);
  state.intercept = undefined;
  state.epoch = 'restarted';
  state.rows = [row(200)];
  const snapshots = [];
  const unsubscribe = view.subscribe(() => {
    snapshots.push(view.getSnapshot());
  });
  await view.reconnect(await connect());
  assert.ok(snapshots.some((value) => value.epoch === 'restarted' && value.rows.length === 0));
  assert.equal(view.getSnapshot().epoch, 'restarted');
  assert.equal(view.getSnapshot().rows.length, 1);
  unsubscribe();
  state.runtimeID = 'another';
  await assert.rejects(view.reconnect(await connect()), TypeError);
  await view.dispose();
  assert.equal(view.getSnapshot().status, 'closed');
  assert.equal(view.getSnapshot().rows.length, 0);
  await assert.rejects(view.start());
});
