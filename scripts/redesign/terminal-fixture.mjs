import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { browserSocket } from '../../packages/sdk/dist/browser.js';

const deadline = () => ({ signal: AbortSignal.timeout(10_000) });
async function until(read, condition) {
  const signal = AbortSignal.timeout(10_000);
  for (;;) { signal.throwIfAborted(); const value = await read(); if (condition(value)) return value; await new Promise(resolve => setTimeout(resolve, 10)); }
}
export async function terminalAcceptance({ start, stop, directory }) {
  let ready = await start(true, true);
  const connect = value => Client.connect(browserSocket(value.web, { expectedRuntimeID: value.runtime_id, expectedProcessEpoch: value.process_epoch }), { clientID: 'terminal-human', ...deadline() });
  let client = await connect(ready);
  const NativeSocket = globalThis.WebSocket;
  let match = () => false;
  let dropped = 0;
  class DropAcknowledgement extends NativeSocket {
    set onmessage(handler) { super.onmessage = handler === null ? null : event => { const value = JSON.parse(event.data); if (match(value)) { dropped++; this.close(); return; } handler.call(this, event); }; }
    get onmessage() { return super.onmessage; }
  }
  match = value => !!value.result?.shell;
  try {
    globalThis.WebSocket = DropAcknowledgement;
    await assert.rejects(client.openTerminal({ cwd: directory, cols: 80, rows: 24 }, deadline()), DeliveryError);
  } finally { globalThis.WebSocket = NativeSocket; }
  let handles = await client.listTerminals(deadline()); assert.equal(handles.items.length, 1); assert.equal(dropped, 1);
  const terminal = handles.items[0];
  let cursor = '0', output = '';
  const readTo = async marker => until(async () => {
    const page = await client.readTerminal(terminal, cursor, 32768, deadline());
    assert.equal(BigInt(page.next), BigInt(page.from) + BigInt(Buffer.from(page.data_base64, 'base64').length));
    if (page.truncated) output = '';
    output = (output + Buffer.from(page.data_base64, 'base64').toString()).slice(-(1 << 20)); cursor = page.next; return output;
  }, text => text.includes(marker));
  const write = text => client.writeTerminal(terminal, new TextEncoder().encode(text), deadline());
  await write("stty -echo; printf 'ambient-%s-end\\n' \"${WHIP_FAKE_CREDENTIAL-unset}\"\n");
  await readTo('ambient-unset-end');
  await client.resizeTerminal(terminal, 110, 41, deadline()); await write("stty size; echo sized-$((4+4))-done\n"); await readTo('sized-8-done'); assert.ok(output.includes('41 110'));
  const sideEffect = join(directory, 'human-input-once');
  match = value => value.result?.accepted === true;
  try {
    globalThis.WebSocket = DropAcknowledgement;
    await assert.rejects(write(`printf 'once\\n' >> ${JSON.stringify(sideEffect)}; echo wrote-$((1+1))-done\n`), DeliveryError);
  } finally { globalThis.WebSocket = NativeSocket; }
  await readTo('wrote-2-done'); assert.equal(await readFile(sideEffect, 'utf8'), 'once\n'); assert.equal(dropped, 2);
  await write('head -c 1500000 /dev/zero; echo flooded-$((2+2))-done\n'); await readTo('flooded-4-done');
  const stale = await client.readTerminal(terminal, '0', 7, deadline()); assert.equal(stale.truncated, true); assert.equal(Buffer.from(stale.data_base64, 'base64').length, 7); assert.ok(BigInt(stale.terminal.end)-BigInt(stale.terminal.start) <= 1n<<20n);
  const pidPath = join(directory, 'terminal-child');
  await write(`sh -c 'trap "" HUP; echo $$ > ${JSON.stringify(pidPath)}; exec sleep 300'\n`);
  const pid = Number(await until(() => readFile(pidPath, 'utf8').catch(() => ''), value => /^\d+\n$/.test(value)));
  await client.closeTerminal(terminal, deadline()); assert.throws(() => process.kill(pid, 0), error => error.code === 'ESRCH');
  assert.equal((await client.listTerminals()).items.length, 0);
  const second = await client.openTerminal({ cwd: directory, cols: 80, rows: 24 }, deadline());
  const shutdownPidPath = join(directory, 'terminal-shutdown-child');
  await client.writeTerminal(second, new TextEncoder().encode(`sh -c 'trap "" HUP; echo $$ > ${JSON.stringify(shutdownPidPath)}; exec sleep 300'\n`), deadline());
  const shutdownPid = Number(await until(() => readFile(shutdownPidPath, 'utf8').catch(() => ''), value => /^\d+\n$/.test(value)));
  const previousEpoch = ready.process_epoch; await stop(); assert.throws(() => process.kill(shutdownPid, 0), error => error.code === 'ESRCH');
  ready = await start(true, true); assert.notEqual(ready.process_epoch, previousEpoch); client = await connect(ready);
  assert.equal((await client.listTerminals()).items.length, 0);
  await assert.rejects(client.readTerminal(second, '0', 10, deadline()), error => error.kind === 'IDENTITY');
  await assert.rejects(client.writeTerminal(second, new Uint8Array([65]), deadline()), error => error.kind === 'IDENTITY');
  await stop();
}
