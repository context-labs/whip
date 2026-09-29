import { useLayoutEffect, useRef, useState } from 'react';
import type { Client } from '@whip/sdk';
import type { ExecutionView, SessionView, TraceView } from '@whip/sdk/state';
import type { AppRuntime } from './runtime';
import type { HostConnection } from './hosts';

export interface WorkspaceRoot { runtimeId: string; rootId: string; sessionId?: string; client?: Client; recovering?: boolean }
export const workspaceRootKey = (root: Pick<WorkspaceRoot, 'runtimeId' | 'rootId'>) => JSON.stringify([root.runtimeId, root.rootId]);
export const workspaceSessionKey = (owner: Pick<WorkspaceRoot, 'runtimeId' | 'rootId' | 'sessionId'>) => JSON.stringify([owner.runtimeId, owner.rootId, owner.sessionId ?? owner.rootId]);
type Lease = ReturnType<AppRuntime['acquireView']>;
export interface WorkspaceLease { client: Client; lease: Lease }
/** Release obsolete session views before admission, including a switch with four live panes. */
export function reconcileWorkspaceViews(runtime: Pick<AppRuntime, 'acquireView'>, leases: Map<string, WorkspaceLease>, roots: readonly WorkspaceRoot[]) {
  const wanted = new Map(roots.map(root => [workspaceSessionKey(root), root]));
  for (const [key, entry] of leases) {
    const next = wanted.get(key);
    // A recovering host suspends its SDK views. Keep the visible owned lease so
    // retained content and local interaction state survive without any new reads.
    if (!next || (next.client ? next.client !== entry.client : !next.recovering)) {
      entry.lease.release(); leases.delete(key);
    }
  }
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
  const [state, setState] = useState<{ clients: ReadonlyMap<string, Client>; views: ReadonlyMap<string, SessionView>; executions: ReadonlyMap<string, ExecutionView>; errors: ReadonlyMap<string, string> }>({ clients: new Map(), views: new Map(), executions: new Map(), errors: new Map() });
  useLayoutEffect(() => () => {
    for (const entry of leases.current.values()) entry.lease.release();
    leases.current.clear();
  }, [runtime]);
  useLayoutEffect(() => {
    const wanted = (JSON.parse(key) as string[]).map(value => {
      const [runtimeId, rootId, sessionId] = JSON.parse(value) as [string, string, string];
      const host = hosts.find(host => host.runtimeId === runtimeId);
      return { runtimeId, rootId, sessionId, client: host?.client, recovering: host?.state === 'stale' || host?.state === 'connecting' };
    });
    const errors = reconcileWorkspaceViews(runtime, leases.current, wanted);
    setState({ clients: new Map([...leases.current].map(([id, entry]) => [id, entry.client])), views: new Map([...leases.current].map(([id, entry]) => [id, entry.lease.view])), executions: new Map([...leases.current].map(([id, entry]) => [id, entry.lease.execution])), errors });
  }, [runtime, hosts, key]);
  return state;
}


interface WorkspaceTrace { runtimeId: string; rootId: string; viewId: string }
export const workspaceTraceKey = (owner: WorkspaceTrace) => JSON.stringify([owner.runtimeId, owner.rootId, owner.viewId]);
/** Visible trace panes retain independent filters/pages even for the same root. */
export function useWorkspaceTraces(runtime: AppRuntime, owners: readonly WorkspaceTrace[], hosts: readonly HostConnection[]) {
  const leases = useRef(new Map<string, { client: Client; lease: ReturnType<AppRuntime['acquireTrace']> }>());
  const key = JSON.stringify([...new Set(owners.map(workspaceTraceKey))].sort());
  const [state, setState] = useState<{ clients: ReadonlyMap<string, Client>; views: ReadonlyMap<string, TraceView>; errors: ReadonlyMap<string, string> }>({ clients: new Map(), views: new Map(), errors: new Map() });
  useLayoutEffect(() => () => {
    for (const entry of leases.current.values()) entry.lease.release();
    leases.current.clear();
  }, [runtime]);
  useLayoutEffect(() => {
    const wanted = new Map((JSON.parse(key) as string[]).map(value => {
      const [runtimeId, rootId, viewId] = JSON.parse(value) as [string, string, string];
      return [value, { runtimeId, rootId, viewId, client: hosts.find(host => host.runtimeId === runtimeId)?.client }] as const;
    }));
    for (const [id, entry] of leases.current) if (wanted.get(id)?.client !== entry.client) { entry.lease.release(); leases.current.delete(id); }
    const errors = new Map<string, string>();
    for (const [id, owner] of wanted) if (!leases.current.has(id)) {
      if (!owner.client) { errors.set(id, 'Connect the original host to inspect this trace.'); continue; }
      try { leases.current.set(id, { client: owner.client, lease: runtime.acquireTrace(owner.runtimeId, owner.rootId, owner.viewId) }); }
      catch (error) { errors.set(id, error instanceof Error ? error.message : String(error)); }
    }
    setState({ clients: new Map([...leases.current].map(([id, entry]) => [id, entry.client])), views: new Map([...leases.current].map(([id, entry]) => [id, entry.lease.view])), errors });
  }, [runtime, hosts, key]);
  return state;
}
