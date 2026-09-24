import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { writeFile } from 'node:fs/promises';
import { get as httpGet } from 'node:http';
import { createWhipClient, webSocket } from '../dist/index.js';
import { unixSocket } from '../dist/node.js';
import { createSessionView, executionRows } from '../dist/state.js';
import { eventually, startFixture } from '../scripts/fixture.mjs';

let fixture;
let reconnectRecoveryMs;
const clients = new Set();
before(async () => { fixture = await startFixture(); });
after(async () => {
  for (const client of clients) client.close();
  await fixture?.close();
});

async function connect(transport, clientId = crypto.randomUUID(), options = {}) {
  const client = createWhipClient({
    endpoint: transport === 'unix' ? unixSocket(fixture.info.socket) : fixture.info.endpoint,
    clientId, clientKind: 'automation', ...options,
  });
  clients.add(client);
  await client.connect();
  return client;
}

const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
test('an external fixture permits only its exact proxy Host and Origin alongside loopback', async () => {
  const externalOrigin = 'https://whip.example.ts.net:8443';
  const discoveryURL = target => target.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '/api/v3/web');
  const discover = (target, origin) => fetch(discoveryURL(target), {
    headers: { Origin: origin }, signal: AbortSignal.timeout(5_000),
  });
  const requestHost = (target, host, origin) => new Promise((resolve, reject) => {
    httpGet(discoveryURL(target), {
      headers: { Host: host, ...(origin ? { Origin: origin } : {}) }, signal: AbortSignal.timeout(5_000),
    }, response => {
      response.resume();
      response.once('end', () => resolve(response.statusCode));
      response.once('error', reject);
    }).once('error', reject);
  });
  assert.equal((await discover(fixture, externalOrigin)).status, 403);
  assert.equal(await requestHost(fixture, new URL(externalOrigin).host), 403);
  const configured = await startFixture({ externalOrigin });
  try {
    assert.equal(new URL(configured.info.endpoint).hostname, '127.0.0.1');
    for (const origin of [externalOrigin, configured.info.frontend]) {
      const response = await discover(configured, origin);
      assert.equal(response.status, 200);
      assert.equal(response.headers.get('Access-Control-Allow-Origin'), origin);
      await response.arrayBuffer();
    }
    for (const origin of ['https://other.example.ts.net:8443', 'http://whip.example.ts.net:8443', 'https://whip.example.ts.net']) {
      assert.equal((await discover(configured, origin)).status, 403);
    }
    const externalHost = new URL(externalOrigin).host;
    for (const origin of [undefined, externalOrigin]) {
      assert.equal(await requestHost(configured, externalHost, origin), 200);
      assert.equal(await requestHost(configured, new URL(configured.info.endpoint).host, origin), 200);
      assert.equal(await requestHost(configured, 'other.example.ts.net:8443', origin), 403);
      assert.equal(await requestHost(configured, 'whip.example.ts.net', origin), 403);
    }
    assert.equal(await requestHost(configured, externalHost, 'https://other.example.ts.net:8443'), 403);
  } finally { await configured.close(); }
});

async function createRoot(client) {
  const outcome = await client.submit('session.create', {
    kind: 'agent', cwd: fixture.directory, model: 'model', provider: 'provider',
  }).result(deadline());
  assert.equal(outcome.status, 'succeeded');
  return outcome.result.root_id;
}

for (const transport of ['unix', 'websocket']) {
  test(`${transport}: model accounting snapshots preserve scope and exact decimal counters`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    // The fixture runner makes no provider requests, so its authoritative
    // accounting is a known empty summary, distinct from an omitted summary.
    const command = await client.session(rootId).submit({ text: 'Initialize accounting inspection' }).result(deadline());
    assert.equal(command.status, 'succeeded');
    const view = createSessionView(client.session(rootId));
    const stop = view.subscribe(() => {});
    try {
      await view.start();
      const tree = view.getSnapshot().root.accounting;
      assert.equal(tree.root_id, rootId);
      assert.equal(tree.agent_id, rootId);
      assert.equal(tree.scope, 'subtree');
      for (const key of ['revision', 'reported_cost_micros', 'estimated_cost_micros', 'reported_cost_calls', 'estimated_cost_calls', 'unknown_cost_calls', 'reported_calls', 'estimated_calls', 'pending_calls']) {
        assert.match(tree[key], /^\d+$/);
      }
      assert.equal(tree.reported_cost_calls, '0');
      assert.equal(tree.unknown_cost_calls, '0');
      const inspection = await client.session(rootId).agents.inspect(rootId);
      assert.equal(inspection.result.accounting.root_id, rootId);
      assert.equal(inspection.result.accounting.agent_id, rootId);
      assert.equal(inspection.result.accounting.scope, 'agent');
      assert.equal(inspection.result.accounting.reported_cost_micros, '0');
      assert.equal(view.getSnapshot().root.accounting.scope, 'subtree', 'explicit agent inspection never replaces the tree summary');
    } finally { stop(); await view.dispose(); client.close(); }
  });

  test(`${transport}: uploaded attachments resolve on the host without embedding bodies in requests`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    assert.ok(client.requireConnected().capabilities.includes('input_attachments'));
    const body = 'Explicit model input; @unexpanded.txt and $not-a-skill stay literal.';
    const reference = await client.upload(new TextEncoder().encode(body), { rootId, mediaType: 'text/plain' });
    const attachment = reference.asAttachment('text', 'context.txt');
    assert.ok(!JSON.stringify(attachment).includes(body));
    const outcome = await client.session(rootId).submit({ text: 'Inspect this context', attachments: [attachment] }).result(deadline());
    assert.equal(outcome.status, 'succeeded');
    const resolved = JSON.parse(outcome.result.text);
    assert.equal(resolved.text, 'Inspect this context');
    assert.equal(resolved.parts[0].type, 'text');
    assert.equal(resolved.parts[0].text, `Attachment "context.txt":\n${body}`);
    const page = await client.session(rootId).history.page();
    assert.ok(Array.isArray(page.messages[0].message.content));
    assert.equal(page.messages[0].message.content[1].text, resolved.parts[0].text);

    const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aX1cAAAAASUVORK5CYII=', 'base64');
    const image = await client.upload(png, { rootId, mediaType: 'image/png' });
    const imageResult = await client.session(rootId).submit({ text: 'Inspect the image', attachments: [image.asAttachment('image', 'pixel.png')] }).result(deadline());
    assert.equal(imageResult.status, 'succeeded', JSON.stringify(imageResult));
    const imageSnapshot = await client.session(rootId).snapshot();
    const imageMessage = imageSnapshot.messages.find(message => Array.isArray(message.content) && message.content.some(part => part.type === 'image_url'));
    assert.equal(imageMessage.content[1].w, 1);
    assert.equal(imageMessage.content[1].image_url.url, `data:image/png;base64,${png.toString('base64')}`);
    const view = createSessionView(client.session(rootId));
    const stopView = view.subscribe(() => {});
    try {
      await view.start();
      await eventually(() => view.getSnapshot().history[rootId]?.messages.some(entry => Array.isArray(entry.message?.content) && entry.message.content.some(part => part.type === 'image_url')));
      assert.equal(view.getSnapshot().status, 'live');
      assert.equal(client.getSnapshot().state, 'connected');
    } finally {
      stopView();
      await view.dispose();
    }
    const other = await createRoot(client);
    await assert.rejects(client.session(other).submit({ text: 'wrong root', attachments: [attachment] }).accepted(deadline()), error => error.kind === 'permission_denied');
    await assert.rejects(client.session(rootId).submit({ text: 'altered size', attachments: [{ ...attachment, content: { ...attachment.content, size: '1' } }] }).accepted(deadline()), error => error.kind === 'invalid_arguments');
    client.close();
  });

  test(`${transport}: built host helpers expose directories, themes, attention, and mailbox inspection`, async () => {
    const client = await connect(transport);
    const folders = await client.host.directories({ path: fixture.directory });
    assert.ok(folders.entries.some(entry => entry.name === 'public'));
    const themes = await client.host.themes.list();
    assert.ok(themes.themes.some(theme => theme.id === 'opencode'));
    assert.equal((await client.host.themes.resolve('dark')).dark, true);
    const imported = await client.host.themes.resolveJSON(JSON.stringify({ name: 'test-import', dark: false, palette: { primary: '#123456' } }));
    assert.equal(imported.colors.primary, '#123456');
    await assert.rejects(client.host.themes.resolve('../config.json'));
    const rootId = await createRoot(client);
    const mailbox = await client.session(rootId).mailbox.list();
    assert.deepEqual(mailbox.items, []);
    await assert.rejects(client.session(rootId).mailbox.read('missing'));
    const sessions = await client.sessions.list({ search: fixture.directory });
    assert.ok(sessions.items.some(item => item.id === rootId && /^[a-f0-9]{64}$/.test(item.workspace_id)));
    const attention = await client.host.attention();
    assert.ok(Array.isArray(attention.items));
    client.close();
  });

  test(`${transport}: commands, deduplication, history, and structured errors`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const commandId = crypto.randomUUID();
    const command = client.submit('submit', { text: `${transport} round trip` }, { rootId, commandId });
    const accepted = await command.accepted(deadline());
    assert.match(accepted.ingress_seq, /^\d+$/);
    const result = await command.result(deadline());
    assert.equal(result.status, 'succeeded');
    assert.deepEqual(result.result, { text: `${transport} round trip` });
    const duplicate = await client.submit('submit', { text: `${transport} round trip` }, { rootId, commandId }).result(deadline());
    assert.equal(duplicate.ingress_seq, accepted.ingress_seq);
    const changed = client.submit('submit', { text: 'changed request' }, { rootId, commandId });
    await assert.rejects(changed.accepted(deadline()), error => error.kind === 'conflict');
    await assert.rejects(client.call('command.status', { command_id: 'not-present' }), error => error.kind === 'command_not_found');
    const snapshot = await client.call('root.snapshot', { root_id: rootId });
    assert.equal(snapshot.messages.length, 2);
    assert.equal(typeof snapshot.cursor, 'string');
    const history = await client.call('history.page', {
      root_id: rootId, agent_id: rootId, through_seq: -1, limit: 128, max_bytes: 524_288,
    });
    assert.equal(history.messages.length, 2);
    assert.equal(typeof history.history_revision, 'string');
    const replay = await client.call('events.replay', { root_id: rootId, cursor: '0', limit: 1000 });
    assert.equal(replay.latest, snapshot.cursor);
    await assert.rejects(client.call('events.replay', { root_id: rootId, cursor: (BigInt(replay.latest) + 1n).toString(), limit: 100 }), error => error.kind === 'resynchronization_required');
    assert.equal((await fixture.effects()).filter(value => value === `${transport} round trip`).length, 1);
    client.close();
  });

  test(`${transport}: detaching and aborting a waiter do not cancel accepted work`, async () => {
    const clientId = crypto.randomUUID();
    const client = await connect(transport, clientId);
    const rootId = await createRoot(client);
    const key = crypto.randomUUID();
    const commandId = crypto.randomUUID();
    const command = client.submit('submit', { text: `hold:${key}` }, { rootId, commandId });
    await command.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes(`hold:${key}`));
    const controller = new AbortController();
    const waiting = command.result({ signal: controller.signal });
    controller.abort();
    await assert.rejects(waiting);
    assert.equal((await command.status()).status, 'running');
    client.close();
    const opposite = transport === 'unix' ? 'websocket' : 'unix';
    const reattached = await connect(opposite, clientId);
    assert.equal((await reattached.call('command.status', { command_id: commandId })).status, 'running');
    await fixture.release(key);
    const result = await eventually(async () => {
      const status = await reattached.call('command.status', { command_id: commandId });
      return status.status === 'succeeded' && status;
    });
    assert.equal(result.result.text, `hold:${key}`);
    assert.equal((await fixture.effects()).filter(value => value === `hold:${key}`).length, 1);
    reattached.close();
  });

  test(`${transport}: real scratch failures retain execution evidence through durable history and replay`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const view = createSessionView(client.session(rootId));
    const release = view.subscribe(() => {});
    const assertCell = snapshot => {
      const rows = executionRows(snapshot, rootId);
      const cell = rows.find(row => row.kind === 'cell' && row.callId === 'scratch-result-cell');
      assert.ok(cell);
      assert.equal(cell.status, 'failed');
      assert.equal(cell.output, '43\n');
      assert.ok(cell.steps > 0);
      assert.match(cell.error, /cell failed/);
      assert.match(cell.scratch, /Scratch checkpoint failed/);
      assert.match(cell.scratch, /Do not replay/);
      assert.ok(rows.some(row => row.kind === 'restart' && row.text === 'Restarted · restored 1 · 1 skipped'));
    };
    try {
      await view.start();
      const outcome = await client.session(rootId).submit({ text: 'scratch-result' }).result(deadline());
      assert.equal(outcome.status, 'succeeded');
      await eventually(() => { assertCell(view.getSnapshot()); return true; });
      const events = await client.call('events.replay', { root_id: rootId, cursor: '0', limit: 1000 });
      assert.ok(events.events.some(event => event.kind === 'stream.tool.completed' && event.payload.result.includes('Scratch checkpoint failed')));
      const replay = createSessionView(client.session(rootId));
      const stop = replay.subscribe(() => {});
      try {
        await replay.start();
        await eventually(() => { assertCell(replay.getSnapshot()); return true; });
        assert.ok(replay.getSnapshot().history[rootId].messages.some(item => item.message?.role === 'tool' && item.message.content.includes('Scratch checkpoint failed')));
      } finally { stop(); await replay.dispose(); }
    } finally { release(); await view.dispose(); client.close(); }
  });

  test(`${transport}: stale cancellation cannot cancel the next turn`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const firstId = crypto.randomUUID();
    await client.submit('submit', { text: 'completed turn' }, { rootId, commandId: firstId }).result(deadline());
    const key = crypto.randomUUID();
    const current = client.submit('submit', { text: `hold:${key}` }, { rootId });
    await current.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes(`hold:${key}`));
    const stale = await client.submit('cancel', { target_command_id: firstId }, { rootId }).result(deadline());
    assert.notEqual(stale.status, 'succeeded');
    assert.equal((await current.status()).status, 'running');
    await fixture.release(key);
    assert.equal((await current.result(deadline())).status, 'succeeded');
    client.close();
  });

  test(`${transport}: actual lost acceptance acknowledgement recovers by identity`, async () => {
    const factory = transport === 'unix' ? unixSocket(fixture.info.socket) : webSocket(fixture.info.endpoint);
    let discarded = false;
    let submissionCount = 0;
    const endpoint = async (handlers, signal) => {
      let acceptedRequestId;
      const connection = await factory({
        ...handlers,
        message(message) {
          const envelope = JSON.parse(message);
          if (!discarded && envelope.id === acceptedRequestId && envelope.result) {
            discarded = true;
            connection.close();
          } else handlers.message(message);
        },
      }, signal);
      return {
        ...connection,
        get bufferedAmount() { return connection.bufferedAmount; },
        send(message) {
          const envelope = JSON.parse(message);
          if (envelope.method === 'command.submit' && envelope.params.operation === 'submit') {
            acceptedRequestId = envelope.id;
            submissionCount++;
          }
          connection.send(message);
        },
      };
    };
    const client = await connect(transport, crypto.randomUUID(), { endpoint });
    const rootId = await createRoot(client);
    const text = 'lost acknowledgement ' + crypto.randomUUID();
    const command = client.submit('submit', { text }, { rootId });
    await command.accepted(deadline()).catch(error => assert.equal(error.kind, 'delivery_uncertain'));
    await eventually(() => client.getSnapshot().state === 'connected');
    assert.equal((await command.result(deadline())).status, 'succeeded');
    assert.ok(discarded);
    assert.equal(submissionCount, 1, 'An accepted command must be looked up, not submitted again');
    assert.equal((await fixture.effects()).filter(value => value === text).length, 1);
    client.close();
  });

  test(`${transport}: slow subscriptions fail explicitly without killing the connection`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const snapshot = await client.call('root.snapshot', { root_id: rootId });
    const stream = await client.events.subscribe(rootId, snapshot.cursor, { maxMessages: 1 });
    let seen = 0;
    const off = client.onEvent(event => { if (event.root_id === rootId) seen++; });
    try {
      assert.equal((await client.session(rootId).submit({ text: 'overflow stream' }).result(deadline())).status, 'succeeded');
      await eventually(() => seen >= 2);
      await assert.rejects(stream.next(), error => error.kind === 'resynchronization_required');
      assert.equal(typeof (await client.call('daemon.ping', {})).generation, 'string');
      const fresh = await client.call('root.snapshot', { root_id: rootId });
      const replacement = await client.events.subscribe(rootId, fresh.cursor);
      await replacement.dispose();
    } finally { off(); await stream.dispose(); client.close(); }
  });

  test(`${transport}: bounded content crosses chunks and preserves root grants`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const otherRoot = await createRoot(client);
    const data = new TextEncoder().encode('content '.repeat(45_000));
    const content = await client.upload(data, { rootId, mediaType: 'text/plain' });
    assert.equal(content.handle.size, String(data.byteLength));
    assert.deepEqual(await content.readBytes({ maxBytes: data.byteLength }), data);
    await assert.rejects(content.readBytes({ maxBytes: 1024 }));
    await assert.rejects(client.content(content.handle, { rootId: otherRoot }).readBytes({ maxBytes: data.byteLength }));
    client.close();
  });

  test(`${transport}: fixture questions return single and batched answers to both clients`, async () => {
    const client = await connect(transport);
    const observer = await connect(transport === 'unix' ? 'websocket' : 'unix', crypto.randomUUID(), { clientKind: 'human' });
    const rootId = await createRoot(client);
    const views = [client, observer].map(connection => createSessionView(connection.session(rootId)));
    try {
      await Promise.all(views.map(view => view.start()));
      for (const kind of ['single', 'batch']) {
        const command = client.session(rootId).submit({ text: `question:${kind}` });
        await command.accepted(deadline());
        const question = await eventually(() => views[0].getSnapshot().root.questions?.[0]);
        await eventually(() => views[1].getSnapshot().root.questions?.some(item => item.question_id === question.question_id));
        assert.equal(question.questions.length, kind === 'batch' ? 3 : 1);
        assert.equal(question.questions[0].options[0].recommended, true);
        const attention = await observer.host.attention();
        assert.ok(attention.items.some(item => item.root_id === rootId && item.questions?.some(item => item.question_id === question.question_id)));
        const expected = [{ answer: ['Proceed'] }];
        if (kind === 'batch') {
          assert.equal(question.questions[1].multiple, true);
          expected.push({ answer: ['Web', 'Mobile', 'Custom fixture answer'] }, { dismissed: true });
        }
        const answer = kind === 'batch'
          ? observer.session(rootId).answerQuestions(question.question_id, [expected[0], expected[1], null])
          : observer.session(rootId).answerQuestion(question.question_id, ['Proceed']);
        assert.equal((await answer.result(deadline())).status, 'succeeded');
        const outcome = await command.result(deadline());
        assert.equal(outcome.status, 'succeeded');
        assert.deepEqual(JSON.parse(outcome.result.text), expected);
        await eventually(() => views.every(view => !view.getSnapshot().root.questions?.some(item => item.question_id === question.question_id)));
        const late = await client.session(rootId).answerQuestion(question.question_id, ['Wait']).result(deadline());
        assert.equal(late.status, 'failed', 'a second client cannot replace the accepted answer');
      }
    } finally {
      await Promise.all(views.map(view => view.dispose()));
      observer.close(); client.close();
    }
  });

  test(`${transport}: unsigned permission decisions preserve scope, deduplication, and competing-client convergence`, async () => {
    const client = await connect(transport);
    const observer = await connect(transport === 'unix' ? 'websocket' : 'unix', crypto.randomUUID(), { clientKind: 'human' });
    const rootId = await createRoot(client);
    const otherRoot = await createRoot(client);
    const views = [client, observer].map(connection => createSessionView(connection.session(rootId)));
    try {
      const mode = client.permissions.setMode(rootId, false, { commandId: 'mode <tag> & café ' + crypto.randomUUID() });
      assert.equal((await mode.result(deadline())).status, 'succeeded');
      assert.equal((await client.recover(mode.record).result(deadline())).status, 'succeeded');
      assert.equal((await observer.permissions.setMode(rootId, true).result(deadline())).status, 'succeeded');
      await Promise.all(views.map(view => view.start()));
      for (const allow of [true, false]) {
        const command = client.session(rootId).submit({ text: 'permission:' + crypto.randomUUID() });
        await command.accepted(deadline());
        const permission = await eventually(() => views[0].getSnapshot().root.permissions?.find(item => item.status === 'pending'));
        const attention = await observer.host.attention();
        assert.ok(attention.items.some(item => item.root_id === rootId && item.pending_permissions === '1'));
        await eventually(() => views[1].getSnapshot().root.permissions?.some(item => item.id === permission.id && item.status === 'pending'));
        await assert.rejects(observer.permissions.decide({ root_id: otherRoot, permission_id: permission.id, allow }));
        const decision = { root_id: rootId, permission_id: permission.id, allow, command_id: crypto.randomUUID(), reason: 'Reviewed <tag> & café' };
        const accepted = await observer.permissions.decide(decision);
        assert.deepEqual(await observer.permissions.decide(decision), accepted);
        await assert.rejects(observer.permissions.decide({ ...decision, allow: !allow }), error => error.kind === 'conflict');
        await assert.rejects(client.permissions.decide({ ...decision, command_id: crypto.randomUUID(), allow: !allow }));
        assert.equal((await command.result(deadline())).status, allow ? 'succeeded' : 'failed');
        await eventually(() => views.every(view => !view.getSnapshot().root.permissions?.some(item => item.id === permission.id && item.status === 'pending')));
      }
      const racing = client.session(rootId).submit({ text: 'permission:' + crypto.randomUUID() });
      await racing.accepted(deadline());
      const permission = await eventually(() => views[0].getSnapshot().root.permissions?.find(item => item.status === 'pending'));
      const results = await Promise.allSettled([
        client.permissions.decide({ root_id: rootId, permission_id: permission.id, allow: true }),
        observer.permissions.decide({ root_id: rootId, permission_id: permission.id, allow: false }),
      ]);
      assert.equal(results.filter(result => result.status === 'fulfilled').length, 1);
      assert.equal((await racing.result(deadline())).status, results[0].status === 'fulfilled' ? 'succeeded' : 'failed');
      await eventually(() => views.every(view => !view.getSnapshot().root.permissions?.some(item => item.id === permission.id && item.status === 'pending')));
    } finally {
      await Promise.all(views.map(view => view.dispose()));
      observer.close(); client.close();
    }
  });

  test(`${transport}: view converges, shares a subscription, and invalidates destructive history`, async () => {
    const writer = await connect(transport);
    const rootId = await createRoot(writer);
    const observer = await connect(transport === 'unix' ? 'websocket' : 'unix');
    const view = createSessionView(observer.session(rootId), { maxBytes: 16_384, maxMessages: 4 });
    const unsubscribeA = view.subscribe(() => {});
    const unsubscribeB = view.subscribe(() => {});
    try {
      await view.start();
      assert.equal(view.getSnapshot().status, 'live');
      assert.strictEqual(view.getSnapshot(), view.getSnapshot());
      assert.ok(Object.isFrozen(view.getSnapshot()));
      const extra = [];
      try {
        for (let index = 0; index < 15; index++) extra.push(await observer.events.subscribe(rootId, view.getSnapshot().root.cursor));
        await assert.rejects(observer.events.subscribe(rootId, view.getSnapshot().root.cursor), error => error.kind === 'resource_limit');
      } finally {
        await Promise.all(extra.map(stream => stream.dispose()));
      }
      const key = crypto.randomUUID();
      const command = writer.session(rootId).submit({ text: `hold:${key}` });
      await command.accepted(deadline());
      await eventually(() => view.getSnapshot().root.presentation?.some(event => event.kind === 'stream.text' && event.payload.text === `hold:${key}`));
      await fixture.release(key);
      assert.equal((await command.result(deadline())).status, 'succeeded');
      await eventually(() => view.getSnapshot().history[rootId]?.messages.length === 2);
      const revision = view.getSnapshot().history[rootId].revision;
      assert.equal((await writer.session(rootId).history.clear(revision).result(deadline())).status, 'succeeded');
      await eventually(() => view.getSnapshot().history[rootId]?.revision !== revision && view.getSnapshot().history[rootId]?.messages.length === 0);
      await assert.rejects(observer.call('history.page', { root_id: rootId, agent_id: rootId, through_seq: -1, revision, limit: 128, max_bytes: 524_288 }), error => error.kind === 'resynchronization_required');
      for (let round = 0; round < 4; round++) await writer.session(rootId).submit({ text: 'bounded ' + round }).result(deadline());
      await eventually(() => view.getSnapshot().history[rootId]?.messages.length === 4);
      const staleClear = await writer.session(rootId).history.clear(revision).result(deadline());
      assert.equal(staleClear.status, 'failed');
      assert.match(staleClear.failure.message, /history revision changed/);
      assert.equal((await writer.session(rootId).history.page()).messages.length, 8);
      assert.ok(view.getSnapshot().retainedBytes <= 16_384);
      assert.ok(view.getSnapshot().history[rootId].messages.length <= 4);
    } finally {
      unsubscribeA(); unsubscribeB();
      await view.dispose();
      observer.close(); writer.close();
    }
  });
}

test('application recovery storage holds only identity metadata and fences admission failure', async () => {
  const records = new Map();
  const storage = {
    async list() { return [...records.values()]; },
    async put(record) { records.set(record.commandId, structuredClone(record)); },
    async delete(record) { records.delete(record.commandId); },
  };
  const clientId = crypto.randomUUID();
  const client = await connect('unix', clientId, { recoveryStorage: storage });
  const rootId = await createRoot(client);
  const text = 'private request text ' + crypto.randomUUID();
  const command = client.session(rootId).submit({ text });
  assert.equal((await command.result(deadline())).status, 'succeeded');
  const record = records.get(command.commandId);
  assert.deepEqual(Object.keys(record).sort(), ['clientId', 'commandId', 'operation', 'rootId', 'runtimeId', 'version']);
  assert.ok(!JSON.stringify(await storage.list()).includes(text));
  client.close();
  const recovery = await connect('websocket', clientId, { recoveryStorage: storage });
  assert.equal((await recovery.recover(record).result(deadline())).result.text, text);
  const missing = { ...record, commandId: crypto.randomUUID() };
  await assert.rejects(recovery.recover(missing).retry(), error => error.kind === 'recovery_required');
  const explicit = recovery.recover(missing, { text: 'explicit original retry' });
  await explicit.retry();
  assert.equal((await explicit.result(deadline())).status, 'succeeded');
  recovery.close();

  const failing = await connect('unix', crypto.randomUUID(), { recoveryStorage: { ...storage, async put() { throw new Error('storage unavailable'); } } });
  const neverSent = failing.submit('submit', { text: 'must never execute' }, { rootId });
  await assert.rejects(neverSent.accepted(), /storage unavailable/);
  await assert.rejects(failing.call('command.status', { command_id: neverSent.commandId }), error => error.kind === 'command_not_found');
  assert.ok(!(await fixture.effects()).includes('must never execute'));
  failing.close();
});

test('configuration revision conflicts preserve the winning host update', async () => {
  const unix = await connect('unix');
  const websocket = await connect('websocket');
  const initial = await unix.configuration.get();
  const results = await Promise.allSettled([
    unix.configuration.update({ revision: initial.revision, max_retries: initial.max_retries + 1 }),
    websocket.configuration.update({ revision: initial.revision, max_retries: initial.max_retries + 2 }),
  ]);
  const successful = results.filter(result => result.status === 'fulfilled');
  const failed = results.filter(result => result.status === 'rejected');
  assert.equal(successful.length, 1);
  assert.equal(failed.length, 1);
  assert.equal(failed[0].reason.kind, 'conflict');
  assert.deepEqual(await websocket.configuration.get(), successful[0].value);
  assert.notEqual(successful[0].value.revision, initial.revision);
  unix.close(); websocket.close();
});

test('actual process crash preserves outcomes and never repeats uncertain effects', async () => {
  const clientId = crypto.randomUUID();
  const client = await connect('unix', clientId, { reconnect: false });
  const rootId = await createRoot(client);
  const key = crypto.randomUUID();
  const commandId = crypto.randomUUID();
  const command = client.submit('submit', { text: `hold:${key}` }, { rootId, commandId });
  const accepted = await command.accepted(deadline());
  await eventually(async () => (await fixture.effects()).includes(`hold:${key}`));
  const uploaded = await client.upload(new TextEncoder().encode('queued context survives restart'), { rootId, mediaType: 'text/plain' });
  const queuedId = crypto.randomUUID();
  const queuedInput = { text: 'queued attachment', attachments: [uploaded.asAttachment('text')] };
  const queued = client.session(rootId).submit(queuedInput, { commandId: queuedId });
  assert.equal((await queued.accepted(deadline())).status, 'queued');
  const observer = await connect('websocket');
  const view = createSessionView(observer.session(rootId));
  await view.start();
  assert.equal(view.getSnapshot().status, 'live');
  const previousRuntime = fixture.info.runtime_id;
  const previousGeneration = fixture.info.generation;
  const recoveryStarted = performance.now();
  await fixture.crashAndRestart();
  assert.equal(fixture.info.runtime_id, previousRuntime);
  assert.equal(fixture.info.generation, previousGeneration + 1);
  for (const transport of ['unix', 'websocket']) {
    const recovery = await connect(transport, clientId);
    const status = await recovery.call('command.status', { command_id: commandId });
    assert.equal(status.status, 'interrupted');
    assert.equal(status.ingress_seq, accepted.ingress_seq);
    assert.ok(status.failure);
    const duplicate = await recovery.submit('submit', { text: `hold:${key}` }, { rootId, commandId }).result(deadline());
    assert.equal(duplicate.status, 'interrupted');
    const resumed = await recovery.session(rootId).submit(queuedInput, { commandId: queuedId }).result(deadline());
    assert.equal(resumed.status, 'succeeded');
    assert.equal(JSON.parse(resumed.result.text).parts[0].text, 'queued context survives restart');
    recovery.close();
  }
  try {
    await eventually(() => observer.getSnapshot().state === 'connected' && observer.getSnapshot().info.generation === String(previousGeneration + 1));
    const authoritative = await observer.call('root.snapshot', { root_id: rootId });
    await eventually(() => {
      const snapshot = view.getSnapshot();
      assert.equal(snapshot.status, 'live');
      assert.ok(BigInt(snapshot.root.cursor) >= BigInt(authoritative.cursor));
      // Replayed lifecycle events advance the cursor before their coalesced
      // snapshot refresh supplies committed message bodies.
      assert.deepEqual(snapshot.root.messages, authoritative.messages);
      assert.deepEqual(snapshot.root.active_turns, authoritative.active_turns);
      return true;
    }, { description: 'reconnected history and active turns to converge' });
    reconnectRecoveryMs = performance.now() - recoveryStarted;
  } finally { await view.dispose(); observer.close(); }
  assert.equal((await fixture.effects()).filter(value => value === `hold:${key}`).length, 1);
  assert.equal((await fixture.effects()).filter(value => value.startsWith('{"text":"queued attachment"')).length, 1);
  client.close();
});

test('measure SDK admission and completion with four concurrent roots', async t => {
  const admission = [], completion = [], eventToView = [], eventDelivery = [], retainedBytes = [];
  let statusRequests = 0;
  await Promise.all(Array.from({ length: 4 }, async (_, index) => {
    const transport = index % 2 ? 'unix' : 'websocket';
    const factory = transport === 'unix' ? unixSocket(fixture.info.socket) : webSocket(fixture.info.endpoint);
    const endpoint = async (handlers, signal) => {
      const connection = await factory(handlers, signal);
      return { ...connection, get bufferedAmount() { return connection.bufferedAmount; }, send(message) {
        if (JSON.parse(message).method === 'command.status') statusRequests++;
        connection.send(message);
      } };
    };
    const client = await connect(transport, crypto.randomUUID(), { endpoint });
    const rootId = await createRoot(client);
    const view = createSessionView(client.session(rootId));
    const arrivals = new Map();
    let start;
    let firstEvent = false;
    const offEvent = client.onEvent(event => {
      if (event.root_id !== rootId || !event.kind.startsWith('stream.')) return;
      arrivals.set(event.seq, performance.now());
      if (firstEvent) { eventDelivery.push(performance.now() - start); firstEvent = false; }
    });
    const offView = view.subscribe(() => {
      const state = view.getSnapshot();
      if (!state.root) return;
      for (const [seq, arrival] of arrivals) {
        if (BigInt(seq) <= BigInt(state.root.cursor)) { eventToView.push(performance.now() - arrival); arrivals.delete(seq); }
      }
      retainedBytes.push(state.retainedBytes);
    });
    try {
      await view.start();
      for (let round = 0; round < 8; round++) {
        start = performance.now();
        firstEvent = true;
        const text = `measure ${index}/${round}`;
        const command = client.session(rootId).submit({ text });
        await command.accepted(deadline());
        admission.push(performance.now() - start);
        assert.equal((await command.result(deadline())).status, 'succeeded');
        completion.push(performance.now() - start);
        await eventually(() => view.getSnapshot().history[rootId]?.messages.at(-1)?.message?.content === text);
      }
      await eventually(() => arrivals.size === 0);
    } finally {
      offEvent(); offView(); await view.dispose(); client.close();
    }
  }));
  const summarize = values => {
    values.sort((a, b) => a - b);
    return { samples: values.length, p50_ms: values[Math.floor(values.length / 2)], p95_ms: values[Math.floor(values.length * 0.95)] };
  };
  assert.ok(eventToView.length > 0 && eventDelivery.length > 0);
  const measurements = {
    concurrent_roots: 4, admission: summarize(admission), completion: summarize(completion),
    submit_to_stream: summarize(eventDelivery), event_to_view: summarize(eventToView),
    max_view_retained_bytes: Math.max(...retainedBytes), command_status_requests: statusRequests,
    crash_to_recovered_view_ms: reconnectRecoveryMs,
    durability: 'SQLite WAL / synchronous=NORMAL',
  };
  t.diagnostic(JSON.stringify(measurements));
  if (process.env.WHIP_SDK_MEASUREMENTS) await writeFile(process.env.WHIP_SDK_MEASUREMENTS, JSON.stringify(measurements, null, 2) + '\n');
});
