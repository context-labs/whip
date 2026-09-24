import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mermaidLimits} from '../src/mermaid-data.ts';
const palette = {background: '#ffffff', foreground: '#202020', line: '#555555', accent: '#0055aa', muted: '#555555', surface: '#eeeeee', border: '#555555', font: 'system' as const, fontSize: 13};
let generation = 0;
class FakeWorker {
  static instances: FakeWorker[] = [];
  onmessage?: (event: {data: unknown}) => void;
  onerror?: () => void;
  onmessageerror?: () => void;
  sent: {id: number; source: string}[] = [];
  terminated = false;
  constructor() {FakeWorker.instances.push(this);}
  terminate() {this.terminated = true;}
  postMessage(data: {id: number; source: string}) {this.sent.push(data);}
  reply(data: unknown) {this.onmessage?.({data});}
  ready() {this.reply({ready: true});}
  complete(svg = '<svg/>') {this.reply({id: this.sent.at(-1)!.id, image: {svg, width: 100, height: 80}});}
}
async function fresh(t: Parameters<Parameters<typeof test>[1]>[0]) {
  FakeWorker.instances = [];
  t.mock.method(globalThis, 'Worker', FakeWorker);
  return import(`../src/mermaid-renderer.ts?generation=${++generation}`);
}
// Worker does not exist in Node. Install only a configurable test stub, restored at process end.
Object.defineProperty(globalThis, 'Worker', {value: FakeWorker, writable: true, configurable: true});

test('lazy validation, shared in-flight work, cache and last-consumer teardown', async t => {
  const {renderMermaid} = await fresh(t);
  await assert.rejects(renderMermaid('pie\n A: 1', palette), {reason: 'unsupported-type'});
  assert.equal(FakeWorker.instances.length, 0);
  const a = new AbortController(), b = new AbortController();
  const p = renderMermaid('flowchart TD\n A --> B', palette, {signal: a.signal});
  const q = renderMermaid('flowchart TD\n A --> B', palette, {signal: b.signal});
  const worker = FakeWorker.instances[0]!;
  assert.equal(FakeWorker.instances.length, 1);
  worker.ready(); assert.equal(worker.sent.length, 1);
  worker.complete(); assert.deepEqual(await p, await q);
  a.abort(); assert.equal(worker.terminated, false);
  b.abort(); assert.equal(worker.terminated, true);
  await renderMermaid('flowchart TD\n A --> B', palette);
  assert.equal(FakeWorker.instances.length, 1, 'cache survives worker teardown without retaining URLs');
});
test('abort removes queued work and terminates an abandoned active layout; stale completion cannot publish', async t => {
  const {renderMermaid} = await fresh(t);
  const a = new AbortController(), b = new AbortController();
  const p = renderMermaid('flowchart TD\n A --> B', palette, {signal: a.signal});
  const rejected = assert.rejects(p, {name: 'AbortError'});
  const old = FakeWorker.instances[0]!; old.ready();
  const q = renderMermaid('flowchart TD\n C --> D', palette, {signal: b.signal});
  a.abort(); await rejected;
  assert.equal(old.terminated, true);
  const next = FakeWorker.instances[1]!; next.ready();
  old.complete('stale'); next.complete('current');
  assert.equal((await q).svg, 'current'); b.abort();
  assert.equal(next.terminated, true);
});
test('bounded queue rejects overflow and cancellation releases its capacity', async t => {
  const {renderMermaid} = await fresh(t);
  const controllers: AbortController[] = [], pending: Promise<unknown>[] = [];
  for (let i = 0; i < mermaidLimits.queue; i++) {
    const controller = new AbortController(); controllers.push(controller);
    pending.push(assert.rejects(renderMermaid(`flowchart TD\n A${i} --> B`, palette, {signal: controller.signal}), {name: 'AbortError'}));
  }
  await assert.rejects(renderMermaid('flowchart TD\n Extra --> B', palette), {reason: 'busy'});
  controllers[0]!.abort();
  const replacement = new AbortController();
  pending.push(assert.rejects(renderMermaid('flowchart TD\n Replacement --> B', palette, {signal: replacement.signal}), {name: 'AbortError'}));
  for (const controller of controllers) controller.abort(); replacement.abort();
  await Promise.all(pending);
  assert.equal(FakeWorker.instances[0]!.terminated, true);
});
test('startup deadline and active deadline terminate computation, fail honestly and permit a new request', async t => {
  t.mock.timers.enable({apis: ['setTimeout']});
  const {renderMermaid} = await fresh(t);
  const startup = assert.rejects(renderMermaid('flowchart TD\n A --> B', palette), {reason: 'unavailable'});
  t.mock.timers.tick(mermaidLimits.startupMs); await startup;
  assert.equal(FakeWorker.instances[0]!.terminated, true);
  const layout = assert.rejects(renderMermaid('flowchart TD\n A --> B', palette), {reason: 'timeout'});
  const stuck = FakeWorker.instances[1]!; stuck.ready();
  t.mock.timers.tick(mermaidLimits.timeoutMs); await layout;
  assert.equal(stuck.terminated, true);
  const success = renderMermaid('flowchart TD\n C --> D', palette);
  const recovered = FakeWorker.instances[2]!; recovered.ready(); recovered.complete(); await success;
  assert.equal(recovered.terminated, true);
});
test('worker startup failure rejects queue once; malformed image rejects and releases worker', async t => {
  const {renderMermaid} = await fresh(t);
  const a = assert.rejects(renderMermaid('flowchart TD\n A --> B', palette), {reason: 'unavailable'});
  const b = assert.rejects(renderMermaid('flowchart TD\n C --> D', palette), {reason: 'unavailable'});
  FakeWorker.instances[0]!.onerror!(); await Promise.all([a, b]);
  assert.equal(FakeWorker.instances.length, 1);
  const bad = assert.rejects(renderMermaid('flowchart TD\n A --> B', palette), {reason: 'render-failed'});
  const worker = FakeWorker.instances[1]!; worker.ready(); worker.reply({id: worker.sent[0]!.id, image: {svg: '<svg/>', width: Infinity, height: 3}}); await bad;
  assert.equal(worker.terminated, true);
});
test('cache keys include exact palette/font settings and cache bytes evict old SVGs', async t => {
  const {renderMermaid} = await fresh(t);
  for (let i = 0; i < 10; i++) {
    const pending = renderMermaid(`flowchart TD\n A${i} --> B`, palette);
    const worker = FakeWorker.instances.at(-1)!; worker.ready(); worker.complete('x'.repeat(900_000)); await pending;
  }
  const before = FakeWorker.instances.length;
  const evicted = renderMermaid('flowchart TD\n A0 --> B', palette);
  assert.equal(FakeWorker.instances.length, before + 1);
  let worker = FakeWorker.instances.at(-1)!; worker.ready(); worker.complete(); await evicted;
  const changed = renderMermaid('flowchart TD\n A0 --> B', {...palette, font: 'inter'});
  assert.equal(FakeWorker.instances.length, before + 2);
  worker = FakeWorker.instances.at(-1)!; worker.ready(); worker.complete(); await changed;
});
