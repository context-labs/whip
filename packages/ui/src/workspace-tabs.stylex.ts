import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from './tokens.stylex';

export const tabMarker = stylex.defineMarker();
export const styles = stylex.create({
  row: { display: 'flex', alignItems: 'center', gap: 4, height: 48, flexShrink: 0, minWidth: 0, paddingInline: scale.space3, backgroundColor: surface.navigation, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, fontFamily: typography.sans },
  list: { display: 'flex', alignItems: 'center', gap: 4, flex: '1 1 auto', minWidth: 0, height: '100%', overflowX: 'auto', overflowY: 'hidden', scrollbarWidth: 'none', overscrollBehaviorX: 'contain' },
  semanticList: { position: 'absolute', width: 1, height: 1, margin: -1, overflow: 'hidden', clipPath: 'inset(50%)', whiteSpace: 'nowrap' },
  item: { display: 'flex', alignItems: 'center', flex: '1 0 144px', width: 200, minWidth: 144, maxWidth: 224, height: { default: 36, [scale.touch]: 44 }, borderRadius: scale.radiusControl, borderWidth: 1, borderStyle: 'solid', borderColor: 'transparent', backgroundColor: { default: 'transparent', ':hover': `color-mix(in srgb, ${colors.hover} 40%, ${surface.navigation})` }, position: 'relative' },
  active: { backgroundColor: { default: colors.background, ':hover': colors.background }, borderColor: surface.quietBorder },
  tab: { display: 'flex', alignItems: 'center', gap: 8, flex: '1 1 auto', minWidth: 0, height: '100%', paddingInlineStart: scale.space3, paddingInlineEnd: scale.space1, borderWidth: 0, borderStyle: 'none', backgroundColor: 'transparent', color: surface.secondaryText, fontFamily: typography.sans, fontSize: 13, fontWeight: 500, lineHeight: '20px', textDecoration: 'none', textAlign: 'start', whiteSpace: 'nowrap', cursor: 'pointer', userSelect: 'none', borderRadius: scale.radiusControl, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  activeText: { color: colors.foreground },
  status: { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flex: '0 0 14px', width: 14, height: 14 },
  title: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' },
  metadata: { flex: '0 0 auto', maxWidth: 56, overflow: 'hidden', textOverflow: 'ellipsis', color: surface.secondaryText, fontSize: 11 },
  menu: { display: 'flex', alignItems: 'center', flexShrink: 0, opacity: { default: 0, [stylex.when.ancestor(':hover', tabMarker)]: 1, [stylex.when.ancestor(':focus-within', tabMarker)]: 1, [scale.touch]: 1 } },
  menuVisible: { opacity: 1 },
  close: { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: { default: 24, [scale.touch]: 44 }, height: { default: 24, [scale.touch]: 44 }, flexShrink: 0, padding: 0, marginInlineEnd: scale.space1, borderWidth: 0, borderStyle: 'none', borderRadius: scale.radiusSmall, color: surface.secondaryText, backgroundColor: { default: 'transparent', ':hover': colors.element }, cursor: 'pointer', opacity: { default: 0, [stylex.when.ancestor(':hover', tabMarker)]: 1, [stylex.when.ancestor(':focus-within', tabMarker)]: 1, [scale.touch]: 1 }, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  closeVisible: { opacity: 1 },
  dragging: { borderColor: colors.borderFocus, backgroundColor: colors.element },
  utilities: { display: 'flex', alignItems: 'center', gap: 4, flex: '0 0 auto' },
});
