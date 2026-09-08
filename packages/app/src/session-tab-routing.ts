import type { AnyRouter } from '@tanstack/react-router';
import type { AppRuntime } from './runtime';
import { selectedSessionTab, sessionSearch, validateSessionSearch } from './session-tabs';

declare module '@tanstack/react-router' {
  interface HistoryState { whipViewId?: string }
}

export function sessionDestination(pathname: string): { runtimeId: string; rootId: string } | undefined {
  const match = /^\/h\/([^/]+)\/s\/([^/]+)\/?$/.exec(pathname);
  if (!match) return;
  try { return { runtimeId: decodeURIComponent(match[1]!), rootId: decodeURIComponent(match[2]!) }; }
  catch { return; }
}

/** A native link can select an already saved host, but cannot create one. */
export function createSessionNavigator(runtime: AppRuntime, navigate: (path: string) => void) {
  let epoch = 0; let disposed = false;
  return {
    async open(path: string) {
      if (disposed) return;
      const current = ++epoch;
      try {
        const url = new URL(path, 'https://whip.invalid');
        const destination = sessionDestination(url.pathname);
        if (!path.startsWith('/h/') || url.origin !== 'https://whip.invalid' || !destination || url.hash)
          throw new Error('Invalid session link');
        const state = runtime.getSnapshot();
        const profile = state.connection.runtimeId === destination.runtimeId ? state.connection : state.hosts.find(host => host.runtimeId === destination.runtimeId);
        if (!profile) throw new Error('This session belongs to an unknown execution host. Connect to that host first, then open this link again.');
        const connection = state.client?.getSnapshot();
        if (connection?.state !== 'connected' || connection.info?.runtime_id !== destination.runtimeId) {
          // connect owns its error state and suppresses failures from a retired
          // host attempt. Reporting that rejection here would undo its guard.
          try { await runtime.connect(profile); } catch { return; }
        }
        const latest = runtime.getSnapshot(); const attached = latest.client?.getSnapshot();
        if (current !== epoch || latest.connection.id !== profile.id || attached?.state !== 'connected' || attached.info?.runtime_id !== destination.runtimeId) return;
        navigate(url.pathname + url.search);
      } catch (error) { if (current === epoch) runtime.report(error); }
    },
    dispose() { disposed = true; ++epoch; },
  };
}

/** The router is the active-tab authority; saved selection is only a boot hint. */
export function bindSessionTabs(runtime: AppRuntime, router: AnyRouter) {
  const initial = router.state.location;
  let startup = initial.pathname === '/' && !Object.keys(initial.search).length;
  let observing = false;
  let disposed = false;
  let capacityNotice: string | undefined;
  let observedLocation: typeof initial | undefined;
  let observedRuntimeId: string | undefined;
  let client = runtime.getSnapshot().client;
  let offClient: (() => void) | undefined;
  const observe = () => {
    if (observing || disposed) return;
    observing = true;
    try {
      const current = router.state.location;
      if (current.href !== initial.href || current.state.__TSR_key !== initial.state.__TSR_key) startup = false;
      const connection = runtime.getSnapshot().client?.getSnapshot();
      const runtimeId = connection?.info?.runtime_id;
      if (!runtimeId) return;
      // Runtime notices and command completions are not navigation. In
      // particular, a storage warning during close must not reopen its old URL.
      if (current === observedLocation && runtimeId === observedRuntimeId && !startup && !capacityNotice) return;
      observedLocation = current;
      observedRuntimeId = runtimeId;
      const destination = sessionDestination(current.pathname);
      if (destination) {
        startup = false;
        const search = current.search as Record<string, unknown>;
        if (!runtime.tabs.canOpen(destination.runtimeId, destination.rootId)) {
          if (capacityNotice !== current.href) {
            capacityNotice = current.href;
            runtime.report('There are 32 open session tabs. Close a tab to open this session.');
          }
          return;
        }
        runtime.tabs.visit(destination.runtimeId, destination.rootId, validateSessionSearch(search), typeof current.state.whipViewId === 'string' ? current.state.whipViewId : undefined);
        if (destination.runtimeId === runtimeId) runtime.rememberSession(runtimeId, destination.rootId);
        capacityNotice = undefined;
      } else if (current.pathname === '/') {
        if (startup && connection?.state !== 'connected') return;
        if (startup) {
          startup = false;
          const workspace = runtime.tabs.workspace(runtimeId);
          const tab = workspace.lastActiveRootId ? selectedSessionTab(workspace) : undefined;
          if (tab) {
            void router.navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId, rootId: tab.rootId }, search: sessionSearch(tab), state: { whipViewId: tab.id }, replace: true }).catch(error => runtime.report(error));
            return;
          }
        }
        runtime.tabs.home(runtimeId);
      } else startup = false;
    } catch (error) { runtime.report(error); }
    finally { observing = false; }
  };
  const attach = () => {
    const next = runtime.getSnapshot().client;
    if (next !== client || !offClient) {
      offClient?.();
      client = next;
      offClient = client?.subscribe(observe);
    }
    observe();
  };
  const offRuntime = runtime.subscribe(attach);
  const offRoute = router.subscribe('onResolved', observe);
  // Closing another tab may make room for a directly linked 33rd session.
  const offTabs = runtime.tabs.subscribe(() => { if (capacityNotice) observe(); });
  attach();
  return () => { disposed = true; offClient?.(); offRuntime(); offRoute(); offTabs(); };
}
