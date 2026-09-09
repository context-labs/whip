import { typography } from '@whip/ui/tokens.stylex';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';

export const sessionMarker = stylex.defineMarker();
export const directoryMarker = stylex.defineMarker();

export const styles = stylex.create({
  hosts: { display: 'flex', flexDirection: 'column', gap: 8 },
  host: { display: 'flex', flexDirection: 'column' },
  collapsedHost: { flex: '0 0 auto', minHeight: 0 },
  hostHeading: { flexShrink: 0, color: colors.foreground },
  brandRow: { minHeight: 40, display: 'flex', alignItems: 'center', flexShrink: 0 },
  // Inset window chrome: the brand row is an empty window drag strip whose
  // height matches the tab strip so the traffic lights sit centered in both;
  // only the collapse toggle opts out of dragging at the trailing edge.
  brandRowInset: { minHeight: 48, marginInline: -8, paddingInline: 8, justifyContent: 'flex-end', userSelect: 'none' },
  // The wordmark sits below the traffic-light strip, aligned with the nav
  // item icons below it (their left edge: aside padding 8 + destination
  // paddingInline 8 + icon offset 1 = 17px from the sidebar's border box).
  wordmarkBelow: { display: 'flex', paddingInlineStart: 9, paddingBlockEnd: 10 },
  brandRowAction: { display: 'flex', alignItems: 'center', flexShrink: 0 },
  wordmark: { fontSize: typography.size28 },
  // The wordmark link is a plain graphic home link — no destination row's
  // hover container, just the mark itself.
  wordmarkLink: { display: 'flex', alignItems: 'center', paddingInline: 8, borderRadius: 6, color: colors.foreground },
  destinations: { display: 'flex', flexDirection: 'column', flexShrink: 0, marginBottom: 12 },
  destination: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, minHeight: { default: 28, [scale.touch]: 44 }, paddingBlock: 0, paddingInline: 8, borderWidth: 0, borderRadius: 6, font: 'inherit', fontSize: typography.size13, lineHeight: `calc(${typography.size13} * 18 / 13)`, textAlign: 'left', textDecoration: 'none', cursor: 'pointer', color: surface.secondaryText, backgroundColor: { default: 'transparent', ':hover': colors.hover } },
  primaryDestination: { color: colors.foreground },
  footer: { minHeight: 48, display: 'flex', alignItems: 'center', flexShrink: 0, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, marginInline: -8, marginBottom: -8, paddingInline: 8 },
  list: { overflowAnchor: 'none', scrollbarWidth: 'thin' },
  group: { display: 'flex', alignItems: 'end', paddingBottom: 2, gap: 2 },
  groupButton: { fontSize: typography.size12, gap: 5, paddingInline: 4, backgroundColor: { default: 'transparent', ':hover': 'transparent' } },
  icon: { minHeight: { default: 28, [scale.touch]: 44 }, height: { default: 28, [scale.touch]: 44 }, width: { default: 28, [scale.touch]: 44 }, minWidth: { default: 28, [scale.touch]: 44 }, padding: 0, justifyContent: 'center', flexShrink: 0, color: surface.secondaryText },
  sessionRow: { display: 'flex', alignItems: 'center', height: '100%', borderRadius: 6, color: surface.secondaryText, backgroundColor: { default: 'transparent', ':hover': colors.element } },
  selected: { backgroundColor: { default: colors.hover, ':hover': colors.hover }, color: colors.foreground },
  sessionLink: { display: 'flex', alignItems: 'center', flex: 1, minWidth: 0, height: '100%', gap: 8, paddingInline: 10, color: 'inherit', textDecoration: 'none', borderRadius: 6, lineHeight: `calc(${typography.size13} * 18 / 13)` },
  indicator: { display: 'flex', alignItems: 'center', width: 12, flexShrink: 0 },
  attention: { color: colors.warning },
  title: { display: 'block' },
  sessionMenu: { opacity: { default: 0, [stylex.when.ancestor(':hover', sessionMarker)]: 1, [stylex.when.ancestor(':focus-within', sessionMarker)]: 1, ':is([aria-expanded="true"])': 1, [scale.touch]: 1 } },
  caret: { display: 'flex', flexShrink: 0, opacity: { default: 0, [stylex.when.ancestor(':hover', directoryMarker)]: 1, [stylex.when.ancestor(':focus-within', directoryMarker)]: 1, [scale.touch]: 1 } },
  revealed: { opacity: 1 },
});
