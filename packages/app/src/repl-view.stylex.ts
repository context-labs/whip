import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

export const styles = stylex.create({
  root: { display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0, minWidth: 0 },
  toolbar: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 8, padding: '4px 12px', borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, fontSize: typography.size13 },
  title: { display: 'flex', alignItems: 'center', gap: 6, fontWeight: 550, marginRight: 4 },
  count: { fontSize: typography.size12, color: surface.secondaryText, marginLeft: 'auto', whiteSpace: 'nowrap' },
  notice: { fontSize: typography.size12, lineHeight: 1.5, color: surface.secondaryText, padding: '8px 16px', margin: 0 },
  content: { maxWidth: 'none', paddingInline: 16, paddingBlock: 16 },
  cell: { paddingBlock: 12, paddingLeft: 12, marginBottom: 12, minWidth: 0, borderLeftWidth: 2, borderLeftStyle: 'solid', borderLeftColor: surface.quietBorder },
  running: { borderLeftColor: colors.primary },
  failed: { borderLeftColor: colors.error },
  interrupted: { borderLeftColor: colors.warning },
  header: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 8, minHeight: { default: 28, [scale.touch]: 44 }, marginBottom: 8, fontSize: typography.size12 },
  ordinal: { fontFamily: typography.mono, fontSize: typography.codeSize, fontWeight: 500 },
  meta: { color: surface.secondaryText, fontSize: typography.size12 },
  grow: { flex: 1 },
  code: { borderWidth: 0, borderStyle: 'none', borderRadius: 6 },
  hosts: { display: 'flex', flexDirection: 'column', gap: 5, paddingBlock: 10, fontFamily: typography.mono, fontSize: typography.codeSize, lineHeight: 1.6 },
  host: { display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 },
  hostName: { overflowWrap: 'anywhere' },
  duration: { color: surface.secondaryText, marginLeft: 'auto', whiteSpace: 'nowrap' },
  error: { color: colors.error, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: typography.mono, fontSize: typography.codeSize, lineHeight: 1.65, marginBlock: 8 },
  section: { marginTop: 10, minWidth: 0 },
  result: { marginTop: 10, minWidth: 0 },
  restart: { display: 'flex', alignItems: 'center', gap: 8, color: surface.secondaryText, fontSize: typography.size12, lineHeight: 1.6, paddingBlock: 16, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  empty: { display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', minHeight: 180, padding: 24, textAlign: 'center', gap: 8, color: surface.secondaryText, fontSize: typography.size13 },
});
