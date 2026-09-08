import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from './tokens.stylex';

export const styles = stylex.create({
  workspace: { position: 'relative', flex: '1 1 auto', width: '100%', height: '100%', minWidth: 0, minHeight: 0, overflow: 'hidden', backgroundColor: colors.background },
  tree: { position: 'absolute', inset: 0 },
  pane: { display: 'flex', flexDirection: 'column', width: '100%', height: '100%', minWidth: 0, minHeight: 0, overflow: 'hidden' },
  header: { position: 'relative', zIndex: 2, flex: '0 0 auto', minWidth: 0 },
  slot: { flex: '1 1 auto', minHeight: 0, minWidth: 0 },
  content: { position: 'absolute', display: 'flex', flexDirection: 'column', minWidth: 0, minHeight: 0, overflowX: 'hidden', overflowY: 'auto', zIndex: 1, backgroundColor: colors.background, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  unmeasured: { visibility: 'hidden' },
  separator: { position: 'relative', zIndex: 3, backgroundColor: surface.quietBorder, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  horizontalSeparator: { width: 1, cursor: 'col-resize' },
  verticalSeparator: { height: 1, cursor: 'row-resize' },
  separatorTarget: { position: 'absolute', inset: { default: -4, [scale.touch]: -9 } },
  preview: { position: 'absolute', zIndex: 4, pointerEvents: 'none', borderWidth: 2, borderStyle: 'solid', borderColor: colors.borderFocus, backgroundColor: `color-mix(in srgb, ${colors.borderFocus} 14%, transparent)`, borderRadius: 3 },
  insertion: { borderRadius: 0, backgroundColor: colors.borderFocus, borderWidth: 0 },
});
