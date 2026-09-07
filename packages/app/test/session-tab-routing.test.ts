import { describe, expect, it, vi } from 'vitest';
import type { AnyRouter } from '@tanstack/react-router';
import type { AppRuntime } from '../src/runtime';
import { bindSessionTabs } from '../src/session-tab-routing';
import { SessionTabs } from '../src/session-tabs';
import type { AppStorage } from '../src/platform';

function fixture(path = '/', key = 'initial', storage?: AppStorage) {
  const runtimeEvents = new Set<() => void>();
  const tabs = new SessionTabs(storage, () => runtimeEvents.forEach(fn => fn()));
  const connectionEvents = new Set<() => void>(), routeEvents = new Set<() => void>();
  let connection = { state: 'connecting', info: undefined as { runtime_id: string } | undefined };
  const client = { getSnapshot: () => connection, subscribe: (fn: () => void) => { connectionEvents.add(fn); return () => connectionEvents.delete(fn); } };
  const runtime = { tabs, getSnapshot: () => ({ client }), subscribe: (fn: () => void) => { runtimeEvents.add(fn); return () => runtimeEvents.delete(fn); }, report: vi.fn(() => runtimeEvents.forEach(fn => fn())) } as unknown as AppRuntime;
  const router = {
    state: { location: { pathname: path, href: path, search: {} as Record<string, unknown>, state: { __TSR_key: key } } },
    subscribe: (_kind: string, fn: () => void) => { routeEvents.add(fn); return () => routeEvents.delete(fn); },
    navigate: vi.fn(async () => {}),
  };
  return { tabs, runtime, router,
    start: () => bindSessionTabs(runtime, router as unknown as AnyRouter),
    connect(runtimeId = 'mac') { connection = { state: 'connected', info: { runtime_id: runtimeId } }; connectionEvents.forEach(fn => fn()); },
    route(pathname: string, search: Record<string, unknown> = {}) { router.state.location = { pathname, href: pathname, search, state: { __TSR_key: crypto.randomUUID() } }; routeEvents.forEach(fn => fn()); },
  };
}
describe('tab route authority', () => {
  it('restores bare-home only once after host identity and preserves child/inspector', () => {
    const f = fixture(); f.tabs.visit('mac', 'root', { agent: 'child', panel: 'execution' }); const dispose = f.start();
    expect(f.router.navigate).not.toHaveBeenCalled(); f.connect();
    expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'root' }, search: { agent: 'child', panel: 'execution' }, replace: true });
    f.route('/h/mac/s/root', { agent: 'child' }); f.route('/'); f.connect();
    expect(f.router.navigate).toHaveBeenCalledTimes(1); expect(f.tabs.workspace('mac').lastActiveRootId).toBeUndefined(); dispose();
  });
  it('deep links beat saved selection and browser back reopens a closed session', () => {
    const f = fixture('/h/mac/s/linked'); f.tabs.visit('mac', 'saved', {}); const dispose = f.start(); f.connect();
    expect(f.router.navigate).not.toHaveBeenCalled(); expect(f.tabs.workspace('mac').lastActiveRootId).toBe('linked');
    f.route('/h/mac/s/saved'); f.tabs.close('mac', ['linked'], 'saved');
    f.route('/h/mac/s/linked'); expect(f.tabs.workspace('mac').tabs.filter(tab => tab.rootId === 'linked')).toHaveLength(1); dispose();
  });
  it('does not immediately reopen the active tab during its close transition', () => {
    const f = fixture('/h/mac/s/a'); f.tabs.open('mac', 'a'); f.tabs.open('mac', 'b'); const dispose = f.start(); f.connect();
    const next = f.tabs.close('mac', ['a'], 'a');
    expect(next).toBe('b'); expect(f.tabs.workspace('mac').tabs.map(tab => tab.rootId)).toEqual(['b']);
    f.route('/h/mac/s/b'); expect(f.tabs.workspace('mac').closed[0]?.tab.rootId).toBe('a'); dispose();
  });
  it('does not re-admit a closing URL when denied storage synchronously reports through runtime', () => {
    let denied = false;
    const disk: AppStorage = { keys: () => [], getItem: () => null, removeItem: () => {}, setItem: () => { if (denied) throw Error('storage denied'); } };
    const f = fixture('/h/mac/s/a', 'initial', disk);
    f.tabs.open('mac', 'a'); f.tabs.open('mac', 'b'); const dispose = f.start(); f.connect(); denied = true;
    expect(f.tabs.close('mac', ['a'], 'a')).toBe('b');
    f.runtime.report('A pending command also completed');
    expect(f.tabs.workspace('mac').tabs.map(tab => tab.rootId)).toEqual(['b']);
    expect(f.tabs.workspace('mac').closed.map(item => item.tab.rootId)).toEqual(['a']);
    f.route('/h/mac/s/b'); f.route('/h/mac/s/a');
    expect(f.tabs.workspace('mac').lastActiveRootId).toBe('a'); dispose();
  });
  it('admits an overflow deep link only after space is explicitly made', () => {
    const f = fixture('/h/mac/s/extra'); for (let i = 0; i < 32; i++) f.tabs.open('mac', `root${i}`); const dispose = f.start(); f.connect();
    expect(f.runtime.report).toHaveBeenCalledTimes(1); expect(f.tabs.workspace('mac').tabs.some(tab => tab.rootId === 'extra')).toBe(false);
    f.tabs.close('mac', ['root0']); expect(f.tabs.workspace('mac').tabs).toHaveLength(32); expect(f.tabs.workspace('mac').lastActiveRootId).toBe('extra'); dispose();
  });
  it('does not replace a navigation made while initial connection was pending', () => {
    const f = fixture(); f.tabs.visit('mac', 'saved', {}); const dispose = f.start(); f.route('/settings'); f.connect(); expect(f.router.navigate).not.toHaveBeenCalled(); dispose();
  });
});
