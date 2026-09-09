/** Device-local presentation only. No host, transcript, or product preferences. */
export interface DisplayPreferences {
  uiFont: 'inter' | 'system';
  codeFont: 'jetbrains-mono' | 'system';
  uiSize: number;
  codeSize: number;
  wrapCode: boolean;
  contrast: 'system' | 'standard' | 'more';
  motion: 'system' | 'reduce';
}
export const displayStorageKey = 'whip.appearance.display.v1';
export const defaultDisplayPreferences: Readonly<DisplayPreferences> = Object.freeze({
  uiFont: 'inter', codeFont: 'jetbrains-mono', uiSize: 13, codeSize: 12,
  wrapCode: false, contrast: 'system', motion: 'system',
});
export function validateDisplayPreferences(value: unknown): DisplayPreferences {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid display preferences.');
  const item = value as Record<string, unknown>;
  if ((typeof item.uiFont !== 'string' || !['inter', 'system'].includes(item.uiFont))
    || (typeof item.codeFont !== 'string' || !['jetbrains-mono', 'system'].includes(item.codeFont))
    || typeof item.uiSize !== 'number' || !Number.isInteger(item.uiSize) || item.uiSize < 12 || item.uiSize > 20
    || typeof item.codeSize !== 'number' || !Number.isInteger(item.codeSize) || item.codeSize < 10 || item.codeSize > 24
    || typeof item.wrapCode !== 'boolean'
    || (typeof item.contrast !== 'string' || !['system', 'standard', 'more'].includes(item.contrast))
    || (typeof item.motion !== 'string' || !['system', 'reduce'].includes(item.motion))) throw new Error('Invalid display preferences.');
  return {uiFont: item.uiFont as DisplayPreferences['uiFont'], codeFont: item.codeFont as DisplayPreferences['codeFont'],
    uiSize: item.uiSize, codeSize: item.codeSize, wrapCode: item.wrapCode,
    contrast: item.contrast as DisplayPreferences['contrast'], motion: item.motion as DisplayPreferences['motion']};
}
export function parseDisplayPreferences(text: string | null): DisplayPreferences {
  if (!text) return {...defaultDisplayPreferences};
  if (text.length > 4096 || new TextEncoder().encode(text).byteLength > 4096) throw new Error('Saved display preferences exceed 4 KiB.');
  const saved: unknown = JSON.parse(text);
  if (!saved || typeof saved !== 'object' || !('version' in saved) || saved.version !== 1 || !('display' in saved)) throw new Error('Unsupported display preferences.');
  return validateDisplayPreferences(saved.display);
}
/** Read at the animation boundary so live changes affect the next frame. */
export function prefersReducedMotion(): boolean {
  return (typeof document !== 'undefined' && document.documentElement.dataset.motion === 'reduce')
    || (typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches);
}
