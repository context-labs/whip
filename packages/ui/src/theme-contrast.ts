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
/** Surface roles shared by the applied document and miniature theme previews. */
export function browserSurfaces(theme: ThemeDefinition, increased = false) {
  const {colors} = theme;
  return {
    secondaryText: readableColor(colors.muted, [colors.background, colors.panel, colors.element, colors.hover], increased ? 7.05 : 4.65),
    controlBorder: readableColor(colors.border, [colors.background, colors.panel, colors.element, colors.hover], 3.1),
    navigation: theme.web?.navigation ?? (theme.dark
      ? `color-mix(in srgb, ${colors.background} 75%, black)`
      : `color-mix(in srgb, ${colors.background} 50%, ${colors.panel})`),
    quietBorder: increased ? colors.border : theme.web?.quietBorder ?? `color-mix(in srgb, ${colors.border} 55%, ${colors.background})`,
    inlineCode: theme.web?.inlineCodeBackground ?? colors.element,
  };
}
/** Keep the TUI catalog exact while deriving readable browser presentation.
 * Only insufficient contrast changes: hue moves toward black/white by the
 * smallest amount meeting WCAG normal-text contrast on all supported surfaces.
 * Inconsistent custom surface ladders move toward their canvas when necessary. */
export function adaptThemeForWeb(source: ThemeDefinition, increased = false): ThemeDefinition {
  const colors = {...source.colors};
  const web = source.web ? {...source.web} : undefined;
  const target = increased ? 7.05 : 4.65;
  const endpoint = contrastRatio('#000000', colors.background) > contrastRatio('#ffffff', colors.background) ? '#000000' : '#ffffff';
  // A middle-luminance canvas cannot support 7:1 text. Keep its hue, moving
  // only as far toward the palette's background endpoint as contrast needs.
  const backgroundFor = (color: string, minimum: number) => readableColor(color, [endpoint], minimum);
  if (increased) colors.background = backgroundFor(colors.background, 7.2);
  for (const role of ['panel', 'element', 'hover'] as const) {
    if (increased) {colors[role] = backgroundFor(colors[role], 7.2); continue;}
    if (contrastRatio(endpoint, colors[role]) >= 5) continue;
    let lo = 0, hi = 1;
    for (let step = 0; step < 18; step++) {const mid = (lo + hi) / 2; if (contrastRatio(endpoint, mix(colors[role], colors.background, mid)) >= 5) hi = mid; else lo = mid;}
    colors[role] = mix(colors[role], colors.background, hi);
  }
  const backgrounds = [colors.background, colors.panel, colors.element, colors.hover];
  if (web?.navigation) {
    // Custom navigation surfaces share the same readable text contract.
    if (increased) web.navigation = backgroundFor(web.navigation, 7.2);
    else if (contrastRatio(endpoint, web.navigation) < 5) web.navigation = colors.background;
    backgrounds.push(web.navigation);
  }
  for (const role of ['foreground', 'muted', 'link', 'emphasis', 'accent'] as const) colors[role] = readableColor(colors[role], backgrounds, target);
  if (increased) {
    for (const role of ['border', 'borderFocus'] as const) colors[role] = readableColor(colors[role], backgrounds, 3.1);
    colors.primary = readableColor(colors.primary, backgrounds, target);
    colors.onPrimary = readableColor(colors.onPrimary, [colors.primary], target);
    if (web?.quietBorder) web.quietBorder = colors.border;
    if (web?.inlineCodeBackground) web.inlineCodeBackground = backgroundFor(web.inlineCodeBackground, 7.2);
  }
  // Status badges/buttons use a 10–15% tint. Include those fills when deriving text.
  for (const role of ['success', 'warning', 'error', 'info'] as const) colors[role] = readableColor(colors[role], [...backgrounds, mix(colors.background, source.colors[role], 0.15)], increased ? 7.05 : 5);
  let codeBackground = web?.codeBackground ?? (source.code.background === source.colors.element ? colors.element : source.code.background);
  if (increased) {codeBackground = backgroundFor(codeBackground, 7.2); if (web?.codeBackground) web.codeBackground = codeBackground;}
  const syntax = {...source.syntax};
  for (const role of Object.keys(syntax) as (keyof typeof syntax)[]) syntax[role] = readableColor(syntax[role], [codeBackground], target);
  const markdown = {...source.markdown};
  for (const role of Object.keys(markdown) as (keyof typeof markdown)[]) markdown[role] = readableColor(markdown[role], role === 'code' && web?.inlineCodeBackground ? [web.inlineCodeBackground] : backgrounds, target);
  const tokens = Object.fromEntries(Object.entries(source.code.tokens).map(([name, token]) => {
    const background = token.background === source.code.background ? codeBackground : increased ? backgroundFor(token.background, 7.2) : token.background;
    return [name, {...token, background, color: readableColor(token.color, [background], target)}];
  }));
  return {...source, ...(web ? {web} : {}), colors, syntax, markdown, code: {...source.code, background: codeBackground, foreground: readableColor(source.code.foreground, [codeBackground], target), tokens}};
}
