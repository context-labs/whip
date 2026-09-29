import { useLayoutEffect, useRef, useState } from 'react';
import type { Client } from '@whip/sdk';
import type { SessionView } from '@whip/sdk/state';
import type { AppRuntime } from './runtime';
import type { HostConnection } from './hosts';

export interface WorkspaceRoot { runtimeId: string; rootId: string; sessionId?: string; client?: Client }
export const workspaceRootKey = (root: Pick<WorkspaceRoot, 'runtimeId' | 'rootId'>) => JSON.stringify([root.runtimeId, root.rootId]);
export const workspaceSessionKey = (owner: Pick<WorkspaceRoot, 'runtimeId' | 'rootId' | 'sessionId'>) => JSON.stringify([owner.runtimeId, owner.rootId, owner.sessionId ?? owner.rootId]);
type Lease = ReturnType<AppRuntime['acquireView']>;
export interface WorkspaceLease { client: Client; lease: Lease }
/** Release obsolete session views before admission, including a switch with four live panes. */
export function reconcileWorkspaceViews(runtime: Pick<AppRuntime, 'acquireView'>, leases: Map<string, WorkspaceLease>, roots: readonly WorkspaceRoot[]) {
  const wanted = new Map(roots.map(root => [workspaceSessionKey(root), root]));
  for (const [key, entry] of leases) if (wanted.get(key)?.client !== entry.client) { entry.lease.release(); leases.delete(key); }
  const errors = new Map<string, string>();
  for (const [key, root] of wanted) if (!leases.has(key)) {
    if (!root.client) { errors.set(key, 'This host is disconnected or unavailable. Connect it in Settings → Servers.'); continue; }
    try { leases.set(key, { client: root.client, lease: runtime.acquireView(root.runtimeId, root.rootId, root.sessionId ?? root.rootId) }); }
    catch (error) { errors.set(key, error instanceof Error ? error.message : String(error)); }
  }
  return errors;
}
export function useWorkspaceViews(runtime: AppRuntime, roots: readonly Pick<WorkspaceRoot, 'runtimeId' | 'rootId' | 'sessionId'>[], hosts: readonly HostConnection[]) {
  const leases = useRef(new Map<string, WorkspaceLease>());
  const key = JSON.stringify([...new Set(roots.map(workspaceSessionKey))].sort());
  const [state, setState] = useState<{ views: ReadonlyMap<string, SessionView>; errors: ReadonlyMap<string, string> }>({ views: new Map(), errors: new Map() });
  useLayoutEffect(() => () => {
    for (const entry of leases.current.values()) entry.lease.release();
    leases.current.clear();
  }, [runtime]);
  useLayoutEffect(() => {
    const wanted = (JSON.parse(key) as string[]).map(value => {
      const [runtimeId, rootId, sessionId] = JSON.parse(value) as [string, string, string];
      return { runtimeId, rootId, sessionId, client: hosts.find(host => host.runtimeId === runtimeId)?.client };
    });
    const errors = reconcileWorkspaceViews(runtime, leases.current, wanted);
    setState({ views: new Map([...leases.current].map(([id, entry]) => [id, entry.lease.view])), errors });
  }, [runtime, hosts, key]);
  return state;
}
