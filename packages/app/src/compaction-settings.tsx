import { useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { ConfigurationUpdate, RuntimeConfiguration } from '@whip/protocol';
import { Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CatalogModelPicker, useProviderCatalog } from './model-selection';
import { SettingRow } from './settings/section-layout';
import { layout } from './styles';

const styles = stylex.create({
  container: { containerType: 'inline-size' },
  control: { width: { default: 300, '@container (max-width: 560px)': '100%' }, maxWidth: '100%', minWidth: 0, flexShrink: 0 },
});

export type CompactionValues = Pick<RuntimeConfiguration, 'compact_model' | 'compact_provider' | 'compact_percent'>;
const thresholds = Array.from({ length: 9 }, (_, index) => (index + 1) * 10);
const effectivePercent = (percent: number) => percent === 0 ? 50 : Math.max(10, Math.min(90, percent));

/** Omit untouched overrides so saving another setting never rewrites legacy values. */
export function compactionPatch(value: CompactionValues, base: CompactionValues): Pick<ConfigurationUpdate, 'compact_model' | 'compact_provider' | 'compact_percent'> {
  return {
    ...(value.compact_model !== base.compact_model || value.compact_provider !== base.compact_provider ? {
      compact_model: value.compact_model.trim(),
      compact_provider: value.compact_model.trim() ? value.compact_provider : '',
    } : {}),
    ...(value.compact_percent !== base.compact_percent ? { compact_percent: value.compact_percent } : {}),
  };
}

/** Shared host-default draft controls; neither surface applies runtime changes here. */
export function CompactionSettings({ client, value, base, disabled, onChange }: {
  client: WhipClient; value: CompactionValues; base: CompactionValues; disabled: boolean;
  onChange(value: CompactionValues): void;
}) {
  const [openedModelPicker, setOpenedModelPicker] = useState(false);
  const catalog = useProviderCatalog(client, !disabled && openedModelPicker);
  const result = catalog.data?.result;
  // Only offer connected routes; saved routes remain visible even when unavailable.
  const connected = (provider: string) => result?.providers[provider]?.available === true;
  const connectedCatalog = result && {
    ...result,
    models: Object.fromEntries(Object.entries(result.models).map(([name, model]) =>
      [name, { ...model, providers: model.providers?.filter(connected) ?? [] }])),
    catalogs: Object.fromEntries(Object.entries(result.catalogs).filter(([provider]) => connected(provider))),
  };
  const percent = value.compact_percent || 50;
  const legacyPercent = !thresholds.includes(percent);
  return <div {...stylex.props(layout.column, styles.container)}>
    <SettingRow id="compact_model" label="Summary model" description={value.compact_model
      ? 'Choose a model and provider for summaries.' : "Use each conversation’s model and provider."}>
      <CatalogModelPicker settings showProvider label="Summary model" model={value.compact_model} provider={value.compact_provider}
        defaultChoice={{ label: 'Conversation Model', description: 'Use each conversation’s model and provider.' }}
        catalog={connectedCatalog} loading={catalog.isLoading} error={disabled ? undefined : catalog.error?.message} onRetry={() => void catalog.refetch()}
        onOpen={() => setOpenedModelPicker(true)}
        disabled={disabled} xstyle={styles.control}
        onChange={(model, provider) => onChange({ ...value, compact_model: model, compact_provider: provider })} />
    </SettingRow>
    <SettingRow id="compact_percent" label="Compact at" description={legacyPercent && percent !== effectivePercent(percent)
      ? 'Saved value: ' + percent + '%. The effective threshold is ' + effectivePercent(percent) + '%; kept until you change it.'
      : 'Of the conversation model’s context window.'}>
      <Select label="Compact at" value={String(percent)} disabled={disabled} xstyle={styles.control}
        options={[
          ...(legacyPercent ? [{ value: String(percent), label: effectivePercent(percent) + '% (saved value)', disabled: true }] : []),
          ...thresholds.map(percent => ({ value: String(percent), label: percent + '%' })),
        ]}
        onValueChange={next => onChange({ ...value, compact_percent: Number(next) === (base.compact_percent || 50) ? base.compact_percent : Number(next) })} />
    </SettingRow>
    {!value.compact_model && <p {...stylex.props(layout.muted)}>Conversation Model requires an up-to-date execution host; older hosts may use DeepSeek instead.</p>}
  </div>;
}
