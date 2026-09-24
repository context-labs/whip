/** @jest-environment node */
import { waitForReady } from './wait-ready';
import type { MobileRuntime } from './runtime';
import type { WhipClient } from '@whip/sdk';
function fixture() {
  const client = {} as WhipClient; let state = { client, ready: false, active: false }; const listeners = new Set<() => void>();
  return { client, listeners, runtime: { getSnapshot: () => state, subscribe: (fn: () => void) => { listeners.add(fn); return () => listeners.delete(fn); } } as unknown as MobileRuntime,
    update: (patch: Partial<typeof state>) => { state = { ...state, ...patch }; listeners.forEach(fn => fn()); } };
}
test('file-picker return waits for foreground reconciliation and releases its observer', async () => {
  const f = fixture(); const ready = waitForReady(f.runtime, f.client, new AbortController().signal);
  f.update({ active: true }); expect(f.listeners.size).toBe(1); f.update({ ready: true }); await ready; expect(f.listeners.size).toBe(0);
});
test('host replacement and cancellation reject instead of resolving through another host', async () => {
  const f = fixture(); const changed = expect(waitForReady(f.runtime, f.client, new AbortController().signal)).rejects.toThrow('source changed');
  f.update({ client: {} as WhipClient, ready: true, active: true }); await changed; expect(f.listeners.size).toBe(0);
  const next = fixture(); const controller = new AbortController(); const aborted = expect(waitForReady(next.runtime, next.client, controller.signal)).rejects.toThrow('cancelled'); controller.abort(); await aborted; expect(next.listeners.size).toBe(0);
});
