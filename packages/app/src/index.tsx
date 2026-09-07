import { QueryClientProvider } from '@tanstack/react-query';
import { createRouter, RouterProvider, type RouterHistory } from '@tanstack/react-router';
import { ThemeProvider } from '@whip/ui/themes';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from './context';
import { AppRuntime } from './runtime';
import type { AppPlatform } from './platform';
import { routeTree } from './routeTree.gen';
import { bindSessionTabs } from './session-tab-routing';

export function createWhipApplication(platform: AppPlatform, history?: RouterHistory) {
  const runtime = new AppRuntime(platform);
  const router = createRouter({ routeTree, context: { runtime }, history, defaultPreload: 'intent', defaultPreloadStaleTime: 0 });
  const unbindTabs = bindSessionTabs(runtime, router);
  function Application() {
    return <RuntimeContext.Provider value={runtime}>
      <ThemeProvider storage={platform.storage} onNotice={message => runtime.report(message)}>
        <UIProvider><QueryClientProvider client={runtime.queries}><RouterProvider router={router} /></QueryClientProvider></UIProvider>
      </ThemeProvider>
    </RuntimeContext.Provider>;
  }
  return { Application, runtime, router, dispose: () => { unbindTabs(); runtime.dispose(); } };
}

declare module '@tanstack/react-router' {
  interface Register { router: ReturnType<typeof createWhipApplication>['router'] }
}
