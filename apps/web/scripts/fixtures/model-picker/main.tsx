import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { initializeTheme, ThemeProvider, UIProvider, defaultDisplayPreferences, displayStorageKey } from '@whip/ui';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
import { ModelPicker } from '../../../../../packages/app/src/model-selection';
import { RuntimeContext } from '../../../../../packages/app/src/context';
import type { AppRuntime } from '../../../../../packages/app/src/runtime';
import type { ComponentProps } from 'react';

const query = new URLSearchParams(location.search);
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: query.get('theme') ?? 'dark' }));
localStorage.setItem(displayStorageKey, JSON.stringify({ version: 1, display: { ...defaultDisplayPreferences, uiSize: Number(query.get('size') ?? 13) } }));
initializeTheme({ storage: localStorage });
const provider = query.get('provider') ?? 'fixture';
const models = Array.from({ length: 40 }, (_, index) => ({
  id: `model-${String(index).padStart(2, '0')}-${'long-name-'.repeat(4)}`,
  context_length: 200_000, input_modalities: ['text', 'image'], reasoning_efforts: ['low', 'high'],
}));
const runtime = { run: () => { throw new Error('Layout check must not change the model'); } } as unknown as AppRuntime;
const props = {
  connected: true,
  root: { meta: { model: models[0].id, provider }, active_turns: {} },
  view: { session: { client: {
    getSnapshot: () => ({ info: { runtime_id: 'layout-fixture' } }),
    providers: { catalogs: async () => ({ result: { catalogs: { [provider]: { models }, alternate: { models: [models[0]] }, 'custom-provider-with-a-very-long-name': { models: [models[1]] } } } }) },
  } } },
} as unknown as ComponentProps<typeof ModelPicker>;
const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
const route = createRootRoute({ component: () => <QueryClientProvider client={queryClient}>
  <RuntimeContext.Provider value={runtime}><ThemeProvider storage={localStorage}><UIProvider>
    <div style={{ position: 'fixed', left: `${query.get('x') ?? 50}%`, bottom: Number(query.get('bottom') ?? 16), transform: 'translateX(-50%)' }}>
      <ModelPicker {...props} />
    </div>
  </UIProvider></ThemeProvider></RuntimeContext.Provider>
</QueryClientProvider> });
createRoot(document.getElementById('root')!).render(<RouterProvider router={createRouter({ routeTree: route })} />);
