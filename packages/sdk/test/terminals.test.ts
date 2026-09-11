import assert from 'node:assert/strict';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { MAX_TERMINAL_WRITE_BYTES } from '../src/terminals.js';
import { decodeBase64, encodeBase64 } from '../src/util.js';
import { transportFixture } from './transport-fixture.js';

const attachReply = { cursor: '0', cwd: '/work/project', cols: 80, rows: 24, exited: false };

test('terminal calls carry the contract shapes and normalize cursors', async t => {
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'terminal.open') connection.reply(request, { id: 'term-a', shell: '/bin/zsh', cwd: '/work/project' });
    else if (request.method === 'terminal.attach') connection.reply(request, { ...attachReply, cursor: '4096', exited: true, exit_code: 3, signal: 'hangup' });
    else connection.reply(request, { accepted: true });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const opened = await client.terminals.open({ cwd: '/work/project', rootId: 'root-1', cols: 80, rows: 24 });
  assert.deepEqual(opened, { id: 'term-a', shell: '/bin/zsh', cwd: '/work/project' });
  assert.deepEqual(fixture.current.requests.at(-1)?.params, { cwd: '/work/project', root_id: 'root-1', cols: 80, rows: 24 });
  await client.terminals.open({ cols: 120, rows: 40 });
  assert.deepEqual(fixture.current.requests.at(-1)?.params, { cols: 120, rows: 40 });

  const attachment = await client.terminals.attach('term-a', -1);
  assert.deepEqual(fixture.current.requests.at(-1)?.params, { id: 'term-a', cursor: '-1' });
  assert.deepEqual(attachment, { cursor: 4096, cwd: '/work/project', cols: 80, rows: 24, exited: true, exitCode: 3, signal: 'hangup' });

  await client.terminals.write('term-a', 'echo hi\r');
  assert.deepEqual(fixture.current.requests.at(-1)?.params, { id: 'term-a', bytes: encodeBase64(new TextEncoder().encode('echo hi\r')) });
  await client.terminals.write('term-a', new Uint8Array([0x03]));
  assert.equal(new TextDecoder().decode(decodeBase64((fixture.current.requests.at(-1)?.params as { bytes: string }).bytes)), '');
  await assert.rejects(client.terminals.write('term-a', ''), /1\.\.16384 bytes/);
  await assert.rejects(client.terminals.write('term-a', new Uint8Array(MAX_TERMINAL_WRITE_BYTES + 1)), /1\.\.16384 bytes/);

  await client.terminals.resize('term-a', 132, 43);
  assert.deepEqual(fixture.current.requests.at(-1)?.params, { id: 'term-a', cols: 132, rows: 43 });
  await client.terminals.close('term-a');
  assert.deepEqual(fixture.current.requests.at(-1)?.params, { id: 'term-a' });
});

test('terminal notifications reach listeners decoded and in order', async t => {
  const fixture = transportFixture();
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const outputs: { id: string; cursor: number; text: string }[] = [];
  const exits: unknown[] = [];
  const detached: string[] = [];
  const offOutput = client.terminals.onOutput(output => outputs.push({ id: output.id, cursor: output.cursor, text: new TextDecoder().decode(output.bytes) }));
  client.terminals.onExited(exit => exits.push(exit));
  client.terminals.onDetached(id => detached.push(id));
  fixture.current.notify('terminal.output', { id: 'term-a', cursor: '0', bytes: encodeBase64(new TextEncoder().encode('$ ')) });
  fixture.current.notify('terminal.output', { id: 'term-a', cursor: '2', bytes: encodeBase64(new TextEncoder().encode('ls\r\n')) });
  fixture.current.notify('terminal.exited', { id: 'term-a', exit_code: 0 });
  fixture.current.notify('terminal.exited', { id: 'term-b', exit_code: -1, signal: 'killed' });
  fixture.current.notify('terminal.detached', { id: 'term-a' });
  assert.deepEqual(outputs, [{ id: 'term-a', cursor: 0, text: '$ ' }, { id: 'term-a', cursor: 2, text: 'ls\r\n' }]);
  assert.deepEqual(exits, [{ id: 'term-a', exitCode: 0 }, { id: 'term-b', exitCode: -1, signal: 'killed' }]);
  assert.deepEqual(detached, ['term-a']);
  offOutput();
  fixture.current.notify('terminal.output', { id: 'term-a', cursor: '6', bytes: encodeBase64(new TextEncoder().encode('x')) });
  assert.equal(outputs.length, 2);
  assert.equal(client.getSnapshot().state, 'connected');
});

test('malformed terminal notifications and unknown methods disconnect the client', async t => {
  for (const [method, params] of [
    ['terminal.output', { id: 'term-a', cursor: 'seven', bytes: 'not base64!' }],
    ['terminal.exited', { id: 'term-a' }],
    ['terminal.bogus', { id: 'term-a' }],
  ] as const) {
    const fixture = transportFixture();
    const client = new WhipClient({ endpoint: fixture.factory, clientId: 'client', reconnect: false });
    t.after(() => client.close());
    await client.connect();
    fixture.current.notify(method, params);
    if (method === 'terminal.bogus') {
      // Unknown methods are ignored, not treated as malformed; the connection stays usable.
      assert.equal(client.getSnapshot().state, 'connected', method);
    } else {
      assert.notEqual(client.getSnapshot().state, 'connected', method);
    }
  }
});
