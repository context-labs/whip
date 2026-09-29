import type { Client, ProviderCatalog, ProviderInventory, ProviderModelsResult } from '@whip/sdk';
export type ProviderModelSettings = NonNullable<ProviderInventory['routes'][number]['models']>[string];
export type ModelSelection = NonNullable<ProviderInventory['defaults']>;
type ModelPrices = ProviderModelSettings['prices'];

export type CatalogModel = Omit<ProviderModelsResult['items'][number], 'metadata_source'> & { metadata_source: ProviderModelsResult['items'][number]['metadata_source'] | 'configured' };
export interface ModelCatalog {
  inventory: ProviderInventory;
  providers: { id: string; catalog: ProviderCatalog; models: CatalogModel[] }[];
  truncated: boolean;
}
export const unknownPrices: ModelPrices = { input: null, output: null, reasoning: null, cached_input: null, cached_output: null };

/** Cached and bundled metadata are observations, never proof of account/model access. */
export async function readModelCatalog(client: Client, signal: AbortSignal, provider?: string): Promise<ModelCatalog> {
  const inventory = await client.listProviders({ signal });
  const providers: ModelCatalog['providers'] = [];
  let remaining = 4096;
  let truncated = false;
  for (const route of inventory.routes.filter(route => !provider || route.id === provider)) {
    signal.throwIfAborted();
    if (!remaining) { truncated = true; break; }
    const [catalog, bundled] = await Promise.all([client.providerCatalog(route.id, { signal }), client.bundledProviderModels(route.id, { signal })]);
    const models = new Map<string, CatalogModel>((catalog.state === 'cached' && catalog.scope_state === 'current' ? [] : bundled.items).map(model => [model.id, model]));
    for (const model of catalog.models) models.set(model.id, model);
    for (const [id, settings] of Object.entries(route.models ?? {})) {
      const metadata = models.get(id);
      models.set(id, { ...metadata, id, name: metadata?.name ?? id, prices: settings.prices,
        context_window_tokens: settings.context_window_tokens, max_output_tokens: settings.max_output_tokens,
        advertised_context_tokens: metadata?.advertised_context_tokens ?? null, effective_context_percent: metadata?.effective_context_percent ?? null,
        reasoning_efforts: metadata?.reasoning_efforts ?? [], input_modalities: metadata?.input_modalities ?? [], output_modalities: metadata?.output_modalities ?? [],
        supports_tools: metadata?.supports_tools ?? null, metadata_source: metadata?.metadata_source ?? 'configured' });
    }
    const values = [...models.values()];
    providers.push({ id: route.id, catalog: { ...catalog, models: [] }, models: values.slice(0, remaining) });
    if (values.length > remaining) truncated = true;
    remaining -= Math.min(values.length, remaining);
  }
  return { inventory, providers, truncated };
}
export function modelOptions(catalog: ModelCatalog | undefined) {
  return (catalog?.providers ?? []).flatMap(({ id, models }) => models.map(model => ({ value: JSON.stringify([model.id, id]), label: `${model.id} · ${id}`, name: model.id, provider: id, model })))
    .sort((left, right) => left.label.localeCompare(right.label));
}

/** Unknown costs stay null; explicitly selecting new metadata never invents free inference. */
export function modelSettings(catalog: ModelCatalog | undefined, provider: string, name: string): ProviderModelSettings | null {
  const configured = catalog?.inventory.routes.find(route => route.id === provider)?.models?.[name];
  if (configured) return configured;
  const model = catalog?.providers.find(entry => entry.id === provider)?.models.find(entry => entry.id === name);
  if (!model) return null;
  return { prices: model.prices, context_window_tokens: model.context_window_tokens, max_output_tokens: model.max_output_tokens ?? '0', timeout_millis: '0', max_attempts: 0 };
}
export function priceLabel(value: string | null): string {
  if (value === null) return 'Unknown';
  const amount = BigInt(value);
  if (amount === 0n) return 'Free';
  return `$${amount / 1000000000n}.${(amount % 1000000000n).toString().padStart(9, '0').replace(/0+$/, '') || '00'} / million tokens`;
}
