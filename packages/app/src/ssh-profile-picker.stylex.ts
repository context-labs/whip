import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

export const sshStyles = stylex.create({
  column: { display: 'flex', flexDirection: 'column', gap: scale.space4, minWidth: 0 },
  title: { fontSize: typography.size13, fontWeight: 500, margin: 0 },
  muted: { fontSize: typography.size13, lineHeight: '20px', color: surface.secondaryText, margin: 0 },
  toolbar: { display: 'flex', alignItems: 'center', gap: scale.space2 },
  search: { flex: 1, minWidth: 0 },
  list: { height: 250, flexShrink: 0, overflowY: 'auto', overscrollBehavior: 'contain', padding: scale.space1, border: `1px solid ${surface.controlBorder}`, borderRadius: scale.radiusControl, backgroundColor: colors.panel },
  empty: { height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: scale.space2, textAlign: 'center', padding: scale.space5 },
  skeleton: { height: 52, marginBottom: scale.space2 },
  summary: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space2, paddingBottom: scale.space4, borderBottom: `1px solid ${surface.controlBorder}` },
  disclosure: { width: '100%' },
  advancedTitle: { display: 'flex', flex: 1, alignItems: 'center', justifyContent: 'space-between', gap: scale.space2 },
  hint: { color: surface.secondaryText, fontSize: typography.size12, fontWeight: 400 },
  fields: { display: 'flex', gap: scale.space3, flexWrap: 'wrap' },
  field: { flex: '1 1 180px', minWidth: 0 },
});
