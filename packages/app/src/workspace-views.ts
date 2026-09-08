import { useLayoutEffect, useRef, useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { SessionView } from '@whip/sdk/state';
import type { AppRuntime } from './runtime';

type Lease = ReturnType<AppRuntime['acquireView']>;
/** Release obsolete roots before admission, including a switch with four live panes. */
export function reconcileWorkspaceViews(runtime: Pick<AppRuntime, 'acquireView'>, leases: Map<string, Lease>, roots: readonly string[]) {
  const wanted = new Set(roots);
  for (const [id, lease] of leases) if (!wanted.has(id)) { lease.release(); leases.delete(id); }
  const errors = new Map<string, string>();
  for (const id of wanted) if (!leases.has(id)) {
    try { leases.set(id, runtime.acquireView(id)); }
    catch (error) { errors.set(id, error instanceof Error ? error.message : String(error)); }
  }
  return errors;
}
export function useWorkspaceViews(runtime: AppRuntime, client: WhipClient, runtimeId: string, roots: readonly string[]) {
  const leases = useRef(new Map<string, Lease>());
  const key = JSON.stringify([...new Set(roots)].sort());
  const [state, setState] = useState<{ views: ReadonlyMap<string, SessionView>; errors: ReadonlyMap<string, string> }>({ views: new Map(), errors: new Map() });
  useLayoutEffect(() => () => {
    for (const lease of leases.current.values()) lease.release();
    leases.current.clear();
  }, [client, runtime, runtimeId]);
  useLayoutEffect(() => {
    const errors = reconcileWorkspaceViews(runtime, leases.current, JSON.parse(key) as string[]);
    setState({ views: new Map([...leases.current].map(([id, lease]) => [id, lease.view])), errors });
  }, [client, runtime, runtimeId, key]);
  return state;
}
