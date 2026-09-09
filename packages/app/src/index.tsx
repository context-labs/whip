import { QueryClientProvider } from '@tanstack/react-query';
import { createRouter, RouterProvider, type RouterHistory } from '@tanstack/react-router';
import { ThemeProvider } from '@whip/ui/themes';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from './context';
import { AppRuntime } from './runtime';
import type { AppPlatform } from './platform';
import { routeTree } from './routeTree.gen';
import { bindSessionTabs } from './session-tab-routing';
import { bindSettingsNavigation } from './settings/navigation';
import { useSyncExternalStore, type ReactNode } from 'react';

export { createHostPrompts, HostPrompts } from './host-prompts';
export { createSessionNavigator } from './session-tab-routing';

export function createWhipApplication(platform: AppPlatform, history?: RouterHistory) {
  const runtime = new AppRuntime(platform);
  const router = createRouter({ routeTree, context: { runtime }, history, defaultPreload: 'intent', defaultPreloadStaleTime: 0 });
  const unbindTabs = bindSessionTabs(runtime, router);
  const unbindSettings = bindSettingsNavigation(runtime, router);
  const subscribeContrast = platform.systemContrast?.subscribe ?? (() => () => {});
  const readContrast = platform.systemContrast?.getSnapshot ?? (() => undefined);
  function Application({ children }: { children?: ReactNode }) {
    const systemContrast = useSyncExternalStore(subscribeContrast, readContrast, readContrast);
    return <RuntimeContext.Provider value={runtime}>
      <ThemeProvider storage={platform.storage} systemContrast={systemContrast} onNotice={message => runtime.report(message)}>
        <UIProvider><QueryClientProvider client={runtime.queries}><RouterProvider router={router} />{children}</QueryClientProvider></UIProvider>
      </ThemeProvider>
    </RuntimeContext.Provider>;
  }
  return { Application, runtime, router, dispose: () => { unbindSettings(); unbindTabs(); runtime.dispose(); } };
}

declare module '@tanstack/react-router' {
  interface Register { router: ReturnType<typeof createWhipApplication>['router'] }
}
