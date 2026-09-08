import { describe, expect, it, vi } from 'vitest';
import type { AppRuntime } from '../src/runtime';
import { reconcileWorkspaceViews } from '../src/workspace-views';

type Lease = ReturnType<AppRuntime['acquireView']>;
function fixture() {
  const active = new Set<string>();
  const events: string[] = [];
  const acquireView = vi.fn((id: string): Lease => {
    if (active.size === 4) throw new Error('Four session views');
    active.add(id); events.push(`acquire:${id}`);
    return { view: { id } as unknown as Lease['view'], release: vi.fn(() => { active.delete(id); events.push(`release:${id}`); }) };
  });
  return { active, events, runtime: { acquireView }, leases: new Map<string, Lease>() };
}
describe('workspace root reconciliation', () => {
  it('releases an obsolete root before admitting its replacement at four active roots', () => {
    const f = fixture();
    reconcileWorkspaceViews(f.runtime, f.leases, ['a', 'b', 'c', 'd']);
    const retained = f.leases.get('b'); f.events.length = 0;
    expect(reconcileWorkspaceViews(f.runtime, f.leases, ['e', 'b', 'c', 'd']).size).toBe(0);
    expect(f.events).toEqual(['release:a', 'acquire:e']);
    expect(f.leases.get('b')).toBe(retained);
    expect([...f.active].sort()).toEqual(['b', 'c', 'd', 'e']);
  });
  it('shares duplicate roots and releases each only after its last pane disappears', () => {
    const f = fixture();
    reconcileWorkspaceViews(f.runtime, f.leases, ['a', 'a', 'b']);
    expect(f.runtime.acquireView).toHaveBeenCalledTimes(2);
    const a = f.leases.get('a')!;
    reconcileWorkspaceViews(f.runtime, f.leases, ['a', 'b']);
    expect(a.release).not.toHaveBeenCalled();
    reconcileWorkspaceViews(f.runtime, f.leases, ['b']);
    expect(a.release).toHaveBeenCalledTimes(1);
    reconcileWorkspaceViews(f.runtime, f.leases, []);
    expect(f.active.size).toBe(0); expect(f.leases.size).toBe(0);
  });
  it('reports failed admissions without dropping already admitted roots and permits retry', () => {
    const f = fixture();
    f.runtime.acquireView.mockImplementationOnce(() => { throw new Error('Root unavailable'); });
    const errors = reconcileWorkspaceViews(f.runtime, f.leases, ['a', 'b']);
    expect([...errors]).toEqual([['a', 'Root unavailable']]);
    expect([...f.leases.keys()]).toEqual(['b']);
    const b = f.leases.get('b');
    expect(reconcileWorkspaceViews(f.runtime, f.leases, ['a', 'b']).size).toBe(0);
    expect(f.leases.get('b')).toBe(b);
  });
});
