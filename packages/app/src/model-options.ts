import type { ProviderCatalogsResult } from '@whip/protocol';

export type CatalogModel = NonNullable<ProviderCatalogsResult['catalogs'][string]['models']>[number];

/** A model name and provider together identify the route the daemon will use. */
export function modelOptions(catalog: ProviderCatalogsResult | undefined) {
  const options = new Map<string, {
    value: string; label: string; name: string; provider: string; model: CatalogModel;
  }>();
  const add = (name: string, provider: string, model: CatalogModel) => {
    if (catalog?.providers?.[provider]?.available === false) return;
    const value = JSON.stringify([name, provider]);
    if (!options.has(value)) options.set(value, { value, label: `${name} · ${provider}`, name, provider, model });
  };
  for (const [name, info] of Object.entries(catalog?.models ?? {})) {
    for (const provider of info.providers ?? []) {
      const id = info.id || name;
      const model = catalog?.catalogs?.[provider]?.models?.find(model => model.id === id);
      add(name, provider, model ?? { id });
    }
  }
  for (const [provider, entry] of Object.entries(catalog?.catalogs ?? {})) {
    for (const model of entry.models ?? []) {
      const configured = catalog?.models?.[model.id];
      // Configured aliases are authoritative even when a catalog uses that name.
      if (configured?.id && configured.id !== model.id) continue;
      add(model.id, provider, model);
    }
  }
  return [...options.values()].sort((left, right) => left.label.localeCompare(right.label));
}
