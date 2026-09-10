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

export function draftDestination(pathname: string): string | undefined {
  const match = /^\/new\/([^/]+)\/?$/.exec(pathname);
  if (!match) return;
  try { return decodeURIComponent(match[1]!); } catch { return; }
}

export function tabDestination(tab: SessionTab) {
  return tab.kind === 'new'
    ? { to: '/new/$draftId' as const, params: { draftId: tab.id }, search: {}, state: { whipViewId: tab.id } }
    : { to: '/h/$runtimeId/s/$rootId' as const, params: { runtimeId: tab.runtimeId, rootId: tab.rootId }, search: sessionSearch(tab), state: { whipViewId: tab.id } };
}

/** Allocate only for explicit creation intent; selecting a tab never calls this. */
export function openNewChat(runtime: AppRuntime, navigate: AnyRouter['navigate'], options: Partial<Pick<NewChatTab, 'hostProfileId' | 'runtimeId' | 'cwd' | 'permissionMode'>> = {}, replace = false) {
  try {
    const state = runtime.getSnapshot();
    const host = state.hosts.find(host => options.hostProfileId ? host.id === options.hostProfileId : options.runtimeId ? host.runtimeId === options.runtimeId : host.id === state.selectedHostId);
    const tab = runtime.tabs.openNew({ ...options, hostProfileId: options.hostProfileId ?? host?.id, runtimeId: options.runtimeId ?? host?.runtimeId });
    void navigate({ ...tabDestination(tab), replace }).catch(error => runtime.report(error));
    return tab;
  } catch (error) { runtime.report(error); }
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
      } catch (error) { if (current === epoch) runtime.report(error); }
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
      if (capacityNotice !== current.href) capacityNotice = undefined;
      const draftId = draftDestination(current.pathname);
      if (draftId) {
        let tab = runtime.tabs.workspace().tabs.find(tab => tab.id === draftId);
        if (!tab && runtime.tabs.workspace().closed.some(record => record.tab.id === draftId)) {
          runtime.tabs.reopenView(draftId);
          tab = runtime.tabs.workspace().tabs.find(tab => tab.id === draftId);
        }
        if (tab) {
          runtime.tabs.activate(tab.id);
          if (tab.kind !== 'new') void router.navigate({ ...tabDestination(tab), replace: true }).catch(error => runtime.report(error));
        }
      } else if (destination) {
        const search = current.search as Record<string, unknown>;
        if (!runtime.tabs.canOpen(destination.runtimeId, destination.rootId)) {
          if (capacityNotice !== current.href) {
            capacityNotice = current.href;
            runtime.report('There are 32 open session tabs. Close a tab to open this session.');
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
          void router.navigate({ ...tabDestination(tab), replace: true }).catch(error => runtime.report(error));
          return;
        }
        runtime.tabs.home();
      }
    } catch (error) { runtime.report(error); }
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
      void router.navigate({ ...tabDestination(tab), replace: true }).catch(error => runtime.report(error));
  });
  observe();
  return () => { disposed = true; offRuntime(); offRoute(); offTabs(); };
}
