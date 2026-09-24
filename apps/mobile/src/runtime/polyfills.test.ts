/** @jest-environment node */
import { installAbortCheckpoint, installAbortReason } from './polyfills';
import { AbortController as NativeAbortController, AbortSignal as NativeAbortSignal } from 'abort-controller';
import { installAbortSignalPatch } from 'expo/src/winter/AbortSignal';
import { createWhipClient } from '@whip/sdk';

const NativeController = NativeAbortController as unknown as typeof AbortController;
const NativeSignal = NativeAbortSignal as unknown as typeof AbortSignal;
const originalController = globalThis.AbortController;
const originalAbort = Object.getOwnPropertyDescriptor(NativeController.prototype, 'abort')!;
const originalAny = Object.getOwnPropertyDescriptor(NativeSignal, 'any');
const originalTimeout = Object.getOwnPropertyDescriptor(NativeSignal, 'timeout');
afterEach(() => {
  globalThis.AbortController = originalController;
  Object.defineProperty(NativeController.prototype, 'abort', originalAbort);
  if (originalAny) Object.defineProperty(NativeSignal, 'any', originalAny); else Reflect.deleteProperty(NativeSignal, 'any');
  if (originalTimeout) Object.defineProperty(NativeSignal, 'timeout', originalTimeout); else Reflect.deleteProperty(NativeSignal, 'timeout');
  jest.useRealTimers();
});

test('legacy native abort publishes exact reasons before listeners run and retains the first reason', () => {
  installAbortReason(NativeController);
  for (const reason of [new Error('paused'), 'cancelled', 0, false, '', null, undefined]) {
    const controller = new NativeController();
    let observed: unknown = 'not observed'; let events = 0;
    controller.signal.addEventListener('abort', () => { observed = controller.signal.reason; events++; });
    controller.abort(reason);
    if (reason === undefined) expect(observed).toEqual(expect.objectContaining({ name: 'AbortError' }));
    else expect(observed).toBe(reason);
    expect(controller.signal.reason).toBe(observed);
    controller.abort(new Error('later'));
    expect(controller.signal.reason).toBe(observed); expect(events).toBe(1);
  }
});

test('reason patch is idempotent and leaves complete native implementations unchanged', () => {
  const native = AbortController.prototype.abort;
  installAbortReason(); expect(AbortController.prototype.abort).toBe(native);
  installAbortReason(NativeController); const patched = NativeController.prototype.abort;
  installAbortReason(NativeController); expect(NativeController.prototype.abort).toBe(patched);
});

test('Expo composition and timeout listeners see reasons during native abort dispatch', () => {
  jest.useFakeTimers(); globalThis.AbortController = NativeController;
  installAbortReason(NativeController); installAbortSignalPatch(NativeSignal);
  const first = new NativeController(); const second = new NativeController();
  const combined = NativeSignal.any([first.signal, second.signal]);
  const reason = new Error('pause local observation'); let observed: unknown;
  combined.addEventListener('abort', () => { observed = combined.reason; });
  first.abort(reason); second.abort(new Error('later'));
  expect(observed).toBe(reason); expect(combined.reason).toBe(reason);
  expect(NativeSignal.any([first.signal]).reason).toBe(reason);
  const timed = NativeSignal.timeout(5); let timedReason: unknown;
  timed.addEventListener('abort', () => { timedReason = timed.reason; });
  jest.advanceTimersByTime(5);
  expect(timedReason).toEqual(expect.objectContaining({ name: 'TimeoutError' }));
  expect(timed.reason).toBe(timedReason);
});

test('SDK initialization preserves the paused reason through a legacy native controller', async () => {
  globalThis.AbortController = NativeController;
  installAbortReason(NativeController); installAbortCheckpoint(NativeSignal.prototype);
  const client = createWhipClient({ clientId: 'native-fixture', endpoint: (_handlers, signal) => new Promise((_resolve, reject) => {
    signal.addEventListener('abort', () => reject(signal.reason), { once: true });
  }) });
  try {
    const connecting = client.connect(); client.pause();
    await expect(connecting).rejects.toMatchObject({ kind: 'paused' });
    expect(client.getSnapshot().state).toBe('paused');
  } finally { client.close(); }
});

test('React Native base signals without a reason throw an AbortError', () => {
  installAbortCheckpoint(NativeSignal.prototype);
  const controller = new NativeAbortController();
  const signal = controller.signal as unknown as AbortSignal;
  expect(() => signal.throwIfAborted()).not.toThrow();
  controller.abort();
  expect(() => signal.throwIfAborted()).toThrow(expect.objectContaining({ name: 'AbortError' }));
});

test('native cancellation checkpoint preserves exact reasons and permits live work', () => {
  const prototype: { throwIfAborted?: (this: AbortSignal) => void } = {};
  installAbortCheckpoint(prototype);
  const live = new AbortController();
  expect(() => prototype.throwIfAborted!.call(live.signal)).not.toThrow();
  for (const reason of [new Error('cancelled'), 'cancelled', 0, null]) {
    const controller = new AbortController(); controller.abort(reason);
    let caught: unknown = 'not-thrown';
    try { prototype.throwIfAborted!.call(controller.signal); } catch (error) { caught = error; }
    expect(caught).toBe(reason);
  }
  live.abort();
  expect(() => prototype.throwIfAborted!.call(live.signal)).toThrow(expect.objectContaining({ name: 'AbortError' }));
});
test('native or future implementations are not replaced', () => {
  const native = jest.fn(); const prototype = { throwIfAborted: native };
  installAbortCheckpoint(prototype); expect(prototype.throwIfAborted).toBe(native);
});
