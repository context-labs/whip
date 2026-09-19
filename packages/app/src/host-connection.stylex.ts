import * as stylex from '@stylexjs/stylex';
import { scale, surface, typography } from '@whip/ui/tokens.stylex';

export const connectionStyles = stylex.create({
  connection: { display: 'flex', flexDirection: 'column', minWidth: 0 },
  body: { display: 'flex', flexDirection: 'column', gap: scale.space4, minHeight: 200, paddingBottom: scale.space4, minWidth: 0, outline: 'none' },
  progress: { display: 'flex', alignItems: 'center', gap: scale.space3, flex: 1, overflowWrap: 'anywhere', fontSize: typography.size13, color: surface.secondaryText },
  promptTitle: { margin: 0, fontSize: typography.size13, fontWeight: 500 },
  message: { margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '30dvh', overflowY: 'auto' },
  footer: { display: 'flex', flexWrap: 'wrap', justifyContent: 'flex-end', gap: scale.space2, paddingTop: scale.space4, borderTop: `1px solid ${surface.quietBorder}` },
});
