import { ErrorNotice } from '../error-feedback';
import { useState } from 'react';
import { Button, CodeBlock, IconButton, NumberField, Select, Slider, Switch, ThemePicker, useTheme } from '@whip/ui';
import { RotateCcw, Upload } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, typography, surface, scale } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from '../context';
import { MessageRow } from '../timeline';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';
import { CustomThemes } from './sections';

const densities = ['compact', 'comfortable', 'detailed'] as const;
const densityLabels = ['Compact', 'Comfortable', 'Detailed'];
const sampleCode = 'results = models.batch([\n    {"model": "fast", "prompt": "Find the smallest change that makes the connection reliable."},\n])\nprint(results[0].text)';
export function AppearanceSettings() {
  const runtime = useRuntime();
  const { home, preferences } = useAppState();
  const { display, setDisplay, resetAppearance, notice } = useTheme();
  const [resetNotice, setResetNotice] = useState('');
  const [error, setError] = useState('');
  const updateDensity = (index: number) => {
    try { runtime.setPreferences({ toolDensity: densities[index] ?? 'compact' }); setError(''); return true; }
    catch (error) { setError(error instanceof Error ? error.message : String(error)); return false; }
  };
  return <>
    <SettingsGroup>
      <SettingRow id="theme" label="Color theme" description="Choose a theme, or follow your system appearance.">
        <ThemePicker presentation="popover" />
      </SettingRow>
      <SettingRow id="custom-themes" label="Custom themes" description={home?.state === 'connected' ? 'Import a theme JSON file. It stays on this device.' : 'Reconnect to Whip to import a theme. Saved themes are still available above.'}>
        {home?.client ? <CustomThemes key={`${home.id}:${home.runtimeId}`} client={home.client} /> : <Button disabled><Upload size={14} />Import theme</Button>}
      </SettingRow>
    </SettingsGroup>
    {notice && <p role="status" {...stylex.props(styles.notice)}>{notice}</p>}
    <SettingsGroup title="Conversations">
      <SettingRow id="toolDensity" label="Tool call density" description="Choose how much tool detail appears in conversations.">
        <div {...stylex.props(settingsSection.control)}>
          <Slider label="Tool call density" min={0} max={2} step={1} value={densities.indexOf(preferences.toolDensity)}
            getValueText={value => densityLabels[value] ?? 'Compact'} onValueChange={updateDensity} />
          <div {...stylex.props(styles.sliderLabels)}><span>Compact</span><span>{preferences.toolDensity === 'comfortable' ? 'Comfortable' : 'Detailed'}</span></div>
        </div>
      </SettingRow>
      {error && <ErrorNotice type="action" owner="tool-density" title="Could not save tool density" error={error} />}
      <SettingRow id="wrapCode" label="Code block word wrap" description="Wrap long lines in code and tool output.">
        <Switch hideLabel label="Code block word wrap" checked={display.wrapCode} onCheckedChange={wrapCode => setDisplay({ wrapCode })} />
      </SettingRow>
    </SettingsGroup>
    <SettingsGroup title="Typography">
      <SettingRow id="uiSize" label="UI font size" description="Size of interface and conversation text.">
        <div {...stylex.props(styles.stepper)}>
          <IconButton variant="ghost" label="Reset UI font size" disabled={display.uiSize === 13} onClick={() => setDisplay({ uiSize: 13 })}><RotateCcw size={14} /></IconButton>
          <NumberField label="UI font size" format={{ maximumFractionDigits: 0 }} value={display.uiSize} min={12} max={20} onValueChange={value => { if (value !== null) setDisplay({ uiSize: value }); }} xstyle={styles.number} />
        </div>
      </SettingRow>
      <SettingRow id="codeSize" label="Code font size" description="Size of code blocks, tool output, and REPL text.">
        <div {...stylex.props(styles.stepper)}>
          <IconButton variant="ghost" label="Reset code font size" disabled={display.codeSize === 12} onClick={() => setDisplay({ codeSize: 12 })}><RotateCcw size={14} /></IconButton>
          <NumberField label="Code font size" format={{ maximumFractionDigits: 0 }} value={display.codeSize} min={10} max={24} onValueChange={value => { if (value !== null) setDisplay({ codeSize: value }); }} xstyle={styles.number} />
        </div>
      </SettingRow>
      <SettingRow id="uiFont" label="UI font family" description="Typeface for the interface and conversations.">
        <Select label="UI font family" value={display.uiFont} options={[{ value: 'inter', label: 'Inter' }, { value: 'system', label: 'System font' }]}
          onValueChange={value => setDisplay({ uiFont: value === 'system' ? 'system' : 'inter' })} />
      </SettingRow>
      <SettingRow id="codeFont" label="Code font family" description="Typeface for code and execution output.">
        <Select label="Code font family" value={display.codeFont} options={[{ value: 'jetbrains-mono', label: 'JetBrains Mono' }, { value: 'system', label: 'System monospace' }]}
          onValueChange={value => setDisplay({ codeFont: value === 'system' ? 'system' : 'jetbrains-mono' })} />
      </SettingRow>
      <div aria-label="Live appearance preview" {...stylex.props(styles.preview)}>
        <p {...stylex.props(styles.previewLabel)}>Live preview</p>
        <p {...stylex.props(styles.previewText)}>I'll check the connection, then summarize the smallest change.</p>
        <MessageRow row={{ id: 'appearance-tool-preview', role: 'tool', label: 'Starlark execution', args: JSON.stringify({ code: 'print("Connection is ready")' }), text: 'Connection is ready' }} readBody={() => {}} />
        <CodeBlock code={sampleCode} language="starlark" />
      </div>
    </SettingsGroup>
    <SettingsGroup title="Accessibility">
      <SettingRow id="contrast" label="Contrast" description="Increase the distinction between text, controls, and backgrounds.">
        <Select label="Contrast" value={display.contrast} options={[{ value: 'system', label: 'Follow system' }, { value: 'standard', label: 'Standard' }, { value: 'more', label: 'Increased' }]}
          onValueChange={value => setDisplay({ contrast: value === 'more' ? 'more' : value === 'standard' ? 'standard' : 'system' })} />
      </SettingRow>
      <SettingRow id="motion" label="Reduce motion" description="Reduce interface animations, including tab movement.">
        <Select label="Reduce motion" value={display.motion} options={[{ value: 'system', label: 'Follow system' }, { value: 'reduce', label: 'Reduce' }]}
          onValueChange={value => setDisplay({ motion: value === 'reduce' ? 'reduce' : 'system' })} />
      </SettingRow>
    </SettingsGroup>
    <div {...stylex.props(styles.reset)}>
      <Button variant="ghost" onClick={() => { resetAppearance(); setResetNotice(updateDensity(0) ? 'Appearance reset. Imported themes are still available.' : 'Display settings reset, but tool density could not be saved.'); }}><RotateCcw size={14} />Reset appearance</Button>
      {resetNotice && <p role="status" {...stylex.props(styles.notice)}>{resetNotice}</p>}
    </div>
  </>;
}
const styles = stylex.create({
  sliderLabels: { display: 'flex', justifyContent: 'space-between', gap: 12, color: surface.secondaryText, fontSize: typography.size12 },
  stepper: { display: 'flex', alignItems: 'center', gap: 4, width: { default: 192, [scale.phone]: 240 }, maxWidth: '100%' },
  number: { flex: 1, minWidth: 0 },
  notice: { color: surface.secondaryText, fontSize: typography.size13, lineHeight: 1.6, margin: 0 },
  preview: { padding: 16, borderRadius: 8, backgroundColor: colors.background, minWidth: 0, overflow: 'hidden' },
  previewLabel: { margin: 0, marginBottom: 12, color: surface.secondaryText, fontSize: typography.size12 },
  previewText: { fontFamily: typography.sans, fontSize: typography.size14, lineHeight: 1.65, margin: 0 },
  reset: { display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 8 },
});
