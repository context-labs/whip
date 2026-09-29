import { createContext, useContext, useEffect, useState, useSyncExternalStore, type PropsWithChildren } from 'react';
import { QueryClientProvider } from '@tanstack/react-query';
import { useIsFocused } from 'expo-router';
import type { SessionLease } from './runtime';
import { MobileRuntime } from './runtime';
import { NativeTheme } from '../theme/theme';
import { WorkspaceAttentionProvider } from '../features/workspace-index';

const RuntimeContext = createContext<MobileRuntime | null>(null);
export function RuntimeProvider({ runtime, children }: PropsWithChildren<{ runtime: MobileRuntime }>) {
  const state = useSyncExternalStore(runtime.subscribe, runtime.getSnapshot);
  return <RuntimeContext.Provider value={runtime}><QueryClientProvider client={runtime.query}><WorkspaceAttentionProvider><NativeTheme appearance={state.appearance} themes={state.customThemes}>{children}</NativeTheme></WorkspaceAttentionProvider></QueryClientProvider></RuntimeContext.Provider>;
}
export function RuntimeScope({ runtime, children }: PropsWithChildren<{ runtime: MobileRuntime }>) { return <RuntimeContext.Provider value={runtime}><QueryClientProvider client={runtime.query}>{children}</QueryClientProvider></RuntimeContext.Provider>; }
export function useRuntime() { const runtime = useContext(RuntimeContext); if (!runtime) throw new Error('Mobile runtime is not ready'); return runtime; }
export function useRuntimeState() { const runtime = useRuntime(); return useSyncExternalStore(runtime.subscribe, runtime.getSnapshot); }
export function useSessionOwner(sessionId: string, runtimeId: string) {
  const runtime = useRuntime(); const focused = useIsFocused();
  const [lease, setLease] = useState<SessionLease>();
  useEffect(() => {
    let held: SessionLease | undefined;
    const inspect = () => {
      if (held?.view.getSnapshot().status === 'closed') { held.release(); held = undefined; setLease(undefined); }
      if (held || !focused || !sessionId || !runtimeId || runtime.getSnapshot().client?.runtimeID !== runtimeId) return;
      try { held = runtime.acquireView(sessionId, runtimeId); setLease(held); } catch (error) { runtime.report(error); }
    };
    inspect(); const unsubscribe = runtime.subscribe(inspect);
    return () => { unsubscribe(); held?.release(); setLease(undefined); };
  }, [focused, sessionId, runtimeId, runtime]);
  return lease;
}
