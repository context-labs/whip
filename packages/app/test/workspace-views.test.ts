import { describe, expect, it, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import type { HostConnection } from '../src/hosts';
import type { AppRuntime } from '../src/runtime';
import type { Client } from '@whip/sdk';
import { reconcileWorkspaceViews, useWorkspaceTraces, workspaceTraceKey, workspaceSessionKey, type WorkspaceLease } from '../src/workspace-views';

type Lease = ReturnType<AppRuntime['acquireView']>;
const local = {} as Client, remote = {} as Client;
const root = (rootId: string, runtimeId = 'local', client: Client | undefined = local) => ({ runtimeId, rootId, client });
const key = (id: string, runtimeId = 'local') => workspaceSessionKey(root(id, runtimeId));
function fixture() {
  const active = new Set<string>();
  const events: string[] = [];
  const acquireView = vi.fn((runtimeId: string, id: string, sessionId = id): Lease => {
    if (active.size === 4) throw new Error('Four session views');
    const k = workspaceSessionKey({ runtimeId, rootId: id, sessionId });
    active.add(k); events.push(`acquire:${k}`);
    return { view: { id } as unknown as Lease['view'], execution: { id } as unknown as Lease['execution'], release: vi.fn(() => { active.delete(k); events.push(`release:${k}`); }) };
  });
  return { active, events, runtime: { acquireView }, leases: new Map<string, WorkspaceLease>() };
}
describe('workspace session reconciliation', () => {
  it('releases obsolete roots before admitting replacements at the window-wide limit', () => {
    const f = fixture();
    reconcileWorkspaceViews(f.runtime, f.leases, ['a', 'b', 'c', 'd'].map(id => root(id)));
    const retained = f.leases.get(key('b')); f.events.length = 0;
    expect(reconcileWorkspaceViews(f.runtime, f.leases, [root('e', 'remote', remote), ...['b', 'c', 'd'].map(id => root(id))]).size).toBe(0);
    expect(f.events).toEqual([`release:${key('a')}`, `acquire:${key('e', 'remote')}`]);
    expect(f.leases.get(key('b'))).toBe(retained);
    expect(f.active.size).toBe(4);
  });
  it('shares duplicates only on the same host, retaining other hosts through disconnect and reconnect', () => {
    const f = fixture();
    reconcileWorkspaceViews(f.runtime, f.leases, [root('a'), root('a'), root('a', 'remote', remote)]);
    expect(f.runtime.acquireView).toHaveBeenCalledTimes(2);
    const a = f.leases.get(key('a'))!, b = f.leases.get(key('a', 'remote'))!;
    reconcileWorkspaceViews(f.runtime, f.leases, [root('a'), { ...root('a', 'remote'), client: undefined }]);
    expect(a.lease.release).not.toHaveBeenCalled();
    expect(b.lease.release).toHaveBeenCalledTimes(1);
    reconcileWorkspaceViews(f.runtime, f.leases, [root('a'), root('a', 'remote', {} as Client)]);
    expect(f.leases.get(key('a'))).toBe(a);
    expect(f.leases.get(key('a', 'remote'))).not.toBe(b);
    reconcileWorkspaceViews(f.runtime, f.leases, []);
    expect(a.lease.release).toHaveBeenCalledTimes(1);
    expect(f.active.size).toBe(0); expect(f.leases.size).toBe(0);
  });
  it('reports failed admissions without dropping healthy roots and permits retry', () => {
    const f = fixture();
    f.runtime.acquireView.mockImplementationOnce(() => { throw new Error('Root unavailable'); });
    const errors = reconcileWorkspaceViews(f.runtime, f.leases, [root('a'), root('b')]);
    expect([...errors]).toEqual([[key('a'), 'Root unavailable']]);
    expect([...f.leases.keys()]).toEqual([key('b')]);
    const b = f.leases.get(key('b'));
    expect(reconcileWorkspaceViews(f.runtime, f.leases, [root('a'), root('b')]).size).toBe(0);
    expect(f.leases.get(key('b'))).toBe(b);
  });
  it('uses one uniform view per exact root or child, while metadata remains rooted in its tree', () => {
    const f = fixture();
    const parent = root('root');
    const child = { ...parent, sessionId: 'child' };
    reconcileWorkspaceViews(f.runtime, f.leases, [parent, child, child]);
    expect(f.runtime.acquireView.mock.calls).toEqual([['local', 'root', 'root'], ['local', 'root', 'child']]);
    expect(f.leases.size).toBe(2);
    const parentLease = f.leases.get(workspaceSessionKey(parent))!;
    const childLease = f.leases.get(workspaceSessionKey(child))!;
    reconcileWorkspaceViews(f.runtime, f.leases, [parent, { ...parent, sessionId: 'other-child' }]);
    expect(parentLease.lease.release).not.toHaveBeenCalled();
    expect(childLease.lease.release).toHaveBeenCalledTimes(1);
    expect(f.leases.has(workspaceSessionKey({ ...parent, sessionId: 'other-child' }))).toBe(true);
  });

});


it('leases only visible trace panes and releases old owners before admission', () => {
  const events: string[] = [];
  const acquireTrace = vi.fn((runtimeId: string, rootId: string, viewId: string) => {
    const id = workspaceTraceKey({ runtimeId, rootId, viewId });
    events.push(`acquire:${id}`);
    return { view: { id } as unknown as ReturnType<AppRuntime['acquireTrace']>['view'], release: vi.fn(() => { events.push(`release:${id}`); }) };
  });
  const runtime = { acquireTrace } as unknown as AppRuntime;
  const first = { runtimeId: 'local', rootId: 'root', viewId: 'a' }, second = { ...first, viewId: 'b' };
  const hosts = [{ runtimeId: 'local', client: local }] as HostConnection[];
  const hook = renderHook(({ owners, hosts }) => useWorkspaceTraces(runtime, owners, hosts), { initialProps: { owners: [first, first, second], hosts } });
  expect(acquireTrace).toHaveBeenCalledTimes(2);
  expect(hook.result.current.views.size).toBe(2);
  const firstView = hook.result.current.views.get(workspaceTraceKey(first));
  events.length = 0;
  const next = { ...second, rootId: 'other' };
  hook.rerender({ owners: [first, next], hosts });
  expect(events).toEqual([`release:${workspaceTraceKey(second)}`, `acquire:${workspaceTraceKey(next)}`]);
  expect(hook.result.current.views.get(workspaceTraceKey(first))).toBe(firstView);
  events.length = 0;
  hook.rerender({ owners: [first], hosts: [{ runtimeId: 'local' }] as HostConnection[] });
  expect(events).toEqual([`release:${workspaceTraceKey(first)}`, `release:${workspaceTraceKey(next)}`]);
  expect(hook.result.current.views.size).toBe(0);
  expect(hook.result.current.errors.get(workspaceTraceKey(first))).toContain('original host');
  hook.rerender({ owners: [first], hosts: [{ runtimeId: 'local', client: remote }] as HostConnection[] });
  expect(hook.result.current.views.size).toBe(1); expect(hook.result.current.errors.size).toBe(0);
  const last = acquireTrace.mock.results.at(-1)!.value;
  hook.unmount(); expect(last.release).toHaveBeenCalledOnce();
});
