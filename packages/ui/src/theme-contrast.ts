import type {ThemeDefinition} from './generated/theme-catalog';

function rgb(color: string): number[] {return [1, 3, 5].map(offset => parseInt(color.slice(offset, offset + 2), 16));}
function luminance(color: string): number {return rgb(color).map(channel => channel / 255).map(channel => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4).reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index]!, 0);}
export function contrastRatio(left: string, right: string): number {const a = luminance(left); const b = luminance(right); return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);}
function mix(left: string, right: string, amount: number): string {const a = rgb(left); const b = rgb(right); return `#${a.map((channel, i) => Math.round(channel + (b[i]! - channel) * amount).toString(16).padStart(2, '0')).join('')}`;}
export function readableColor(color: string, backgrounds: readonly string[], target = 4.65): string {
  const score = (candidate: string) => Math.min(...backgrounds.map(background => contrastRatio(candidate, background)));
  if (score(color) >= target) return color;
  const endpoint = score('#000000') > score('#ffffff') ? '#000000' : '#ffffff';
  if (score(endpoint) < target) return endpoint;
  let lo = 0, hi = 1;
  for (let step = 0; step < 18; step++) {const mid = (lo + hi) / 2; if (score(mix(color, endpoint, mid)) >= target) hi = mid; else lo = mid;}
  return mix(color, endpoint, hi);
}
/** Keep the TUI catalog exact while deriving readable browser presentation.
 * Only insufficient contrast changes: hue moves toward black/white by the
 * smallest amount meeting WCAG normal-text contrast on all supported surfaces.
 * Inconsistent custom surface ladders move toward their canvas when necessary. */
export function adaptThemeForWeb(source: ThemeDefinition): ThemeDefinition {
  const colors = {...source.colors};
  const endpoint = contrastRatio('#000000', colors.background) > contrastRatio('#ffffff', colors.background) ? '#000000' : '#ffffff';
  for (const role of ['panel', 'element', 'hover'] as const) {
    if (contrastRatio(endpoint, colors[role]) >= 5) continue;
    let lo = 0, hi = 1;
    for (let step = 0; step < 18; step++) {const mid = (lo + hi) / 2; if (contrastRatio(endpoint, mix(colors[role], colors.background, mid)) >= 5) hi = mid; else lo = mid;}
    colors[role] = mix(colors[role], colors.background, hi);
  }
  const backgrounds = [colors.background, colors.panel, colors.element, colors.hover];
  for (const role of ['foreground', 'muted', 'link', 'emphasis', 'accent'] as const) colors[role] = readableColor(colors[role], backgrounds);
  // Status badges/buttons use a 10–15% tint. Include those fills when deriving text.
  for (const role of ['success', 'warning', 'error', 'info'] as const) colors[role] = readableColor(colors[role], [...backgrounds, mix(colors.background, source.colors[role], 0.15)], 5);
  const codeBackground = source.code.background === source.colors.element ? colors.element : source.code.background;
  const syntax = {...source.syntax};
  for (const role of Object.keys(syntax) as (keyof typeof syntax)[]) syntax[role] = readableColor(syntax[role], [codeBackground]);
  const markdown = {...source.markdown};
  for (const role of Object.keys(markdown) as (keyof typeof markdown)[]) markdown[role] = readableColor(markdown[role], backgrounds);
  const tokens = Object.fromEntries(Object.entries(source.code.tokens).map(([name, token]) => [name, {...token, background: token.background === source.code.background ? codeBackground : token.background, color: readableColor(token.color, [token.background === source.code.background ? codeBackground : token.background])}]));
  return {...source, colors, syntax, markdown, code: {...source.code, background: codeBackground, foreground: readableColor(source.code.foreground, [codeBackground]), tokens}};
}
