import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { cp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { DatabaseSync } from 'node:sqlite';
import { startFixture, eventually } from './fixture.mjs';
import { normalize } from './compare.mjs';

const deadline = () => ({ signal: AbortSignal.timeout(30_000) });
const hash = value => createHash('sha256').update(value).digest('hex');
const plain = value => JSON.parse(JSON.stringify(value));
let cellSequence = 0;
const cell = code => 'Run this cell:\n\n```cell\n' + code + '\n```';

export async function loadSDK(baseline) {
  return { ...await import(pathToFileURL(join(baseline, 'packages/sdk/dist/index.js'))), ...await import(pathToFileURL(join(baseline, 'packages/sdk/dist/node.js'))) };
}

async function connect(sdk, fixture, transport, clientId, wire = []) {
  const factory = transport === 'unix' ? sdk.unixSocket(fixture.info.socket) : sdk.webSocket(fixture.info.endpoint);
  const endpoint = async (handlers, signal) => factory({ ...handlers, message(message) {
    wire.push(JSON.parse(message)); handlers.message(message);
  } }, signal);
  const client = sdk.createWhipClient({ endpoint, clientId, clientKind: 'automation', reconnect: false });
  await client.connect();
  return client;
}

async function createRoot(client, cwd, engine, commandId = 'create-root') {
  const result = await client.submit('session.create', { kind: 'agent', cwd, model: 'model', provider: 'provider', ...(engine ? { execution_engine: engine } : {}) }, { commandId }).result(deadline());
  assert.equal(result.status, 'succeeded', JSON.stringify(result));
  return result.result.root_id;
}

async function errorOf(action) {
  try { await action(); } catch (error) { assert.ok(error.rpc, `expected wire RPC error, got ${error}`); return plain(error.rpc); }
  assert.fail('request unexpectedly succeeded');
}

export async function smoke(sdk, binary, directory, transport) {
  const fixture = await startFixture(binary, directory);
  const wire = [];
  let client, stream;
  try {
    client = await connect(sdk, fixture, transport, 'compatibility', wire);
    const initialization = client.requireConnected();
    const { runtime_id, connection_id, protocol_major, protocol_minor, capabilities, negotiated_capabilities, operations, limits, execution_engines, default_execution_engine } = initialization;
    const configuration = await client.configuration.get();
    const updated = await client.configuration.update({ revision: configuration.revision, max_retries: configuration.max_retries + 1 });
    const configurationConflict = await errorOf(() => client.configuration.update({ revision: configuration.revision, max_retries: configuration.max_retries + 2 }));
    const provider = await client.providers.get('openai');
    const unknownProvider = await errorOf(() => client.providers.get('compatibility-missing'));
    const rootId = await createRoot(client, directory);
    const initial = await client.session(rootId).snapshot();
    stream = await client.events.subscribe(rootId, initial.cursor, deadline());
    // Larger than the supervisor's 32 KiB coalescing limit: both fragments must
    // remain separate regardless of scheduling, and each exercises content.
    const text = 'A'.repeat(16385) + 'B'.repeat(16385);
    const command = client.session(rootId).submit({ text }, { commandId: 'ordered-input' });
    const accepted = await command.accepted(deadline());
    assert.ok(['queued', 'running', 'succeeded'].includes(accepted.status));
    const outcome = await command.result(deadline());
    assert.equal(outcome.status, 'succeeded');
    const replay = await client.call('events.replay', { root_id: rootId, cursor: initial.cursor, limit: 1000 });
    await eventually(() => wire.filter(message => message.method === 'event').some(message => message.params.event.seq === replay.latest), 'live events reach durable cursor');
    const live = wire.filter(message => message.method === 'event').map(message => message.params.event);
    const replayComparable = replay.events.map(event => ({ ...event, subscription_id: stream.id }));
    assert.deepEqual(live, replayComparable, 'live events differ from durable order/multiplicity');
    const fragments = replay.events.filter(event => event.kind === 'stream.text');
    assert.equal(fragments.length, 2);
    const bodies = [];
    for (const event of fragments) bodies.push(await client.content(event.payload.content, { rootId }).readJSON({ maxBytes: 1 << 20 }));
    assert.equal(bodies.map(body => body.text).join(''), text);
    const status = await client.call('command.status', { command_id: command.commandId });
    const duplicate = await client.submit('submit', { text }, { rootId, commandId: command.commandId }).result(deadline());
    assert.equal(duplicate.ingress_seq, accepted.ingress_seq);
    const conflict = await errorOf(() => client.submit('submit', { text: 'changed' }, { rootId, commandId: command.commandId }).accepted(deadline()));
    const missing = await errorOf(() => client.call('command.status', { command_id: 'absent' }));
    const futureCursor = await errorOf(() => client.call('events.replay', { root_id: rootId, cursor: String(BigInt(replay.latest) + 1n), limit: 10 }));
    const snapshot = await client.session(rootId).snapshot();
    const history = await client.session(rootId).history.page();
    const transcript = plain({ initialization: { runtime_id, connection_id, protocol_major, protocol_minor, capabilities, negotiated_capabilities, operations, limits, execution_engines, default_execution_engine }, configuration, updated, configurationConflict, provider, unknownProvider,
      initial, status, duplicate, conflict, missing, futureCursor, snapshot, history, replay, live, bodies, acceptedIdentity: { command_id: accepted.command_id, ingress_seq: accepted.ingress_seq } });
    await writeFile(join(directory, 'wire.json'), JSON.stringify(wire, null, 2));
    return normalize(transcript, { directory });
  } finally { await stream?.dispose(); client?.close(); await fixture.close(); }
}

export async function lifecycle(sdk, binary, directory) {
  let fixture = await startFixture(binary, directory);
  const clients = [];
  const open = async transport => { const client = await connect(sdk, fixture, transport, 'lifecycle'); clients.push(client); return client; };
  try {
    let client = await open('unix');
    const rootId = await createRoot(client, directory);
    const command = client.session(rootId).submit({ text: 'hold:detach' }, { commandId: 'detach' });
    const accepted = await command.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes('hold:detach'), 'held work starts after acceptance');
    const abort = new AbortController();
    const waiting = command.result({ signal: abort.signal }); abort.abort();
    await assert.rejects(waiting);
    assert.equal((await command.status()).status, 'running');
    client.close(); client = await open('websocket');
    assert.equal((await client.call('command.status', { command_id: 'detach' })).ingress_seq, accepted.ingress_seq);
    await fixture.release('detach');
    assert.equal((await client.submit('submit', { text: 'hold:detach' }, { rootId, commandId: 'detach' }).result(deadline())).status, 'succeeded');
    const cancel = client.session(rootId).submit({ text: 'hold:cancel' }, { commandId: 'cancelled-work' });
    await cancel.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes('hold:cancel'), 'cancelled work starts');
    await cancel.cancel().result(deadline());
    assert.equal((await cancel.result(deadline())).status, 'cancelled');
    const held = client.session(rootId).submit({ text: 'hold:crash' }, { commandId: 'crashed-work' });
    const crashAccepted = await held.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes('hold:crash'), 'crash effect starts');
    const content = await client.upload(new TextEncoder().encode('durable queued attachment'), { rootId, mediaType: 'text/plain' });
    const input = { text: 'queued', attachments: [content.asAttachment('text')] };
    const queued = client.session(rootId).submit(input, { commandId: 'queued-work' });
    assert.equal((await queued.accepted(deadline())).status, 'queued');
    const runtime = fixture.info.runtime_id;
    await fixture.close(true);
    fixture = await startFixture(binary, directory);
    assert.equal(fixture.info.runtime_id, runtime);
    for (const transport of ['unix', 'websocket']) {
      client = await open(transport);
      const recovered = await client.call('command.status', { command_id: 'crashed-work' });
      assert.equal(recovered.status, 'interrupted'); assert.ok(recovered.failure);
      assert.equal(recovered.ingress_seq, crashAccepted.ingress_seq);
      assert.equal((await client.submit('submit', { text: 'hold:crash' }, { rootId, commandId: 'crashed-work' }).result(deadline())).status, 'interrupted');
      const outcome = await client.session(rootId).submit(input, { commandId: 'queued-work' }).result(deadline());
      assert.equal(outcome.status, 'succeeded');
      assert.equal(JSON.parse(outcome.result.text).parts[0].text, 'durable queued attachment');
    }
    const effects = await fixture.effects();
    for (const text of ['hold:detach', 'hold:cancel', 'hold:crash']) assert.equal(effects.filter(value => value === text).length, 1);
    assert.equal(effects.filter(value => value.startsWith('{"text":"queued"')).length, 1, 'queued attachment executed more than once');
  } finally { for (const client of clients) client.close(); await fixture.close(); }
}

async function runCell(client, root, code, child) {
  const text = `Compatibility step ${++cellSequence}.\n${cell(code)}`;
  const handle = child ? client.session(root).agents.submit(child, text) : client.session(root).submit({ text });
  const result = await handle.result(deadline());
  assert.equal(result.status, 'succeeded', JSON.stringify(result));
  // Submission accepts an inbox entry, not its completed turn. Match this
  // unique prompt, then require that turn's own successful tool result.
  const inspection = await eventually(async () => {
    const value = (await client.session(root).agents.inspect(child ?? root)).result;
    const messages = value.page.messages ?? [];
    return value.agent.status === 'idle' && messages.some(entry => entry.message?.role === 'user' && entry.message.content === text) && value;
  }, 'the submitted turn settles');
  assert.equal(inspection.agent.last_turn.status, 'succeeded', JSON.stringify(inspection.agent.last_turn));
  const messages = inspection.page.messages;
  const start = messages.findIndex(entry => entry.message?.role === 'user' && entry.message.content === text);
  const turn = messages.slice(start + 1);
  assert.ok(!turn.some(entry => entry.message?.role === 'user'), 'another turn followed the submitted prompt');
  const tool = turn.find(entry => entry.message?.role === 'tool' && entry.message.name === 'rlm_exec');
  assert.ok(tool, 'the submitted child turn has no cell result');
  assert.equal((tool.presentation ?? tool.message.presentation).turn_id, inspection.agent.last_turn.turn_id);
  const page = await client.call('trace.page', { root_id: root });
  const spans = page.spans.filter(span => span.turn_id === inspection.agent.last_turn.turn_id && span.agent_id === (child ?? root));
  for (const kind of ['agent', 'llm', 'tool']) assert.ok(spans.some(span => span.kind === kind), `new turn lost ${kind} trace`);
  const agent = spans.find(span => span.kind === 'agent');
  const toolSpan = spans.find(span => span.kind === 'tool');
  if (text.length <= 512) assert.equal(agent.attrs.input, text);
  assert.equal(toolSpan.parent_id, agent.id);
  for (const span of spans) {
    assert.ok(BigInt(span.end_ns) >= BigInt(span.start_ns) && BigInt(span.start_ns) > 0n);
    if (span.kind === 'llm') { assert.equal(span.parent_id, agent.id); assert.equal(span.attrs.provider, 'provider'); assert.equal(span.attrs.model, 'model'); }
  }
  assert.ok(spans.some(span => span.kind === 'llm' && span.attrs.model_call_id === toolSpan.attrs.emitting_call_id), 'tool lost its emitting model call');
  for (const name of new Set([...code.matchAll(/\b(state\.[a-z_]+|agents\.spawn)\(/g)].map(match => match[1]))) {
    const host = spans.find(span => span.kind === 'host' && span.name === name);
    assert.ok(host, `new turn lost ${name} host trace`);
    assert.equal(host.parent_id, toolSpan.id);
  }
  return JSON.parse(tool.message.content).output ?? '';
}

function inspectStore(directory, ids) {
  const database = new DatabaseSync(join(directory, 'home/runtime-v2/sessions.db'), { readOnly: true });
  try {
    assert.equal(database.prepare('PRAGMA user_version').get().user_version, 21);
    assert.equal(database.prepare('SELECT identity FROM runtime_schema').get().identity, 'whip-recursive-runtime-v21');
    assert.equal(database.prepare('PRAGMA integrity_check').get().integrity_check, 'ok');
    const checkpoints = database.prepare('SELECT root_id,agent_id,envelope,image,bytes FROM agent_checkpoints ORDER BY root_id,agent_id').all();
    for (const id of ids) assert.ok(checkpoints.some(row => row.agent_id === id), `missing checkpoint for ${id}`);
    for (const row of checkpoints) {
      const envelope = JSON.parse(Buffer.from(row.envelope));
      assert.equal(envelope.sha256, hash(row.image)); assert.equal(envelope.bytes, row.image.length); assert.equal(row.bytes, row.image.length);
    }
    return { schema: database.prepare('SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name').all(), checkpoints: checkpoints.map(row => ({ root: row.root_id, agent: row.agent_id, envelope: hash(row.envelope), image: hash(row.image) })), objects: database.prepare('SELECT digest,size FROM content_objects ORDER BY digest').all() };
  } finally { database.close(); }
}

async function checkObjects(directory, objects) {
  for (const object of objects) {
    const bytes = await readFile(join(directory, 'home/runtime-v2/artifacts/sha256', object.digest));
    assert.equal(hash(bytes), object.digest); assert.equal(bytes.length, object.size);
  }
}

async function trace(client, root) {
  const page = await client.call('trace.page', { root_id: root });
  assert.ok(page.spans.some(span => span.kind === 'agent'));
  for (const kind of ['llm', 'tool', 'host']) assert.ok(page.spans.some(span => span.kind === kind), `missing ${kind} spans`);
  const ids = new Set(page.spans.map(span => span.id));
  for (const span of page.spans) {
    assert.ok(BigInt(span.end_ns) >= BigInt(span.start_ns) && BigInt(span.start_ns) > 0n, `open/invalid span ${span.id}`);
    if (span.parent_id) assert.ok(ids.has(span.parent_id), `broken span parent ${span.id}`);
  }
  const exported = await client.call('trace.export', { root_id: root });
  const document = await client.content(exported.content, { rootId: root }).readJSON({ maxBytes: 8 << 20 });
  assert.equal(exported.spans, page.spans.length);
  assert.ok(document.resourceSpans?.length > 0);
  return { page, document };
}

export async function rollback(sdk, baseline, candidate, directory) {
  await mkdir(directory, { recursive: true });
  const workspace = join(directory, 'workspace'); await mkdir(workspace);
  const seed = join(directory, 'seed'), upgraded = join(directory, 'candidate');
  let fixture = await startFixture(baseline, seed, { agents: true });
  const roots = [];
  let client;
  try {
    client = await connect(sdk, fixture, 'unix', 'rollback');
    for (const engine of ['starlark', 'quickjs']) {
      const root = await createRoot(client, workspace, engine, `create-${engine}`);
      await client.session(root).setPermissionMode(false).result(deadline());
      const code = engine === 'starlark' ? 'memo = 41\ndef increment():\n    return memo + 1\nstate.private_set(key="phase", value="base")' : 'var memo = 41; var increment = () => memo + 1; await state.private_set({key:"phase",value:"base"});';
      await runCell(client, root, code);
      const prompt = cell(engine === 'starlark' ? 'child_memo = 71\ndef child_value():\n    return child_memo + 1' : 'var child_memo = 71; var child_value = () => child_memo + 1;');
      const spawn = engine === 'starlark' ? `agents.spawn(name="keeper", prompt=${JSON.stringify(prompt)}, report="message")` : `await agents.spawn({name:"keeper",prompt:${JSON.stringify(prompt)},report:"message"});`;
      await runCell(client, root, spawn);
      const listed = await client.session(root).agents.list();
      const agents = Array.isArray(listed.result) ? listed.result : listed.result.agents;
      const child = agents.find(agent => agent.name === 'keeper').id;
      await runCell(client, root, engine === 'starlark' ? 'print(child_value())' : 'print(child_value());', child);
      const content = await client.upload(new TextEncoder().encode('rollback content '.repeat(25_000)), { rootId: root, mediaType: 'text/plain' });
      const expectedTrace = await trace(client, root);
      roots.push({ root, child, engine, content: content.handle, expectedTrace });
      await writeFile(join(directory, `trace-${engine}.json`), JSON.stringify(expectedTrace, null, 2));
    }
    client.close(); await fixture.close();
    const original = inspectStore(seed, roots.flatMap(value => [value.root, value.child]));
    await checkObjects(seed, original.objects);
    await cp(seed, upgraded, { recursive: true });
    assert.deepEqual(inspectStore(upgraded, []).checkpoints, original.checkpoints, 'copy lost checkpoints');
    fixture = await startFixture(candidate, upgraded, { agents: true });
    for (const phase of ['candidate', 'rollback']) {
      client = await connect(sdk, fixture, phase === 'candidate' ? 'websocket' : 'unix', 'rollback');
      for (const record of roots) {
        const { root, child, engine, content } = record;
        const observed = await trace(client, root);
        assert.ok(BigInt(observed.page.server_time_ns) > 0n);
        assert.deepEqual({ ...observed.page, server_time_ns: 'clock' }, { ...record.expectedTrace.page, server_time_ns: 'clock' }, 'persisted trace changed across binary restart');
        assert.deepEqual(observed.document, record.expectedTrace.document, 'OTLP export changed across binary restart');
        const body = await client.content(content, { rootId: root }).readText({ maxBytes: 1 << 20 });
        assert.equal(body, 'rollback content '.repeat(25_000));
        const rootCode = engine === 'starlark' ? 'state.private_get(key=\"phase\")\nprint(increment())' : 'await state.private_get({key:\"phase\"}); print(increment());';
        assert.equal(await runCell(client, root, rootCode), '42\n');
        assert.equal(await runCell(client, root, engine === 'starlark' ? 'print(child_value())' : 'print(child_value());', child), '72\n');
        if (phase === 'candidate') {
          await runCell(client, root, engine === 'starlark' ? 'new_memo = 99\nstate.private_set(key=\"phase\", value=\"candidate\")' : 'var new_memo = 99; await state.private_set({key:\"phase\",value:\"candidate\"});');
          await runCell(client, root, engine === 'starlark' ? 'child_new = 199\nstate.private_set(key=\"phase\", value=\"candidate\")' : 'var child_new = 199; await state.private_set({key:\"phase\",value:\"candidate\"});', child);
        } else {
          assert.equal(await runCell(client, root, engine === 'starlark' ? 'print(new_memo)' : 'print(new_memo);'), '99\n');
          assert.equal(await runCell(client, root, engine === 'starlark' ? 'print(child_new)' : 'print(child_new);', child), '199\n');
        }
        record.expectedTrace = await trace(client, root);
      }
      client.close(); await fixture.close();
      const persisted = inspectStore(upgraded, roots.flatMap(value => [value.root, value.child]));
      assert.deepEqual(persisted.schema, original.schema, 'candidate changed schema');
      await checkObjects(upgraded, persisted.objects);
      if (phase === 'candidate') fixture = await startFixture(baseline, upgraded, { agents: true });
    }
    await writeFile(join(directory, 'rollback.json'), JSON.stringify({ roots, original, final: inspectStore(upgraded, []) }, null, 2));
  } finally { client?.close(); await fixture.close(); }
}
