import type {ThemeDefinition} from './generated/theme-catalog';

export const colorRoles = ['background', 'foreground', 'muted', 'faint', 'primary', 'onPrimary', 'accent', 'success', 'warning', 'error', 'info', 'link', 'emphasis', 'border', 'borderFocus', 'diffAdd', 'diffDel', 'panel', 'element', 'hover'] as const;
export const syntaxRoles = ['keyword', 'string', 'number', 'comment', 'function', 'type', 'operator', 'punctuation'] as const;
export const markdownRoles = ['heading', 'strong', 'code', 'quote'] as const;
const colorPattern = /^#[\da-f]{6}$/i;

/** Validate resolved Go theme output. Raw TUI JSON must go through the daemon's
 * shared theme resolver, which knows ANSI palette and Chroma override semantics. */
export function validateTheme(input: unknown): ThemeDefinition {
  if (!input || typeof input !== 'object') throw new Error('The resolved theme must be an object.');
  const serialized = JSON.stringify(input);
  if (serialized.length > 262144) throw new Error('The resolved theme exceeds 256 KiB.');
  const value = input as ThemeDefinition;
  if (typeof value.id !== 'string' || !value.id || value.id.length > 240 || typeof value.name !== 'string' || !value.name || value.name.length > 120 || typeof value.dark !== 'boolean') throw new Error('The theme needs a valid ID, name and appearance.');
  const check = (group: unknown, keys: readonly string[]) => {
    if (!group || typeof group !== 'object') throw new Error('The theme is missing resolved colors.');
    for (const key of keys) if (!colorPattern.test((group as Record<string, string>)[key] ?? '')) throw new Error(`Invalid resolved theme color: ${key}.`);
  };
  check(value.colors, colorRoles); check(value.syntax, syntaxRoles); check(value.markdown, markdownRoles);
  if (value.web !== undefined) {
    if (!value.web || typeof value.web !== 'object' || Array.isArray(value.web)) throw new Error('Invalid web theme surfaces.');
    for (const [key, color] of Object.entries(value.web)) {
      if (!['navigation', 'quietBorder', 'codeBackground', 'inlineCodeBackground'].includes(key) || typeof color !== 'string' || !colorPattern.test(color)) throw new Error(`Invalid web theme surface: ${key}.`);
    }
  }
  check(value.code, ['foreground', 'background']);
  if (!value.code.tokens || typeof value.code.tokens !== 'object' || Object.keys(value.code.tokens).length > 512) throw new Error('Invalid syntax token catalog.');
  for (const token of Object.values(value.code.tokens)) {
    check(token, ['color', 'background']);
    if (typeof token.bold !== 'boolean' || typeof token.italic !== 'boolean' || typeof token.underline !== 'boolean') throw new Error('Invalid syntax token attributes.');
  }
  // Copy data rather than retaining a caller-owned mutable theme object.
  return JSON.parse(serialized) as ThemeDefinition;
}

export { themeCatalog, themeIds, type ThemeDefinition } from './generated/theme-catalog.ts';
export { contrastRatio, readableColor, adaptThemeForWeb } from './theme-contrast.ts';
