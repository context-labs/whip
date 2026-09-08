import * as stylex from '@stylexjs/stylex';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';

export const sessionMarker = stylex.defineMarker();
export const directoryMarker = stylex.defineMarker();

export const styles = stylex.create({
  brandRow: { minHeight: 40, display: 'flex', alignItems: 'center', flexShrink: 0 },
  destinations: { display: 'flex', flexDirection: 'column', flexShrink: 0, marginBottom: 12 },
  destination: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, minHeight: { default: 28, [scale.touch]: 44 }, paddingBlock: 0, paddingInline: 8, borderWidth: 0, borderRadius: 6, font: 'inherit', fontSize: 13, lineHeight: '18px', textAlign: 'left', textDecoration: 'none', cursor: 'pointer', color: surface.secondaryText, backgroundColor: { default: 'transparent', ':hover': colors.hover } },
  primaryDestination: { color: colors.foreground },
  footer: { minHeight: 48, display: 'flex', alignItems: 'center', flexShrink: 0, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, marginInline: -8, marginBottom: -8, paddingInline: 8 },
  list: { overflowAnchor: 'none', scrollbarWidth: 'thin' },
  group: { display: 'flex', alignItems: 'end', paddingBottom: 2, gap: 2 },
  groupButton: { fontSize: 12, gap: 5, paddingInline: 4, backgroundColor: { default: 'transparent', ':hover': 'transparent' } },
  icon: { minHeight: { default: 28, [scale.touch]: 44 }, height: { default: 28, [scale.touch]: 44 }, width: { default: 28, [scale.touch]: 44 }, minWidth: { default: 28, [scale.touch]: 44 }, padding: 0, justifyContent: 'center', flexShrink: 0, color: surface.secondaryText },
  sessionRow: { display: 'flex', alignItems: 'center', height: '100%', borderRadius: 6, color: surface.secondaryText, backgroundColor: { default: 'transparent', ':hover': colors.element } },
  selected: { backgroundColor: { default: colors.hover, ':hover': colors.hover }, color: colors.foreground },
  sessionLink: { display: 'flex', alignItems: 'center', flex: 1, minWidth: 0, height: '100%', gap: 8, paddingInline: 10, color: 'inherit', textDecoration: 'none', borderRadius: 6, lineHeight: '18px' },
  indicator: { display: 'flex', alignItems: 'center', width: 12, flexShrink: 0 },
  attention: { color: colors.warning },
  title: { display: 'block' },
  sessionMenu: { opacity: { default: 0, [stylex.when.ancestor(':hover', sessionMarker)]: 1, [stylex.when.ancestor(':focus-within', sessionMarker)]: 1, ':is([aria-expanded="true"])': 1, [scale.touch]: 1 } },
  caret: { display: 'flex', flexShrink: 0, opacity: { default: 0, [stylex.when.ancestor(':hover', directoryMarker)]: 1, [stylex.when.ancestor(':focus-within', directoryMarker)]: 1, [scale.touch]: 1 } },
  revealed: { opacity: 1 },
});
