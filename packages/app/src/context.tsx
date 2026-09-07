import { createContext, useContext, useSyncExternalStore } from 'react';
import type { AppRuntime } from './runtime';

export const RuntimeContext = createContext<AppRuntime | null>(null);
export function useRuntime() {
  const value = useContext(RuntimeContext);
  if (!value) throw new Error('WHIP application context is missing');
  return value;
}
export function useAppState() {
  const runtime = useRuntime();
  return useSyncExternalStore(runtime.subscribe, runtime.getSnapshot, runtime.getSnapshot);
}
export function useSessionTabs() {
  const { tabs } = useRuntime();
  return useSyncExternalStore(tabs.subscribe, tabs.getSnapshot, tabs.getSnapshot);
}
