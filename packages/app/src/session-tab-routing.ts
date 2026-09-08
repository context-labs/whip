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

/** The router is the active-tab authority; saved selection is only a boot hint. */
export function bindSessionTabs(runtime: AppRuntime, router: AnyRouter) {
  const initial = router.state.location;
  let startup = initial.pathname === '/' && !Object.keys(initial.search).length;
  let observing = false;
  let disposed = false;
  let capacityNotice: string | undefined;
  let observedLocation: typeof initial | undefined;
  const observe = () => {
    if (observing || disposed) return;
    observing = true;
    try {
      const current = router.state.location;
      if (current.href !== initial.href || current.state.__TSR_key !== initial.state.__TSR_key) startup = false;
      const destination = sessionDestination(current.pathname);
      // Runtime notices and command completions are not navigation. In
      // particular, a storage warning during close must not reopen its old URL.
      if (current === observedLocation && !startup && !capacityNotice) return;
      observedLocation = current;
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
        runtime.rememberSession(destination.runtimeId, destination.rootId);
        capacityNotice = undefined;
      } else if (current.pathname === '/') {
        if (startup) {
          startup = false;
          const workspace = runtime.tabs.workspace();
          const tab = workspace.restoreSelection ? selectedSessionTab(workspace) : undefined;
          if (tab) {
            void router.navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: tab.runtimeId, rootId: tab.rootId }, search: sessionSearch(tab), state: { whipViewId: tab.id }, replace: true }).catch(error => runtime.report(error));
            return;
          }
        }
        runtime.tabs.home();
      } else startup = false;
    } catch (error) { runtime.report(error); }
    finally { observing = false; }
  };
  const offRuntime = runtime.subscribe(observe);
  const offRoute = router.subscribe('onResolved', observe);
  // Closing another tab may make room for a directly linked 33rd session.
  const offTabs = runtime.tabs.subscribe(() => { if (capacityNotice) observe(); });
  observe();
  return () => { disposed = true; offRuntime(); offRoute(); offTabs(); };
}
