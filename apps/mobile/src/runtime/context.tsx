import { createContext, useContext, useEffect, useState, useSyncExternalStore, type PropsWithChildren } from 'react';
import { QueryClientProvider } from '@tanstack/react-query';
import { useIsFocused } from 'expo-router';
import type { DeepReadonly, SessionListSnapshot, SessionView } from '@whip/sdk/state';
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
const emptyList: DeepReadonly<SessionListSnapshot> = Object.freeze({ status: 'idle', truncated: false });
const noop = () => () => {};
export function useSessionList() {
  const { list, active } = useRuntimeState();
  const focused = useIsFocused();
  return useSyncExternalStore(focused && active && list ? list.subscribe : noop, list ? list.getSnapshot : () => emptyList);
}
export function useRootView(rootId: string, runtimeId: string) {
  const runtime = useRuntime();
  const { client } = useRuntimeState();
  const focused = useIsFocused();
  const [view, setView] = useState<SessionView>();
  useEffect(() => {
    if (!client || !focused || !rootId || !runtimeId) { setView(undefined); return; }
    try {
      const held = runtime.acquireView(rootId, runtimeId); setView(held.view);
      return () => { setView(undefined); held.release(); };
    } catch (error) { runtime.report(error); }
  }, [client, focused, rootId, runtimeId, runtime]);
  return view;
}
