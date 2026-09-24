import { createContext, useContext, useSyncExternalStore, type PropsWithChildren } from 'react';
import { MobileWorkspace } from './workspace';
const Context = createContext<MobileWorkspace | null>(null);
export function WorkspaceProvider({ workspace, children }: PropsWithChildren<{ workspace: MobileWorkspace }>) { return <Context.Provider value={workspace}>{children}</Context.Provider>; }
export function useWorkspace() { const workspace = useContext(Context); if (!workspace) throw new Error('Mobile workspace is not ready'); return workspace; }
export function useWorkspaceState() { const workspace = useWorkspace(); return useSyncExternalStore(workspace.subscribe, workspace.getSnapshot); }
