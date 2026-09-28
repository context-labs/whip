import { useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { ConfigurationUpdate, RuntimeConfiguration } from '@whip/protocol';
import { Collapsible, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CatalogModelPicker, useProviderCatalog } from './model-selection';
import { SettingRow, settingsSection } from './settings/section-layout';
import { layout } from './styles';

const styles = stylex.create({ summary: { minWidth: 0, overflowWrap: 'anywhere' } });

// Keep invalid input text in the draft; numeric zero is reserved for Automatic.
export type CompactionValues = Pick<RuntimeConfiguration, 'compact_model' | 'compact_provider'> & { compact_percent: number | string };

export function compactionError(value: CompactionValues, base: CompactionValues): string | undefined {
  if (value.compact_percent === base.compact_percent || value.compact_percent === 0) return;
  const percent = Number(value.compact_percent);
  if (!Number.isInteger(percent) || percent < 10 || percent > 90) {
    return 'Use a whole-number percentage from 10 to 90, or choose Automatic.';
  }
}

/** Omit untouched overrides so saving another setting never rewrites legacy values. */
export function compactionPatch(value: CompactionValues, base: CompactionValues): Pick<ConfigurationUpdate, 'compact_model' | 'compact_provider' | 'compact_percent'> {
  return {
    ...(value.compact_model !== base.compact_model || value.compact_provider !== base.compact_provider ? {
      compact_model: value.compact_model.trim(),
      compact_provider: value.compact_model.trim() ? value.compact_provider : '',
    } : {}),
    ...(value.compact_percent !== base.compact_percent ? { compact_percent: Number(value.compact_percent) } : {}),
  };
}

/** Shared host-default draft controls; neither surface applies runtime changes here. */
export function CompactionSettings({ client, value, base, disabled, onChange }: {
  client: WhipClient; value: CompactionValues; base: CompactionValues; disabled: boolean;
  onChange(value: CompactionValues): void;
}) {
  const [advanced, setAdvanced] = useState(false);
  const [choosingModel, setChoosingModel] = useState(false);
  const customModel = !!value.compact_model.trim();
  const customTiming = value.compact_percent !== 0;
  const catalog = useProviderCatalog(client, !disabled && advanced && (customModel || choosingModel));
  const result = catalog.data?.result;
  // A custom override must select a known, connected route, not arbitrary provider text.
  const connected = (provider: string) => result?.providers[provider]?.available === true;
  const connectedCatalog = result && {
    ...result,
    models: Object.fromEntries(Object.entries(result.models).map(([name, model]) =>
      [name, { ...model, providers: model.providers?.filter(connected) ?? [] }])),
    catalogs: Object.fromEntries(Object.entries(result.catalogs).filter(([provider]) => connected(provider))),
  };
  const percent = Number(value.compact_percent);
  const effectivePercent = percent === 0 ? 50 : Math.max(10, Math.min(90, percent));
  const error = compactionError(value, base);
  const legacyPercent = value.compact_percent === base.compact_percent && percent !== 0 && percent !== effectivePercent;
  return <>
    <p {...stylex.props(layout.muted)}>Older active context is summarized. Full conversation history remains available.</p>
    <SettingRow id="compact_model" label="Summary model">
      <span {...stylex.props(styles.summary)}>{customModel ? `Custom · ${value.compact_model} · ${value.compact_provider || 'provider resolved by host'}` : 'Automatic · conversation model and provider'}</span>
    </SettingRow>
    <SettingRow id="compact_percent" label="When to compact"
      description={legacyPercent ? `Saved value: ${percent}%. The effective threshold is ${effectivePercent}%; saving other settings keeps the saved value.` : undefined}>
      <span>{error ? 'Custom · choose 10–90%' : `${percent === 0 ? 'Automatic' : 'Custom'} · ${effectivePercent}%`}</span>
    </SettingRow>
    <Collapsible title="Advanced compaction settings" open={advanced} onOpenChange={setAdvanced}>
      <div {...stylex.props(layout.column)}>
        <SettingRow id="compact_model_mode" label="Summary model override" description="Use the conversation model, or choose a connected model and provider together.">
          <Select label="Summary model mode" value={customModel || choosingModel ? 'custom' : 'automatic'} disabled={disabled} xstyle={settingsSection.control}
            options={[{ value: 'automatic', label: 'Automatic' }, { value: 'custom', label: 'Custom' }]}
            onValueChange={mode => {
              setChoosingModel(mode === 'custom');
              if (mode === 'automatic') onChange({ ...value, compact_model: '', compact_provider: '' });
            }} />
        </SettingRow>
        {(customModel || choosingModel) && <SettingRow id="compact_custom_model" label="Custom summary model"
          description="If this route is unavailable, the conversation model is used instead. Saved overrides stay unchanged until you edit them.">
          <CatalogModelPicker settings label="Custom summary model" model={value.compact_model} provider={customModel ? value.compact_provider : ''}
            catalog={connectedCatalog} loading={catalog.isLoading} error={disabled ? undefined : catalog.error?.message} onRetry={() => void catalog.refetch()}
            disabled={disabled} xstyle={settingsSection.control}
            onChange={(model, provider) => { setChoosingModel(false); onChange({ ...value, compact_model: model, compact_provider: provider }); }} />
        </SettingRow>}
        <SettingRow id="compact_timing" label="Compaction timing">
          <Select label="Compaction timing" value={customTiming ? 'custom' : 'automatic'} disabled={disabled} xstyle={settingsSection.control}
            options={[{ value: 'automatic', label: 'Automatic · 50%' }, { value: 'custom', label: 'Custom' }]}
            onValueChange={mode => onChange({ ...value, compact_percent: mode === 'automatic' ? 0 : 50 })} />
        </SettingRow>
        {customTiming && <SettingRow id="compact_custom_percent" label="Context window percentage" description="Compact when active context reaches this share of the model’s context window. Choose 10–90%.">
          <Input aria-label="Context window percentage" type="number" min={10} max={90} step={1} disabled={disabled}
            aria-invalid={!!error} aria-describedby={error ? 'compact-percent-error' : undefined} xstyle={settingsSection.control}
            value={value.compact_percent} onChange={event => {
              const next = event.target.valueAsNumber;
              onChange({ ...value, compact_percent: Number.isInteger(next) && next >= 10 && next <= 90 ? next : event.target.value });
            }} />
        </SettingRow>}
        {error && <p id="compact-percent-error" role="alert">{error}</p>}
      </div>
    </Collapsible>
  </>;
}
