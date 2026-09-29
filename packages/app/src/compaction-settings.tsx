import { useState } from 'react';
import type { Client, HostExecutionDefaults } from '@whip/sdk';
import { Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CatalogModelPicker, useProviderCatalog } from './model-selection';
import { modelSettings } from './model-options';
import { SettingRow } from './settings/section-layout';
import { layout } from './styles';

type Preferences = HostExecutionDefaults['preferences'];
export type CompactionValues = Pick<Preferences, 'compaction_model' | 'compaction_percent'>;
const thresholds = Array.from({ length: 9 }, (_, index) => (index + 1) * 10);
const styles = stylex.create({
  container: { containerType: 'inline-size' },
  control: { width: { default: 300, '@container (max-width: 560px)': '100%' }, maxWidth: '100%', minWidth: 0, flexShrink: 0 },
});

/** Reference compaction controls edit the category draft, never save separately. */
export function CompactionSettings({ client, value, base, disabled, onChange }: {
  client: Client; value: CompactionValues; base: CompactionValues; disabled: boolean; onChange(value: CompactionValues): void;
}) {
  const [opened, setOpened] = useState(false);
  const catalog = useProviderCatalog(client, !disabled && opened);
  const result = catalog.data && { ...catalog.data, providers: catalog.data.providers.filter(provider => {
    const route = catalog.data.inventory.routes.find(route => route.id === provider.id);
    return route && !route.disabled && ['available', 'not_required', 'unchecked', 'refresh_required'].includes(route.credential.state);
  }) };
  const selection = value.compaction_model.selection;
  const percent = value.compaction_percent || 50;
  return <div {...stylex.props(layout.column, styles.container)}>
    <SettingRow id="compact_model" label="Summary model" description={selection ? 'Choose a model and provider for summaries.' : 'Use each conversation’s model and provider.'}>
      <CatalogModelPicker settings showProvider label="Summary model" model={selection?.name ?? ''} provider={selection?.provider ?? ''}
        defaultChoice={{ label: 'Conversation Model', description: 'Use each conversation’s model and provider.' }}
        catalog={result} loading={catalog.isLoading} error={disabled ? undefined : catalog.error?.message} onRetry={() => void catalog.refetch()}
        onOpen={() => setOpened(true)} disabled={disabled} xstyle={styles.control}
        onChange={(name, provider) => onChange({ ...value, compaction_model: { selection: name ? { name, provider, effort: '' } : null, settings: name ? modelSettings(catalog.data, provider, name) : null } })} />
    </SettingRow>
    <SettingRow id="compact_percent" label="Compact at" description="Of the conversation model’s context window.">
      <Select label="Compact at" value={String(percent)} disabled={disabled} xstyle={styles.control}
        options={[...(!thresholds.includes(percent) ? [{ value: String(percent), label: `${percent}% (saved value)`, disabled: true }] : []), ...thresholds.map(percent => ({ value: String(percent), label: `${percent}%` }))]}
        onValueChange={next => onChange({ ...value, compaction_percent: Number(next) === (base.compaction_percent || 50) ? base.compaction_percent : Number(next) })} />
    </SettingRow>
  </div>;
}
