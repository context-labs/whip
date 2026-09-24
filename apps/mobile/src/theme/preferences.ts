import { themeCatalog, validateTheme, type ThemeDefinition } from '@whip/ui/theme-data';
export type Appearance = { mode: 'system' | 'light' | 'dark'; light: string; dark: string; contrast?: 'system' | 'standard' | 'increased'; motion?: 'system' | 'reduce'; font?: 'inter' | 'system'; codeFont?: 'jetbrains-mono' | 'system'; textScale?: number; codeSize?: number; wrapCode?: boolean; toolDensity?: 'compact' | 'comfortable' | 'detailed' };
export const defaultAppearance: Appearance = { mode: 'system', light: 'github-light', dark: 'claude-code', contrast: 'system', motion: 'system', font: 'inter', codeFont: 'jetbrains-mono', textScale: 1, codeSize: 13, wrapCode: true, toolDensity: 'compact' };
export type AppearanceRecord = { version: 2; appearance: Appearance; themes: ThemeDefinition[] };
export function appearanceRecord(input: Appearance, themes: readonly ThemeDefinition[] = []): AppearanceRecord {
  if (themes.length > 16) throw new Error('Keep up to 16 custom themes on this phone.');
  const ids = new Set(themeCatalog.map(t => t.id));
  const customs = themes.map(value => { const theme = validateTheme(value); if (ids.has(theme.id)) throw new Error('Theme IDs must be unique and cannot replace built-in themes.'); ids.add(theme.id); return theme; });
  const all = [...themeCatalog, ...customs]; const next = { ...defaultAppearance, ...input };
  for (const kind of ['light', 'dark'] as const) if (!all.some(t => t.id === next[kind] && t.dark === (kind === 'dark'))) throw new Error(`Choose an available ${kind} theme.`);
  if (!['system', 'light', 'dark'].includes(next.mode) || !['system', 'standard', 'increased'].includes(next.contrast!) || !['system', 'reduce'].includes(next.motion!) || !['inter', 'system'].includes(next.font!) || !['jetbrains-mono', 'system'].includes(next.codeFont!) || !['compact', 'comfortable', 'detailed'].includes(next.toolDensity!)) throw new Error('These appearance settings are not supported.');
  if (!Number.isFinite(next.textScale) || next.textScale! < 0.9 || next.textScale! > 1.3 || !Number.isFinite(next.codeSize) || next.codeSize! < 12 || next.codeSize! > 24 || typeof next.wrapCode !== 'boolean') throw new Error('Choose a supported text size and code wrapping setting.');
  const result: AppearanceRecord = { version: 2, appearance: next, themes: customs };
  if (new TextEncoder().encode(JSON.stringify(result)).length + 16 > 256 * 1024) throw new Error('Custom themes exceed this phone’s 256 KiB limit.');
  return result;
}
