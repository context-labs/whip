import { createRoot } from 'react-dom/client';
import { createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { initializeTheme, ThemeProvider, UIProvider, defaultDisplayPreferences, displayStorageKey } from '@whip/ui';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
import { CatalogModelPicker } from '../../../../../packages/app/src/model-selection';
import { unknownPrices, type CatalogModel, type ModelCatalog } from '../../../../../packages/app/src/model-options';

const query = new URLSearchParams(location.search);
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: query.get('theme') ?? 'dark' }));
localStorage.setItem(displayStorageKey, JSON.stringify({ version: 1, display: { ...defaultDisplayPreferences, uiSize: Number(query.get('size') ?? 13) } }));
initializeTheme({ storage: localStorage });
const provider = query.get('provider') ?? 'fixture';
const models: CatalogModel[] = Array.from({ length: 40 }, (_, index) => ({
  id: `model-${String(index).padStart(2, '0')}-${'long-name-'.repeat(4)}`, name: `Model ${index}`,
  context_window_tokens: '200000', max_output_tokens: null, advertised_context_tokens: null,
  effective_context_percent: null, input_modalities: ['text', 'image'], output_modalities: ['text'],
  reasoning_efforts: ['low', 'high'], supports_tools: true, metadata_source: 'bundled', prices: unknownPrices,
}));
const catalog: ModelCatalog = {
  inventory: { revision: 'a'.repeat(64), routes: [], defaults: null, compaction_model: null }, truncated: false,
  providers: [{ id: provider, models }, { id: 'alternate', models: models.slice(0, 1) },
    { id: 'custom-provider-with-a-very-long-name', models: models.slice(1, 2) }].map(entry => ({ ...entry,
      catalog: { provider: entry.id, state: 'missing', scope_state: 'unverified', discovery: 'not_checked', fetched_at: null, stale: false, failure: null, models: [] },
    })),
};
const route = createRootRoute({ component: () => <ThemeProvider storage={localStorage}><UIProvider>
  <div style={{ position: 'fixed', left: `${query.get('x') ?? 50}%`, bottom: Number(query.get('bottom') ?? 16), transform: 'translateX(-50%)' }}>
    <CatalogModelPicker catalog={catalog} model={models[0]!.id} provider={provider}
      onChange={() => { throw new Error('Layout check must not change the model'); }} />
  </div>
</UIProvider></ThemeProvider> });
createRoot(document.getElementById('root')!).render(<RouterProvider router={createRouter({ routeTree: route })} />);
