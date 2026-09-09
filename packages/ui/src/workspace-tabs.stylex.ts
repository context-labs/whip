import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from './tokens.stylex';

export const tabMarker = stylex.defineMarker();
export const styles = stylex.create({
  row: { display: 'flex', alignItems: 'center', gap: 4, height: 48, flexShrink: 0, minWidth: 0, paddingInline: scale.space3, backgroundColor: surface.navigation, position: 'relative', fontFamily: typography.sans, '::before': { content: '""', position: 'absolute', insetInline: 0, bottom: 0, height: 1, backgroundColor: surface.quietBorder, pointerEvents: 'none' } },
  list: { position: 'relative', display: 'flex', alignItems: 'flex-end', gap: 4, flex: '1 1 auto', minWidth: 0, height: '100%', paddingInline: 12, overflowX: 'auto', overflowY: 'hidden', scrollbarWidth: 'none', overscrollBehaviorX: 'contain' },
  semanticList: { position: 'absolute', width: 1, height: 1, margin: -1, overflow: 'hidden', clipPath: 'inset(50%)', whiteSpace: 'nowrap' },
  item: { display: 'flex', alignItems: 'center', flex: '1 0 144px', width: 200, minWidth: 144, maxWidth: 224, height: { default: 42, [scale.touch]: 44 }, borderWidth: 0, borderStyle: 'none', position: 'relative', isolation: 'isolate', opacity: { default: 1, ':is([data-tab-settling])': 0 } },
  active: { zIndex: 1 },
  shape: { position: 'absolute', inset: 0, zIndex: -1, pointerEvents: 'none', color: { default: 'transparent', [stylex.when.ancestor(':hover', tabMarker)]: `color-mix(in srgb, ${colors.hover} 40%, ${surface.navigation})` }, stroke: 'transparent' },
  shapeActive: { color: { default: colors.background, [stylex.when.ancestor(':hover', tabMarker)]: colors.background }, stroke: surface.quietBorder },
  shapeCenter: { position: 'absolute', insetBlock: 0, insetInline: 12, backgroundColor: 'currentColor', borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: 'transparent' },
  shapeCenterActive: { borderTopColor: surface.quietBorder },
  shapeEdge: { position: 'absolute', top: 0, left: -12, width: 24, height: '100%', overflow: 'visible' },
  shapeEdgeRight: { left: 'auto', right: -12, transform: 'scaleX(-1)' },
  divider: { position: 'absolute', insetInlineEnd: -2, top: 12, height: 18, width: 1, backgroundColor: surface.quietBorder, pointerEvents: 'none' },
  tab: { display: 'flex', alignItems: 'center', gap: 8, flex: '1 1 auto', minWidth: 0, height: '100%', paddingInlineStart: scale.space3, paddingInlineEnd: scale.space1, borderWidth: 0, borderStyle: 'none', backgroundColor: 'transparent', color: surface.secondaryText, fontFamily: typography.sans, fontSize: 13, fontWeight: 500, lineHeight: '20px', textDecoration: 'none', textAlign: 'start', whiteSpace: 'nowrap', cursor: 'pointer', userSelect: 'none', borderStartStartRadius: scale.radiusDialog, borderStartEndRadius: scale.radiusDialog, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  activeText: { color: colors.foreground },
  status: { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flex: '0 0 14px', width: 14, height: 14 },
  title: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' },
  metadata: { flex: '0 0 auto', maxWidth: 56, overflow: 'hidden', textOverflow: 'ellipsis', color: surface.secondaryText, fontSize: 11 },
  menu: { display: 'flex', alignItems: 'center', flexShrink: 0, opacity: { default: 0, [stylex.when.ancestor(':hover', tabMarker)]: 1, [stylex.when.ancestor(':focus-within', tabMarker)]: 1, [scale.touch]: 1 } },
  menuVisible: { opacity: 1 },
  close: { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: { default: 24, [scale.touch]: 44 }, height: { default: 24, [scale.touch]: 44 }, flexShrink: 0, padding: 0, marginInlineEnd: scale.space1, borderWidth: 0, borderStyle: 'none', borderRadius: scale.radiusSmall, color: surface.secondaryText, backgroundColor: { default: 'transparent', ':hover': colors.element }, cursor: 'pointer', opacity: { default: 0, [stylex.when.ancestor(':hover', tabMarker)]: 1, [stylex.when.ancestor(':focus-within', tabMarker)]: 1, [scale.touch]: 1 }, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  closeVisible: { opacity: 1 },
  dragging: { opacity: 0 },
  preview: { position: 'fixed', top: 0, left: 0, zIndex: 110, pointerEvents: 'none', maxWidth: 'none', minWidth: 0, margin: 0, color: colors.foreground, fontFamily: typography.sans, willChange: 'transform' },
  dragFace: { pointerEvents: 'none' },
  utilities: { display: 'flex', alignItems: 'center', gap: 4, flex: '0 0 auto' },
  // Inset window chrome (desktop hosts with a hidden title bar): the strip —
  // including the list's empty stretch — is the window drag region; only tab
  // items and utility cells opt out, and the traffic-light zone reserves
  // space. Zone width matches the host's traffic-light position
  // (x: 12, three dots, ~8px gaps) plus trailing clearance.
  windowDrag: { WebkitAppRegion: 'drag', userSelect: 'none' },
  windowNoDrag: { WebkitAppRegion: 'no-drag' },
  trafficLightInset: { paddingInlineStart: 84 },
});
