import {MermaidRenderError, mermaidBytes, mermaidLimits, mermaidVersion, validateMermaid, validateMermaidPalette} from './mermaid-data.ts';
import type {MermaidImage, MermaidPalette, MermaidReason} from './mermaid-data.ts';
export {MermaidRenderError, mermaidLimits, validateMermaid} from './mermaid-data.ts';
export type {MermaidImage, MermaidPalette, MermaidReason, MermaidType, MermaidValidation} from './mermaid-data.ts';

type Consumer = {resolve: (image: MermaidImage) => void; reject: (error: Error) => void; finish: (retain: boolean) => void};
type Task = {id: number; key: string; source: string; palette: MermaidPalette; bytes: number; consumers: Set<Consumer>};
const cache = new Map<string, {image: MermaidImage; bytes: number}>();
let cacheBytes = 0;
const queue: Task[] = [];
let queuedBytes = 0;
let active: Task | undefined;
let worker: Worker | undefined;
let ready = false;
let serial = 0;
let deadline: ReturnType<typeof setTimeout> | undefined;
let leases = 0;
const abortError = () => new DOMException('Diagram rendering was cancelled.', 'AbortError');

function stopWorker() {
  clearTimeout(deadline); deadline = undefined;
  worker?.terminate(); worker = undefined; ready = false;
}
function remember(task: Task, image: MermaidImage) {
  const bytes = mermaidBytes(task.key) + mermaidBytes(image.svg);
  if (bytes > mermaidLimits.cacheBytes) return;
  while (cache.size >= mermaidLimits.cache || cacheBytes + bytes > mermaidLimits.cacheBytes) {
    const first = cache.keys().next().value!;
    cacheBytes -= cache.get(first)!.bytes; cache.delete(first);
  }
  cache.set(task.key, {image, bytes}); cacheBytes += bytes;
}
function settle(task: Task, image?: MermaidImage, error?: Error) {
  for (const consumer of task.consumers) {
    consumer.finish(!!image);
    if (image) consumer.resolve({...image}); else consumer.reject(error!);
  }
  task.consumers.clear();
}
function failWorker(reason: MermaidReason, message: string) {
  stopWorker();
  const failure = new MermaidRenderError(reason, message);
  // A startup error affects all queued consumers. Do not retry a broken chunk once per diagram.
  if (active) {const task = active; active = undefined; settle(task, undefined, failure);}
  for (const task of queue.splice(0)) settle(task, undefined, failure);
  queuedBytes = 0;
}
function pump() {
  if (active) return;
  if (!queue.length) {if (!leases) stopWorker(); return;}
  if (!worker) {
    try {
      const instance = new Worker(new URL('./mermaid-worker.ts', import.meta.url), {type: 'module', name: 'whip-mermaid'});
      worker = instance;
      deadline = setTimeout(() => failWorker('unavailable', 'The diagram renderer could not start in time.'), mermaidLimits.startupMs);
      instance.onerror = () => {if (worker === instance) failWorker('unavailable', 'The diagram renderer is unavailable.');};
      instance.onmessageerror = () => {if (worker === instance) failWorker('render-failed', 'The diagram renderer returned an unreadable response.');};
      instance.onmessage = event => {
        if (worker !== instance) return;
        if (event.data?.ready === true) {clearTimeout(deadline); ready = true; pump(); return;}
        if (!active || event.data?.id !== active.id) return;
        clearTimeout(deadline);
        const task = active; active = undefined;
        const image = event.data.image as MermaidImage | undefined;
        if (image && typeof image.svg === 'string' && mermaidBytes(image.svg) <= mermaidLimits.outputBytes && [image.width, image.height].every(value => Number.isFinite(value) && value > 0 && value <= mermaidLimits.dimension) && image.width * image.height <= mermaidLimits.pixels) {
          remember(task, image); settle(task, image);
        } else {
          const failure = event.data.error;
          settle(task, undefined, new MermaidRenderError(failure?.reason ?? 'render-failed', failure?.message ?? 'The diagram renderer returned an invalid image.'));
        }
        pump();
      };
    } catch {failWorker('unavailable', 'Diagram rendering is unavailable in this browser.');}
    return;
  }
  if (!ready) return;
  active = queue.shift()!; queuedBytes -= active.bytes;
  const task = active;
  deadline = setTimeout(() => {
    if (active !== task) return;
    stopWorker(); active = undefined;
    settle(task, undefined, new MermaidRenderError('timeout', 'This diagram exceeded the two-second layout budget.'));
    pump();
  }, mermaidLimits.timeoutMs);
  try {worker.postMessage({id: task.id, source: task.source, palette: task.palette});}
  catch {failWorker('unavailable', 'The diagram renderer could not receive this diagram.');}
}

/** One lazy worker. A supplied signal owns the mounted consumer, including after its image resolves. */
export function renderMermaid(source: string, palette: MermaidPalette, {signal}: {signal?: AbortSignal} = {}): Promise<MermaidImage> {
  if (signal?.aborted) return Promise.reject(abortError());
  const validation = validateMermaid(source);
  if (!validation.ok) return Promise.reject(new MermaidRenderError(validation.reason, validation.message));
  try {validateMermaidPalette(palette);} catch (error) {return Promise.reject(error);}
  const key = JSON.stringify([mermaidVersion, source, palette.background, palette.foreground, palette.line, palette.accent, palette.muted, palette.surface, palette.border, palette.font, palette.fontSize]);
  const cached = cache.get(key);
  if (cached) {cache.delete(key); cache.set(key, cached); return Promise.resolve({...cached.image});}
  if (leases >= 128) return Promise.reject(new MermaidRenderError('busy', 'Too many diagrams are mounted. Showing the original source.'));
  let task = active?.key === key ? active : queue.find(item => item.key === key);
  const bytes = mermaidBytes(source);
  if (!task && (queue.length >= mermaidLimits.queue || queuedBytes + bytes > mermaidLimits.queueBytes)) return Promise.reject(new MermaidRenderError('busy', 'The diagram rendering queue is full. Try again after other diagrams finish.'));
  if (!task) {
    task = {id: ++serial, key, source, palette: {...palette}, bytes, consumers: new Set()};
    queue.push(task); queuedBytes += bytes;
  }
  const pending = task;
  leases++;
  return new Promise((resolve, reject) => {
    let released = false;
    const release = () => {
      if (released) return;
      released = true; leases--;
      signal?.removeEventListener('abort', cancel);
      if (!leases && !active && !queue.length) stopWorker();
    };
    const consumer: Consumer = {resolve, reject, finish: retain => {if (!signal || !retain) release();}};
    const cancel = () => {
      release();
      if (!pending.consumers.delete(consumer)) return;
      reject(abortError());
      if (!pending.consumers.size) {
        if (active === pending) {stopWorker(); active = undefined;}
        else {const index = queue.indexOf(pending); if (index >= 0) {queue.splice(index, 1); queuedBytes -= pending.bytes;}}
        pump();
      }
    };
    pending.consumers.add(consumer);
    signal?.addEventListener('abort', cancel, {once: true});
    pump();
  });
}
