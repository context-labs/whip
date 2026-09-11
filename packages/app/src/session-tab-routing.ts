import type { AnyRouter } from '@tanstack/react-router';
import type { AppRuntime } from './runtime';
import { selectedSessionTab, sessionSearch, validateSessionSearch, type SessionTab, type NewChatTab } from './session-tabs';

declare module '@tanstack/react-router' {
  interface HistoryState { whipViewId?: string }
}

export function sessionDestination(pathname: string): { runtimeId: string; rootId: string } | undefined {
  const match = /^\/h\/([^/]+)\/s\/([^/]+)\/?$/.exec(pathname);
  if (!match) return;
  try { return { runtimeId: decodeURIComponent(match[1]!), rootId: decodeURIComponent(match[2]!) }; }
  catch { return; }
}

export function terminalDestination(pathname: string): { runtimeId: string; terminalId: string } | undefined {
  const match = /^\/h\/([^/]+)\/t\/([^/]+)\/?$/.exec(pathname);
  if (!match) return;
  try { return { runtimeId: decodeURIComponent(match[1]!), terminalId: decodeURIComponent(match[2]!) }; }
  catch { return; }
}

export function draftDestination(pathname: string): string | undefined {
  const match = /^\/new\/([^/]+)\/?$/.exec(pathname);
  if (!match) return;
  try { return decodeURIComponent(match[1]!); } catch { return; }
}

export function tabDestination(tab: SessionTab) {
  if (tab.kind === 'new') return { to: '/new/$draftId' as const, params: { draftId: tab.id }, search: {}, state: { whipViewId: tab.id } };
  if (tab.kind === 'terminal') return { to: '/h/$runtimeId/t/$terminalId' as const, params: { runtimeId: tab.runtimeId, terminalId: tab.terminalId }, search: {}, state: { whipViewId: tab.id } };
  return { to: '/h/$runtimeId/s/$rootId' as const, params: { runtimeId: tab.runtimeId, rootId: tab.rootId }, search: sessionSearch(tab), state: { whipViewId: tab.id } };
}

/** Explicit creation intent: start a shell on the host, then open and select its tab. */
export async function openTerminalTab(runtime: AppRuntime, navigate: AnyRouter['navigate'], options: { runtimeId: string; cwd?: string; rootId?: string; paneId?: string }) {
  try {
    const client = runtime.connections.host(options.runtimeId)?.client;
    const connection = client?.getSnapshot();
    if (!client || connection?.state !== 'connected') throw new Error('Connect this host before opening a terminal.');
    if (!connection.info?.capabilities?.includes('terminals')) throw new Error('This host\'s Whip does not offer terminals. Update it to a build with protocol 6.5 or newer.');
    if (!runtime.tabs.canOpen()) throw new Error('There are 32 open session tabs. Close a tab before opening a terminal.');
    const opened = await client.terminals.open({ ...(options.cwd ? { cwd: options.cwd } : {}), ...(options.rootId ? { rootId: options.rootId } : {}), cols: 80, rows: 24 });
    const id = runtime.tabs.openTerminal(options.runtimeId, opened.id, opened.cwd, options.paneId);
    const tab = runtime.tabs.workspace().tabs.find(item => item.id === id)!;
    await navigate(tabDestination(tab));
    return id;
  } catch (error) { runtime.reportWorkspace(error); }
}

/** Explicit view opening creates an adjacent REPL; URL observation only selects it. */
export async function openSessionView(runtime: AppRuntime, navigate: AnyRouter['navigate'], sourceId: string, kind: 'chat' | 'repl') {
  try {
    const tab = runtime.tabs.openRelated(sourceId, kind);
    await navigate(tabDestination(tab));
    requestAnimationFrame(() => {
      if (selectedSessionTab(runtime.tabs.workspace())?.id === tab.id)
        Array.from(document.querySelectorAll<HTMLElement>('[data-workspace-view]'))
          .find(panel => panel.dataset.workspaceView === tab.id)?.focus({ preventScroll: true });
    });
    return tab;
  } catch (error) { runtime.reportWorkspace(error); }
}

/** Allocate only for explicit creation intent; selecting a tab never calls this. */
export function openNewChat(runtime: AppRuntime, navigate: AnyRouter['navigate'], options: Partial<Pick<NewChatTab, 'hostProfileId' | 'runtimeId' | 'cwd' | 'permissionMode'>> = {}, replace = false) {
  try {
    const state = runtime.getSnapshot();
    const host = state.hosts.find(host => options.hostProfileId ? host.id === options.hostProfileId : options.runtimeId ? host.runtimeId === options.runtimeId : host.id === state.selectedHostId);
    const tab = runtime.tabs.openNew({ ...options, hostProfileId: options.hostProfileId ?? host?.id, runtimeId: options.runtimeId ?? host?.runtimeId });
    void navigate({ ...tabDestination(tab), replace }).catch(error => runtime.reportWorkspace(error));
    return tab;
  } catch (error) { runtime.reportWorkspace(error); }
}

/** A native link can select an already saved host, but cannot create one. */
export function createSessionNavigator(runtime: AppRuntime, navigate: (path: string) => void, currentLocation?: () => unknown) {
  let epoch = 0; let disposed = false;
  return {
    async open(path: string) {
      if (disposed) return;
      const current = ++epoch;
      const location = currentLocation?.();
      try {
        const url = new URL(path, 'https://whip.invalid');
        const destination = sessionDestination(url.pathname);
        if (!path.startsWith('/h/') || url.origin !== 'https://whip.invalid' || !destination || url.hash)
          throw new Error('Invalid session link');
        const state = runtime.getSnapshot();
        const profile = state.hosts.find(host => host.runtimeId === destination.runtimeId);
        if (!profile) throw new Error('This session belongs to an unknown execution host. Connect to that host first, then open this link again.');
        const connection = profile.client?.getSnapshot();
        if (connection?.state !== 'connected' || connection.info?.runtime_id !== destination.runtimeId) {
          // connect owns its error state and suppresses failures from a retired
          // host attempt. Reporting that rejection here would undo its guard.
          try { await runtime.connections.connect(profile.id); } catch { return; }
        }
        const latest = runtime.connections.host(destination.runtimeId); const attached = latest?.client?.getSnapshot();
        if (current !== epoch || currentLocation?.() !== location || latest?.id !== profile.id || attached?.state !== 'connected' || attached.info?.runtime_id !== destination.runtimeId) return;
        navigate(url.pathname + url.search);
      } catch (error) { if (current === epoch) runtime.reportWorkspace(error); }
    },
    dispose() { disposed = true; ++epoch; },
  };
}

/** The router is the active-tab authority; saved selection is only a boot hint. */
export function bindSessionTabs(runtime: AppRuntime, router: AnyRouter) {
  const initial = router.state.location;
  let observing = false;
  let disposed = false;
  let capacityNotice: string | undefined;
  let observedLocation: typeof initial | undefined;
  const observe = () => {
    if (observing || disposed) return;
    observing = true;
    try {
      const current = router.state.location;
      const destination = sessionDestination(current.pathname);
      // Runtime notices and command completions are not navigation. In
      // particular, a storage warning during close must not reopen its old URL.
      if (current === observedLocation && !capacityNotice) return;
      observedLocation = current;
      runtime.clearWorkspaceError();
      if (capacityNotice !== current.href) capacityNotice = undefined;
      const terminal = terminalDestination(current.pathname);
      if (terminal) {
        // A terminal URL selects an open tab; it never starts a shell, so a
        // stale link shows the missing state instead of creating one.
        const tab = runtime.tabs.workspace().tabs.find(tab => tab.kind === 'terminal' && tab.runtimeId === terminal.runtimeId && tab.terminalId === terminal.terminalId);
        if (tab) runtime.tabs.activate(tab.id);
        return;
      }
      const draftId = draftDestination(current.pathname);
      if (draftId) {
        let tab = runtime.tabs.workspace().tabs.find(tab => tab.id === draftId);
        if (!tab && runtime.tabs.workspace().closed.some(record => record.tab.id === draftId)) {
          runtime.tabs.reopenView(draftId);
          tab = runtime.tabs.workspace().tabs.find(tab => tab.id === draftId);
        }
        if (tab) {
          runtime.tabs.activate(tab.id);
          if (tab.kind !== 'new') void router.navigate({ ...tabDestination(tab), replace: true }).catch(error => runtime.reportWorkspace(error));
        }
      } else if (destination) {
        const search = current.search as Record<string, unknown>;
        if (!runtime.tabs.canOpen(destination.runtimeId, destination.rootId)) {
          if (capacityNotice !== current.href) {
            capacityNotice = current.href;
            runtime.reportWorkspace('There are 32 open session tabs. Close a tab to open this session.');
          }
          return;
        }
        runtime.tabs.visit(destination.runtimeId, destination.rootId, validateSessionSearch(search), typeof current.state.whipViewId === 'string' ? current.state.whipViewId : undefined);
        runtime.rememberSession(destination.runtimeId, destination.rootId);
        capacityNotice = undefined;
      } else if (current.pathname === '/') {
        const search = current.search as { new?: 1 | '1'; cwd?: string; runtimeId?: string };
        if (search.new === 1 || search.new === '1' || search.runtimeId || search.cwd) {
          openNewChat(runtime, router.navigate, { runtimeId: search.runtimeId, cwd: search.cwd }, true);
          return;
        }
        const tab = selectedSessionTab(runtime.tabs.workspace());
        if (tab) {
          void router.navigate({ ...tabDestination(tab), replace: true }).catch(error => runtime.reportWorkspace(error));
          return;
        }
        runtime.tabs.home();
      }
    } catch (error) { runtime.reportWorkspace(error); }
    finally { observing = false; }
  };
  const offRuntime = runtime.subscribe(observe);
  const offRoute = router.subscribe('onResolved', observe);
  // Closing another tab may make room for a directly linked 33rd session.
  const offTabs = runtime.tabs.subscribe(() => {
    if (capacityNotice) observe();
    const draftId = draftDestination(router.state.location.pathname);
    const tab = selectedSessionTab(runtime.tabs.workspace());
    // An accepted background/closed draft never steals the person's place.
    if (!observing && draftId && tab?.id === draftId && tab.kind !== 'new')
      void router.navigate({ ...tabDestination(tab), replace: true }).catch(error => runtime.reportWorkspace(error));
  });
  observe();
  return () => { disposed = true; offRuntime(); offRoute(); offTabs(); };
}
